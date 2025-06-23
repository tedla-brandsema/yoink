package zipline

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"text/template"
)

type file struct {
	Name    string
	Content string
	Path    string
}

var (
	fileIdx = make(map[string]file)
	files   = []file{
		{
			Name: "local.txt",
			Content: `
1 line 
// Full snippet1 
.zip snippet1.txt
2 line 
3 line 
// Partial snippet1
.zip snippet1.txt /START OMIT/,/END OMIT/
4 line 
// Named 'outer' snippet2 
.zip snippet2.txt /START outer OMIT/,/END outer OMIT/
5 line 
// Named 'inner' snippet2 
.zip snippet2.txt /START inner OMIT/,/END inner OMIT/
`,
		}, {
			Name: "remote.txt",
			Content: `
1 line 
// Full snippet1 
.zip {{.BaseURL}}/snippet1.txt
2 line 
3 line 
// Partial snippet1
.zip {{.BaseURL}}/snippet1.txt /START OMIT/,/END OMIT/
4 line 
// Named 'outer' snippet2 
.zip {{.BaseURL}}/snippet2.txt /START outer OMIT/,/END outer OMIT/
5 line 
// Named 'inner' snippet2 
.zip {{.BaseURL}}/snippet2.txt /START inner OMIT/,/END inner OMIT/
`,
		}, {

			Name: "snippet1.txt",
			Content: `
1.1 Included
/START OMIT
1.2 Included
/END OMIT
1.3 Included
`,
		}, {
			Name: "snippet2.txt",
			Content: `
/START outer OMIT
2.1 Included
/START inner OMIT
2.2 Included
/END inner OMIT
2.3 Included
2.4 Included OMIT
/END outer OMIT
`,
		},
	}
)

func writeTempFiles(t *testing.T, files []file) {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		f.Path = filepath.Join(dir, f.Name)
		if err := os.WriteFile(f.Path, []byte(f.Content), 0644); err != nil {
			t.Fatalf("failed to write temp file %s: %v", f.Name, err)
		}
		fileIdx[f.Name] = f
	}
}

func updateTmpFile(t *testing.T, path string, tmpl string, data any) {
	t.Helper()

	tpl, err := template.New("remote").Parse(tmpl)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to update file %s: %v", path, err)
	}
}

func TestParse_Local(t *testing.T) {
	writeTempFiles(t, files)

	root := fileIdx["local.txt"]

	if root.Path != "" {
		src, err := os.Open(root.Path)
		if err != nil {
			t.Fatal(err)
		}
		defer src.Close()

		parsed, err := Parse(src, root.Path)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println(parsed)
	}
}

func TestParse_Remote(t *testing.T) {
	writeTempFiles(t, files)

	root := fileIdx["remote.txt"]

	dir := filepath.Dir(root.Path)
	fs := http.FileServer(http.Dir(dir))
	server := httptest.NewServer(fs)
	defer server.Close()

	updateTmpFile(t, root.Path, root.Content, map[string]string{
		"BaseURL": server.URL,
	})

	if root.Path != "" {
		src, err := os.Open(root.Path)
		if err != nil {
			t.Fatal(err)
		}
		defer src.Close()

		parsed, err := Parse(src, root.Path)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println(parsed)
	}
}
