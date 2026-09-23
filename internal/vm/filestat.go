// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io/fs"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// statFields are the POSIX-only stat numbers (uid/gid/inode/device/link-count/
// block-size). They come from the platform-specific syscall.Stat_t on Unix and
// take zero/one defaults on Windows; statSys (build-tagged) fills them. hasSys
// reports whether the underlying Sys() carried a real *Stat_t — false makes
// uid/gid/… report their defaults without pretending to be real numbers.
type statFields struct {
	uid, gid, ino, dev, nlink, blksize int64
	rdev, blocks                       int64
	hasSys                             bool
	// atime/ctime/btime are the stat struct's access, status-change and birth
	// times in whole seconds, filled by statTimestamps rather than by the
	// build-tagged statSys: the FIELDS are spelled differently per platform
	// (Linux's Atim vs the BSDs' Atimespec) but reading them is one shared rule,
	// so it lives in the shared file where the POSIX lanes can cover it.
	// hasBtime is false where the platform has no creation time at all.
	atime, ctime, btime int64
	hasBtime            bool
}

// FileStat is the Ruby File::Stat value: a thin shell over Go's fs.FileInfo plus
// the POSIX numbers extracted by the build-tagged statSys. It is a first-class
// object.Value (like Time), dispatched through vm.cFileStat.
type FileStat struct {
	fi   fs.FileInfo
	sys  statFields
	path string
}

func (s *FileStat) ToS() string  { return "#<File::Stat>" }
func (s *FileStat) Truthy() bool { return true }

// Inspect renders File::Stat#inspect, MRI's rb_stat_inspect (ruby/ruby v3_4_0
// file.c:1080): the member list in that exact order, dev and rdev in
// hexadecimal, mode in octal with a leading zero, and the rest through their
// own inspect form. birthtime appears only where the platform has a creation
// time, which is MRI's HAVE_STRUCT_STAT_ST_BIRTHTIMESPEC guard.
//
// It is the NATIVE form rather than a dispatch through the Ruby accessors
// because rb_stat_inspect calls the C member functions directly: a subclass
// that overrides #uid does not change what #inspect prints. One implementation
// also means Kernel#p, which reaches this method, prints exactly what
// stat.inspect returns.
func (s *FileStat) Inspect() string {
	var b strings.Builder
	b.WriteString("#<File::Stat ")
	for _, m := range []struct {
		name string
		val  string
	}{
		{"dev", "0x" + strconv.FormatInt(s.sys.dev, 16)},
		{"ino", strconv.FormatInt(s.sys.ino, 10)},
		{"mode", "0" + strconv.FormatInt(s.modeBits(), 8)},
		{"nlink", strconv.FormatInt(s.sys.nlink, 10)},
		{"uid", strconv.FormatInt(s.sys.uid, 10)},
		{"gid", strconv.FormatInt(s.sys.gid, 10)},
		{"rdev", "0x" + strconv.FormatInt(s.sys.rdev, 16)},
		{"size", strconv.FormatInt(s.fi.Size(), 10)},
		{"blksize", strconv.FormatInt(s.sys.blksize, 10)},
		{"blocks", s.blocksValue().Inspect()},
		{"atime", statTime(s.sys.atime).Inspect()},
		{"mtime", statTime(s.fi.ModTime().Unix()).Inspect()},
		{"ctime", statTime(s.sys.ctime).Inspect()},
	} {
		if b.Len() > len("#<File::Stat ") {
			b.WriteString(", ")
		}
		b.WriteString(m.name + "=" + m.val)
	}
	if s.sys.hasBtime {
		b.WriteString(", birthtime=" + statTime(s.sys.btime).Inspect())
	}
	b.WriteString(">")
	return b.String()
}

// sysExtract is the seam over the build-tagged statSys, so a test can swap in a
// stub and drive both the real-Sys and missing-Sys branches identically on every
// platform.
var sysExtract = statSys

