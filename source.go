package yoink

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type executionBudget struct {
	mu                              sync.Mutex
	inputLimit, inputUsed, reserved int64
	probing, exceeded               bool
	changed                         chan struct{}
	operations, operationLimit      int
	output, outputLimit             int64
}

func newBudget(o Options) *executionBudget {
	return &executionBudget{inputLimit: o.MaxInputBytes, operationLimit: o.MaxOperations, outputLimit: o.MaxOutputBytes, changed: make(chan struct{})}
}
func (b *executionBudget) operation() error {
	if b.operations == b.operationLimit {
		return &LimitError{"operations", int64(b.operationLimit)}
	}
	b.operations++
	return nil
}
func (b *executionBudget) emit(n int) error {
	if int64(n) > b.outputLimit-b.output {
		return &LimitError{"output bytes", b.outputLimit}
	}
	b.output += int64(n)
	return nil
}

// Reservations precede reads, so simultaneous readers cannot spend the same
// remaining bytes. At exhaustion, one shared probe distinguishes EOF from excess.
func (b *executionBudget) take(ctx context.Context, want int) (int, bool, error) {
	for {
		if err := ctx.Err(); err != nil {
			return 0, false, err
		}
		b.mu.Lock()
		if b.exceeded {
			b.mu.Unlock()
			return 0, false, &LimitError{"input bytes", b.inputLimit}
		}
		remaining := b.inputLimit - b.inputUsed - b.reserved
		if remaining > 0 {
			n := int64(want)
			if n > remaining {
				n = remaining
			}
			b.reserved += n
			b.mu.Unlock()
			return int(n), false, nil
		}
		if b.reserved == 0 && !b.probing {
			b.probing = true
			b.mu.Unlock()
			return 1, true, nil
		}
		changed := b.changed
		b.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return 0, false, ctx.Err()
		}
	}
}
func (b *executionBudget) finish(reserved, n int, probe bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if probe {
		b.probing = false
		if n > 0 {
			b.exceeded = true
		}
	} else {
		b.reserved -= int64(reserved)
	}
	b.inputUsed += int64(n)
	close(b.changed)
	b.changed = make(chan struct{})
	if b.exceeded {
		return &LimitError{"input bytes", b.inputLimit}
	}
	return nil
}

type budgetReader struct {
	ctx context.Context
	r   io.Reader
	b   *executionBudget
}

func (r budgetReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n, probe, err := r.b.take(r.ctx, len(p))
	if err != nil {
		return 0, err
	}
	used, readErr := r.r.Read(p[:n])
	if used < 0 || used > n {
		r.b.finish(n, 0, probe)
		return 0, fmt.Errorf("invalid io.Reader byte count %d", used)
	}
	if err := r.b.finish(n, used, probe); err != nil {
		return 0, err
	}
	if err := r.ctx.Err(); err != nil {
		return used, err
	}
	return used, readErr
}

type sourceRef struct {
	name, identity string
	base           string
	remote         *url.URL
}
type sourceDocument struct {
	source sourceRef
	data   []byte
	lo, hi int
	depth  int
	root   bool
	key    expansionKey
}
type expansionKey struct {
	identity string
	base     string
	root     bool
	lo, hi   int
	hash     [32]byte
}

func localSource(name string) (sourceRef, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return sourceRef{}, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if !os.IsNotExist(err) {
			return sourceRef{}, err
		}
		canonical = absolute
	}
	return sourceRef{name: name, identity: canonical, base: filepath.Dir(canonical)}, nil
}
func remoteSource(u *url.URL) sourceRef {
	normalized := *u
	normalized.Scheme = strings.ToLower(u.Scheme)
	normalized.Fragment = ""
	normalized.RawFragment = ""
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port != "" && !(normalized.Scheme == "http" && port == "80") && !(normalized.Scheme == "https" && port == "443") {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	normalized.Host = host
	if normalized.Path == "" {
		normalized.Path = "/"
	}
	// RFC URL resolution removes dot segments while preserving meaningful empty
	// path segments and encoded separators, unlike filesystem path cleaning.
	normalized = *normalized.ResolveReference(&url.URL{})
	return sourceRef{name: u.String(), identity: normalized.String(), remote: u}
}
func rootSource(name string) (sourceRef, error) {
	if u, err := url.Parse(name); err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
		return remoteSource(u), nil
	}
	source, err := localSource(name)
	if err != nil {
		return sourceRef{}, err
	}
	// The caller owns the root Reader: its supplied name determines its base,
	// independently of any same-named file's contents or symlink target.
	absolute, err := filepath.Abs(name)
	if err != nil {
		return sourceRef{}, err
	}
	source.base = filepath.Dir(absolute)
	return source, nil
}

