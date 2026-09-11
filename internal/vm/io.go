package vm

import (
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// ioFd returns o's descriptor for #fileno/#to_i/#inspect: the fixed 0/1/2 for
// the standard streams (registering them in the fd table), or a distinct
// synthetic descriptor assigned on first request for any other stream. rbgo has
// no OS fds, so this is an identity, not a real descriptor.
func (vm *VM) ioFd(o *IOObj) int {
	switch o.label {
	case "STDIN":
		vm.fdTable[0] = o
		return 0
	case "STDOUT":
		vm.fdTable[1] = o
		return 1
	case "STDERR":
		vm.fdTable[2] = o
		return 2
	}
	if o.fd == 0 {
		vm.nextFd++
		o.fd = vm.nextFd
		vm.fdTable[o.fd] = o
	}
	return o.fd
}

// ioPathString reports o's backing path (rb_io_path / rb_io_inspect's pathv): the
// explicit File/IO path, or a standard stream's "<NAME>" pseudo-path. The bool is
// false when no path is known (a plain fd wrapper or a pipe end), which #path
// renders as nil and #inspect as "fd N".
func ioPathString(o *IOObj) (string, bool) {
	if o.path != "" {
		return o.path, true
	}
	switch o.label {
	case "STDIN", "STDOUT", "STDERR":
		return "<" + o.label + ">", true
	}
	return "", false
}

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
	openMode   string // the fopen-style access mode a file stream was opened with ("r", "w+", "ab"…)
	nonblock   bool   // O_NONBLOCK is set (io/nonblock): false for a file, true for a pipe end
	// encSet records that #set_encoding has resolved this stream's encodings
	// against the defaults in force at that moment (io.c io_encoding_set →
	// rb_io_ext_int_to_enc). An empty extEnc then means "no encoding" — MRI's
	// fptr->encs.enc == NULL — rather than "fall back to Encoding.default_external",
	// so a later change to the defaults cannot move it.
	encSet      bool
	extEnc      string // external encoding name, "" ⇒ Encoding.default_external
	intEnc      string // internal encoding name, "" ⇒ none (nil)
	fd          int    // synthetic file descriptor for #fileno (0 ⇒ not yet assigned)
	binmode     bool   // opened in binary mode ("b"/binmode:) — #binmode? is true
	noAutoclose bool   // autoclose: false was requested — #autoclose? is false
	// close-on-exec defaults to set (#close_on_exec? is true), so the flag records
	// only its clearing — a zero-value IOObj reports close-on-exec, as MRI does.
	closeOnExecOff bool

	// strObj is the live String object backing a StringIO — MRI's StringIO holds
	// (and mutates in place) the very String passed to it, so #string returns that
	// same object (identity) and writes/truncation are visible through it. buf is
	// kept in sync with strObj on every mutation. strNil marks a StringIO whose
	// backing string has been dropped (set to nil), as StringIO.open does after
	// yielding: #string then returns nil.
	strObj    *object.String
	strNil    bool
	rdModeOff bool // StringIO opened without a read half (write-only mode) — #close_read raises
	wrModeOff bool // StringIO opened without a write half (read-only mode) — #close_write raises

	// Pipe ends (IO.pipe) share a single byte buffer in *pipe. The write end
	// appends; the read end drains from pipe.rpos. Because subprocess execution
	// in this VM is synchronous (Process.spawn / Kernel.exec run to completion
	// before returning), the buffer is fully populated by the time a reader
	// drains it, so a buffered model faithfully reproduces the blocking-read and
	// EOF behaviour Puppet's execute loop relies on. reopened tracks the IO a
	// standard stream (STDOUT/STDERR) was rebound to via #reopen, so a forked
	// block's writes (and Kernel.exec's captured output) land on the pipe.
	pipe       *pipeBuf
	pipeSynced int // bytes of pipe.data already folded into this reader's buf
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
		o.syncStr()
		if o.sync { // sync=true: a File's writes reach disk immediately (no buffering)
			ioFlush(o)
		}
		return len(p)
	}
	n, _ := o.w.Write(p)
	return n
}

func (o *IOObj) writeStr(s string) int { return o.writeBytes([]byte(s)) }

// syncStr mirrors the working buffer back into the backing String object of a
// StringIO so that #string (which returns that very object) and any external
// reference to it observe writes and truncation. It is a no-op for a real IO or
// a File (no backing String object).
func (o *IOObj) syncStr() {
	if o.strObj != nil {
		o.strObj.SetBytes(o.buf)
	}
}