// statEuid / statEgid / statGroups are the identity seams used by the
// readable?/writable?/executable? access checks. They default to the running
// process's effective ids; tests override them to exercise the owner/group/other
// branches deterministically (and they are no-ops on Windows, where Geteuid
// returns -1 and the "root" short-circuit then governs).
var (
	statEuid   = os.Geteuid
	statEgid   = os.Getegid
	statGroups = func() []int { g, _ := os.Getgroups(); return g }
)

// statRuid / statRgid are the real-user-id seams behind the *_real? access
// predicates (readable_real?/writable_real?/executable_real?), which check the
// process's real (rather than effective) identity. Tests override them to drive
// the owner/group/other branches deterministically.
var (
	statRuid = os.Getuid
	statRgid = os.Getgid
)

// newFileStat builds a FileStat from an fs.FileInfo, extracting the POSIX fields.
func newFileStat(fi fs.FileInfo, path string) *FileStat {
	return &FileStat{fi: fi, sys: statTimestamps(fi, sysExtract(fi)), path: path}
}

// statTimestamps fills the access/status-change/birth times of a stat struct
// that statSys cannot name portably. MRI reads st_atime, st_ctime and
// st_birthtime straight off struct stat (ruby/ruby v3_4_0 file.c
// rb_file_s_atime / stat_atime), but Go's syscall.Stat_t spells them
// differently on each platform — Atim/Ctim/Mtim on Linux, Atimespec/Ctimespec/
// Birthtimespec on the BSDs and macOS, and Windows has no such struct at all —
// so naming a field in shared code does not compile. Reflection reads whichever
// one the platform provides, and leaves the value at the modification time when
// there is none, which is what rbgo reported for every platform before.
func statTimestamps(fi fs.FileInfo, sf statFields) statFields {
	mtime := fi.ModTime().Unix()
	sys := fi.Sys()
	sf.atime, sf.ctime = mtime, mtime
	if sec, ok := statTimeField(sys, "Atim", "Atimespec"); ok {
		sf.atime = sec
	}
	if sec, ok := statTimeField(sys, "Ctim", "Ctimespec"); ok {
		sf.ctime = sec
	}
	// The creation time exists on the BSDs and macOS (Birthtimespec) and on
	// Windows; Linux's struct stat has none without statx, and MRI raises
	// NotImplementedError there, so its absence is recorded rather than faked.
	if sec, ok := statTimeField(sys, "Birthtimespec", "Btim", "CreationTime"); ok {
		sf.btime, sf.hasBtime = sec, true
	}
	return sf
}

// statTimeField reads the whole seconds of the first timespec field of sys (a
// pointer to the platform's stat struct) that carries one of the given names. It
// works by reflection because the field names are platform-specific; a field
// that is not a struct with an integer Sec member is ignored, so a platform
// whose Sys() is some unrelated value simply reports nothing.
func statTimeField(sys any, names ...string) (int64, bool) {
	if sys == nil {
		return 0, false
	}
	v := reflect.ValueOf(sys)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, false
	}
	for _, name := range names {
		f := v.FieldByName(name)
		if !f.IsValid() || f.Kind() != reflect.Struct {
			continue
		}
		sec := f.FieldByName("Sec")
		if sec.IsValid() && sec.CanInt() {
			return sec.Int(), true
		}
	}
	return 0, false
}

// modeBits returns the full MRI-style st_mode: the permission and setuid/setgid/
// sticky bits in the low 12, OR'd with the file-type bits (S_IFDIR/S_IFREG/…) so
// that, as in MRI, `stat.mode` of a directory is 0o40000|perm.
func (s *FileStat) modeBits() int64 {
	m := s.fi.Mode()
	bits := int64(m.Perm()) // low 9 permission bits
	if m&fs.ModeSetuid != 0 {
		bits |= 0o4000
	}
	if m&fs.ModeSetgid != 0 {
		bits |= 0o2000
	}
	if m&fs.ModeSticky != 0 {
		bits |= 0o1000
	}
	return bits | int64(s.ifmt())
}

