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
	cls        *RClass // IO, StringIO or File — so classOf/is_a? are exact
	w          io.Writer
	buf        []byte // StringIO / File content (buffered in memory)
	pos        int    // read/write cursor
	isStr      bool   // buffer-backed (StringIO / File) vs writer-backed (real IO)
	sync       bool
	closed     bool
	label      string // "STDOUT"/"STDERR"/"STDIN" for inspect
	path       string // backing file path for a File stream (else "")
	writable   bool   // a File opened for writing — flush the buffer on flush/close
	lineno     int    // #lineno — advanced by each successful line read (gets/readline)
	rdClosed   bool   // #close_read (or a write-only mode) — reads raise "not opened for reading"
	wrClosed   bool   // #close_write (or a read-only mode) — writes raise "not opened for writing"
	appendMode bool   // opened in append mode ("a"/"a+") — every write lands at end-of-buffer
	extEnc     string // external encoding name, "" ⇒ Encoding.default_external
	intEnc     string // internal encoding name, "" ⇒ none (nil)
	fd         int    // synthetic file descriptor for #fileno (0 ⇒ not yet assigned)

	// Pipe ends (IO.pipe) share a single byte buffer in *pipe. The write end
	// appends; the read end drains from pipe.rpos. Because subprocess execution
	// in this VM is synchronous (Process.spawn / Kernel.exec run to completion
	// before returning), the buffer is fully populated by the time a reader
	// drains it, so a buffered model faithfully reproduces the blocking-read and
	// EOF behaviour Puppet's execute loop relies on. reopened tracks the IO a
	// standard stream (STDOUT/STDERR) was rebound to via #reopen, so a forked
	// block's writes (and Kernel.exec's captured output) land on the pipe.
	pipe       *pipeBuf
	isWriteEnd bool
	reopened   *IOObj
}