// pipeRefresh folds any newly written pipe bytes into the reader's own buf/pos
// view so the existing StringIO read methods (read/gets/eof?) operate on the
// latest pipe contents. Bytes are appended rather than the whole buffer being
// re-snapshotted, so characters pushed back with #ungetc/#ungetbyte (which are
// spliced into buf ahead of the cursor) survive a refresh. It is a no-op for
// non-pipe streams.
func (o *IOObj) pipeRefresh() {
	if o.pipe != nil && !o.isWriteEnd && o.pipeSynced < len(o.pipe.data) {
		o.buf = append(o.buf, o.pipe.data[o.pipeSynced:]...)
		o.pipeSynced = len(o.pipe.data)
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
	// `require "fcntl"` installs the Fcntl constant module (ext/fcntl/fcntl.c),
	// and `require "io/nonblock"` the IO#nonblock accessors
	// (ext/io/nonblock/nonblock.c) — both lazily as MRI does, so neither resolves
	// before its require.
	if vm.featureHooks == nil {
		vm.featureHooks = map[string]func(){}
	}
	vm.featureHooks["fcntl"] = vm.installFcntl
	vm.featureHooks["io/nonblock"] = func() { installIONonblock(cIO) }
	// IO::WaitReadable / IO::WaitWritable (io.c Init_IO) are the marker modules a
	// non-blocking read or write raises with: IO::EAGAINWaitReadable is an
	// Errno::EAGAIN subclass that includes IO::WaitReadable, so `rescue
	// IO::WaitReadable` and `e.is_a?(Errno::EAGAIN)` both hold — MRI 4.0.5 on this
	// host reports IO::EAGAINWaitReadable "Resource temporarily unavailable - read
	// would block" and [true, true] for those two predicates. EWOULDBLOCK is EAGAIN
	// on every platform rbgo targets, so IO::EWOULDBLOCKWait* is the very same
	// class under a second name, as MRI makes it.
	eagain := vm.consts["Errno::EAGAIN"].(*RClass) // registered with File, above
	for _, w := range []string{"WaitReadable", "WaitWritable"} {
		mod := newClass("IO::"+w, nil)
		mod.isModule = true
		cIO.consts[w], vm.consts["IO::"+w] = mod, mod
		exc := newClass("IO::EAGAIN"+w, eagain)
		exc.includes = append(exc.includes, mod)
		for _, name := range []string{"EAGAIN" + w, "EWOULDBLOCK" + w} {
			cIO.consts[name], vm.consts["IO::"+name] = exc, exc
		}
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
		return object.IntValue(int64(vm.ioFd(o)))
	}
	cIO.define("fileno", fileno)
	cIO.methods["to_i"] = cIO.methods["fileno"] // #to_i is a true alias of #fileno

	// IO#to_io returns the IO itself (rb_io_to_io), for open or closed streams.
	cIO.define("to_io", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	// IO#path (rb_io_path) returns the stream's backing path: the path given at
	// creation (File.open, or IO.new's path: option), the "<STDIN>"/"<STDOUT>"/
	// "<STDERR>" pseudo-path of a standard stream, or nil when none is known (a
	// plain fd wrapper or a pipe end). IO#to_path is a true alias.
	cIO.define("path", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if p, ok := ioPathString(self.(*IOObj)); ok {
			return object.NewString(p)
		}
		return object.NilV
	})
	cIO.methods["to_path"] = cIO.methods["path"]
	// IO#inspect (rb_io_inspect): "#<Class:PATH>" when a path is known — a closed
	// path stream appends " (closed)" — else "#<Class:fd N>" for an open plain
	// descriptor and "#<Class:(closed)>" once it is closed. Defining it on IO (not
	// inheriting Object#inspect) makes IO its Method object's owner, as in MRI.
	cIO.define("inspect", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		var b strings.Builder
		b.WriteString("#<")
		b.WriteString(vm.classOf(self).name)
		b.WriteByte(':')
		if p, ok := ioPathString(o); ok {
			b.WriteString(p)
			if o.closed {
				b.WriteString(" (closed)")
			}
		} else if o.closed {
			b.WriteString("(closed)")
		} else {
			b.WriteString("fd ")
			b.WriteString(strconv.Itoa(vm.ioFd(o)))
		}
		b.WriteByte('>')
		return object.NewString(b.String())
	})
	// IO#close_on_exec? / #close_on_exec= (rb_io_close_on_exec_p / _set): the flag
	// defaults to set; assigning a false/nil value clears it, any other value sets
	// it. Both raise IOError on a closed stream.
	cIO.define("close_on_exec?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed {
			raise("IOError", "closed stream")
		}
		return object.Bool(!o.closeOnExecOff)
	})
	cIO.define("close_on_exec=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed {
			raise("IOError", "closed stream")
		}
		o.closeOnExecOff = !args[0].Truthy()
		return args[0]
	})
	// IO#dup (rb_io_dup) returns an independent IO on a duplicated descriptor: a
	// fresh synthetic fd, its own open/close state, and — as MRI documents — the
	// autoclose and close-on-exec flags always set on the new object regardless of
	// the receiver's. A closed receiver raises IOError.
	cIO.define("dup", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed {
			raise("IOError", "closed stream")
		}
		cp := *o
		cp.fd = 0                 // a distinct descriptor is assigned on first #fileno
		cp.noAutoclose = false    // dup always sets autoclose on the new IO
		cp.closeOnExecOff = false // dup always sets close-on-exec on the new IO
		vm.ioFd(&cp)              // assign it now so #fileno already differs from the receiver's
		return &cp
	})
	// IO#initialize (rb_io_initialize) reassociates the receiver with an existing
	// descriptor: the fd is coerced with #to_int (an IO/nil/String is a TypeError),
	// looked up (an unknown one raises Errno::EBADF, a closed one IOError), and the
	// receiver adopts its buffer, path and access mode.
	cIO.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		pos, opts := splitIOOpts(args)
		if len(pos) < 1 || len(pos) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(pos))
		}
		var fd int
		switch fv := pos[0].(type) {
		case object.Integer:
			fd = int(fv)
		default:
			if vm.respondsToDynamic(pos[0], "to_int") {
				fd = int(vm.repeatLong(pos[0]))
			} else {
				raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(pos[0]))
			}
		}
		src, ok := vm.fdTable[fd]
		if !ok {
			raise("Errno::EBADF", "Bad file descriptor - fd %d", fd)
		}
		if src.closed {
			raise("IOError", "closed stream")
		}
		o.fd, o.closed = fd, false
		vm.ioAdoptDescriptor(o, src, pos, opts)
		vm.fdTable[fd] = o // #fileno now returns this descriptor for the receiver
		return self
	})
	vm.setInstanceVisibility(cIO, "initialize", visPrivate) // MRI keeps #initialize private

	cStringIO := newClass("StringIO", vm.cObject)
	vm.consts["StringIO"] = cStringIO
	defIOWrite(cStringIO)
	defStringIORead(cStringIO)
	defIOReadExtra(cStringIO)
	defStringIOExtra(vm, cStringIO)

	// The standard streams carry their real access half: $stdout/$stderr are
	// write-only (a read raises "not opened for reading") and $stdin read-only (a
	// write raises "not opened for writing"), as MRI's fd modes dictate.
	stdout := &IOObj{cls: cIO, w: vm.out, label: "STDOUT", rdClosed: true}
	// STDERR is synchronous by default (MRI: STDERR.sync == true), unlike STDOUT.
	stderr := &IOObj{cls: cIO, w: vm.errOut, label: "STDERR", sync: true, rdClosed: true}
	stdin := &IOObj{cls: cIO, isStr: true, label: "STDIN", wrClosed: true} // empty input by default
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
		o := vm.openFileArgs(cFile, args) // openFileArgs rejects a missing path
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
		return vm.openFileArgs(cFile, args)
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

