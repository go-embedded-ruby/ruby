package vm

import (
	"os"
	"strings"

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
	def := func(name string, fn NativeFn) { cDir.smethods[name] = &Method{name: name, owner: cDir, native: fn} }

	def("pwd", dirPwd)
	def("getwd", dirPwd)
	def("home", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(toSlash(dirHomeStr()))
	})
	def("entries", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		names := dirNames(strArg(args[0]))
		elems := []object.Value{object.NewString("."), object.NewString("..")}
		for _, n := range names {
			elems = append(elems, object.NewString(n))
		}
		return object.NewArrayFromSlice(elems)
	})
	def("children", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var elems []object.Value
		for _, n := range dirNames(strArg(args[0])) {
			elems = append(elems, object.NewString(n))
		}
		return object.NewArrayFromSlice(elems)
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
	def("empty?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := strArg(args[0])
		fi, err := os.Stat(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ dir_s_empty_p - %s", p)
		}
		if !fi.IsDir() {
			return object.Bool(false)
		}
		return object.Bool(len(dirNames(p)) == 0)
	})
	def("mkdir", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := strArg(args[0])
		if err := os.Mkdir(p, 0o755); err != nil {
			if os.IsExist(err) {
				raise("Errno::EEXIST", "File exists @ dir_s_mkdir - %s", p)
			}
			raise("Errno::ENOENT", "No such file or directory @ dir_s_mkdir - %s", p)
		}
		return object.IntValue(0)
	})
	rm := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := strArg(args[0])
		if err := os.Remove(p); err != nil {
			raise("Errno::ENOENT", "No such file or directory @ dir_s_rmdir - %s", p)
		}
		return object.IntValue(0)
	}
	def("rmdir", rm)
	def("delete", rm)
	def("unlink", rm)
	def("chdir", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		var target string
		if len(args) > 0 {
			target = strArg(args[0])
		} else {
			target = dirHomeStr()
		}
		old, _ := os.Getwd()
		if err := os.Chdir(target); err != nil {
			raise("Errno::ENOENT", "No such file or directory @ dir_chdir - %s", target)
		}
		if blk != nil {
			defer os.Chdir(old)
			return vm.callBlock(blk, []object.Value{object.NewString(toSlash(target))})
		}
		return object.IntValue(0)
	})
	def("each_child", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		for _, n := range dirNames(strArg(args[0])) {
			vm.callBlock(blk, []object.Value{object.NewString(n)})
		}
		return object.NilV
	})
	def("foreach", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		vm.callBlock(blk, []object.Value{object.NewString(".")})
		vm.callBlock(blk, []object.Value{object.NewString("..")})
		for _, n := range dirNames(strArg(args[0])) {
			vm.callBlock(blk, []object.Value{object.NewString(n)})
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
}

func (d *DirObj) ToS() string     { return "#<Dir:" + d.path + ">" }
func (d *DirObj) Inspect() string { return d.ToS() }
func (d *DirObj) Truthy() bool    { return true }

// openDirObj snapshots a directory's entries into a DirObj, raising Errno::ENOENT
// (a SystemCallError) when the path is not a readable directory — matching MRI's
// Dir.new / Dir.open.
func openDirObj(path string) *DirObj {
	entries := append([]string{".", ".."}, dirNames(path)...)
	return &DirObj{path: path, entries: entries}
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
		return openDirObj(pathArg(vm, args[0]))
	}}
	cDir.smethods["open"] = &Method{name: "open", owner: cDir, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		d := openDirObj(pathArg(vm, args[0]))
		if blk == nil {
			return d
		}
		// A block form closes the handle on the way out — even if the block raises.
		defer func() { d.closed = true }()
		return vm.callBlock(blk, []object.Value{d})
	}}

	d := func(name string, fn NativeFn) { cDir.define(name, fn) }

	d("read", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		dir := self(v)
		checkOpen(dir)
		if dir.pos >= len(dir.entries) {
			return object.NilV
		}
		name := dir.entries[dir.pos]
		dir.pos++
		return object.NewString(name)
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
		return object.NewString(self(v).path) // works even on a closed handle
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
			return enumFor(dir, "each")
		}
		for _, n := range dir.entries {
			vm.callBlock(blk, []object.Value{object.NewString(n)})
		}
		dir.pos = len(dir.entries) // each leaves the cursor at the end (MRI)
		return dir
	})
	d("each_child", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		dir := self(v)
		checkOpen(dir)
		if blk == nil {
			return enumFor(dir, "each_child")
		}
		for _, n := range dir.entries {
			if n == "." || n == ".." {
				continue
			}
			vm.callBlock(blk, []object.Value{object.NewString(n)})
		}
		return dir
	})
}