// pipeBuf is the shared byte channel behind an IO.pipe reader/writer pair.
type pipeBuf struct {
	data    []byte
	rpos    int
	wClosed bool
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
	// A standard stream reopened onto another IO (Puppet's safe_posix_fork does
	// STDOUT.reopen(pipe_writer)) forwards its writes to that target. Follow the
	// chain iteratively with a cycle guard so a degenerate self/loop reopen
	// (e.g. STDOUT.reopen($stdout)) cannot recurse without bound.
	for cur, seen := o, map[*IOObj]bool{}; cur.reopened != nil && cur.reopened != cur && !seen[cur]; {
		seen[cur] = true
		cur = cur.reopened
		o = cur
	}
	if o.pipe != nil && o.isWriteEnd {
		o.pipe.data = append(o.pipe.data, p...)
		return len(p)
	}
	if o.isStr {
		if o.appendMode { // append mode: every write lands at end, ignoring position
			o.pos = len(o.buf)
		}
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

// pipeRefresh snapshots a pipe reader's shared buffer into the IOObj's own
// buf/pos view so the existing StringIO read methods (read/gets/eof?) operate on
// the latest pipe contents. It is a no-op for non-pipe streams.
func (o *IOObj) pipeRefresh() {
	if o.pipe != nil && !o.isWriteEnd {
		o.buf = o.pipe.data
	}
}

// pipeWriterClosed reports whether the write end of a pipe reader has been
// closed (EOF) — used by read_nonblock and IO.select to model EOF.
func (o *IOObj) pipeWriterClosed() bool {
	return o.pipe != nil && o.pipe.wClosed
}

// registerIO installs the IO class with the writing methods, the StringIO class
// (read + write), and the standard streams as both globals ($stdout/$stderr/
// $stdin) and constants (STDOUT/STDERR/STDIN). Kernel#puts/print/p are routed
// through the current $stdout so reassigning it (e.g. to a StringIO) captures
// output, as in MRI.
func (vm *VM) registerIO() {
	cIO := newClass("IO", vm.cObject)
	vm.consts["IO"] = cIO
	// The IO#seek whence constants live on IO (File inherits them, File < IO). IO
	// also mixes in File::Constants so IO::RDONLY and friends resolve, as in MRI.
	for name, val := range map[string]int64{
		"SEEK_SET": 0, "SEEK_CUR": 1, "SEEK_END": 2, "SEEK_DATA": 3, "SEEK_HOLE": 4,
	} {
		cIO.consts[name] = object.IntValue(val)
	}
	if fc, ok := vm.consts["File"].(*RClass).consts["Constants"].(*RClass); ok {
		cIO.includes = append(cIO.includes, fc)
	}
	// IO.try_convert(obj): obj if it is an IO, else its #to_io conversion, else nil.
	cIO.smethods["try_convert"] = &Method{name: "try_convert", owner: cIO,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.tryConvert(args[0], cIO, "to_io")
		}}
	defIOWrite(cIO)
	defStringIORead(cIO) // IO carries the read protocol too ($stdin, File streams)
	defIOReadExtra(cIO)
	defIOSeekable(cIO) // pread/pwrite/sysseek/binmode?/autoclose — IO+File, not StringIO

	// fileno / to_i: the stream's descriptor. The standard streams are 0/1/2; any
	// other IO gets a distinct synthetic descriptor on first request. rbgo has no
	// real file descriptors, so this is an identity, not an OS fd. StringIO has no
	// #fileno, so it is defined only here on IO. A closed stream raises IOError.
	vm.nextFd = 2 // synthetic descriptors for non-standard streams start at 3
	vm.fdTable = map[int]*IOObj{}
	fileno := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed {
			raise("IOError", "closed stream")
		}
		switch o.label {
		case "STDIN":
			vm.fdTable[0] = o
			return object.IntValue(0)
		case "STDOUT":
			vm.fdTable[1] = o
			return object.IntValue(1)
		case "STDERR":
			vm.fdTable[2] = o
			return object.IntValue(2)
		}
		if o.fd == 0 {
			vm.nextFd++
			o.fd = vm.nextFd
			vm.fdTable[o.fd] = o
		}
		return object.IntValue(int64(o.fd))
	}
	cIO.define("fileno", fileno)
	cIO.define("to_i", fileno)

	cStringIO := newClass("StringIO", vm.cObject)
	vm.consts["StringIO"] = cStringIO
	defIOWrite(cStringIO)
	defStringIORead(cStringIO)
	defIOReadExtra(cStringIO)
	cStringIO.smethods["new"] = &Method{name: "new", owner: cStringIO, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		o := &IOObj{cls: cStringIO, isStr: true}
		// A trailing options Hash may carry mode: (StringIO.new(str, mode: "r")).
		mode, modeGiven := "", false
		if h, ok := lastHash(args); ok {
			if v, ok := h.Get(object.Symbol("mode")); ok {
				mode, modeGiven = stringIOModeString(v), true
			}
			args = args[:len(args)-1]
		}
		frozenSrc := false
		if len(args) > 0 && !object.IsNil(args[0]) {
			s, ok := args[0].(*object.String)
			if !ok {
				raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
			}
			o.buf = append([]byte(nil), s.Bytes()...)
			frozenSrc = s.Frozen
		}
		if len(args) > 1 && !object.IsNil(args[1]) {
			mode, modeGiven = stringIOModeString(args[1]), true
		}
		if !modeGiven {
			// With no explicit mode, a frozen backing string opens read-only; a
			// mutable one (or none) is read/write, matching MRI.
			mode = "r+"
			if frozenSrc {
				mode = "r"
			}
		}
		read, write, trunc, appnd := stringIOModeFlags(mode)
		o.rdClosed, o.wrClosed, o.appendMode = !read, !write, appnd
		if trunc {
			o.buf = nil
		}
		return o
	}}

	stdout := &IOObj{cls: cIO, w: vm.out, label: "STDOUT"}
	stderr := &IOObj{cls: cIO, w: vm.errOut, label: "STDERR"}
	stdin := &IOObj{cls: cIO, isStr: true, label: "STDIN"} // empty input by default
	vm.consts["STDOUT"], vm.consts["STDERR"], vm.consts["STDIN"] = stdout, stderr, stdin
	vm.globals["$stdout"], vm.globals["$stderr"], vm.globals["$stdin"] = stdout, stderr, stdin

	// Kernel#warn builds one message — each argument on its own line, a newline
	// appended only when it does not already end with one — and routes it through
	// Warning.warn(message, category:) so a program that overrides Warning.warn (or
	// reads the category) sees it, as in MRI. A String category is converted to a
	// Symbol (anything else is a TypeError); a category whose warnings are disabled
	// (Warning[category] is false) is dropped, and an unknown category raises. The
	// uplevel: keyword is validated (a negative or non-Integer value raises) but not
	// acted on, as rbgo has no caller line to prepend. With no message it does
	// nothing.
	vm.cObject.define("warn", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var category object.Value = object.NilV
		pos := args
		if kw := trailingKwHash(args); kw != nil {
			pos = args[:len(args)-1]
			if v, ok := kw.Get(object.SymVal("category")); ok {
				category = v
			}
			if v, ok := kw.Get(object.SymVal("uplevel")); ok && !object.IsNil(v) {
				n, isInt := v.(object.Integer)
				if !isInt {
					raise("TypeError", "no implicit conversion of %s into Integer", vm.classOf(v).name)
				}
				if int64(n) < 0 {
					raise("ArgumentError", "negative level (%d)", int64(n))
				}
			}
		}
		var b strings.Builder
		for _, a := range pos {
			s := vm.displayStr(a)
			b.WriteString(s)
			if !strings.HasSuffix(s, "\n") {
				b.WriteByte('\n')
			}
		}
		if b.Len() == 0 {
			return object.NilV
		}
		if !object.IsNil(category) {
			switch c := category.(type) {
			case object.Symbol:
			case *object.String:
				category = object.Symbol(c.Str())
			default:
				raise("TypeError", "no implicit conversion of %s into Symbol", vm.classOf(category).name)
			}
			// Warning[category] filters the message (and raises for an unknown one).
			if !vm.send(vm.consts["Warning"], "[]", []object.Value{category}, nil).Truthy() {
				return object.NilV
			}
		}
		kw := object.NewHash()
		kw.Set(object.SymVal("category"), category)
		return vm.send(vm.consts["Warning"], "warn", []object.Value{object.NewString(b.String()), kw}, nil)
	})

	// File streams: File.open returns a buffered, file-backed IO carrying the
	// same read+write protocol (File acts as an IO subtype). The block form
	// flushes and closes afterwards, returning the block's value.
	cFile := vm.consts["File"].(*RClass)
	cFile.super = cIO // File < IO, inheriting the read+write protocol; is_a?(IO) holds
	cFile.smethods["open"] = &Method{name: "open", owner: cFile, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		o := openFileIO(cFile, pathArg(vm, args[0]), fileMode(args))
		if blk != nil {
			defer ioFlushClose(o)
			return vm.callBlock(blk, []object.Value{o})
		}
		return o
	}}
	// File.new opens a file-backed IO like File.open, but never takes a block (it
	// always returns the open stream). A missing path argument is an ArgumentError.
	cFile.smethods["new"] = &Method{name: "new", owner: cFile, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		return openFileIO(cFile, pathArg(vm, args[0]), fileMode(args))
	}}
	// File instance metadata operations. Puppet's replace_file writes to a
	// Uniquefile (a DelegateClass(File)) and then chmod/chowns it before renaming
	// it into place, so the open File needs path/chmod/chown that act on its
	// backing path. They flush the buffer first so the on-disk file reflects the
	// writes the caller has made.
	cFile.define("path", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*IOObj).path)
	})
	cFile.define("to_path", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*IOObj).path)
	})
	cFile.define("chmod", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioFlush(o)
		if err := fileChmod(o.path, os.FileMode(intArg(args[0])&0o7777)); err != nil {
			raise("Errno::ENOENT", "No such file or directory @ apply2files - %s", o.path)
		}
		return object.IntValue(0)
	})
	cFile.define("chown", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioFlush(o)
		if err := fileChown(o.path, chownID(args[0]), chownID(args[1])); err != nil {
			raise("Errno::ENOENT", "No such file or directory @ apply2files - %s", o.path)
		}
		return object.IntValue(0)
	})
	// IO.read/binread/write/binwrite/foreach/readlines, installed identically on IO
	// and File (File.readlines/foreach included) now that both classes exist.
	vm.registerIOClassMethods(cIO, cFile)

	// Kernel#open opens a path the way File.open does (block form yields the stream
	// and closes it afterwards). The "|command" subprocess form is not supported in
	// this synchronous VM, so it surfaces as Errno::ENOENT — a caller that rescues a
	// failed open then treats the argument as a real path, as MRI code commonly does.
	vm.cObject.define("open", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		if name := pathArg(vm, args[0]); strings.HasPrefix(name, "|") {
			raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", name)
		}
		return vm.send(cFile, "open", args, blk)
	})
}