// stringIOModeVal resolves a StringIO mode argument to either an Integer flag set
// or a String access mode: Integer and String are taken directly, and any other
// object is converted via #to_str (raising TypeError otherwise). Keeping the
// Integer form (rather than mapping it to an fopen-style string) preserves the
// O_TRUNC / access bits that a lossy string mapping would drop.
func stringIOModeVal(vm *VM, v object.Value) object.Value {
	switch v.(type) {
	case object.Integer, *object.String:
		return v
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return nil
}

// stringIOFlagBits decodes an Integer StringIO mode (a bit-OR of IO/File open
// flags) into its capabilities. The access bits (RDONLY/WRONLY/RDWR) set read /
// write; O_TRUNC empties the buffer; O_APPEND forces writes to the end. Unlike a
// mapping through an fopen-style string, a bare WRONLY does not imply truncation.
func stringIOFlagBits(flags int64) (read, write, trunc, appnd bool) {
	switch flags & 0x3 { // O_RDONLY / O_WRONLY / O_RDWR
	case fO_WRONLY:
		read, write = false, true
	case fO_RDWR:
		read, write = true, true
	default: // fO_RDONLY
		read, write = true, false
	}
	return read, write, flags&fO_TRUNC != 0, flags&fO_APPEND != 0
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

// openFileArgs opens a file for File.open / File.new / Kernel#open, following
// io.c rb_scan_args "12:": the arguments are (path, [mode], [perm], **opts) with
// a trailing Hash taken as options rather than the mode. The access mode comes
// from the positional mode (a String or an Integer flag set) or the :mode
// option; the external/internal encoding comes from the mode string's
// ":ext[:int]" suffix or the :encoding / :external_encoding / :internal_encoding
// options (rb_io_extract_modeenc), and is recorded on the stream so reads honour
// it. A trailing Hash mode argument is thus no longer mistaken for a mode String.
func (vm *VM) openFileArgs(cls *RClass, args []object.Value) *IOObj {
	pos, opts := splitIOOpts(args)
	if len(pos) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
	}
	mode := "r"
	if len(pos) > 1 && !object.IsNil(pos[1]) {
		mode = vm.vmodeString(pos[1])
	} else if opts != nil {
		if m, ok := opts.Get(object.Symbol("mode")); ok && !object.IsNil(m) {
			mode = vm.vmodeString(m)
		}
	}
	o := openFileIO(cls, pathArg(vm, pos[0]), mode)
	ms := vm.ioResolveModeEnc(pos, opts)
	o.extEnc, o.intEnc, o.binmode = ms.extEnc, ms.intEnc, ms.binmode
	return o
}

// vmodeString reduces a File.open mode argument to the fopen-style base mode
// string openFileIO understands: an Integer (or #to_int object) is mapped through
// flagsToMode; a String is taken as-is (its ":enc" suffix is tolerated and later
// resolved for encoding); any other object is coerced with #to_str.
func (vm *VM) vmodeString(v object.Value) string {
	if i, ok := v.(object.Integer); ok {
		return flagsToMode(int64(i))
	}
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	if vm.respondsToDynamic(v, "to_int") {
		return flagsToMode(vm.repeatLong(v))
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s.Str()
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return ""
}

// openFileIO opens path into a buffered, file-backed IOObj per mode (r/w/a, with
// an optional "+" making a read mode writable). The file's bytes are read into
// the buffer; writes accumulate there and are flushed back on flush/close.
func openFileIO(cls *RClass, p, mode string) *IOObj {
	if mode == "" {
		raise("ArgumentError", "invalid access mode %s", mode)
	}
	o := &IOObj{cls: cls, isStr: true, path: p, openMode: mode}
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
		b, err := os.ReadFile(p) // append to the existing content (or a new file)
		if err != nil {
			// O_APPEND carries O_CREAT in MRI's "a"/"a+" fmode (io.c
			// rb_io_fmode_oflags: FMODE_APPEND ⇒ O_CREAT|O_APPEND), so the file
			// exists on disk as soon as it is opened — `File.open(p, "a")` then
			// `File.exist?(p)` is true before any write. Materialise it now, as
			// the 'w' branch above does for O_CREAT|O_TRUNC.
			if werr := os.WriteFile(p, nil, 0o644); werr != nil {
				raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", p)
			}
		}
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

// ioWriteAll writes every argument to o and returns the total byte count, the
// shared core of IO#write and IO#write_nonblock. Each argument is coerced to a
// String (rb_obj_as_string / #to_s); an all-empty write returns 0 before any
// closed/writable check (io.c io_write returns before rb_io_check_writable when
// the string is empty), so writing "" to a read-only or closed stream does not
// raise, while a non-empty write does. A non-BINARY external encoding transcodes
// each argument (ioWriteEncode).
func (vm *VM) ioWriteAll(o *IOObj, args []object.Value) int64 {
	strs := make([]*object.String, len(args))
	empty := true
	for i, a := range args {
		s := vm.asWriteString(a)
		strs[i] = s
		if len(s.Bytes()) != 0 {
			empty = false
		}
	}
	if empty {
		return 0
	}
	ioCheckOpen(o)
	n := 0
	for _, s := range strs {
		n += o.writeBytes(vm.ioWriteEncode(o, s))
	}
	return int64(n)
}

// asWriteString coerces a value to the String IO#write should write, following
// rb_obj_as_string: a String is returned unchanged (never re-coerced, so a
// frozen argument stays untouched); anything else is sent #to_s, with a
// non-String result falling back to the object's default string form.
func (vm *VM) asWriteString(v object.Value) *object.String {
	if s, ok := v.(*object.String); ok {
		return s
	}
	if s, ok := vm.send(v, "to_s", nil, nil).(*object.String); ok {
		return s
	}
	return object.NewString(v.ToS())
}

// ioWriteEncode returns the bytes a write of s should place on the stream,
// applying MRI's write conversion (io.c do_writeconv / make_writeconv): when the
// stream has an explicit external encoding that is not BINARY, each written
// String is transcoded from its own encoding to that external encoding
// (rb_str_encode), so unrepresentable characters raise
// Encoding::UndefinedConversionError / InvalidByteSequenceError. A stream with
// no explicit external encoding, a BINARY one, or a StringIO (whose bytes stay
// in the backing String's encoding) writes the argument's bytes unchanged. The
// argument String is never mutated — the conversion produces a new String.
func (vm *VM) ioWriteEncode(o *IOObj, s *object.String) []byte {
	ext := o.extEnc
	if ext == "" || ext == "ASCII-8BIT" || ext == "BINARY" || ioIsStringIO(o) {
		return s.Bytes()
	}
	if s.EncName() == ext {
		return s.Bytes()
	}
	return vm.stringEncode(s, []object.Value{object.NewString(ext)}).Bytes()
}

// defIOWrite defines the writing half of the IO protocol on cls (shared by IO
// and StringIO).
func defIOWrite(cls *RClass) {
	cls.define("write", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.IntValue(vm.ioWriteAll(self.(*IOObj), args))
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
		// rb_io_printf: the format is coerced with StringValue (#to_str).
		o.writeStr(vm.formatString(string(vm.strToStr(args[0])), args[1:]))
		return object.NilV
	})
	cls.define("putc", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckOpen(o)
		// rb_io_putc: a String yields its first character; anything else is coerced
		// with NUM2CHR (#to_int, then the low byte), so nil/false/true raise
		// TypeError. The single-character String is written through #write (so a
		// subclass' or mock's #write observes it), and the original argument returns.
		var ch []byte
		if s, ok := args[0].(*object.String); ok {
			if b := s.Bytes(); len(b) > 0 {
				_, sz := utf8.DecodeRune(b)
				ch = b[:sz]
			}
		} else {
			ch = []byte{byte(vm.toIntCoerce(args[0]))}
		}
		vm.send(self, "write", []object.Value{object.NewStringBytes(ch)}, nil)
		return args[0]
	})
	cls.define("flush", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioClosedRealIO(o) // MRI: IO#flush raises on a closed stream; StringIO#flush does not
		ioFlush(o)
		return self
	})
	cls.define("fsync", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		ioClosedRealIO(self.(*IOObj)) // as flush: real IO raises on close, StringIO does not
		return object.IntValue(0)
	})
	// IO#advise(advice, offset = 0, len = 0) (rb_io_advise + advice_arg_check in
	// io.c). rbgo has no posix_fadvise, so like MRI on a platform without it the
	// call is a validated no-op returning nil. Argument checks, in MRI's order:
	// a non-Symbol advice raises TypeError; an unrecognized one NotImplementedError
	// "Unsupported advice: :sym"; a closed stream IOError; then offset and len are
	// coerced as off_t (a non-Integer raises TypeError, a too-large Bignum
	// RangeError).
	cls.define("advise", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 || len(args) > 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..3)", len(args))
		}
		sym, ok := args[0].(object.Symbol)
		if !ok {
			raise("TypeError", "advice must be a Symbol")
		}
		switch string(sym) {
		case "normal", "sequential", "random", "willneed", "dontneed", "noreuse":
			// recognized advice types (io.c io_advise_sym_to_const)
		default:
			raise("NotImplementedError", "Unsupported advice: %s", args[0].Inspect())
		}
		ioClosedRealIO(self.(*IOObj))
		if len(args) > 1 && !object.IsNil(args[1]) {
			vm.ioOfftArg(args[1]) // offset: coerced for its TypeError/RangeError checks
		}
		if len(args) > 2 && !object.IsNil(args[2]) {
			vm.ioOfftArg(args[2]) // len: likewise
		}
		return object.NilV
	})
	// These sync/sync= entries serve a real IO; StringIO overrides both in
	// defStringIOExtra (its #sync is always true), so the closed-stream guard here
	// only ever fires for a real IO/File, which MRI makes raise IOError.
	cls.define("sync", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioClosedRealIO(o)
		return object.Bool(o.sync)
	})
	cls.define("sync=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioClosedRealIO(o)
		o.sync = args[0].Truthy()
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
	cls.define("tty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		ioClosedRealIO(self.(*IOObj)) // real IO raises on a closed stream; StringIO does not
		return object.Bool(false)
	})
	// #isatty is a true alias of #tty?, as in MRI.
	cls.methods["isatty"] = cls.methods["tty?"]
	// IO#binmode marks the stream binary (#binmode? true) and, as MRI's
	// rb_io_ascii8bit_binmode does, sets the external encoding to ASCII-8BIT and
	// clears the internal encoding.
	cls.define("binmode", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed {
			raise("IOError", "closed stream")
		}
		o.binmode = true
		o.extEnc, o.intEnc = "ASCII-8BIT", ""
		return self
	})

	// external_encoding: the stream's external encoding — the one set explicitly
	// (at creation or via #set_encoding), else Encoding.default_external.
	cls.define("external_encoding", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.extEnc != "" {
			if e, ok := vm.findEncoding(o.extEnc); ok {
				return e
			}
		}
		// rb_io_external_encoding: with no converter and no encoding recorded, a
		// WRITABLE stream reports nil, while a readable one falls back to
		// Encoding.default_external (io_read_encoding). Once #set_encoding has
		// resolved the pair (encSet) the empty name IS the answer; otherwise the
		// open-time resolution is reproduced lazily, and a default internal
		// encoding is what would have forced a converter into place.
		if ioFmodeWritable(o) && (o.encSet || object.IsNil(vm.send(vm.cEncoding, "default_internal", nil, nil))) {
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
		// A trailing Hash carries econv options (invalid:/replace:/…), not an
		// encoding, so it is separated before the 1..2 arity is checked
		// (rb_scan_args "11:").
		pos, _ := splitIOOpts(args)
		if len(pos) < 1 || len(pos) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(pos))
		}
		o.extEnc, o.intEnc = "", ""
		if !object.IsNil(pos[0]) {
			// A single String — or a non-Encoding coerced via #to_str
			// (rb_check_string_type) — of the form "ext:int" names both encodings;
			// otherwise the first argument is one external encoding.
			s, isStr := pos[0].(*object.String)
			if !isStr {
				if _, isEnc := pos[0].(*encodingObj); !isEnc && vm.respondsToDynamic(pos[0], "to_str") {
					s, isStr = vm.send(pos[0], "to_str", nil, nil).(*object.String)
				}
			}
			if isStr {
				if i := strings.IndexByte(s.Str(), ':'); i >= 0 && len(pos) == 1 {
					o.extEnc = vm.lookupEncodingName(s.Str()[:i]).name
					o.intEnc = vm.lookupEncodingName(s.Str()[i+1:]).name
				} else {
					o.extEnc = vm.lookupEncodingName(s.Str()).name
				}
			} else {
				o.extEnc = vm.encodingArg(pos[0]).name
			}
		}
		if len(pos) > 1 && !object.IsNil(pos[1]) {
			o.intEnc = vm.encodingArg(pos[1]).name
		}
		// io_encoding_set: an internal encoding equal to the external one means no
		// transcoding, so the internal encoding is dropped (enc2 = NULL).
		if o.intEnc == o.extEnc {
			o.intEnc = ""
		}
		if object.IsNil(pos[0]) {
			o.extEnc, o.intEnc = vm.extIntToEnc(o.intEnc)
		}
		o.encSet = true
		return self
	})
}

