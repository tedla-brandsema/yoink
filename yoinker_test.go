package yoink

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write temp file %s: %v", name, err)
	}
	return path
}

func TestParseYoink(t *testing.T) {
	tmpDir := t.TempDir()
	code := `
package main

import "fmt"

func main() {
	fmt.Println("hello") // OMIT
	fmt.Println("world")
}`
	codeFile := writeTempFile(t, tmpDir, "code.go", code)

	tests := []struct {
		name    string
		cmd     string
		wantSub string
		wantErr bool
	}{
		{
			name:    "include full file",
			cmd:     ".yoink code.go",
			wantSub: `package main`,
		},
		//{
		//	name:    "regex include",
		//	cmd:     ".yoink code.go /main/",
		//	wantSub: `func main() {`,
		//},
		//{
		//	name:    "line offset skip OMIT",
		//	cmd:     ".yoink code.go /main/+2",
		//	wantSub: `fmt.Println("world")`,
		//},
		{
			name:    "invalid command syntax",
			cmd:     ".yoink",
			wantErr: true,
		},
		{
			name:    "nonexistent file",
			cmd:     ".yoink nofile.go",
			wantErr: true,
		},
		{
			name:    "bad regex",
			cmd:     `.yoink code.go /[unclosed/`,
			wantErr: true,
		},
		{
			name:    "reverse search unsupported",
			cmd:     `.yoink code.go -/main/`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceFile := filepath.Join(tmpDir, "doc.txt") // fake doc location
			cmd := strings.Replace(tt.cmd, "code.go", filepath.Base(codeFile), 1)
			out, err := yoinkParser(sourceFile, 10, cmd)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, tt.wantSub) {
				t.Errorf("expected output to contain %q but got:\n%s", tt.wantSub, out)
			}
		})
	}
}
