// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"testing"
)

// dirSandbox creates a temp directory holding files "a" and "b" and returns its
// forward-slashed path.
func dirSandbox(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(d, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return slash(d)
}

// TestDirInstance covers the Dir instance surface (open/new, read/pos/tell/seek/
// pos=/rewind/path/to_path/each/each_child/close) over a real temp directory.
func TestDirInstance(t *testing.T) {
	d := dirSandbox(t)
	cases := []struct{ src, want string }{
		// new/open build a Dir; path and to_path echo the path (open and closed).
		{`dir = Dir.new("` + d + `"); r = [dir.path, dir.to_path, dir.is_a?(Dir)]; dir.close; p r`,
			`["` + d + `", "` + d + `", true]` + "\n"},
		{`dir = Dir.open("` + d + `"); dir.close; p dir.to_path`, `"` + d + `"` + "\n"},
		// read walks every entry (".", "..", "a", "b") then returns nil.
		{`dir = Dir.open("` + d + `"); a = []; while e = dir.read; a << e; end; dir.close; p a.sort`,
			`[".", "..", "a", "b"]` + "\n"},
		// pos is an Integer and changes across a read; tell is the same method as pos.
		{`dir = Dir.open("` + d + `"); a = dir.pos; dir.read; b = dir.pos; dir.close; p [a.is_a?(Integer), a != b, Dir.instance_method(:tell) == Dir.instance_method(:pos)]`,
			"[true, true, true]\n"},
		// seek(pos) returns the Dir and returns the cursor to the saved position.
		{`dir = Dir.open("` + d + `"); pos = dir.pos; a = dir.read; b = dir.read; ret = dir.seek(pos); c = dir.read; dir.close; p [ret.equal?(dir), a != b, c == a]`,
			"[true, true, true]\n"},
		// pos= moves the cursor and returns its argument.
		{`dir = Dir.open("` + d + `"); pos = dir.pos; a = dir.read; dir.read; rv = (dir.pos = pos); c = dir.read; dir.close; p [rv, c == a]`,
			"[0, true]\n"},
		// rewind returns the Dir and resets the cursor.
		{`dir = Dir.open("` + d + `"); a = dir.read; dir.read; ret = dir.rewind; c = dir.read; dir.close; p [ret.equal?(dir), c == a]`,
			"[true, true]\n"},
		// each yields every entry, returns the Dir, and leaves the cursor at the end.
		{`dir = Dir.open("` + d + `"); a = []; r = dir.each { |e| a << e }; nx = dir.read; dir.close; p [a.sort, r.equal?(dir), nx.nil?]`,
			`[[".", "..", "a", "b"], true, true]` + "\n"},
		// each_child skips "." and ".." and returns the Dir.
		{`dir = Dir.open("` + d + `"); a = []; r = dir.each_child { |e| a << e }; dir.close; p [a.sort, r.equal?(dir)]`,
			`[["a", "b"], true]` + "\n"},
		// each / each_child without a block return an Enumerator.
		{`dir = Dir.open("` + d + `"); r = [dir.each.class.to_s, dir.each_child.class.to_s]; dir.close; p r`,
			`["Enumerator", "Enumerator"]` + "\n"},
		// close returns nil and is idempotent.
		{`dir = Dir.open("` + d + `"); p [dir.close, dir.close]`, "[nil, nil]\n"},
		// open with a block yields the Dir, returns the block value, and closes it.
		{`v = Dir.open("` + d + `") { |x| x.read; :val }; p v`, ":val\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDirInstanceErrors covers the error surface: a closed handle raises IOError
// for every read-side method (path/to_path still work), fileno raises
// NotImplementedError while open, Dir.open on a missing path raises a
// SystemCallError, and a block that raises still closes the handle.
func TestDirInstanceErrors(t *testing.T) {
	d := dirSandbox(t)

	// Every method that needs an open handle raises IOError once closed.
	for _, m := range []string{"read", "pos", "tell", "rewind", "fileno",
		"seek(0)", "each { |x| }", "each_child { |x| }"} {
		expr := `dir = Dir.open("` + d + `"); dir.close; dir.` + m
		if got := runFSErr(t, expr); got != "IOError" {
			t.Errorf("closed Dir#%s: got %q want IOError", m, got)
		}
	}
	// pos= on a closed handle also raises IOError.
	if got := runFSErr(t, `dir = Dir.open("`+d+`"); dir.close; dir.pos = 0`); got != "IOError" {
		t.Errorf("closed Dir#pos=: got %q want IOError", got)
	}
	// fileno on an open handle raises NotImplementedError (no dirfd in rbgo).
	if got := runFSErr(t, `dir = Dir.open("`+d+`"); dir.fileno`); got != "NotImplementedError" {
		t.Errorf("open Dir#fileno: got %q want NotImplementedError", got)
	}
	// Dir.open / Dir.new on a missing path raises a SystemCallError (Errno::ENOENT).
	if got := runFSErr(t, `Dir.open("`+d+`/nope")`); got != "Errno::ENOENT" {
		t.Errorf("Dir.open(missing): got %q want Errno::ENOENT", got)
	}
	if got := runFSErr(t, `Dir.new("`+d+`/nope")`); got != "Errno::ENOENT" {
		t.Errorf("Dir.new(missing): got %q want Errno::ENOENT", got)
	}
	// A block that raises still closes the handle: the escaped Dir is unusable.
	closedByRaise := `cd = nil
begin
  Dir.open("` + d + `") { |x| cd = x; raise "boom" }
rescue RuntimeError
end
cd.read`
	if got := runFSErr(t, closedByRaise); got != "IOError" {
		t.Errorf("open-block-raise leaves handle open: got %q want IOError", got)
	}
}

// TestDirObjDisplay covers DirObj's ToS/Inspect/Truthy.
func TestDirObjDisplay(t *testing.T) {
	d := &DirObj{path: "/some/dir"}
	if d.ToS() != "#<Dir:/some/dir>" || d.Inspect() != "#<Dir:/some/dir>" || !d.Truthy() {
		t.Errorf("display: ToS=%q Inspect=%q Truthy=%v", d.ToS(), d.Inspect(), d.Truthy())
	}
}