// extIntToEnc resolves a #set_encoding whose external argument was nil, which
// io.c io_encoding_set passes to rb_io_ext_int_to_enc with a NULL external: the
// external becomes Encoding.default_external (default_ext), and an internal that
// was not named becomes Encoding.default_internal. When there is no internal left
// — or it equals the external — no converter is needed, and the recorded encoding
// is NULL precisely when the external was defaulted and differs from the
// internal; otherwise the pair is kept and drives the converter.
//
// The consequence the specs pin down is that the answer is FROZEN here: a stream
// reset with `set_encoding nil, nil` while the defaults are UTF-8 / nil reports
// nil for both afterwards even once the defaults change, whereas one reset while
// they are IBM437 / IBM866 reports that pair.
func (vm *VM) extIntToEnc(named string) (ext, intn string) {
	if vm.defExternalEnc != nil {
		ext = vm.defExternalEnc.name
	}
	intn = named
	if intn == "" && ext != "ASCII-8BIT" && vm.defInternalEnc != nil {
		intn = vm.defInternalEnc.name
	}
	if intn == "" || intn == ext {
		if intn != ext { // a defaulted external with no internal records nothing
			return "", ""
		}
		return ext, ""
	}
	return ext, intn
}

// ioFmodeWritable reports MRI's FMODE_WRITABLE for a stream: a file opened for
// writing, a pipe write end, or one of the writer-backed standard streams
// ($stdout / $stderr, whose bytes go straight to an io.Writer). It is the
// predicate rb_io_external_encoding branches on, and is deliberately not
// o.wrClosed: a pipe READ end leaves that false while being read-only.
func ioFmodeWritable(o *IOObj) bool { return o.writable || o.w != nil }