// blocksValue is File::Stat#blocks: the number of 512-byte blocks allocated, or
// nil on a platform with no POSIX stat behind it (Windows), which is what MRI
// reports where HAVE_STRUCT_STAT_ST_BLOCKS is undefined.
//
// It exists so the accessor and #inspect read ONE rule. They had already
// drifted: #inspect formatted the raw field and printed "blocks=0" on Windows
// where #blocks answers nil, which is the kind of disagreement a member-by-member
// comparison catches and a smoke test does not.
func (s *FileStat) blocksValue() object.Value {
	if !s.sys.hasSys {
		return object.NilV
	}
	return object.IntValue(s.sys.blocks)
}

// ifmt returns the POSIX S_IFMT type bits for the file's kind (the high-order
// part of st_mode), matching the constants MRI exposes through File::Stat#mode.
func (s *FileStat) ifmt() int {
	m := s.fi.Mode()
	switch {
	case m&fs.ModeDir != 0:
		return 0o040000 // S_IFDIR
	case m&fs.ModeSymlink != 0:
		return 0o120000 // S_IFLNK
	case m&fs.ModeNamedPipe != 0:
		return 0o010000 // S_IFIFO
	case m&fs.ModeSocket != 0:
		return 0o140000 // S_IFSOCK
	case m&fs.ModeDevice != 0:
		if m&fs.ModeCharDevice != 0 {
			return 0o020000 // S_IFCHR
		}
		return 0o060000 // S_IFBLK
	default:
		return 0o100000 // S_IFREG
	}
}

// ftype returns the MRI File::Stat#ftype string for the file's kind.
func (s *FileStat) ftype() string {
	m := s.fi.Mode()
	switch {
	case m&fs.ModeDir != 0:
		return "directory"
	case m&fs.ModeSymlink != 0:
		return "link"
	case m&fs.ModeNamedPipe != 0:
		return "fifo"
	case m&fs.ModeSocket != 0:
		return "socket"
	case m&fs.ModeCharDevice != 0:
		return "characterSpecial"
	case m&fs.ModeDevice != 0:
		return "blockDevice"
	default:
		return "file"
	}
}

// accessible reports whether the current effective user may act on the file for
// the given permission class (read=4/write=2/execute=1), choosing the owner,
// group or other permission triad the way POSIX eaccess does. Root (euid 0) may
// always read and write, and may execute when any execute bit is set — matching
// MRI's File::Stat#readable?/writable?/executable?.
func (s *FileStat) accessible(want int) bool {
	return s.permFor(statEuid(), statEgid(), want)
}

// accessibleReal is accessible against the process's real (not effective) user
// and group ids — the identity MRI's *_real? predicates consult.
func (s *FileStat) accessibleReal(want int) bool {
	return s.permFor(statRuid(), statRgid(), want)
}

// permFor is the shared owner/group/other permission decision for a given
// (uid, gid) identity. Root (uid 0) may always read and write, and may execute
// when any execute bit is set; otherwise the owner, group or other triad is
// chosen the way POSIX eaccess does. The supplementary-group set (statGroups)
// is consulted for the group triad regardless of which identity is checked.
func (s *FileStat) permFor(uid, gid, want int) bool {
	perm := int(s.fi.Mode().Perm())
	if uid == 0 {
		if want == 1 { // execute: at least one x bit must be set
			return perm&0o111 != 0
		}
		return true
	}
	var shift uint
	switch {
	case s.sys.hasSys && int64(uid) == s.sys.uid:
		shift = 6 // owner triad
	case s.sys.hasSys && inGroupFor(s.sys.gid, gid):
		shift = 3 // group triad
	default:
		shift = 0 // other triad
	}
	return perm&(want<<shift) != 0
}

// inGroupFor reports whether fileGid is the given primary gid or one of the
// process's supplementary groups (used to pick the group permission triad).
func inGroupFor(fileGid int64, gid int) bool {
	if int64(gid) == fileGid {
		return true
	}
	for _, g := range statGroups() {
		if int64(g) == fileGid {
			return true
		}
	}
	return false
}

