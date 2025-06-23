package zipline

import (
	"bufio"
	"fmt"
	"io"
	"strings"
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

// TODO: user-agent for http(s) request

// Parse parses a document from r.
// func Parse(r io.Reader, name string, mode ParseMode) (*Doc, error) {
func Parse(r io.Reader, name string) (string, error) {
	lines, err := readLines(r)
	if err != nil {
		return "", err
	}

	lines.comment = "//"

	if err = parseLines(name, lines); err != nil {
		return "", err
	}

	return strings.Join(lines.text, "\n"), nil
}

type Lines struct {
	line    int // 0 indexed, so has 1-indexed number of last line returned
	text    []string
	comment string
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
	return &Lines{0, lines, "#"}, nil
}

func (l *Lines) next() (text string, ok bool) {
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
	txtLen := len(l.text)
	if line < 0 || line >= txtLen {
		return fmt.Errorf("unable to set line  #%d: out of bounds", line)
	}
	l.text[line] = txt
	return nil
}

func (l *Lines) back() {
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

func parseLines(name string, lines *Lines) error {

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

			parser := parsers[args[0]]
			if parser == nil {
				return fmt.Errorf("%s:%d: unknown command %q", name, lines.line, text)
			}
			t, err := parser(name, lines.line, text)
			if err != nil {
				return err
			}
			if err = lines.set(t, lines.line-1); err != nil {
				return err
			}
		}
	}
	return nil
}