// ioReadEnc returns the (external, internal) encoding names in effect for a
// whole-stream read, filling the unset sides from Encoding.default_external and
// Encoding.default_internal. A BINARY external encoding — or an internal equal
// to the external — suppresses transcoding (io.c rb_io_ext_int_to_enc leaves the
// second converter NULL), so the returned internal name is "".
func (vm *VM) ioReadEnc(o *IOObj) (ext, intn string) {
	ext = o.extEnc
	if ext == "" && vm.defExternalEnc != nil {
		ext = vm.defExternalEnc.name
	}
	intn = o.intEnc
	if intn == "" && vm.defInternalEnc != nil {
		intn = vm.defInternalEnc.name
	}
	if ext == "ASCII-8BIT" || intn == ext {
		intn = ""
	}
	return ext, intn
}

// stripBOMPrefix removes a leading "BOM|" marker (case-insensitive) from an
// encoding name. MRI's rb_io_extract_encoding_option treats "BOM|UTF-8" as the
// UTF-8 encoding plus a byte-order-mark flag; for name resolution only the
// encoding part matters (io.c parse_mode_enc / rb_econv_prepare_options).
func stripBOMPrefix(name string) string {
	if len(name) >= 4 && strings.EqualFold(name[:4], "bom|") {
		return name[4:]
	}
	return name
}

// encPairFromArgs resolves an (external, internal) encoding-name pair from the
// positional encoding arguments accepted by IO.pipe (and IO.new's ext/int form):
// the first positional may be an Encoding, an encoding name, a combined
// "ext:int" name, or an object answering #to_str; the second, when present,
// names the internal encoding. A leading "BOM|" marker on a name is stripped.
// Unset sides come back as "". io.c: rb_io_extract_modeenc / io_extract_encoding.
func (vm *VM) encPairFromArgs(pos []object.Value) (ext, intn string) {
	if len(pos) >= 1 && !object.IsNil(pos[0]) {
		s, isStr := pos[0].(*object.String)
		if !isStr {
			if _, isEnc := pos[0].(*encodingObj); !isEnc && vm.respondsToDynamic(pos[0], "to_str") {
				s, isStr = vm.send(pos[0], "to_str", nil, nil).(*object.String)
			}
		}
		if isStr {
			name := stripBOMPrefix(s.Str())
			// "ext:int" names both sides, but only when a single positional was
			// given — a separate internal argument takes precedence.
			if i := strings.IndexByte(name, ':'); i >= 0 && (len(pos) < 2 || object.IsNil(pos[1])) {
				ext = vm.lookupEncodingName(name[:i]).name
				intn = vm.lookupEncodingName(name[i+1:]).name
			} else {
				ext = vm.lookupEncodingName(name).name
			}
		} else {
			ext = vm.encodingArg(pos[0]).name
		}
	}
	if len(pos) >= 2 && !object.IsNil(pos[1]) {
		intn = vm.encodingArg(pos[1]).name
	}
	return ext, intn
}

// ioDecodeRead builds the String a whole-stream read (or a gets-family line)
// returns from raw external bytes: when an internal encoding is in effect the
// bytes are transcoded external→internal and tagged with it; otherwise they are
// tagged with the external encoding unchanged (io.c io_enc_str).
func (vm *VM) ioDecodeRead(o *IOObj, data []byte) *object.String {
	ext, intn := vm.ioReadEnc(o)
	b := append([]byte(nil), data...)
	if intn == "" {
		return object.NewStringBytesEnc(b, ext)
	}
	// Pure-ASCII content is byte-identical across ASCII-compatible encodings, so it
	// is simply retagged with the internal encoding — no converter needed (this is
	// how MRI transcodes e.g. IBM866→UTF-8 for an ASCII line).
	if asciiOnly(b) && vm.encAsciiCompat(ext) && vm.encAsciiCompat(intn) {
		return object.NewStringBytesEnc(b, intn)
	}
	src := object.NewStringBytesEnc(b, ext)
	return vm.stringEncode(src, []object.Value{object.NewString(intn)})
}

