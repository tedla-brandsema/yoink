package zipline

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
)

var (
	parsers = make(map[string]ParseFunc)
)

type ParseFunc func(fileName string, lineNumber int, inputLine string) (string, error)

// Register binds the named action, which does not begin with a period, to the
// specified parser to be invoked when the name, with a period, appears in the
// present input text.
func Register(name string, parser ParseFunc) {
	if len(name) == 0 || name[0] == ';' {
		panic("bad name in Register: " + name)
	}
	parsers["."+name] = parser
}

// Parse parses a document from r.
// func Parse(r io.Reader, name string, mode ParseMode) (*Doc, error) {
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
	var wg sync.WaitGroup
	errCh := make(chan error, 1)

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
			parallelParse(&wg, errCh, parse, name, lines, lines.line, text)

		}
	}
	select {
	case err := <-errCh:
		log.Fatalf("parsing failed: %v", err)
	case <-ctx.Done():
		log.Fatalf("context cancelled: %v", ctx.Err())
	case <-wait(&wg):
		log.Println("done")
	}
	return nil
}

func parallelParse(wg *sync.WaitGroup, errCh chan<- error, parse ParseFunc, sourceFile string, lines *Lines, sourceLine int, cmd string) {
	if wg == nil {
		errCh <- errors.New("WaitGroup is nil")
		return
	}
	wg.Add(1)

	go func(wg *sync.WaitGroup) {
		defer wg.Done()

		var err error

		t, err := parse(sourceFile, sourceLine, cmd)
		if err != nil {
			errCh <- err
			return
		}

		if err = lines.set(t, sourceLine-1); err != nil {
			errCh <- err
			return
		}
	}(wg)
}

func wait(wg *sync.WaitGroup) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}
