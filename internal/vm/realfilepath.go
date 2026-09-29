// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "path/filepath"

// --- the second path an ISeq carries ------------------------------------------
//
// MRI keeps TWO paths for every compiled file, not one: `path`, the name the
// file was reached by, and `realpath`, the same file with its symlinks resolved.
// rb_iseq_path and rb_iseq_realpath read one each, and the APIs divide between
// them -- which is why two methods that look like synonyms are not:
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
// file's resolved path when it LOADS it, exactly where MRI computes the ISeq's
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
// MRI's own C for the loading half (load.c) is absent from the local source
// corpus, so the require/load side of this is established from the running
// ruby 4.0.5 oracle and from ruby/spec, and cited as such. The __dir__ contract
// IS in the corpus: r4-eval.c f_current_dirname is
// `rb_file_dirname(rb_current_realfilepath())`, returning nil when there is no
// realpath.

// noteRealFilePath records the canonical path of a file the VM is loading under
// the name spelled -- the moment MRI computes an ISeq's realpath. Only a file
// whose canonical name DIFFERS is stored: when the two coincide, which is the
// common case, realFilePath's fallback already answers correctly, and the map
// stays the size of the symlinked files actually loaded rather than of every
// file loaded.
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
	real, err := realpathResolve(abs, true, false)
	if err != nil || real == spelled {
		// Unresolvable (the file is gone between the read and here, or a component
		// is unreadable) leaves the spelling standing, which is what MRI reports
		// when rb_realpath_internal fails: the ISeq keeps its path and answers it.
		return
	}
	if vm.realpaths == nil {
		vm.realpaths = map[string]string{}
	}
	vm.realpaths[spelled] = real
}

// realFilePath answers the canonical path of the file loaded under spelled,
// falling back to spelled itself. The fallback is MRI's answer too in the two
// cases that reach it: a file whose path holds no symlink (realpath == path) and
// a compiled unit that was never on disk, where eval's filename argument is
// carried as BOTH path and realpath -- `eval("__dir__", nil, "foo/bar.rb")` is
// "foo" in ruby 4.0.5, the lexical dirname of a path that resolves to nothing.
func (vm *VM) realFilePath(spelled string) string {
	if real, ok := vm.realpaths[spelled]; ok {
		return real
	}
	return spelled
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
// It returns "" when no file is executing, so callers can keep their own
// fallback for a unit that has no path at all (a -e script, a bare eval).
func (vm *VM) currentRealDir() string {
	f := vm.currentFile()
	if f == "" {
		return ""
	}
	return filepath.Dir(vm.realFilePath(f))
}