// defStringIORead defines the reading half of the protocol, plus the cursor and
// content methods, on StringIO.
func defStringIORead(cls *RClass) {
	cls.define("string", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.strNil { // backing string dropped (StringIO.open after its block)
			return object.NilV
		}
		if o.strObj != nil { // return the backing object itself, kept in sync
			o.syncStr()
			return o.strObj
		}
		if o.cls != nil && o.cls.name == "StringIO" { // bare-allocated, not yet initialised
			raise("IOError", "uninitialized stream")
		}
		return object.NewString(string(o.buf))
	})
	cls.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(self.(*IOObj).buf)))
	})
	// #size is a true alias of #length (identical UnboundMethod), as in MRI.
	cls.methods["size"] = cls.methods["length"]
	cls.define("eof?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioCheckReadable(o) // MRI: eof? raises on a closed or non-readable stream
		o.pipeRefresh()
		return object.Bool(o.pos >= len(o.buf))
	})
	// #eof is a true alias of #eof?, as in MRI.
	cls.methods["eof"] = cls.methods["eof?"]
	cls.define("pos", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		// A real IO/File raises on a closed stream; StringIO#pos tolerates it (MRI).
		if o.closed && !ioIsStringIO(o) {
			raise("IOError", "closed stream")
		}
		return object.IntValue(int64(o.pos))
	})
	// #tell is a true alias of #pos, as in MRI.
	cls.methods["tell"] = cls.methods["pos"]
	cls.define("pos=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed && !ioIsStringIO(o) { // real IO/File raises; StringIO tolerates it
			raise("IOError", "closed stream")
		}
		n := vm.ioOfftArg(args[0])
		if n < 0 {
			raise("Errno::EINVAL", "Invalid argument")
		}
		o.pos = n
		return args[0]
	})
	cls.define("seek", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.closed {
			raise("IOError", "closed stream")
		}
		amount := vm.ioOfftArg(args[0])
		whence := 0
		if len(args) > 1 {
			// IO#seek accepts the :SET/:CUR/:END whence symbols; StringIO#seek
			// (strio_seek) does not — a Symbol there raises the Integer TypeError.
			if ioIsStringIO(o) {
				whence = int(vm.toIntCoerce(args[1]))
			} else {
				whence = vm.seekWhence(args[1])
			}
		}
		var newPos int
		switch whence {
		case 1: // SEEK_CUR
			newPos = o.pos + amount
		case 2: // SEEK_END
			newPos = len(o.buf) + amount
		case 0: // SEEK_SET
			newPos = amount
		default:
			raise("Errno::EINVAL", "Invalid argument")
		}
		if newPos < 0 {
			raise("Errno::EINVAL", "Invalid argument")
		}
		o.pos = newPos
		return object.IntValue(0)
	})
	cls.define("rewind", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		ioClosedRealIO(o)      // real IO/File raises on a closed stream; StringIO tolerates it
		o.pos, o.lineno = 0, 0 // #rewind resets both the position and the line number
		return object.IntValue(0)
	})
	cls.define("truncate", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if o.wrClosed { // a read-only stream cannot be truncated
			raise("IOError", "not opened for writing")
		}
		n := int(vm.toIntCoerce(args[0]))
		if n < 0 {
			raise("Errno::EINVAL", "Invalid argument")
		}
		if n < len(o.buf) {
			o.buf = o.buf[:n]
		} else if n > len(o.buf) {
			o.buf = append(o.buf, make([]byte, n-len(o.buf))...)
		}
		o.syncStr()
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
					buf.SetBytes(nil) // read at EOF clears the buffer, leaving its encoding
					return object.NilV
				}
				if lengthGiven {
					// read(size, buf): the bytes are binary and the buffer's own
					// encoding is left unchanged (io.c io_read: no transcoding path).
					buf.SetBytes(append([]byte(nil), data...))
					return buf
				}
				dec := vm.ioDecodeRead(o, data)
				buf.SetBytes(append([]byte(nil), dec.Bytes()...))
				buf.Enc = dec.Enc // a full read retags the buffer with the read encoding
				return buf
			}
			if isNil {
				return object.NilV
			}
			// read(length) returns a binary (ASCII-8BIT) String; read with no
			// length returns the remainder in the stream's read encoding, transcoded
			// external→internal when an internal encoding is in effect.
			if lengthGiven {
				return object.NewStringBytesEnc(append([]byte(nil), data...), "ASCII-8BIT")
			}
			return vm.ioDecodeRead(o, data)
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
	// leave $_ alone, matching MRI. Every caller guards end-of-stream (a nil line)
	// itself, so v is always a real line here.
	setLineGlobals := func(vm *VM, o *IOObj, v object.Value, lastLine bool) {
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
	// #each_codepoint (rb_io_each_codepoint, io.c) yields each character's integer
	// codepoint from the current position; MRI raises ArgumentError on an invalid /
	// incomplete byte sequence and returns an Enumerator (size nil) with no block.
	// It lives here on the shared read protocol so File/IO carry it, not only
	// StringIO.
	cls.define("each_codepoint", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		o := self.(*IOObj)
		if blk == nil { // no block ⇒ an Enumerator (buildable even on a closed stream)
			return enumForSized(self, "each_codepoint", enumSizeNil)
		}
		ioCheckReadable(o) // iterating an unreadable stream raises, as in MRI
		for o.pos < len(o.buf) {
			r, sz := utf8.DecodeRune(o.buf[o.pos:])
			if r == utf8.RuneError && sz <= 1 { // a lone continuation / invalid byte
				raise("ArgumentError", "invalid byte sequence in UTF-8")
			}
			o.pos += sz
			vm.callBlock(blk, []object.Value{object.IntValue(int64(r))})
		}
		return self
	})
	// #each is a true alias of #each_line — it must share the method entry so
	// IO.instance_method(:each) == IO.instance_method(:each_line), as in MRI.
	cls.methods["each"] = cls.methods["each_line"]
}

// seekWhence resolves a seek / sysseek whence argument to a SEEK_* constant,
// mirroring MRI's interpret_seek_whence (io.c): the symbols :SET/:CUR/:END and
// :DATA/:HOLE map to their constants, and anything else is coerced through
// #to_int (a Symbol that is not one of those raises the Integer TypeError).
func (vm *VM) seekWhence(v object.Value) int {
	if s, ok := v.(object.Symbol); ok {
		switch string(s) {
		case "SET":
			return 0
		case "CUR":
			return 1
		case "END":
			return 2
		case "DATA":
			return 3
		case "HOLE":
			return 4
		}
		raise("TypeError", "no implicit conversion of Symbol into Integer")
	}
	return int(vm.toIntCoerce(v))
}

// ioIsStringIO reports whether o is a StringIO (or a subclass), which — unlike a
// real IO/File — tolerates #pos / #pos= on a closed stream rather than raising.
func ioIsStringIO(o *IOObj) bool {
	for c := o.cls; c != nil; c = c.super {
		if c.name == "StringIO" {
			return true
		}
	}
	return false
}

// defStringIOExtra installs the StringIO-only surface that MRI adds on top of the
// shared read/write protocol: the allocate/new/open class methods, the private
// #initialize and #reopen instance methods (which share the backing-string setup
// in stringIOSetup), #string=, and the StringIO-specific overrides of
// #sync/#binmode/#close_read/#close_write/#fcntl whose behaviour differs from a
// real IO's.
// includeStringIOEnumerable mixes Enumerable into IO and StringIO (whose #each
// yields lines), as in MRI. It runs after the prelude, where Enumerable is
// defined.
func (vm *VM) includeStringIOEnumerable() {
	if en, ok := vm.consts["Enumerable"].(*RClass); ok {
		for _, name := range []string{"IO", "StringIO"} {
			if c, ok := vm.consts[name].(*RClass); ok {
				c.includes = append(c.includes, en)
			}
		}
	}
}

