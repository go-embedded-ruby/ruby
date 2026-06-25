//go:build ignore

// Command frontend is the front-end-only conformance checker for the heavyweight
// (Rails / Puppet) parse-conformance sweep.
//
// It runs each input file through rbgo's front-end — parser.Parse followed by
// compiler.Compile — WITHOUT executing the program on the VM. A file that parses
// and compiles but would fail at runtime (missing stdlib, missing method, etc.)
// still counts as a front-end success: this isolates parser/compiler gaps from
// runtime gaps, which is exactly what drives fixes in go-ruby-parser.
//
// Usage:
//
//	frontend -list files.txt          # read newline-separated paths, emit TSV
//	frontend <file.rb> [<file.rb>...]  # check the given files
//
// Output is one tab-separated record per file:
//
//	OK    <path>
//	PARSE <path>\t<one-line error>     # parser.Parse failed
//	COMP  <path>\t<one-line error>     # compiler.Compile failed
//
// It never panics out: a panic in the front-end is reported as a PANIC record so
// one pathological file cannot abort the whole sweep.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

func main() {
	args := os.Args[1:]
	var files []string
	if len(args) >= 2 && args[0] == "-list" {
		f, err := os.Open(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "frontend: %v\n", err)
			os.Exit(2)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				files = append(files, line)
			}
		}
		f.Close()
	} else {
		files = args
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for _, path := range files {
		kind, msg := check(path)
		if msg == "" {
			fmt.Fprintf(out, "%s\t%s\n", kind, path)
		} else {
			fmt.Fprintf(out, "%s\t%s\t%s\n", kind, path, oneLine(msg))
		}
	}
}

// check returns the result kind ("OK"/"PARSE"/"COMP"/"PANIC"/"READ") and an
// error message (empty for OK). It recovers from panics in the front-end.
func check(path string) (kind, msg string) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "READ", err.Error()
	}
	defer func() {
		if r := recover(); r != nil {
			kind, msg = "PANIC", fmt.Sprint(r)
		}
	}()
	prog, err := parser.Parse(string(src))
	if err != nil {
		return "PARSE", err.Error()
	}
	if _, err := compiler.Compile(prog); err != nil {
		return "COMP", err.Error()
	}
	return "OK", ""
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
