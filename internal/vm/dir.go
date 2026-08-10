package vm

import (
	"os"
	"path" // always '/'-separated, as Ruby's Dir is
	"sort"
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
	glob := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var elems []object.Value
		for _, m := range dirGlob(strArg(args[0])) {
			elems = append(elems, object.NewString(m))
		}
		return object.NewArrayFromSlice(elems)
	}
	def("glob", glob)
	def("[]", glob)
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

// dirGlob matches a single-level shell pattern (e.g. "dir/*.rb"), returning the
// sorted matches with the pattern's directory prefix preserved. Hidden entries
// are skipped unless the pattern's basename is itself explicit about the dot.
func dirGlob(pattern string) []string {
	prefix, base := path.Split(pattern)
	readDir := strings.TrimSuffix(prefix, "/")
	switch readDir {
	case "":
		readDir = "."
	}
	entries, err := os.ReadDir(readDir)
	if err != nil {
		return nil
	}
	var matches []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		if ok, _ := path.Match(base, name); ok {
			matches = append(matches, prefix+name)
		}
	}
	sort.Strings(matches)
	return matches
}