func defStringIOExtra(vm *VM, cls *RClass) {
	// StringIO::VERSION — the bundled stringio gem version (specs guard behaviour on
	// it via version_is / guard blocks). Matches MRI 4.0.6's stringio 3.2.0.
	cls.consts["VERSION"] = object.NewFrozenStringView("3.2.0")
	// StringIO.allocate returns a bare, uninitialised StringIO (an IOObj rather
	// than the generic RObject) so a following #initialize / #reopen can populate
	// it — the StringIO.allocate; io.send(:initialize, ...) construction MRI allows.
	cls.smethods["allocate"] = &Method{name: "allocate", owner: cls, native: func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &IOObj{cls: self.(*RClass), isStr: true}
	}}
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := &IOObj{cls: self.(*RClass), isStr: true}
		stringIOSetup(vm, o, args)
		return o
	}}
	// StringIO.open(...) { |io| ... } opens like .new and, when a block is given,
	// yields self, then closes it AND drops its backing string (#string ⇒ nil)
	// afterwards — even if the block raises — returning the block's value. Without
	// a block it behaves exactly like .new.
	cls.smethods["open"] = &Method{name: "open", owner: cls, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		o := &IOObj{cls: self.(*RClass), isStr: true}
		stringIOSetup(vm, o, args)
		if blk == nil {
			return o
		}
		defer func() { o.closed, o.strNil, o.strObj = true, true, nil }()
		return vm.callBlock(blk, []object.Value{o})
	}}
	cls.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		stringIOSetup(vm, self.(*IOObj), args)
		return self
	})
	vm.setInstanceVisibility(cls, "initialize", visPrivate) // MRI keeps #initialize private
	cls.define("reopen", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		// A single non-String argument is (converted to) a StringIO whose backing
		// string and mode self adopts — MRI tries #to_strio here, never #to_str.
		if len(args) == 1 && !object.IsNil(args[0]) {
			if _, isStr := args[0].(*object.String); !isStr {
				src, ok := stringIOToStrIO(vm, args[0])
				if !ok {
					raise("TypeError", "no implicit conversion of %s into StringIO", classNameOf(args[0]))
				}
				o.buf, o.strObj, o.strNil = src.buf, src.strObj, false
				o.pos, o.lineno, o.appendMode = 0, 0, src.appendMode
				o.rdClosed, o.wrClosed, o.closed = src.rdClosed, src.wrClosed, false
				o.rdModeOff, o.wrModeOff = src.rdModeOff, src.wrModeOff
				return o
			}
		}
		stringIOSetup(vm, o, args)
		return o
	})
	// #string= replaces the backing string (coercing via #to_str), resetting the
	// position and line number, and returns the assigned string.
	cls.define("string=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		s := stringIOStrArg(vm, args[0])
		o.strObj, o.buf, o.strNil = s, s.MutableBytes(), false
		o.pos, o.lineno = 0, 0
		return s
	})
	// StringIO#sync is always true and cannot be turned off, unlike a real IO's.
	cls.define("sync", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(true)
	})
	cls.define("sync=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return args[0]
	})
	// StringIO#binmode sets the external encoding to ASCII-8BIT (BINARY), clears
	// any internal encoding, and returns self.
	cls.define("binmode", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		o.binmode = true
		o.extEnc, o.intEnc = "ASCII-8BIT", ""
		return self
	})
	// StringIO#close_read / #close_write (the mode-off IOError guard) live in the
	// shared defIOReadExtra, keyed on rdModeOff/wrModeOff which only StringIO sets.
	// StringIO#fcntl is unsupported, as in MRI.
	cls.define("fcntl", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		raise("NotImplementedError", "fcntl() function is unimplemented on this machine")
		return object.NilV
	})
}

// stringIOSetup populates (or resets, for #reopen / #initialize on an existing
// object) a StringIO IOObj from its (string, mode, options) arguments, matching
// MRI's StringIO#initialize: the passed String becomes the live backing object,
// the mode (a String, an Integer flag set, or a :mode option) fixes the read /
// write / append / truncate capabilities, and a frozen backing string raises
// Errno::EACCES for a writable mode or FrozenError for a truncating one.
func stringIOSetup(vm *VM, o *IOObj, args []object.Value) {
	o.isStr = true
	o.pos, o.lineno = 0, 0
	o.closed, o.strNil, o.appendMode = false, false, false
	o.extEnc, o.intEnc = "", ""

	var modeVal object.Value // Integer flag set or String access mode, once resolved
	binGiven, binVal, textGiven, encGiven := false, false, false, false
	if h, ok := lastHash(args); ok {
		args = args[:len(args)-1]
		if v, ok := h.Get(object.Symbol("mode")); ok && !object.IsNil(v) {
			modeVal = stringIOModeVal(vm, v)
		}
		if v, ok := h.Get(object.Symbol("binmode")); ok {
			binGiven, binVal = true, v.Truthy()
		}
		if _, ok := h.Get(object.Symbol("textmode")); ok {
			textGiven = true
		}
		for _, k := range []string{"encoding", "external_encoding", "internal_encoding"} {
			if _, ok := h.Get(object.Symbol(k)); ok {
				encGiven = true
			}
		}
	}
	// A second positional argument is the mode, overriding a :mode option.
	if len(args) > 1 && !object.IsNil(args[1]) {
		modeVal = stringIOModeVal(vm, args[1])
	}
	// The ":"/"b"/"t" flags only exist on a String mode; an Integer flag set never
	// carries them, so these checks and the binary detection apply to a String mode.
	modeStr, _ := modeVal.(*object.String)
	modeBase := ""
	if modeStr != nil {
		modeBase = modeStr.Str()
		if i := strings.IndexByte(modeBase, ':'); i >= 0 {
			modeBase = modeBase[:i]
		}
	}
	modeHasBT := strings.ContainsAny(modeBase, "bt")
	// Reject the ways MRI forbids specifying an encoding or the binary/text choice
	// twice: an encoding both in the mode string and as an option; binmode /
	// textmode both as a b/t mode flag and as an option, or as two options at once.
	if encGiven && modeStr != nil && strings.Contains(modeStr.Str(), ":") {
		raise("ArgumentError", "encoding specified twice")
	}
	if (binGiven || textGiven) && modeHasBT {
		raise("ArgumentError", "mode specified twice")
	}
	if binGiven && textGiven {
		raise("ArgumentError", "both textmode and binmode specified")
	}

	var s *object.String
	if len(args) > 0 && !object.IsNil(args[0]) {
		s = stringIOStrArg(vm, args[0])
	}
	read, write, trunc, appnd := true, true, false, false
	switch {
	case modeVal == nil:
		// With no explicit mode, a frozen backing string opens read-only; a mutable
		// one (or none) is read/write, matching MRI.
		if s != nil && s.Frozen {
			write = false
		}
	case modeStr != nil:
		read, write, trunc, appnd = stringIOModeFlags(modeStr.Str())
	default: // Integer flag set (IO::RDONLY | IO::TRUNC | ...)
		read, write, trunc, appnd = stringIOFlagBits(int64(modeVal.(object.Integer)))
	}
	if s != nil && s.Frozen {
		if write {
			raise("Errno::EACCES", "Permission denied")
		}
		if trunc {
			raise("FrozenError", "can't modify frozen String: %s", s.Inspect())
		}
	}
	if s == nil {
		s = object.NewString("")
	} else if trunc {
		s.SetBytes(nil) // truncate the backing string in place, keeping its identity/encoding
	}
	o.strObj = s
	o.buf = s.MutableBytes()
	if strings.Contains(modeBase, "b") || (binGiven && binVal) {
		o.extEnc = "ASCII-8BIT"
	}
	o.rdClosed, o.wrClosed = !read, !write
	o.rdModeOff, o.wrModeOff = !read, !write
	o.appendMode = appnd
}

