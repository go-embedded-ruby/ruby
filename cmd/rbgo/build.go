//go:build !rbgo_closed

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/aot"
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// buildCmd implements `rbgo build [-o out] <file.rb>`: it AOT-compiles the
// program's lowerable top-level methods to Go (internal/aot.CompileProgram) and
// links them into a specialised rbgo binary via `go build -overlay`, so running
// `out <file.rb>` dispatches those methods to native code instead of the
// interpreter. Methods the compiler cannot lower stay interpreted; if none can
// be lowered, a plain rbgo binary is produced.
//
// The binary is specialised for this program: its baked-in method bodies are
// registered globally, so run it on the program it was built from.
func buildCmd(args []string) {
	out := "a.out"
	var file string
	closed := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o" && i+1 < len(args):
			out, i = args[i+1], i+1
		case args[i] == "--closed" || args[i] == "-closed":
			closed = true
		case file == "" && !strings.HasPrefix(args[i], "-"):
			file = args[i]
		default:
			fatal("usage: rbgo build [-o out] [--closed] <file.rb>")
		}
	}
	if file == "" {
		fatal("usage: rbgo build [-o out] [--closed] <file.rb>")
	}

	src, err := os.ReadFile(file)
	if err != nil {
		fatal("rbgo build: %v", err)
	}
	prog, err := parser.Parse(string(src))
	if err != nil {
		fatal("rbgo build: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		fatal("rbgo build: %v", err)
	}

	root, err := moduleRoot()
	if err != nil {
		fatal("rbgo build: %v", err)
	}

	// Each overlay entry maps an in-tree path the Go tool will compile to the
	// generated source backing it (the source tree is never touched).
	overlays := map[string]string{}

	content, keys, aotOK := aot.CompileProgram(iseq)
	if aotOK {
		overlays[filepath.Join(root, "internal", "vm", "aot_build_gen.go")] = content
	}

	goArgs := []string{"build", "-o", out}
	if closed {
		// Closed-world: bake the program in as bytecode (embeddedProgram) and
		// build with rbgo_closed, which drops the lexer/parser/compiler.
		prog := aot.FreezeISeq(iseq, "main", "embeddedProgram", "rbgo_closed")
		overlays[filepath.Join(root, "cmd", "rbgo", "closed_program_gen.go")] = prog
		goArgs = append(goArgs, "-tags", "rbgo_closed")
	}

	if len(overlays) > 0 {
		overlay, cleanup, err := writeOverlay(root, overlays)
		if err != nil {
			fatal("rbgo build: %v", err)
		}
		defer cleanup()
		goArgs = append(goArgs, "-overlay", overlay)
	}
	goArgs = append(goArgs, "./cmd/rbgo")

	cmd := exec.Command("go", goArgs...)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fatal("rbgo build: go build failed: %v", err)
	}

	report(out, keys, aotOK, closed, iseq)
}

// report prints what the build produced: the AOT-compiled methods, and — in
// closed-world mode — that the front-end was dropped, the resulting binary size,
// and a warning for any front-end-dependent call (eval/require/…) the program
// makes, since those raise in the closed binary.
func report(out string, keys []string, aotOK, closed bool, iseq *bytecode.ISeq) {
	if aotOK {
		fmt.Fprintf(os.Stderr, "rbgo build: %s (%d method(s) AOT-compiled: %s)\n",
			out, len(keys), strings.Join(keys, ", "))
	} else {
		fmt.Fprintf(os.Stderr, "rbgo build: %s (no methods AOT-compiled; interpreter core)\n", out)
	}
	if !closed {
		return
	}
	fmt.Fprintf(os.Stderr, "rbgo build: closed-world — front-end dropped (no lexer/parser/compiler linked)\n")
	if fi, err := os.Stat(out); err == nil {
		fmt.Fprintf(os.Stderr, "rbgo build: binary size %.1f MiB\n", float64(fi.Size())/(1<<20))
	}
	if uses := aot.FrontendUses(iseq); len(uses) > 0 {
		fmt.Fprintf(os.Stderr, "rbgo build: warning — these front-end calls raise NotImplementedError at runtime: %s\n",
			strings.Join(uses, ", "))
	}
}

// moduleRoot returns the directory of the module's go.mod.
func moduleRoot() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", err
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == "/dev/null" {
		return "", fmt.Errorf("not inside a Go module")
	}
	return filepath.Dir(gomod), nil
}

// writeOverlay writes each generated source to a temp file and returns the path
// of an overlay JSON mapping the in-tree target paths to them, plus a cleanup.
// The Go tool then compiles the generated files in place of (here: in addition
// to) the named paths, without ever touching the source tree.
func writeOverlay(root string, files map[string]string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "rbgo-build")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }

	replace := make(map[string]string, len(files))
	i := 0
	for target, content := range files {
		genGo := filepath.Join(dir, fmt.Sprintf("gen%d.go", i))
		i++
		if err := os.WriteFile(genGo, []byte(content), 0o644); err != nil {
			cleanup()
			return "", nil, err
		}
		replace[target] = genGo
	}

	overlay := struct{ Replace map[string]string }{Replace: replace}
	data, err := json.Marshal(overlay)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, data, 0o644); err != nil {
		cleanup()
		return "", nil, err
	}
	return overlayPath, cleanup, nil
}
