// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// argfObj backs the ARGF stream ($< / ARGF): a virtual concatenation of the
// files named in ARGV (falling back to $stdin when ARGV is empty), read as one
// continuous stream. It consumes ARGV — each filename is shifted off as it is
// opened — and tracks a cumulative line number ($.).
//
// The two integers are MRI's, not a convenience: io.c keeps ARGF's whole
// file-switching state in `init_p` and `next_p` (see argf_next_argv), and every
// ARGF method is written against them. They are reproduced here because the
// distinction they carry is OBSERVABLE and cannot be recovered from
// "cur is exhausted":
//
//   - a file that has been read to its end is still ARGF's current file — #to_io,
//     #file, #filename and #fileno keep naming it — until a reader explicitly asks
//     for the next one. Looking ahead (advancing as soon as the buffer runs out)
//     answers #eof? about the whole concatenation where MRI answers about one file.
//   - a file that has been CLOSED (init_p == -1) is still the current file too,
//     which is the only reason #closed? has anything to return and why #eof? can
//     raise IOError while #fileno raises ArgumentError on the same state.
type argfObj struct {
	vm  *VM
	cur *IOObj // ARGF.current_file: the stream last selected, open or closed
	// initP mirrors MRI's ARGF.init_p: 0 before any stream has been selected,
	// 1 once one has, -1 when the selected one has been closed (argf_close).
	initP int
	// nextP mirrors MRI's ARGF.next_p: 1 = open the next filename before reading
	// again, 0 = cur is the live stream, -1 = there were no filenames at all, so
	// cur is $stdin and stays $stdin.
	nextP    int
	lineno   int           // cumulative line count across all files ($.)
	curName  string        // the current file's name ("-" for $stdin)
	files    *object.Array // an ARGF.class.new(*files) instance's own filename list
	fromARGV bool          // the singleton ARGF: draw filenames from the live ARGV
	binmode  bool          // #binmode has been called — every stream reads as BINARY
}

// argvArray returns the mutable Array ARGF shifts its filenames off. For the
// singleton that is the live ARGV (MRI: ARGF.argv IS $*, so replacing ARGV is
// visible); for an ARGF.class.new instance it is the list it was built with.
func (a *argfObj) argvArray() *object.Array {
	if a.fromARGV {
		arr, _ := a.vm.consts["ARGV"].(*object.Array)
		return arr
	}
	return a.files
}

// pending reports how many filenames are still waiting — MRI's
// RARRAY_LEN(ARGF.argv), which argf_next_argv and argf_getpartial both branch on.
func (a *argfObj) pending() int {
	if arr := a.argvArray(); arr != nil {
		return len(arr.Elems)
	}
	return 0
}

// shiftName removes and returns the next filename, mirroring rb_ary_shift.
func (a *argfObj) shiftName() (string, bool) {
	arr := a.argvArray()
	if arr == nil || len(arr.Elems) == 0 {
		return "", false
	}
	v := arr.Elems[0]
	arr.Elems = arr.Elems[1:]
	if s, ok := v.(*object.String); ok {
		return s.Str(), true
	}
	return a.vm.send(v, "to_s", nil, nil).(*object.String).Str(), true
}

func (a *argfObj) ToS() string     { return "ARGF" }
func (a *argfObj) Inspect() string { return "ARGF" }
func (a *argfObj) Truthy() bool    { return true }

// stdin returns the stream $stdin currently names. MRI reads rb_stdin, which the
// $stdin setter writes, so assigning $stdin redirects ARGF — which is what the
// read_nonblock specs rely on when they point $stdin at a pipe.
func (a *argfObj) stdin() *IOObj {
	o, _ := a.vm.globals["$stdin"].(*IOObj)
	return o
}

// closeCur mirrors io.c argf_close: close the current file and record that it is
// closed (init_p = -1). $stdin is never closed, and the "closed" mark is not set
// for it either, so a stdin-backed ARGF stays readable.
func (a *argfObj) closeCur() {
	if a.cur == nil || a.cur == a.stdin() {
		return
	}
	if !a.cur.closed {
		a.vm.send(a.cur, "close", nil, nil)
	}
	a.initP = -1
}