// devMajor / devMinor decompose a device number into its major and minor parts.
// The encoding follows glibc's gnu_dev_major / gnu_dev_minor (the Linux layout,
// which CI runs against); on other platforms the result is still a stable
// integer, which is all MRI's File::Stat#dev_major / #rdev_major guarantee.
func devMajor(dev int64) int64 {
	d := uint64(dev)
	return int64((d>>8)&0xfff | ((d >> 32) &^ 0xfff))
}

func devMinor(dev int64) int64 {
	d := uint64(dev)
	return int64(d&0xff | ((d >> 12) &^ 0xff))
}

// devPart applies a major/minor decomposition to a device number, returning nil
// (as MRI does) when the platform carries no real POSIX device model.
func devPart(s *FileStat, dev int64, part func(int64) int64) object.Value {
	if !s.sys.hasSys {
		return object.NilV
	}
	return object.IntValue(part(dev))
}

// statTime wraps a stat timestamp as a Ruby Time (whole-second resolution, like
// the rest of rbgo's Time surface).
func statTime(unix int64) *Time { return unixTime(unix) }

// osStat / osLstat / osReadlink are seams over os.Stat / os.Lstat / os.Readlink
// so the missing-path and unreadable-link error branches are reachable without
// depending on real filesystem state.
var (
	osStat     = os.Stat
	osLstat    = os.Lstat
	osReadlink = os.Readlink
)

// statOrRaise stats path (following symlinks when follow is true), raising
// Errno::ENOENT — with MRI's rb_file_s_stat / rb_file_s_lstat marker — when the
// path is missing.
func statOrRaise(path string, follow bool) *FileStat {
	var fi fs.FileInfo
	var err error
	marker := "rb_file_s_lstat"
	if follow {
		fi, err = osStat(path)
		marker = "rb_file_s_stat"
	} else {
		fi, err = osLstat(path)
	}
	if err != nil {
		raise("Errno::ENOENT", "No such file or directory @ %s - %s", marker, path)
	}
	return newFileStat(fi, path)
}

