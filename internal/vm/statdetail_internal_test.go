// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeTimespec/fakeStat stand in for a platform stat struct so statTimeField's
// name resolution can be driven on every GOOS, including the ones whose
// syscall.Stat_t has no such fields at all.
type fakeTimespec struct{ Sec, Nsec int64 }

type fakeStat struct {
	Atim          fakeTimespec
	Ctimespec     fakeTimespec
	Birthtimespec fakeTimespec
	NotATime      int64
}

// TestStatTimeField covers every decision in the reflection lookup: the Linux
// spelling, the BSD spelling, a name the struct does not carry, a name whose
// field is not a struct, a non-struct argument, a nil pointer and a nil value.
func TestStatTimeField(t *testing.T) {
	st := &fakeStat{Atim: fakeTimespec{Sec: 11}, Ctimespec: fakeTimespec{Sec: 22}, Birthtimespec: fakeTimespec{Sec: 33}}
	for _, c := range []struct {
		names   []string
		want    int64
		wantOk  bool
		comment string
	}{
		{[]string{"Atim", "Atimespec"}, 11, true, "the Linux spelling, found first"},
		{[]string{"Ctim", "Ctimespec"}, 22, true, "the BSD spelling, found second"},
		{[]string{"Birthtimespec", "Btim"}, 33, true, "a creation time"},
		{[]string{"Mtim", "Mtimespec"}, 0, false, "a name the struct does not carry"},
		{[]string{"NotATime"}, 0, false, "a field that is not a timespec"},
	} {
		got, ok := statTimeField(st, c.names...)
		if got != c.want || ok != c.wantOk {
			t.Errorf("%s: got %d,%v want %d,%v", c.comment, got, ok, c.want, c.wantOk)
		}
	}
	if _, ok := statTimeField(nil, "Atim"); ok {
		t.Errorf("a nil Sys() must report nothing")
	}
	if _, ok := statTimeField((*fakeStat)(nil), "Atim"); ok {
		t.Errorf("a nil pointer must report nothing")
	}
	if _, ok := statTimeField(42, "Atim"); ok {
		t.Errorf("a non-struct must report nothing")
	}
	// A struct with a Sec field that is not an integer is ignored too.
	type oddTime struct{ Sec string }
	if _, ok := statTimeField(&struct{ Atim oddTime }{}, "Atim"); ok {
		t.Errorf("a non-integer Sec must report nothing")
	}
}

// stubInfo is an fs.FileInfo whose Sys() is whatever the test hands it, so
// statTimestamps' fallback can be driven without a real filesystem.
type stubInfo struct {
	mod time.Time
	sys any
}

func (s stubInfo) Name() string       { return "stub" }
func (s stubInfo) Size() int64        { return 0 }
func (s stubInfo) Mode() fs.FileMode  { return 0o644 }
func (s stubInfo) ModTime() time.Time { return s.mod }
func (s stubInfo) IsDir() bool        { return false }
func (s stubInfo) Sys() any           { return s.sys }

// TestStatTimestamps covers the two shapes statTimestamps must handle: a stat
// struct that carries the timestamps, and one that carries none — where the
// modification time stands in and no creation time is recorded, so
// File::Stat#birthtime raises the way MRI does on Linux.
func TestStatTimestamps(t *testing.T) {
	mod := time.Unix(500, 0)
	full := statTimestamps(stubInfo{mod: mod, sys: &fakeStat{
		Atim: fakeTimespec{Sec: 1}, Ctimespec: fakeTimespec{Sec: 2}, Birthtimespec: fakeTimespec{Sec: 3},
	}}, statFields{})
	if full.atime != 1 || full.ctime != 2 || full.btime != 3 || !full.hasBtime {
		t.Errorf("full stat: %+v", full)
	}
	bare := statTimestamps(stubInfo{mod: mod, sys: nil}, statFields{})
	if bare.atime != 500 || bare.ctime != 500 {
		t.Errorf("a stat without timestamps must fall back to mtime: %+v", bare)
	}
	if bare.hasBtime {
		t.Errorf("a stat without a creation time must not claim one")
	}
}

// TestFileStatBirthtimeUnsupported drives the NotImplementedError branch through
// the Ruby surface by handing newFileStat a stat struct with no creation time —
// the state a Linux host is always in.
func TestFileStatBirthtimeUnsupported(t *testing.T) {
	st := &FileStat{fi: stubInfo{mod: time.Unix(7, 0)}, sys: statTimestamps(stubInfo{mod: time.Unix(7, 0)}, statFields{})}
	got := expectRaise(t, func() {
		New(nil).send(st, "birthtime", nil, nil)
	})
	if got != "NotImplementedError" {
		t.Errorf("birthtime without a creation time raised %q", got)
	}
}

