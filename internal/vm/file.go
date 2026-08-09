package vm

import (
	"os"
	"os/user"
	"path"          // always '/'-separated, as Ruby's File is — not path/filepath
	"path/filepath" // OS-native, only for symlink resolution (File.realpath)
	"strings"
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
		errno.consts[name] = c
		vm.consts["Errno::"+name] = c
	}

	cFile := newClass("File", vm.cObject)
	vm.consts["File"] = cFile
	// Path constants (POSIX). ALT_SEPARATOR is nil on unix-like platforms; NULL
	// is the null device. These let path-handling code branch on File::SEPARATOR
	// etc. without a runtime error.
	cFile.consts["SEPARATOR"] = object.NewString("/")
	cFile.consts["ALT_SEPARATOR"] = object.NilV
	cFile.consts["PATH_SEPARATOR"] = object.NewString(":")
	cFile.consts["NULL"] = object.NewString("/dev/null")
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
		if len(args) < 1 || len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
		}
		base := rubyBasename(pathArg(vm, args[0]))
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
		p := pathArg(vm, args[0])
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
		return object.NewString(rubyExtname(rubyBasename(pathArg(vm, args[0]))))
	})
	def("split", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		p := pathArg(vm, args[0])
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
		return object.NewString(fileExpand(pathArg(vm, args[0]), args[1:], true))
	})
	// absolute_path resolves a path to an absolute one against an optional base
	// directory (defaulting to the CWD), like expand_path but without ~ expansion.
	// Puppet uses it with relative paths and an explicit base, where the two agree.
	def("absolute_path", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(fileExpand(pathArg(vm, args[0]), args[1:], false))
	})
	def("absolute_path?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(path.IsAbs(toSlash(pathArg(vm, args[0]))))
	})

	def("exist?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		_, err := os.Stat(pathArg(vm, args[0]))
		return object.Bool(err == nil)
	})
	def("file?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
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
	// File.realpath returns the canonical absolute path with every symlink
	// resolved; the path (and each component) must exist, otherwise — as in MRI —
	// Errno::ENOENT is raised. An optional second argument is the base directory
	// a relative path is resolved against.
	def("realpath", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := fileExpand(pathArg(vm, args[0]), args[1:], true)
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ realpath_rec - %s", p)
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
				return object.Bool(false)
			}
			return object.Bool(newFileStat(fi, p).accessible(want))
		}
	}
	def("readable?", access(4))
	def("writable?", access(2))
	def("executable?", access(1))
	def("executable_real?", access(1))

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
	def("empty?", zero)
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
func fileExpand(p string, rest []object.Value, expandTilde bool) string {
	if expandTilde {
		p = expandTildePath(p)
	}
	if isAbsPath(p) {
		return cleanAbs(p)
	}
	base := ""
	if len(rest) > 0 && rest[0] != object.NilV {
		base = fileExpand(strArg(rest[0]), nil, expandTilde)
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
	return timeSeconds(v)
}
