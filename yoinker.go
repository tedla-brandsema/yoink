// This file, or portions thereof, are derived from the Go Present tool,
// licensed under the BSD 3-Clause License. See the NOTICE file for details.

package yoink

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Semaphore struct {
	sem chan struct{}
}

func NewSemaphore(max int) *Semaphore {
	return &Semaphore{
		sem: make(chan struct{}, max),
	}
}

func (s *Semaphore) Acquire() {
	s.sem <- struct{}{}
}

func (s *Semaphore) acquireContext(ctx context.Context) error {
	select {
	case s.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Semaphore) Release() {
	<-s.sem
}

var (
	sem      *Semaphore
	throttle time.Duration
)

func configure() {
	if Config.MaxConcurrent > 0 {
		sem = NewSemaphore(Config.MaxConcurrent)
	}
	if Config.MinInterval > 0 {
		throttle = Config.MinInterval
	}
}

func init() {
	configure()
	RegisterParser("yoink", yoinkCommand{})
}

type yoinkCommand struct{}

func (yoinkCommand) Parse(sourceFile string, sourceLine int, cmd string) (string, error) {
	return yoinkParser(sourceFile, sourceLine, cmd)
}

func (yoinkCommand) ParseContext(ctx context.Context, sourceFile string, sourceLine int, cmd string) (string, error) {
	return yoinkParserContext(ctx, sourceFile, sourceLine, cmd)
}

// yoinkParser parses a .yoink directive:
//
//	.yoink <URL|filename> [address]
func yoinkParser(sourceFile string, sourceLine int, cmd string) (string, error) {
	text, err := yoinkParserContext(context.Background(), sourceFile, sourceLine, cmd)
	if err != nil {
		return "", fmt.Errorf("%s:%d: %w", sourceFile, sourceLine, err)
	}
	return text, nil
}

func yoinkParserContext(ctx context.Context, sourceFile string, sourceLine int, cmd string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cmd = strings.TrimSpace(cmd)

	parts := strings.Fields(cmd)
	if len(parts) < 2 {
		return "", fmt.Errorf("syntax error for .yoink invocation")
	}

	file := parts[1]
	addr := ""
	if len(parts) > 2 {
		addr = strings.Join(parts[2:], " ")
	}

	var textBytes []byte
	var err error

	if strings.HasPrefix(file, "http://") || strings.HasPrefix(file, "https://") {
		if sem != nil {
			if err := sem.acquireContext(ctx); err != nil {
				return "", err
			}
			defer sem.Release()
		}
		if throttle > 0 {
			timer := time.NewTimer(throttle)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		uri, err := url.ParseRequestURI(file)
		if err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri.String(), nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("GET %s: HTTP %s", file, resp.Status)
		}

		textBytes, err = io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
	} else {
		filename := filepath.Join(filepath.Dir(sourceFile), file)
		// Special files can block indefinitely even while opening. Includes are
		// ordinary files; do not start asynchronous reads that could outlive Parse.
		info, err := os.Stat(filename)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s: inclusion requires a regular file", filename)
		}
		f, err := os.Open(filename)
		if err != nil {
			return "", err
		}
		defer f.Close()
		textBytes, err = io.ReadAll(contextReader{ctx, f})
		if err != nil {
			return "", err
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	lo, hi, err := addrToByteRange(addr, 0, textBytes)
	if err != nil {
		return "", err
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

	text := strings.Join(extractLines(textBytes, lo, hi), "\n")
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return text, nil
}

// extractLines takes a source file and returns the lines that
// span the byte range specified by start and end.
// It discards lines that end in "OMIT".
func extractLines(src []byte, start, end int) (lines []string) {
	// Unlike Scanner, splitting cannot silently truncate an included long line.
	for _, line := range bytes.Split(src[start:end], []byte{'\n'}) {
		l := strings.TrimSuffix(string(line), "\r")
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