// applyBinmode reproduces argf_next_argv's `if (ARGF.binmode)
// rb_io_ascii8bit_binmode(ARGF.current_file)`: once ARGF is in binary mode every
// stream it opens — not only the one open when #binmode was called — reads as
// BINARY with no newline or encoding conversion.
func (a *argfObj) applyBinmode(o *IOObj) {
	if o == nil || o.closed {
		return
	}
	o.binmode = true
	o.extEnc, o.intEnc = "ASCII-8BIT", ""
}

// nextArgv mirrors io.c argf_next_argv — the ONE place ARGF changes which file it
// is reading. It reports whether there is a current stream to work with; false
// means every input has been consumed, and the callers differ sharply in what
// they then do (#fileno raises ArgumentError, #eof? goes on to interrogate the
// closed stream, #read returns what it has).
//
// It does not look ahead: a current file that happens to be at EOF is left in
// place. Advancing is the reader's decision, taken by setting nextP = 1 and
// calling again — exactly as argf_getline, argf_read and argf_getpartial do.
func (a *argfObj) nextArgv() bool {
	if a.initP == 0 {
		if a.pending() > 0 {
			a.nextP = 1
		} else {
			a.nextP = -1
		}
		a.initP = 1
	} else if a.nextP == -1 && a.pending() > 0 {
		a.nextP = 1
	}

	switch a.nextP {
	case 1:
		if a.initP == 1 {
			a.closeCur() // the previous file is closed before the next is opened
		}
		name, ok := a.shiftName()
		if !ok {
			a.nextP = 1 // stay armed: a later ARGV push would still be picked up
			return false
		}
		if name == "-" {
			a.cur = a.stdin() // MRI: a lone "-" names rb_stdin, it is not a path
		} else {
			a.cur = openFileIO(a.vm.consts["File"].(*RClass), name, "r")
		}
		a.curName = name
		a.applyBinmodeIfSet()
		a.nextP = 0
	case -1:
		a.cur = a.stdin()
		a.curName = "-"
		a.applyBinmodeIfSet()
	}
	if a.initP == -1 {
		a.initP = 1
	}
	return true
}

func (a *argfObj) applyBinmodeIfSet() {
	if a.binmode {
		a.applyBinmode(a.cur)
	}
}

// readAcross runs the `retry:` loop that argf_getline, argf_getc and argf_getbyte
// share: read from the current file, and when it yields nothing close that file,
// arm the next one and try again. nil means every input is exhausted.
func (a *argfObj) readAcross(read func(o *IOObj) object.Value) object.Value {
	for {
		if !a.nextArgv() || a.cur == nil {
			return object.NilV
		}
		v := read(a.cur)
		if !object.IsNil(v) {
			return v
		}
		if a.nextP == -1 {
			return object.NilV // $stdin is the only input; there is no next file
		}
		a.closeCur()
		a.nextP = 1
	}
}

// bumpLine advances ARGF's line counter and mirrors it into $. as MRI does.
func (a *argfObj) bumpLine() {
	a.lineno++
	a.vm.globals["$."] = object.IntValue(int64(a.lineno))
}

// getline is io.c argf_getline: the next line across the files, advancing $. but
// NOT $_ — only #gets and #readline set the last-read line, while #each_line and
// #readlines leave it alone (argf_gets calls rb_lastline_set, argf_getline does
// not).
func (a *argfObj) getline(vm *VM, args []object.Value) object.Value {
	line := a.readAcross(func(o *IOObj) object.Value { return vm.ioGets(o, args) })
	if !object.IsNil(line) {
		a.bumpLine()
	}
	return line
}

