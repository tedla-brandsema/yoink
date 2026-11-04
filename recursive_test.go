package yoink

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func recursiveParse(t *testing.T, dir, input string, o Options) (string, error) {
	t.Helper()
	return ParseWithOptions(context.Background(), strings.NewReader(input), filepath.Join(dir, "root.md"), o)
}

func TestRecursiveDepthsAndProvenance(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sections", "deeper"), 0755)
	os.Mkdir(filepath.Join(dir, "knowledge"), 0755)
	writeTempFile(t, dir, "sections/one", "one\n.yoink deeper/two\nend-one\n")
	writeTempFile(t, dir, "sections/deeper/two", "two\n.yoink ../../knowledge/three\nend-two\n")
	writeTempFile(t, dir, "knowledge/three", "three\n")
	for _, tt := range []struct {
		depth int
		want  string
	}{
		{1, "root\none\n.yoink deeper/two\nend-one\nend-root"},
		{2, "root\none\ntwo\n.yoink ../../knowledge/three\nend-two\nend-one\nend-root"},
		{3, "root\none\ntwo\nthree\nend-two\nend-one\nend-root"},
		{-1, "root\none\ntwo\nthree\nend-two\nend-one\nend-root"},
	} {
		got, err := recursiveParse(t, dir, "root\n.yoink sections/./one\nend-root", Options{MaxDepth: tt.depth})
		if err != nil || got != tt.want {
			t.Fatalf("depth%d: %q %v want%q", tt.depth, got, err, tt.want)
		}
	}
}

