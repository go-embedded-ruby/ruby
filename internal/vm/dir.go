package vm

import (
	"errors"
	"os"
	"os/user"
	"strings"
	"syscall"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerDir installs the Dir class — directory listing (entries/children/
// glob), queries (exist?/empty?/pwd/home), and mutation (mkdir/rmdir/chdir) —
// operating on '/'-separated paths and raising Errno::* as MRI does. It runs
// after registerFile, reusing the Errno module set up there.
func (vm *VM) registerDir() {
	// registerFile already populated the Errno module (including EEXIST), so reuse
	// those classes rather than minting new ones — recreating Errno::EEXIST here
	// would shadow the registerFile version and break a `rescue Errno::EEXIST` that
	// caught the original object.
	cDir := newClass("Dir", vm.cObject)
	vm.consts["Dir"] = cDir
	// Dir includes Enumerable (its #each yields the entries) so Enumerable's
	// map/select/to_a/… work on an open Dir — MRI asserts Dir.include?(Enumerable).
	// The Enumerable module is defined by the prelude, which runs AFTER this
	// bootstrap registration, so the mix-in is deferred to includeDirEnumerable
	// (called post-prelude); doing it here would silently no-op.
	def := func(name string, fn NativeFn) { cDir.smethods[name] = &Method{name: name, owner: cDir, native: fn} }

	def("pwd", dirPwd)
	// getwd is a genuine alias of pwd (one shared Method record), so
	// Dir.method(:getwd) == Dir.method(:pwd), as MRI's spec checks.
	cDir.smethods["getwd"] = cDir.smethods["pwd"]
	// Dir.home(user=nil): with no argument (or nil) the current user's home,
	// reading $HOME first and falling back to the passwd database; with a user
	// name, that user's home from the passwd database, raising ArgumentError when
	// the user does not exist (ruby/ruby v3_4_0 dir.c dir_s_home / rb_home_dir_of).
	def("home", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 && args[0] != object.NilV {
			name := strArg(args[0])
			h, err := userHomeDir(name)
			if err != nil {
				raise("ArgumentError", "user %s doesn't exist", name)
			}
			return object.NewString(toSlash(h))
		}
		return object.NewString(toSlash(dirHomeStr()))
	})
	def("entries", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pos, enc := vm.dirEncOption(args)
		names := append([]string{".", ".."}, dirNames(vm.filePathArg(pos[0]))...)
		return object.NewArrayFromSlice(vm.dirNameValues(names, enc))
	})
	def("children", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pos, enc := vm.dirEncOption(args)
		return object.NewArrayFromSlice(vm.dirNameValues(dirNames(vm.filePathArg(pos[0])), enc))
	})
	// Dir.glob(pattern, flags=0, base: nil, sort: true) — pattern is a String or
	// an Array of Strings; flags is an FNM_* OR (positional, or the `flags:`
	// keyword). With a block it yields each match and returns nil.
	def("glob", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		base, sortR, flags, pos := parseGlobArgs(vm, args)
		if len(pos) < 1 || len(pos) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(pos))
		}
		if len(pos) == 2 {
			flags = int(intArg(pos[1]))
		}
		return globResult(vm, globPatternArg(vm, pos[0]), flags, base, sortR, blk)
	})
	// Dir.[](*patterns, base: nil, sort: true) — like glob but takes one or more
	// pattern arguments and no flags/block.
	def("[]", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		base, sortR, _, pos := parseGlobArgs(vm, args)
		var patterns []string
		for _, a := range pos {
			patterns = append(patterns, globPatternArg(vm, a)...)
		}
		return globResult(vm, patterns, 0, base, sortR, nil)
	})
	def("exist?", dirExist)
	def("exists?", dirExist)
	def("empty?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := vm.filePathArg(args[0])
		fi, err := os.Stat(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ dir_s_empty_p - %s", p)
		}
		if !fi.IsDir() {
			return object.Bool(false)
		}
		return object.Bool(len(dirNames(p)) == 0)
	})
	// Dir.mkdir(path, mode=0777): the path is a rb_get_path argument (#to_path,
	// encoding/NUL checks) and the mode is coerced with #to_int, defaulting to
	// 0777 (the OS applies umask), matching dir.c dir_s_mkdir / check_dirname.
	def("mkdir", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 || len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
		}
		p := vm.filePathArg(args[0])
		mode := int64(0o777)
		if len(args) == 2 {
			mode = coerceInt(vm, args[1])
		}
		if err := os.Mkdir(p, os.FileMode(mode)&os.ModePerm); err != nil {
			raiseMkdirErr(err, p)
		}
		return object.IntValue(0)
	})
	rm := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := vm.filePathArg(args[0])
		// MRI's dir_s_rmdir enforces rmdir(2) semantics rather than os.Remove's
		// unlink-or-rmdir: a non-directory must be Errno::ENOTDIR, never silently
		// unlinked. lstat detects that case; every other failure is left to
		// os.Remove and classified by raiseRmdirErr (EACCES / ENOTEMPTY / ENOENT).
		if fi, err := os.Lstat(p); err == nil && !fi.IsDir() {
			raise("Errno::ENOTDIR", "Not a directory @ dir_s_rmdir - %s", p)
		}
		if err := os.Remove(p); err != nil {
			raiseRmdirErr(err, p)
		}
		return object.IntValue(0)
	}
	def("delete", rm)
	// rmdir and unlink are genuine aliases of delete (shared Method records), so
	// Dir.method(:rmdir) == Dir.method(:delete), as MRI's specs check.
	cDir.smethods["rmdir"] = cDir.smethods["delete"]
	cDir.smethods["unlink"] = cDir.smethods["delete"]
	def("chdir", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		var target string
		if len(args) > 0 {
			target = vm.filePathArg(args[0])
		} else {
			target = dirHomeStr()
		}
		// dir.c chdir_path takes the directory it will restore ONLY on the block
		// form — `if (chdir_alone_block_p()) { args.old_path =
		// rb_str_encode_ospath(rb_dir_getwd()); ... }` — and rb_dir_getwd raises.
		// So `Dir.chdir(d) { }` out of a removed working directory is
		// Errno::ENOENT - getcwd, while the plain `Dir.chdir(d)` never consults
		// getcwd at all and still returns 0. Both witnessed against MRI 4.0.5;
		// reading the working directory unconditionally here would raise where MRI
		// succeeds.
		var old string
		if blk != nil {
			old = getwdOrFail()
		}
		if err := os.Chdir(target); err != nil {
			raiseChdirErr(err, target)
		}
		if blk != nil {
			defer os.Chdir(old)
			return vm.callBlock(blk, []object.Value{object.NewString(toSlash(target))})
		}
		return object.IntValue(0)
	})
	// Dir.chroot(path) changes the process root to path and returns 0 (dir.c
	// dir_s_chroot: check_dirname then chroot(2), rb_sys_fail_path on -1). A
	// non-privileged process cannot chroot, so the real call fails with
	// Errno::EPERM — the behaviour the spec exercises as a regular user. The path
	// is a rb_get_path argument (#to_path). chrootFn is a platform seam so the
	// error mapping is testable without re-rooting the test process.
	def("chroot", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		p := pathArg(vm, args[0])
		if err := chrootFn(p); err != nil {
			raiseChrootErr(err, p)
		}
		return object.IntValue(0)
	})
	// Dir.each_child(path) yields each child name (no "." / ".."); Dir.foreach(path)
	// yields every entry including "." and "..". Both return nil after a block and,
	// with no block, an Enumerator over the snapshot names (MRI dir.c) — whose #size
	// is nil, and whose iteration re-yields the names. The block-less form must not
	// dereference a nil block (the previous version crashed there).
	def("each_child", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		pos, enc := vm.dirEncOption(args)
		names := dirNames(vm.filePathArg(pos[0]))
		if blk == nil {
			return dirEnumerator(vm.dirNameValues(names, enc))
		}
		for _, n := range names {
			vm.callBlock(blk, []object.Value{vm.dirExternalStr(n, enc)})
		}
		return object.NilV
	})
	def("foreach", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		pos, enc := vm.dirEncOption(args)
		names := append([]string{".", ".."}, dirNames(vm.filePathArg(pos[0]))...)
		if blk == nil {
			return dirEnumerator(vm.dirNameValues(names, enc))
		}
		for _, n := range names {
			vm.callBlock(blk, []object.Value{vm.dirExternalStr(n, enc)})
		}
		return object.NilV
	})

	vm.registerDirInstance(cDir)
}

