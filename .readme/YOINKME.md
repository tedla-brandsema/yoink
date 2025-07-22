_Yoink_ is a stripped-down version of the [GO present tool](https://pkg.go.dev/golang.org/x/tools/present). _Yoink_ is 
built to include files (or parts thereof) into another file, concurrently. Those files can be included locally or over http(s).

You can extend _Yoink_ by [creating your own command](#creating-your-own-command). 

_Yoink_ is  available as a library or as a commandline tool. Below you can find instructions on how to build and use 
[Yoink On The Commandline](#yoink-on-the-commandline).

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

Then, import _Yoink_ in your application:

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

To let _Yoink_ resolve this, we first need to open our root file and then pass it to `yoink.Parse` in the form of an
`io.Reader`.

```go
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/1_local/main.go
```


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
All commands that are registered with _Yoink_ can be called from within the file that is parsed._

# The Address Argument

Yoinks most useful and powerful feature is the ability to target a specific region by adding an address at the end 
of a `.yoink` command.

The address syntax is similar in its simplest form to that of ed, but comes from sam and is more general. See 
[Table II](https://plan9.io/sys/doc/sam/sam.html) of _The Text Editor sam_ manual for full details. 
The displayed block is always rounded out to a full line at both ends.

In our previous examples, we only included entire files into our _base_ file. With the help of the address argument, 
however, we can target a specific region or excerpt from a file.


Consider the following _base_ file. You can find the full example
[here](https://github.com/tedla-brandsema/examples/tree/main/yoink/3_address).

```
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/3_address/data/sonnet-18-jumbled.txt
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
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/3_address/data/sonnet-18-quatrains.txt
```

Running our jumbled _base_ file with _Yoink_, yields the following result.
```
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/3_address/data/result.txt
```

So we succeeded in jumbling the output using addresses, but the address demarcations ended up in the resulting output. 

Luckily we have a way to circumvent this; any line in the program that ends with the four characters `OMIT` is deleted 
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

And our `sonnet-18-quatrains.txt` file to:
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

The result is still jumbled, but no longer contains the address demarcations.


# Creating Your Own Command

You create your own command by first creating a parser and then registering the parser with its accompanying command string.

There are two ways of creating your own parser:
1. Implement `yoink.ParseFunc`.
2. Implement `yoink.Parser`.

The former is stateless and therefore the preferred method for creating your own command. The latter provides a manner 
to introduce state.

## Stateless

Let's start out by creating a stateless parser by implementing the `yoink.ParseFunc`. You can find the full example 
[here](https://github.com/tedla-brandsema/examples/tree/main/yoink/4_stateless).


```go
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/4_stateless/main.go /START ParseFunc OMIT/,/END ParseFunc OMIT/
```

We also need to register our `HelloParser` with _Yoink_:
```go
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/4_stateless/main.go /START RegisterParserFunc OMIT/,/END RegisterParserFunc OMIT/
```

After that we can parse files containing `.hello` commands like the one below:
```
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/4_stateless/data/hello.txt
```

The full stateless example looks like this:
```go
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/4_stateless/main.go 
```

## Stateful

If you need shared state between invocations of your parser, you need to implement `yoink.Parser`.

Let's create a parser that counts how many times it has been invoked. You can find the full example 
[here](https://github.com/tedla-brandsema/examples/tree/main/yoink/5_stateful)

First, we need to create our parser:

```go
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/5_stateful/main.go /START Parser OMIT/,/END Parser OMIT/
```


After that, we need to register an instance of our parser:
```go
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/5_stateful/main.go /START RegisterParser OMIT/,/END RegisterParser OMIT/
```


After that we can parse a file containing `.count` commands, with the full example below:
```go
.yoink https://raw.githubusercontent.com/tedla-brandsema/examples/refs/heads/main/yoink/5_stateful/main.go
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

## Build From Scratch

Prerequisites:
1. GO 1.24+
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
  yoink -o <outputFile>              Reads from stdin, writes to file
  yoink -o <outputFile> <inputFile>  Reads from file, writes to file
  cat file | yoink                   Pipe input, writes to stdout
  cat file | yoink -o <outputFile>   Pipe input, writes to file
  yoink < file                       Redirect input, writes to stdout
  yoink -o <outputFile> < file       Redirect input, writes to file

Options:
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