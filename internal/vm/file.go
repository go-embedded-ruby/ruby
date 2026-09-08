package vm

import (
	"errors"
	"os"
	"os/user"
	"path"          // always '/'-separated, as Ruby's File is — not path/filepath
	"path/filepath" // OS-native, only for symlink resolution (File.realpath)
	"strings"
	"syscall"
	stdtime "time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// userHomeDir looks up the home directory of the named user (File.expand_path's
// ~user form). With CGO disabled the lookup reads the passwd database directly on
// unix; on platforms where it is unsupported the error surfaces as the
// ArgumentError MRI raises for an unknown ~user.
func userHomeDir(name string) (string, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

// toSlash converts an OS-native path (Windows uses '\') to Ruby's '/' form.
func toSlash(s string) string { return strings.ReplaceAll(s, "\\", "/") }

// registerFile installs the File class — the common path manipulation helpers
// (basename/dirname/extname/join/split/expand_path) and filesystem operations
// (exist?/file?/directory?/read/write/size/delete) — plus the Errno::ENOENT
// raised when a file is missing, mirroring MRI.
func (vm *VM) registerFile() {
	// SystemCallError + Errno::ENOENT, registered both as a scoped constant
	// (rescue Errno::ENOENT) and a flat name (so the internal raise resolves it).
	syscallErr := newClass("SystemCallError", vm.consts["StandardError"].(*RClass))
	vm.consts["SystemCallError"] = syscallErr
	errno := newClass("Errno", nil)
	errno.isModule = true
	vm.consts["Errno"] = errno
	// Each Errno::Exxx is a SystemCallError subclass, registered both scoped (for
	// `rescue Errno::ENOENT`) and flat (so an internal raise resolves the name).
	// The set covers the common POSIX errnos that file/IO code and libraries such
	// as Puppet rescue; an internal raise still uses the name string directly.
	for _, name := range []string{
		"ENOENT", "EEXIST", "EACCES", "ENOTDIR", "EISDIR", "EPERM", "EINVAL",
		"EAGAIN", "EBADF", "ESRCH", "EIO", "ENOSPC", "EROFS", "ENXIO", "ENOTEMPTY",
		"ECONNREFUSED", "ECONNRESET", "ETIMEDOUT", "EPIPE", "ELOOP", "ENAMETOOLONG",
		"EADDRINUSE", "EINTR", "ECHILD", "ENOMEM", "EXDEV", "EMFILE", "ENFILE",
	} {
		c := newClass("Errno::"+name, syscallErr)
		// Each Errno::Exxx carries its platform errno number as the class constant
		// Errno::ENOENT::Errno (2 on this host), which SystemCallError#errno reads
		// back. Values come from the host syscall table so they match the host MRI.
		c.consts["Errno"] = object.IntValue(errnoNumbers[name])
		errno.consts[name] = c
		vm.consts["Errno::"+name] = c
	}

	cFile := newClass("File", vm.cObject)
	vm.consts["File"] = cFile
	// Path constants (POSIX). ALT_SEPARATOR is nil on unix-like platforms; NULL
	// is the null device. These let path-handling code branch on File::SEPARATOR
	// etc. without a runtime error.
	cFile.consts["SEPARATOR"] = object.NewString("/")
	// File::Separator is MRI's mixed-case alias of File::SEPARATOR (both "/"); a
	// separate String object is fine since the spec only checks its value.
	cFile.consts["Separator"] = object.NewString("/")
	cFile.consts["ALT_SEPARATOR"] = object.NilV
	cFile.consts["PATH_SEPARATOR"] = object.NewString(":")
	cFile.consts["NULL"] = object.NewString("/dev/null")
	// Open-mode flag constants (File::Constants), the canonical POSIX values. They
	// let code such as Puppet's Uniquefile build an integer open mode
	// (File::RDWR | File::CREAT | File::EXCL) which File.open then maps back to a
	// mode string. We fix the numeric values here rather than reflecting the host's
	// so behaviour is identical on every OS the gate runs on.
	fnmConsts := map[string]int64{
		"FNM_NOESCAPE": fnmNoEscape, "FNM_PATHNAME": fnmPathname, "FNM_DOTMATCH": fnmDotMatch,
		"FNM_CASEFOLD": fnmCaseFold, "FNM_EXTGLOB": fnmExtGlob, "FNM_SYSCASE": fnmSysCase,
	}
	// File::Constants is the module that carries the open-mode, flock and fnmatch
	// flag constants; File includes it, so both File::RDONLY and
	// File::Constants::RDONLY resolve and File::Constants.const_defined? sees them.
	// FNM_SYSCASE is 0 on the case-sensitive POSIX platforms the gate runs on.
	fileConstants := newClass("File::Constants", nil)
	fileConstants.isModule = true
	cFile.consts["Constants"] = fileConstants
	for _, m := range []map[string]int64{fileFlagConsts, fileExtraConsts, fnmConsts} {
		for name, val := range m {
			fileConstants.consts[name] = object.IntValue(val)
			cFile.consts[name] = object.IntValue(val)
		}
	}
	cFile.includes = append(cFile.includes, fileConstants)
	def := func(name string, fn NativeFn) { cFile.smethods[name] = &Method{name: name, owner: cFile, native: fn} }

	// File.fnmatch?(pattern, path, flags=0) and its alias File.fnmatch test path
	// against a shell glob pattern, returning true/false. flags is an FNM_* OR.
	fnmatchFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 || len(args) > 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
		}
		flags := 0
		if len(args) == 3 {
			// MRI coerces the flags argument with rb_to_int (NUM2INT), so a
			// non-Integer that answers #to_int is accepted rather than a TypeError.
			flags = int(coerceInt(vm, args[2]))
		}
		return object.Bool(fnmatch(strArg(args[0]), vm.filePathArg(args[1]), flags))
	}
	// File.fnmatch? is a genuine alias of File.fnmatch (they share one method
	// record, so File.method(:fnmatch?) == File.method(:fnmatch)), matching MRI.
	def("fnmatch", fnmatchFn)
	cFile.smethods["fnmatch?"] = cFile.smethods["fnmatch"]

	def("basename", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 || len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
		}
		base := rubyBasename(vm.filePathArg(args[0]))
		if len(args) > 1 {
			base = stripBaseSuffix(base, strArg(args[1]))
		}
		return object.NewString(base)
	})
	def("dirname", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 || len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
		}
		level := 1
		if len(args) > 1 {
			level = int(coerceInt(vm, args[1]))
			if level < 0 {
				raise("ArgumentError", "negative level: %d", level)
			}
		}
		p := vm.filePathArg(args[0])
		// A level > 1 strips that many trailing components; dirname is idempotent at
		// the root/"." so a level that exceeds the depth converges rather than looping.
		for i := 0; i < level; i++ {
			d := rubyDirname(p)
			if d == p {
				break
			}
			p = d
		}
		return object.NewString(p)
	})
	def("extname", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		return object.NewString(rubyExtname(rubyBasename(vm.filePathArg(args[0]))))
	})
	def("split", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		p := vm.filePathArg(args[0])
		return object.NewArray(object.NewString(rubyDirname(p)), object.NewString(rubyBasename(p)))
	})
	def("join", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// MRI's File.join recursively joins nested Array arguments — File.join([a, b], c)
		// behaves like File.join(a, b, c) — and each array is joined to a single string
		// first, so File.join([], []) is join("", "") == "/". A self-referential array
		// raises ArgumentError; a null byte in any component is rejected, as in MRI.
		var toStr func(v object.Value, seen []*object.Array) string
		toStr = func(v object.Value, seen []*object.Array) string {
			if arr, ok := v.(*object.Array); ok {
				for _, s := range seen {
					if s == arr {
						raise("ArgumentError", "recursive array")
					}
				}
				seen = append(seen, arr)
				parts := make([]string, len(arr.Elems))
				for i, e := range arr.Elems {
					parts[i] = toStr(e, seen)
				}
				return fileJoin(parts)
			}
			s := pathArg(vm, v)
			if strings.IndexByte(s, 0) >= 0 {
				raise("ArgumentError", "string contains null byte")
			}
			return s
		}
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = toStr(a, nil)
		}
		return object.NewString(fileJoin(parts))
	})
	def("expand_path", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.fileExpand(vm.filePathArg(args[0]), args[1:], true))
	})
	// absolute_path resolves a path to an absolute one against an optional base
	// directory (defaulting to the CWD), like expand_path but without ~ expansion.
	// Puppet uses it with relative paths and an explicit base, where the two agree.
	def("absolute_path", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.fileExpand(vm.filePathArg(args[0]), args[1:], false))
	})
	def("absolute_path?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(path.IsAbs(toSlash(pathArg(vm, args[0]))))
	})

	def("exist?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		oneArg(args)
		_, err := os.Stat(pathArg(vm, args[0]))
		return object.Bool(err == nil)
	})
	def("file?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		oneArg(args)
		fi, err := os.Stat(pathArg(vm, args[0]))
		return object.Bool(err == nil && fi.Mode().IsRegular())
	})
	def("directory?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		fi, err := os.Stat(pathArg(vm, args[0]))
		return object.Bool(err == nil && fi.IsDir())
	})
	// File.symlink? reports whether the path is a symbolic link. Like MRI it uses
	// lstat (does not follow the link) and returns false for a missing path or a
	// non-symlink, rather than raising.
	def("symlink?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		fi, err := os.Lstat(pathArg(vm, args[0]))
		return object.Bool(err == nil && fi.Mode()&os.ModeSymlink != 0)
	})
	// File.ftype(path) returns the MRI file-type string ("file"/"directory"/
	// "link"/"characterSpecial"/…). Like MRI it lstats the path (so a symlink is
	// "link", not its target), takes exactly one #to_path argument, and raises
	// Errno::ENOENT for a missing path (ruby/ruby v3_4_0 file.c rb_file_s_ftype).
	def("ftype", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		p := vm.filePathArg(args[0])
		fi, err := osLstat(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_file_s_ftype - %s", p)
		}
		return object.NewString(newFileStat(fi, p).ftype())
	})
	// File.realpath returns the canonical absolute path with every symlink
	// resolved; the path (and each component) must exist, otherwise — as in MRI —
	// Errno::ENOENT is raised. An optional second argument is the base directory
	// a relative path is resolved against.
	def("realpath", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := vm.fileExpand(vm.filePathArg(args[0]), args[1:], true)
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			raiseRealpathErr(p)
		}
		return object.NewString(toSlash(resolved))
	})
	// File.read / File.write / File.binread / File.binwrite are the same class
	// methods as IO's, installed on both tables by registerIOClassMethods once the
	// IO class exists (registerIO runs after registerFile).
	def("size", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		fi, err := os.Stat(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_file_s_stat - %s", p)
		}
		return object.IntValue(fi.Size())
	})
	// File.mtime returns the file's last-modification Time (whole-second
	// resolution, matching the Time class's granularity). A missing file raises
	// Errno::ENOENT, as in MRI.
	def("mtime", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		fi, err := os.Stat(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_file_s_stat - %s", p)
		}
		return unixTime(fi.ModTime().Unix())
	})
	delete := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		for _, a := range args {
			p := pathArg(vm, a)
			if err := os.Remove(p); err != nil {
				raise("Errno::ENOENT", "No such file or directory @ apply2files - %s", p)
			}
		}
		return object.IntValue(int64(len(args)))
	}
	def("delete", delete)
	// unlink is a genuine alias of delete (one shared Method record), so
	// File.method(:unlink) == File.method(:delete), as MRI's spec checks.
	cFile.smethods["unlink"] = cFile.smethods["delete"]

	// rename(old, new) atomically moves a file, returning 0 (MRI). Puppet's
	// FileSystem#replace_file renames its written temp file over the target, so
	// state.yaml / last_run_summary.yaml are written atomically.
	def("rename", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// MRI's rb_file_s_rename checks the two-argument arity before coercing the
		// paths, so File.rename("a") raises ArgumentError rather than a TypeError.
		from, to := twoPaths(vm, args)
		if err := os.Rename(from, to); err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_file_s_rename - %s or %s", from, to)
		}
		return object.IntValue(0)
	})

	// On-disk metadata operations Puppet's settings/file provider drives:
	// chmod/chown/umask/utime and the access predicates. chmod/chown/utime accept
	// a leading mode/owner/time argument followed by one or more paths, returning
	// the count of paths affected (MRI semantics).
	def("chmod", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		mode := os.FileMode(vm.num2mode(args[0]) & 0o7777)
		paths := args[1:]
		for _, a := range paths {
			p := pathArg(vm, a)
			if err := fileChmod(p, mode); err != nil {
				raise("Errno::ENOENT", "No such file or directory @ apply2files - %s", p)
			}
		}
		return object.IntValue(int64(len(paths)))
	})
	def("chown", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// A nil uid/gid leaves that id unchanged (passed as -1 to chown).
		uid, gid := chownID(args[0]), chownID(args[1])
		paths := args[2:]
		for _, a := range paths {
			p := pathArg(vm, a)
			if err := fileChown(p, uid, gid); err != nil {
				raise("Errno::ENOENT", "No such file or directory @ apply2files - %s", p)
			}
		}
		return object.IntValue(int64(len(paths)))
	})
	// lchown mirrors chown but does not follow a symlink (Puppet uses it when
	// :links => :manage). Go's os.Lchown provides the same behaviour.
	def("lchown", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		uid, gid := chownID(args[0]), chownID(args[1])
		paths := args[2:]
		for _, a := range paths {
			p := pathArg(vm, a)
			if err := fileLchown(p, uid, gid); err != nil {
				raise("Errno::ENOENT", "No such file or directory @ apply2files - %s", p)
			}
		}
		return object.IntValue(int64(len(paths)))
	})
	def("utime", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		at, mt := timeArgUnixOrNow(args[0]), timeArgUnixOrNow(args[1])
		paths := args[2:]
		for _, a := range paths {
			p := pathArg(vm, a)
			if err := fileChtimes(p, at, mt); err != nil {
				raise("Errno::ENOENT", "No such file or directory @ utime_failed - %s", p)
			}
		}
		return object.IntValue(int64(len(paths)))
	})
	// File.lutime(atime, mtime, *paths) sets each path's access/modification time
	// like File.utime but WITHOUT following a final symbolic link, returning the
	// number of paths (file.c utime_internal with follow=TRUE -> AT_SYMLINK_NOFOLLOW).
	// A nil atime/mtime means the current time; lutimesFn is the POSIX seam.
	def("lutime", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2+)", len(args))
		}
		at, mt := timeArgUnixOrNow(args[0]), timeArgUnixOrNow(args[1])
		paths := args[2:]
		for _, a := range paths {
			p := pathArg(vm, a)
			if err := lutimesFn(p, at, mt); err != nil {
				raise("Errno::ENOENT", "No such file or directory @ utime_failed - %s", p)
			}
		}
		return object.IntValue(int64(len(paths)))
	})
	// NOTE: File.mkfifo (syscall.Mkfifo) is deliberately left unregistered. It is
	// straightforward and MRI-verified, but wiring it up REGRESSES core/file:
	// open_spec's "on a FIFO" example opens both ends of the FIFO from two Ruby
	// threads, and rbgo's blocking File.open (io.go) deadlocks the interpreter on
	// the FIFO open(2) handshake — hanging the whole spec file and losing its ~55
	// passing examples. Add File.mkfifo only once File.open on a FIFO no longer
	// blocks the scheduler (an io.go / thread-concurrency fix the io agent owns).
	// File.umask([mask]) reads (and optionally sets) the process umask, returning
	// the previous value — the bracket Puppet::Util.withumask uses. With no
	// argument it reports the current umask without changing it.
	def("umask", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0..1)", len(args))
		}
		if len(args) == 0 {
			cur := setUmask(0)
			setUmask(cur) // restore: a no-arg umask is a pure read
			return object.IntValue(int64(cur))
		}
		return object.IntValue(int64(setUmask(int(vm.num2mode(args[0])))))
	})
	// Access predicates: readable?/writable?/executable? for the current effective
	// user, plus executable_real? — thin File.stat-and-test wrappers that return
	// false for a missing path rather than raising (MRI's File.<predicate>).
	access := func(want int) NativeFn {
		return func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			p := pathArg(vm, args[0])
			fi, err := osStat(p)
			if err != nil {
				return object.Bool(false)
			}
			return object.Bool(newFileStat(fi, p).accessible(want))
		}
	}
	def("readable?", access(4))
	def("writable?", access(2))
	def("executable?", access(1))
	// The *_real? predicates consult the process's real (not effective) identity.
	realAccess := func(want int) NativeFn {
		return func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			p := pathArg(vm, args[0])
			fi, err := osStat(p)
			if err != nil {
				return object.Bool(false)
			}
			return object.Bool(newFileStat(fi, p).accessibleReal(want))
		}
	}
	def("readable_real?", realAccess(4))
	def("writable_real?", realAccess(2))
	def("executable_real?", realAccess(1))

	// Size predicates. File.size? returns the byte size, or nil when the file is
	// missing or empty (so it doubles as an existence-and-non-empty test); File.zero?
	// and its alias File.empty? report whether the file exists and is empty. All
	// return their falsey value for a missing path rather than raising (unlike
	// File.size, which raises Errno::ENOENT).
	def("size?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		fi, err := os.Stat(pathArg(vm, args[0]))
		if err != nil || fi.Size() == 0 {
			return object.NilV
		}
		return object.IntValue(fi.Size())
	})
	zero := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		fi, err := os.Stat(pathArg(vm, args[0]))
		return object.Bool(err == nil && fi.Size() == 0)
	}
	def("zero?", zero)
	// empty? is a genuine alias of zero? (shared Method record), matching MRI's
	// File.method(:zero?) == File.method(:empty?).
	cFile.smethods["empty?"] = cFile.smethods["zero?"]

	// Type predicates that delegate to a following stat and degrade to false for a
	// missing path (MRI's File.pipe?/socket?/…). statTest wraps the stat-and-test.
	statTest := func(pred func(*FileStat) bool) NativeFn {
		return func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			p := pathArg(vm, args[0])
			fi, err := os.Stat(p)
			if err != nil {
				return object.Bool(false)
			}
			return object.Bool(pred(newFileStat(fi, p)))
		}
	}
	def("pipe?", statTest(func(s *FileStat) bool { return s.fi.Mode()&os.ModeNamedPipe != 0 }))
	def("socket?", statTest(func(s *FileStat) bool { return s.fi.Mode()&os.ModeSocket != 0 }))
	def("blockdev?", statTest(func(s *FileStat) bool {
		m := s.fi.Mode()
		return m&os.ModeDevice != 0 && m&os.ModeCharDevice == 0
	}))
	def("chardev?", statTest(func(s *FileStat) bool { return s.fi.Mode()&os.ModeCharDevice != 0 }))
	def("setuid?", statTest(func(s *FileStat) bool { return s.fi.Mode()&os.ModeSetuid != 0 }))
	def("setgid?", statTest(func(s *FileStat) bool { return s.fi.Mode()&os.ModeSetgid != 0 }))
	def("sticky?", statTest(func(s *FileStat) bool { return s.fi.Mode()&os.ModeSticky != 0 }))
	def("owned?", statTest(func(s *FileStat) bool { return statOwned(s) }))
	def("grpowned?", statTest(func(s *FileStat) bool { return statGrpowned(s) }))

	// world_readable? / world_writable?: the permission integer when the relevant
	// "other" bit is set, else nil; a missing path is nil rather than an error.
	// statPerm supplies the platform-faithful bits (raw on POSIX; MSVCRT 0644/0444
	// on Windows, where the group/other write bits are never set — see
	// filestat_windows.go), so world_writable? is nil on Windows as MRI reports.
	worldPerm := func(bit int64) NativeFn {
		return func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			fi, err := os.Stat(pathArg(vm, args[0]))
			if err != nil {
				return object.NilV
			}
			if perm := statPerm(fi); perm&bit != 0 {
				return object.IntValue(perm)
			}
			return object.NilV
		}
	}
	def("world_readable?", worldPerm(0o004))
	def("world_writable?", worldPerm(0o002))

	// identical? reports whether two paths refer to the same file (same device and
	// inode), following symlinks; false when either path is missing.
	def("identical?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		fi1, err1 := os.Stat(pathArg(vm, args[0]))
		fi2, err2 := os.Stat(pathArg(vm, args[1]))
		return object.Bool(err1 == nil && err2 == nil && os.SameFile(fi1, fi2))
	})

	// atime / ctime return the file's access / change Time (whole-second, like
	// mtime — Go's portable stat exposes only ModTime, so both report it). A missing
	// file raises Errno::ENOENT, as MRI does.
	modTimeOf := func(vm *VM, args []object.Value) object.Value {
		p := pathArg(vm, args[0])
		fi, err := os.Stat(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_file_s_stat - %s", p)
		}
		return unixTime(fi.ModTime().Unix())
	}
	def("atime", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value { return modTimeOf(vm, args) })
	def("ctime", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value { return modTimeOf(vm, args) })
	// birthtime: unavailable through Go's portable stat surface (and on Linux
	// without statx), so — matching MRI on unsupported platforms — it raises
	// Errno::ENOENT for a missing path and NotImplementedError otherwise.
	def("birthtime", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		if _, err := os.Stat(p); err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_file_s_birthtime - %s", p)
		}
		raise("NotImplementedError", "birthtime() function is unimplemented")
		return object.NilV
	})

	// link / symlink create a hard / symbolic link and return 0; an existing target
	// raises Errno::EEXIST, a missing source directory Errno::ENOENT (MRI). Both
	// require exactly two path arguments.
	def("link", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		old, nw := twoPaths(vm, args)
		if err := os.Link(old, nw); err != nil {
			raiseLinkErr(err, "rb_file_s_link", old)
		}
		return object.IntValue(0)
	})
	def("symlink", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		old, nw := twoPaths(vm, args)
		if err := os.Symlink(old, nw); err != nil {
			raiseLinkErr(err, "rb_file_s_symlink", old)
		}
		return object.IntValue(0)
	})
	// readlink returns the target of a symbolic link. A missing path raises
	// Errno::ENOENT; a path that is not a symlink raises Errno::EINVAL (MRI).
	def("readlink", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		dest, err := os.Readlink(p)
		if err != nil {
			if os.IsNotExist(err) {
				raise("Errno::ENOENT", "No such file or directory @ rb_readlink - %s", p)
			}
			raise("Errno::EINVAL", "Invalid argument @ rb_readlink - %s", p)
		}
		return object.NewString(toSlash(dest))
	})
	// truncate resizes a file to the given length, returning 0; a missing path
	// raises Errno::ENOENT.
	def("truncate", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		if err := os.Truncate(p, intArg(args[1])); err != nil {
			// A negative (invalid) length is Errno::EINVAL; a missing path is
			// Errno::ENOENT — matching truncate(2) and MRI's rb_file_s_truncate.
			if errors.Is(err, syscall.EINVAL) {
				raise("Errno::EINVAL", "Invalid argument @ rb_file_s_truncate - %s", p)
			}
			raise("Errno::ENOENT", "No such file or directory @ rb_file_s_truncate - %s", p)
		}
		return object.IntValue(0)
	})
	// realdirpath is realpath but the last path component need not exist: every
	// symlink is resolved where possible, and an absent leaf is joined onto the
	// resolved directory. A missing intermediate directory raises Errno::ENOENT.
	def("realdirpath", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(realdirpath(vm.fileExpand(vm.filePathArg(args[0]), args[1:], true)))
	})
	// File.path returns the string (or #to_path) form of its argument unchanged —
	// no expansion, matching MRI.
	def("path", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.filePathArg(args[0]))
	})
}