// DirObj is the Ruby Dir *instance*: an open directory handle with a snapshot of
// its entries (".", ".." and the sorted names) and a read cursor. It is a
// first-class object.Value, dispatched through the Dir class.
type DirObj struct {
	path    string
	entries []string
	pos     int
	closed  bool
	// enc is dir.c's dp->enc: the encoding every name this handle yields is
	// tagged with, from the `encoding:` keyword or the filesystem encoding.
	enc string
	// pathStr is dir.c's dp->path: `rb_str_dup_frozen(dirname)` taken BEFORE
	// rb_str_encode_ospath, so #path / #to_path give back the argument as it was
	// passed — encoding included. A Go string cannot carry that.
	pathStr *object.String
}

func (d *DirObj) ToS() string     { return "#<Dir:" + d.path + ">" }
func (d *DirObj) Inspect() string { return d.ToS() }
func (d *DirObj) Truthy() bool    { return true }

// openDirObj snapshots a directory's entries into a DirObj, raising Errno::ENOENT
// (a SystemCallError) when the path is not a readable directory — matching MRI's
// Dir.new / Dir.open. enc is the handle's encoding (dir.c dir_initialize's
// dp->enc): every name it later hands out is tagged with it.
func openDirObj(path, enc string, orig *object.String) *DirObj {
	entries := append([]string{".", ".."}, dirNames(path)...)
	return &DirObj{path: path, entries: entries, enc: enc, pathStr: orig}
}

