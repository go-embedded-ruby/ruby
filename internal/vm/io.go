package vm

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// IOObj backs a Ruby IO and StringIO. A real IO (the $stdout/$stderr/$stdin
// streams) writes to / reads from an os-level writer/reader; a StringIO is an
// in-memory byte buffer with a read/write cursor. The two share the write path
// so puts/print/printf/<< work uniformly, and StringIO adds the read methods.
type IOObj struct {
	cls      *RClass // IO, StringIO or File — so classOf/is_a? are exact
	w        io.Writer
	buf      []byte // StringIO / File content (buffered in memory)
	pos      int    // read/write cursor
	isStr    bool   // buffer-backed (StringIO / File) vs writer-backed (real IO)
	sync     bool
	closed   bool
	label    string // "STDOUT"/"STDERR"/"STDIN" for inspect
	path     string // backing file path for a File stream (else "")
	writable bool   // a File opened for writing — flush the buffer on flush/close
}

func (o *IOObj) ToS() string {
	if o.isStr {
		return "#<StringIO>"
	}
	return "#<IO:<" + o.label + ">>"
}
func (o *IOObj) Inspect() string { return o.ToS() }
func (o *IOObj) Truthy() bool    { return true }

// writeBytes appends p to the stream (advancing the StringIO cursor, overwriting
// then extending) and returns the byte count.
func (o *IOObj) writeBytes(p []byte) int {
	if o.isStr {
		if end := o.pos + len(p); end > len(o.buf) {
			o.buf = append(o.buf, make([]byte, end-len(o.buf))...)
		}
		copy(o.buf[o.pos:], p)
		o.pos += len(p)
		return len(p)
	}
	n, _ := o.w.Write(p)
	return n
}

func (o *IOObj) writeStr(s string) int { return o.writeBytes([]byte(s)) }

// registerIO installs the IO class with the writing methods, the StringIO class
// (read + write), and the standard streams as both globals ($stdout/$stderr/
// $stdin) and constants (STDOUT/STDERR/STDIN). Kernel#puts/print/p are routed
// through the current $stdout so reassigning it (e.g. to a StringIO) captures
// output, as in MRI.
func (vm *VM) registerIO() {
	cIO := newClass("IO", vm.cObject)
	vm.consts["IO"] = cIO
	defIOWrite(cIO)
	defStringIORead(cIO) // IO carries the read protocol too ($stdin, File streams)

	cStringIO := newClass("StringIO", vm.cObject)
	vm.consts["StringIO"] = cStringIO
	defIOWrite(cStringIO)
	defStringIORead(cStringIO)
	cStringIO.smethods["new"] = &Method{name: "new", owner: cStringIO, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		o := &IOObj{cls: cStringIO, isStr: true}
		if len(args) > 0 {
			if s, ok := args[0].(*object.String); ok {
				o.buf = append([]byte(nil), s.B...)
			} else {
				raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
			}
		}
		return o
	}}

	stdout := &IOObj{cls: cIO, w: vm.out, label: "STDOUT"}
	stderr := &IOObj{cls: cIO, w: vm.errOut, label: "STDERR"}
	stdin := &IOObj{cls: cIO, isStr: true, label: "STDIN"} // empty input by default
	vm.consts["STDOUT"], vm.consts["STDERR"], vm.consts["STDIN"] = stdout, stderr, stdin
	vm.globals["$stdout"], vm.globals["$stderr"], vm.globals["$stdin"] = stdout, stderr, stdin

	// Kernel#warn writes each message (newline-terminated) to the current $stderr.
	vm.cObject.define("warn", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		o := vm.curStderr()
		for _, a := range args {
			vm.ioPutsValue(o, a)
		}
		return object.NilV
	})

	// File streams: File.open returns a buffered, file-backed IO carrying the
	// same read+write protocol (File acts as an IO subtype). The block form
	// flushes and closes afterwards, returning the block's value.
	cFile := vm.consts["File"].(*RClass)
	cFile.super = cIO // File < IO, inheriting the read+write protocol; is_a?(IO) holds
	cFile.smethods["open"] = &Method{name: "open", owner: cFile, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		o := openFileIO(cFile, strArg(args[0]), fileMode(args))
		if blk != nil {
			defer ioFlushClose(o)
			return vm.callBlock(blk, []object.Value{o})
		}
		return o
	}}
	cFile.smethods["readlines"] = &Method{name: "readlines", owner: cFile, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		o := openFileIO(cFile, strArg(args[0]), "r")
		var lines []object.Value
		for v := ioGets(o, nil); v != object.NilV; v = ioGets(o, nil) {
			lines = append(lines, v)
		}
		return &object.Array{Elems: lines}
	}}
	cFile.smethods["foreach"] = &Method{name: "foreach", owner: cFile, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		o := openFileIO(cFile, strArg(args[0]), "r")
		for v := ioGets(o, nil); v != object.NilV; v = ioGets(o, nil) {
			vm.callBlock(blk, []object.Value{v})
		}
		return object.NilV
	}}
}

