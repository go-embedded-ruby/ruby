// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestFilePathArgConversion drives filePathArg / pathStr through every File
// entry point that uses them, covering the #to_path and #to_str conversion paths
// and the two guards MRI's rb_get_path applies after conversion:
// Encoding::CompatibilityError for an ASCII-incompatible encoding and
// ArgumentError for an embedded NUL byte (ruby/ruby v3_4_0 file.c
// check_path_encoding / rb_get_path_check_convert).
func TestFilePathArgConversion(t *testing.T) {
	// Successful conversions: a String directly, an object answering #to_path,
	// and one answering only #to_str (the fallback rb_get_path_check_to_string
	// falls through to).
	ok := []struct{ src, want string }{
		{`p File.path("plain")`, "\"plain\"\n"},
		{`o = Object.new; def o.to_path; "viapath"; end; p File.path(o)`, "\"viapath\"\n"},
		{`o = Object.new; def o.to_str; "viastr"; end; p File.path(o)`, "\"viastr\"\n"},
		// basename/dirname/extname/split share the same coercion.
		{`o = Object.new; def o.to_path; "/a/b.rb"; end; p File.basename(o)`, "\"b.rb\"\n"},
		{`o = Object.new; def o.to_path; "/a/b.rb"; end; p File.extname(o)`, "\".rb\"\n"},
	}
	for _, c := range ok {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	// NUL byte in the path (or in a #to_path result) is an ArgumentError, on
	// every File entry point that goes through filePathArg.
	nulErr := []string{
		`File.path("\0")`,
		`File.path("a\0c")`,
		`File.basename("a\0c")`,
		`File.dirname("a\0c")`,
		`File.extname("a\0c")`,
		`File.split("a\0c")`,
		`File.expand_path("a\0c")`,
		`File.fnmatch("*", "a\0c")`,
		`o = Object.new; def o.to_path; "a\0c"; end; File.path(o)`,
	}
	for _, src := range nulErr {
		if got := runFSErr(t, src); got != "ArgumentError" {
			t.Errorf("%s: got %q want ArgumentError", src, got)
		}
	}

	// An ASCII-incompatible encoding is an Encoding::CompatibilityError.
	encErr := []string{
		`File.path("abc".encode(Encoding::UTF_32BE))`,
		`File.basename("abc".encode(Encoding::UTF_16LE))`,
		`File.dirname("abc".encode(Encoding::UTF_16BE))`,
		`File.expand_path("abc".encode(Encoding::UTF_32LE))`,
	}
	for _, src := range encErr {
		if got := runFSErr(t, src); got != "Encoding::CompatibilityError" {
			t.Errorf("%s: got %q want Encoding::CompatibilityError", src, got)
		}
	}

	// #to_path returning a non-String, and a value answering neither #to_path nor
	// #to_str, are both TypeError (pathStr's two raise branches).
	typeErr := []string{
		`o = Object.new; def o.to_path; 42; end; File.path(o)`,
		`File.path(42)`,
		`File.basename(Object.new)`,
	}
	for _, src := range typeErr {
		if got := runFSErr(t, src); got != "TypeError" {
			t.Errorf("%s: got %q want TypeError", src, got)
		}
	}
}

// TestFileFnmatchCoercionAndAlias covers the two File.fnmatch fixes: the flags
// argument is coerced with #to_int (rb_to_int/NUM2INT) rather than requiring an
// Integer, and File.fnmatch? shares one method record with File.fnmatch so the
// two Method objects compare equal (a genuine built-in alias, as in MRI).
func TestFileFnmatchCoercionAndAlias(t *testing.T) {
	// A #to_int-coercible flags argument does not raise and is honoured
	// (FNM_CASEFOLD == 8 makes the match case-insensitive).
	src := `o = Object.new; def o.to_int; File::FNM_CASEFOLD; end; p File.fnmatch("A*", "abc", o)`
	if got := runFS(t, src); got != "true\n" {
		t.Errorf("fnmatch to_int flags: got %q want true", got)
	}
	if got := runFS(t, `p File.method(:fnmatch?) == File.method(:fnmatch)`); got != "true\n" {
		t.Errorf("fnmatch? alias: got %q want true", got)
	}
}

// TestFileExpandPathBaseToPath covers the base-directory argument of
// File.expand_path being coerced via #to_path (rb_get_path), not a bare String
// check — so a Pathname-like object is accepted, matching MRI.
func TestFileExpandPathBaseToPath(t *testing.T) {
	src := `o = Object.new; def o.to_path; "/base/dir"; end; p File.expand_path("a", o)`
	if got := runFS(t, src); got != "\"/base/dir/a\"\n" {
		t.Errorf("expand_path base to_path: got %q want /base/dir/a", got)
	}
}