// osUserHomeDir is a seam over os.UserHomeDir so the no-HOME error path is
// testable without manipulating the process environment.
var osUserHomeDir = os.UserHomeDir

// dirHomeStr returns the user's home directory (OS-native), raising ArgumentError
// when it cannot be determined, as MRI's Dir.home / Dir.chdir do.
func dirHomeStr() string {
	h, err := osUserHomeDir()
	if err != nil {
		raise("ArgumentError", "couldn't find HOME environment -- expanding `~'")
	}
	return h
}

func dirPwd(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
	wd, _ := os.Getwd()
	return object.NewString(toSlash(wd))
}

func dirExist(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	fi, err := os.Stat(strArg(args[0]))
	return object.Bool(err == nil && fi.IsDir())
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
				sortR = v.Truthy()
			}
			if v, ok := h.Get(object.Symbol("flags")); ok && v != object.NilV {
				flags = int(intArg(v))
			}
		}
	}
	return
}

// globPatternArg coerces a Dir.glob pattern argument — a String or an Array of
// Strings — into a slice of pattern strings.
func globPatternArg(vm *VM, v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = strArg(e)
		}
		return out
	}
	return []string{pathArg(vm, v)}
}

// globResult runs every pattern, coalesces and (by default) sorts the matches,
// then either yields each to a block (returning nil) or returns them as an Array.
func globResult(vm *VM, patterns []string, flags int, base string, sortR bool, blk *Proc) object.Value {
	var matches []string
	for _, pat := range patterns {
		matches = append(matches, globPattern(pat, base, flags)...)
	}
	matches = sortedUnique(matches, sortR)
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

// globPattern returns the existing filesystem paths under base that match one
// glob pattern, expanding '{a,b}' braces first (glob always honours braces) and
// walking each resulting pattern against the real directory tree.
func globPattern(pat, base string, flags int) []string {
	var out []string
	for _, expanded := range braceExpand(pat, flags&fnmNoEscape == 0) {
		globExpanded(expanded, base, flags, &out)
	}
	return out
}

// globExpanded walks one brace-free pattern. It resolves the start directory
// (base, or '/' for an absolute pattern), records whether a trailing '/' restricts
// matches to directories, and delegates the segment walk to globWalk.
func globExpanded(pat, base string, flags int, out *[]string) {
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
	globWalk(fsDir, outPrefix, segs, dirOnly, flags, out)
}

// globWalk matches the remaining pattern segments against the directory fsDir,
// appending each existing match (with the Ruby-slash outPrefix) to out. A '**'
// segment that is not final recurses into subdirectories (skipping hidden ones
// unless FNM_DOTMATCH); a literal segment is resolved by a direct stat so '.',
// '..' and explicit hidden names work; other segments are matched against the
// directory's entries, with a synthetic '.' offered for a terminal segment so
// MRI's inclusion of "." under a matching pattern is reproduced.
func globWalk(fsDir, outPrefix string, segs []string, dirOnly bool, flags int, out *[]string) {
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
			globWalk(fsDir, outPrefix, rest, dirOnly, flags, out) // rest matches here (zero levels)
		}
		for _, name := range readDirNames(fsDir) {
			if period && strings.HasPrefix(name, ".") {
				continue // do not recurse into a hidden directory without DOTMATCH
			}
			if child := fsJoin(fsDir, name); isDirFS(child) {
				globWalk(child, outPrefix+name+"/", segs, dirOnly, flags, out)
			}
		}
		return
	}

	matchSeg := seg
	if seg == "**" { // a plain trailing '**' behaves like '*'
		matchSeg = "*"
	}

	// A metacharacter-free segment is resolved by a direct stat (matching MRI and
	// letting '.', '..' and explicit hidden names through unconditionally).
	if lit, ok := literalSegment(matchSeg, escape); ok {
		globEmit(fsDir, outPrefix, lit, rest, isLast, dirOnly, flags, out)
		return
	}

	names := readDirNames(fsDir)
	if isLast {
		names = append(names, ".") // MRI matches "." (never "..") for a terminal segment
	}
	for _, name := range names {
		if !matchSegment(matchSeg, name, escape, nocase, period) {
			continue
		}
		globEmit(fsDir, outPrefix, name, rest, isLast, dirOnly, flags, out)
	}
}

// globEmit records a matched entry name: as a terminal result (honouring the
// directory-only trailing slash) when no pattern segments remain, or by recursing
// into it when it is an existing directory and more segments follow.
func globEmit(fsDir, outPrefix, name string, rest []string, isLast, dirOnly bool, flags int, out *[]string) {
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
		globWalk(child, matchedPath+"/", rest, dirOnly, flags, out)
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

// isDirFS reports whether p is an existing directory.
func isDirFS(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// fsExists reports whether p exists (file, directory or otherwise).
func fsExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