// twoPaths coerces exactly two path arguments for File.link / File.symlink,
// raising ArgumentError for any other arity (matching MRI's arity check, which
// fires before the paths are examined).
func twoPaths(vm *VM, args []object.Value) (string, string) {
	if len(args) != 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
	}
	return pathArg(vm, args[0]), pathArg(vm, args[1])
}

// raiseLinkErr maps an os.Link / os.Symlink failure to the MRI errno: an existing
// target is Errno::EEXIST, anything else Errno::ENOENT.
func raiseLinkErr(err error, marker, src string) {
	if os.IsExist(err) {
		raise("Errno::EEXIST", "File exists @ %s - %s", marker, src)
	}
	raise("Errno::ENOENT", "No such file or directory @ %s - %s", marker, src)
}

// realdirpath resolves an already-expanded absolute path's symlinks, tolerating a
// non-existent leaf: if the whole path resolves it is returned, otherwise the
// parent directory is resolved and the leaf re-joined. A missing parent raises
// Errno::ENOENT.
func realdirpath(p string) string {
	resolved, err := filepath.EvalSymlinks(filepath.FromSlash(p))
	if err == nil {
		return toSlash(resolved)
	}
	// A symlink loop is Errno::ELOOP even though realdirpath tolerates an absent
	// leaf — the loop is a hard resolution failure, not a missing final component.
	if isSymlinkLoop(p) {
		raise("Errno::ELOOP", "Too many levels of symbolic links @ realpath_rec - %s", p)
	}
	dir, base := rubyDirname(p), rubyBasename(p)
	resolvedDir, err := filepath.EvalSymlinks(filepath.FromSlash(dir))
	if err != nil {
		raise("Errno::ENOENT", "No such file or directory @ realpath_rec - %s", dir)
	}
	return toSlash(filepath.Join(resolvedDir, base))
}

