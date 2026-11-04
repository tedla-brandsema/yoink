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
	"time"

	"github.com/tedla-brandsema/yoink"
)

func TestRecursiveIntegrationFixture(t *testing.T) {
	name := filepath.Join("testdata", "recursive", "grand-vision.md")
	expected, err := os.ReadFile(filepath.Join("testdata", "recursive", "expected.md"))
	if err != nil {
		t.Fatal(err)
	}
	compose := func(depth int) string {
		t.Helper()
		input, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		got, err := yoink.ParseWithOptions(t.Context(), input, name, yoink.Options{MaxDepth: depth})
		if err != nil {
			t.Fatalf("depth %d: %v", depth, err)
		}
		return got
	}
	flat := compose(1)
	if !strings.Contains(flat, ".yoink sections/overview.md") || strings.Contains(flat, "Prepare the reusable unit.") {
		t.Fatalf("depth 1 did not stop at the first inclusion boundary: %q", flat)
	}
	two := compose(2)
	if strings.Contains(two, ".yoink sections/overview.md") || !strings.Contains(two, ".yoink ../knowledge/economy.md /BEGIN DETAIL OMIT/,/END DETAIL OMIT/") || strings.Contains(two, "Selected supporting detail.") {
		t.Fatalf("depth 2 did not stop at the second inclusion boundary: %q", two)
	}
	for _, depth := range []int{3, -1} {
		got := compose(depth)
		if got+"\n" != string(expected) {
			t.Fatalf("depth %d output mismatch\ngot:  %q\nwant: %q", depth, got, string(expected))
		}
		const body = "<!-- BEGIN STAGE1-SUBSTANCE -->\n## Stage 1\n\nPrepare the reusable unit.\n\nSelected supporting detail.\n\nDeliver the reusable unit.\n\n<!-- END STAGE1-SUBSTANCE -->"
		if strings.Count(got, body) != 1 {
			t.Fatalf("depth %d: complete Stage 1 body does not occur exactly once", depth)
		}
		if strings.Contains(got, ".yoink ") {
			t.Fatalf("depth %d left an unintended directive", depth)
		}
		opportunity, stage, strategy := strings.Index(got, "A shared opportunity."), strings.Index(got, "## Stage 1"), strings.Index(got, "A deliberate demand strategy.")
		if opportunity < 0 || stage <= opportunity || strategy <= stage {
			t.Fatalf("depth %d changed composition order", depth)
		}
	}
}

// Keep the host process boundary explicit: an internal panic or fatal exit must
// fail this subprocess rather than terminate the test runner.
func TestRecursiveEmbeddingRecovery(t *testing.T) {
	if os.Getenv("YOINK_RECURSIVE_EMBEDDING_HELPER") != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecursiveEmbeddingRecovery$", "-test.v")
		cmd.Env = append(os.Environ(), "YOINK_RECURSIVE_EMBEDDING_HELPER=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("recursive embedding host exited unexpectedly: %v\n%s", err, output)
		}
		if !strings.Contains(string(output), "recursive host continued serving") {
			t.Fatalf("recursive host did not complete: %s", output)
		}
		return
	}
	dir := t.TempDir()
	name := filepath.Join(dir, "root.md")
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("leaf.md", "valid\n")
	write("middle.md", ".yoink leaf.md\n")
	opts := yoink.Options{MaxDepth: -1}
	valid := func() {
		t.Helper()
		got, err := yoink.ParseWithOptions(t.Context(), strings.NewReader(".yoink middle.md"), name, opts)
		if err != nil || got != "valid" {
			t.Fatalf("subsequent valid parse: %q %v", got, err)
		}
	}
	valid()
	write("cycle.md", ".yoink cycle.md\n")
	got, err := yoink.ParseWithOptions(t.Context(), strings.NewReader(".yoink cycle.md"), name, opts)
	if got != "" || err == nil || !strings.Contains(strings.ToLower(err.Error()), "cycle") {
		t.Fatalf("cycle did not return an ordinary error: %q %v", got, err)
	}
	valid()
	write("broken.md", "\nBEGIN OMIT\n.yoink leaf.md /trailing\\\nEND OMIT\n")
	got, err = yoink.ParseWithOptions(t.Context(), strings.NewReader(".yoink broken.md /BEGIN OMIT/,/END OMIT/"), name, opts)
	if got != "" || err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "broken.md")+":3:") {
		t.Fatalf("nested malformed source location: %q %v", got, err)
	}
	valid()
	entered := make(chan struct{})
	ended := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(ended)
		io.WriteString(w, "partial remote body\n")
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	write("remote.md", ".yoink "+server.URL+"\n")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		got, err := yoink.ParseWithOptions(ctx, strings.NewReader(".yoink remote.md"), name, opts)
		if got != "" {
			done <- errors.New("cancelled nested parse returned partial output")
		} else {
			done <- err
		}
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("nested HTTP cancellation: %v", err)
	}
	<-ended
	valid()
	// A custom callback's resource is released before the parse call completes.
	started := make(chan struct{})
	var active atomic.Int64
	yoink.RegisterParser("recursive-integration-lifetime", recursiveFixtureParser{started, &active})
	write("callback.md", ".recursive-integration-lifetime\n")
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	go func() {
		got, err := yoink.ParseWithOptions(ctx, strings.NewReader(".yoink callback.md"), name, opts)
		if got != "" {
			done <- errors.New("cancelled callback parse returned partial output")
		} else {
			done <- err
		}
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if active.Load() != 0 {
		t.Fatal("custom parser resource outlived public parse completion")
	}
	valid()
	t.Log("recursive host continued serving")
}

type recursiveFixtureParser struct {
	started chan struct{}
	active  *atomic.Int64
}

func (p recursiveFixtureParser) Parse(string, int, string) (string, error) {
	return "", errors.New("expected optional context-aware callback")
}
func (p recursiveFixtureParser) ParseContext(ctx context.Context, _ string, _ int, _ string) (string, error) {
	p.active.Add(1)
	defer p.active.Add(-1)
	close(p.started)
	<-ctx.Done()
	return "", ctx.Err()
}