func TestRecursiveSelectionsAndErrors(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "one", ".yoink excluded-missing\n\nBEGIN OMIT\n.yoink two /START/,/END/\nEND OMIT\n.yoink excluded-missing\n")
	writeTempFile(t, dir, "two", ".yoink excluded-missing\nSTART OMIT\n keep \n.yoink ignored OMIT\nEND OMIT\n.yoink excluded-missing\n")
	got, err := recursiveParse(t, dir, ".yoink one /BEGIN/,/END/", Options{MaxDepth: -1})
	if err != nil || got != " keep " {
		t.Fatalf("%q %v", got, err)
	}
	writeTempFile(t, dir, "two", ".yoink excluded-missing\nSTART OMIT\n.yoink absent\nEND OMIT\n")
	got, err = recursiveParse(t, dir, ".yoink one /BEGIN/,/END/", Options{MaxDepth: -1})
	if got != "" || !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), filepath.Join(dir, "two")+":3:") {
		t.Fatalf("source line and identity lost: %q %v", got, err)
	}
	for _, bad := range []string{`.yoink two /trailing\`, ".yoink two /[/", ".yoink two /never-matches/", ".yoink"} {
		writeTempFile(t, dir, "one", "\n// comment\n"+bad+"\n")
		got, err = recursiveParse(t, dir, ".yoink one", Options{MaxDepth: -1})
		if got != "" || err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "one")+":3:") {
			t.Fatalf("nested malformed %q: %q %v", bad, got, err)
		}
	}
}

func TestRecursiveCyclesAndRepeatedSources(t *testing.T) {
	dir := t.TempDir()
	a := writeTempFile(t, dir, "a", ".yoink b\n")
	writeTempFile(t, dir, "b", ".yoink c\n")
	writeTempFile(t, dir, "c", ".yoink ./a\n")
	got, err := recursiveParse(t, dir, ".yoink a", Options{MaxDepth: -1})
	if got != "" || !errors.Is(err, ErrCycle) {
		t.Fatalf("%q %v", got, err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if !strings.Contains(err.Error(), filepath.Join(dir, name)) {
			t.Fatal(err)
		}
	}
	writeTempFile(t, dir, "a", ".yoink a\n")
	got, err = recursiveParse(t, dir, ".yoink a", Options{MaxDepth: 2})
	if err != nil || got != ".yoink a" {
		t.Fatalf("finite cycle should stop literally: %q %v", got, err)
	}
	got, err = recursiveParse(t, dir, ".yoink a", Options{MaxDepth: -1})
	if got != "" || !errors.Is(err, ErrCycle) {
		t.Fatalf("self cycle: %q %v", got, err)
	}
	writeTempFile(t, dir, "a", ".yoink a /^payload$/\npayload\n")
	got, err = recursiveParse(t, dir, ".yoink a", Options{MaxDepth: -1})
	if err != nil || got != "payload\npayload" {
		t.Fatalf("terminating selected self: %q %v", got, err)
	}
	// Same range containing its own directive must still be a cycle.
	writeTempFile(t, dir, "a", ".yoink a 1\n")
	got, err = recursiveParse(t, dir, ".yoink a", Options{MaxDepth: -1})
	if got != "" || !errors.Is(err, ErrCycle) {
		t.Fatalf("selected cycle: %q %v", got, err)
	}
	writeTempFile(t, dir, "a", ".yoink shared\n")
	writeTempFile(t, dir, "b", ".yoink shared\n")
	writeTempFile(t, dir, "shared", "common\n")
	got, err = recursiveParse(t, dir, ".yoink a\n.yoink b", Options{MaxDepth: -1})
	if err != nil || got != "common\ncommon" {
		t.Fatalf("cross-branch reuse: %q %v", got, err)
	}
	// Caller Reader bytes differ from a real file of the same name.
	got, err = ParseWithOptions(context.Background(), strings.NewReader(".yoink a"), a, Options{MaxDepth: -1})
	if err != nil || got != "common" {
		t.Fatalf("root reader identity false cycle: %q %v", got, err)
	}
}

func TestRecursiveSymlinkAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires platform privileges")
	}
	dir := t.TempDir()
	writeTempFile(t, dir, "a", ".yoink alias\n")
	if err := os.Symlink("a", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	got, err := recursiveParse(t, dir, ".yoink a", Options{MaxDepth: -1})
	if got != "" || !errors.Is(err, ErrCycle) {
		t.Fatalf("symlink cycle: %q %v", got, err)
	}
}

func TestRecursiveCustomOutputAndOrder(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "one", ".generated\n.ordered-recursive first\n.ordered-recursive second\n")
	first := make(chan struct{})
	second := make(chan struct{})
	RegisterParserFunc("generated", func(string, int, string) (string, error) { return ".yoink absent", nil })
	RegisterParserFunc("ordered-recursive", func(_ string, _ int, text string) (string, error) {
		if strings.HasSuffix(text, "first") {
			close(first)
			<-second
			return "first", nil
		}
		<-first
		close(second)
		return "second", nil
	})
	got, err := recursiveParse(t, dir, ".yoink one", Options{MaxDepth: -1})
	if err != nil || got != ".yoink absent\nfirst\nsecond" {
		t.Fatalf("%q %v", got, err)
	}
	sentinel := errors.New("nested custom")
	RegisterParserFunc("nested-error", func(string, int, string) (string, error) { return "ignored", sentinel })
	writeTempFile(t, dir, "one", "\n.nested-error\n")
	got, err = recursiveParse(t, dir, ".yoink one", Options{MaxDepth: -1})
	if got != "" || !errors.Is(err, sentinel) || !strings.Contains(err.Error(), filepath.Join(dir, "one")+":2:") {
		t.Fatalf("%q %v", got, err)
	}
}

func TestRecursiveLimits(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "one", "a\nb\n")
	for _, tt := range []struct {
		name, input string
		opts        Options
		resource    string
	}{
		{"operations", ".yoink one\n.yoink one", Options{MaxDepth: -1, MaxOperations: 1}, "operations"},
		{"root input", "abcd", Options{MaxDepth: -1, MaxInputBytes: 3}, "input bytes"},
		{"included input", ".yoink one", Options{MaxDepth: -1, MaxInputBytes: int64(len(".yoink one") + 3)}, "input bytes"},
		{"literal output", "abcd", Options{MaxDepth: -1, MaxOutputBytes: 3}, "output bytes"},
		{"separator output", "a\nb", Options{MaxDepth: -1, MaxOutputBytes: 2}, "output bytes"},
		{"branch output", ".yoink one\n.yoink one", Options{MaxDepth: -1, MaxOutputBytes: 6}, "output bytes"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := recursiveParse(t, dir, tt.input, tt.opts)
			var limit *LimitError
			if got != "" || !errors.Is(err, ErrLimitExceeded) || !errors.As(err, &limit) || limit.Resource != tt.resource {
				t.Fatalf("%q %v", got, err)
			}
		})
	}
	for _, tt := range []struct {
		input string
		opts  Options
		want  string
	}{
		{"abc", Options{MaxDepth: -1, MaxInputBytes: 3, MaxOutputBytes: 3}, "abc"},
		{".yoink one", Options{MaxDepth: -1, MaxInputBytes: int64(len(".yoink one") + 4), MaxOutputBytes: 3, MaxOperations: 1}, "a\nb"},
		{".yoink one\n.yoink one", Options{MaxDepth: -1, MaxInputBytes: int64(len(".yoink one\n.yoink one") + 8), MaxOutputBytes: 7, MaxOperations: 2}, "a\nb\na\nb"},
	} {
		got, err := recursiveParse(t, dir, tt.input, tt.opts)
		if err != nil || got != tt.want {
			t.Fatalf("exact limit %q %v want%q", got, err, tt.want)
		}
	}
	RegisterParserFunc("large-result", func(string, int, string) (string, error) { return strings.Repeat("x", 1024), nil })
	got, err := recursiveParse(t, dir, ".large-result", Options{MaxDepth: -1, MaxOutputBytes: 10})
	if got != "" || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("custom output limit: %q %v", got, err)
	}
}

func TestRecursiveOptionsValidation(t *testing.T) {
	for _, opts := range []Options{{MaxDepth: -2}, {MaxOperations: -1}, {MaxOperations: 1000001}, {MaxInputBytes: -1}, {MaxInputBytes: 1<<30 + 1}, {MaxOutputBytes: -1}, {MaxOutputBytes: 1<<30 + 1}, {Timeout: -1}, {Timeout: 24*time.Hour + 1}} {
		if got, err := recursiveParse(t, t.TempDir(), "plain", opts); got != "" || err == nil {
			t.Fatalf("invalid %+v: %q %v", opts, got, err)
		}
	}
	got, err := recursiveParse(t, t.TempDir(), "plain", Options{MaxDepth: -1, Timeout: time.Nanosecond})
	if got != "" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout %q %v", got, err)
	}
}

func TestRecursiveInputSharedReservations(t *testing.T) {
	const limit = 127
	b := newBudget(Options{MaxInputBytes: limit})
	var actual atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := budgetReader{context.Background(), countedReader{&actual}, b}
			_, err := io.ReadAll(r)
			if !errors.Is(err, ErrLimitExceeded) {
				t.Errorf("%v", err)
			}
		}()
	}
	wg.Wait()
	if actual.Load() != limit+1 {
		t.Fatalf("shared readers consumed %d bytes, want at most one shared probe beyond %d", actual.Load(), limit)
	}
}

type countedReader struct{ n *atomic.Int64 }

func (r countedReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.n.Add(int64(len(p)))
	return len(p), nil
}

func TestRecursiveWorkersJoined(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "one", ".recursive-legacy\n.recursive-aware\n.recursive-failure\n")
	entered := make(chan struct{})
	awareEntered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	RegisterParserFunc("recursive-legacy", func(string, int, string) (string, error) {
		close(entered)
		defer close(exited)
		<-release
		return "discarded", nil
	})
	RegisterParser("recursive-aware", contextTestParser{func(ctx context.Context) (string, error) {
		close(awareEntered)
		<-ctx.Done()
		close(cancelled)
		return "", ctx.Err()
	}})
	sentinel := errors.New("recursive failure")
	RegisterParserFunc("recursive-failure", func(string, int, string) (string, error) { <-entered; <-awareEntered; return "", sentinel })
	done := make(chan error, 1)
	go func() {
		out, err := ParseWithOptions(context.Background(), strings.NewReader(".yoink one"), filepath.Join(dir, "root"), Options{MaxDepth: -1})
		if out != "" {
			done <- fmt.Errorf("partial %q", out)
		} else {
			done <- err
		}
	}()
	<-cancelled
	select {
	case err := <-done:
		close(release)
		t.Fatalf("returned before callback completion: %v", err)
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("worker outlived parse")
	}
}

func TestRecursiveConcurrentOptions(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "one", ".yoink two\n")
	writeTempFile(t, dir, "two", "done\n")
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			depth := 1 + i%3
			want := "done"
			if depth == 1 {
				want = ".yoink two"
			}
			out, err := ParseWithOptions(context.Background(), strings.NewReader(".yoink one"), filepath.Join(dir, "root"), Options{MaxDepth: depth})
			if err != nil || out != want {
				t.Errorf("%q %v", out, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestRecursiveDeepChain(t *testing.T) {
	dir := t.TempDir()
	const count = 10000
	for i := 0; i < count; i++ {
		text := "leaf\n"
		if i+1 < count {
			text = fmt.Sprintf(".yoink %05d\n", i+1)
		}
		writeTempFile(t, dir, fmt.Sprintf("%05d", i), text)
	}
	got, err := recursiveParse(t, dir, ".yoink 00000", Options{MaxDepth: -1, MaxOperations: count, Timeout: 2 * time.Minute})
	if err != nil || got != "leaf" {
		t.Fatalf("10000-deep chain: %q %v", got, err)
	}
}

func TestRecursiveFanout(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "leaf", "x\n")
	input := strings.Repeat(".yoink leaf\n", 1000)
	got, err := recursiveParse(t, dir, input, Options{MaxDepth: -1, MaxOperations: 1000})
	if err != nil || got != strings.TrimSuffix(strings.Repeat("x\n", 1000), "\n") {
		t.Fatalf("fanout length%d %v", len(got), err)
	}
	got, err = recursiveParse(t, dir, input, Options{MaxDepth: -1, MaxOperations: 999})
	if got != "" || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("fanout limit: %q %v", got, err)
	}
}

func TestRecursiveLocalFailures(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "one", ".yoink absent\n")
	for _, input := range []string{".yoink one", ".yoink ."} {
		got, err := recursiveParse(t, dir, input, Options{MaxDepth: -1})
		if got != "" || err == nil {
			t.Fatalf("%q %v", got, err)
		}
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		p := writeTempFile(t, dir, "private", "secret\n")
		os.Chmod(p, 0)
		defer os.Chmod(p, 0600)
		got, err := recursiveParse(t, dir, ".yoink private", Options{MaxDepth: -1})
		if got != "" || !errors.Is(err, os.ErrPermission) {
			t.Fatalf("permission %q %v", got, err)
		}
	}
}

func TestRecursiveRemote(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/docs/one", http.StatusFound)
		case "/docs/one":
			io.WriteString(w, ".yoink ../shared/two /START/,/END/\n")
		case "/shared/two":
			io.WriteString(w, ".yoink excluded\nSTART OMIT\nremote\nEND OMIT\n")
		case "/cycle":
			io.WriteString(w, ".yoink ./cycle#alias\n")
		case "/error":
			io.WriteString(w, ".yoink missing\n")
		default:
			http.Error(w, "not included", http.StatusNotFound)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	got, err := recursiveParse(t, dir, ".yoink "+server.URL+"/redirect\n.yoink "+server.URL+"/redirect", Options{MaxDepth: -1})
	if err != nil || got != "remote\nremote" {
		t.Fatalf("redirect/relative/repeated: %q %v", got, err)
	}
	got, err = recursiveParse(t, dir, ".yoink "+server.URL+"/cycle", Options{MaxDepth: -1})
	if got != "" || !errors.Is(err, ErrCycle) {
		t.Fatalf("remote cycle %q %v", got, err)
	}
	got, err = recursiveParse(t, dir, ".yoink "+server.URL+"/error", Options{MaxDepth: -1})
	if got != "" || err == nil || !strings.Contains(err.Error(), "/error:1:") || !strings.Contains(err.Error(), "404") {
		t.Fatalf("nested HTTP error %q %v", got, err)
	}
	got, err = ParseWithOptions(context.Background(), strings.NewReader(".yoink ../shared/two /START/,/END/"), server.URL+"/docs/root", Options{MaxDepth: -1})
	if err != nil || got != "remote" {
		t.Fatalf("remote root %q %v", got, err)
	}
	got, err = recursiveParse(t, dir, ".yoink ftp://example.invalid/file", Options{MaxDepth: -1})
	if got != "" || err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("scheme %q %v", got, err)
	}
}

func TestRecursiveRemoteInputBudget(t *testing.T) {
	var sent atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 8; i++ {
			n, err := io.WriteString(w, strings.Repeat("x", 4096))
			sent.Add(int64(n))
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	input := ".yoink " + server.URL + "\n.yoink " + server.URL
	got, err := recursiveParse(t, dir, input, Options{MaxDepth: -1, MaxInputBytes: int64(len(input) + 100)})
	if got != "" || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("%q %v", got, err)
	}
	if len(sem.sem) != 0 {
		t.Fatal("remote permits retained after budget error")
	}
}

func TestRecursiveLocalURLCharacters(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a%zz.md", "a#fragment.md", "a?query.md"} {
		writeTempFile(t, dir, name, "local\n")
		got, err := recursiveParse(t, dir, ".yoink "+name, Options{MaxDepth: -1})
		if err != nil || got != "local" {
			t.Fatalf("local %s: %q %v", name, got, err)
		}
	}
}

func TestRecursiveLongLinesBudgetedBeforeCopy(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "long", strings.Repeat("x", 128*1024)+"\n")
	got, err := recursiveParse(t, dir, ".yoink long", Options{MaxDepth: -1, MaxOutputBytes: 16})
	if got != "" || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("%q %v", got, err)
	}
	got, err = recursiveParse(t, dir, ".yoink long", Options{MaxDepth: -1})
	if err != nil || len(got) != 128*1024 {
		t.Fatalf("length%d %v", len(got), err)
	}
	writeTempFile(t, dir, "long", ".yoink "+strings.Repeat("x", 128*1024)+"\n")
	got, err = recursiveParse(t, dir, ".yoink long", Options{MaxDepth: -1})
	if got != "" || err == nil || !strings.Contains(err.Error(), "directive line exceeds") {
		t.Fatalf("%q %v", got, err)
	}
}

func TestRecursivePoolCancellation(t *testing.T) {
	entered := make(chan struct{}, compositionWorkers)
	var active, maximum atomic.Int64
	RegisterParser("pool-cancellation", contextTestParser{func(ctx context.Context) (string, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		out, err := ParseWithOptions(ctx, strings.NewReader(strings.Repeat(".pool-cancellation\n", 100)), "pool", Options{MaxDepth: -1})
		if out != "" {
			done <- fmt.Errorf("partial %q", out)
		} else {
			done <- err
		}
	}()
	for i := 0; i < compositionWorkers; i++ {
		<-entered
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if active.Load() != 0 || maximum.Load() != compositionWorkers {
		t.Fatalf("pool lifecycle active%d maximum%d", active.Load(), maximum.Load())
	}
}

func TestRecursiveBlockedInputCompletion(t *testing.T) {
	r := &blockingReader{entered: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := ParseWithOptions(ctx, r, "reader", Options{MaxDepth: -1}); done <- err }()
	<-r.entered
	cancel()
	select {
	case err := <-done:
		close(r.release)
		t.Fatalf("abandoned input: %v", err)
	default:
	}
	close(r.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r.active.Load() {
		t.Fatal("reader outlived ParseWithOptions")
	}
}

func TestRecursiveRemoteDistinctEmptyPathSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dir/doc":
			io.WriteString(w, ".yoink ../doc\n")
		case "/doc":
			io.WriteString(w, "leaf\n")
		default:
			http.Error(w, "missing", http.StatusNotFound)
		}
	}))
	defer server.Close()
	got, err := ParseWithOptions(context.Background(), strings.NewReader(".yoink ../doc\n"), server.URL+"/dir//doc", Options{MaxDepth: -1})
	if err != nil || got != "leaf" {
		t.Fatalf("distinct HTTP paths falsely cycled: %q %v", got, err)
	}
}

func TestRecursiveRegisteredYoinkReplacement(t *testing.T) {
	parsersMu.RLock()
	original := parsers[".yoink"]
	parsersMu.RUnlock()
	defer RegisterParser("yoink", original)
	RegisterParserFunc("yoink", func(string, int, string) (string, error) { return ".yoink absent", nil })
	got, err := ParseWithOptions(context.Background(), strings.NewReader(".yoink ignored"), "custom", Options{MaxDepth: -1})
	if err != nil || got != ".yoink absent" {
		t.Fatalf("custom yoink output recursively interpreted: %q %v", got, err)
	}
}

func TestRecursiveRegistryConcurrentRegistration(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "one", ".recursive-registration\n")
	RegisterParserFunc("recursive-registration", func(string, int, string) (string, error) { return "ok", nil })
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				RegisterParserFunc("recursive-registration", func(string, int, string) (string, error) { return "ok", nil })
				got, err := ParseWithOptions(context.Background(), strings.NewReader(".yoink one"), filepath.Join(dir, "root"), Options{MaxDepth: -1})
				if err != nil || got != "ok" {
					t.Errorf("%q %v", got, err)
				}
			}
		}()
	}
	wg.Wait()
}

func TestRecursiveSymlinkResolutionBases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires platform privileges")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "base", "one"), 0755)
	writeTempFile(t, dir, "doc", "leaf\n")
	writeTempFile(t, dir, "base/doc", ".yoink ../doc\n")
	alias := filepath.Join(dir, "base", "one", "alias")
	if err := os.Symlink("../doc", alias); err != nil {
		t.Fatal(err)
	}
	// Root Reader uses its supplied name; the loaded file then uses the actual
	// target directory, a distinct expansion context despite equal source bytes.
	got, err := ParseWithOptions(context.Background(), strings.NewReader(".yoink ../doc\n"), alias, Options{MaxDepth: -1})
	if err != nil || got != "leaf" {
		t.Fatalf("symlink alias false cycle: %q %v", got, err)
	}
	// Loaded symlink targets resolve children from their canonical directory.
	writeTempFile(t, dir, "base/doc", ".yoink child\n")
	writeTempFile(t, dir, "base/child", "target child\n")
	writeTempFile(t, dir, "base/one/child", "alias child\n")
	got, err = recursiveParse(t, dir, ".yoink base/one/alias", Options{MaxDepth: -1})
	if err != nil || got != "target child" {
		t.Fatalf("loaded target directory: %q %v", got, err)
	}
	// Directory symlinks must not evade cycle detection by growing lexical paths.
	if err := os.Symlink(".", filepath.Join(dir, "again")); err != nil {
		t.Fatal(err)
	}
	writeTempFile(t, dir, "cycle", ".yoink again/cycle\n")
	got, err = recursiveParse(t, dir, ".yoink cycle", Options{MaxDepth: -1})
	if got != "" || !errors.Is(err, ErrCycle) {
		t.Fatalf("directory alias cycle: %q %v", got, err)
	}
}

func TestRecursiveRootOMITIdentity(t *testing.T) {
	dir := t.TempDir()
	text := ".yoink aOMIT\n"
	name := writeTempFile(t, dir, "aOMIT", text)
	// The root executes its directive; the included copy removes it via OMIT and
	// terminates. Equal bytes are not equal expansion states across these modes.
	got, err := ParseWithOptions(context.Background(), strings.NewReader(text), name, Options{MaxDepth: -1, MaxOperations: 1})
	if err != nil || got != "" {
		t.Fatalf("root/include filtering falsely cycled: %q %v", got, err)
	}
	flat, err := Parse(context.Background(), strings.NewReader(text), name)
	if err != nil || got != flat {
		t.Fatalf("legacy OMIT behavior differs: flat%q err%v recursive%q", flat, err, got)
	}
}
