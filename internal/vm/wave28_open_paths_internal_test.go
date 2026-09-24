// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestWave28SysopenIntegerModes reaches vmodeString and flagsToMode, the
// lossy Integer→mode-string mapping the remaining callers still use:
// IO.sysopen, IO#reopen and IO.popen all take a mode that way. File.open no
// longer does — it works from the open(2) flags themselves — so these are the
// only paths left through it, and each flag combination maps to the fopen-style
// mode openFileIO understands.
func TestWave28SysopenIntegerModes(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/m.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, want string }{
		// Each Integer mode reaches flagsToMode; the resulting stream reports the
		// access halves that mode implies.
		{`fd = IO.sysopen("` + f + `", File::RDONLY); io = IO.for_fd(fd); p [io.read, io.closed?]`, "[\"body\", false]\n"},
		{`fd = IO.sysopen("` + f + `", File::RDWR); io = IO.for_fd(fd); p io.read`, "\"body\"\n"},
		{`fd = IO.sysopen("` + f + `", File::APPEND); io = IO.for_fd(fd); p io.class`, "IO\n"},
		{`fd = IO.sysopen("` + f + `", File::APPEND | File::RDWR); io = IO.for_fd(fd); p io.class`, "IO\n"},
		{`fd = IO.sysopen("` + dir + `/w.txt", File::WRONLY); p File.exist?("` + dir + `/w.txt")`, "true\n"},
		{`fd = IO.sysopen("` + dir + `/c.txt", File::RDWR | File::CREAT); p File.exist?("` + dir + `/c.txt")`, "true\n"},
		// A String mode is taken as it stands.
		{`fd = IO.sysopen("` + f + `", "r"); p IO.for_fd(fd).read`, "\"body\"\n"},
		// An object converts through #to_int (an Integer mode) or #to_str (a
		// String one); anything else is the TypeError naming String.
		{`o = Object.new; def o.to_int; File::RDONLY; end; fd = IO.sysopen("` + f + `", o); p IO.for_fd(fd).read`, "\"body\"\n"},
		{`o = Object.new; def o.to_str; "r"; end; fd = IO.sysopen("` + f + `", o); p IO.for_fd(fd).read`, "\"body\"\n"},
		{`begin; IO.sysopen("` + f + `", :r); rescue => e; p [e.class, e.message]; end`,
			"[TypeError, \"no implicit conversion of Symbol into String\"]\n"},
		// openFileIO's own refusals: an empty and an unrecognised mode.
		{`begin; IO.sysopen("` + f + `", ""); rescue => e; p [e.class, e.message]; end`,
			"[ArgumentError, \"invalid access mode \"]\n"},
		{`begin; IO.sysopen("` + f + `", "zz"); rescue => e; p [e.class, e.message]; end`,
			"[ArgumentError, \"invalid access mode zz\"]\n"},
		// openFileIO's 'a' branch on a path that does not exist yet materialises it,
		// because FMODE_APPEND carries O_CREAT.
		{`fd = IO.sysopen("` + dir + `/a.txt", "a"); p File.exist?("` + dir + `/a.txt")`, "true\n"},
		{`fd = IO.sysopen("` + f + `", "a"); p File.exist?("` + f + `")`, "true\n"},
		// ...and its 'r' and 'a' branches on something that is not a regular file
		// open with an empty buffer rather than trying to read the whole of it.
		{`fd = IO.sysopen("` + dir + `"); p IO.for_fd(fd).read`, "\"\"\n"},
		{`fd = IO.sysopen("` + dir + `", "a"); p IO.for_fd(fd).class`, "IO\n"},
		// Both write branches materialise the file, and report the failure to do so
		// as the ENOENT a missing directory gives.
		{`begin; IO.sysopen("` + dir + `/nodir/x", File::WRONLY); rescue => e; p [e.class, e.message]; end`,
			"[Errno::ENOENT, \"No such file or directory @ rb_sysopen - " + dir + "/nodir/x\"]\n"},
		{`begin; IO.sysopen("` + dir + `/nodir/y", "a"); rescue => e; p [e.class, e.message]; end`,
			"[Errno::ENOENT, \"No such file or directory @ rb_sysopen - " + dir + "/nodir/y\"]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestWave28FileSizeFromTheDescriptor covers rb_file_size's two ends: it is an
// fstat on the OPEN descriptor, so a closed stream has none to ask ("closed
// stream") and an UNLINKED path still answers — the descriptor outlives the
// directory entry, which is what core/file/size_spec.rb calls "the cached size
// of the file if subsequently deleted". MRI 4.0.5.
func TestWave28FileSizeFromTheDescriptor(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/s.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("rubinius"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, want string }{
		{`p File.new("` + f + `").size`, "8\n"},
		// Another stream appends: the size is read afresh, not remembered.
		{`io = File.new("` + f + `"); File.open("` + f + `", "a") { |g| g.write "!" }; p io.size`, "9\n"},
		// The path is gone, but the stream still has its bytes.
		{`io = File.new("` + f + `"); File.delete("` + f + `"); p io.size`, "8\n"},
		{`io = File.new("` + f + `"); io.close; begin; io.size; rescue => e; p [e.class, e.message]; end`,
			"[IOError, \"closed stream\"]\n"},
	}
	for _, c := range cases {
		if err := os.WriteFile(filepath.FromSlash(f), []byte("rubinius"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestWave28OpenFlagsCombinations reaches the arms of ioModeSpec.oflags that
// only the :flags option asks for — it is the one caller that has to turn an
// fmode back into an open(2) flag set (rb_io_fmode_oflags), so each of
// O_RDWR, O_APPEND and O_EXCL has to survive the round trip.
func TestWave28OpenFlagsCombinations(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/f.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, want string }{
		// "r+" is O_RDWR, and survives as a readable AND writable stream.
		{`File.open("` + f + `", "r+", flags: 0) { |io| p [io.read, io.class] }`, "[\"body\", File]\n"},
		// "a" is O_WRONLY|O_APPEND|O_CREAT; the append half survives, so the write
		// still lands at the end.
		{`File.open("` + f + `", "a", flags: 0) { |io| io.write "!" }; p File.read("` + f + `")`, "\"body!\"\n"},
		// "wx" is O_EXCL, and the flag set it produces still refuses an existing
		// file — the round trip did not drop it.
		{`begin; File.open("` + f + `", "wx", flags: 0) {}; rescue => e; p [e.class, e.message]; end`,
			"[Errno::EEXIST, \"File exists @ rb_sysopen - " + f + "\"]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestWave28OpenPermissionFailures covers the two places openFileSpec turns a
// refused system call into an Errno: reading a file the process may not read,
// and the writability probe that stands in for the open(2) a buffer-backed
// stream never makes. Both are EACCES, and both are what MRI 4.0.5 raises.
func TestWave28OpenPermissionFailures(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not refuse the owner here")
	}
	dir := t.TempDir()
	unreadable := filepath.Join(dir, "none.txt")
	readonly := filepath.Join(dir, "ro.txt")
	for _, p := range []string{unreadable, readonly} {
		if err := os.WriteFile(p, []byte("body"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readonly, 0o444); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chmod(unreadable, 0o644)
		_ = os.Chmod(readonly, 0o644)
	}()

	// The read of an unreadable file.
	got := runFS(t, `begin; File.open("`+slash(unreadable)+`"); rescue => e; p [e.class, e.message]; end`)
	want := "[Errno::EACCES, \"Permission denied @ rb_sysopen - " + slash(unreadable) + "\"]\n"
	if got != want {
		t.Errorf("unreadable: got %q, want %q", got, want)
	}
	// "r+" wants to write but does not truncate, so it is the writability probe —
	// not the read and not the truncate — that refuses it.
	got = runFS(t, `begin; File.open("`+slash(readonly)+`", "r+"); rescue => e; p [e.class, e.message]; end`)
	want = "[Errno::EACCES, \"Permission denied @ rb_sysopen - " + slash(readonly) + "\"]\n"
	if got != want {
		t.Errorf("read-only: got %q, want %q", got, want)
	}
}

// TestWave28FlockDescriptorReuse covers lockDescriptor's second visit: a lock
// taken and then CHANGED — shared after exclusive — must act on the descriptor
// already held, because flock(2) upgrades and downgrades a lock in place and
// taking a second descriptor would instead have the process block against itself.
func TestWave28FlockDescriptorReuse(t *testing.T) {
	p := flockScratch(t)
	opens := 0
	restore := swapFlockSeam(func(string) (int, error) { opens++; return 7, nil },
		func(int, int) error { return nil },
		func(int) error { return nil })
	defer restore()
	src := `f = File.open("` + p + `", "r"); p [f.flock(File::LOCK_EX), f.flock(File::LOCK_SH)]; f.close`
	if got := runFS(t, src); got != "[0, 0]\n" {
		t.Fatalf("got %q", got)
	}
	if opens != 1 {
		t.Errorf("opened %d descriptors for two locks, want 1", opens)
	}
}

// TestWave28FlockClosedWhileWaiting covers the rb_io_check_closed rb_file_flock
// performs after each 0.1 s wait: a blocking lock request is not a commitment,
// and a stream closed from elsewhere while the request waits ends it with an
// IOError rather than spinning forever. The waiting fiber releases the GVL, which
// is what lets the closing thread run at all.
func TestWave28FlockClosedWhileWaiting(t *testing.T) {
	p := flockScratch(t)
	restore := swapFlockSeam(func(string) (int, error) { return 7, nil },
		func(int, int) error { return syscall.EAGAIN },
		func(int) error { return nil })
	defer restore()
	src := `
f = File.open("` + p + `", "r")
t = Thread.new do
  begin
    f.flock(File::LOCK_EX)
    "no error"
  rescue IOError => e
    e.message
  end
end
Thread.pass until t.status == "sleep" || !t.status
f.close
p t.value
`
	if got := runFS(t, src); got != "\"closed stream\"\n" {
		t.Errorf("got %q, want %q", got, "\"closed stream\"\n")
	}
}

// TestWave28FileRecvClassFallback covers fileRecvClass's guard. File.open and
// File.new always reach it with the class they were called on, so the fallback
// cannot be produced from Ruby — it is there because a native method's receiver
// is an object.Value and nothing in the type says it is a class.
func TestWave28FileRecvClassFallback(t *testing.T) {
	cls := newClass("Fallback", nil)
	if got := fileRecvClass(object.NilV, cls); got != cls {
		t.Errorf("a non-class receiver must fall back to the given class, got %v", got)
	}
	other := newClass("Receiver", nil)
	if got := fileRecvClass(other, cls); got != other {
		t.Errorf("a class receiver must be used as-is, got %v", got)
	}
}