// argfGetPartial mirrors io.c io_getpartial against the in-memory stream: at most
// n bytes of what is already buffered, nil at END OF FILE (io_getpartial returns
// Qnil for n == 0 bytes read rather than raising — the EOFError decision belongs
// to the ARGF layer above), and, for a non-blocking read of a stream that could
// still deliver more, either :wait_readable or IO::EAGAINWaitReadable.
func argfGetPartial(o *IOObj, n int, buf *object.String, noException, nonblock bool) object.Value {
	ioCheckReadable(o)
	if n == 0 {
		return ioReadResult(nil, buf)
	}
	o.pipeRefresh()
	if nonblock {
		o.nonblock = true // rb_io_set_nonblock on the descriptor being read
	}
	avail := len(o.buf) - o.pos
	if avail <= 0 {
		if nonblock && o.pipe != nil && !o.pipeWriterClosed() {
			// The write end is still open, so this is "no data yet", not EOF.
			if noException {
				return object.Symbol("wait_readable")
			}
			raise("IO::EAGAINWaitReadable", "Resource temporarily unavailable - read would block")
		}
		ioReadResult(nil, buf) // io_set_read_length(str, 0): the buffer is emptied
		return object.NilV
	}
	if n > avail {
		n = avail
	}
	data := o.buf[o.pos : o.pos+n]
	o.pos += n
	return ioReadResult(data, buf)
}

// getPartial mirrors io.c argf_getpartial, which backs BOTH #readpartial
// (nonblock false) and #read_nonblock (nonblock true). Its whole subtlety is what
// an end-of-file means on a stream made of several files:
//
//   - bytes available            -> the String (or the output buffer)
//   - EOF, another file waiting  -> "" — the caller is expected to read again
//   - EOF on the last file       -> EOFError, or nil when exception: false
//   - would block (nonblock)     -> IO::EAGAINWaitReadable, or :wait_readable
//
// io_nonblock_eof is the function that turns the last-file EOF into an EOFError
// or a nil, and it is deliberately NOT the same answer as a would-block: with
// exception: false a would-block is :wait_readable and an EOF is nil.
func (a *argfObj) getPartial(vm *VM, args []object.Value, nonblock bool) object.Value {
	pos := args
	noException := false
	if nonblock {
		var opts *object.Hash
		pos, opts = splitIOOpts(args)
		if opts != nil {
			if v, ok := opts.Get(object.Symbol("exception")); ok {
				noException = !v.Truthy()
			}
		}
	}
	if len(pos) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	n := vm.ioOfftArg(pos[0])
	if n < 0 {
		raise("ArgumentError", "negative length %d given", n)
	}
	var buf *object.String
	if len(pos) > 1 && !object.IsNil(pos[1]) {
		buf = vm.ioBufferArg(pos[1])
	}
	// io_nonblock_eof: EOFError unless the caller asked for a value instead.
	nonblockEOF := func() object.Value {
		if noException {
			return object.NilV
		}
		raise("EOFError", "end of file reached")
		return object.NilV
	}
	if !a.nextArgv() || a.cur == nil {
		// rb_eof_error() — unconditional here, even under exception: false, because
		// there is no stream at all rather than a stream that ended.
		if buf != nil {
			buf.SetBytes(nil)
		}
		raise("EOFError", "end of file reached")
	}
	tmp := argfGetPartial(a.cur, n, buf, noException, nonblock)
	if !object.IsNil(tmp) {
		return tmp
	}
	if a.nextP == -1 {
		return nonblockEOF()
	}
	a.closeCur()
	a.nextP = 1
	if a.pending() == 0 {
		return nonblockEOF()
	}
	if buf != nil {
		return buf // already emptied by argfGetPartial
	}
	return object.NewString("")
}