// registerFileStat installs File::Stat (its instance methods), the File.stat /
// File.lstat constructors, and the FileTest module. It runs after registerFile
// (which created the File class and the Errno hierarchy) and after the prelude
// (so Comparable is available to mix in for File::Stat#<=> ordering).
func (vm *VM) registerFileStat() {
	cFile := vm.consts["File"].(*RClass)

	cStat := newClass("File::Stat", vm.cObject)
	vm.cFileStat = cStat
	cFile.consts["Stat"] = cStat
	vm.consts["File::Stat"] = cStat
	// Comparable gives <, <=, >, >= from #<=> (used by code that sorts stats by
	// mtime); the prelude registered it before this runs.
	if cmp, ok := vm.consts["Comparable"].(*RClass); ok {
		cStat.includes = append(cStat.includes, cmp)
	}

	// File::Stat.new(path) stats the path eagerly (following symlinks), the same
	// as File.stat — MRI raises Errno::ENOENT for a missing path here too.
	cStat.smethods["new"] = &Method{name: "new", owner: cStat, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return statOrRaise(pathArg(vm, args[0]), true)
	}}

	self := func(v object.Value) *FileStat { return v.(*FileStat) }
	d := func(name string, fn NativeFn) { cStat.define(name, fn) }

	d("directory?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).fi.Mode()&fs.ModeDir != 0)
	})
	d("file?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).fi.Mode().IsRegular())
	})
	d("symlink?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).fi.Mode()&fs.ModeSymlink != 0)
	})
	d("pipe?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).fi.Mode()&fs.ModeNamedPipe != 0)
	})
	d("socket?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).fi.Mode()&fs.ModeSocket != 0)
	})
	d("blockdev?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		m := self(v).fi.Mode()
		return object.Bool(m&fs.ModeDevice != 0 && m&fs.ModeCharDevice == 0)
	})
	d("chardev?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).fi.Mode()&fs.ModeCharDevice != 0)
	})
	d("ftype", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ftype())
	})
	d("mode", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).modeBits())
	})
	d("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).fi.Size())
	})
	d("size?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if sz := self(v).fi.Size(); sz > 0 {
			return object.IntValue(sz)
		}
		return object.NilV
	})
	d("zero?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).fi.Size() == 0)
	})
	d("uid", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).sys.uid)
	})
	d("gid", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).sys.gid)
	})
	d("ino", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).sys.ino)
	})
	d("dev", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).sys.dev)
	})
	d("nlink", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).sys.nlink)
	})
	d("blksize", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).sys.blksize)
	})
	d("owned?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(statOwned(self(v)))
	})
	d("readable?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).accessible(4))
	})
	d("writable?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).accessible(2))
	})
	d("executable?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).accessible(1))
	})
	d("world_writable?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		perm := statPerm(self(v).fi)
		if perm&0o002 != 0 {
			return object.IntValue(perm) // MRI returns the perm bits when world-writable
		}
		return object.NilV
	})
	d("mtime", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return statTime(self(v).fi.ModTime().Unix())
	})
	// File::Stat#inspect — see FileStat.Inspect, which Kernel#p reaches too.
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Inspect())
	})
	// ctime/atime are the stat struct's own st_ctime / st_atime (statTimestamps),
	// falling back to the modification time on a platform that reports neither.
	d("ctime", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return statTime(self(v).sys.ctime)
	})
	d("atime", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return statTime(self(v).sys.atime)
	})
	d("<=>", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*FileStat)
		if !ok {
			return object.NilV
		}
		a, b := self(v).fi.ModTime().Unix(), other.fi.ModTime().Unix()
		switch {
		case a < b:
			return object.IntValue(-1)
		case a > b:
			return object.IntValue(1)
		default:
			return object.IntValue(0)
		}
	})
	// Special mode bits, read straight off the file mode.
	d("setuid?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).fi.Mode()&fs.ModeSetuid != 0)
	})
	d("setgid?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).fi.Mode()&fs.ModeSetgid != 0)
	})
	d("sticky?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).fi.Mode()&fs.ModeSticky != 0)
	})
	// world_readable? mirrors world_writable?: the permission integer when the
	// other-read bit is set, else nil (MRI).
	d("world_readable?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		perm := statPerm(self(v).fi)
		if perm&0o004 != 0 {
			return object.IntValue(perm)
		}
		return object.NilV
	})
	// grpowned? is true when the file's gid is the process's effective gid or one
	// of its supplementary groups (false without real POSIX ids, i.e. on Windows).
	d("grpowned?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(statGrpowned(self(v)))
	})
	d("readable_real?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).accessibleReal(4))
	})
	d("writable_real?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).accessibleReal(2))
	})
	d("executable_real?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).accessibleReal(1))
	})
	// rdev is the device id a special file represents (0 for a regular file); it is
	// always an Integer, matching MRI on every platform.
	d("rdev", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).sys.rdev)
	})
	// blocks is the number of 512-byte blocks allocated; nil where the platform
	// cannot report it (Windows), a non-negative Integer otherwise.
	d("blocks", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).blocksValue()
	})
	// dev_major / dev_minor / rdev_major / rdev_minor decompose dev / rdev; nil on
	// platforms without the POSIX device model (Windows), an Integer otherwise.
	d("dev_major", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return devPart(self(v), self(v).sys.dev, devMajor)
	})
	d("dev_minor", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return devPart(self(v), self(v).sys.dev, devMinor)
	})
	d("rdev_major", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return devPart(self(v), self(v).sys.rdev, devMajor)
	})
	d("rdev_minor", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return devPart(self(v), self(v).sys.rdev, devMinor)
	})
	// birthtime is the stat struct's creation time where the platform has one (the
	// BSDs and macOS spell it st_birthtime). Linux's struct stat has none without
	// statx, and MRI raises NotImplementedError there with exactly this message,
	// so the absence is reported rather than faked.
	d("birthtime", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self(v)
		if !s.sys.hasBtime {
			raise("NotImplementedError", "birthtime() function is unimplemented")
		}
		return statTime(s.sys.btime)
	})

	// File.stat / File.lstat class methods on the File class.
	cFile.smethods["stat"] = &Method{name: "stat", owner: cFile, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return statOrRaise(pathArg(vm, args[0]), true)
	}}
	cFile.smethods["lstat"] = &Method{name: "lstat", owner: cFile, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return statOrRaise(pathArg(vm, args[0]), false)
	}}

	// File#stat / File#lstat: the stat of the open stream's own path. MRI's
	// rb_file_stat (ruby/ruby v3_4_0 file.c) fstats the descriptor, so it still
	// answers for a file that has been unlinked while open; rbgo's File streams
	// are buffered by path rather than held open on a descriptor, so the stat goes
	// through the path and an unlinked file is Errno::ENOENT — the one case the
	// two differ, tracked as a known gap rather than papered over.
	statOfStream := func(follow bool) NativeFn {
		return func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) != 0 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
			}
			o := self.(*IOObj)
			// Only the CLOSED check applies: MRI's rb_io_stat works on a
			// read-only stream, so ioCheckOpen (which also refuses one whose
			// write half is shut) is the wrong guard here.
			if o.closed {
				raise("IOError", "closed stream")
			}
			return statOrRaise(o.path, follow)
		}
	}
	cFile.define("stat", statOfStream(true))
	cFile.define("lstat", statOfStream(false))

	vm.registerFileTest()

	// Dir includes Enumerable — deferred here because registerDir runs before the
	// prelude that defines the module, and registerFileStat runs after it.
	vm.includeDirEnumerable()
}

