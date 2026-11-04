# Yoink

_Yoink_ is a stripped-down version of the [GO present tool](https://pkg.go.dev/golang.org/x/tools/present). _Yoink_ is 
built to include files (or parts thereof) into another file, concurrently. Those files can be included locally or over http(s).

You can extend _Yoink_ by [creating your own command](#creating-your-own-command). 

_Yoink_ is  available as a library or as a commandline tool. Below you can find instructions on how to build and use 
[yoink on the commandline](#yoink-on-the-commandline).

NOTE:
_Yoink_ is a line-based composition library. Its default is flat inclusion; v0.2.0 also offers bounded or unlimited recursive inclusion through per-call options. It has no contextual awareness of markup or hierarchical formats. You can use it with HTML, JSON, and similar formats, but the resulting output may not be valid in those formats.

![Tests](https://github.com/tedla-brandsema/yoink/actions/workflows/test.yml/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/tedla-brandsema/yoink)](https://goreportcard.com/report/github.com/tedla-brandsema/yoink)

# Why Yoink?

The _GO present tool_:
1. produces either Markdown or HTML;
2. parses commands sequentially and not concurrently;
3. does not possess the ability to include remote files over http(s).

I could have added point 3 (remote files) within the existing API, but both points one (output format) and two 
(sequential parsing) are tightly integrated into the parser of the _GO present tool_.

This, by no means, is meant as criticism; the _GO present tool_ was made with a very specific task in mind, and it executes 
that task splendidly. It was more the case of me wanting to **misuse** the _GO present tool_ outside its main purpose, 
which led me to adapt its codebase into _Yoink_.

# Installing The Library

Use Go 1.24.4 or newer to add the versioned library dependency. CI tests the latest stable Go release.
```
go get github.com/tedla-brandsema/yoink@v0.2.0
```

Then, import _Yoink_ in your application:

```go
import "github.com/tedla-brandsema/yoink"
```

## Library API and errors

```go
func Parse(ctx context.Context, r io.Reader, name string) (string, error)
```

Pass a non-nil context and reader. `name` identifies the source in errors and
provides the directory for relative local includes. Independent calls to `Parse`
can run concurrently. On failure, `Parse` returns an empty string and an error;
malformed input and ordinary cancellation do not exit the caller's process.
Errors retain the source filename and directive line, and wrap underlying errors
so callers can use `errors.Is` and `errors.As`.

For example, a caller can handle cancellation and continue serving other work:

```go
result, err := yoink.Parse(ctx, reader, "docs/input.txt")
if err != nil {
    if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
        return // the caller decides how to handle cancellation
    }
    fmt.Fprintln(os.Stderr, err)
    return
}
fmt.Print(result)
```

This fragment uses the standard `context`, `errors`, `fmt`, and `os` packages.
A missing include, unmatched address, malformed address, invalid regular expression,
or registered parser error rejects the entire composition. Unknown commands keep
the original line and produce a standard logger warning, as before.

## Cancellation and completion

`Parse` joins every started parser worker before returning, including when a worker
fails or the context expires. The first reported worker error cancels sibling work.
HTTP requests, response reads, remote concurrency waits, and the per-request delay
honor cancellation. A successful return preserves document order even when callbacks
finish in another order. No successful partial result is returned on failure.

Existing `Parser` and `ParseFunc` callbacks remain supported. They must return
before `Parse` can return: Go cannot forcibly stop a blocked callback. A parser can
add cooperative cancellation by also implementing this optional interface:

```go
type ContextParser interface {
    ParseContext(ctx context.Context, fileName string, lineNumber int, inputLine string) (string, error)
}
```

Register it with `RegisterParser` as usual; it must still implement `Parser`.
Yoink calls `ParseContext` in preference to `Parse`. Check `ctx.Done()` or propagate
the context to blocking operations. Callbacks must join any goroutines they create
and synchronize their own mutable state; registered instances are shared across
commands and concurrent documents. Registration and lookup are synchronized, and
each document uses one registry snapshot. Invalid names passed to `RegisterParser`
remain programmer errors and panic. Panics in third-party callbacks are not recovered.

The input reader is caller-owned and is not closed by Yoink. Input and local-file
reads run synchronously, checking cancellation between reads. A blocked arbitrary
reader or filesystem system call must finish before cancellation can return; no
background reader is left behind. Local includes must be regular files. Yoink
closes files and HTTP response bodies before their workers finish.

## Composition and resource boundaries

`Parse` and the default CLI invocation remain flat: a directive inside included
content is emitted literally. `ParseWithOptions` and `-depth` enable recursion
as described below. Root lines and included lines use LF output;
spaces within lines are preserved. Included lines ending exactly in `OMIT` are
removed, and leading/trailing empty included lines are trimmed. `Parse` does not
append a final newline; the CLI appends one. Root input retains the scanner's
roughly 64 KiB per-line limit and returns an error on oversized lines. Included
lines can be longer and are no longer silently truncated.

Local paths are relative to the source document. Remote includes support HTTP(S),
follow the default HTTP client's redirects, and require a final 2xx response.
Remote work retains the default limit of five concurrent requests across calls,
with a 200 ms delay per request after acquiring a slot. The legacy `Config` value
is consumed during package initialization; changing it later does not reconfigure
the built-in parser.

The compatible flat `Parse` API has no default HTTP timeout or response size
limit; use a deadline when needed. Recursive composition has shared finite budgets
and a timeout, described below. Documents and includes are held in memory. Yoink
is not a filesystem or network sandbox: paths may traverse directories or follow
symlinks, and URLs may reach internal services. Recursion lets one document reach
additional files and network endpoints. Applications handling untrusted documents
must enforce their own path, URL, and access policies; resource budgets do not
replace those controls.


# Recursive Composition

`Parse` retains the v0.1.0 behavior. Use the additive API for per-call depth and
resource options:

```go
func ParseWithOptions(ctx context.Context, r io.Reader, name string, opts Options) (string, error)

type Options struct {
    MaxDepth       int
    MaxOperations  int
    MaxInputBytes  int64
    MaxOutputBytes int64
    Timeout        time.Duration
}
```

Options belong to one call. Concurrent callers can choose different depths and
limits without changing global configuration. `Options{}` and
`Options{MaxDepth: 1}` are equivalent to `Parse` when no budget field is set.

## Depth and source locations

| `MaxDepth` | Meaning |
|---|---|
| `0` | Default to depth 1 |
| `1` | Flat: expand directives in the root only |
| `N >= 2` | Expand up to N inclusion levels |
| `-1` | Unlimited depth, subject to resource budgets and cycle detection |

Other negative depths are invalid. The root document has depth 0; a directive in
it includes a document at depth 1, whose directives include documents at depth 2.
At a finite boundary, remaining directives stay literal. Reaching that boundary
is successful composition, not an error.

For example:

```text
documents/
├── grand-vision.md       # .yoink sections/stage-1.md
├── sections/
│   └── stage-1.md        # .yoink ../knowledge/economy.md
└── knowledge/
    └── economy.md
```

Depth 1 includes `stage-1.md` and leaves its directive literal. Depth 2 also
includes `economy.md`. The second path resolves relative to `sections/stage-1.md`,
not the root document. Source identities and original line numbers remain attached
to nested directives, including directives in selected excerpts.

The caller-owned root reader uses the directory of its supplied `name`. Loaded
recursive local sources use the canonical target file's directory: a file symlink
resolves its nested includes from the target directory, not the alias directory.
Directory symlink aliases therefore cannot grow the expansion path indefinitely.
Diagnostics and custom callbacks retain the source path used to reach the file.
The legacy flat `Parse` path remains unchanged.

For remote documents, a relative target resolves against the containing document's
HTTP(S) URL. After a redirect, the final response URL supplies that base. An
absolute HTTP(S) URL can be included from a local or remote document. Recursive
remote inclusion supports HTTP and HTTPS, not arbitrary URL schemes.

An address selects and rounds the source range first; OMIT filtering and trimming
of leading/trailing empty included lines happen before recursive expansion.
Directives in excluded ranges or removed OMIT lines do not run. Existing sam-style
address syntax and output ordering remain unchanged.

Registered `Parser`, `ParseFunc`, and `ContextParser` commands work inside expanded
sources without changes to their interfaces. Their generated output is inserted
literally, even if it contains `.yoink` text. Only actual built-in `.yoink`
inclusions establish another source document. Registering a custom replacement for
`yoink` therefore retains ordinary custom-parser output behavior.

## Repeated sources and cycles

Independent branches may include the same file repeatedly. Unlimited mode detects
a cycle only when the same expansion identity recurs in its active inclusion
ancestry. That identity includes the canonical file or URL, effective resolution base,
selected byte range, actual selected content, and root-versus-include filtering
mode (OMIT applies only to included content). Local identity canonicalization accounts for path
aliases and symlinks; remote identities use the final response URL.

This permits terminating range-selected self-inclusion, where the selected region
contains no further recursive directive. A genuine unlimited cycle returns an
error describing the inclusion chain; `errors.Is(err, yoink.ErrCycle)` identifies
it. Finite-depth requests may unfold repeated sources until their requested
boundary, within the same resource budgets.

The composer uses an explicit work queue and iterative output traversal rather
than recursive Go calls. Deep finite chains either complete within the budgets or
return a controlled limit error.

## Shared resource budgets

Recursion enables these finite defaults. Setting any budget field also enables
budgeted composition for flat depth; zero budget fields then use the defaults.
Negative budget values are invalid.

| Option | Default | Maximum override |
|---|---:|---:|
| `MaxOperations` | 10,000 recognized command invocations | 1,000,000 |
| `MaxInputBytes` | 32 MiB | 1 GiB |
| `MaxOutputBytes` | 32 MiB | 1 GiB |
| `Timeout` | 30 seconds | 24 hours |

All limits are shared across the complete call, including parallel branches.
Operations count built-in inclusions and registered custom commands. Input bytes
include the root, entire fetched sources before address selection, and repeated
reads of the same source. At most one additional byte may be read on an input-limit
failure to distinguish EOF from excess data. Output bytes count the final expanded
result, including newline separators and custom-parser output.

An explicit pool of eight composition workers bounds concurrency and fan-out;
remote requests also retain the shared five-request throttle. Output size is
checked before final string allocation, without repeatedly copying intermediate
expanded documents. A custom parser allocates its own result before Yoink can
check that result's size, so custom code remains responsible for its allocations.
Input accounting covers the root and built-in source reads; custom parsers must
bound their own I/O as well as allocations.
Recognized nested directive lines must be shorter than 64 KiB to bound command
tokenization; ordinary included lines can be longer. Directives left literal at a
finite depth boundary are not parsed or subject to this command-line limit.

Budget failures return an empty result and a meaningful error. Use
`errors.Is(err, yoink.ErrLimitExceeded)` or `errors.As` with `*yoink.LimitError`;
its `Resource` and `Limit` fields identify the exhausted budget. The earlier of
the caller's context deadline and `Timeout` governs cooperative work, with
`context.DeadlineExceeded` remaining detectable through `errors.Is`.

Unlimited depth removes only the requested depth boundary. It does not remove
operation, byte, or time limits. All workers still finish before return, including
on a cycle, error, limit, or cancellation. Legacy callbacks and synchronous readers
or filesystem calls that ignore cancellation can delay completion beyond the
deadline; Go cannot forcibly stop those operations.

## Go example

This complete program composes a file named by its first argument:

```go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/tedla-brandsema/yoink"
)

func main() {
    if len(os.Args) != 2 {
        fmt.Fprintln(os.Stderr, "usage: compose input.md")
        return
    }
    name := os.Args[1]
    input, err := os.Open(name)
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        return
    }
    defer input.Close()

    result, err := yoink.ParseWithOptions(context.Background(), input, name, yoink.Options{
        MaxDepth:       -1, // or 1 for flat, 2 for two levels
        MaxOperations:  20_000,
        MaxInputBytes:  64 << 20,
        MaxOutputBytes: 64 << 20,
        Timeout:        45 * time.Second,
    })
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        return
    }
    fmt.Println(result)
}
```

From a checkout, use `testdata/recursive/grand-vision.md` as the input for a small
source-relative example. The same fixture also works with the CLI below.

# Working With Local Files ([example](https://github.com/tedla-brandsema/examples/tree/main/yoink/1_local))

You can include local files by adding the `.yoink` command at the start of a line, followed by the name of
the file to include.

Below is an example file that contains two such commands.

```
Sonnet 18: Shall I compare thee to a summer’s day?
By William Shakespeare

.yoink sonnet-18-quatrains.txt

.yoink sonnet-18-rhyming-couplet.txt
```

One command pointing to `sonnet-18-quatrains.txt` and the other to `sonnet-18-rhyming-couplet.txt`.

To let _Yoink_ resolve this, we first need to open our root file and then pass it to `yoink.Parse` in the form of an
`io.Reader`.

```go
package main

import (
	"context"
	"fmt"
	"github.com/tedla-brandsema/yoink"
	"os"
)

func main() {
	// Open the root file
	name := "./data/sonnet-18.txt"
	file, err := os.Open(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	defer file.Close()

	// Resolve .yoink commands in the root file
	txt, err := yoink.Parse(context.Background(), file, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	fmt.Println(txt)
}
```

Running the program yields the following result:
```
Sonnet 18: Shall I compare thee to a summer’s day?
By William Shakespeare

Shall I compare thee to a summer’s day?
Thou art more lovely and more temperate:
Rough winds do shake the darling buds of May,
And summer’s lease hath all too short a date;

Sometime too hot the eye of heaven shines,
And often is his gold complexion dimm'd;
And every fair from fair sometime declines,
By chance or nature’s changing course untrimm'd;

But thy eternal summer shall not fade,
Nor lose possession of that fair thou ow’st;
Nor shall death brag thou wander’st in his shade,
When in eternal lines to time thou grow’st:

So long as men can breathe or eyes can see,
So long lives this, and this gives life to thee.
```

# Working With Remote Files ([example](https://github.com/tedla-brandsema/examples/tree/main/yoink/2_remote))

Including remote files works much the same as working with local files; you add the `.yoink` command to the start of a line,
but instead of adding the name of the file to include, you add the URL of the file to include.

In the example below, there are two `.yoink` commands pointing to URLs with raw text data.

```
Sonnet 18: Shall I compare thee to a summer’s day?
By William Shakespeare

.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/1_local/data/sonnet-18-quatrains.txt

.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/1_local/data/sonnet-18-rhyming-couplet.txt
```

Parsing of a file that contains a `.yoink` command that points to a URL is exactly the same as parsing a file with `.yoink`
commands that point to local files.

```go
package main

import (
	"context"
	"fmt"
	"github.com/tedla-brandsema/yoink"
	"os"
)

func main() {
	// Open the root file
	name := "./data/sonnet-18-remote.txt"
	file, err := os.Open(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	defer file.Close()

	// Resolve .yoink commands in the root file
	txt, err := yoink.Parse(context.Background(), file, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	fmt.Println(txt)
}
```

Running the program yields the exact same result that we saw in the previous example.

**Q: _Can you add local and remote `.yoink` commands to the same file?_**  
**A:** _Absolutely you can. In fact, this is a crucial concept to grasp; every command is parsed individually. This also 
goes for commands you might implement yourself. All commands that are registered with _Yoink_ can be called from within 
the file that is parsed._

# The Address Argument ([example](https://github.com/tedla-brandsema/examples/tree/main/yoink/3_address))

Yoinks most useful and powerful feature is the ability to target a specific region by adding an address at the end 
of a `.yoink` command.

The address syntax is similar in its simplest form to that of ed, but comes from sam and is more general. See 
[Table II](https://plan9.io/sys/doc/sam/sam.html) of _The Text Editor sam_ manual for full details. 
The displayed block is always rounded out to a full line at both ends. Reverse regular-expression searches are not implemented. A final closing regex slash is optional; a trailing escape is an error. Numeric line selection also accepts a final line without a newline.

In our previous examples, we only included entire files into our _base_ file. With the help of the address argument, 
however, we can target a specific region or excerpt from a file.


Consider the following _base_ file:
```
Sonnet 18: Shall I compare thee to a summer’s day?
By William Shakespeare

.yoink sonnet-18-quatrains.txt /START stanza-2/,/END stanza-2/

.yoink sonnet-18-rhyming-couplet.txt

.yoink sonnet-18-quatrains.txt /START stanza-3/,/END stanza-3/

.yoink sonnet-18-quatrains.txt /START stanza-1/,/END stanza-1/
```

Here you can see that all `.yoink` commands that point to `sonnet-18-quatrains.txt` have a second argument; an address.

For example, the address of the first `.yoink` command reads as follows:
```
/START stanza-2/,/END stanza-2/
```

If you take a look at [Table II](https://plan9.io/sys/doc/sam/sam.html) of _The Text Editor sam_ manual, 
we see that we have a regular expression`/START stanza-2/` followed by the address mark `,` and finally we have a second 
regular expression `/END stanza-2/`. The regular expressions here are just literal matches, 
but note that you can use more involved regular expression pattern matching syntax.

The addresses in our _base_ file correspond to demarcations in our target file: sonnet-18-quatrains.txt. See below:
```
#START stanza-1
Shall I compare thee to a summer’s day?
Thou art more lovely and more temperate:
Rough winds do shake the darling buds of May,
And summer’s lease hath all too short a date;
#END stanza-1

#START stanza-2
Sometime too hot the eye of heaven shines,
And often is his gold complexion dimm'd;
And every fair from fair sometime declines,
By chance or nature’s changing course untrimm'd;
#END stanza-2

#START stanza-3
But thy eternal summer shall not fade,
Nor lose possession of that fair thou ow’st;
Nor shall death brag thou wander’st in his shade,
When in eternal lines to time thou grow’st:
#END stanza-3
```

Running our jumbled _base_ file with _Yoink_, yields the following result.
```
Sonnet 18: Shall I compare thee to a summer’s day?
By William Shakespeare

#START stanza-2
Sometime too hot the eye of heaven shines,
And often is his gold complexion dimm'd;
And every fair from fair sometime declines,
By chance or nature’s changing course untrimm'd;
#END stanza-2

So long as men can breathe or eyes can see,
So long lives this, and this gives life to thee.

#START stanza-3
But thy eternal summer shall not fade,
Nor lose possession of that fair thou ow’st;
Nor shall death brag thou wander’st in his shade,
When in eternal lines to time thou grow’st:
#END stanza-3

#START stanza-1
Shall I compare thee to a summer’s day?
Thou art more lovely and more temperate:
Rough winds do shake the darling buds of May,
And summer’s lease hath all too short a date;
#END stanza-1
```

We succeeded in jumbling the output using addresses, but we now have the undesirable effect that the address 
demarcations ended up in the resulting output. 

Luckily we have a way to circumvent this; any line in the program that ends with the four characters `OMIT` is deleted 
from the source before inclusion.

So if we were to change our _base_ file to:
```
Sonnet 18: Shall I compare thee to a summer’s day?
By William Shakespeare

.yoink sonnet-18-quatrains-OMIT.txt /START stanza-2 OMIT/,/END stanza-2 OMIT/

.yoink sonnet-18-rhyming-couplet.txt

.yoink sonnet-18-quatrains-OMIT.txt /START stanza-3 OMIT/,/END stanza-3 OMIT/

.yoink sonnet-18-quatrains-OMIT.txt /START stanza-1 OMIT/,/END stanza-1 OMIT/
```

And our `sonnet-18-quatrains.txt` file to:
```
#START stanza-1 OMIT
Shall I compare thee to a summer’s day?

#START stanza-2 OMIT
Sometime too hot the eye of heaven shines,
And often is his gold complexion dimm'd;
And every fair from fair sometime declines,
By chance or nature’s changing course untrimm'd;
#END stanza-2 OMIT

#START stanza-3 OMIT
But thy eternal summer shall not fade,
Nor lose possession of that fair thou ow’st;
Nor shall death brag thou wander’st in his shade,
When in eternal lines to time thou grow’st:
#END stanza-3 OMIT
```

The result would be as follows:
```
Sonnet 18: Shall I compare thee to a summer’s day?
By William Shakespeare

Sometime too hot the eye of heaven shines,
And often is his gold complexion dimm'd;
And every fair from fair sometime declines,
By chance or nature’s changing course untrimm'd;

So long as men can breathe or eyes can see,
So long lives this, and this gives life to thee.

But thy eternal summer shall not fade,
Nor lose possession of that fair thou ow’st;
Nor shall death brag thou wander’st in his shade,
When in eternal lines to time thou grow’st:

Shall I compare thee to a summer’s day?
Thou art more lovely and more temperate:
Rough winds do shake the darling buds of May,
And summer’s lease hath all too short a date;
```

The result is still jumbled, but no longer contains the address demarcations.


# Creating Your Own Command

You create your own command by first creating a parser and then registering the parser with its accompanying command string.

There are two ways of creating your own parser:
1. Implement `yoink.ParseFunc`.
2. Implement `yoink.Parser`.

A function is convenient for a stateless command. Either approach can share state; functions that close over mutable state need synchronization too.

## Stateless ([example](https://github.com/tedla-brandsema/examples/tree/main/yoink/4_stateless))

Let's start out by creating a stateless parser by implementing the `yoink.ParseFunc`:
```go
func HelloParser(sourceFile string, sourceLine int, cmd string) (string, error) {
	// Default subject of the greeting
	subject := fmt.Sprintf("from %s", sourceFile)

	// Split the command into its parts where the first part is always the command associated with this parser.
	// In this case .hello
	parts := strings.Fields(cmd)
	if len(parts) > 1 {
		// Substitute the default subject with the arguments provided
		subject = strings.Join(parts[1:], " ")
	}

	// Return the greeting. Starting with the line number, followed by the greeting to our subject.
	return fmt.Sprintf("%d. Hello, %s!", sourceLine, subject), nil
}
```

We also need to register our `HelloParser` with _Yoink_:
```go
	yoink.RegisterParserFunc("hello", HelloParser)
```

After that we can parse files containing `.hello` commands like the one below:
```
1. Line one.
.hello
3. Line three.
.hello Tedla Brandsema
```

The full stateless example looks like this:
```go
package main

import (
	"context"
	"fmt"
	"github.com/tedla-brandsema/yoink"
	"os"
	"strings"
)

func HelloParser(sourceFile string, sourceLine int, cmd string) (string, error) {
	// Default subject of the greeting
	subject := fmt.Sprintf("from %s", sourceFile)

	// Split the command into its parts where the first part is always the command associated with this parser.
	// In this case .hello
	parts := strings.Fields(cmd)
	if len(parts) > 1 {
		// Substitute the default subject with the arguments provided
		subject = strings.Join(parts[1:], " ")
	}

	// Return the greeting. Starting with the line number, followed by the greeting to our subject.
	return fmt.Sprintf("%d. Hello, %s!", sourceLine, subject), nil
}


func main() {
	// Register the HelloParser, which is a ParseFunc
	yoink.RegisterParserFunc("hello", HelloParser)

	// Open the root file
	name := "./data/hello.txt"
	file, err := os.Open(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	defer file.Close()

	// Resolve .hello commands in the root file
	txt, err := yoink.Parse(context.Background(), file, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	fmt.Println(txt)

}
```

Running the program yields the following result:
```
1. Line one.
2. Hello, from ./data/hello.txt!
3. Line three.
4. Hello, Tedla Brandsema!
```

## Stateful ([example](https://github.com/tedla-brandsema/examples/tree/main/yoink/5_stateful))

If you need shared state between invocations of your parser, you need to implement `yoink.Parser`.

Let's create a parser that counts how many times it has been invoked. 

First, we need to create our parser:
```go
type CountParser struct {
	mut   sync.Mutex
	count int
}

func (p *CountParser) Parse(fileName string, lineNumber int, inputLine string) (string, error) {
	// Since we share state over multiple goroutines, we need to guard against possible race conditions
	p.mut.Lock()
	defer p.mut.Unlock()

	// Increment the counter
	p.count++

	// Return the invocation count
	return fmt.Sprintf("Command %q has been invoked %d times", strings.Fields(inputLine)[0], p.count), nil
}
```


After that, we need to register an instance of our parser:
```go
	yoink.RegisterParser("count", &CountParser{})
```


Then we can parse a file containing `.count` commands, with the full example below:
```go
package main

import (
	"context"
	"fmt"
	"github.com/tedla-brandsema/yoink"
	"os"
	"strings"
	"sync"
)

type CountParser struct {
	mut   sync.Mutex
	count int
}

func (p *CountParser) Parse(fileName string, lineNumber int, inputLine string) (string, error) {
	// Since we share state over multiple goroutines, we need to guard against possible race conditions
	p.mut.Lock()
	defer p.mut.Unlock()

	// Increment the counter
	p.count++

	// Return the invocation count
	return fmt.Sprintf("Command %q has been invoked %d times", strings.Fields(inputLine)[0], p.count), nil
}


func main() {
	// Register an instance of CountParser
	yoink.RegisterParser("count", &CountParser{})

	// Open the root file
	name := "./data/count.txt"
	file, err := os.Open(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	defer file.Close()

	// Resolve .count commands in the root file
	txt, err := yoink.Parse(context.Background(), file, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	fmt.Println(txt)
}
```
A possible result from running this example is shown below. It should immediately become clear that sharing state might 
not yield the desired results. Here we see that evidence that the order in which the goroutines are started does not 
guarantee the order in which they are returned.
```
Command ".count" has been invoked 3 times
Command ".count" has been invoked 4 times
Command ".count" has been invoked 1 times
Command ".count" has been invoked 7 times
Command ".count" has been invoked 5 times
Command ".count" has been invoked 12 times
Command ".count" has been invoked 2 times
Command ".count" has been invoked 6 times
Command ".count" has been invoked 8 times
Command ".count" has been invoked 9 times
Command ".count" has been invoked 10 times
Command ".count" has been invoked 11 times
Command ".count" has been invoked 13 times
Command ".count" has been invoked 14 times
Command ".count" has been invoked 17 times
Command ".count" has been invoked 19 times
Command ".count" has been invoked 15 times
Command ".count" has been invoked 16 times
Command ".count" has been invoked 18 times
```

So be cautious when choosing a stateful over a stateless parser. 

## Considerations When Working With Local Files

Since _Yoink_ parses commands concurrently, you need to take the following considerations into account when creating your own parser:

* Accessing the same file concurrently is generally considered safe _as long as you're only reading_.
  This means that functions like:`os.ReadFile`, `os.Open` and `os.OpenFile(name, O_RDONLY, 0)` are safe to use in your parser.
* Since each call to the functions mentioned above returns its own _file descriptor_, there is a change that you might
  hit the file descriptor limit (`ulimit -n`). This is unlikely to happen unless scaled up to thousands of goroutines.
* Hammering the I/O subsystem is a possibility when reading repeatedly from disk, but unlikely to happen.
* Using `bufio.Reader` or similar on top of `*os.File` in multiple goroutines is **NOT** safe, if they share the same `*os.File`.
* If you do decide to modify data on disk from inside your parser, race conditions might occur. You need to manage this yourself.

# Yoink On The Commandline

Install the versioned CLI directly:

```sh
go install github.com/tedla-brandsema/yoink/cmd/yoink@v0.2.0
```

The CLI returns nonzero on parsing or output errors. It composes the input before
opening an output file, so parsing failures leave an existing output file intact.

Select inclusion depth with `-depth` before the input filename:

```sh
yoink document.md             # flat, unchanged default
yoink -depth 1 document.md     # explicit flat behavior
yoink -depth 2 document.md     # two inclusion levels
yoink -depth 5 document.md     # up to five levels
yoink -depth all document.md   # unlimited depth within shared budgets
yoink -depth all -o composed.md document.md
cat document.md | yoink -depth 2 -o composed.md
yoink -depth all testdata/recursive/grand-vision.md
```

Depth accepts positive decimal integers or exactly `all`. Zero, signed values,
other strings, missing values, and integer overflow are errors. The CLI uses the
library's default resource budgets for recursive requests. For stdin, local paths
resolve from the current directory; file input resolves them from its source file.


## Build From Scratch

Prerequisites:
1. Go 1.24.4 or newer
2. git 2+

Clone the repository:
```
git clone https://github.com/tedla-brandsema/yoink.git
```

Change to the newly cloned `yoink` directory:
```
cd yoink
```

Next, build the Yoink commandline tool:
```
go build -o ./yoink ./cmd/yoink/
```

You should now see a `yoink` executable in your directory:
```
ls -alh | grep -w yoink
-rwxr-xr-x. 1 ted ted 8.4M Jul 22 17:45 yoink
```

Test that the executable works by typing:
```
 ./yoink -h
```

You should see an output similar to the following:
```
Usage:
  yoink [options] <inputFile>

  yoink <inputFile>                  Reads from file, writes to stdout
  yoink -depth 2 <inputFile>         Expands two inclusion levels
  yoink -depth all <inputFile>       Expands recursively within resource budgets
  yoink -o <outputFile>              Reads from stdin, writes to file
  yoink -o <outputFile> <inputFile>  Reads from file, writes to file
  cat file | yoink                   Pipe input, writes to stdout
  cat file | yoink -o <outputFile>   Pipe input, writes to file
  yoink < file                       Redirect input, writes to stdout
  yoink -o <outputFile> < file       Redirect input, writes to file

Options:
  -depth string
        Inclusion depth: a positive integer or all (default flat) (default "1")
  -o string
        Output file (defaults to stdout when omitted)
```


To be able to use `yoink` from any location, make sure to add the `yoink` executable to the `PATH` environment variable:
```
export PATH="/path/to/yoink/executable/file:$PATH"
```

Where `/path/to/app/executable/file/directory` should be replaced with the final destination of the `yoink` executable.

You can also opt to move the `yoink` executable to a location that is already added to the `PATH` environment variable, 
like `$HOME/bin` or `$HOME/.local/bin`.
