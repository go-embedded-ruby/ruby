package vm

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// preloadedFeatures are already loaded at startup (like MRI 4.x's preloaded
// gems), so require returns false. rubygems is preloaded by default in MRI, and
// the Gem module lives in the prelude, so its constants are usable with no
// require at all (Puppet references Gem::Version before requiring rubygems).
var preloadedFeatures = map[string]bool{"set": true, "rubygems": true}

// providedFeatures are standard-library names the VM supplies — either as
// built-in Go classes or as prelude pure-Ruby modules. The file never needs
// loading, but require still returns true on the first call and false
// afterwards, matching a normal gem load. English is listed under its MRI
// filename ("English", capital E); the lookup is case-sensitive like MRI's.
var providedFeatures = map[string]bool{
	"date": true, "time": true, "bigdecimal": true, "bag": true,
	"base64": true, "digest": true, "json": true, "zlib": true,
	"digest/md5": true, "digest/sha1": true, "digest/sha2": true,
	"digest/rmd160": true, "digest/bubblebabble": true,
	"stringio": true, "securerandom": true,
	"English": true, "ostruct": true, "benchmark": true,
	"forwardable": true, "delegate": true, "pathname": true, "uri": true,
	"tmpdir": true, "openssl": true, "timeout": true, "rbconfig": true,
	"yaml": true, "fileutils": true, "getoptlong": true, "etc": true,
	"concurrent": true, "syslog": true, "cgi": true, "monitor": true,
	"observer": true,
	"net/http": true, "net/https": true, "resolv": true, "singleton": true,
	"optparse": true, "ripper": true, "erb": true, "find": true,
	"tempfile": true, "open3": true,
	"strscan": true, "fiber": true, "objspace": true, "csv": true,
	"shellwords": true, "prime": true, "tsort": true, "abbrev": true,
	"did_you_mean": true, "cmath": true, "matrix": true, "ipaddr": true,
	"unicode_normalize": true, "scanf": true, "prettyprint": true,
	"rexml": true, "rexml/document": true,
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
	// Kernel#load re-executes a file every call (unlike require, which loads once)
	// and returns true. Exposed as a private instance method (a bare `load`) and as
	// a module method on Kernel (`Kernel.load`, the form Puppet's autoloader uses).
	loadFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.doLoad(requireName(vm, args))
	}
	vm.cObject.define("load", loadFn)
	if kernel, ok := vm.consts["Kernel"].(*RClass); ok {
		kernel.smethods["load"] = &Method{name: "load", owner: kernel, native: loadFn}
	}
}

// doLoad implements Kernel#load: it locates name (an explicit path, or a file
// searched on $LOAD_PATH and the CWD) and executes it, always re-running it
// regardless of any prior load/require. The .rb suffix is NOT auto-appended (MRI
// loads the literal name), and a missing file raises LoadError.
func (vm *VM) doLoad(name string) object.Value {
	for _, cand := range vm.requireCandidates(name, false) {
		src, err := os.ReadFile(cand)
		if err != nil {
			continue // not found / unreadable — try the next candidate
		}
		abs, _ := filepath.Abs(cand)
		iseq, cerr := parseCompileFn(string(src))
		if cerr != nil {
			return raise("SyntaxError", "%s", cerr.Error())
		}
		iseq.Name = abs
		setISeqFile(iseq, abs)
		vm.requireDirs = append(vm.requireDirs, filepath.Dir(abs))
		defer func() { vm.requireDirs = vm.requireDirs[:len(vm.requireDirs)-1] }()
		vm.exec(iseq, vm.main, nil, vm.cObject, "", nil, nil, nil, nil)
		return object.Bool(true)
	}
	return raise("LoadError", "cannot load such file -- %s", name)
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
	// Features the VM provides as built-in classes need no file.
	if !relative {
		if preloadedFeatures[name] {
			return object.Bool(false) // already loaded at startup
		}
		if providedFeatures[name] {
			key := "feature:" + name
			if vm.loaded[key] {
				return object.Bool(false)
			}
			vm.loaded[key] = true
			// Some features install their Ruby surface lazily on first require
			// (MRI-style), not eagerly at startup — e.g. shellwords creates the
			// Shellwords module and the String/Array core extensions here.
			if hook := vm.featureHooks[name]; hook != nil {
				hook()
			}
			return object.Bool(true)
		}
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
		iseq, cerr := parseCompileFn(string(src))
		if cerr != nil {
			return raise("SyntaxError", "%s", cerr.Error())
		}
		iseq.Name = abs
		// Stamp the file on this ISeq and every nested child so __FILE__ in a method
		// reports the file the method was defined in, regardless of where it is
		// called from.
		setISeqFile(iseq, abs)
		vm.loaded[abs] = true
		// Push the file's directory so a nested require_relative resolves against it.
		vm.requireDirs = append(vm.requireDirs, filepath.Dir(abs))
		defer func() { vm.requireDirs = vm.requireDirs[:len(vm.requireDirs)-1] }()
		vm.exec(iseq, vm.main, nil, vm.cObject, "", nil, nil, nil, nil)
		return object.Bool(true)
	}
	return raise("LoadError", "cannot load such file -- %s", name)
}

// setISeqFile stamps path onto iseq and all of its nested children, so a method
// or block body compiled from this file reports the file via __FILE__ even when
// invoked from another file. Already-stamped children are left alone (an ISeq is
// only ever loaded from one file).
func setISeqFile(iseq *bytecode.ISeq, path string) {
	if iseq == nil || iseq.File == path {
		return
	}
	iseq.File = path
	for _, c := range iseq.Children {
		setISeqFile(c, path)
	}
}

// requireCandidates lists the paths to try for file. require_relative resolves
// against the requiring file's directory; a plain require searches that
// directory, the process CWD, then each $LOAD_PATH entry; an absolute path is
// used as-is.
func (vm *VM) requireCandidates(file string, relative bool) []string {
	switch {
	case relative:
		// require_relative resolves against the directory of the file where the call
		// is written — the executing ISeq's file — so a require_relative inside a
		// method works even when that method is called from another file. Fall back
		// to the require stack's directory when no file is stamped (e.g. a -e script).
		if f := vm.currentFile(); f != "" {
			return []string{filepath.Join(filepath.Dir(f), file)}
		}
		return []string{filepath.Join(vm.currentDir(), file)}
	case filepath.IsAbs(file):
		return []string{file}
	default:
		cands := []string{filepath.Join(vm.currentDir(), file), file}
		for _, dir := range vm.loadPathDirs() {
			cands = append(cands, filepath.Join(dir, file))
		}
		return cands
	}
}

// loadPathDirs returns the directory strings currently in $LOAD_PATH. Non-string
// entries are skipped (MRI coerces them, but the embedded load path holds plain
// strings).
func (vm *VM) loadPathDirs() []string {
	lp, ok := vm.globals["$LOAD_PATH"].(*object.Array)
	if !ok {
		return nil
	}
	dirs := make([]string, 0, len(lp.Elems))
	for _, e := range lp.Elems {
		if s, ok := e.(*object.String); ok {
			dirs = append(dirs, string(s.B))
		}
	}
	return dirs
}

// currentDir is the directory of the file currently being required, falling back
// to the script directory and then the process CWD.
func (vm *VM) currentDir() string {
	if n := len(vm.requireDirs); n > 0 {
		return vm.requireDirs[n-1]
	}
	return "."
}
