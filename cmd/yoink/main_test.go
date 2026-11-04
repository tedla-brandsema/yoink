package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCLI(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "yoink")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}

	docs := filepath.Join(dir, "documents")
	if err := os.Mkdir(docs, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(docs, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("snippet.txt", "\r\nBEGIN OMIT\r\none \r\ntwo\r\nEND OMIT\r\n\r\n")
	write("second.txt", "three\n")
	const input = "before\r\n.yoink snippet.txt /BEGIN/,/END/\r\nbetween\r\n.yoink second.txt\r\nafter\r\n"
	const want = "before\none \ntwo\nbetween\nthree\nafter\n"
	inputPath := write("input.txt", input)

	run := func(t *testing.T, cwd, stdin string, args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = cwd
		cmd.Stdin = strings.NewReader(stdin)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("CLI did not complete: %v", ctx.Err())
		}
		return stdout.String(), stderr.String(), err
	}
	for _, fileInput := range []bool{true, false} {
		for _, fileOutput := range []bool{false, true} {
			name := "stdin-to-stdout"
			if fileInput {
				name = "file-to-stdout"
			}
			if fileOutput {
				name = strings.TrimSuffix(name, "stdout") + "file"
			}
			t.Run(name, func(t *testing.T) {
				var args []string
				outputPath := filepath.Join(dir, name+".txt")
				if fileOutput {
					args = append(args, "-o", outputPath)
				}
				cwd, stdin := docs, input
				if fileInput {
					// Resolve includes against the input file, even from another cwd.
					cwd, stdin = dir, ""
					args = append(args, inputPath)
				}
				stdout, stderr, err := run(t, cwd, stdin, args...)
				if err != nil || stderr != "" {
					t.Fatalf("CLI: %v, stderr=%q", err, stderr)
				}
				got := stdout
				if fileOutput {
					if stdout != "" {
						t.Fatalf("output-file invocation wrote stdout: %q", stdout)
					}
					data, err := os.ReadFile(outputPath)
					if err != nil {
						t.Fatal(err)
					}
					got = string(data)
				}
				if got != want {
					t.Fatalf("output=%q; want %q", got, want)
				}
			})
		}
	}

	for _, tc := range []struct{ name, directive string }{
		{"missing-file", ".yoink absent.txt"},
		{"unmatched-address", ".yoink snippet.txt /absent/"},
		{"invalid-regexp", ".yoink snippet.txt /[/"},
		{"trailing-escape", `.yoink snippet.txt /broken\`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := write(tc.name+".txt", "before\n"+tc.directive+"\nafter\n")
			for _, toFile := range []bool{false, true} {
				var args []string
				outputPath := write(tc.name+"-output.txt", "keep existing output\n")
				if toFile {
					args = append(args, "-o", outputPath)
				}
				args = append(args, path)
				stdout, stderr, err := run(t, dir, "", args...)
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 {
					t.Fatalf("failure should exit 1: %v, stderr=%q", err, stderr)
				}
				if stdout != "" || !strings.Contains(stderr, "Parsing error:") || !strings.Contains(stderr, path+":2:") {
					t.Fatalf("failed composition: stdout=%q stderr=%q", stdout, stderr)
				}
				data, err := os.ReadFile(outputPath)
				if err != nil || string(data) != "keep existing output\n" {
					t.Fatalf("parse failure changed existing output: %q, %v", data, err)
				}
			}
		})
	}

	t.Run("input-open-error", func(t *testing.T) {
		stdout, stderr, err := run(t, dir, "", filepath.Join(dir, "absent.txt"))
		if err == nil || stdout != "" || !strings.Contains(stderr, "Error opening input file:") {
			t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
		}
	})
	t.Run("output-create-error", func(t *testing.T) {
		stdout, stderr, err := run(t, dir, "valid\n", "-o", docs)
		if err == nil || stdout != "" || !strings.Contains(stderr, "Error creating output file:") {
			t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
		}
	})
	if runtime.GOOS == "linux" {
		t.Run("output-write-error", func(t *testing.T) {
			stdout, stderr, err := run(t, dir, "valid\n", "-o", "/dev/full")
			if err == nil || stdout != "" || !strings.Contains(stderr, "Error writing output:") {
				t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
			}
		})
	}
}
