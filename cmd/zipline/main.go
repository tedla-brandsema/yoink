package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/tedla-brandsema/zipline"
)

func main() {
	outputFile := flag.String("o", "", "Output file (default: stdout)")
	flag.Usage = usage
	flag.Parse()

	ctx := context.Background()

	input := os.Stdin
	inputName := "stdin"

	if flag.NArg() > 0 { // flags
		file, err := os.Open(flag.Arg(0))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening input file: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()
		input = file
		inputName = flag.Arg(0)
	} else { // piped
		info, err := os.Stdin.Stat()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			os.Exit(1)
		}

		if (info.Mode() & os.ModeCharDevice) != 0 {
			// Stdin is a terminal - no piped input, no file provided
			fmt.Fprintln(os.Stderr, "zipline: no input provided (expected piped input or file argument)")
			os.Exit(1)
		}
	}

	output := os.Stdout
	if *outputFile != "" {
		file, err := os.Create(*outputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()
		output = file
	}

	result, err := zipline.Parse(ctx, input, inputName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Parsing error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(output, result)
}

func usage() {
	w := tabwriter.NewWriter(os.Stderr, 0, 4, 2, ' ', 0)
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "\tzipline [options] <inputFile>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "\tzipline <inputFile>\tReads from file, writes to stdout")
	fmt.Fprintln(os.Stderr, "\tzipline -o <outputFile>\tReads from stdin, writes to file")
	fmt.Fprintln(os.Stderr, "\tzipline -o <outputFile> <inputFile>\tReads from file, writes to file")
	fmt.Fprintln(os.Stderr, "\tcat file | zipline\tPipe input, writes to stdout")
	fmt.Fprintln(os.Stderr, "\tcat file | zipline -o <outputFile>\tPipe input, writes to file")
	fmt.Fprintln(os.Stderr, "\tzipline < file\tRedirect input, writes to stdout")
	fmt.Fprintln(os.Stderr, "\tzipline -o <outputFile> < file\tRedirect input, writes to file")
	flag.PrintDefaults()
	w.Flush()
}