// File open-mode flag constants (File::RDWR etc.). The values are the canonical
// POSIX ones, fixed here so behaviour is identical on every OS the test gate runs
// on rather than reflecting the host's <fcntl.h>.
const (
	fO_RDONLY = 0x0
	fO_WRONLY = 0x1
	fO_RDWR   = 0x2
	fO_APPEND = 0x8
	fO_CREAT  = 0x40
	fO_EXCL   = 0x80
	fO_TRUNC  = 0x200
)

// fileFlagConsts maps File::Constants open-mode names to their numeric flag
// value (a bitwise OR of these is what File.open's integer-mode form accepts).
var fileFlagConsts = map[string]int64{
	"RDONLY": fO_RDONLY, "WRONLY": fO_WRONLY, "RDWR": fO_RDWR,
	"APPEND": fO_APPEND, "CREAT": fO_CREAT, "EXCL": fO_EXCL, "TRUNC": fO_TRUNC,
}

// fileExtraConsts holds the remaining File::Constants that are not open-mode
// flags: the flock() operations (LOCK_*) and the open flags rbgo does not act on
// but which must still be defined (fixed to their canonical Linux values here, as
// the specs only assert the constants exist).
var fileExtraConsts = map[string]int64{
	"LOCK_SH": 0x1, "LOCK_EX": 0x2, "LOCK_NB": 0x4, "LOCK_UN": 0x8,
	"NONBLOCK": 0x800, "NOCTTY": 0x100, "SYNC": 0x101000, "SHARE_DELETE": 0x0,
}

// flagsToMode maps an integer open-mode (a bitwise OR of File::RDWR/CREAT/...) to
// the access-mode string openFileIO understands. RDWR/WRONLY without APPEND or
// O_RDONLY-only reads map to the closest fopen-style mode; APPEND maps to "a".
func flagsToMode(flags int64) string {
	switch {
	case flags&fO_APPEND != 0:
		if flags&fO_RDWR != 0 {
			return "a+"
		}
		return "a"
	case flags&fO_RDWR != 0:
		// RDWR with CREAT (the Uniquefile temp-file path) starts from an empty,
		// freshly created file, like "w+"; plain RDWR keeps existing content, "r+".
		if flags&fO_CREAT != 0 {
			return "w+"
		}
		return "r+"
	case flags&fO_WRONLY != 0:
		return "w"
	default:
		return "r"
	}
}

// fileMode returns the access mode argument of File.open (default "r"). The mode
// may be a string ("w", "r+", ...) or an integer bit-OR of File::Constants flags
// (e.g. File::RDWR | File::CREAT | File::EXCL); a trailing opts Hash is ignored.
func fileMode(args []object.Value) string {
	if len(args) > 1 {
		if i, ok := args[1].(object.Integer); ok {
			return flagsToMode(int64(i))
		}
		return strArg(args[1])
	}
	return "r"
}

// stringIOModeString normalises a StringIO mode argument (a String like "r+" or
// an Integer bit-OR of File::Constants flags) to an access-mode string.
func stringIOModeString(v object.Value) string {
	if i, ok := v.(object.Integer); ok {
		return flagsToMode(int64(i))
	}
	return strArg(v)
}