// dirPathArg coerces a Dir.new / Dir.open path argument and keeps the String it
// came from, which #to_path has to hand back unchanged (see DirObj.pathStr).
func (vm *VM) dirPathArg(v object.Value) (string, *object.String) {
	p := pathArg(vm, v)
	// FilePathValue has already coerced anything that is not a String, and
	// dp->path is the COERCED value — a Pathname argument leaves a plain UTF-8
	// path behind, not the Pathname's own encoding.
	orig := object.NewString(p)
	if s, ok := v.(*object.String); ok {
		orig = s
	}
	return p, orig
}

// dirEncOption is dir.c dir_initialize's
// `NIL_P(enc) ? rb_filesystem_encoding() : rb_to_encoding(enc)`: the `encoding:`
// keyword every Dir reader accepts, defaulting to the filesystem encoding. It
// also strips the option Hash off the positional arguments, since rbgo passes
// keywords through as a trailing Hash.
func (vm *VM) dirEncOption(args []object.Value) ([]object.Value, string) {
	pos, opts := splitIOOpts(args)
	if opts != nil {
		if v, ok := opts.Get(object.Symbol("encoding")); ok && !object.IsNil(v) {
			return pos, vm.encodingArg(v).name
		}
	}
	return pos, vm.fsEncName()
}

// dirExternalStr is what dir.c does to EVERY name it reads —
// rb_external_str_new_with_enc (string.c), through dir_read and dir_each_entry:
// tag the bytes with the handle's encoding, then, when
// Encoding.default_internal is set, CONVERT them to it. Tagging alone is not
// enough: `Dir.children(d)` under default_internal = EUC-KR hands back EUC-KR
// strings, not filesystem-encoded ones.
//
// rb_external_str_with_enc's one special case is kept: a US-ASCII handle
// encoding over bytes that are not ASCII is ASCII-8BIT instead, and is not
// converted — there is no meaningful source encoding to convert from.
//
// The conversion is rb_str_conv_enc, NOT String#encode, and the difference
// decides whether this works at all: rb_str_conv_enc_opts ends with
// "/* some error, return original */ return str;". A name that cannot be
// represented in the internal encoding is handed back AS IS, in the filesystem
// encoding, rather than raising. A directory holding "こんにちは.txt" read under
// default_internal = EUC-KR is exactly that case, and MRI lists it without
// complaint.
func (vm *VM) dirExternalStr(name, enc string) object.Value {
	if enc == "US-ASCII" && !isASCIIOnly(name) {
		return object.NewStringBytesEnc([]byte(name), "ASCII-8BIT")
	}
	s := object.NewStringBytesEnc([]byte(name), enc)
	if vm.defInternalEnc == nil || vm.defInternalEnc.name == enc {
		return s
	}
	return vm.convEncOrOriginal(s, vm.defInternalEnc.name)
}

// convEncOrOriginal is rb_str_conv_enc: a BEST-EFFORT transcode that answers the
// original string when the conversion fails, where String#encode raises.
//
// Treating ANY recovery as a failed conversion is safe HERE and nowhere in
// general: stringEncode calls no Ruby block, so it cannot be carrying a
// break/return/throw signal that swallowing would lose — the only thing it
// panics with is the Encoding::UndefinedConversionError this exists to absorb.
func (vm *VM) convEncOrOriginal(s *object.String, to string) object.Value {
	converted := object.Value(s)
	if rec := recoverAny(func() {
		converted = vm.stringEncode(s, []object.Value{object.NewString(to)})
	}); rec != nil {
		return s
	}
	return converted
}

// isASCIIOnly reports whether every byte of s is 7-bit (string.c
// is_ascii_string).
func isASCIIOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// dirNameValues maps a snapshot of entry names through dirExternalStr.
func (vm *VM) dirNameValues(names []string, enc string) []object.Value {
	elems := make([]object.Value, len(names))
	for i, n := range names {
		elems[i] = vm.dirExternalStr(n, enc)
	}
	return elems
}

