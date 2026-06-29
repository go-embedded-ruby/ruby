// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestPathnameLexical covers the lexical (no-I/O) path algebra now backed by
// github.com/go-ruby-pathname/pathname through the native __lex_* helpers. Every
// expectation is asserted against MRI Ruby 4.0.5's pathname standard library.
func TestPathnameLexical(t *testing.T) {
	cases := []struct{ src, want string }{
		// to_s / to_path / to_str / inspect.
		{`p Pathname.new("/usr/bin/ruby").to_s`, "\"/usr/bin/ruby\"\n"},
		{`p Pathname.new("a/b").to_path`, "\"a/b\"\n"},
		{`p Pathname.new("a/b").to_str`, "\"a/b\"\n"},
		{`p Pathname.new("x")`, "#<Pathname:x>\n"},
		// constructor coercion: a Pathname argument is unwrapped.
		{`p Pathname.new(Pathname.new("a/b")).to_s`, "\"a/b\"\n"},

		// + / / / join.
		{`p (Pathname.new("a") + "b").to_s`, "\"a/b\"\n"},
		{`p (Pathname.new("a") + Pathname.new("b")).to_s`, "\"a/b\"\n"},
		{`p (Pathname.new("a") / "/b").to_s`, "\"/b\"\n"},
		{`p Pathname.new("a/b").join("c", "d").to_s`, "\"a/b/c/d\"\n"},
		{`p Pathname.new("a").join.to_s`, "\"a\"\n"},

		// basename / dirname / parent / extname.
		{`p Pathname.new("/a/b/c.txt").basename.to_s`, "\"c.txt\"\n"},
		{`p Pathname.new("/a/b/c.txt").basename(".txt").to_s`, "\"c\"\n"},
		{`p Pathname.new("/a/b/c.txt").basename(".*").to_s`, "\"c\"\n"},
		{`p Pathname.new("/a/b/c.txt").dirname.to_s`, "\"/a/b\"\n"},
		{`p Pathname.new("/a/b/c.txt").parent.to_s`, "\"/a/b\"\n"},
		{`p Pathname.new("a").dirname.to_s`, "\".\"\n"},
		{`p Pathname.new("/a").dirname.to_s`, "\"/\"\n"},
		{`p Pathname.new("/a/b/c.txt").extname`, "\".txt\"\n"},
		// trailing-dot extname is "." in MRI (was a prelude bug, fixed by the lib).
		{`p Pathname.new("a.").extname`, "\".\"\n"},
		{`p Pathname.new(".bashrc").extname`, "\"\"\n"},

		// cleanpath (the fiddly one), matched to MRI.
		{`p Pathname.new("/a/b/../c").cleanpath.to_s`, "\"/a/c\"\n"},
		{`p Pathname.new("a//b/./c").cleanpath.to_s`, "\"a/b/c\"\n"},
		{`p Pathname.new("a/../..").cleanpath.to_s`, "\"..\"\n"},
		{`p Pathname.new("").cleanpath.to_s`, "\".\"\n"},

		// split / each_filename.
		{`p Pathname.new("/a/b/c").split.map(&:to_s)`, "[\"/a/b\", \"c\"]\n"},
		{`p Pathname.new("/a/b/c").each_filename.to_a`, "[\"a\", \"b\", \"c\"]\n"},
		{`a = []; Pathname.new("a/b").each_filename { |f| a << f }; p a`, "[\"a\", \"b\"]\n"},

		// ascend / descend.
		{`p Pathname.new("/a/b/c").ascend.map(&:to_s)`, "[\"/a/b/c\", \"/a/b\", \"/a\", \"/\"]\n"},
		{`p Pathname.new("/a/b/c").descend.map(&:to_s)`, "[\"/\", \"/a\", \"/a/b\", \"/a/b/c\"]\n"},
		{`a = []; Pathname.new("a/b").ascend { |p| a << p.to_s }; p a`, "[\"a/b\", \"a\"]\n"},
		{`a = []; Pathname.new("a/b").descend { |p| a << p.to_s }; p a`, "[\"a\", \"a/b\"]\n"},

		// sub_ext.
		{`p Pathname.new("foo.rb").sub_ext(".o").to_s`, "\"foo.o\"\n"},
		{`p Pathname.new("foo").sub_ext(".o").to_s`, "\"foo.o\"\n"},

		// absolute? / relative? / root?.
		{`p Pathname.new("/abs").absolute?`, "true\n"},
		{`p Pathname.new("rel").absolute?`, "false\n"},
		{`p Pathname.new("rel").relative?`, "true\n"},
		{`p Pathname.new("/").root?`, "true\n"},
		{`p Pathname.new("/a").root?`, "false\n"},

		// relative_path_from (the other fiddly one).
		{`p Pathname.new("a/b/c").relative_path_from("a").to_s`, "\"b/c\"\n"},
		{`p Pathname.new("/a/b").relative_path_from("/a/b/c/d").to_s`, "\"../..\"\n"},
		{`p Pathname.new("/a").relative_path_from(Pathname.new("/a")).to_s`, "\".\"\n"},

		// comparison / equality / hash (Comparable + ==/eql?/<=>/hash).
		{`p (Pathname.new("a") <=> Pathname.new("b"))`, "-1\n"},
		{`p (Pathname.new("a") <=> 5)`, "nil\n"},
		{`p (Pathname.new("a") < Pathname.new("b"))`, "true\n"},
		{`p (Pathname.new("a") == Pathname.new("a"))`, "true\n"},
		{`p (Pathname.new("a") == "a")`, "false\n"},
		{`p (Pathname.new("a").eql?(Pathname.new("a")))`, "true\n"},
		{`p (Pathname.new("x").hash == Pathname.new("x").hash)`, "true\n"},
	}
	for _, c := range cases {
		src := `require "pathname"; ` + c.src
		if got := eval(t, src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPathnameRelativePathFromErrors covers the ArgumentError branch of the
// native __lex_relative_path_from helper, which surfaces the library's error
// (MRI's exact message) for an incompatible base directory.
func TestPathnameRelativePathFromErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		// mixing relative dest with absolute base.
		{`Pathname.new("a").relative_path_from("/b")`, "[ArgumentError, \"different prefix: \\\"\\\" and \\\"/b\\\"\"]\n"},
		// mixing absolute dest with relative base.
		{`Pathname.new("/a").relative_path_from("b")`, "[ArgumentError, \"different prefix: \\\"/\\\" and \\\"b\\\"\"]\n"},
		// a ".." that escapes a relative base.
		{`Pathname.new("a").relative_path_from("../b")`, "[ArgumentError, \"base_directory has ..: \\\"../b\\\"\"]\n"},
	}
	for _, c := range cases {
		src := `require "pathname"; begin; ` + c.src + `; rescue => e; p [e.class, e.message]; end`
		if got := eval(t, src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPathnameFilesystemStaysHostSide covers that the filesystem delegations of
// Pathname (read/write/exist?/file?/directory?/each_line/open) remain host-side
// in rbgo — forwarding to the File class against a real temp tree — and were not
// disturbed by moving the lexical algebra into the library.
func TestPathnameFilesystemStaysHostSide(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	f := dir + "/data.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := func(s string) string { return strconv.Quote(s) }

	cases := []struct{ src, want string }{
		{`p Pathname.new(` + q(f) + `).exist?`, "true\n"},
		{`p Pathname.new(` + q(dir+"/missing") + `).exist?`, "false\n"},
		{`p Pathname.new(` + q(f) + `).file?`, "true\n"},
		{`p Pathname.new(` + q(dir) + `).directory?`, "true\n"},
		{`p Pathname.new(` + q(f) + `).read`, "\"a\\nb\\n\"\n"},
		{`lines = []; Pathname.new(` + q(f) + `).each_line { |l| lines << l }; p lines`, "[\"a\\n\", \"b\\n\"]\n"},
		{`Pathname.new(` + q(dir+"/out.txt") + `).write("hi"); p File.read(` + q(dir+"/out.txt") + `)`, "\"hi\"\n"},
		{`p Pathname.new(` + q(f) + `).open { |io| io.read }`, "\"a\\nb\\n\"\n"},
	}
	for _, c := range cases {
		src := `require "pathname"; ` + c.src
		if got := eval(t, src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