// fileMode returns the access mode argument of File.open (default "r").
func fileMode(args []object.Value) string {
	if len(args) > 1 {
		return strArg(args[1])
	}
	return "r"
}

// openFileIO opens path into a buffered, file-backed IOObj per mode (r/w/a, with
// an optional "+" making a read mode writable). The file's bytes are read into
// the buffer; writes accumulate there and are flushed back on flush/close.
func openFileIO(cls *RClass, p, mode string) *IOObj {
	if mode == "" {
		raise("ArgumentError", "invalid access mode %s", mode)
	}
	o := &IOObj{cls: cls, isStr: true, path: p}
	switch mode[0] {
	case 'r':
		b, err := os.ReadFile(p)
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", p)
		}
		o.buf, o.writable = b, strings.Contains(mode, "+")
	case 'w':
		o.writable = true // empty buffer; flush truncates the file
		// Materialise the (truncated) file on disk now, as MRI's O_CREAT|O_TRUNC
		// open does, so File.stat/chmod and friends see it before the first flush —
		// the buffered writes are still flushed back on flush/close.
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", p)
		}
	case 'a':
		b, _ := os.ReadFile(p) // append to the existing content (or a new file)
		o.buf, o.pos, o.writable = b, len(b), true
	default:
		raise("ArgumentError", "invalid access mode %s", mode)
	}
	return o
}

// ioFlush writes a writable file stream's buffer back to disk.
func ioFlush(o *IOObj) {
	if o.writable && o.path != "" {
		if err := os.WriteFile(o.path, o.buf, 0o644); err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", o.path)
		}
	}
}

// ioFlushClose flushes then marks the stream closed (the File.open block exit).
func ioFlushClose(o *IOObj) {
	ioFlush(o)
	o.closed = true
}

// curStdout / curStderr / curStdin return the IO currently bound to the global,
// falling back to the raw VM writer when a host rebinds it to a non-IO value.
func (vm *VM) curStdout() *IOObj { return vm.curIO("$stdout", vm.out, "STDOUT") }
func (vm *VM) curStderr() *IOObj { return vm.curIO("$stderr", vm.errOut, "STDERR") }

func (vm *VM) curIO(global string, w io.Writer, label string) *IOObj {
	if o, ok := vm.globals[global].(*IOObj); ok {
		return o
	}
	return &IOObj{cls: vm.consts["IO"].(*RClass), w: w, label: label}
}