// registerDirInstance installs Dir.new / Dir.open and the Dir instance methods
// (read/tell/seek/pos/rewind/path/close/each/…). Operations on a closed handle
// raise IOError, as MRI does; path/to_path keep working after close.
func (vm *VM) registerDirInstance(cDir *RClass) {
	self := func(v object.Value) *DirObj { return v.(*DirObj) }
	// checkOpen raises IOError for a method that requires an open handle.
	checkOpen := func(d *DirObj) {
		if d.closed {
			raise("IOError", "closed directory")
		}
	}

	cDir.smethods["new"] = &Method{name: "new", owner: cDir, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pos, enc := vm.dirEncOption(args)
		p, orig := vm.dirPathArg(pos[0])
		return openDirObj(p, enc, orig)
	}}
	cDir.smethods["open"] = &Method{name: "open", owner: cDir, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		pos, enc := vm.dirEncOption(args)
		p, orig := vm.dirPathArg(pos[0])
		d := openDirObj(p, enc, orig)
		if blk == nil {
			return d
		}
		// A block form closes the handle on the way out — even if the block raises.
		defer func() { d.closed = true }()
		return vm.callBlock(blk, []object.Value{d})
	}}

	d := func(name string, fn NativeFn) { cDir.define(name, fn) }

	d("read", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		dir := self(v)
		checkOpen(dir)
		if dir.pos >= len(dir.entries) {
			return object.NilV
		}
		name := dir.entries[dir.pos]
		dir.pos++
		return vm.dirExternalStr(name, dir.enc)
	})
	d("pos", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		dir := self(v)
		checkOpen(dir)
		return object.IntValue(int64(dir.pos))
	})
	// tell is an alias of pos (same Method identity, as MRI's spec checks).
	cDir.methods["tell"] = cDir.methods["pos"]
	d("seek", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		dir := self(v)
		checkOpen(dir)
		dir.pos = int(intArg(args[0]))
		return dir // seek returns the Dir instance
	})
	d("pos=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		dir := self(v)
		checkOpen(dir)
		dir.pos = int(intArg(args[0]))
		return args[0] // pos= returns its argument
	})
	d("rewind", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		dir := self(v)
		checkOpen(dir)
		dir.pos = 0
		return dir
	})
	d("to_path", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		// dp->path is the ORIGINAL argument, so its encoding survives the round
		// trip. Works even on a closed handle.
		return self(v).pathStr
	})
	cDir.methods["path"] = cDir.methods["to_path"] // path is an alias of to_path
	// fileno: rbgo's Dir has no underlying file descriptor, so — as MRI does on
	// platforms without dirfd — it raises NotImplementedError (but IOError first
	// when the handle is already closed).
	d("fileno", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		checkOpen(self(v))
		raise("NotImplementedError", "fileno() function is unimplemented on this machine")
		return object.NilV
	})
	d("close", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).closed = true // idempotent: closing an already-closed Dir is a no-op
		return object.NilV
	})
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		dir := self(v)
		checkOpen(dir)
		if blk == nil {
			// A block-less #each returns an Enumerator whose #size is nil (MRI does
			// not pre-count a directory), backed by the current entry snapshot.
			return dirEnumerator(vm.dirNameValues(dir.entries, dir.enc))
		}
		for _, n := range dir.entries {
			vm.callBlock(blk, []object.Value{vm.dirExternalStr(n, dir.enc)})
		}
		dir.pos = len(dir.entries) // each leaves the cursor at the end (MRI)
		return dir
	})
	d("each_child", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		dir := self(v)
		checkOpen(dir)
		if blk == nil {
			return dirEnumerator(vm.dirNameValues(dirChildNames(dir.entries), dir.enc))
		}
		for _, n := range dir.entries {
			if n == "." || n == ".." {
				continue
			}
			vm.callBlock(blk, []object.Value{vm.dirExternalStr(n, dir.enc)})
		}
		return dir
	})
	// Dir#children returns the entry names of the open handle minus "." and ".."
	// (Ruby 2.5+ dir.c dir_collect_children). It reads the snapshot, so repeated
	// calls return the same result regardless of the read cursor.
	d("children", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		dir := self(v)
		checkOpen(dir)
		return object.NewArrayFromSlice(vm.dirNameValues(dirChildNames(dir.entries), dir.enc))
	})
	// Dir#chdir changes the process working directory to the handle's path,
	// returning 0 (or the block's value for the block form, which restores the
	// previous directory afterwards and — unlike Dir.chdir — yields nil rather
	// than the path). Ruby 3.5+ (dir.c dir_chdir0 / fdopendir path).
	d("chdir", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		dir := self(v)
		checkOpen(dir)
		// The discarded error is DELIBERATE, and the opposite of dirPwd's: Dir#chdir
		// is dir.c dir_chdir -> dir_s_fchdir(dir_fileno(dir)), whose block form
		// saves the previous directory by OPENING "." (dir_initialize(..., "."))
		// rather than by calling getcwd. So `Dir.new(d).chdir { }` out of a removed
		// working directory succeeds in MRI 4.0.5 — witnessed — and raising here
		// would be a divergence, not a fix. rbgo has no fd for the old directory,
		// so it can only restore by path and gives up silently when there is none.
		old, _ := os.Getwd()
		if err := os.Chdir(dir.path); err != nil {
			raiseChdirErr(err, dir.path)
		}
		if blk != nil {
			defer os.Chdir(old)
			return vm.callBlock(blk, []object.Value{object.NilV})
		}
		return object.IntValue(0)
	})
}

// osUserHomeDir is a seam over os.UserHomeDir so the no-HOME error path is
// testable without manipulating the process environment.
var osUserHomeDir = os.UserHomeDir

// osCurrentUserHome is a seam over the passwd-database lookup of the current
// user's home directory (getpwuid), used as Dir.home's fallback when $HOME is
// unset — matching MRI's rb_default_home_dir. It is a var so the fallback and
// its failure branch are testable without depending on the runner's passwd db.
var osCurrentUserHome = func() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