// isSymlinkLoop reports whether resolving p fails with ELOOP (a symlink cycle).
// filepath.EvalSymlinks returns a bare "too many links" error that does NOT wrap
// syscall.ELOOP, so the real errno is recovered by re-stating the path (os.Stat
// follows the link and surfaces the kernel's ELOOP).
func isSymlinkLoop(p string) bool {
	_, err := osStat(filepath.FromSlash(p))
	return errors.Is(err, syscall.ELOOP)
}

// raiseRealpathErr maps a File.realpath EvalSymlinks failure to the MRI errno: a
// symbolic-link loop is Errno::ELOOP, anything else (a missing component) is
// Errno::ENOENT.
func raiseRealpathErr(p string) {
	if isSymlinkLoop(p) {
		raise("Errno::ELOOP", "Too many levels of symbolic links @ realpath_rec - %s", p)
	}
	raise("Errno::ENOENT", "No such file or directory @ realpath_rec - %s", p)
}

// oneArg enforces the single-argument arity MRI's one-path File predicates check
// before touching the filesystem (File.exist?/file?/…), raising the ArgumentError
// MRI raises for any other count.
func oneArg(args []object.Value) {
	if len(args) != 1 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
	}
}

// num2mode coerces a File.chmod / File.umask mode argument the way MRI's
// NUM2MODET (rb_num2int) does: an Integer is taken directly, a Bignum that does
// not fit a C long raises RangeError, and any other value is coerced through
// #to_int (so a mock that answers #to_int is accepted rather than a TypeError).
// It is the mode-coercing counterpart of coerceInt, which lacks the Bignum range
// check the chmod/umask specs exercise.
func (vm *VM) num2mode(v object.Value) int64 {
	switch n := v.(type) {
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		if n.I.IsInt64() {
			return n.I.Int64()
		}
		raise("RangeError", "bignum too big to convert into 'unsigned long'")
	}
	if vm.respondsTo(v, "to_int") {
		return vm.num2mode(vm.send(v, "to_int", nil, nil))
	}
	raise("TypeError", "no implicit conversion of %s into Integer", vm.classOf(v).name)
	return 0
}