// stringIOModeFlags decodes a StringIO access mode ("r", "r+", "w", "w+", "a",
// "a+") into its capabilities: whether reads and writes are permitted, whether
// the buffer is truncated on open ("w"/"w+"), and whether writes append ("a"/"a+").
// The mode may carry trailing flags (":BINARY", "b", "t"), which do not affect
// these bits. An unrecognised mode raises ArgumentError, as MRI does.
func stringIOModeFlags(mode string) (read, write, trunc, appnd bool) {
	base := mode
	if i := strings.IndexByte(base, ':'); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimRight(base, "bt")
	plus := strings.Contains(base, "+")
	switch {
	case strings.HasPrefix(base, "r"):
		return true, plus, false, false
	case strings.HasPrefix(base, "w"):
		return plus, true, true, false
	case strings.HasPrefix(base, "a"):
		return plus, true, false, true
	}
	raise("ArgumentError", "invalid access mode %s", mode)
	return false, false, false, false
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
		if notRegular(p) {
			// Nothing that is not a regular file can be read whole, because
			// some of them do not end: File.open("/dev/zero") allocated 83 GB
			// here and killed a CI runner before the open returned. A character
			// device, a fifo or a socket opens with an empty buffer instead, so
			// the position arithmetic works — which is all core/io/seek_spec.rb
			// asks of /dev/zero — and a read sees end-of-file rather than the
			// machine going away.
			//
			// That reads see nothing is a limit of a buffer-backed IO rather
			// than a decision: see IOObj, whose whole model is the file's bytes
			// in memory, and which is that way for stated reasons.
			o.writable = strings.Contains(mode, "+")
			break
		}
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
		if notRegular(p) {
			o.writable = true // as above: nothing to append to that can be read
			break
		}
		b, _ := os.ReadFile(p) // append to the existing content (or a new file)
		o.buf, o.pos, o.writable = b, len(b), true
	default:
		raise("ArgumentError", "invalid access mode %s", mode)
	}
	// Enforce the mode's access half: a write-only ("w"/"a") stream raises on
	// read, a read-only ("r") stream raises on write, matching MRI. A "+" mode
	// permits both. Append modes ("a"/"a+") force every write to end-of-buffer.
	plus := strings.Contains(mode, "+")
	switch mode[0] {
	case 'w', 'a':
		o.rdClosed = !plus
		o.appendMode = mode[0] == 'a'
	case 'r':
		o.wrClosed = !plus
	}
	return o
}

// notRegular reports whether a path names something other than a regular file —
// a device, a fifo, a socket. A path that does not exist is not one of those:
// the caller raises ENOENT for it, and answering true here would swallow that.
func notRegular(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.Mode().IsRegular()
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
	cls.define("write", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		n := 0
		for _, a := range args {
			n += o.writeStr(vm.displayStr(a))
		}
		return object.IntValue(int64(n))
	})
	cls.define("<<", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		o.writeStr(vm.displayStr(args[0]))
		return self
	})
	cls.define("print", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		// With no arguments, #print writes $_ (the last line read). Multiple
		// arguments are separated by the output field separator $, (when set) and
		// the whole is terminated by the output record separator $\ (when set).
		if len(args) == 0 {
			if lastLine := vm.gvar("$_"); !object.IsNil(lastLine) {
				o.writeStr(vm.displayStr(lastLine))
			}
		} else {
			ofs, ofsSet := vm.optStrGlobal("$,")
			for i, a := range args {
				if i > 0 && ofsSet {
					o.writeStr(ofs)
				}
				o.writeStr(vm.displayStr(a))
			}
		}
		if ors, ok := vm.optStrGlobal("$\\"); ok {
			o.writeStr(ors)
		}
		return object.NilV
	})
	cls.define("puts", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ioCheckOpen(self.(*IOObj))
		vm.ioPuts(self, args)
		return object.NilV
	})
	cls.define("printf", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		o.writeStr(vm.formatString(args[0].ToS(), args[1:]))
		return object.NilV
	})
	cls.define("putc", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		switch a := args[0].(type) {
		case object.Integer:
			o.writeBytes([]byte{byte(a)})
		case *object.String:
			if len(a.Bytes()) > 0 {
				o.writeBytes(a.Bytes()[:1])
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
	cls.define("fsync", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return object.IntValue(0) })
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
		if o.pipe != nil && o.isWriteEnd {
			o.pipe.wClosed = true // signal EOF to the read end
		}
		o.closed = true
		return object.NilV
	})
	cls.define("closed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*IOObj).closed)
	})
	cls.define("tty?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return object.Bool(false) })
	cls.define("isatty", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return object.Bool(false) })
	cls.define("binmode", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self })

	// external_encoding: the stream's external encoding — the one set explicitly
	// (at creation or via #set_encoding), else Encoding.default_external.
	cls.define("external_encoding", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.extEnc != "" {
			if e, ok := vm.findEncoding(o.extEnc); ok {
				return e
			}
		}
		// A write-only stream with no explicit encoding reports nil (unless a
		// default internal encoding forces transcoding); a readable one reports
		// Encoding.default_external.
		if o.writable && object.IsNil(vm.send(vm.cEncoding, "default_internal", nil, nil)) {
			return object.NilV
		}
		return vm.send(vm.cEncoding, "default_external", nil, nil)
	})
	// internal_encoding: the encoding reads are transcoded to, or nil when none.
	cls.define("internal_encoding", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.intEnc != "" {
			if e, ok := vm.findEncoding(o.intEnc); ok {
				return e
			}
		}
		return object.NilV
	})
	// set_encoding(ext, int = nil): set the external (and optional internal)
	// encoding. ext may be an Encoding, an encoding name, or a combined
	// "external:internal" string; a nil argument clears that side. Returns self.
	cls.define("set_encoding", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		o.extEnc, o.intEnc = "", ""
		if len(args) > 0 && !object.IsNil(args[0]) {
			if s, ok := args[0].(*object.String); ok {
				if i := strings.IndexByte(s.Str(), ':'); i >= 0 && len(args) == 1 {
					o.extEnc = vm.lookupEncodingName(s.Str()[:i]).name
					o.intEnc = vm.lookupEncodingName(s.Str()[i+1:]).name
				} else {
					o.extEnc = vm.encodingArg(args[0]).name
				}
			} else {
				o.extEnc = vm.encodingArg(args[0]).name
			}
		}
		if len(args) > 1 && !object.IsNil(args[1]) {
			o.intEnc = vm.encodingArg(args[1]).name
		}
		return o
	})
}