// dirHomeStr returns the user's home directory (OS-native), reading $HOME first
// and falling back to the passwd database when it is unset (MRI's
// rb_default_home_dir), raising ArgumentError only when neither yields a home.
func dirHomeStr() string {
	if h, err := osUserHomeDir(); err == nil && h != "" {
		return h
	}
	if h, err := osCurrentUserHome(); err == nil && h != "" {
		return h
	}
	raise("ArgumentError", "couldn't find HOME environment -- expanding `~'")
	return ""
}

// raiseMkdirErr maps an os.Mkdir failure to the MRI errno Dir.mkdir raises: an
// existing entry is Errno::EEXIST, a permission failure (e.g. an unwritable
// parent) Errno::EACCES — a SystemCallError, as the spec requires — and any
// other failure (a missing intermediate directory) Errno::ENOENT.
func raiseMkdirErr(err error, path string) {
	switch {
	case os.IsExist(err):
		raise("Errno::EEXIST", "File exists @ dir_s_mkdir - %s", path)
	case os.IsPermission(err):
		raise("Errno::EACCES", "Permission denied @ dir_s_mkdir - %s", path)
	default:
		raise("Errno::ENOENT", "No such file or directory @ dir_s_mkdir - %s", path)
	}
}

// dirPwd is Dir.pwd / Dir.getwd: dir.c dir_s_getwd -> rb_dir_getwd ->
// rb_dir_getwd_ospath -> util.c ruby_getcwd. ruby_getcwd never hands a failure
// back to its caller — it calls rb_syserr_fail(errno, "getcwd") — so a working
// directory that was removed under the process RAISES here instead of returning
// the path that no longer exists. Discarding that error was worth about 145
// corpus examples that no reference could reach (#681): the Dir specs remove the
// working directory in teardown, MRI's failure cascades from that point on, and
// rbgo therefore scored ABOVE MRI read through the same shim.
func dirPwd(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
	return object.NewString(toSlash(getwdOrFail()))
}

// getwdOrFail is util.c ruby_getcwd(): it returns the working directory or
// raises, never both. The errno is getcwd(2)'s own and the operation MRI names
// is "getcwd", so the message is "<strerror> - getcwd" (rb_syserr_fail in
// error.c, which is what sysFail reproduces) — Errno::ENOENT for the removed
// directory, Errno::EACCES for a parent that became unsearchable, and a
// rescuable SystemCallError for anything with no registered class.
func getwdOrFail() string {
	wd, err := os.Getwd()
	if err != nil {
		sysFail(err, "getcwd")
	}
	if err := cwdStillNamed(wd); err != nil {
		sysFail(err, "getcwd")
	}
	return wd
}

// cwdStillNamed answers the question os.Getwd cannot be trusted with on darwin.
// Go's syscall.Getwd there is getattrlist(".", ATTR_CMN_FULLPATH), which keeps
// reporting the recorded path of an UNLINKED directory and returns no error at
// all — measured on this host (macOS 25, Go 1.26.4): with the working directory
// removed, os.Getwd hands back the dead path and err == nil, while MRI's
// getcwd(3) fails. Checking the error alone would therefore have fixed nothing
// here. So the answer is confirmed the way POSIX getcwd(3) confirms it, by
// resolving the name it produced and requiring it to still BE "." — the same
// kludge os.Getwd itself applies to $PWD. Three cases were witnessed against MRI
// 4.0.5 and all three fall out of this one check: the directory removed
// (Errno::ENOENT), removed and recreated, so the name resolves to a DIFFERENT
// inode (Errno::ENOENT again), and a parent that became unsearchable, where the
// resolution fails with the errno getcwd(3) reports (Errno::EACCES). A working
// directory reached through a symlink still passes: os.Getwd names the physical
// path, which is the same file.
func cwdStillNamed(wd string) error {
	dot, err := os.Stat(".")
	if err != nil {
		return err
	}
	named, err := os.Stat(wd)
	if err != nil {
		return err
	}
	if !os.SameFile(dot, named) {
		return syscall.ENOENT
	}
	return nil
}

// raiseChdirErr reports a failed chdir(2) the way dir.c chdir_path does:
// rb_sys_fail_path(path), which keeps the REAL errno rather than assuming the
// directory was missing, and names the C function it failed in. So chdir into a
// regular file is Errno::ENOTDIR ("Not a directory @ chdir_path - /etc/hosts"),
// not the Errno::ENOENT this used to raise for every failure. Both the class and
// the "chdir_path" label were witnessed against MRI 4.0.5.
func raiseChdirErr(err error, path string) {
	var eno syscall.Errno
	if errors.As(err, &eno) {
		if name, ok := errnoClasses[int64(eno)]; ok {
			raise("Errno::"+name, "%s @ chdir_path - %s", errnoStrerror(int64(eno)), path)
		}
		raise("SystemCallError", "%s @ chdir_path - %s", errnoStrerror(int64(eno)), path)
	}
	raise("Errno::ENOENT", "No such file or directory @ chdir_path - %s", path)
}

