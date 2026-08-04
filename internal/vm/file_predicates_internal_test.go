// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFileSizePredicates covers File.size? / File.zero? / File.empty? and
// File.new: size? returns the size for a non-empty file and nil for an empty or
// missing one; zero?/empty? report emptiness (false for a missing path); File.new
// returns a readable File stream and raises ArgumentError with no path.
func TestFileSizePredicates(t *testing.T) {
	dir := slash(t.TempDir())
	full := dir + "/full.txt"
	empty := dir + "/empty.txt"
	if err := os.WriteFile(filepath.FromSlash(full), []byte("12345678"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.FromSlash(empty), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	missing := dir + "/nope.txt"
	cases := []struct{ src, want string }{
		{`p File.size?("` + full + `")`, "8\n"},
		{`p File.size?("` + empty + `")`, "nil\n"},   // empty ⇒ nil
		{`p File.size?("` + missing + `")`, "nil\n"}, // missing ⇒ nil
		{`p [File.zero?("` + empty + `"), File.empty?("` + empty + `")]`, "[true, true]\n"},
		{`p [File.zero?("` + full + `"), File.zero?("` + missing + `")]`, "[false, false]\n"},
		// File.new opens a readable file-backed stream.
		{`f = File.new("` + full + `"); s = f.read; f.close; p s`, "\"12345678\"\n"},
		{`f = File.new("` + full + `"); p f.size`, "8\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if got := runFSErr(t, `File.new`); got != "ArgumentError" {
		t.Errorf("File.new no-args: got %q want ArgumentError", got)
	}
}
