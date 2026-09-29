// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "path/filepath"

// --- the second path an ISeq carries ------------------------------------------
//
// MRI keeps TWO paths for every compiled unit, not one. iseq->body->location
// .pathobj is either a single String -- the file's path, when that IS its
// canonical name -- or a frozen two-element Array [path, realpath] when the two
// differ, and rb_iseq_path / rb_iseq_realpath read one element each
// (iseq.c-ruby_4_0:1468, vm_core.h-ruby_4_0:358 pathobj_realpath). The path APIs
// divide between them:
//
//	Thread::Backtrace::Location#path            -> path      (as spelled)
//	Thread::Backtrace::Location#absolute_path   -> realpath  (RESOLVED)
//	__FILE__, const_source_location,
//	Method#source_location, $LOADED_FEATURES    -> path      (as spelled)
//	__dir__                                     -> dirname(realpath)
//	require_relative's base                     -> dirname(realpath)
//
// rbgo carried only the first, so every API on the right-hand side answered the
// left-hand one. The registry here is the missing second field: the VM records a
// file's resolved path when it LOADS it, exactly where MRI computes an ISeq's
// realpath, and the reading APIs look it up by the spelling they hold.
//
// Recording at load time rather than resolving on demand is not an optimisation,
// it is the behaviour: ruby/spec's
// core/thread/backtrace/location/absolute_path_spec.rb asserts that
// #absolute_path is still the resolved path "even when __FILE__ is removed" --
// its fixture deletes the symlink from inside the file being loaded. A lazy
// resolve has nothing left to walk by then; a value taken when the file was read
// survives.
//
// A path with no entry defaults to being its OWN canonical path, which is MRI's
// single-String pathobj: path and realpath are the same object there. The one
// thing that genuinely has no realpath is an eval unit, and eval is recorded
// separately for exactly that reason -- see noteEvalFile.
//
// MRI's C for the loading half (load.c) is absent from the local source corpus,
// so the require/load side is established from the running ruby 4.0.5 oracle and
// from ruby/spec, and cited as such. The two reading contracts ARE in the corpus
// and are quoted at their call sites: r4-eval.c:2148 f_current_dirname and
// vm_eval.c-ruby_4_0:2834 rb_current_realfilepath.

// noteRealFilePath records the canonical path of a file the VM is loading under
// the name spelled -- the moment MRI computes an ISeq's realpath.
//
// spelled must already be a featurePath (absolute, cleaned, forward slashes);
// every call site has just built one to key $LOADED_FEATURES with.
func (vm *VM) noteRealFilePath(spelled string) { vm.noteRealFilePathAs(spelled, spelled) }

// noteRealFilePathAs records under the key spelled the canonical path of the
// file found at abs. The two differ for exactly one file -- the main script,
// whose path MRI leaves as the command line wrote it while still resolving a
// realpath from it -- so the key a reader holds and the name on disk are not the
// same string there.
func (vm *VM) noteRealFilePathAs(spelled, abs string) {
	if vm.realpaths == nil {
		vm.realpaths = map[string]string{}
	}
	vm.realpaths[spelled] = canonicalPath(abs)
}

// canonicalPath is rb_check_realpath with MRI's own fallback: the resolved path,
// or the argument unchanged when it cannot be resolved -- a component became
// unreadable, or the name is one no filesystem answers for. MRI falls back the
// same way (to rb_file_expand_path_fast) rather than dropping the path.
func canonicalPath(p string) string {
	if real, err := realpathResolve(p, true, false); err == nil {
		return real
	}
	return p
}

// noteEvalFile records a filename that names NO file on disk -- the third
// argument of eval(str, binding, file, line), and the "(eval at …)" MRI
// synthesises when none is given. MRI compiles those with realpath Qnil, which
// is the whole of rb_iseq_from_eval_p (iseq.c-ruby_4_0:1480), so
// Location#absolute_path on such a frame is nil.
//
// It is deliberately NOT the default for an unknown path. A Location the program
// built itself -- Exception#set_backtrace(["a:1:in 'm'"]) -- is a dummy frame,
// and MRI gives a dummy frame a plain String pathobj
// (rb_iseq_alloc_with_dummy_path, iseq.c-ruby_4_0:589), where pathobj_realpath
// returns the string: #absolute_path answers "a", not nil. Only eval gets the
// [path, Qnil] pair, so only eval is recorded here.
func (vm *VM) noteEvalFile(name string) {
	if _, loaded := vm.realpaths[name]; loaded {
		// A real file of that name is already loaded; its realpath wins. rbgo keys
		// this registry by path string where MRI keys it per ISeq, and this is the
		// one place the two can disagree.
		return
	}
	if vm.evalFiles == nil {
		vm.evalFiles = map[string]bool{}
	}
	vm.evalFiles[name] = true
}

// realFilePath answers the canonical path of the file loaded under spelled. The
// second result is false only for MRI's realpath-is-nil case: a unit compiled by
// eval. Anything else answers TRUE, defaulting to spelled itself -- that is
// MRI's single-String pathobj, where path and realpath are the same object and
// pathobj_realpath hands back the path (vm_core.h-ruby_4_0:358).
func (vm *VM) realFilePath(spelled string) (string, bool) {
	if real, ok := vm.realpaths[spelled]; ok {
		return real, true
	}
	if vm.evalFiles[spelled] {
		return "", false
	}
	return spelled, true
}

// currentRealDir is MRI's dirname(rb_current_realfilepath()): the directory of
// the file currently executing, with symlinks resolved. It backs Kernel#__dir__
// and is the base require_relative expands against -- rb_f_require_relative
// passes dirname(rb_current_realfilepath()) to rb_file_absolute_path, so the
// BASE is canonical while the name joined onto it is not. ruby/spec pins that
// asymmetry directly: require_relative_spec.rb "does not canonicalize the path
// and stores a path with symlinks" requires through a symlinked directory NAME
// and expects the symlink to survive in $LOADED_FEATURES.
//
// When the executing unit has no recorded realpath it falls back to the path as
// spelled, which is rb_current_realfilepath's own second branch
// (vm_eval.c-ruby_4_0:2840-2843: the realpath if there is one, else the path).
// That is why eval("__dir__", nil, "foo/bar.rb") is "foo" in ruby 4.0.5 -- the
// lexical dirname of a name that is on no disk -- while Location#absolute_path
// on the same frame is nil, reading the realpath alone.
//
// It returns "" when no file is executing, so callers can keep their own
// fallback for a unit that has no path at all (a -e script, a bare eval).
func (vm *VM) currentRealDir() string {
	f := vm.currentFile()
	if f == "" {
		return ""
	}
	if real, ok := vm.realFilePath(f); ok {
		return filepath.Dir(real)
	}
	return filepath.Dir(f)
}
