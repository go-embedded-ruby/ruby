// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestAFlushWritesOnlyWhatThisStreamWrote: another writer's bytes, put in the
// file while this stream had a write pending, survive the flush (#974). Every
// want is MRI 4.0.7's output for the same program (arm64-darwin25, rubydiff,
// 2026-11-22). Before the fix a flush rewrote the stream's whole snapshot:
// the first row ended "AB23456789", the other writer's XY gone.
func TestAFlushWritesOnlyWhatThisStreamWrote(t *testing.T) {
	p := filepath.ToSlash(filepath.Join(t.TempDir(), "f"))
	cases := []struct{ name, mode, act, want string }{
		{"another writer's bytes elsewhere", "r+",
			`f.write("AB"); File.open(p_, "r+") { |g| g.seek(5); g.write("XY") }`, `"AB234XY789"`},
		{"pread's flush keeps them too", "r+",
			`f.write("AB"); File.open(p_, "r+") { |g| g.seek(5); g.write("XY") }; print f.pread(10, 0).inspect`,
			`"AB234XY789""AB234XY789"`},
		{"another writer's append", "r+",
			`f.write("AB"); File.open(p_, "a") { |g| g.write("TAIL") }`, `"AB23456789TAIL"`},
		{"two separate ranges, another write between them", "r+",
			`f.seek(8); f.write("Q"); f.seek(1); f.write("W"); File.open(p_, "r+") { |g| g.seek(4); g.write("M") }`,
			`"0W23M567Q9"`},
		{"append mode lands after the file's new end", "a",
			`f.write("AB"); File.open(p_, "a") { |g| g.write("TAIL") }`, `"0123456789TAILAB"`},
		{"a hole past the end", "r+", `f.seek(12); f.write("Z")`, `"0123456789\x00\x00Z"`},
		{"another process's truncation stands", "r+", `f.write("AB"); File.truncate(p_, 3)`, `"AB2"`},
		{"a write after a flush writes only itself", "r+",
			`f.write("AB"); f.flush; File.open(p_, "r+") { |g| g.write("xyz") }; f.write("C")`, `"xyC3456789"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := fmt.Sprintf("p_ = %q; File.binwrite(p_, \"0123456789\")\n"+
				"File.open(p_, %q) { |f| %s }\nprint File.binread(p_).inspect", p, c.mode, c.act)
			if got := eval(t, src); got != c.want {
				t.Errorf("got=%s\nwant=%s", got, c.want)
			}
		})
	}
}