// defStringIORead defines the reading half of the protocol, plus the cursor and
// content methods, on StringIO.
func defStringIORead(cls *RClass) {
	cls.define("string", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(string(self.(*IOObj).buf))
	})
	cls.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(self.(*IOObj).buf)))
	})
	cls.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(self.(*IOObj).buf)))
	})
	cls.define("eof?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		o.pipeRefresh()
		return object.Bool(o.pos >= len(o.buf))
	})
	cls.define("eof", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		o.pipeRefresh()
		return object.Bool(o.pos >= len(o.buf))
	})
	cls.define("pos", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*IOObj).pos))
	})
	cls.define("tell", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*IOObj).pos))
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
		return object.IntValue(0)
	})
	cls.define("rewind", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		self.(*IOObj).pos = 0
		return object.IntValue(0)
	})
	cls.define("truncate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		n := int(toInt(args[0]))
		if n < len(o.buf) {
			o.buf = o.buf[:n]
		} else if n > len(o.buf) {
			o.buf = append(o.buf, make([]byte, n-len(o.buf))...)
		}
		return object.IntValue(0)
	})
	cls.define("read", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		o.pipeRefresh()
		lengthGiven := len(args) > 0 && !object.IsNil(args[0])
		// An optional second argument is an output-buffer String (coerced via
		// #to_str): the read fills it (and is returned in its place), and it is
		// cleared when the read yields nil.
		var buf *object.String
		if len(args) > 1 {
			buf = vm.ioBufferArg(args[1])
		}
		result := func(data []byte, isNil bool) object.Value {
			if buf != nil {
				if buf.Frozen {
					raise("FrozenError", "can't modify frozen String: %s", buf.Inspect())
				}
				if isNil {
					buf.SetBytes(nil)
					return object.NilV
				}
				buf.SetBytes(append([]byte(nil), data...))
				return buf
			}
			if isNil {
				return object.NilV
			}
			// read(length) returns a binary (ASCII-8BIT) String; read with no
			// length returns the remainder in the stream's default encoding.
			if lengthGiven {
				return object.NewStringBytesEnc(append([]byte(nil), data...), "ASCII-8BIT")
			}
			return object.NewStringBytes(append([]byte(nil), data...))
		}
		if lengthGiven {
			n := int(vm.toIntCoerce(args[0]))
			if n < 0 {
				raise("ArgumentError", "negative length %d given", n)
			}
			if o.pos >= len(o.buf) {
				return result(nil, n > 0) // a length read at EOF is nil; a 0-length read is ""
			}
			end := min(o.pos+n, len(o.buf))
			data := o.buf[o.pos:end]
			o.pos = end
			return result(data, false)
		}
		start := min(o.pos, len(o.buf)) // pos= may have moved past the end
		data := o.buf[start:]
		o.pos = len(o.buf)
		return result(data, false)
	})
	// readpartial(maxlen, out = nil): read up to maxlen bytes, blocking only until
	// some are available. On the in-memory buffer that is the leading maxlen bytes;
	// a zero maxlen yields ""; reading at end of input raises EOFError.
	cls.define("readpartial", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		o.pipeRefresh()
		n := int(toInt(args[0]))
		if n < 0 {
			raise("ArgumentError", "negative length %d given", n)
		}
		var buf *object.String
		if len(args) > 1 {
			if b, ok := args[1].(*object.String); ok {
				buf = b
			}
		}
		fill := func(data []byte) object.Value {
			if buf != nil {
				if buf.Frozen {
					raise("FrozenError", "can't modify frozen String: %s", buf.Inspect())
				}
				buf.SetBytes(append([]byte(nil), data...))
				return buf
			}
			return object.NewStringBytes(append([]byte(nil), data...))
		}
		if n == 0 {
			return fill(nil)
		}
		if o.pos >= len(o.buf) {
			raise("EOFError", "end of file reached")
		}
		end := min(o.pos+n, len(o.buf))
		data := o.buf[o.pos:end]
		o.pos = end
		return fill(data)
	})
	cls.define("getc", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		o.pipeRefresh()
		if o.pos >= len(o.buf) {
			return object.NilV
		}
		r, sz := utf8.DecodeRune(o.buf[o.pos:])
		s := object.NewString(string(r))
		o.pos += sz
		return s
	})
	// setLineGlobals records a successful line read the way MRI's IO#gets family
	// does. $. always becomes the reading IO's line number (a StringIO updates it
	// too). $_ (the "last read line") is set only by the single-line readers —
	// gets/readline (lastLine true) — not by the bulk readlines/each_line, which
	// leave $_ alone, matching MRI.
	setLineGlobals := func(vm *VM, o *IOObj, v object.Value, lastLine bool) {
		if v == object.NilV {
			return
		}
		vm.globals["$."] = object.IntValue(int64(o.lineno))
		if lastLine {
			vm.globals["$_"] = v
		}
	}
	gets := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		sep, limit, chomp := vm.resolveGetsArgs(args)
		v := vm.ioGetsResolved(o, sep, limit, chomp)
		if v == object.NilV {
			// Reading past the last line clears $_ (but leaves $. reporting the
			// final line number), matching MRI.
			vm.globals["$_"] = object.NilV
		} else {
			setLineGlobals(vm, o, v, true)
		}
		return v
	}
	cls.define("gets", gets)
	cls.define("readline", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		v := gets(vm, self, args, nil)
		if v == object.NilV {
			raise("EOFError", "end of file reached")
		}
		return v
	})
	cls.define("readlines", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o)
		sep, limit, chomp := vm.resolveGetsArgs(args)
		checkResolvedLimit(limit, "readlines")
		var lines []object.Value
		for {
			v := vm.ioGetsResolved(o, sep, limit, chomp)
			if v == object.NilV {
				break
			}
			setLineGlobals(vm, o, v, false)
			lines = append(lines, v)
		}
		return object.NewArrayFromSlice(lines)
	})
	cls.define("each_line", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		o := self.(*IOObj)
		if blk == nil { // no block ⇒ an Enumerator (buildable even on a closed stream)
			return enumForSized(self, "each_line", enumSizeNil, args...)
		}
		ioCheckReadable(o) // iterating a closed/unreadable stream raises, as in MRI
		sep, limit, chomp := vm.resolveGetsArgs(args)
		checkResolvedLimit(limit, "each_line")
		for {
			v := vm.ioGetsResolved(o, sep, limit, chomp)
			if v == object.NilV {
				break
			}
			setLineGlobals(vm, o, v, false)
			vm.callBlock(blk, []object.Value{v})
		}
		return self
	})
	cls.define("each_char", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		o := self.(*IOObj)
		if blk == nil { // no block ⇒ an Enumerator (buildable even on a closed stream)
			return enumForSized(self, "each_char", enumSizeNil)
		}
		ioCheckReadable(o) // iterating a closed/unreadable stream raises, as in MRI
		for o.pos < len(o.buf) {
			r, sz := utf8.DecodeRune(o.buf[o.pos:])
			o.pos += sz
			vm.callBlock(blk, []object.Value{object.NewString(string(r))})
		}
		return self
	})
	// #each is a true alias of #each_line — it must share the method entry so
	// IO.instance_method(:each) == IO.instance_method(:each_line), as in MRI.
	cls.methods["each"] = cls.methods["each_line"]
}