// fileTestFunctions is MRI's define_filetest_function list (ruby/ruby v3_4_0
// file.c Init_File): every name there is installed on BOTH FileTest and File as
// one and the same C function, so File.exist? and FileTest.exist? are the same
// method. rbgo defines them on File (registerFile); registerFileTest re-exports
// the very same Method records on FileTest rather than reimplementing them, so
// the two surfaces cannot drift and an alias pair such as zero?/empty? stays a
// genuine alias on both.
var fileTestFunctions = []string{
	"directory?", "exist?", "readable?", "readable_real?", "world_readable?",
	"writable?", "writable_real?", "world_writable?", "executable?",
	"executable_real?", "file?", "zero?", "empty?", "size?", "size", "owned?",
	"grpowned?", "pipe?", "symlink?", "socket?", "blockdev?", "chardev?",
	"setuid?", "setgid?", "sticky?", "identical?",
}

// reexportSingletons installs src's singleton methods named in names onto dst,
// sharing the very same *Method record. Sharing is what makes an alias pair
// survive the copy: two names that resolve to one record on src still resolve to
// one record on dst, so File.method(:zero?) == File.method(:empty?) holds on
// FileTest too. A name src does not define is skipped rather than installing nil,
// which would turn a missing method into a crash at call time.
func reexportSingletons(dst, src *RClass, names []string) {
	for _, name := range names {
		if m, ok := src.smethods[name]; ok && m != nil {
			dst.smethods[name] = m
		}
	}
}

// registerFileTest installs the FileTest module — the predicate surface Puppet
// reaches for widely (directory?/file?/exist?/readable?/…) and that ruby/spec
// exercises through core/filetest. Each predicate is File's own method record
// (see fileTestFunctions), so the arity and #to_path coercion checks, the
// missing-path-is-false degradation and the alias identities are shared rather
// than duplicated.
func (vm *VM) registerFileTest() {
	mod := newClass("FileTest", nil)
	mod.isModule = true
	vm.consts["FileTest"] = mod
	reexportSingletons(mod, vm.consts["File"].(*RClass), fileTestFunctions)
}
