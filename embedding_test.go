package yoink_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tedla-brandsema/yoink"
)

// Run the ordinary public API in an isolated host so a regression to log.Fatal
// or a worker panic is reported as a test failure rather than killing the suite.
func TestEmbeddingRecovery(t *testing.T) {
	if os.Getenv("YOINK_EMBEDDING_HELPER") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestEmbeddingRecovery$", "-test.v")
		cmd.Env = append(os.Environ(), "YOINK_EMBEDDING_HELPER=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("embedding host exited unexpectedly: %v\n%s", err, output)
		}
		if !strings.Contains(string(output), "host continued serving") {
			t.Fatalf("host did not finish: %s", output)
		}
		return
	}
	dir := t.TempDir()
	name := filepath.Join(dir, "document")
	if err := os.WriteFile(filepath.Join(dir, "snippet"), []byte("valid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	valid := func() {
		t.Helper()
		out, err := yoink.Parse(context.Background(), strings.NewReader(".yoink snippet"), name)
		if err != nil || out != "valid" {
			t.Fatalf("valid request: %q %v", out, err)
		}
	}
	valid()
	for _, cmd := range []string{".yoink snippet /missing/", `.yoink snippet /trailing\`, ".yoink snippet /[/", ".yoink snippet $-#999"} {
		out, err := yoink.Parse(context.Background(), strings.NewReader(cmd), name)
		if err == nil || out != "" {
			t.Fatalf("rejected request: %q %v", out, err)
		}
		valid()
	}
	entered := make(chan struct{})
	ended := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(ended)
		io.WriteString(w, "incomplete\n")
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		out, err := yoink.Parse(ctx, strings.NewReader(".yoink "+server.URL), name)
		if out != "" {
			done <- errors.New("cancelled request returned partial output")
		} else {
			done <- err
		}
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-ended
	valid()
	// A context-aware extension releases its own resource before Parse returns.
	started := make(chan struct{})
	var active atomic.Int64
	yoink.RegisterParser("embedding-lifetime", resourceParser{started, &active})
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	go func() { _, err := yoink.Parse(ctx, strings.NewReader(".embedding-lifetime"), name); done <- err }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if active.Load() != 0 {
		t.Fatal("worker resource outlived Parse")
	}
	valid()
	t.Log("host continued serving")
}

type resourceParser struct {
	started chan struct{}
	active  *atomic.Int64
}

func (p resourceParser) Parse(string, int, string) (string, error) {
	return "", errors.New("expected ParseContext")
}
func (p resourceParser) ParseContext(ctx context.Context, _ string, _ int, _ string) (string, error) {
	p.active.Add(1)
	defer p.active.Add(-1)
	close(p.started)
	<-ctx.Done()
	return "", ctx.Err()
}
