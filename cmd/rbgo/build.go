package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/aot"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/parser"
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
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o" && i+1 < len(args):
			out, i = args[i+1], i+1
		case file == "" && !strings.HasPrefix(args[i], "-"):
			file = args[i]
		default:
			fatal("usage: rbgo build [-o out] <file.rb>")
		}
	}
	if file == "" {
		fatal("usage: rbgo build [-o out] <file.rb>")
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

	content, keys, ok := aot.CompileProgram(iseq)

	root, err := moduleRoot()
	if err != nil {
		fatal("rbgo build: %v", err)
	}

	goArgs := []string{"build", "-o", out}
	if ok {
		overlay, cleanup, err := writeOverlay(root, content)
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

	if ok {
		fmt.Fprintf(os.Stderr, "rbgo build: %s (%d method(s) AOT-compiled: %s)\n",
			out, len(keys), strings.Join(keys, ", "))
	} else {
		fmt.Fprintf(os.Stderr, "rbgo build: %s (no methods AOT-compiled; plain interpreter)\n", out)
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

// writeOverlay writes content to a temp file and returns the path of an overlay
// JSON that injects it into internal/vm as a new package file, plus a cleanup.
func writeOverlay(root, content string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "rbgo-aot")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }

	genGo := filepath.Join(dir, "aot_build_gen.go")
	if err := os.WriteFile(genGo, []byte(content), 0o644); err != nil {
		cleanup()
		return "", nil, err
	}

	// Map a not-on-disk path inside internal/vm to the generated file, so the Go
	// tool compiles it as part of package vm without touching the source tree.
	target := filepath.Join(root, "internal", "vm", "aot_build_gen.go")
	overlay := struct{ Replace map[string]string }{Replace: map[string]string{target: genGo}}
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