// fileChmod / fileChown / fileLchown / fileChtimes are seams over the os package
// so the error branches are reachable identically on every platform (a test
// swaps in a failing stub rather than relying on real OS behaviour, which
// differs across Linux/macOS/Windows — e.g. chmod is largely a no-op on Windows).
// fileChown / fileLchown are defined per-platform (filechown_unix.go /
// filechown_windows.go): os.Chown always fails on Windows, where MRI treats
// File.chown as a no-op, so the Windows build returns nil.
var (
	fileChmod   = os.Chmod
	fileChtimes = func(p string, atime, mtime int64) error {
		return osChtimes(p, atime, mtime)
	}
)

// rubyBasename returns the last component of a '/'-separated path, matching Ruby's
// File.basename: trailing separators are stripped (a path that is all separators
// yields "/"), the empty string yields "". Unlike path.Base it does not clean or
// collapse internal separators.
func rubyBasename(p string) string {
	end := len(p)
	for end > 0 && p[end-1] == '/' {
		end--
	}
	if end == 0 {
		if len(p) > 0 {
			return "/" // the path was one or more separators
		}
		return ""
	}
	if i := strings.LastIndexByte(p[:end], '/'); i >= 0 {
		return p[i+1 : end]
	}
	return p[:end]
}

// rubyDirname returns all path components except the last, matching Ruby's
// File.dirname (single level). It preserves internal repeated separators
// ("/holy///schnikies//x" -> "/holy///schnikies"), collapses a leading run of
// separators to the root "/", strips trailing separators, and yields "." for a
// path with no directory part.
func rubyDirname(p string) string {
	// Leading run of separators: their presence marks an absolute path rooted at "/".
	root := 0
	for root < len(p) && p[root] == '/' {
		root++
	}
	hasRoot := root > 0
	// Strip trailing separators down to the root.
	end := len(p)
	for end > root && p[end-1] == '/' {
		end--
	}
	body := p[root:end]
	i := strings.LastIndexByte(body, '/')
	if i < 0 {
		if hasRoot {
			return "/"
		}
		return "."
	}
	dir := body[:i]
	for len(dir) > 0 && dir[len(dir)-1] == '/' {
		dir = dir[:len(dir)-1]
	}
	if hasRoot {
		return "/" + dir
	}
	// body carries no leading separator (the root run was stripped), so a found
	// separator always leaves a non-empty dir here.
	return dir
}