// defIOWrite defines the writing half of the IO protocol on cls (shared by IO
// and StringIO).
func defIOWrite(cls *RClass) {
	cls.define("write", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		n := 0
		for _, a := range args {
			n += o.writeStr(a.ToS())
		}
		return object.Integer(n)
	})
	cls.define("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		o.writeStr(args[0].ToS())
		return self
	})
	cls.define("print", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		for _, a := range args {
			o.writeStr(vm.displayStr(a))
		}
		return object.NilV
	})
	cls.define("puts", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		vm.ioPuts(o, args)
		return object.NilV
	})
	cls.define("printf", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		o.writeStr(formatString(args[0].ToS(), args[1:]))
		return object.NilV
	})
	cls.define("putc", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		switch a := args[0].(type) {
		case object.Integer:
			o.writeBytes([]byte{byte(a)})
		case *object.String:
			if len(a.B) > 0 {
				o.writeBytes(a.B[:1])
			}
		default:
			raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(args[0]))
		}
		return args[0]
	})
	cls.define("flush", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		ioFlush(self.(*IOObj))
		return self
	})
	cls.define("fsync", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return object.Integer(0) })
	cls.define("sync", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*IOObj).sync)
	})
	cls.define("sync=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*IOObj).sync = args[0].Truthy()
		return args[0]
	})
	cls.define("close", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioFlush(o)
		o.closed = true
		return object.NilV
	})
	cls.define("closed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*IOObj).closed)
	})
	cls.define("tty?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return object.Bool(false) })
	cls.define("isatty", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return object.Bool(false) })
	cls.define("binmode", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self })
}

// defStringIORead defines the reading half of the protocol, plus the cursor and
// content methods, on StringIO.
func defStringIORead(cls *RClass) {
	cls.define("string", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(string(self.(*IOObj).buf))
	})
	cls.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(self.(*IOObj).buf))
	})
	cls.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(self.(*IOObj).buf))
	})
	cls.define("eof?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		return object.Bool(o.pos >= len(o.buf))
	})
	cls.define("eof", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		return object.Bool(o.pos >= len(o.buf))
	})
	cls.define("pos", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*IOObj).pos)
	})
	cls.define("tell", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*IOObj).pos)
	})
	cls.define("pos=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*IOObj).pos = int(toInt(args[0]))
		return args[0]
	})
	cls.define("seek", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		amount := int(toInt(args[0]))
		whence := 0
		if len(args) > 1 {
			whence = int(toInt(args[1]))
		}
		switch whence {
		case 1: // SEEK_CUR
			o.pos += amount
		case 2: // SEEK_END
			o.pos = len(o.buf) + amount
		default: // SEEK_SET
			o.pos = amount
		}
		return object.Integer(0)
	})
	cls.define("rewind", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		self.(*IOObj).pos = 0
		return object.Integer(0)
	})
	cls.define("truncate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		n := int(toInt(args[0]))
		if n < len(o.buf) {
			o.buf = o.buf[:n]
		} else if n > len(o.buf) {
			o.buf = append(o.buf, make([]byte, n-len(o.buf))...)
		}
		return object.Integer(0)
	})
	cls.define("read", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if len(args) > 0 && args[0] != object.NilV {
			n := int(toInt(args[0]))
			if o.pos >= len(o.buf) {
				return object.NilV // a length read at EOF yields nil
			}
			end := min(o.pos+n, len(o.buf))
			s := object.NewString(string(o.buf[o.pos:end]))
			o.pos = end
			return s
		}
		start := min(o.pos, len(o.buf)) // pos= may have moved past the end
		s := object.NewString(string(o.buf[start:]))
		o.pos = len(o.buf)
		return s
	})
	cls.define("getc", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.pos >= len(o.buf) {
			return object.NilV
		}
		r, sz := utf8.DecodeRune(o.buf[o.pos:])
		s := object.NewString(string(r))
		o.pos += sz
		return s
	})
	gets := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		return ioGets(o, args)
	}
	cls.define("gets", gets)
	cls.define("readline", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		v := gets(vm, self, args, nil)
		if v == object.NilV {
			raise("EOFError", "end of file reached")
		}
		return v
	})
	cls.define("readlines", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		var lines []object.Value
		for {
			v := ioGets(o, args)
			if v == object.NilV {
				break
			}
			lines = append(lines, v)
		}
		return &object.Array{Elems: lines}
	})
	cls.define("each_line", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		o := self.(*IOObj)
		for {
			v := ioGets(o, args)
			if v == object.NilV {
				break
			}
			vm.callBlock(blk, []object.Value{v})
		}
		return self
	})
	cls.define("each_char", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		o := self.(*IOObj)
		for o.pos < len(o.buf) {
			r, sz := utf8.DecodeRune(o.buf[o.pos:])
			o.pos += sz
			vm.callBlock(blk, []object.Value{object.NewString(string(r))})
		}
		return self
	})
}

