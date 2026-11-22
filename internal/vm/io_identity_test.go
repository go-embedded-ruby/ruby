// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
)

// TestAWriteByPathReachesOnlyTheOpenedFile: a File writes through the file it
// opened, never through whatever its path names later (#974, TOCTOU). Every
// want is MRI 4.0.7's output for the same program (arm64-darwin25, rubydiff,
// 2026-11-22). The first seven rows diverged before the fix. The symlink row
// is the security one: a stream whose path was swapped for a symlink wrote its
// pending bytes into the link's target ("ABctim"). The last four are guards: a
// file opened THROUGH a symlink, a read-only file written by the open's grant,
// and plain writes, which must keep reaching the file.
func TestAWriteByPathReachesOnlyTheOpenedFile(t *testing.T) {
	const prelude = "path = File.join(d, \"f\"); other = File.join(d, \"other\")\n" +
		"replace = ->(c) { File.write(other, c); File.rename(other, path) }\n"
	cases := []struct {
		name, src, want string
		symlink         bool // needs File.symlink, which Windows runners refuse
		unixMode        bool // asserts permission bits Windows does not keep
	}{
		{name: "an unlinked file is not created again", src: `f = File.open(path, "w"); f.write("AB"); File.delete(path); f.flush
			a = File.exist?(path); f.close; p [a, File.exist?(path)]`, want: "[false, false]\n"},
		{name: "a file renamed over the path is not written", src: `f = File.open(path, "w"); f.write("AB"); replace.("other"); f.close
			p File.read(path)`, want: "\"other\"\n"},
		{name: "a symlink put in its place is not followed", symlink: true, src: `victim = File.join(d, "victim"); File.write(victim, "victim")
			f = File.open(path, "w"); f.write("AB"); File.delete(path); File.symlink(victim, path); f.close
			p File.read(victim)`, want: "\"victim\"\n"},
		{name: "pwrite", src: `f = File.open(path, "w"); f.write("x"); f.flush; replace.("other"); f.pwrite("ZZ", 0); f.close
			p File.read(path)`, want: "\"other\"\n"},
		{name: "truncate", src: `f = File.open(path, "w"); f.write("xyz"); f.flush; replace.("other"); f.truncate(1); f.close
			p File.read(path)`, want: "\"other\"\n"},
		{name: "the stream's own reads still see its bytes", src: `File.write(path, "abcdef")
			f = File.open(path, "r+"); f.write("XY"); replace.("other"); f.rewind
			p [f.read, f.size, f.pread(2, 0)]; f.close; p File.read(path)`, want: "[\"XYcdef\", 6, \"XY\"]\n\"other\"\n"},
		{name: "append mode", src: `File.write(path, "abc"); f = File.open(path, "a"); f.write("Z"); replace.("other"); f.close
			p File.read(path)`, want: "\"other\"\n"},
		{name: "integer flags", src: `File.write(path, "abc"); f = File.open(path, File::RDWR); f.write("Z"); replace.("other"); f.close
			p File.read(path)`, want: "\"other\"\n"},
		{name: "after reopen, the reopened file", src: `real = File.join(d, "real"); File.write(path, "abc"); File.write(real, "real")
			f = File.open(real, "r+"); f.reopen(path, "r+"); f.write("Z"); replace.("other"); f.close
			p [File.read(path), File.read(real)]`, want: "[\"other\", \"real\"]\n"},
		{name: "sync = true", src: `File.write(path, "abc"); f = File.open(path, "r+"); f.sync = true; replace.("other"); f.write("Z"); f.close
			p File.read(path)`, want: "\"other\"\n"},
		{name: "deleted, then created again by another writer", src: `File.write(path, "abc")
			f = File.open(path, "r+"); f.write("Z"); File.delete(path); File.write(path, "new"); f.close
			p File.read(path)`, want: "\"new\"\n"},
		// Guards: these must keep writing.
		{name: "guard: opened through a symlink", symlink: true, src: `real = File.join(d, "real"); link = File.join(d, "link")
			File.write(real, "0123"); File.symlink(real, link)
			f = File.open(link, "r+"); f.write("AB"); f.close; a = File.read(real)
			f = File.open(link, "a"); f.write("Z"); f.flush; f.pwrite("Q", 0); f.truncate(3); f.close
			p [a, File.read(real)]`, want: "[\"AB23\", \"QB2\"]\n"},
		{name: "guard: a read-only file written by the open's grant", unixMode: true,
			src:  `f = File.new(path, "w", 0o444); f.write("ro"); f.close; p [File.read(path), File.stat(path).mode & 0o777]`,
			want: "[\"ro\", 292]\n"},
		{name: "guard: truncate a file made read-only after the open", unixMode: true,
			src: `File.write(path, "abcdef"); f = File.open(path, "r+"); File.chmod(0o444, path); f.truncate(2); f.close
			p [File.size(path), File.stat(path).mode & 0o777]`, want: "[2, 292]\n"},
		{name: "guard: plain integer flags", src: `File.write(path, "abc"); f = File.open(path, File::RDWR); f.write("Z"); f.close
			p File.read(path)`, want: "\"Zbc\"\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if runtime.GOOS == "windows" && (c.symlink || c.unixMode) {
				t.Skip("needs symlinks or Unix permission bits")
			}
			d := filepath.ToSlash(t.TempDir())
			src := fmt.Sprintf("d = %q\n%s%s\n", d, prelude, c.src)
			if got := eval(t, src); got != c.want {
				t.Errorf("got=%q\nwant=%q", got, c.want)
			}
		})
	}
}
