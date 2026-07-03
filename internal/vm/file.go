package vm

import (
	"os"
	"path"          // always '/'-separated, as Ruby's File is — not path/filepath
	"path/filepath" // OS-native, only for symlink resolution (File.realpath)
	"strings"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// toSlash converts an OS-native path (Windows uses '\') to Ruby's '/' form.
func toSlash(s string) string { return strings.ReplaceAll(s, "\\", "/") }

// registerFile installs the File class — the common path manipulation helpers
// (basename/dirname/extname/join/split/expand_path) and filesystem operations
// (exist?/file?/directory?/read/write/size/delete) — plus the Errno::ENOENT
// raised when a file is missing, mirroring MRI.
func (vm *VM) registerFile() {
	// SystemCallError + Errno::ENOENT, registered both as a scoped constant
	// (rescue Errno::ENOENT) and a flat name (so the internal raise resolves it).
	syscallErr := newClass("SystemCallError", object.Kind[*RClass](vm.consts["StandardError"]))
	vm.consts["SystemCallError"] = object.Wrap(syscallErr)
	errno := newClass("Errno", nil)
	errno.isModule = true
	vm.consts["Errno"] = object.Wrap(errno)
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
		errno.consts[name] = object.Wrap(c)
		vm.consts["Errno::"+name] = object.Wrap(c)
	}

	cFile := newClass("File", vm.cObject)
	vm.consts["File"] = object.Wrap(cFile)
	// Path constants (POSIX). ALT_SEPARATOR is nil on unix-like platforms; NULL
	// is the null device. These let path-handling code branch on File::SEPARATOR
	// etc. without a runtime error.
	cFile.consts["SEPARATOR"] = object.Wrap(object.NewString("/"))
	cFile.consts["ALT_SEPARATOR"] = object.NilVal()
	cFile.consts["PATH_SEPARATOR"] = object.Wrap(object.NewString(":"))
	cFile.consts["NULL"] = object.Wrap(object.NewString("/dev/null"))
	// Open-mode flag constants (File::Constants), the canonical POSIX values. They
	// let code such as Puppet's Uniquefile build an integer open mode
	// (File::RDWR | File::CREAT | File::EXCL) which File.open then maps back to a
	// mode string. We fix the numeric values here rather than reflecting the host's
	// so behaviour is identical on every OS the gate runs on.
	for name, val := range fileFlagConsts {
		cFile.consts[name] = object.IntValue(val)
	}
	def := func(name string, fn NativeFn) { cFile.smethods[name] = &Method{name: name, owner: cFile, native: fn} }

	def("basename", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		base := path.Base(pathArg(vm, args[0]))
		if len(args) > 1 {
			suf := strArg(args[1])
			if suf == ".*" {
				if ext := fileExt(base); ext != "" {
					base = base[:len(base)-len(ext)]
				}
			} else if strings.HasSuffix(base, suf) && base != suf {
				base = base[:len(base)-len(suf)]
			}
		}
		return object.Wrap(object.NewString(base))
	})
	def("dirname", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(path.Dir(pathArg(vm, args[0]))))
	})
	def("extname", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(fileExt(path.Base(pathArg(vm, args[0])))))
	})
	def("split", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		return object.Wrap(object.NewArray(object.Wrap(object.NewString(path.Dir(p))), object.Wrap(object.NewString(path.Base(p)))))
	})
	def("join", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// MRI's File.join flattens nested Array arguments, so File.join([a, b], c)
		// behaves like File.join(a, b, c).
		var parts []string
		var add func(v object.Value)
		add = func(v object.Value) {
			if arr, ok := object.KindOK[*object.Array](v); ok {
				for _, e := range arr.Elems {
					add(e)
				}
				return
			}
			parts = append(parts, pathArg(vm, v))
		}
		for _, a := range args {
			add(a)
		}
		return object.Wrap(object.NewString(fileJoin(parts)))
	})
	def("expand_path", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(fileExpand(pathArg(vm, args[0]), args[1:])))
	})
	// absolute_path resolves a path to an absolute one against an optional base
	// directory (defaulting to the CWD), like expand_path but without ~ expansion.
	// Puppet uses it with relative paths and an explicit base, where the two agree.
	def("absolute_path", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(fileExpand(pathArg(vm, args[0]), args[1:])))
	})
	def("absolute_path?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(path.IsAbs(toSlash(pathArg(vm, args[0]))))))
	})

	def("exist?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		_, err := os.Stat(pathArg(vm, args[0]))
		return object.BoolValue(bool(object.Bool(err == nil)))
	})
	def("file?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		fi, err := os.Stat(pathArg(vm, args[0]))
		return object.BoolValue(bool(object.Bool(err == nil && fi.Mode().IsRegular())))
	})
	def("directory?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		fi, err := os.Stat(pathArg(vm, args[0]))
		return object.BoolValue(bool(object.Bool(err == nil && fi.IsDir())))
	})
	// File.symlink? reports whether the path is a symbolic link. Like MRI it uses
	// lstat (does not follow the link) and returns false for a missing path or a
	// non-symlink, rather than raising.
	def("symlink?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		fi, err := os.Lstat(pathArg(vm, args[0]))
		return object.BoolValue(bool(object.Bool(err == nil && fi.Mode()&os.ModeSymlink != 0)))
	})
	// File.realpath returns the canonical absolute path with every symlink
	// resolved; the path (and each component) must exist, otherwise — as in MRI —
	// Errno::ENOENT is raised. An optional second argument is the base directory
	// a relative path is resolved against.
	def("realpath", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := fileExpand(pathArg(vm, args[0]), args[1:])
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ realpath_rec - %s", p)
		}
		return object.Wrap(object.NewString(toSlash(resolved)))
	})
	def("read", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		b, err := os.ReadFile(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", p)
		}
		return object.Wrap(object.NewString(string(b)))
	})
	def("write", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		data := []byte(strArg(args[1]))
		if err := os.WriteFile(p, data, 0o644); err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", p)
		}
		return object.IntValue(int64(len(data)))
	})
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
		return object.Wrap(&Time{t: gotime.FromUnix(fi.ModTime().Unix())})
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
	def("unlink", delete)

	// rename(old, new) atomically moves a file, returning 0 (MRI). Puppet's
	// FileSystem#replace_file renames its written temp file over the target, so
	// state.yaml / last_run_summary.yaml are written atomically.
	def("rename", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		from, to := pathArg(vm, args[0]), pathArg(vm, args[1])
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
		mode := os.FileMode(intArg(args[0]) & 0o7777)
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
		at, mt := timeArgUnix(args[0]), timeArgUnix(args[1])
		paths := args[2:]
		for _, a := range paths {
			p := pathArg(vm, a)
			if err := fileChtimes(p, at, mt); err != nil {
				raise("Errno::ENOENT", "No such file or directory @ utime_failed - %s", p)
			}
		}
		return object.IntValue(int64(len(paths)))
	})
	// File.umask([mask]) reads (and optionally sets) the process umask, returning
	// the previous value — the bracket Puppet::Util.withumask uses. With no
	// argument it reports the current umask without changing it.
	def("umask", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			cur := setUmask(0)
			setUmask(cur) // restore: a no-arg umask is a pure read
			return object.IntValue(int64(cur))
		}
		return object.IntValue(int64(setUmask(int(intArg(args[0])))))
	})
	// Access predicates: readable?/writable?/executable? for the current effective
	// user, plus executable_real? — thin File.stat-and-test wrappers that return
	// false for a missing path rather than raising (MRI's File.<predicate>).
	access := func(want int) NativeFn {
		return func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			p := pathArg(vm, args[0])
			fi, err := osStat(p)
			if err != nil {
				return object.BoolValue(bool(object.Bool(false)))
			}
			return object.BoolValue(bool(object.Bool(newFileStat(fi, p).accessible(want))))
		}
	}
	def("readable?", access(4))
	def("writable?", access(2))
	def("executable?", access(1))
	def("executable_real?", access(1))
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

