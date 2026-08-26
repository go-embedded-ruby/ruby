// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestIOConstants covers the IO seek-whence constants (SEEK_SET/CUR/END, plus
// SEEK_DATA/HOLE) defined on IO, that File inherits them (File < IO), and that IO
// mixes in File::Constants so the open-mode flags resolve as IO::RDONLY too.
// Verified against ruby 4.0.6.
func TestIOConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p IO::SEEK_SET`, `0`},
		{`p IO::SEEK_CUR`, `1`},
		{`p IO::SEEK_END`, `2`},
		{`p [IO::SEEK_SET, IO::SEEK_CUR, IO::SEEK_END]`, `[0, 1, 2]`},
		{`p IO.const_defined?(:SEEK_SET) && IO.const_defined?(:SEEK_CUR) && IO.const_defined?(:SEEK_END)`, `true`},
		{`p IO.const_defined?(:SEEK_DATA) && IO.const_defined?(:SEEK_HOLE)`, `true`},
		// File inherits the seek constants (File < IO).
		{`p File::SEEK_SET`, `0`},
		{`p File::SEEK_END`, `2`},
		// IO mixes in File::Constants, so the open-mode flags resolve through IO.
		{`p IO.include?(File::Constants)`, `true`},
		{`p IO::RDONLY`, `0`},
		{`p [IO::RDONLY, IO::WRONLY, IO::RDWR]`, `[0, 1, 2]`},
		{`p IO.const_defined?(:LOCK_EX)`, `true`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
