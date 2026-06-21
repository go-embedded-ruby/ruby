package vm

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// builtinFeatures are standard-library names already provided by the embedded VM
// (as built-in classes), so `require "name"` is satisfied without a file.
var builtinFeatures = map[string]bool{
	"set": true, "date": true, "time": true, "bigdecimal": true, "bag": true,
}

// registerRequire installs Kernel#require and #require_relative — the runtime
// loader half of the embedded front-end (the eval half is in eval.go). A file is
// parsed, compiled and run once at the top level; a second require returns false.
func (vm *VM) registerRequire() {
	vm.cObject.define("require", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.doRequire(requireName(vm, args), false)
	})
	vm.cObject.define("require_relative", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.doRequire(requireName(vm, args), true)
	})
}

// requireName extracts the String feature name, raising TypeError otherwise.
func requireName(vm *VM, args []object.Value) string {
	s, ok := args[0].(*object.String)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", vm.classOf(args[0]).name)
	}
	return string(s.B)
}

func (vm *VM) doRequire(name string, relative bool) object.Value {
	// A built-in feature (e.g. "set") is already provided by the VM — like the
	// default gems MRI 4.x preloads, so require returns false (already loaded).
	if !relative && builtinFeatures[name] {
		return object.Bool(false)
	}

	file := name
	if !strings.HasSuffix(file, ".rb") {
		file += ".rb"
	}
	// Try each candidate by reading it directly — the read is the existence test.
	for _, cand := range vm.requireCandidates(file, relative) {
		src, err := os.ReadFile(cand)
		if err != nil {
			continue // not found / unreadable — try the next candidate
		}
		abs, _ := filepath.Abs(cand)
		if vm.loaded[abs] {
			return object.Bool(false)
		}
		prog, perr := parser.Parse(string(src))
		if perr != nil {
			return raise("SyntaxError", "%s", perr.Error())
		}
		iseq, cerr := compiler.Compile(prog)
		if cerr != nil {
			return raise("SyntaxError", "%s", cerr.Error())
		}
		iseq.Name = abs
		vm.loaded[abs] = true
		// Push the file's directory so a nested require_relative resolves against it.
		vm.requireDirs = append(vm.requireDirs, filepath.Dir(abs))
		defer func() { vm.requireDirs = vm.requireDirs[:len(vm.requireDirs)-1] }()
		vm.exec(iseq, vm.main, nil, vm.cObject, "", nil, nil, nil)
		return object.Bool(true)
	}
	return raise("LoadError", "cannot load such file -- %s", name)
}

// requireCandidates lists the paths to try for file. require_relative resolves
// against the requiring file's directory; a plain require searches that
// directory and the process CWD; an absolute path is used as-is.
func (vm *VM) requireCandidates(file string, relative bool) []string {
	switch {
	case relative:
		return []string{filepath.Join(vm.currentDir(), file)}
	case filepath.IsAbs(file):
		return []string{file}
	default:
		return []string{filepath.Join(vm.currentDir(), file), file}
	}
}

// currentDir is the directory of the file currently being required, falling back
// to the script directory and then the process CWD.
func (vm *VM) currentDir() string {
	if n := len(vm.requireDirs); n > 0 {
		return vm.requireDirs[n-1]
	}
	return "."
}