// fileExt returns the extension of a base name, treating a leading-dot name with
// no other dot (".bashrc") as having no extension — matching Ruby's File.extname.
func fileExt(base string) string {
	i := strings.LastIndexByte(base, '.')
	if i <= 0 {
		return "" // no dot, or a leading-dot dotfile (".bashrc")
	}
	return base[i:] // includes a lone trailing dot: "a." -> "."
}

// fileJoin joins path components with a single separator, collapsing a separator
// that would otherwise double at a boundary — Ruby's File.join.
func fileJoin(parts []string) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			prevSlash := b.Len() > 0 && b.String()[b.Len()-1] == '/'
			curSlash := strings.HasPrefix(p, "/")
			switch {
			case prevSlash && curSlash:
				p = strings.TrimPrefix(p, "/")
			case !prevSlash && !curSlash:
				b.WriteByte('/')
			}
		}
		b.WriteString(p)
	}
	return b.String()
}

// isAbsPath reports whether p is absolute, recognising both the forward-slash
// rooted form ("/x") and — on Windows — a drive-letter root ("C:/x"). rbgo keeps
// paths forward-slashed internally, so path.IsAbs alone would treat a Windows
// absolute path as relative and wrongly prepend the working directory.
func isAbsPath(p string) bool {
	return path.IsAbs(p) || filepath.IsAbs(filepath.FromSlash(p))
}

// fileExpand implements File.expand_path: ~ expands to the home directory, a
// relative path is resolved against the optional base (default: the working
// directory), and the result is cleaned (so .. and . collapse).
func fileExpand(p string, rest []object.Value) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = toSlash(home) + strings.TrimPrefix(p, "~")
		}
	}
	if isAbsPath(p) {
		return path.Clean(p)
	}
	base := ""
	if len(rest) > 0 {
		base = fileExpand(strArg(rest[0]), nil)
	} else if wd, err := os.Getwd(); err == nil {
		base = toSlash(wd)
	}
	return path.Clean(path.Join(base, p))
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
	if t, ok := object.KindOK[*Time](v); ok {
		return t.t.ToUnix()
	}
	return timeSeconds(v)
}