// ioGets reads one line (up to and including the separator, default "\n") from a
// StringIO, returning nil at end of input. It accepts MRI's (sep, limit, chomp:)
// argument shapes: a leading Integer positional is the byte limit (separator
// defaults to "\n"); a String or nil is the separator, optionally followed by an
// Integer limit; a trailing chomp: true strips the separator from the result. A
// successful (non-nil) read advances #lineno.
func ioGets(o *IOObj, args []object.Value) object.Value {
	o.pipeRefresh()
	sep, limit, chomp := parseGetsArgs(args)
	v := ioGetsLine(o, sep, limit, chomp)
	if v != object.NilV {
		o.lineno++
	}
	return v
}

// parseGetsArgs decodes the (sep, limit, chomp:) arguments of gets/readline/
// each_line. sepSet is false when the separator defaults to "\n"; a nil separator
// (read the whole remainder) is reported as sepSet with sep == "" and nilSep.
func parseGetsArgs(args []object.Value) (sep getsSep, limit int, chomp bool) {
	sep, limit = getsSep{s: "\n"}, -1
	if h, ok := lastHash(args); ok {
		if v, ok := h.Get(object.Symbol("chomp")); ok {
			chomp = v.Truthy()
		}
		args = args[:len(args)-1]
	}
	if len(args) > 0 {
		switch a := args[0].(type) {
		case object.Integer:
			limit = int(a)
		case *object.String:
			sep = getsSep{s: a.Str(), set: true}
		default:
			if args[0] == object.NilV {
				sep = getsSep{s: "", set: true, nilSep: true}
			}
		}
	}
	if len(args) > 1 {
		if n, ok := args[1].(object.Integer); ok {
			limit = int(n)
		}
	}
	return sep, limit, chomp
}

// enumSizeNil is the #size block for a reader Enumerator (each_line/each_char/
// each_byte with no block): the number of elements a buffered read will yield is
// not known in advance, so #size is nil, matching MRI.
func enumSizeNil(*VM) object.Value { return object.NilV }

