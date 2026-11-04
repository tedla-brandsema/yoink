package yoink

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"unicode"
)

const compositionWorkers = 8

type outputNode struct {
	text  string
	parts []*outputNode
}
type ancestry struct {
	key    expansionKey
	name   string
	parent *ancestry
}
type compositionJob struct {
	source      sourceRef
	line, depth int
	text        string
	parser      Parser
	parent      *ancestry
	output      *outputNode
}
type compositionResult struct {
	job      compositionJob
	document *sourceDocument
	text     string
	err      error
}

type composer struct {
	ctx      context.Context
	opts     Options
	budget   *executionBudget
	registry map[string]Parser
	pending  []compositionJob
}

func compose(ctx context.Context, r io.Reader, name string, opts Options) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	budget := newBudget(opts)
	data, err := io.ReadAll(budgetReader{ctx, r, budget})
	if err != nil {
		return "", fmt.Errorf("%s: reading input: %w", name, err)
	}
	source, err := rootSource(name)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	root := &sourceDocument{source: source, data: data, lo: 0, hi: len(data), root: true, key: expansionKey{identity: source.identity, base: source.base, root: true, lo: 0, hi: len(data), hash: sha256.Sum256(data)}}
	c := &composer{ctx: ctx, opts: opts, budget: budget, registry: make(map[string]Parser)}
	parsersMu.RLock()
	for name, parser := range parsers {
		c.registry[name] = parser
	}
	parsersMu.RUnlock()
	output := &outputNode{}
	if err := c.expand(root, nil, output); err != nil {
		return "", err
	}
	// Neither workers nor the scheduler recursively invoke the composer. Workers
	// release all source resources before the scheduler discovers descendants.
	jobs := make(chan compositionJob)
	results := make(chan compositionResult, compositionWorkers)
	var workers sync.WaitGroup
	for i := 0; i < compositionWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					continue
				}
				result := compositionResult{job: job}
				if _, builtin := job.parser.(yoinkCommand); builtin {
					result.document, result.err = includedDocument(ctx, job, budget)
				} else if aware, ok := job.parser.(ContextParser); ok {
					result.text, result.err = aware.ParseContext(ctx, job.source.name, job.line, job.text)
				} else {
					result.text, result.err = job.parser.Parse(job.source.name, job.line, job.text)
				}
				select {
				case results <- result:
				case <-ctx.Done():
				}
			}
		}()
	}
	defer func() { cancel(); close(jobs); workers.Wait() }()
	active := 0
	for len(c.pending) > 0 || active > 0 {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		var send chan compositionJob
		var next compositionJob
		if len(c.pending) > 0 {
			send = jobs
			next = c.pending[0]
		}
		select {
		case send <- next:
			c.pending[0] = compositionJob{}
			c.pending = c.pending[1:]
			active++
		case result := <-results:
			active--
			if result.err != nil {
				return "", fmt.Errorf("%s:%d: %w", result.job.source.name, result.job.line, result.err)
			}
			if result.document != nil {
				if err := c.expand(result.document, result.job.parent, result.job.output); err != nil {
					return "", fmt.Errorf("%s:%d: %w", result.job.source.name, result.job.line, err)
				}
			} else {
				if err := budget.emit(len(result.text)); err != nil {
					return "", fmt.Errorf("%s:%d: %w", result.job.source.name, result.job.line, err)
				}
				result.job.output.text = result.text
			}
		case <-ctx.Done():
			return "", fmt.Errorf("%s: %w", name, ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	// Final output has already been charged, so allocate it once. Traverse with
	// explicit frames rather than recursive calls even for very deep chains.
	var builder strings.Builder
	builder.Grow(int(budget.output))
	type frame struct {
		node  *outputNode
		index int
	}
	stack := []frame{{node: output}}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		f := &stack[len(stack)-1]
		if len(f.node.parts) == 0 {
			builder.WriteString(f.node.text)
			stack = stack[:len(stack)-1]
			continue
		}
		if f.index == len(f.node.parts) {
			stack = stack[:len(stack)-1]
			continue
		}
		child := f.node.parts[f.index]
		f.index++
		stack = append(stack, frame{node: child})
	}
	return builder.String(), nil
}

func (c *composer) expand(doc *sourceDocument, parent *ancestry, out *outputNode) error {
	if c.opts.MaxDepth == -1 {
		for p := parent; p != nil; p = p.parent {
			if p.key == doc.key {
				return cycleError(parent, doc.source.name)
			}
		}
	}
	current := &ancestry{key: doc.key, name: doc.source.name, parent: parent}
	first := true
	var literals strings.Builder
	flush := func() {
		if literals.Len() > 0 {
			out.parts = append(out.parts, &outputNode{text: literals.String()})
			literals = strings.Builder{}
		}
	}
	visit := func(text []byte, line int) error {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		if !first {
			if err := c.budget.emit(1); err != nil {
				return fmt.Errorf("%s:%d: %w", doc.source.name, line, err)
			}
			literals.WriteByte('\n')
		}
		first = false
		expandable := c.opts.MaxDepth == -1 || doc.depth < c.opts.MaxDepth
		unknown := false
		if expandable && len(text) > 0 && text[0] == '.' {
			end := bytes.IndexFunc(text, unicode.IsSpace)
			if end < 0 {
				end = len(text)
			}
			parser := c.registry[string(text[:end])]
			if parser != nil {
				if len(text) >= bufio.MaxScanTokenSize {
					return fmt.Errorf("%s:%d: directive line exceeds %d byte scanner limit", doc.source.name, line, bufio.MaxScanTokenSize)
				}
				if err := c.budget.operation(); err != nil {
					return fmt.Errorf("%s:%d: %w", doc.source.name, line, err)
				}
				flush()
				child := &outputNode{}
				out.parts = append(out.parts, child)
				c.pending = append(c.pending, compositionJob{source: doc.source, line: line, depth: doc.depth + 1, text: string(text), parser: parser, parent: current, output: child})
				return nil
			}
			unknown = true
		}
		if err := c.budget.emit(len(text)); err != nil {
			return fmt.Errorf("%s:%d: %w", doc.source.name, line, err)
		}
		if unknown {
			log.Printf("%s:%d: unknown command %q", doc.source.name, line, text)
		}
		literals.Write(text)
		return nil
	}
	if doc.root {
		scanner := bufio.NewScanner(bytes.NewReader(doc.data))
		line := 0
		for scanner.Scan() {
			line++
			if err := visit(scanner.Bytes(), line); err != nil {
				return err
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("%s: reading input: %w", doc.source.name, err)
		}
	} else if err := visitIncluded(c.ctx, doc, visit); err != nil {
		return err
	}
	flush()
	return nil
}

func cycleError(parent *ancestry, name string) error {
	chain := []string{name}
	count := 0
	for p := parent; p != nil; p = p.parent {
		count++
		if count <= 32 {
			chain = append(chain, p.name)
		}
	}
	if count > 32 {
		chain = append(chain, fmt.Sprintf("... %d earlier sources", count-32))
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return fmt.Errorf("%w: %s", ErrCycle, strings.Join(chain, " -> "))
}
