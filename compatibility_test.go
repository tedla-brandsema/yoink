package yoink

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These expected bytes reproduce the existing v0.1.0 TestCompositionExact and
// CLI fixture conventions. The protected qualification also runs the archived
// v0.1.0 test suite and compares old/new executable output and errors directly.
func TestFlatCompatibility(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "snippet", "\r\nSTART OMIT\r\n  alpha  \r\nbeta\r\nEND OMIT\r\n\r\n")
	writeTempFile(t, dir, "flat", ".yoink absent\n")
	RegisterParserFunc("flat-compat", func(string, int, string) (string, error) { return ".yoink absent", nil })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "\r\nSTART OMIT\r\n  alpha  \r\nbeta\r\nEND OMIT\r\n\r\n")
	}))
	defer server.Close()
	apis := []struct {
		name  string
		parse func(context.Context, io.Reader, string) (string, error)
	}{
		{"Parse", Parse}, {"zero", func(ctx context.Context, r io.Reader, name string) (string, error) {
			return ParseWithOptions(ctx, r, name, Options{})
		}}, {"one", func(ctx context.Context, r io.Reader, name string) (string, error) {
			return ParseWithOptions(ctx, r, name, Options{MaxDepth: 1})
		}},
	}
	for _, api := range apis {
		t.Run(api.name, func(t *testing.T) {
			for _, target := range []string{"snippet", server.URL} {
				input := "header\r\n\r\n// comment\r\n.yoink " + target + " /START/,/END/\r\n.yoink flat\r\n.flat-compat\r\n.unknown keep\r\nfooter\r\n"
				want := "header\n\n// comment\n  alpha  \nbeta\n.yoink absent\n.yoink absent\n.unknown keep\nfooter"
				got, err := api.parse(context.Background(), strings.NewReader(input), filepath.Join(dir, "doc"))
				if err != nil || got != want {
					t.Fatalf("%q %v want%q", got, err, want)
				}
			}
			got, err := api.parse(context.Background(), strings.NewReader("before\n.yoink absent"), filepath.Join(dir, "doc"))
			if got != "" || !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "doc:2:") {
				t.Fatalf("%q %v", got, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			got, err = api.parse(ctx, strings.NewReader("valid"), "cancelled")
			if got != "" || !errors.Is(err, context.Canceled) {
				t.Fatalf("%q %v", got, err)
			}
		})
	}
}
