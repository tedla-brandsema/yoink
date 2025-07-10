// This file, or portions thereof, are derived from the Go Present tool,
// licensed under the BSD 3-Clause License. See the NOTICE file for details.

package yoink

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
)

type ConcurrentContext struct {
	Ctx   context.Context
	WG    *sync.WaitGroup
	errCh chan error
}

func (c *ConcurrentContext) ErrCh() <-chan error {
	return c.errCh
}

func (c *ConcurrentContext) SendErr(err error) {
	select {
	case c.errCh <- err:
	default:
	}
}

func (c *ConcurrentContext) Wait() error {
	select {
	case err := <-c.errCh:
		return fmt.Errorf("parsing failed: %w", err)
	case <-c.Ctx.Done():
		return fmt.Errorf("context cancelled: %w", c.Ctx.Err())
	case <-wait(c.WG):
		return nil
	}
}

var (
	parsersMu sync.RWMutex
	parsers   = make(map[string]Parser)
)

type Parser interface {
	Parse(fileName string, lineNumber int, inputLine string) (string, error)
}

type ParseFunc func(fileName string, lineNumber int, inputLine string) (string, error)

func (p ParseFunc) Parse(fileName string, lineNumber int, inputLine string) (string, error) {
	return p(fileName, lineNumber, inputLine)
}

func RegisterParser(name string, parser Parser) {
	if len(name) == 0 || name[0] == ';' {
		panic("invalid parser name: " + name)
	}
	parsersMu.Lock()
	defer parsersMu.Unlock()
	parsers["."+name] = parser
}

func RegisterParserFunc(name string, fn ParseFunc) {
	RegisterParser(name, fn)
}

// Parse parses a document from r.
func Parse(ctx context.Context, r io.Reader, name string) (string, error) {
	lines, err := readLines(r)
	if err != nil {
		return "", err
	}

	lines.comment = "//"

	if err = parseLines(ctx, name, lines); err != nil {
		return "", err
	}

	return strings.Join(lines.text, "\n"), nil
}

type Lines struct {
	line    int // 0 indexed, so has 1-indexed number of last line returned
	text    []string
	comment string
	mut     sync.RWMutex
}

func readLines(r io.Reader) (*Lines, error) {
	var lines []string
	s := bufio.NewScanner(r)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return &Lines{
		line:    0,
		text:    lines,
		comment: "#"}, nil
}

func (l *Lines) next() (text string, ok bool) {
	l.mut.Lock()
	defer l.mut.Unlock()

	for {
		current := l.line
		l.line++
		if current >= len(l.text) {
			return "", false
		}
		text = l.text[current]
		// Lines starting with l.comment are comments.
		if l.comment == "" || !strings.HasPrefix(text, l.comment) {
			ok = true
			break
		}
	}
	return
}

func (l *Lines) set(txt string, line int) error {
	l.mut.Lock()
	defer l.mut.Unlock()

	txtLen := len(l.text)
	if line < 0 || line >= txtLen { // line number bounds check
		return fmt.Errorf("unable to set line  #%d: out of bounds", line)
	}
	l.text[line] = txt
	return nil
}

func (l *Lines) back() {
	l.mut.Lock()
	defer l.mut.Unlock()

	l.line--
}

func (l *Lines) nextNonEmpty() (text string, ok bool) {
	for {
		text, ok = l.next()
		if !ok {
			return
		}
		if len(text) > 0 {
			break
		}
	}
	return
}

func parseLines(ctx context.Context, name string, lines *Lines) error {
	pc := &ConcurrentContext{
		Ctx:   ctx,
		WG:    &sync.WaitGroup{},
		errCh: make(chan error, 1),
	}

	for i := 1; ; i++ {
		text, ok := lines.nextNonEmpty()
		for ok && text == "" { // skip empty lines
			text, ok = lines.next()
		}
		if !ok {
			break
		}

		if strings.HasPrefix(text, ".") { // Handle command
			args := strings.Fields(text)

			parse := parsers[args[0]]
			if parse == nil {
				log.Printf("%s:%d: unknown command %q", name, lines.line, text)
				continue
			}
			concurrentParse(pc, parse, name, lines, lines.line, text)

		}
	}
	if err := pc.Wait(); err != nil {
		log.Fatal(err)
	}
	return nil
}

func concurrentParse(pc *ConcurrentContext, parser Parser, sourceFile string, lines *Lines, sourceLine int, cmd string) {
	pc.WG.Add(1)

	go func() {
		defer pc.WG.Done()

		t, err := parser.Parse(sourceFile, sourceLine, cmd)
		if err != nil {
			pc.SendErr(err)
			return
		}

		if err = lines.set(t, sourceLine-1); err != nil {
			pc.SendErr(err)
		}
	}()
}

func wait(wg *sync.WaitGroup) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}