// ioGetsResolved reads one line from o with already-resolved (sep, limit, chomp)
// — the coercion of the separator/limit arguments has been done once by the
// caller so a bulk reader (readlines/each_line) never re-runs a #to_str/#to_int
// side effect per line. A successful (non-nil) read advances #lineno.
func (vm *VM) ioGetsResolved(o *IOObj, sep getsSep, limit int, chomp bool) object.Value {
	o.pipeRefresh()
	v := ioGetsLine(o, sep, limit, chomp)
	if v != object.NilV {
		o.lineno++
	}
	return v
}

// resolveGetsArgs decodes the (sep, limit, chomp:) arguments of the StringIO/IO
// gets family, coercing a non-String separator via #to_str and a non-Integer
// limit via #to_int exactly once, and defaulting the separator to $/ (the input
// record separator) when none is given. A nil separator selects whole-remainder
// mode; an Integer first positional is the byte limit.
func (vm *VM) resolveGetsArgs(args []object.Value) (sep getsSep, limit int, chomp bool) {
	sep, limit = vm.defaultGetsSep(), -1
	if h, ok := lastHash(args); ok {
		if v, ok := h.Get(object.Symbol("chomp")); ok {
			chomp = v.Truthy()
		}
		args = args[:len(args)-1]
	}
	if len(args) > 0 {
		switch a := args[0].(type) {
		case object.Integer:
			limit = int(a)
		case *object.String:
			sep = getsSep{s: a.Str(), set: true}
		default:
			switch {
			case object.IsNil(args[0]):
				sep = getsSep{s: "", set: true, nilSep: true}
			case vm.respondsToDynamic(args[0], "to_str"):
				// A #to_str-convertible first argument is the separator (MRI checks
				// the String conversion before the Integer one).
				sep = getsSep{s: string(vm.strArgConv(args, 0)), set: true}
			default:
				// Otherwise a single non-String positional is the byte limit,
				// coerced via #to_int (gets(obj) where obj defines only #to_int).
				limit = int(vm.toIntCoerce(args[0]))
			}
		}
	}
	if len(args) > 1 && !object.IsNil(args[1]) {
		limit = int(vm.toIntCoerce(args[1]))
	}
	return sep, limit, chomp
}

// optChomp returns whether chomp is in effect given a separated options Hash
// (IO.readlines/foreach carry their keyword arguments there rather than as a
// trailing positional Hash): a chomp: entry in opts overrides the incoming value.
func optChomp(opts *object.Hash, cur bool) bool {
	if opts != nil {
		if v, ok := opts.Get(object.Symbol("chomp")); ok {
			return v.Truthy()
		}
	}
	return cur
}

// defaultGetsSep returns the separator used when gets is called with no explicit
// one: the input record separator $/ when it holds a String, else "\n".
func (vm *VM) defaultGetsSep() getsSep {
	if s, ok := vm.gvar("$/").(*object.String); ok {
		return getsSep{s: s.Str(), set: true}
	}
	return getsSep{s: "\n"}
}

// checkResolvedLimit raises ArgumentError for an explicit limit of 0 on a
// line-iterating read (readlines/each_line), matching MRI (a 0-byte line would
// never advance the cursor).
func checkResolvedLimit(limit int, meth string) {
	if limit == 0 {
		raise("ArgumentError", "invalid limit: 0 for %s", meth)
	}
}

// checkGetsLimit raises ArgumentError for an explicit limit of 0 on a
// line-iterating read (readlines/each_line/foreach/each): a 0-byte line never
// advances the cursor, so MRI rejects it rather than looping. A single #gets(0)
// is allowed (it just returns "") and does not go through this guard.
func checkGetsLimit(args []object.Value, meth string) {
	if _, limit, _ := parseGetsArgs(args); limit == 0 {
		raise("ArgumentError", "invalid limit: 0 for %s", meth)
	}
}

// getsSep is a resolved line separator for ioGetsLine.
type getsSep struct {
	s      string
	set    bool // an explicit separator was given (else the "\n" default)
	nilSep bool // an explicit nil separator: read the whole remainder as one line
}

// ioGetsLine reads one line honouring the separator, an optional byte limit
// (negative for none) and chomp, advancing the cursor. It returns nil at EOF.
func ioGetsLine(o *IOObj, sep getsSep, limit int, chomp bool) object.Value {
	if o.pos >= len(o.buf) {
		return object.NilV
	}
	if sep.set && sep.s == "" && !sep.nilSep { // an empty separator selects paragraph mode
		return ioGetsParagraph(o, limit, chomp)
	}
	rest := o.buf[o.pos:]
	end := len(o.buf) // default: read to end (nil separator, or separator not found)
	if !sep.nilSep {
		if i := strings.Index(string(rest), sep.s); i >= 0 {
			end = o.pos + i + len(sep.s)
		}
	}
	if limit >= 0 && o.pos+limit < end {
		end = o.pos + limit
	}
	line := o.buf[o.pos:end]
	o.pos = end
	if chomp && !sep.nilSep {
		line = getsChomp(line, sep.s)
	}
	return object.NewString(string(line))
}

// getsChomp removes a single trailing separator run from line (the "\n" default
// also strips a preceding "\r", matching MRI's universal-newline chomp).
func getsChomp(line []byte, sep string) []byte {
	if sep == "\n" {
		if n := len(line); n > 0 && line[n-1] == '\n' {
			line = line[:n-1]
			if n := len(line); n > 0 && line[n-1] == '\r' {
				line = line[:n-1]
			}
		}
		return line
	}
	return []byte(strings.TrimSuffix(string(line), sep))
}