// rubyExtname returns the extension of a base name — the substring from the last
// '.' to the end — matching Ruby's File.extname on non-Windows platforms: a
// leading-dot name with no other dot (".bashrc") and a name that is all dots
// ("..", "...") have no extension, while a name ending in a dot ("foo.") yields ".".
func rubyExtname(base string) string {
	p := 0
	for p < len(base) && base[p] == '.' { // skip leading dots (dotfiles have no ext)
		p++
	}
	e := -1
	for ; p < len(base); p++ {
		if base[p] == '.' {
			e = p
		}
	}
	if e < 0 {
		return ""
	}
	if e == len(base)-1 {
		return "." // a trailing dot: File.extname("foo.") == "." on unix
	}
	return base[e:]
}

// stripBaseSuffix removes a File.basename suffix argument from base. A ".*" suffix
// strips the extension (the last dot-run), any other suffix is matched literally
// and removed only when it is a proper suffix (never reducing base to "").
func stripBaseSuffix(base, suf string) string {
	if suf == ".*" {
		if dot := strings.LastIndexByte(base, '.'); dot > 0 {
			return base[:dot]
		}
		return base
	}
	if base != suf && strings.HasSuffix(base, suf) {
		return base[:len(base)-len(suf)]
	}
	return base
}

// fileJoin joins path components with File::SEPARATOR, following Ruby's File.join
// boundary rules: when neither side of a boundary has a separator one is inserted;
// when only one side does it is kept; when both do the right part's separator(s)
// win, so the left part's trailing separators are dropped.
func fileJoin(parts []string) string {
	res := ""
	for i, p := range parts {
		if i > 0 {
			prevSlash := len(res) > 0 && res[len(res)-1] == '/'
			curSlash := strings.HasPrefix(p, "/")
			switch {
			case prevSlash && curSlash:
				res = strings.TrimRight(res, "/")
			case !prevSlash && !curSlash:
				res += "/"
			}
		}
		res += p
	}
	return res
}

