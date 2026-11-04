package yoink

import (
	"errors"
	"regexp/syntax"
	"strings"
	"testing"
)

func TestAddressRanges(t *testing.T) {
	tests := []struct {
		name, addr, data string
		lo, hi           int
	}{
		{"whole file", "", "alpha\nbeta\n", 0, 11},
		{"first line", "1", "alpha\nbeta\n", 0, 6},
		{"line range", "1,2", "alpha\nbeta\n", 0, 11},
		{"final line without newline", "2", "alpha\nbeta", 6, 10},
		{"range without final newline", "1,2", "alpha\nbeta", 0, 10},
		{"single unterminated line", "1", "alpha", 0, 5},
		{"regex", "/beta/", "alpha\nbeta\n", 6, 10},
		{"optional final slash", "/beta", "alpha\nbeta\n", 6, 10},
		{"escaped slash", `/a\/b/`, "a/b\n", 0, 3},
		{"escaped backslash", `/a\\/`, "a\\\n", 0, 2},
		{"empty regex", "/", "alpha\n", 0, 0},
		{"regex comma", "/a,b/", "a,b\n", 0, 3},
		{"multiple endpoints", "1,2,3", "a\nb\nc\n", 0, 6},
		{"open endpoint", "2,", "a\nb\nc\n", 2, 6},
		{"forward rune", "#2", "aβ🙂", 3, 3},
		{"backward rune at EOF", "$-#1", "aβ🙂", 3, 3},
		{"two backward runes", "$-#2", "aβ🙂", 1, 1},
		{"backward rune from interior", "#2-#1", "aβ🙂", 1, 1},
		{"backward invalid UTF8", "$-#1", "a\xff", 1, 1},
		{"empty file", "$-#0", "", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lo, hi, err := addrToByteRange(tt.addr, 0, []byte(tt.data))
			if err != nil || lo != tt.lo || hi != tt.hi {
				t.Fatalf("address %q = (%d, %d, %v), want (%d, %d, nil)", tt.addr, lo, hi, err, tt.lo, tt.hi)
			}
		})
	}
}

func TestMalformedAddresses(t *testing.T) {
	for _, addr := range []string{
		`/\`, `/alpha\`, `/alpha/\`, `/[/`, "/missing/", "99", "-#1", "$+#1",
		"99999999999999999999999999999999", "?", "-/alpha/", "1,99", "2,3",
	} {
		t.Run(addr, func(t *testing.T) {
			if _, _, err := addrToByteRange(addr, 0, []byte("alpha\nbeta")); err == nil {
				t.Fatalf("address %q succeeded", addr)
			}
		})
	}
	for _, start := range []int{-1, 4} {
		if _, _, err := addrToByteRange("", start, []byte("abc")); err == nil {
			t.Fatalf("start %d succeeded", start)
		}
	}
	_, _, err := addrToByteRange("/[/", 0, nil)
	var syntaxErr *syntax.Error
	if !errors.As(err, &syntaxErr) {
		t.Fatalf("regexp error identity lost: %v", err)
	}
}

func TestAddressManyEndpoints(t *testing.T) {
	// Endpoints require constant stack space even for maliciously long input.
	lo, hi, err := addrToByteRange(strings.Repeat(",", 100000), 0, []byte("alpha\n"))
	if err != nil || lo != 0 || hi != 6 {
		t.Fatalf("many endpoints = (%d, %d, %v)", lo, hi, err)
	}
}

func FuzzAddress(f *testing.F) {
	for _, addr := range []string{"", "1,2", "/alpha/", "/alpha", `/\`, `/alpha\`, "/[/", "$-#1", "#2-#1", "1,2,3", "999999999999999999999999", "?", "-,"} {
		f.Add(addr, []byte("alpha\nβ🙂\n"), 0)
	}
	f.Add("$-#1", []byte{}, 0)
	f.Add("", []byte("abc"), -1)
	f.Fuzz(func(t *testing.T, addr string, data []byte, start int) {
		if len(addr) > 4096 || len(data) > 4096 {
			t.Skip()
		}
		lo, hi, err := addrToByteRange(addr, start, data)
		if err == nil && (lo < 0 || hi < 0 || lo > len(data) || hi > len(data)) {
			t.Fatalf("invalid range (%d, %d) for %d bytes, addr %q, start %d", lo, hi, len(data), addr, start)
		}
	})
}