// ioGetsParagraph implements gets/each_line paragraph mode (an empty separator):
// leading blank lines are skipped, then the paragraph up to the run of two or
// more newlines that terminates it (or end of input) is returned. A non-negative
// limit caps the number of bytes returned. A StringIO keeps the whole terminating
// newline run in the result, whereas a real IO/File keeps only the two newlines
// that close the paragraph (the rest are skipped as leading blanks on the next
// read) — matching MRI, whose StringIO and IO differ here.
func ioGetsParagraph(o *IOObj, limit int, chomp bool) object.Value {
	for o.pos < len(o.buf) && o.buf[o.pos] == '\n' { // skip leading blank lines
		o.pos++
	}
	if o.pos >= len(o.buf) {
		return object.NilV
	}
	start := o.pos
	end := len(o.buf)
	sepFound := false
	if idx := strings.Index(string(o.buf[start:]), "\n\n"); idx >= 0 {
		sepFound = true
		end = start + idx
		for end < len(o.buf) && o.buf[end] == '\n' { // consume the whole newline run
			end++
		}
		if !(o.cls != nil && o.cls.name == "StringIO") { // real IO keeps only two
			if two := start + idx + 2; two < end {
				end = two
			}
		}
	}
	if limit >= 0 && start+limit < end {
		end = start + limit
	}
	o.pos = end
	line := o.buf[start:end]
	if chomp && sepFound { // chomp strips the terminating newline run (the paragraph
		for len(line) > 0 && line[len(line)-1] == '\n' { // separator), not a lone EOF "\n"
			line = line[:len(line)-1]
		}
	}
	return object.NewString(string(line))
}

// ioPuts writes args to self with Kernel#puts semantics (arrays flattened, a
// trailing newline added unless already present; no args ⇒ a lone newline).
// Every piece is emitted through self's #write method (via vm.send) rather than
// the buffer directly, so a stream that overrides #write sees puts's output, as
// in MRI; each value is also stringified through its (possibly user-defined)
// #to_s.
func (vm *VM) ioPuts(self object.Value, args []object.Value) {
	write := func(s string) { vm.send(self, "write", []object.Value{object.NewString(s)}, nil) }
	if len(args) == 0 {
		write("\n")
		return
	}
	for _, a := range args {
		vm.ioPutsValueRec(write, a, nil)
	}
}

// ioPutsValueRec emits one puts value through write, guarding against a
// self-referential array: puts recurses into nested arrays (each element on its
// own line), so a member that is its own container is written as "[...]" (as MRI
// does) rather than looping forever. seen tracks the arrays currently expanding.
func (vm *VM) ioPutsValueRec(write func(string), v object.Value, seen map[*object.Array]struct{}) {
	if arr, ok := v.(*object.Array); ok {
		if _, rec := seen[arr]; rec {
			write("[...]\n")
			return
		}
		if seen == nil {
			seen = map[*object.Array]struct{}{}
		}
		seen[arr] = struct{}{}
		defer delete(seen, arr)
		// An empty array writes nothing (MRI), unlike a no-arg puts which writes a
		// lone newline.
		for _, e := range arr.Elems {
			vm.ioPutsValueRec(write, e, seen)
		}
		return
	}
	// A non-Array object that implements #to_ary is expanded like an Array (each
	// element on its own line), matching MRI's puts — which tries #to_ary before
	// falling back to #to_s.
	if vm.respondsToDynamic(v, "to_ary") {
		if arr, ok := vm.send(v, "to_ary", nil, nil).(*object.Array); ok {
			vm.ioPutsValueRec(write, arr, seen)
			return
		}
	}
	if s := vm.displayStr(v); strings.HasSuffix(s, "\n") {
		write(s)
	} else {
		write(s + "\n")
	}
}

// optStrGlobal reads a global that is meaningful only when set to a String — the
// output field/record separators $, and $\ that #print honours. It reports the
// separator and whether one is in effect (a nil or unset global ⇒ false).
func (vm *VM) optStrGlobal(name string) (string, bool) {
	if s, ok := vm.gvar(name).(*object.String); ok {
		return s.Str(), true
	}
	return "", false
}

// ioBufferArg resolves the output-buffer argument of #read/#readpartial to a
// mutable String, coercing a non-String via #to_str (as MRI does). Anything that
// is neither a String nor #to_str-convertible raises TypeError.
func (vm *VM) ioBufferArg(v object.Value) *object.String {
	if s, ok := v.(*object.String); ok {
		return s
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return nil
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

// ioCheckOpen raises IOError when writing to a closed stream (fully closed, or
// with its write half shut by #close_write).
func ioCheckOpen(o *IOObj) {
	if o.closed {
		raise("IOError", "closed stream")
	}
	if o.wrClosed {
		raise("IOError", "not opened for writing")
	}
}

// ioCheckReadable raises IOError when reading from a stream whose read half is
// unavailable: a fully closed stream ("closed stream") or one shut for reading by
// #close_read ("not opened for reading").
func ioCheckReadable(o *IOObj) {
	if o.closed {
		raise("IOError", "closed stream")
	}
	if o.rdClosed {
		raise("IOError", "not opened for reading")
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
