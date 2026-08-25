// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestFileConstantsModule covers the File::Constants module: it carries the
// open-mode, flock and fnmatch flag constants, File includes it (so both
// File::RDONLY and File::Constants::RDONLY resolve and File::Constants.const_defined?
// sees every name), and the previously missing flock/extra flags are defined.
// Verified against ruby 4.0.6.
func TestFileConstantsModule(t *testing.T) {
	cases := []struct{ src, want string }{
		// File::Constants is a module File includes.
		{`p File::Constants.class`, `Module`},
		{`p File.include?(File::Constants)`, `true`},
		// Every documented name is defined on the module.
		{`p ["APPEND", "CREAT", "EXCL", "FNM_CASEFOLD", "FNM_DOTMATCH", "FNM_EXTGLOB", "FNM_NOESCAPE", "FNM_PATHNAME", "FNM_SYSCASE", "LOCK_EX", "LOCK_NB", "LOCK_SH", "LOCK_UN", "NONBLOCK", "NOCTTY", "RDONLY", "RDWR", "SYNC", "TRUNC", "WRONLY", "SHARE_DELETE"].all? { |c| File::Constants.const_defined?(c) }`, `true`},
		// The flags resolve through both File and File::Constants.
		{`p File::RDONLY`, `0`},
		{`p File::Constants::RDWR`, `2`},
		{`p File::LOCK_EX`, `2`},
		{`p File::LOCK_SH`, `1`},
		{`p File::LOCK_NB`, `4`},
		{`p File::LOCK_UN`, `8`},
		{`p [File::RDONLY, File::WRONLY, File::RDWR]`, `[0, 1, 2]`},
		// The flag values compose the way File.open's integer-mode form expects.
		{`p File::WRONLY | File::CREAT | File::TRUNC`, `577`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