// registerARGF installs the ARGF constant (and the $< / $stdin-style globals)
// with the core reading protocol. It runs after registerIO so File and $stdin
// exist.
func (vm *VM) registerARGF() {
	cls := newClass("ARGF.class", vm.cObject)
	vm.cARGF = cls
	a := &argfObj{vm: vm, fromARGV: true}
	vm.consts["ARGF"] = a
	vm.globals["$<"] = a

	// ARGF.class.new(*filenames): a fresh ARGF reading exactly the given files
	// (used by the spec harness); no arguments reads $stdin.
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		files := object.NewArray()
		for _, v := range args {
			if s, ok := v.(*object.String); ok {
				files.Elems = append(files.Elems, s)
			} else {
				files.Elems = append(files.Elems, vm.send(v, "to_s", nil, nil))
			}
		}
		return &argfObj{vm: vm, files: files}
	}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *argfObj { return v.(*argfObj) }

	// read(length = nil, outbuf = nil): io.c argf_read. The files are read one
	// after another until the requested length is satisfied (or, with no length,
	// until every input is exhausted), and each exhausted file is CLOSED on the
	// way — which is why #eof?/#fileno/#pos behave differently after a bare #read.
	d("read", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a := self(v)
		hasLen := len(args) > 0 && !object.IsNil(args[0])
		length := 0
		if hasLen {
			length = vm.ioOfftArg(args[0])
		}
		var str *object.String
		if len(args) > 1 && !object.IsNil(args[1]) {
			str = vm.ioBufferArg(args[1])
			str.SetBytes(nil) // rb_str_resize(str, 0)
		}
		want := length
		for {
			if !a.nextArgv() || a.cur == nil {
				if str == nil {
					return object.NilV
				}
				return str
			}
			var readArgs []object.Value
			if hasLen {
				readArgs = []object.Value{object.IntValue(int64(want))}
			}
			tmp := vm.send(a.cur, "read", readArgs, nil)
			if str == nil {
				if s, ok := tmp.(*object.String); ok {
					str = object.NewStringBytes(append([]byte(nil), s.Bytes()...))
					str.Enc = s.Enc // the stream's external encoding, not UTF-8
				}
			} else if s, ok := tmp.(*object.String); ok {
				str.SetBytes(append(str.Bytes(), s.Bytes()...))
			}
			if object.IsNil(tmp) || !hasLen {
				if a.nextP != -1 {
					a.closeCur()
					a.nextP = 1
					continue
				}
			} else if str != nil && len(str.Bytes()) < length {
				want = length - len(str.Bytes())
				continue
			}
			if str == nil {
				return object.NilV
			}
			return str
		}
	})

	// gets(sep = $/, ...): io.c argf_gets — argf_getline plus rb_lastline_set, so
	// $_ becomes the line read (and nil once the stream is done).
	d("gets", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		line := self(v).getline(vm, args)
		vm.globals["$_"] = line
		return line
	})

	// readline: io.c argf_readline — like gets but raises EOFError at end of input.
	d("readline", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		line := vm.send(v, "gets", args, nil)
		if object.IsNil(line) {
			raise("EOFError", "end of file reached")
		}
		return line
	})

	// each_line / each { |line| … }: yield every line; returns self (or an
	// Enumerator with no block). MRI's argf_each_line goes through argf_getline,
	// so it does NOT touch $_.
	eachLine := func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		a := self(v)
		if blk == nil {
			return argfEnum(v, "each_line", args...)
		}
		for {
			line := a.getline(vm, args)
			if object.IsNil(line) {
				return v
			}
			vm.callBlock(blk, []object.Value{line})
		}
	}
	d("each_line", eachLine)
	aliasBuiltin(cls, "each", "each_line")

	// readlines / to_a: every remaining line as an Array.
	readlines := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a := self(v)
		out := object.NewArray()
		for {
			line := a.getline(vm, args)
			if object.IsNil(line) {
				return out
			}
			out.Elems = append(out.Elems, line)
		}
	}
	d("readlines", readlines)
	aliasBuiltin(cls, "to_a", "readlines")

	// eof? / eof: io.c argf_eof — the question is about the CURRENT FILE, not the
	// whole concatenation, so it is true at the end of each file in turn. Once the
	// current file has been closed (a bare #read closes it) the closed stream is
	// still the one asked, and IO#eof? raises IOError on it.
	eof := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		a.nextArgv()
		if a.cur == nil {
			return object.Bool(false)
		}
		if a.initP == 0 {
			return object.Bool(true)
		}
		a.nextArgv()
		return object.Bool(vm.send(a.cur, "eof?", nil, nil).Truthy())
	}
	d("eof?", eof)
	aliasBuiltin(cls, "eof", "eof?")

	// lineno / lineno=: the cumulative line number ($.).
	d("lineno", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).lineno))
	})
	d("lineno=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).lineno = int(intArg(args[0]))
		return args[0]
	})

	// filename / path: io.c argf_filename — the name of the file currently being
	// read ("-" for stdin). next_argv selects the first input if reading has not
	// started, and leaves the last name in place once every input is gone.
	filename := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		a.nextArgv()
		return object.NewString(a.curName)
	}
	d("filename", filename)
	aliasBuiltin(cls, "path", "filename")

	// to_io / file: io.c argf_to_io / argf_file — the current file, open or closed.
	// Neither looks ahead, so two reads inside one file see the same IO.
	curFile := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		a.nextArgv()
		if a.cur == nil {
			return object.NilV
		}
		return a.cur
	}
	d("to_io", curFile)
	d("file", curFile)

	// to_s / inspect: io.c argf_to_s, with inspect a true ALIAS of it
	// (rb_define_alias), which is what ARGF.method(:inspect) == ARGF.method(:to_s)
	// asserts — two separately defined methods with the same body would not be
	// equal.
	d("to_s", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString("ARGF")
	})
	aliasBuiltin(cls, "inspect", "to_s")

	// argv: the (mutating) Array ARGF draws its inputs from — the live ARGV for
	// the singleton, its own list for an ARGF.class.new instance.
	d("argv", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if arr := self(v).argvArray(); arr != nil {
			return arr
		}
		return object.NilV
	})

	// close: io.c argf_close_m — closes the current file (so an IO taken from
	// #to_io beforehand reports closed?), arms the next one, and resets the line
	// counter. Calling it again on a spent stream is not an error.
	d("close", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		a.nextArgv()
		a.closeCur()
		if a.nextP != -1 {
			a.nextP = 1
		}
		a.lineno = 0
		return v
	})

	// closed?: io.c argf_closed — asks the current file, which is why ARGF has to
	// keep naming a stream it has closed.
	d("closed?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		a.nextArgv()
		if a.cur == nil {
			return object.Bool(false)
		}
		return object.Bool(vm.send(a.cur, "closed?", nil, nil).Truthy())
	})

	// skip: io.c argf_skip — abandons the current file so the next read starts on
	// the following one. It has no effect before reading has started, or when the
	// next file is already armed, so calling it twice is the same as once.
	d("skip", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		if a.initP != 0 && a.nextP == 0 {
			a.closeCur()
			a.nextP = 1
		}
		return v
	})

	// binmode / binmode?: io.c argf_binmode_m / argf_binmode_p. Binary mode is a
	// property of ARGF, not of one file: it is applied to the current stream and to
	// every stream opened afterwards, and it cannot be undone.
	d("binmode", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		a.binmode = true
		a.nextArgv()
		a.applyBinmode(a.cur)
		return v
	})
	d("binmode?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).binmode)
	})

	// getc / readchar: the next character across the files.
	d("getc", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).readAcross(func(o *IOObj) object.Value { return vm.send(o, "getc", nil, nil) })
	})
	d("readchar", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		c := vm.send(v, "getc", nil, nil)
		if object.IsNil(c) {
			raise("EOFError", "end of file reached")
		}
		return c
	})
	// getbyte / readbyte: the next byte across the files.
	d("getbyte", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).readAcross(func(o *IOObj) object.Value { return vm.send(o, "getbyte", nil, nil) })
	})
	d("readbyte", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		b := vm.send(v, "getbyte", nil, nil)
		if object.IsNil(b) {
			raise("EOFError", "end of file reached")
		}
		return b
	})

	// each_byte / each_char / each_codepoint yield every unit across the files;
	// with no block each returns an Enumerator.
	iter := func(name, one string, conv func(vm *VM, c object.Value) object.Value) NativeFn {
		return func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
			if blk == nil {
				return argfEnum(v, name)
			}
			for {
				c := vm.send(v, one, nil, nil)
				if object.IsNil(c) {
					return v
				}
				vm.callBlock(blk, []object.Value{conv(vm, c)})
			}
		}
	}
	same := func(_ *VM, c object.Value) object.Value { return c }
	d("each_byte", iter("each_byte", "getbyte", same))
	d("each_char", iter("each_char", "getc", same))
	d("each_codepoint", iter("each_codepoint", "getc", func(vm *VM, c object.Value) object.Value {
		return vm.send(c, "ord", nil, nil)
	}))

	// readpartial / read_nonblock: io.c argf_readpartial / argf_read_nonblock, both
	// argf_getpartial. Unlike #read they stop at each file boundary, returning ""
	// there and raising EOFError only on the last file.
	d("readpartial", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return self(v).getPartial(vm, args, false)
	})
	d("read_nonblock", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return self(v).getPartial(vm, args, true)
	})

	// pos / tell / pos= / seek / fileno / to_i delegate to the current file's IO
	// (which carries the byte cursor and descriptor). io.c raises ArgumentError —
	// NOT IOError — when next_argv finds no stream, and each site has its own
	// message; #tell/#pos and #fileno/#to_i are true aliases, which their specs
	// assert through Method equality.
	delegate := func(meth, noStream string) NativeFn {
		return func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			a := self(v)
			if !a.nextArgv() || a.cur == nil {
				raise("ArgumentError", "%s", noStream)
			}
			return vm.send(a.cur, meth, args, nil)
		}
	}
	d("pos", delegate("pos", "no stream to tell"))
	aliasBuiltin(cls, "tell", "pos")
	d("pos=", delegate("pos=", "no stream to set position"))
	d("seek", delegate("seek", "no stream to seek"))
	d("fileno", delegate("fileno", "no stream"))
	aliasBuiltin(cls, "to_i", "fileno")
	d("set_encoding", delegate("set_encoding", "no stream to set encoding"))
	d("external_encoding", delegate("external_encoding", "no stream"))
	d("internal_encoding", delegate("internal_encoding", "no stream"))
	// rewind returns the current file to its start and resets the line counter.
	d("rewind", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		if !a.nextArgv() || a.cur == nil {
			raise("ArgumentError", "no stream to rewind")
		}
		a.cur.pos = 0
		a.cur.lineno = 0
		a.lineno = 0
		a.vm.globals["$."] = object.IntValue(0)
		return object.IntValue(0)
	})
}

// includeARGFEnumerable mixes Enumerable into ARGF.class, as io.c Init_IO does
// with rb_include_module(rb_cARGF, rb_mEnumerable). It cannot run inside
// registerARGF: Enumerable is defined by the Ruby prelude, which is loaded after
// the built-ins, so the module does not exist yet there — the same reason
// includeStringIOEnumerable is a separate post-prelude step.
func (vm *VM) includeARGFEnumerable() {
	en, ok := vm.consts["Enumerable"].(*RClass)
	if !ok || vm.cARGF == nil || hasInclude(vm.cARGF, en) {
		return
	}
	vm.cARGF.includes = append(vm.cARGF.includes, en)
}

// argfEnum is the Enumerator ARGF's iterators return without a block. Its #size
// is nil, not a count: MRI gives ARGF's each_byte/each_char/each_codepoint/
// each_line enumerators NO size function, because ARGF cannot know how many units
// remain without reading the files. Counting them (which is what an Enumerator
// with no size block falls back to) would answer a question MRI declines.
func argfEnum(recv object.Value, meth string, args ...object.Value) *Enumerator {
	return enumForSized(recv, meth, func(*VM) object.Value { return object.NilV }, args...)
}
