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
	"regexp/syntax"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSafetyErrors(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "snippet", "alpha\nbeta\n")
	name := filepath.Join(dir, "doc")
	for _, tt := range []struct{ command, message string }{
		{".yoink absent", "absent"}, {".yoink snippet /absent/", "no match"},
		{".yoink snippet /[/", "error parsing regexp"}, {`.yoink snippet /alpha\`, "trailing escape"},
		{".yoink", "syntax error"}, {".yoink snippet $-#999", "out of range"},
	} {
		t.Run(tt.command, func(t *testing.T) {
			out, err := Parse(context.Background(), strings.NewReader("header\n\n// comment\n"+tt.command), name)
			if err == nil || out != "" || !strings.Contains(err.Error(), name+":4:") || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("output=%q error=%v", out, err)
			}
			if strings.Contains(tt.command, "absent") && tt.command == ".yoink absent" && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("lost filesystem error: %v", err)
			}
			if strings.Contains(tt.command, "/[/") {
				var re *syntax.Error
				if !errors.As(err, &re) {
					t.Fatalf("lost regexp type: %v", err)
				}
			}
		})
	}
	sentinel := errors.New("custom failure")
	RegisterParserFunc("safety-error", func(string, int, string) (string, error) { return "ignored", sentinel })
	out, err := Parse(context.Background(), strings.NewReader(".safety-error"), "custom")
	if out != "" || !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "custom:1:") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCompositionExact(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "snippet", "\r\nSTART OMIT\r\n  alpha  \r\nbeta\r\nEND OMIT\r\n\r\n")
	writeTempFile(t, dir, "flat", ".yoink absent\n")
	name := filepath.Join(dir, "doc")
	input := "header\r\n\r\n// comment\r\n.yoink snippet /START/,/END/\r\n.yoink flat\r\n.unknown keep\r\nfooter\r\n"
	want := "header\n\n// comment\n  alpha  \nbeta\n.yoink absent\n.unknown keep\nfooter"
	got, err := Parse(context.Background(), strings.NewReader(input), name)
	if err != nil || got != want {
		t.Fatalf("got %q (%v), want %q", got, err, want)
	}
	for _, addr := range []string{"/ph/", "#16,#17"} {
		got, err = Parse(context.Background(), strings.NewReader(".yoink snippet "+addr), name)
		if err != nil || got != "  alpha  " {
			t.Fatalf("rounding %q: %q %v", addr, got, err)
		}
	}
	long := strings.Repeat("x", 128*1024)
	writeTempFile(t, dir, "long", long+"\nend\n")
	got, err = Parse(context.Background(), strings.NewReader(".yoink long"), name)
	if err != nil || got != long+"\nend" {
		t.Fatalf("long include truncated: length=%d error=%v", len(got), err)
	}
}

type contextTestParser struct {
	run func(context.Context) (string, error)
}

func (p contextTestParser) Parse(string, int, string) (string, error) {
	return "", errors.New("legacy method must not run")
}
func (p contextTestParser) ParseContext(ctx context.Context, _ string, _ int, _ string) (string, error) {
	return p.run(ctx)
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := Parse(ctx, panicReader{}, "cancelled")
	if out != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("%q %v", out, err)
	}
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			entered := make(chan struct{})
			exited := make(chan struct{})
			RegisterParser("cancel-active", contextTestParser{func(ctx context.Context) (string, error) {
				close(entered)
				defer close(exited)
				<-ctx.Done()
				return "", ctx.Err()
			}})
			ctx, cancel := context.WithCancel(context.Background())
			want := context.Canceled
			var timerCancel context.CancelFunc
			if deadline {
				ctx, timerCancel = context.WithTimeout(ctx, time.Second)
				defer timerCancel()
				want = context.DeadlineExceeded
			}
			defer cancel()
			done := make(chan error, 1)
			go func() {
				out, err := Parse(ctx, strings.NewReader(".cancel-active"), "active")
				if out != "" {
					done <- fmt.Errorf("partial output %q", out)
				} else {
					done <- err
				}
			}()
			<-entered
			if !deadline {
				cancel()
			}
			if err := <-done; !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
			select {
			case <-exited:
			default:
				t.Fatal("callback outlived Parse")
			}
		})
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("cancelled Parse read input") }

func TestWorkersJoinedOnError(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	cancelled := make(chan struct{})
	RegisterParserFunc("join-legacy", func(string, int, string) (string, error) {
		close(started)
		defer close(exited)
		<-release
		return "legacy", nil
	})
	sentinel := errors.New("join error")
	// Ensure all three callbacks have started before permitting the failure.
	awareStarted := make(chan struct{})
	RegisterParser("join-aware", contextTestParser{func(ctx context.Context) (string, error) {
		close(awareStarted)
		<-ctx.Done()
		close(cancelled)
		return "", ctx.Err()
	}})
	RegisterParserFunc("join-error", func(string, int, string) (string, error) { <-started; <-awareStarted; return "", sentinel })
	done := make(chan error, 1)
	go func() {
		out, err := Parse(context.Background(), strings.NewReader(".join-legacy\n.join-aware\n.join-error"), "join")
		if out != "" {
			done <- fmt.Errorf("partial output %q", out)
		} else {
			done <- err
		}
	}()
	<-cancelled
	select {
	case err := <-done:
		close(release)
		t.Fatalf("returned with live legacy worker: %v", err)
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, sentinel) {
		t.Fatalf("lost original error: %v", err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("legacy worker outlived Parse")
	}
}

func TestSourceOrder(t *testing.T) {
	firstStarted := make(chan struct{})
	secondDone := make(chan struct{})
	RegisterParserFunc("ordered", func(_ string, _ int, cmd string) (string, error) {
		if strings.HasSuffix(cmd, "first") {
			close(firstStarted)
			<-secondDone
			return "first", nil
		}
		<-firstStarted
		close(secondDone)
		return "second", nil
	})
	got, err := Parse(context.Background(), strings.NewReader(".ordered first\n.ordered second"), "order")
	if err != nil || got != "first\nsecond" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestConcurrentRegistryAndParse(t *testing.T) {
	var wg sync.WaitGroup
	var calls atomic.Int64
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent-%d", i)
			for j := 0; j < 15; j++ {
				RegisterParserFunc(name, func(string, int, string) (string, error) { calls.Add(1); return "ok", nil })
				RegisterParserFunc("replaced", func(string, int, string) (string, error) { return "shared", nil })
				got, err := Parse(context.Background(), strings.NewReader("."+name+"\n.replaced"), "concurrent")
				if err != nil || got != "ok\nshared" {
					t.Errorf("%q %v", got, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if calls.Load() != 24*15 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestHTTPInclusion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.Error(w, "not included", http.StatusNotFound)
			return
		}
		io.WriteString(w, "START OMIT\nremote\nEND OMIT\n")
	}))
	defer server.Close()
	got, err := Parse(context.Background(), strings.NewReader(".yoink "+server.URL+" /START/,/END/"), "remote")
	if err != nil || got != "remote" {
		t.Fatalf("%q %v", got, err)
	}
	got, err = Parse(context.Background(), strings.NewReader(".yoink "+server.URL+"/missing"), "remote")
	if got != "" || err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "remote:1:") {
		t.Fatalf("%q %v", got, err)
	}
}

func TestHTTPCancellation(t *testing.T) {
	for _, body := range []bool{false, true} {
		t.Run(fmt.Sprint(body), func(t *testing.T) {
			entered := make(chan struct{})
			serverDone := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(serverDone)
				if body {
					w.WriteHeader(http.StatusOK)
					io.WriteString(w, "partial\n")
					w.(http.Flusher).Flush()
				}
				close(entered)
				<-r.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				out, err := Parse(ctx, strings.NewReader(".yoink "+server.URL), "http-cancel")
				if out != "" {
					done <- fmt.Errorf("partial output %q", out)
				} else {
					done <- err
				}
			}()
			<-entered
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			<-serverDone
		})
	}
}

func TestSemaphoreCancellation(t *testing.T) {
	s := NewSemaphore(1)
	s.Acquire()
	defer s.Release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.acquireContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(s.sem) != 1 {
		t.Fatal("cancelled acquire consumed capacity")
	}
}

func TestInputReadError(t *testing.T) {
	sentinel := errors.New("read failed")
	out, err := Parse(context.Background(), errorReader{sentinel}, "input")
	if out != "" || !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "input") {
		t.Fatalf("%q %v", out, err)
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

// Signal the precise cancellation-select boundary without sleeping.
type observedContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *observedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestThrottleCancellation(t *testing.T) {
	oldSem, oldThrottle := sem, throttle
	sem = nil
	throttle = time.Hour
	defer func() { sem, throttle = oldSem, oldThrottle }()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &observedContext{Context: parent, entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := yoinkParserContext(ctx, "throttle", 1, ".yoink http://127.0.0.1:1"); done <- err }()
	<-ctx.entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestHTTPFailureCancelsSiblings(t *testing.T) {
	started := make(chan struct{})
	exited := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			<-started
			http.Error(w, "failure", http.StatusInternalServerError)
			return
		}
		defer close(exited)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	out, err := Parse(context.Background(), strings.NewReader(".yoink "+server.URL+"/slow\n.yoink "+server.URL+"/fail"), "siblings")
	if out != "" || err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("%q %v", out, err)
	}
	<-exited
	if len(sem.sem) != 0 {
		t.Fatalf("HTTP permit leaked: %d", len(sem.sem))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed *atomic.Bool
}

func (b trackedBody) Close() error { b.closed.Store(true); return nil }

func TestHTTPBodyClosed(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	for _, status := range []int{200, 503} {
		var closed atomic.Bool
		http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d test", status), Body: trackedBody{strings.NewReader("body\n"), &closed}, Header: make(http.Header)}, nil
		})}
		out, err := Parse(context.Background(), strings.NewReader(".yoink https://example.invalid"), "closed")
		if status == 200 && (out != "body" || err != nil) {
			t.Fatalf("%q %v", out, err)
		}
		if status == 503 && (out != "" || err == nil) {
			t.Fatalf("%q %v", out, err)
		}
		if !closed.Load() {
			t.Fatalf("status %d body not closed before Parse returned", status)
		}
	}
}

func TestRejectNonRegularInclude(t *testing.T) {
	dir := t.TempDir()
	out, err := Parse(context.Background(), strings.NewReader(".yoink ."), filepath.Join(dir, "doc"))
	if out != "" || err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("%q %v", out, err)
	}
}

// A blocked read is released by its owner, rather than abandoned in a goroutine.
type blockingReader struct {
	entered chan struct{}
	release chan struct{}
	active  atomic.Bool
}

func (r *blockingReader) Read([]byte) (int, error) {
	r.active.Store(true)
	defer r.active.Store(false)
	close(r.entered)
	<-r.release
	return 0, io.EOF
}
func TestInputCancellationCompletion(t *testing.T) {
	r := &blockingReader{entered: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		out, err := Parse(ctx, r, "blocked-reader")
		if out != "" {
			done <- fmt.Errorf("partial output %q", out)
		} else {
			done <- err
		}
	}()
	<-r.entered
	cancel()
	select {
	case err := <-done:
		close(r.release)
		t.Fatalf("Parse abandoned its reader: %v", err)
	default:
	}
	close(r.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r.active.Load() {
		t.Fatal("reader outlived Parse")
	}
}

func TestLegacyCancellationCompletion(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	RegisterParserFunc("legacy-parent-cancel", func(string, int, string) (string, error) {
		close(entered)
		defer close(exited)
		<-release
		return "discarded", nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		out, err := Parse(ctx, strings.NewReader(".legacy-parent-cancel"), "legacy")
		if out != "" {
			done <- fmt.Errorf("partial output %q", out)
		} else {
			done <- err
		}
	}()
	<-entered
	cancel()
	select {
	case err := <-done:
		close(release)
		t.Fatalf("Parse abandoned its legacy callback: %v", err)
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("legacy callback outlived Parse")
	}
}
