// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io/fs"
	"os"

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
}

// FileStat is the Ruby File::Stat value: a thin shell over Go's fs.FileInfo plus
// the POSIX numbers extracted by the build-tagged statSys. It is a first-class
// object.Value (like Time), dispatched through vm.cFileStat.
type FileStat struct {
	fi   fs.FileInfo
	sys  statFields
	path string
}

func (s *FileStat) ToS() string     { return "#<File::Stat>" }
func (s *FileStat) Inspect() string { return "#<File::Stat>" }
func (s *FileStat) Truthy() bool    { return true }

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
	return &FileStat{fi: fi, sys: sysExtract(fi), path: path}
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

// osStat / osLstat are seams over os.Stat / os.Lstat so the missing-path error
// branch is reachable without depending on real filesystem state.
var (
	osStat  = os.Stat
	osLstat = os.Lstat
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
		s := self(v)
		return object.Bool(s.sys.hasSys && int64(statEuid()) == s.sys.uid)
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
		s := self(v)
		perm := int64(s.fi.Mode().Perm())
		if perm&0o002 != 0 {
			return object.IntValue(perm) // MRI returns the perm bits when world-writable
		}
		return object.NilV
	})
	d("mtime", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return statTime(self(v).fi.ModTime().Unix())
	})
	// ctime/atime: Go's fs.FileInfo exposes only ModTime portably, so both report
	// the modification time (whole-second). Puppet reads mtime; ctime/atime are
	// provided for completeness and never raise.
	d("ctime", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return statTime(self(v).fi.ModTime().Unix())
	})
	d("atime", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return statTime(self(v).fi.ModTime().Unix())
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
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Inspect())
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
		perm := int64(self(v).fi.Mode().Perm())
		if perm&0o004 != 0 {
			return object.IntValue(perm)
		}
		return object.NilV
	})
	// grpowned? is true when the file's gid is the process's effective gid or one
	// of its supplementary groups (false without real POSIX ids, i.e. on Windows).
	d("grpowned?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self(v)
		return object.Bool(s.sys.hasSys && inGroupFor(s.sys.gid, statEgid()))
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
		s := self(v)
		if !s.sys.hasSys {
			return object.NilV
		}
		return object.IntValue(s.sys.blocks)
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
	// birthtime: Go's portable stat surface (fs.FileInfo) does not expose the
	// creation time, and it is unavailable on Linux without statx, so — as MRI does
	// on unsupported platforms/filesystems — it raises NotImplementedError.
	d("birthtime", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		raise("NotImplementedError", "birthtime() function is unimplemented")
		return object.NilV
	})

	// File.stat / File.lstat class methods on the File class.
	cFile.smethods["stat"] = &Method{name: "stat", owner: cFile, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return statOrRaise(pathArg(vm, args[0]), true)
	}}
	cFile.smethods["lstat"] = &Method{name: "lstat", owner: cFile, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return statOrRaise(pathArg(vm, args[0]), false)
	}}

	vm.registerFileTest()
}

// registerFileTest installs the FileTest module — the predicate surface Puppet
// reaches for widely (directory?/file?/exist?/readable?/…). Each predicate is a
// thin stat-and-test that returns false for a missing path rather than raising,
// matching MRI's FileTest.
func (vm *VM) registerFileTest() {
	mod := newClass("FileTest", nil)
	mod.isModule = true
	vm.consts["FileTest"] = mod
	sdef := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// statOf stats path (following symlinks) returning nil for a missing path, so
	// each predicate degrades to false rather than raising.
	statOf := func(p string) *FileStat {
		fi, err := osStat(p)
		if err != nil {
			return nil
		}
		return newFileStat(fi, p)
	}

	sdef("exist?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		_, err := osStat(pathArg(vm, args[0]))
		return object.Bool(err == nil)
	})
	mod.smethods["exists?"] = mod.smethods["exist?"]
	sdef("directory?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s := statOf(pathArg(vm, args[0]))
		return object.Bool(s != nil && s.fi.Mode()&fs.ModeDir != 0)
	})
	sdef("file?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s := statOf(pathArg(vm, args[0]))
		return object.Bool(s != nil && s.fi.Mode().IsRegular())
	})
	sdef("symlink?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		fi, err := osLstat(pathArg(vm, args[0]))
		return object.Bool(err == nil && fi.Mode()&fs.ModeSymlink != 0)
	})
	sdef("zero?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s := statOf(pathArg(vm, args[0]))
		return object.Bool(s != nil && s.fi.Size() == 0)
	})
	sdef("size", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		s := statOf(p)
		if s == nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_file_s_stat - %s", p)
		}
		return object.IntValue(s.fi.Size())
	})
	sdef("size?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s := statOf(pathArg(vm, args[0]))
		if s == nil || s.fi.Size() == 0 {
			return object.NilV
		}
		return object.IntValue(s.fi.Size())
	})
	sdef("readable?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s := statOf(pathArg(vm, args[0]))
		return object.Bool(s != nil && s.accessible(4))
	})
	sdef("writable?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s := statOf(pathArg(vm, args[0]))
		return object.Bool(s != nil && s.accessible(2))
	})
	sdef("executable?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s := statOf(pathArg(vm, args[0]))
		return object.Bool(s != nil && s.accessible(1))
	})
}