// raiseChrootErr maps a chroot(2) failure to the MRI errno Dir.chroot raises via
// rb_sys_fail_path: a permission failure (the ordinary case for a non-root
// process, and what macOS returns even for a missing path) is Errno::EPERM, a
// missing directory Errno::ENOENT, a non-directory Errno::ENOTDIR, and anything
// else falls back to Errno::EPERM. Each is a SystemCallError, as the spec checks.
func raiseChrootErr(err error, path string) {
	switch {
	case os.IsPermission(err):
		raise("Errno::EPERM", "Operation not permitted @ dir_s_chroot - %s", path)
	case os.IsNotExist(err):
		raise("Errno::ENOENT", "No such file or directory @ dir_s_chroot - %s", path)
	case errors.Is(err, syscall.ENOTDIR):
		raise("Errno::ENOTDIR", "Not a directory @ dir_s_chroot - %s", path)
	default:
		raise("Errno::EPERM", "Operation not permitted @ dir_s_chroot - %s", path)
	}
}

// includeDirEnumerable mixes the prelude-defined Enumerable module into Dir. It
// runs post-prelude (registerDir itself runs before the prelude, so the module
// does not exist yet there), driven from registerFileStat which also runs after
// the prelude. Both constants always exist by then, matching the unguarded
// mix-ins elsewhere (see registerActiveSupport / includeMySQLEnumerable).
func (vm *VM) includeDirEnumerable() {
	cDir := vm.consts["Dir"].(*RClass)
	en := vm.consts["Enumerable"].(*RClass)
	cDir.includes = append(cDir.includes, en)
}

func dirExist(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	fi, err := os.Stat(vm.filePathArg(args[0]))
	return object.Bool(err == nil && fi.IsDir())
}

// dirEnumerator wraps a snapshot of directory entry names as the block-less
// Enumerator MRI's Dir.foreach / Dir.each_child return: iterating it re-yields
// the names, and its #size is nil (MRI does not pre-count a directory
// enumerator). It is backed by an Array's #each so materialisation is a plain
// array walk rather than a re-entrant directory read.
// dirChildNames returns the entry names with "." and ".." removed — the
// child-name view shared by Dir#children and the block-less Dir#each_child
// enumerator.
func dirChildNames(entries []string) []string {
	var out []string
	for _, n := range entries {
		if n != "." && n != ".." {
			out = append(out, n)
		}
	}
	return out
}

// dirEnumerator wraps already-encoded entry names (dirNameValues) in the
// Enumerator the block-less Dir readers return. The names must arrive encoded:
// the `encoding:` keyword applies to `Dir.foreach(d, encoding: e).to_a` exactly
// as it does to the block form, and building plain UTF-8 strings here would
// silently drop it on the enumerator path alone.
func dirEnumerator(elems []object.Value) *Enumerator {
	return enumForSized(object.NewArrayFromSlice(elems), "each",
		func(*VM) object.Value { return object.NilV })
}

// raiseRmdirErr maps an os.Remove failure on a directory to the MRI errno
// Dir.delete/rmdir raises: a permission failure is Errno::EACCES, a non-empty
// directory Errno::ENOTEMPTY, and anything else Errno::ENOENT.
func raiseRmdirErr(err error, path string) {
	switch {
	case os.IsPermission(err):
		raise("Errno::EACCES", "Permission denied @ dir_s_rmdir - %s", path)
	case errors.Is(err, syscall.ENOTEMPTY):
		raise("Errno::ENOTEMPTY", "Directory not empty @ dir_s_rmdir - %s", path)
	default:
		raise("Errno::ENOENT", "No such file or directory @ dir_s_rmdir - %s", path)
	}
}

// dirNames returns the directory's entry names (sorted, no "." / ".."), raising
// Errno::ENOENT when the path is not a readable directory.
func dirNames(p string) []string {
	entries, err := os.ReadDir(p)
	if err != nil {
		raise("Errno::ENOENT", "No such file or directory @ dir_initialize - %s", p)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

// parseGlobArgs splits a Dir.glob / Dir.[] argument list into its keyword options
// (base:, sort:, flags:) and the remaining positional arguments. base defaults to
// the current directory (""), sort to true and flags to 0, matching MRI.
func parseGlobArgs(vm *VM, args []object.Value) (base string, sortR bool, flags int, pos []object.Value) {
	sortR = true
	pos = args
	if n := len(args); n > 0 {
		if h, ok := args[n-1].(*object.Hash); ok {
			pos = args[:n-1]
			if v, ok := h.Get(object.Symbol("base")); ok && v != object.NilV {
				base = pathArg(vm, v)
			}
			if v, ok := h.Get(object.Symbol("sort")); ok {
				// MRI accepts only true or false for sort:, raising ArgumentError
				// ("expected true or false as sort: …") for anything else (dir.c
				// glob keyword handling).
				if v != object.True && v != object.False {
					raise("ArgumentError", "expected true or false as sort: %s", v.Inspect())
				}
				sortR = v == object.True
			}
			if v, ok := h.Get(object.Symbol("flags")); ok && v != object.NilV {
				flags = int(intArg(v))
			}
		}
	}
	return
}

// globPatternArg coerces a Dir.glob pattern argument — a String or an Array of
// Strings — into a slice of pattern strings, applying MRI's GlobPathValue checks
// to each (see globPatternStr).
func globPatternArg(vm *VM, v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = vm.globPatternStr(e)
		}
		return out
	}
	return []string{vm.globPatternStr(v)}
}

