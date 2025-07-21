_Yoink_ is a stripped-down version of the [GO present tool](https://pkg.go.dev/golang.org/x/tools/present). _Yoink_ is 
built to include files (or parts thereof) into another file, concurrently. Those files can be included locally or over http(s).

You can extend _Yoink_ by creating your own command by implementing the `yoink.ParserFunc`
and then registering it with `yoink.Register`.

_Yoink_ is  available as a library or as a commandline tool. See [TODO: heading] for instructions on how to use _Yoink_ on the 
commandline.

NOTE:
_Yoink_ is a line-based, flat-include library. It is not suited for including data in hierarchical/nested structures.

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

Use `go get` to install the latest version
of the library.
```
go get -u github.com/tedla-brandsema/yoink@latest
```

Then, import Yoink in your application:

```go
import "github.com/tedla-brandsema/yoink"
```

# Working With Local Files

You can include local files by adding the `.yoink` command at the start of a line, followed by the name of
the file to include.

Below is an example file that contains two such commands. You can find the full example 
[here](https://github.com/tedla-brandsema/examples/tree/main/yoink/1_local).

```
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/1_local/data/sonnet-18.txt
```

One command pointing to `sonnet-18-quatrains.txt` and the other to `sonnet-18-rhyming-couplet.txt`.

To let Yoink resolve this, we first need to open our root file and then pass it to `yoink.Parse` in the form of an
`io.Reader`.

```go
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/1_local/main.go
```

## Considerations When Working With Local Files

_Yoink_ parses commands concurrently. If you want to create your own parser, you need to take the following 
considerations into account:

* Accessing the same file concurrently is generally considered safe _as long as you're only reading_. 
This means that functions like:`os.ReadFile`, `os.Open` and `os.OpenFile(name, O_RDONLY, 0)` are safe to use in your parser.
* Since each call to the functions mentioned above returns its own _file descriptor_, there is a change that you might 
hit the file descriptor limit (`ulimit -n`). This is unlikely to happen unless scaled up to thousands of goroutines.
* Hammering the I/O subsystem is a possibility when reading repeatedly from disk, but unlikely to happen.
* Using `bufio.Reader` or similar on top of `*os.File` in multiple goroutines is **NOT** safe, if they share the same `*os.File`.
* If you do decide to modify data on disk from inside your parser, race conditions might occur. You need to manage that yourself.

# Working With Remote Files

Including remote files works much the same as working with local files; you add the `.yoink` command to the start of a line,
but instead of adding the name of the file to include, you add the URL of the file to include.

In the example below, there are two `.yoink` commands pointing to URLs with raw text data. You can find the full example
[here](https://github.com/tedla-brandsema/examples/tree/main/yoink/2_remote).

```
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/2_remote/data/sonnet-18-remote.txt
```

Parsing of a file that contains a `.yoink` command that points to a URL is exactly the same as parsing a file with `.yoink`
commands that point to local files.

```go
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/2_remote/main.go
```

**Q: _Can you add local and remote `.yoink` commands to the same file?_**  
**A:** _Absolutely you can. In fact, this is a crucial concept to grasp; every command is parsed individually. This also 
goes for commands you might implement yourself using `yoink.ParserFunc` and then register using `yoink.Register`. 
All commands that are registered with Yoink can be called from within the file that is parsed._

# The address argument

Yoinks most useful and powerful feature is the ability to target a specific region by adding an address at the end 
of a `.yoink` command.

The address syntax is similar in its simplest form to that of ed, but comes from sam and is more general. See 
[Table II](https://plan9.io/sys/doc/sam/sam.html) for full details. 
The displayed block is always rounded out to a full line at both ends.

In our previous examples, we only included entire files into our _base_ file. With the help of addresses, however, 
we can target a specific region or excerpt from a file.


Consider the following _base_ file. You can find the full example
[here](https://github.com/tedla-brandsema/examples/tree/main/yoink/3_address).

```
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/3_address/data/sonnet-18-jumbled.txt
```

Here you can see that all `.yoink` commands that point to `sonnet-18-quatrains.txt` have a second argument; an address.

For example, address of the first `.yoink` command reads as follows:
```
/START stanza-2/,/END stanza-2/
```

If you take a look at [Table II](https://plan9.io/sys/doc/sam/sam.html), we see that we have a regular expression 
`/START stanza-2/` followed by the address mark `,` and finally we have a second regular expression 
`/END stanza-2/`. The regular expressions here are just literal matches, but note that you can use regex syntax.

The regular expressions in our _base_ file correspond to addresses in our target file: sonnet-18-quatrains.txt. See below:
```
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/3_address/data/sonnet-18-quatrains.txt
```

Running our jumbled _base_ file with Yoink, yields the following result.
```
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/3_address/data/result.txt
```

So we succeeded in jumbling the output using addresses, but the address demarcations ended up in the resulting output. 

Luckily we have a way to circumvent this; any line in the program that ends with the four characters `OMIT`is deleted 
from the source before inclusion.

So if we were to change our _base_ file to:
```
Sonnet 18: Shall I compare thee to a summer’s day?
By William Shakespeare

.yoink sonnet-18-quatrains.txt /START stanza-2 OMIT/,/END stanza-2 OMIT/
.yoink sonnet-18-rhyming-couplet.txt
.yoink sonnet-18-quatrains.txt /START stanza-3 OMIT/,/END stanza-3 OMIT/
.yoink sonnet-18-quatrains.txt /START stanza-1 OMIT/,/END stanza-1 OMIT/
```

And our sonnet-18-quatrains.txt file to:
```
#START stanza-1 OMIT
Shall I compare thee to a summer’s day?
Thou art more lovely and more temperate:
Rough winds do shake the darling buds of May,
And summer’s lease hath all too short a date;
#END stanza-1 OMIT
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

Still jumbled, but without our address demarcations.


