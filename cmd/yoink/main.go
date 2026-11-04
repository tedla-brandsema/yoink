package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/tedla-brandsema/yoink"
)

func main() {
	outputFile := flag.String("o", "", "Output file (defaults to stdout when omitted)")
	depthFlag := flag.String("depth", "1", "Inclusion depth: a positive integer or all (default flat)")
	flag.Usage = usage
	flag.Parse()
	depth, err := parseDepth(*depthFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid depth: %v\n", err)
		os.Exit(2)
	}

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
			fmt.Fprintln(os.Stderr, "yoink: no input provided (expected piped input or file argument)")
			os.Exit(1)
		}
	}

	result, err := yoink.ParseWithOptions(ctx, input, inputName, yoink.Options{MaxDepth: depth})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Parsing error: %v\n", err)
		os.Exit(1)
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

	if _, err := fmt.Fprintln(output, result); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
		os.Exit(1)
	}
}

func parseDepth(value string) (int, error) {
	if value == "all" {
		return -1, nil
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("expected a positive decimal integer or all, got %q", value)
		}
	}
	depth, err := strconv.Atoi(value)
	if err != nil || depth < 1 {
		return 0, fmt.Errorf("expected a positive decimal integer or all, got %q", value)
	}
	return depth, nil
}

func usage() {
	w := tabwriter.NewWriter(os.Stderr, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "\tyoink [options] <inputFile>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "\tyoink <inputFile>\tReads from file, writes to stdout")
	fmt.Fprintln(w, "\tyoink -depth 2 <inputFile>\tExpands two inclusion levels")
	fmt.Fprintln(w, "\tyoink -depth all <inputFile>\tExpands recursively within resource budgets")
	fmt.Fprintln(w, "\tyoink -o <outputFile>\tReads from stdin, writes to file")
	fmt.Fprintln(w, "\tyoink -o <outputFile> <inputFile>\tReads from file, writes to file")
	fmt.Fprintln(w, "\tcat file | yoink\tPipe input, writes to stdout")
	fmt.Fprintln(w, "\tcat file | yoink -o <outputFile>\tPipe input, writes to file")
	fmt.Fprintln(w, "\tyoink < file\tRedirect input, writes to stdout")
	fmt.Fprintln(w, "\tyoink -o <outputFile> < file\tRedirect input, writes to file")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Options:")
	w.Flush()
	flag.PrintDefaults()
}
