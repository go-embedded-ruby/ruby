// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// callStat invokes a File::Stat instance method by name on a crafted FileStat,
// so every accessor's body is driven deterministically on any platform.
func callStat(t *testing.T, name string, st *FileStat) object.Value {
	t.Helper()
	vm := New(nil)
	m := vm.cFileStat.methods[name]
	if m == nil {
		t.Fatalf("File::Stat#%s not defined", name)
	}
	return m.native(vm, st, nil, nil)
}

// TestFileStatSpecialBits covers setuid?/setgid?/sticky?/world_readable?/blocks/
// rdev/dev_major/dev_minor/rdev_major/rdev_minor over crafted stats — both the
// present and absent branch of each — independent of platform.
func TestFileStatSpecialBits(t *testing.T) {
	set := &FileStat{fi: fakeInfo{mode: fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky | 0o755}}
	if callStat(t, "setuid?", set) != object.True || callStat(t, "setgid?", set) != object.True ||
		callStat(t, "sticky?", set) != object.True {
		t.Errorf("special bits set: expected all true")
	}
	plain := &FileStat{fi: fakeInfo{mode: 0o644}}
	if callStat(t, "setuid?", plain) != object.False || callStat(t, "setgid?", plain) != object.False ||
		callStat(t, "sticky?", plain) != object.False {
		t.Errorf("special bits clear: expected all false")
	}

	// world_readable?: perm int when other-read set, else nil.
	wr := &FileStat{fi: fakeInfo{mode: 0o644}}
	if got := callStat(t, "world_readable?", wr); got != object.Integer(0o644) {
		t.Errorf("world_readable? 0644: got %v want 420", got)
	}
	if got := callStat(t, "world_readable?", &FileStat{fi: fakeInfo{mode: 0o640}}); got != object.NilV {
		t.Errorf("world_readable? 0640: got %v want nil", got)
	}

	// hasSys numbers: rdev always Integer; blocks/dev_* Integer with Sys, nil without.
	withSys := &FileStat{fi: fakeInfo{mode: 0o644}, sys: statFields{rdev: 0x1234, dev: 0x0500, blocks: 8, hasSys: true}}
	if got := callStat(t, "rdev", withSys); got != object.Integer(0x1234) {
		t.Errorf("rdev: got %v", got)
	}
	if got := callStat(t, "blocks", withSys); got != object.Integer(8) {
		t.Errorf("blocks: got %v", got)
	}
	for _, name := range []string{"dev_major", "dev_minor", "rdev_major", "rdev_minor"} {
		if _, ok := callStat(t, name, withSys).(object.Integer); !ok {
			t.Errorf("%s with Sys: not an Integer", name)
		}
	}
	// Without Sys (Windows-shaped): blocks and dev_* report nil; rdev is Integer 0.
	noSys := &FileStat{fi: fakeInfo{mode: 0o644}}
	if callStat(t, "blocks", noSys) != object.NilV {
		t.Errorf("blocks without Sys: want nil")
	}
	for _, name := range []string{"dev_major", "dev_minor", "rdev_major", "rdev_minor"} {
		if callStat(t, name, noSys) != object.NilV {
			t.Errorf("%s without Sys: want nil", name)
		}
	}
	if callStat(t, "rdev", noSys) != object.Integer(0) {
		t.Errorf("rdev without Sys: want 0")
	}
}

// TestFileStatGrpowned covers grpowned?: true when the file gid is in the
// process group set (with Sys), false otherwise / without Sys.
func TestFileStatGrpowned(t *testing.T) {
	defer restoreIdentitySeams()
	statEgid = func() int { return 2000 }
	statGroups = func() []int { return nil }
	// On Windows grpowned? is always false (no gid model — statGrpowned returns
	// false regardless of the crafted gid), matching MRI on Windows.
	var wantMatch object.Value = object.True
	if runtime.GOOS == "windows" {
		wantMatch = object.False
	}
	if callStat(t, "grpowned?", &FileStat{fi: fakeInfo{}, sys: statFields{gid: 2000, hasSys: true}}) != wantMatch {
		t.Errorf("grpowned? matching egid: want %v", wantMatch)
	}
	if callStat(t, "grpowned?", &FileStat{fi: fakeInfo{}, sys: statFields{gid: 3, hasSys: true}}) != object.False {
		t.Errorf("grpowned? non-member: want false")
	}
	if callStat(t, "grpowned?", &FileStat{fi: fakeInfo{}, sys: statFields{gid: 2000, hasSys: false}}) != object.False {
		t.Errorf("grpowned? without Sys: want false")
	}
}

// TestFileStatRealAccess covers accessibleReal (and the *_real? predicates)
// through the real-id seams, exercising the owner triad.
func TestFileStatRealAccess(t *testing.T) {
	defer restoreRealSeams()
	st := &FileStat{fi: fakeInfo{mode: 0o700}, sys: statFields{uid: 1000, gid: 2000, hasSys: true}}
	statRuid = func() int { return 1000 }
	statRgid = func() int { return 2000 }
	if !st.accessibleReal(4) || !st.accessibleReal(2) || !st.accessibleReal(1) {
		t.Errorf("real owner triad rwx denied")
	}
	if callStat(t, "readable_real?", st) != object.True ||
		callStat(t, "writable_real?", st) != object.True ||
		callStat(t, "executable_real?", st) != object.True {
		t.Errorf("*_real? predicates: expected all true for owner 0700")
	}
}

// restoreRealSeams resets the real-id seams the real-access test overrides.
func restoreRealSeams() {
	statRuid = os.Getuid
	statRgid = os.Getgid
}

