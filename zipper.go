package zipline

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register("zip", parseZip)
}

// parseZip parses an .zip directive:
//
//	.zip <URL|filename> [address]
func parseZip(sourceFile string, sourceLine int, cmd string) (string, error) {
	cmd = strings.TrimSpace(cmd)

	parts := strings.Fields(cmd)
	if len(parts) < 2 {
		return "", fmt.Errorf("%s:%d: syntax error for .inc invocation", sourceFile, sourceLine)
	}

	file := parts[1]
	addr := ""
	if len(parts) > 2 {
		addr = strings.Join(parts[2:], " ")
	}

	var textBytes []byte
	var err error

	if strings.HasPrefix(file, "http://") || strings.HasPrefix(file, "https://") {
		uri, err := url.ParseRequestURI(file)
		if err != nil {
			return "", err
		}
		resp, err := http.Get(uri.String())
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		textBytes, err = io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
	} else {
		filename := filepath.Join(filepath.Dir(sourceFile), file)
		textBytes, err = os.ReadFile(filename)
		if err != nil {
			return "", fmt.Errorf("%s:%d: %v", sourceFile, sourceLine, err)
		}
	}

	lo, hi, err := addrToByteRange(addr, 0, textBytes)
	if err != nil {
		return "", fmt.Errorf("%s:%d: %v", sourceFile, sourceLine, err)
	}
	if lo > hi {
		hi, lo = lo, hi
	}

	// Round to full lines
	for lo > 0 && textBytes[lo-1] != '\n' {
		lo--
	}
	if hi > 0 {
		for hi < len(textBytes) && textBytes[hi-1] != '\n' {
			hi++
		}
	}

	return strings.Join(extractLines(textBytes, lo, hi), "\n"), nil
}

// extractLines takes a source file and returns the lines that
// span the byte range specified by start and end.
// It discards lines that end in "OMIT".
func extractLines(src []byte, start, end int) (lines []string) {
	startLine := 1
	for i, b := range src {
		if i == start {
			break
		}
		if b == '\n' {
			startLine++
		}
	}
	s := bufio.NewScanner(bytes.NewReader(src[start:end]))
	for n := startLine; s.Scan(); n++ {
		l := s.Text()
		if strings.HasSuffix(l, "OMIT") {
			continue
		}
		lines = append(lines, l)
	}
	// Trim leading and trailing blank lines.
	for len(lines) > 0 && len(lines[0]) == 0 {
		lines = lines[1:]
	}
	for len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return
}
