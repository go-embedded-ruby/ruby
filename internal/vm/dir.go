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
	vm.consts["Dir"] = object.Wrap(cDir)
	def := func(name string, fn NativeFn) { cDir.smethods[name] = &Method{name: name, owner: cDir, native: fn} }

	def("pwd", dirPwd)
	def("getwd", dirPwd)
	def("home", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(toSlash(dirHomeStr())))
	})
	def("entries", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		names := dirNames(strArg(args[0]))
		elems := []object.Value{object.Wrap(object.NewString(".")), object.Wrap(object.NewString(".."))}
		for _, n := range names {
			elems = append(elems, object.Wrap(object.NewString(n)))
		}
		return object.Wrap(object.NewArrayFromSlice(elems))
	})
	def("children", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var elems []object.Value
		for _, n := range dirNames(strArg(args[0])) {
			elems = append(elems, object.Wrap(object.NewString(n)))
		}
		return object.Wrap(object.NewArrayFromSlice(elems))
	})
	glob := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var elems []object.Value
		for _, m := range dirGlob(strArg(args[0])) {
			elems = append(elems, object.Wrap(object.NewString(m)))
		}
		return object.Wrap(object.NewArrayFromSlice(elems))
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
			return object.BoolValue(bool(object.Bool(false)))
		}
		return object.BoolValue(bool(object.Bool(len(dirNames(p)) == 0)))
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
			return vm.callBlock(blk, []object.Value{object.Wrap(object.NewString(toSlash(target)))})
		}
		return object.IntValue(0)
	})
	def("each_child", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		for _, n := range dirNames(strArg(args[0])) {
			vm.callBlock(blk, []object.Value{object.Wrap(object.NewString(n))})
		}
		return object.NilVal()
	})
	def("foreach", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		vm.callBlock(blk, []object.Value{object.Wrap(object.NewString("."))})
		vm.callBlock(blk, []object.Value{object.Wrap(object.NewString(".."))})
		for _, n := range dirNames(strArg(args[0])) {
			vm.callBlock(blk, []object.Value{object.Wrap(object.NewString(n))})
		}
		return object.NilVal()
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
	return object.Wrap(object.NewString(toSlash(wd)))
}

func dirExist(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	fi, err := os.Stat(strArg(args[0]))
	return object.BoolValue(bool(object.Bool(err == nil && fi.IsDir())))
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