// globPatternStr coerces one glob-pattern argument to a string the way MRI's
// GlobPathValue does: convert via #to_path/#to_str, reject an ASCII-incompatible
// encoding with Encoding::CompatibilityError, and reject an embedded NUL with
// ArgumentError ("nul-separated glob pattern is deprecated" — dir.c rb_push_glob).
func (vm *VM) globPatternStr(v object.Value) string {
	s := vm.pathStr(v)
	vm.checkPathEncoding(s)
	str := s.Str()
	if strings.IndexByte(str, 0) >= 0 {
		raise("ArgumentError", "nul-separated glob pattern is deprecated")
	}
	return str
}

// globResult globs every pattern and either yields each match to a block
// (returning nil) or returns the matches as an Array. Each brace expansion is an
// independent sub-glob: MRI sorts (when sort:) and de-duplicates it on its own,
// then concatenates the sub-globs in brace order — so duplicates ACROSS brace
// alternatives or repeated patterns are preserved (Dir["*","*"] doubles its
// matches) and the final list is not globally re-sorted (dir.c ruby_brace_expand
// drives one ruby_glob0 per expansion).
func globResult(vm *VM, patterns []string, flags int, base string, sortR bool, blk *Proc) object.Value {
	var matches []string
	escape := flags&fnmNoEscape == 0
	for _, pat := range patterns {
		for _, expanded := range braceExpand(pat, escape) {
			var sub []string
			globExpanded(expanded, base, flags, &sub)
			matches = append(matches, sortedUnique(sub, sortR)...)
		}
	}
	if blk != nil {
		for _, m := range matches {
			vm.callBlock(blk, []object.Value{object.NewString(m)})
		}
		return object.NilV
	}
	elems := make([]object.Value, len(matches))
	for i, m := range matches {
		elems[i] = object.NewString(m)
	}
	return object.NewArrayFromSlice(elems)
}

// globExpanded walks one brace-free pattern. It resolves the start directory
// (base, or '/' for an absolute pattern), records whether a trailing '/' restricts
// matches to directories, and delegates the segment walk to globWalk. An empty
// pattern matches nothing (MRI's Dir.glob("") is []), so it is short-circuited
// before the leading-"" rule could mistake it for an absolute root.
func globExpanded(pat, base string, flags int, out *[]string) {
	if pat == "" {
		return
	}
	segs := strings.Split(pat, "/")
	// globStart resolves the walk's start directory and output prefix, consuming
	// any leading absolute-root segment. On POSIX that is only a leading "" (a
	// "/foo" pattern); on Windows it also recognises a drive-letter root such as
	// "C:/foo" (see glob_windows.go), so an absolute Windows pattern globs from
	// its drive rather than being mistaken for a relative name under base.
	fsDir, outPrefix, segs := globStart(base, segs)
	dirOnly := false
	for len(segs) > 0 && segs[len(segs)-1] == "" { // trailing '/': directories only
		dirOnly = true
		segs = segs[:len(segs)-1]
	}
	if len(segs) == 0 {
		if outPrefix == "/" && isDirFS("/") { // the pattern was "/" (or "//…")
			*out = append(*out, "/")
		}
		return
	}
	// litPath starts true: the walk begins on a real (base or root) directory
	// reached by no wildcard, so a terminal segment may match the synthetic "."
	// (globWalk's dot rule).
	globWalk(fsDir, outPrefix, segs, dirOnly, true, flags, out)
}

