_Yoink_ is a stripped-down version of the [GO present tool](https://pkg.go.dev/golang.org/x/tools/present). _Yoink_ is 
built to include files (or parts thereof) into another file, concurrently. Those files can be included locally or over http(s).

You can extend _Yoink_ by creating your own command by implementing the `yoink.ParserFunc`
and then registering it with `yoink.Register`.

You can use _Yoink_ as a library or as a commandline tool. See [TODO: heading] for instructions on how to use _Yoink_ on the 
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

## Considerations For Working With Local Files

_Yoink_ parses commands concurrently. When it comes to accessing the same file through multiple `.yoink` commands,
this is generally considered safe _if you're only reading_. This means that functions like: `os.ReadFile`, `os.Open` and 
`os.OpenFile(name, O_RDONLY, 0)` are considered safe, since each call returns its own _file descriptor_. One caveat here 
is that you can hit the file descriptor limit (`ulimit -n`), but this is unlikely to happen unless scaled up to thousands 
of goroutines.

Other considerations:
* Race conditions when modifying data.
* Hammering the I/O subsystem when reading repeatedly from disk.

_Yoink_ builtin commands take this into consideration,
but if you were to create your own parser, you need to take the above into consideration. 

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

# Regular Expressions

Yoinks most useful and powerful feature is the ability to add a regular expression to the end of a `.yoink` command.

In our previous examples, we only included entire files into our _base_ or _root_ file. Regular expressions, however, 
allow the user to target a specific region or excerpt from a file.