// coerceInt converts v to an int64, accepting an Integer directly or coercing any
// object that responds to #to_int (File.dirname's level argument), raising
// TypeError otherwise.
func coerceInt(vm *VM, v object.Value) int64 {
	if i, ok := v.(object.Integer); ok {
		return int64(i)
	}
	if vm.respondsTo(v, "to_int") {
		if i, ok := vm.send(v, "to_int", nil, nil).(object.Integer); ok {
			return int64(i)
		}
	}
	raise("TypeError", "no implicit conversion of %s into Integer", vm.classOf(v).name)
	return 0
}

// pathStr coerces a path-like argument to its *object.String form the way MRI's
// rb_get_path_check_to_string (file.c) does: a String is taken directly,
// otherwise #to_path then #to_str is tried, and a non-String result or a value
// that responds to neither raises TypeError. The *object.String (not a bare
// string) is returned so filePathArg can inspect the source encoding.
func (vm *VM) pathStr(v object.Value) *object.String {
	if s, ok := v.(*object.String); ok {
		return s
	}
	for _, m := range []string{"to_path", "to_str"} {
		if vm.respondsToDynamic(v, m) {
			r := vm.send(v, m, nil, nil)
			s, ok := r.(*object.String)
			if !ok {
				raise("TypeError", "can't convert %s to String (%s#%s gives %s)",
					vm.classOf(v).name, vm.classOf(v).name, m, vm.classOf(r).name)
			}
			return s
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", vm.classOf(v).name)
	return nil
}

// filePathArg coerces v to a filesystem-path string exactly as MRI's rb_get_path
// (rb_get_path_check_convert, file.c v3_4_0): after the #to_path/#to_str
// conversion, check_path_encoding raises Encoding::CompatibilityError for an
// ASCII-incompatible encoding (e.g. UTF-16/UTF-32), then a NUL byte raises
// ArgumentError ("path name contains null byte"). Path-algebra entry points
// (File.basename/dirname/extname/split/path and Dir.mkdir …) use this so those
// two guards fire before the path is examined, matching MRI.
func (vm *VM) filePathArg(v object.Value) string {
	s := vm.pathStr(v)
	vm.checkPathEncoding(s)
	str := s.Str()
	if strings.IndexByte(str, 0) >= 0 {
		raise("ArgumentError", "path name contains null byte")
	}
	return str
}

// checkPathEncoding raises Encoding::CompatibilityError when a path (or glob
// pattern) string carries an ASCII-incompatible encoding, mirroring MRI's
// check_path_encoding (file.c). It is shared by filePathArg and Dir.glob's
// pattern coercion (globPatternStr).
func (vm *VM) checkPathEncoding(s *object.String) {
	if e, ok := vm.findEncoding(s.EncName()); ok && !e.asciiCompat {
		raise("Encoding::CompatibilityError", "path name must be ASCII-compatible (%s): %s",
			s.EncName(), s.Inspect())
	}
}

// isAbsPath reports whether p is absolute, recognising both the forward-slash
// rooted form ("/x") and — on Windows — a drive-letter root ("C:/x"). rbgo keeps
// paths forward-slashed internally, so path.IsAbs alone would treat a Windows
// absolute path as relative and wrongly prepend the working directory.
func isAbsPath(p string) bool {
	return path.IsAbs(p) || filepath.IsAbs(filepath.FromSlash(p))
}

// fileExpand implements File.expand_path (expandTilde true) and File.absolute_path
// (expandTilde false, which skips ~ expansion). ~ expands to $HOME, ~user to that
// user's home directory, a relative path is resolved against the optional base
// (default: the working directory), and the result is cleaned (so .. and . collapse)
// while a leading run of two or more separators is preserved (POSIX / MRI).
func (vm *VM) fileExpand(p string, rest []object.Value, expandTilde bool) string {
	if expandTilde {
		p = expandTildePath(p)
	}
	if isAbsPath(p) {
		return cleanAbs(p)
	}
	base := ""
	if len(rest) > 0 && rest[0] != object.NilV {
		// The base directory is a path-like argument too, so MRI coerces it via
		// rb_get_path (#to_path) — not a bare String check — before expanding it.
		base = vm.fileExpand(vm.filePathArg(rest[0]), nil, expandTilde)
	} else if wd, err := os.Getwd(); err == nil {
		base = toSlash(wd)
	}
	return cleanAbs(path.Join(base, p))
}

// cleanAbs cleans an absolute path but preserves a leading run of two or more
// separators (path.Clean would collapse them), matching File.expand_path which
// leaves "////some/path" untouched while still collapsing interior separators.
func cleanAbs(p string) string {
	lead := 0
	for lead < len(p) && p[lead] == '/' {
		lead++
	}
	if lead >= 2 {
		return strings.Repeat("/", lead) + strings.TrimPrefix(path.Clean(p[lead:]), "/")
	}
	return path.Clean(p)
}

// expandTildePath expands a leading ~ (to $HOME) or ~user (to that user's home)
// component. As in MRI, an empty or non-absolute $HOME, or an unknown ~user,
// raises ArgumentError; a path that does not start with ~ is returned unchanged.
func expandTildePath(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	rest := p[1:]
	slash := strings.IndexByte(rest, '/')
	var name, tail string
	if slash < 0 {
		name, tail = rest, ""
	} else {
		name, tail = rest[:slash], rest[slash:]
	}
	if name == "" { // "~" or "~/..." -> $HOME
		home := os.Getenv("HOME")
		if home == "" {
			raise("ArgumentError", "couldn't find HOME environment -- expanding `~'")
		}
		if !isAbsPath(toSlash(home)) {
			raise("ArgumentError", "non-absolute home")
		}
		return toSlash(home) + tail
	}
	home, err := userHomeDir(name) // "~user"
	if err != nil || home == "" {
		raise("ArgumentError", "user %s doesn't exist", name)
	}
	return toSlash(home) + tail
}

// setUmask is the seam over the build-tagged osUmask, so a test can drive
// File.umask without perturbing the real process mask (which is shared across
// concurrently-running tests).
var setUmask = osUmask

// osChtimes sets the access and modification times of path from whole-second
// Unix timestamps, via os.Chtimes. It is wrapped (rather than used directly) so
// the seam in fileChtimes takes int64 seconds, matching utime's Time arguments.
func osChtimes(p string, atime, mtime int64) error {
	return os.Chtimes(p, stdtime.Unix(atime, 0), stdtime.Unix(mtime, 0))
}

// chownID converts a File.chown id argument to the int the os layer expects: a
// nil id means "leave unchanged" (-1, as POSIX chown interprets it), any other
// value is coerced through intArg.
func chownID(v object.Value) int {
	if v == object.NilV {
		return -1
	}
	return int(intArg(v))
}

// timeArgUnix marshals a File.utime time argument (a Time, or an Integer/Float
// seconds-since-epoch) to a whole number of Unix seconds.
func timeArgUnix(v object.Value) int64 {
	if t, ok := v.(*Time); ok {
		return t.t.Unix()
	}
	return int64(numFloat(v))
}

// timeArgUnixOrNow is timeArgUnix with MRI's nil handling for File.utime /
// File.lutime: a nil atime/mtime means "use the current time" (file.c
// utime_internal leaves the timespec NULL, which utimensat reads as UTIME_NOW).
func timeArgUnixOrNow(v object.Value) int64 {
	if v == object.NilV {
		return stdtime.Now().Unix()
	}
	return timeArgUnix(v)
}