// TestStatRealSeamDefaults invokes the default real-id seams so their closures
// run at least once, independent of any override.
func TestStatRealSeamDefaults(t *testing.T) {
	restoreRealSeams()
	_ = statRuid()
	_ = statRgid()
}

// TestFileStatBirthtimeUnimplemented covers File::Stat#birthtime raising
// NotImplementedError (Go's portable stat cannot report a creation time).
func TestFileStatBirthtimeUnimplemented(t *testing.T) {
	vm := New(nil)
	m := vm.cFileStat.methods["birthtime"]
	if got := catchRaise(func() { m.native(vm, &FileStat{fi: fakeInfo{}}, nil, nil) }); got != "NotImplementedError" {
		t.Errorf("birthtime: got %q want NotImplementedError", got)
	}
}

// TestDevMajorMinor covers the device-number decomposition helpers.
func TestDevMajorMinor(t *testing.T) {
	// glibc encoding: makedev(0x12, 0x34) == 0x1200|0x34 for small numbers.
	dev := int64(0x1234)
	if devMajor(dev) != 0x12 {
		t.Errorf("devMajor(0x1234)=%#x want 0x12", devMajor(dev))
	}
	if devMinor(dev) != 0x34 {
		t.Errorf("devMinor(0x1234)=%#x want 0x34", devMinor(dev))
	}
}

// TestFileLevelPredicates drives the File.* class-method predicates over a real
// temp tree (present) and a missing path (degrade to false/nil/ENOENT), covering
// every new File-level branch.
func TestFileLevelPredicates(t *testing.T) {
	dir := slash(t.TempDir())
	reg := dir + "/reg.txt"
	if err := os.WriteFile(filepath.FromSlash(reg), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	wworld := dir + "/wworld.txt"
	if err := os.WriteFile(filepath.FromSlash(wworld), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.FromSlash(wworld), 0o666)
	priv := dir + "/priv.txt"
	if err := os.WriteFile(filepath.FromSlash(priv), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.FromSlash(priv), 0o600)

	// Two assertions read the POSIX permission model, which Windows lacks: it has
	// only a read-only bit, so a writable file always stats as MSVCRT 0644 and a
	// read-only one as 0444, and there is no group ownership. Both wants below are
	// verified against ruby 4.0.6 (aarch64-mingw-ucrt) on Windows — owned? true /
	// grpowned? false, and world_writable? nil (0644 has no other-write) while
	// world_readable? is 420 (0644) even for the file created 0600.
	ownWant := "[\"TrueClass\", \"TrueClass\"]\n"
	worldWant := "[\"Integer\", nil]\n"
	if runtime.GOOS == "windows" {
		ownWant = "[\"TrueClass\", \"FalseClass\"]\n"
		worldWant = "[\"NilClass\", 420]\n"
	}

	cases := []struct{ src, want string }{
		// Type predicates on a regular file are false; missing path is false.
		{`p [File.pipe?("` + reg + `"), File.socket?("` + reg + `"), File.blockdev?("` + reg + `"), File.chardev?("` + reg + `")]`,
			"[false, false, false, false]\n"},
		{`p [File.setuid?("` + reg + `"), File.setgid?("` + reg + `"), File.sticky?("` + reg + `")]`,
			"[false, false, false]\n"},
		{`p [File.pipe?("/no/x"), File.socket?("/no/x"), File.setuid?("/no/x"), File.blockdev?("/no/x"), File.chardev?("/no/x")]`,
			"[false, false, false, false, false]\n"},
		// owned?/grpowned? return booleans (runner owns the temp file; on Windows
		// MRI reports owned? true but grpowned? false — see ownWant above).
		{`p [File.owned?("` + reg + `").class.to_s, File.grpowned?("` + reg + `").class.to_s]`,
			ownWant},
		{`p [File.owned?("/no/x"), File.grpowned?("/no/x")]`, "[false, false]\n"},
		// world_readable?/world_writable?: Integer when the bit is set, else nil.
		{`p [File.world_readable?("` + reg + `").class.to_s, File.world_writable?("` + reg + `")]`,
			"[\"Integer\", nil]\n"},
		{`p [File.world_writable?("` + wworld + `").class.to_s, File.world_readable?("` + priv + `")]`,
			worldWant},
		{`p [File.world_readable?("/no/x"), File.world_writable?("/no/x")]`, "[nil, nil]\n"},
		// identical?: same path true, distinct false, missing false.
		{`p [File.identical?("` + reg + `", "` + reg + `"), File.identical?("` + reg + `", "` + priv + `"), File.identical?("` + reg + `", "/no/x")]`,
			"[true, false, false]\n"},
		// atime/ctime are Times.
		{`p [File.atime("` + reg + `").is_a?(Time), File.ctime("` + reg + `").is_a?(Time)]`, "[true, true]\n"},
		// real-access predicates: true for a readable/writable 0644 file owned by
		// the runner, false for a missing path.
		{`p [File.readable_real?("` + reg + `"), File.writable_real?("` + reg + `")]`, "[true, true]\n"},
		{`p [File.readable_real?("/no/x"), File.writable_real?("/no/x"), File.executable_real?("/no/x")]`,
			"[false, false, false]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// atime/ctime/birthtime on a missing path raise Errno::ENOENT; birthtime on an
	// existing file raises NotImplementedError.
	for _, expr := range []string{`File.atime("/no/x")`, `File.ctime("/no/x")`, `File.birthtime("/no/x")`} {
		if got := runFSErr(t, expr); got != "Errno::ENOENT" {
			t.Errorf("%s: got %q want Errno::ENOENT", expr, got)
		}
	}
	if got := runFSErr(t, `File.birthtime("`+reg+`")`); got != "NotImplementedError" {
		t.Errorf("File.birthtime(existing): got %q want NotImplementedError", got)
	}
}
