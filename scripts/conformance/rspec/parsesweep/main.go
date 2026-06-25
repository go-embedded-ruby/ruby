// Command parsesweep runs every .rb file under one or more roots through
// rbgo's front-end (parse + compile, NO execution) and reports an
// acceptance rate. It mirrors exactly what `rbgo run` does up to (but not
// including) vm.Run, so a file that "compiles" here is one rbgo can load.
//
// Output is a TSV stream on stdout (path<TAB>status<TAB>stage<TAB>message)
// plus a summary on stderr, so callers can post-process with awk/sort.
//
// Usage:
//
//	parsesweep <root> [<root>...]
//
// status is one of: ok | parse-error | compile-error
// stage  is one of: parse | compile | -
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

type result struct {
	path    string
	status  string
	stage   string
	message string
}

func sweepFile(path string) result {
	b, err := os.ReadFile(path)
	if err != nil {
		return result{path, "read-error", "-", err.Error()}
	}
	src := string(b)

	// Guard against panics in the front-end (parser/compiler are not
	// hardened against every malformed input); treat a panic as a reject
	// attributed to whichever stage was running.
	var res result
	stage := "parse"
	func() {
		defer func() {
			if r := recover(); r != nil {
				res = result{path, "panic", stage, oneLine(fmt.Sprint(r))}
			}
		}()
		prog, perr := parser.Parse(src)
		if perr != nil {
			res = result{path, "parse-error", "parse", oneLine(perr.Error())}
			return
		}
		stage = "compile"
		if _, cerr := compiler.Compile(prog); cerr != nil {
			res = result{path, "compile-error", "compile", oneLine(cerr.Error())}
			return
		}
		res = result{path, "ok", "-", ""}
	}()
	return res
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: parsesweep <root> [<root>...]")
		os.Exit(2)
	}

	var files []string
	for _, root := range os.Args[1:] {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".rb") {
				files = append(files, p)
			}
			return nil
		})
	}
	sort.Strings(files)

	counts := map[string]int{}
	for _, f := range files {
		r := sweepFile(f)
		counts[r.status]++
		fmt.Printf("%s\t%s\t%s\t%s\n", r.path, r.status, r.stage, r.message)
	}

	total := len(files)
	ok := counts["ok"]
	fmt.Fprintf(os.Stderr, "\n=== parsesweep summary ===\n")
	fmt.Fprintf(os.Stderr, "total .rb files : %d\n", total)
	statuses := make([]string, 0, len(counts))
	for s := range counts {
		statuses = append(statuses, s)
	}
	sort.Strings(statuses)
	for _, s := range statuses {
		fmt.Fprintf(os.Stderr, "  %-14s: %d\n", s, counts[s])
	}
	if total > 0 {
		fmt.Fprintf(os.Stderr, "acceptance (ok/total): %.1f%%\n", 100*float64(ok)/float64(total))
	}
}
