package vm

import (
	"os"
	"path" // always '/'-separated, as Ruby's File is — not path/filepath
	"strings"

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
	syscallErr := newClass("SystemCallError", vm.consts["StandardError"].(*RClass))
	vm.consts["SystemCallError"] = syscallErr
	errno := newClass("Errno", nil)
	errno.isModule = true
	vm.consts["Errno"] = errno
	enoent := newClass("Errno::ENOENT", syscallErr)
	errno.consts["ENOENT"] = enoent
	vm.consts["Errno::ENOENT"] = enoent

	cFile := newClass("File", vm.cObject)
	vm.consts["File"] = cFile
	// Path constants (POSIX). ALT_SEPARATOR is nil on unix-like platforms; NULL
	// is the null device. These let path-handling code branch on File::SEPARATOR
	// etc. without a runtime error.
	cFile.consts["SEPARATOR"] = object.NewString("/")
	cFile.consts["ALT_SEPARATOR"] = object.NilV
	cFile.consts["PATH_SEPARATOR"] = object.NewString(":")
	cFile.consts["NULL"] = object.NewString("/dev/null")
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
		return object.NewString(base)
	})
	def("dirname", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(path.Dir(pathArg(vm, args[0])))
	})
	def("extname", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(fileExt(path.Base(pathArg(vm, args[0]))))
	})
	def("split", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		return &object.Array{Elems: []object.Value{object.NewString(path.Dir(p)), object.NewString(path.Base(p))}}
	})
	def("join", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// MRI's File.join flattens nested Array arguments, so File.join([a, b], c)
		// behaves like File.join(a, b, c).
		var parts []string
		var add func(v object.Value)
		add = func(v object.Value) {
			if arr, ok := v.(*object.Array); ok {
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
		return object.NewString(fileJoin(parts))
	})
	def("expand_path", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(fileExpand(pathArg(vm, args[0]), args[1:]))
	})
	// absolute_path resolves a path to an absolute one against an optional base
	// directory (defaulting to the CWD), like expand_path but without ~ expansion.
	// Puppet uses it with relative paths and an explicit base, where the two agree.
	def("absolute_path", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(fileExpand(pathArg(vm, args[0]), args[1:]))
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
	def("read", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		b, err := os.ReadFile(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", p)
		}
		return object.NewString(string(b))
	})
	def("write", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		data := []byte(strArg(args[1]))
		if err := os.WriteFile(p, data, 0o644); err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", p)
		}
		return object.Integer(len(data))
	})
	def("size", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p := pathArg(vm, args[0])
		fi, err := os.Stat(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_file_s_stat - %s", p)
		}
		return object.Integer(fi.Size())
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
		return &Time{t: gotime.FromUnix(fi.ModTime().Unix())}
	})
	delete := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		for _, a := range args {
			p := pathArg(vm, a)
			if err := os.Remove(p); err != nil {
				raise("Errno::ENOENT", "No such file or directory @ apply2files - %s", p)
			}
		}
		return object.Integer(len(args))
	}
	def("delete", delete)
	def("unlink", delete)
}

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

// fileExpand implements File.expand_path: ~ expands to the home directory, a
// relative path is resolved against the optional base (default: the working
// directory), and the result is cleaned (so .. and . collapse).
func fileExpand(p string, rest []object.Value) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = toSlash(home) + strings.TrimPrefix(p, "~")
		}
	}
	if path.IsAbs(p) {
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
