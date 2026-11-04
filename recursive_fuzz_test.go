package yoink

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func FuzzRecursive(f *testing.F) {
	for _, seed := range [][]byte{{0, 1, 2, 3}, {1, 1, 1}, {2, 0, 2}, {3, 255, 4, 7}, {255, 255}, {5, 6, 7, 8}, {0}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, graph []byte) {
		if len(graph) > 128 {
			t.Skip()
		}
		dir := t.TempDir()
		for i := 0; i < 4; i++ {
			var b strings.Builder
			b.WriteString("BEGIN OMIT\n")
			for j := i; j < len(graph); j += 4 {
				v := graph[j]
				switch v % 6 {
				case 0:
					fmt.Fprintf(&b, ".yoink ./f%d\n", v%4)
				case 1:
					fmt.Fprintf(&b, ".yoink sub/../f%d /BEGIN/,/END/\n", v%4)
				case 2:
					b.WriteString(".yoink absent\n")
				case 3:
					fmt.Fprintf(&b, ".yoink f%d /broken\\\n", v%4)
				case 4:
					b.WriteString("text OMIT\n")
				case 5:
					b.WriteString("value\n")
				}
			}
			b.WriteString("END OMIT\n")
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d", i)), []byte(b.String()), 0600); err != nil {
				t.Fatal(err)
			}
		}
		os.Mkdir(filepath.Join(dir, "sub"), 0700)
		depth := -1
		if len(graph) > 0 && graph[0]%2 == 0 {
			depth = 1 + int(graph[0]%5)
		}
		out, err := ParseWithOptions(context.Background(), strings.NewReader(".yoink f0"), filepath.Join(dir, "root"), Options{MaxDepth: depth, MaxOperations: 16, MaxInputBytes: 2048, MaxOutputBytes: 1024, Timeout: time.Second})
		if err != nil && out != "" {
			t.Fatalf("failed composition returned partial output %q", out)
		}
		if len(out) > 1024 {
			t.Fatalf("output budget exceeded: %d", len(out))
		}
		valid, err := ParseWithOptions(context.Background(), strings.NewReader("alive"), filepath.Join(dir, "root"), Options{MaxDepth: -1})
		if err != nil || valid != "alive" {
			t.Fatalf("host cannot continue: %q %v", valid, err)
		}
	})
}