// TestFileStatInspect pins rb_stat_inspect's layout: the member order, dev/rdev
// in hex, mode in octal with a leading zero, and birthtime present only where
// the platform has one. The values are checked against the accessors, so the
// test cannot pass by agreeing with a wrong reading of the stat struct.
func TestFileStatInspect(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	if err := os.WriteFile(f, []byte("rubinius"), 0o644); err != nil {
		t.Fatal(err)
	}
	// MRI 4.0.5 on darwin builds the same string from the accessors; this is
	// core/file/stat/inspect_spec.rb's own expectation, in Go.
	out := runFS(t, `
st = File.stat(`+rq(f)+`)
e = "#<File::Stat dev=0x#{st.dev.to_s(16)}, ino=#{st.ino}, mode=#{sprintf("%07o", st.mode)}, nlink=#{st.nlink}"
e << ", uid=#{st.uid}, gid=#{st.gid}, rdev=0x#{st.rdev.to_s(16)}, size=#{st.size}, blksize=#{st.blksize.inspect}"
e << ", blocks=#{st.blocks.inspect}, atime=#{st.atime.inspect}, mtime=#{st.mtime.inspect}, ctime=#{st.ctime.inspect}"
has_birthtime = begin; st.birthtime; true; rescue NotImplementedError; false; end
e << ", birthtime=#{st.birthtime.inspect}" if has_birthtime
e << ">"
puts st.inspect == e ? "match" : "GOT #{st.inspect}\nWANT #{e}"
`)
	if out != "match\n" {
		t.Errorf("File::Stat#inspect does not match the member-by-member expectation:\n%s", out)
	}
	// Kernel#p reaches the same rendering, so the two cannot drift.
	st, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	native := newFileStat(st, f).Inspect()
	if got := runFS(t, `puts File.stat(`+rq(f)+`).inspect`); got != native+"\n" {
		t.Errorf("Ruby #inspect %q differs from the native one %q", got, native)
	}
	// blocks and blksize come from the POSIX stat that Windows does not have:
	// MRI answers nil for #blocks where HAVE_STRUCT_STAT_ST_BLOCKS is undefined,
	// and rb_stat_inspect prints that nil. So the layout accepts either shape
	// rather than asserting a number that only POSIX can produce.
	if !regexp.MustCompile(`^#<File::Stat dev=0x[0-9a-f]+, ino=\d+, mode=0\d+, nlink=\d+, uid=\d+, gid=\d+, rdev=0x[0-9a-f]+, size=8, blksize=(?:\d+|nil), blocks=(?:\d+|nil), atime=.*, mtime=.*, ctime=.*>$`).
		MatchString(strings.TrimSuffix(regexp.MustCompile(`, birthtime=[^>]*`).ReplaceAllString(native, ""), "")) {
		t.Errorf("unexpected layout: %s", native)
	}
}

// TestFileStatAtimeIsNotMtime is the witness that atime is READ rather than
// substituted: File.utime sets the two apart, and MRI 4.0.5 reports them apart.
func TestFileStatAtimeIsNotMtime(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows' Sys() is a *syscall.Win32FileAttributeData, whose LastAccessTime
		// is a Filetime rather than a Sec-bearing timespec, so statTimeField finds
		// nothing and atime falls back to mtime. MRI on Windows does report a real
		// atime; there is no Windows MRI on this host to witness the right
		// behaviour against, so the case is skipped rather than asserted loosely.
		t.Skip("no Windows atime support and no Windows MRI witness — see the per-platform stat issue")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := runFS(t, `
File.utime(Time.utc(2000), Time.utc(2001), `+rq(f)+`)
st = File.stat(`+rq(f)+`)
p [st.atime.utc.to_s, st.mtime.utc.to_s]
`)
	want := "[\"2000-01-01 00:00:00 UTC\", \"2001-01-01 00:00:00 UTC\"]\n"
	if got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

// TestFileStreamStat covers File#stat / File#lstat: both answer for the open
// stream's own path, lstat does not follow a final symlink, neither takes an
// argument, and a closed stream is an IOError. Witnessed against MRI 4.0.5.
func TestFileStreamStat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the symlink half needs privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("abcd"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	got := runFS(t, `
f = File.open(`+rq(link)+`)
p [f.stat.class, f.stat.size, f.stat.file?, f.stat.symlink?, f.lstat.symlink?]
begin; f.stat(1); rescue ArgumentError => e; puts e.message; end
f.close
begin; f.stat; rescue IOError => e; puts e.message; end
begin; f.lstat; rescue IOError => e; puts e.message; end
`)
	want := "[File::Stat, 4, true, false, true]\n" +
		"wrong number of arguments (given 1, expected 0)\n" +
		"closed stream\nclosed stream\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