// stringIOStrArg coerces a StringIO backing-string argument to a String object,
// returning the very object passed (so #string preserves identity) or the result
// of #to_str, and raising TypeError when neither applies.
func stringIOStrArg(vm *VM, v object.Value) *object.String {
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

// stringIOToStrIO resolves a #reopen source that must be a StringIO: the object
// itself when it is one, or the result of #to_strio. It never falls back to
// #to_str — MRI's single-argument #reopen does not try a String conversion.
func stringIOToStrIO(vm *VM, v object.Value) (*IOObj, bool) {
	if o, ok := v.(*IOObj); ok && o.isStr {
		return o, true
	}
	if vm.respondsToDynamic(v, "to_strio") {
		if o, ok := vm.send(v, "to_strio", nil, nil).(*IOObj); ok {
			return o, true
		}
	}
	return nil, false
}

// ioGets reads one line from o using MRI's (sep, limit, chomp:) argument shapes,
// returning nil at end of input and advancing #lineno on a successful read. It is
// the single-shot entry used by ARGF#gets and the IRB reader; the coercion and
// $/ default live in resolveGetsArgs, shared with the StringIO/IO gets family.
func (vm *VM) ioGets(o *IOObj, args []object.Value) object.Value {
	sep, limit, chomp := vm.resolveGetsArgs(args)
	return vm.ioGetsResolved(o, sep, limit, chomp)
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
	v := vm.ioGetsLine(o, sep, limit, chomp)
	if v != object.NilV {
		o.lineno++
		// A line is read in the external encoding (the separator is matched on those
		// bytes) then transcoded to the internal encoding and tagged, exactly like a
		// whole-stream read (io.c rb_io_getline_1 → io_enc_str).
		if s, ok := v.(*object.String); ok {
			v = vm.ioDecodeRead(o, s.Bytes())
		}
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
	// gets/readline/each_line take at most a separator and a limit (chomp: is a
	// keyword, stripped above); more positional arguments raise ArgumentError.
	if len(args) > 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 0..2)", len(args))
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
				// The limit is a C off_t, so a Bignum too large raises RangeError
				// rather than TypeError (io.c rb_io_getline_1 / NUM2OFFT).
				limit = vm.ioOfftArg(args[0])
			}
		}
	}
	if len(args) > 1 && !object.IsNil(args[1]) {
		limit = vm.ioOfftArg(args[1])
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

// getsSep is a resolved line separator for ioGetsLine.
type getsSep struct {
	s      string
	set    bool // an explicit separator was given (else the "\n" default)
	nilSep bool // an explicit nil separator: read the whole remainder as one line
}

// ioGetsLine reads one line honouring the separator, an optional byte limit
// (negative for none) and chomp, advancing the cursor. It returns nil at EOF.
func (vm *VM) ioGetsLine(o *IOObj, sep getsSep, limit int, chomp bool) object.Value {
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
		ext, _ := vm.ioReadEnc(o)
		end = vm.relaxGetsLimit(o.buf, o.pos, o.pos+limit, end, ext)
	}
	line := o.buf[o.pos:end]
	o.pos = end
	if chomp && !sep.nilSep {
		line = getsChomp(line, sep.s)
	}
	return object.NewString(string(line))
}

// relaxGetsLimit returns the end offset a byte-limited gets should actually stop
// at. io.c rb_io_getline_0 does not cut a line in the middle of a character: when
// the limit is exhausted and the trailing character is still incomplete
// (MBCLEN_NEEDMORE_P on the last character), it relaxes the limit by one byte and
// reads again, up to an extra_limit of 16 bytes. The trailing character is
// re-anchored on every pass (rb_enc_prev_char), which is what makes a run of
// truncated leads consume the full 16 rather than stopping at the first invalid
// sequence. hardEnd caps the relaxation at the separator (or end of stream), as
// appendline's newline check does.
func (vm *VM) relaxGetsLimit(buf []byte, start, end, hardEnd int, enc string) int {
	switch enc {
	case "", "ASCII-8BIT", "US-ASCII", "ISO-8859-1":
		return end // a single-byte encoding has no character to split
	}
	anchor := start
	for extra := 16; extra > 0 && end < hardEnd && anchor < end; extra-- {
		anchor += lastCharStart(vm, buf[anchor:end], enc)
		if _, _, _, st := vm.decodeCharFrom(buf[anchor:end], enc); st != stepIncomplete {
			return end
		}
		end++
	}
	return end
}

// lastCharStart returns the offset within s at which its final character begins,
// walking the characters forward from the start of the slice (MRI's
// rb_enc_prev_char scans backwards from the end, but only ever to find the same
// boundary). An invalid sequence is stepped over by its maximal valid subpart, so
// a run of truncated leads re-anchors on the last of them.
func lastCharStart(vm *VM, s []byte, enc string) int {
	last := 0
	for i := 0; i < len(s); {
		last = i
		_, n, readLen, st := vm.decodeCharFrom(s[i:], enc)
		if st == stepIncomplete {
			return last
		}
		if n <= 0 {
			n = max(readLen, 1)
		}
		i += n
	}
	return last
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

// ioClosedRealIO raises IOError "closed stream" when a real IO/File is closed. It
// is a no-op for a StringIO, whose #flush/#fsync/#tty?/#sync tolerate a closed
// stream where a real IO raises (MRI).
func ioClosedRealIO(o *IOObj) {
	if o.closed && !ioIsStringIO(o) {
		raise("IOError", "closed stream")
	}
}

// ioOfftArg coerces a stream position/offset argument the way MRI's NUM2OFFT
// does: an Integer or #to_int value narrowed to the off_t (a long long) the
// position is stored in, raising RangeError for a Bignum too large to fit.
func (vm *VM) ioOfftArg(v object.Value) int {
	if _, ok := v.(*object.Bignum); ok {
		raise("RangeError", "bignum too big to convert into 'long long'")
	}
	return int(vm.toIntCoerce(v))
}

// ioCIntArg coerces an argument the way MRI's NUM2INT does for a value stored in
// a C int (e.g. #lineno=): an Integer or #to_int value that fits a 32-bit int,
// raising RangeError for a Bignum or for an in-range-Integer that still overflows
// the int.
func (vm *VM) ioCIntArg(v object.Value) int {
	if _, ok := v.(*object.Bignum); ok {
		raise("RangeError", "bignum too big to convert into 'long'")
	}
	n := vm.toIntCoerce(v)
	if n < -2147483648 || n > 2147483647 { // int32 bounds
		raise("RangeError", "integer %d too big to convert to 'int'", n)
	}
	return int(n)
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