// ioGets reads one line (up to and including the separator, default "\n") from a
// StringIO, returning nil at end of input.
func ioGets(o *IOObj, args []object.Value) object.Value {
	if o.pos >= len(o.buf) {
		return object.NilV
	}
	sep := "\n"
	if len(args) > 0 {
		if s, ok := args[0].(*object.String); ok {
			sep = s.Str()
		}
	}
	rest := o.buf[o.pos:]
	if i := strings.Index(string(rest), sep); i >= 0 {
		end := o.pos + i + len(sep)
		s := object.NewString(string(o.buf[o.pos:end]))
		o.pos = end
		return s
	}
	s := object.NewString(string(rest))
	o.pos = len(o.buf)
	return s
}

// ioPuts writes args to o with Kernel#puts semantics (arrays flattened, a
// trailing newline added unless already present; no args ⇒ a lone newline). It
// is a VM method so each value is stringified through its (possibly
// user-defined) #to_s, matching MRI.
func (vm *VM) ioPuts(o *IOObj, args []object.Value) {
	if len(args) == 0 {
		o.writeStr("\n")
		return
	}
	for _, a := range args {
		vm.ioPutsValue(o, a)
	}
}

func (vm *VM) ioPutsValue(o *IOObj, v object.Value) {
	if arr, ok := v.(*object.Array); ok {
		// An empty array writes nothing (MRI), unlike a no-arg puts which writes a
		// lone newline.
		for _, e := range arr.Elems {
			vm.ioPutsValue(o, e)
		}
		return
	}
	if s := vm.displayStr(v); strings.HasSuffix(s, "\n") {
		o.writeStr(s)
	} else {
		o.writeStr(s + "\n")
	}
}

// displayStr renders v the way Kernel#print / #puts / String() do: a user object
// (RObject) goes through its (possibly user-defined) #to_s, so an overridden to_s
// is honoured; built-in value types use their authoritative native ToS directly.
// A non-String #to_s result falls back to the native ToS.
func (vm *VM) displayStr(v object.Value) string {
	if _, ok := v.(*RObject); !ok {
		return v.ToS()
	}
	r := vm.send(v, "to_s", nil, nil)
	if s, ok := r.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// inspectStr renders v the way Kernel#p does: a user object goes through its
// (possibly user-defined) #inspect; built-in value types use their native
// Inspect. A non-String #inspect result falls back to the native Inspect.
func (vm *VM) inspectStr(v object.Value) string {
	if _, ok := v.(*RObject); !ok {
		return v.Inspect()
	}
	r := vm.send(v, "inspect", nil, nil)
	if s, ok := r.(*object.String); ok {
		return s.Str()
	}
	return v.Inspect()
}

// ioCheckOpen raises IOError when writing to a closed stream.
func ioCheckOpen(o *IOObj) {
	if o.closed {
		raise("IOError", "closed stream")
	}
}

// toInt coerces a small Integer position/length argument to int64 (raising for
// anything else, including a Bignum — a stream offset that large is nonsensical).
func toInt(v object.Value) int64 {
	if n, ok := v.(object.Integer); ok {
		return int64(n)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(v))
	return 0
}