func fetchSource(ctx context.Context, from sourceRef, target string, b *executionBudget) (sourceRef, []byte, error) {
	if err := ctx.Err(); err != nil {
		return sourceRef{}, nil, err
	}
	// A local filename may contain percent signs and URL-reserved characters.
	// Parse as a URL only for a remote origin or an explicit URI scheme.
	u := &url.URL{}
	colon := strings.IndexByte(target, ':')
	explicitScheme := colon > 0 && !filepath.IsAbs(target) && filepath.VolumeName(target) == "" && !strings.ContainsAny(target[:colon], "/\\")
	var err error
	if from.remote != nil || explicitScheme {
		u, err = url.Parse(target)
		if err != nil {
			return sourceRef{}, nil, err
		}
	}
	remote := from.remote != nil || strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
	if u.Scheme != "" && !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") && filepath.VolumeName(target) == "" {
		return sourceRef{}, nil, fmt.Errorf("unsupported inclusion URL scheme %q", u.Scheme)
	}
	if remote {
		if from.remote != nil {
			u = from.remote.ResolveReference(u)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			u.Scheme = strings.ToLower(u.Scheme)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return sourceRef{}, nil, fmt.Errorf("unsupported inclusion URL scheme %q", u.Scheme)
		}
		if sem != nil {
			if err := sem.acquireContext(ctx); err != nil {
				return sourceRef{}, nil, err
			}
			defer sem.Release()
		}
		if throttle > 0 {
			timer := time.NewTimer(throttle)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return sourceRef{}, nil, ctx.Err()
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return sourceRef{}, nil, err
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return sourceRef{}, nil, err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return sourceRef{}, nil, fmt.Errorf("GET %s: HTTP %s", u, response.Status)
		}
		final := u
		if response.Request != nil && response.Request.URL != nil {
			final = response.Request.URL
		}
		if final.Scheme != "http" && final.Scheme != "https" {
			return sourceRef{}, nil, fmt.Errorf("unsupported redirected URL scheme %q", final.Scheme)
		}
		data, err := io.ReadAll(budgetReader{ctx, response.Body, b})
		return remoteSource(final), data, err
	}
	filename := target
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(from.base, filename)
	}
	filename, err = filepath.Abs(filename)
	if err != nil {
		return sourceRef{}, nil, err
	}
	info, err := os.Stat(filename)
	if err != nil {
		return sourceRef{}, nil, err
	}
	if !info.Mode().IsRegular() {
		return sourceRef{}, nil, fmt.Errorf("%s: inclusion requires a regular file", filename)
	}
	f, err := os.Open(filename)
	if err != nil {
		return sourceRef{}, nil, err
	}
	defer f.Close()
	source, err := localSource(filename)
	if err != nil {
		return sourceRef{}, nil, err
	}
	data, err := io.ReadAll(budgetReader{ctx, f, b})
	return source, data, err
}

func includedDocument(ctx context.Context, job compositionJob, b *executionBudget) (*sourceDocument, error) {
	parts := strings.Fields(job.text)
	if len(parts) < 2 {
		return nil, fmt.Errorf("syntax error for .yoink invocation")
	}
	source, data, err := fetchSource(ctx, job.source, parts[1], b)
	if err != nil {
		return nil, err
	}
	addr := ""
	if len(parts) > 2 {
		addr = strings.Join(parts[2:], " ")
	}
	lo, hi, err := addrToByteRange(addr, 0, data)
	if err != nil {
		return nil, err
	}
	if lo > hi {
		lo, hi = hi, lo
	}
	for lo > 0 && data[lo-1] != '\n' {
		lo--
	}
	if hi > 0 {
		for hi < len(data) && data[hi-1] != '\n' {
			hi++
		}
	}
	return &sourceDocument{source: source, data: data, lo: lo, hi: hi, depth: job.depth, key: expansionKey{identity: source.identity, base: source.base, lo: lo, hi: hi, hash: sha256.Sum256(data[lo:hi])}}, nil
}

// visitIncluded retains original physical source line numbers through selection,
// OMIT filtering, and edge trimming. It keeps no per-line source slice inventory.
func visitIncluded(ctx context.Context, doc *sourceDocument, visit func([]byte, int) error) error {
	selected := doc.data[doc.lo:doc.hi]
	first, last := -1, -1
	for offset := 0; offset < len(selected); {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := bytes.IndexByte(selected[offset:], '\n')
		next := len(selected)
		if end < 0 {
			end = len(selected)
		} else {
			end += offset
			next = end + 1
		}
		line := bytes.TrimSuffix(selected[offset:end], []byte{'\r'})
		if len(line) > 0 && !bytes.HasSuffix(line, []byte("OMIT")) {
			if first < 0 {
				first = offset
			}
			last = next
		}
		offset = next
	}
	if first < 0 {
		return nil
	}
	lineNumber := 1 + bytes.Count(doc.data[:doc.lo], []byte{'\n'}) + bytes.Count(selected[:first], []byte{'\n'})
	for offset := first; offset < last; lineNumber++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := bytes.IndexByte(selected[offset:last], '\n')
		next := last
		if end < 0 {
			end = last
		} else {
			end += offset
			next = end + 1
		}
		line := bytes.TrimSuffix(selected[offset:end], []byte{'\r'})
		if !bytes.HasSuffix(line, []byte("OMIT")) {
			if err := visit(line, lineNumber); err != nil {
				return err
			}
		}
		offset = next
	}
	return nil
}