// globWalk matches the remaining pattern segments against the directory fsDir,
// appending each existing match (with the Ruby-slash outPrefix) to out. A '**'
// segment that is not final recurses into subdirectories (skipping hidden ones
// unless FNM_DOTMATCH); a literal segment is resolved by a direct stat so '.',
// '..' and explicit hidden names work; other segments are matched against the
// directory's entries, with a synthetic '.' offered for a terminal segment so
// MRI's inclusion of "." under a matching pattern is reproduced.
//
// litPath records whether fsDir was reached through literal path components only
// (never a matched wildcard or a '**' recursion). MRI matches the synthetic "."
// for a terminal segment only on such an all-literal path (so Dir["nested/.*"]
// yields "nested/." but Dir["*/.*"] does not), or — for a '**'-bearing pattern —
// at the top level under FNM_DOTMATCH (Dir.glob("**/.*", File::FNM_DOTMATCH)
// yields "." but the plain form does not).
func globWalk(fsDir, outPrefix string, segs []string, dirOnly, litPath bool, flags int, out *[]string) {
	period := flags&fnmDotMatch == 0
	nocase := flags&fnmCaseFold != 0
	escape := flags&fnmNoEscape == 0

	seg, rest := segs[0], segs[1:]
	isLast := len(rest) == 0

	// '**' recurses when it is not the final segment, and also when it is the final
	// segment of a directory-only pattern ('a/**/'), where each level it reaches is
	// itself a match. A plain trailing '**' (no slash) instead behaves like '*'.
	if seg == "**" && (!isLast || dirOnly) {
		if isLast { // dirOnly: the directory itself matches (zero levels)
			if outPrefix != "" && isDirFS(fsDir) {
				*out = append(*out, outPrefix)
			}
		} else {
			// The zero-level match reaches rest through '**', so litPath is false.
			globWalk(fsDir, outPrefix, rest, dirOnly, false, flags, out)
		}
		for _, name := range readDirNames(fsDir) {
			if period && strings.HasPrefix(name, ".") {
				continue // do not recurse into a hidden directory without DOTMATCH
			}
			// '**' descends into real subdirectories only: a symlink to a directory
			// is matched as an entry (by the zero-level walk above) but never
			// traversed, matching MRI (glob_helper's do_lstat gate) — which also
			// avoids symlink cycles. Recursion is through '**', so litPath is false.
			if child := fsJoin(fsDir, name); isRealDirFS(child) {
				globWalk(child, outPrefix+name+"/", segs, dirOnly, false, flags, out)
			}
		}
		return
	}

	matchSeg := seg
	if seg == "**" { // a plain trailing '**' behaves like '*'
		matchSeg = "*"
	}

	// A metacharacter-free segment is resolved by a direct stat (matching MRI and
	// letting '.', '..' and explicit hidden names through unconditionally). It is a
	// literal component, so the recursion keeps litPath unchanged.
	if lit, ok := literalSegment(matchSeg, escape); ok {
		globEmit(fsDir, outPrefix, lit, rest, isLast, dirOnly, litPath, flags, out)
		return
	}

	names := readDirNames(fsDir)
	// The synthetic "." (a terminal segment matching the current directory) is
	// offered only on an all-literal path, or at the top level of a '**'-bearing
	// pattern under FNM_DOTMATCH (period false).
	topLevel := outPrefix == "" || outPrefix == "/"
	if isLast && (litPath || (!period && topLevel)) {
		names = append(names, ".") // MRI matches "." (never "..") for a terminal segment
	}
	for _, name := range names {
		if !matchSegment(matchSeg, name, escape, nocase, period) {
			continue
		}
		// A wildcard match reaches its child through a metacharacter, so litPath
		// becomes false for the recursion.
		globEmit(fsDir, outPrefix, name, rest, isLast, dirOnly, false, flags, out)
	}
}

// globEmit records a matched entry name: as a terminal result (honouring the
// directory-only trailing slash) when no pattern segments remain, or by recursing
// into it when it is an existing directory and more segments follow.
func globEmit(fsDir, outPrefix, name string, rest []string, isLast, dirOnly, litPath bool, flags int, out *[]string) {
	matchedPath := outPrefix + name
	if isLast {
		full := fsJoin(fsDir, name)
		if dirOnly {
			if isDirFS(full) {
				*out = append(*out, matchedPath+"/")
			}
			return
		}
		if fsExists(full) {
			*out = append(*out, matchedPath)
		}
		return
	}
	if child := fsJoin(fsDir, name); isDirFS(child) {
		globWalk(child, matchedPath+"/", rest, dirOnly, litPath, flags, out)
	}
}

// literalSegment reports whether seg is free of the glob metacharacters '*', '?'
// and '[' (after honouring '\' escapes), returning the unescaped literal text
// when so — used to resolve a segment by a direct stat.
func literalSegment(seg string, escape bool) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if escape && c == '\\' && i+1 < len(seg) {
			b.WriteByte(seg[i+1])
			i++
			continue
		}
		if c == '*' || c == '?' || c == '[' {
			return "", false
		}
		b.WriteByte(c)
	}
	return b.String(), true
}

// readDirNames returns the entry names of dir (no "." / ".."), or nil when dir is
// not a readable directory — an unreadable directory simply yields no matches.
func readDirNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

// fsJoin joins a directory and an entry name into a filesystem path, treating ""
// and "." as the current directory and "/" as the root.
func fsJoin(dir, name string) string {
	switch dir {
	case "", ".":
		return name
	case "/":
		return "/" + name
	default:
		return dir + "/" + name
	}
}

// isDirFS reports whether p is an existing directory, following a final symlink
// (used for explicit path segments, which MRI resolves).
func isDirFS(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// isRealDirFS reports whether p is a directory that is not itself a symlink
// (os.Lstat does not follow the final component), so a '**' walk descends into
// real subdirectories only — never through a symlink to a directory.
func isRealDirFS(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.IsDir()
}

// fsExists reports whether p exists (file, directory or otherwise).
func fsExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
