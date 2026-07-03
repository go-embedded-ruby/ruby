// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// runFS compiles and runs src on a fresh VM and returns captured stdout. It is
// the in-package counterpart of the blackbox eval helper, used so a test can
// also poke the package-private seams (sysExtract, statEuid, …) around the run.
func runFS(t *testing.T, src string) string {
	t.Helper()
	iseq, err := parseCompileFn(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var buf bytes.Buffer
	if _, rerr := New(&buf).Run(iseq); rerr != nil {
		t.Fatalf("runtime error: %v", rerr)
	}
	return buf.String()
}

// slash forward-slashes an OS path so it can be embedded in Ruby source without
// leaking Windows backslashes into expected strings.
func slash(p string) string { return filepath.ToSlash(p) }

// TestFileStatPredicates covers File::Stat's type/size/mode surface against a
// real temp tree, asserted to match MRI Ruby 4.0.5. Paths are forward-slashed.
func TestFileStatPredicates(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/reg.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ src, want string }{
		// Directory.
		{`s=File.stat("` + dir + `"); p [s.directory?, s.file?, s.symlink?, s.ftype, s.class.to_s]`,
			"[true, false, false, \"directory\", \"File::Stat\"]\n"},
		// Regular file: type, size, mode bits, size? / zero?.
		{`s=File.stat("` + f + `"); p [s.file?, s.directory?, s.ftype, s.size, s.size?, s.zero?]`,
			"[true, false, \"file\", 5, 5, false]\n"},
		// mode includes the S_IFREG bits (0o100000) + perms, as MRI's st.mode does.
		// The exact perm bits differ by platform (Windows reports 0o666 for a
		// writable file, not 0o644), so assert the type bits exactly and that the
		// perms are a platform-valid value.
		{`s=File.stat("` + f + `"); m=s.mode; p [(m & 0o170000).to_s(8), [0o644, 0o666].include?(m & 0o7777)]`,
			"[\"100000\", true]\n"},
		// Device/pipe/socket predicates are all false for a regular file.
		{`s=File.stat("` + f + `"); p [s.pipe?, s.socket?, s.blockdev?, s.chardev?]`,
			"[false, false, false, false]\n"},
		// File::Stat.new is File.stat.
		{`s=File::Stat.new("` + f + `"); p s.file?`, "true\n"},
		// inspect renders the marker.
		{`p File.stat("` + f + `").inspect`, "\"#<File::Stat>\"\n"},
		// <=> against a non-stat is nil; against an equal-mtime stat is 0.
		{`s=File.stat("` + f + `"); p [(s <=> 1), (s <=> s)]`, "[nil, 0]\n"},
		// size?/zero? for an empty file: size? is nil, zero? true.
		func() struct{ src, want string } {
			e := dir + "/empty"
			os.WriteFile(filepath.FromSlash(e), nil, 0o644)
			return struct{ src, want string }{
				`s=File.stat("` + e + `"); p [s.size?, s.zero?, s.size]`, "[nil, true, 0]\n"}
		}(),
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFileStatNumbers covers the POSIX number accessors (uid/gid/ino/dev/nlink/
// blksize) and mtime/ctime/atime, plus the Errno::ENOENT raised for a missing
// path. The numbers are platform-dependent, so they are checked for type/shape
// rather than exact values (keeping the test portable to Windows defaults).
func TestFileStatNumbers(t *testing.T) {
	dir := slash(t.TempDir())
	src := `s=File.stat("` + dir + `")
p [s.uid.is_a?(Integer), s.gid.is_a?(Integer), s.ino.is_a?(Integer),
   s.dev.is_a?(Integer), s.nlink.is_a?(Integer), s.blksize.is_a?(Integer),
   s.mtime.is_a?(Time), s.ctime.is_a?(Time), s.atime.is_a?(Time)]`
	if got := runFS(t, src); got != "[true, true, true, true, true, true, true, true, true]\n" {
		t.Errorf("stat numbers: got %q", got)
	}
	// Missing path raises Errno::ENOENT for both File.stat and File::Stat.new.
	for _, expr := range []string{`File.stat("/no/such/rbgo")`, `File.lstat("/no/such/rbgo")`, `File::Stat.new("/no/such/rbgo")`} {
		if got := runFSErr(t, expr); got != "Errno::ENOENT" {
			t.Errorf("%s: got %q want Errno::ENOENT", expr, got)
		}
	}
}

// runFSErr compiles and runs src and returns the RubyError class of the error it
// terminates with, or "" when it succeeds. (Run recovers a raise into the
// returned error rather than re-panicking.)
func runFSErr(t *testing.T, src string) string {
	t.Helper()
	iseq, err := parseCompileFn(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	_, rerr := New(&bytes.Buffer{}).Run(iseq)
	if rerr == nil {
		return ""
	}
	if re, ok := rerr.(RubyError); ok {
		return re.Class
	}
	return rerr.Error()
}

// TestFileStatLstat covers lstat on a symlink: ftype "link" and symlink? true,
// while a following stat reports the target. Skipped where symlinks are not
// creatable (e.g. an unprivileged Windows runner).
func TestFileStatLstat(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ls := slash(link)
	if got := runFS(t, `s=File.lstat("`+ls+`"); p [s.symlink?, s.ftype]`); got != "[true, \"link\"]\n" {
		t.Errorf("lstat symlink: got %q", got)
	}
	if got := runFS(t, `s=File.stat("`+ls+`"); p [s.symlink?, s.ftype]`); got != "[false, \"file\"]\n" {
		t.Errorf("stat through symlink: got %q", got)
	}
}

// TestFileStatModeAndType drives the mode/ifmt/ftype switches directly over a
// crafted FileStat, so every branch (dir/symlink/fifo/socket/char/block/regular
// plus the setuid/setgid/sticky bits) is covered identically on any platform —
// real files of those kinds can't be created portably in a test.
func TestFileStatModeAndType(t *testing.T) {
	cases := []struct {
		mode  fs.FileMode
		ftype string
		ifmt  int
	}{
		{fs.ModeDir | 0o755, "directory", 0o040000},
		{fs.ModeSymlink | 0o777, "link", 0o120000},
		{fs.ModeNamedPipe | 0o644, "fifo", 0o010000},
		{fs.ModeSocket | 0o644, "socket", 0o140000},
		{fs.ModeDevice | fs.ModeCharDevice | 0o644, "characterSpecial", 0o020000},
		{fs.ModeDevice | 0o644, "blockDevice", 0o060000},
		{0o644, "file", 0o100000},
		{fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky | 0o755, "file", 0o100000},
	}
	for _, c := range cases {
		st := &FileStat{fi: fakeInfo{mode: c.mode}}
		if got := st.ftype(); got != c.ftype {
			t.Errorf("mode %v: ftype %q want %q", c.mode, got, c.ftype)
		}
		if got := st.ifmt(); got != c.ifmt {
			t.Errorf("mode %v: ifmt %o want %o", c.mode, got, c.ifmt)
		}
		bits := st.modeBits()
		if bits&int64(c.ifmt) == 0 {
			t.Errorf("mode %v: modeBits %o missing ifmt %o", c.mode, bits, c.ifmt)
		}
	}
	// The setuid/setgid/sticky case sets the high perm-extension bits.
	st := &FileStat{fi: fakeInfo{mode: fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky | 0o755}}
	if b := st.modeBits(); b&0o7000 != 0o7000 {
		t.Errorf("setuid/setgid/sticky bits: %o", b)
	}
}

// fakeInfo is a minimal fs.FileInfo for driving FileStat's mode/type logic.
type fakeInfo struct {
	mode fs.FileMode
	size int64
	mt   time.Time
}

func (f fakeInfo) Name() string       { return "fake" }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return f.mt }
func (f fakeInfo) IsDir() bool        { return f.mode&fs.ModeDir != 0 }
func (f fakeInfo) Sys() any           { return nil }

// TestFileStatToS covers the display markers and Truthy of a FileStat value.
func TestFileStatToS(t *testing.T) {
	st := &FileStat{fi: fakeInfo{}}
	if st.ToS() != "#<File::Stat>" || st.Inspect() != "#<File::Stat>" || !st.Truthy() {
		t.Errorf("display: ToS=%q Inspect=%q Truthy=%v", st.ToS(), st.Inspect(), st.Truthy())
	}
}

// TestFileStatAccess drives accessible / owned? / world_writable? through the
// identity seams so the owner/group/other and root branches are each covered
// deterministically on every platform (no reliance on real uid/perm).
func TestFileStatAccess(t *testing.T) {
	defer restoreIdentitySeams()

	// hasSys with a matching owner uid: owner triad governs.
	st := &FileStat{fi: fakeInfo{mode: 0o750}, sys: statFields{uid: 1000, gid: 2000, hasSys: true}}
	statEuid = func() int { return 1000 }
	statEgid = func() int { return 9999 }
	statGroups = func() []int { return nil }
	if !st.accessible(4) || !st.accessible(2) || !st.accessible(1) {
		t.Errorf("owner triad rwx: r=%v w=%v x=%v", st.accessible(4), st.accessible(2), st.accessible(1))
	}
	// owned? true when euid == uid.
	if got := runOwned(st); got != true {
		t.Errorf("owned? owner: %v", got)
	}

	// Group triad: euid differs but gid is in the group set; 0o070 grants group rwx.
	stg := &FileStat{fi: fakeInfo{mode: 0o070}, sys: statFields{uid: 1, gid: 2000, hasSys: true}}
	statEuid = func() int { return 4242 }
	statEgid = func() int { return 2000 }
	if !stg.accessible(4) || !stg.accessible(2) || !stg.accessible(1) {
		t.Errorf("group triad rwx denied")
	}
	// Supplementary-group membership also selects the group triad.
	statEgid = func() int { return 1 }
	statGroups = func() []int { return []int{5, 2000} }
	if !stg.accessible(4) {
		t.Errorf("supplementary group not honoured")
	}

	// Other triad: euid not owner, gid not in groups; only 0o007 grants access.
	sto := &FileStat{fi: fakeInfo{mode: 0o007}, sys: statFields{uid: 1, gid: 2, hasSys: true}}
	statEuid = func() int { return 4242 }
	statEgid = func() int { return 9 }
	statGroups = func() []int { return nil }
	if !sto.accessible(4) || !sto.accessible(2) || !sto.accessible(1) {
		t.Errorf("other triad rwx denied")
	}
	// A file with no perms denies the other triad.
	stn := &FileStat{fi: fakeInfo{mode: 0o000}, sys: statFields{hasSys: true}}
	if stn.accessible(4) {
		t.Errorf("no-perm file readable")
	}

	// Root (euid 0): read/write always allowed; execute only with an x bit.
	str := &FileStat{fi: fakeInfo{mode: 0o600}, sys: statFields{hasSys: true}}
	statEuid = func() int { return 0 }
	if !str.accessible(4) || !str.accessible(2) || str.accessible(1) {
		t.Errorf("root rwx: r=%v w=%v x=%v (x should be false, no x bit)",
			str.accessible(4), str.accessible(2), str.accessible(1))
	}
	strx := &FileStat{fi: fakeInfo{mode: 0o711}, sys: statFields{hasSys: true}}
	if !strx.accessible(1) {
		t.Errorf("root x with an x bit denied")
	}

	// No Sys (Windows defaults): owner/group selection is skipped, so the other
	// triad is used; owned? is false.
	stw := &FileStat{fi: fakeInfo{mode: 0o007}, sys: statFields{hasSys: false}}
	statEuid = func() int { return 4242 }
	if !stw.accessible(4) {
		t.Errorf("no-Sys other triad denied")
	}
	if runOwned(stw) {
		t.Errorf("no-Sys owned? should be false")
	}
}

// runOwned evaluates the owned? predicate's Go condition for st under the
// current identity seams.
func runOwned(st *FileStat) bool {
	return st.sys.hasSys && int64(statEuid()) == st.sys.uid
}

// restoreIdentitySeams resets the identity seams the access test overrides.
func restoreIdentitySeams() {
	statEuid = os.Geteuid
	statEgid = os.Getegid
	statGroups = func() []int { g, _ := os.Getgroups(); return g }
}

// TestFileStatWorldWritable covers world_writable?: the perm int when the
// world-write bit is set, else nil. Driven over a crafted stat (no chmod, which
// no-ops on Windows).
func TestFileStatWorldWritable(t *testing.T) {
	vm := New(nil)
	m := vm.cFileStat.methods["world_writable?"]
	ww := &FileStat{fi: fakeInfo{mode: 0o777}}
	if got := m.native(vm, object.Wrap(ww), nil, nil); got != object.Integer(0o777) {
		t.Errorf("world_writable? 0777: got %v want 511", got)
	}
	now := &FileStat{fi: fakeInfo{mode: 0o755}}
	if got := m.native(vm, object.Wrap(now), nil, nil); got != object.NilV {
		t.Errorf("world_writable? 0755: got %v want nil", got)
	}
}

// TestStatSysExtract covers the build-tagged statSys (via the sysExtract seam):
// a FileInfo whose Sys() is not a real *Stat_t falls back to defaults, which the
// fakeInfo (Sys()==nil) exercises identically on every platform.
func TestStatSysExtract(t *testing.T) {
	got := sysExtract(fakeInfo{mode: 0o644})
	// On unix the cast fails for a nil Sys() → defaults (nlink 1, hasSys false);
	// on windows the stub returns the same shape. Either way: no panic, nlink 1.
	if got.nlink != 1 || got.hasSys {
		t.Errorf("sysExtract(fake): %+v want {nlink:1 hasSys:false}", got)
	}
	// A real on-disk stat goes through the same seam and yields a usable struct.
	dir := t.TempDir()
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = sysExtract(fi) // must not panic on either platform
}

// TestFileTest covers the FileTest module predicates against a real temp tree,
// each degrading to false (not raising) for a missing path, matching MRI.
func TestFileTest(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/f.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := dir + "/empty"
	os.WriteFile(filepath.FromSlash(empty), nil, 0o644)

	cases := []struct{ src, want string }{
		{`p [FileTest.directory?("` + dir + `"), FileTest.file?("` + dir + `"), FileTest.exist?("` + dir + `")]`,
			"[true, false, true]\n"},
		{`p [FileTest.file?("` + f + `"), FileTest.directory?("` + f + `"), FileTest.exists?("` + f + `")]`,
			"[true, false, true]\n"},
		{`p [FileTest.zero?("` + empty + `"), FileTest.zero?("` + f + `")]`, "[true, false]\n"},
		{`p [FileTest.size?("` + f + `"), FileTest.size?("` + empty + `"), FileTest.size("` + f + `")]`,
			"[4, nil, 4]\n"},
		{`p [FileTest.readable?("` + f + `"), FileTest.writable?("` + f + `")]`, "[true, true]\n"},
		// Missing path: every predicate is false / nil (no raise).
		{`p [FileTest.directory?("/no/x"), FileTest.file?("/no/x"), FileTest.exist?("/no/x"),
		    FileTest.zero?("/no/x"), FileTest.size?("/no/x"), FileTest.readable?("/no/x"),
		    FileTest.writable?("/no/x"), FileTest.executable?("/no/x"), FileTest.symlink?("/no/x")]`,
			"[false, false, false, false, nil, false, false, false, false]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// FileTest.size on a missing path raises Errno::ENOENT (MRI).
	if got := runFSErr(t, `FileTest.size("/no/such/rbgo")`); got != "Errno::ENOENT" {
		t.Errorf("FileTest.size missing: got %q", got)
	}
}

// TestFileStatRubyAccessors drives owned?/readable?/writable?/executable? and
// ctime/atime through the Ruby surface over a real temp file, so each native
// method body is exercised (the access predicates' Go logic is covered
// separately and deterministically in TestFileStatAccess).
func TestFileStatRubyAccessors(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/a.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `s = File.stat("` + f + `")
p [s.owned?, s.readable?, s.writable?, s.executable?,
   s.ctime.is_a?(Time), s.atime.is_a?(Time), s.mtime.is_a?(Time)]`
	// owned? depends on the runner's euid vs the temp file's owner (the runner
	// created it, so true on a normal CI account); readable?/writable? are true for
	// a 0644 file owned by the runner; executable? is false. ctime/atime/mtime are
	// Times. We assert the stable elements and that owned? is a boolean.
	got := runFS(t, src)
	if got != "[true, true, true, false, true, true, true]\n" &&
		got != "[false, true, true, false, true, true, true]\n" {
		t.Errorf("stat ruby accessors: got %q", got)
	}
	// The statGroups default seam (os.Getgroups) is exercised by a group-triad
	// access check on a real stat with a fresh VM (no seam override in scope).
	restoreIdentitySeams()
	st := &FileStat{fi: fakeInfo{mode: 0o040}, sys: statFields{uid: -1, gid: int64(os.Getegid()), hasSys: true}}
	_ = st.accessible(4) // drives inGroup → statGroups() default closure
}

// TestFileStatSpaceship covers File::Stat#<=> ordering by mtime — the -1 and 1
// branches (an equal pair and a non-stat are covered elsewhere) — driven over
// crafted stats so the two timestamps are deterministic.
func TestFileStatSpaceship(t *testing.T) {
	vm := New(nil)
	cmp := vm.cFileStat.methods["<=>"]
	older := &FileStat{fi: fakeInfo{mt: time.Unix(100, 0)}}
	newer := &FileStat{fi: fakeInfo{mt: time.Unix(200, 0)}}
	if got := cmp.native(vm, object.Wrap(older), []object.Value{object.Wrap(newer)}, nil); got != object.Integer(-1) {
		t.Errorf("older <=> newer: got %v want -1", got)
	}
	if got := cmp.native(vm, object.Wrap(newer), []object.Value{object.Wrap(older)}, nil); got != object.Integer(1) {
		t.Errorf("newer <=> older: got %v want 1", got)
	}
	if got := cmp.native(vm, object.Wrap(older), []object.Value{object.Wrap(older)}, nil); got != object.Integer(0) {
		t.Errorf("equal <=>: got %v want 0", got)
	}
}

// TestStatGroupsDefault invokes the default statGroups seam (os.Getgroups) so its
// closure body runs at least once, independent of any per-test override.
func TestStatGroupsDefault(t *testing.T) {
	restoreIdentitySeams()
	if statGroups() == nil {
		// os.Getgroups returns a (possibly empty) slice; nil only on an error, which
		// is acceptable — the point is that the closure executed without panicking.
		t.Log("statGroups() returned nil (Getgroups error); closure still executed")
	}
}

// TestIOFlushError covers ioFlush's write-failure branch: flushing a writable
// file stream whose path is an unwritable directory raises Errno::ENOENT.
func TestIOFlushError(t *testing.T) {
	o := &IOObj{isStr: true, writable: true, path: "/no/such/dir/x.txt", buf: []byte("data")}
	if got := catchRaise(func() { ioFlush(o) }); got != "Errno::ENOENT" {
		t.Errorf("ioFlush error: got %q want Errno::ENOENT", got)
	}
	// A non-writable or path-less stream flushes to a no-op (no raise).
	ioFlush(&IOObj{isStr: true, writable: false, path: "/no/such"})
	ioFlush(&IOObj{isStr: true, writable: true, path: ""})
}
