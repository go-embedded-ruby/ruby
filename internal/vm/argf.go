// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// argfObj backs the ARGF stream ($< / ARGF): a virtual concatenation of the
// files named in ARGV (falling back to $stdin when ARGV is empty), read as one
// continuous stream. It consumes ARGV — each filename is shifted off as it is
// opened — and tracks a cumulative line number ($.).
type argfObj struct {
	vm       *VM
	cur      *IOObj   // the stream currently being read, nil before the first read
	started  bool     // whether any stream has been selected yet
	lineno   int      // cumulative line count across all files ($.)
	curName  string   // the current file's name ("-" for $stdin)
	files    []string // pending filenames for an ARGF.class.new(*files) instance
	fromARGV bool     // the singleton ARGF: draw filenames from the live ARGV
}

// nextName shifts and returns the next input filename (from the live ARGV for the
// singleton, else from the instance's own list), or "", false when none remain.
func (a *argfObj) nextName() (string, bool) {
	if a.fromARGV {
		if argv, ok := a.vm.consts["ARGV"].(*object.Array); ok && len(argv.Elems) > 0 {
			name := ""
			if s, ok := argv.Elems[0].(*object.String); ok {
				name = s.Str()
			}
			argv.Elems = argv.Elems[1:]
			return name, true
		}
		return "", false
	}
	if len(a.files) > 0 {
		name := a.files[0]
		a.files = a.files[1:]
		return name, true
	}
	return "", false
}

func (a *argfObj) ToS() string     { return "ARGF" }
func (a *argfObj) Inspect() string { return "ARGF" }
func (a *argfObj) Truthy() bool    { return true }

// ensure returns the stream that still has bytes to read, advancing to the next
// ARGV file (or $stdin, once, when ARGV is empty) as each is exhausted. It
// returns nil when every input has been consumed.
func (a *argfObj) ensure() *IOObj {
	for {
		if a.cur != nil {
			a.cur.pipeRefresh()
			if a.cur.pos < len(a.cur.buf) {
				return a.cur
			}
		}
		if name, ok := a.nextName(); ok {
			a.cur = openFileIO(a.vm.consts["File"].(*RClass), name, "r")
			a.curName = name
			a.started = true
			continue
		}
		if !a.started {
			a.cur, _ = a.vm.globals["$stdin"].(*IOObj)
			a.curName = "-"
			a.started = true
			if a.cur != nil {
				continue
			}
		}
		return nil
	}
}

// curIO returns the current stream (selecting the first input if reading has not
// started), even when it is at EOF — unlike ensure, which skips past an exhausted
// file. It is nil only when there is no input at all.
func (a *argfObj) curIO() *IOObj {
	if !a.started {
		a.ensure()
	}
	return a.cur
}

// bumpLine advances ARGF's line counter and mirrors it into $. as MRI does.
func (a *argfObj) bumpLine() {
	a.lineno++
	a.vm.globals["$."] = object.IntValue(int64(a.lineno))
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
		files := make([]string, 0, len(args))
		for _, v := range args {
			if s, ok := v.(*object.String); ok {
				files = append(files, s.Str())
			} else {
				files = append(files, vm.send(v, "to_s", nil, nil).(*object.String).Str())
			}
		}
		return &argfObj{vm: vm, files: files}
	}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *argfObj { return v.(*argfObj) }

	// read(length = nil): read length bytes (or all remaining when nil) across the
	// files. With a length, returns nil once every input is exhausted; with none,
	// returns "" at end of input.
	d("read", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a := self(v)
		length, hasLen := -1, false
		if len(args) > 0 && !object.IsNil(args[0]) {
			length, hasLen = int(intArg(args[0])), true
		}
		var out []byte
		for {
			o := a.ensure()
			if o == nil {
				break
			}
			avail := o.buf[o.pos:]
			if !hasLen {
				out = append(out, avail...)
				o.pos = len(o.buf)
				continue
			}
			take := length - len(out)
			if take > len(avail) {
				take = len(avail)
			}
			out = append(out, avail[:take]...)
			o.pos += take
			if len(out) >= length {
				break
			}
		}
		if hasLen && len(out) == 0 {
			return object.NilV
		}
		return object.NewString(string(out))
	})

	// gets(sep = $/, ...): the next line across the files, advancing $. ; nil at
	// end of input.
	d("gets", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a := self(v)
		o := a.ensure()
		if o == nil {
			return object.NilV
		}
		// ensure guarantees o has bytes, so ioGets yields a line here.
		line := vm.ioGets(o, args)
		a.bumpLine()
		return line
	})

	// readline: like gets but raises EOFError at end of input.
	d("readline", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		line := vm.send(v, "gets", args, nil)
		if object.IsNil(line) {
			raise("EOFError", "end of file reached")
		}
		return line
	})

	// each_line / each { |line| … }: yield every line; returns self (or an
	// Enumerator with no block).
	eachLine := func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(v, "each_line", args...)
		}
		for {
			line := vm.send(v, "gets", args, nil)
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
		out := object.NewArray()
		for {
			line := vm.send(v, "gets", args, nil)
			if object.IsNil(line) {
				return out
			}
			out.Elems = append(out.Elems, line)
		}
	}
	d("readlines", readlines)
	aliasBuiltin(cls, "to_a", "readlines")

	// eof? / eof: true when the current stream is exhausted and no file remains.
	eof := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ensure() == nil)
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

	// filename / path: the name of the file currently being read ("-" for stdin),
	// selecting the first input if reading has not started.
	filename := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		if !a.started {
			a.ensure() // selects the first input, setting curName
		}
		return object.NewString(a.curName)
	}
	d("filename", filename)
	aliasBuiltin(cls, "path", "filename")

	// to_io / file: the underlying IO of the current file.
	d("to_io", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		if o := a.ensure(); o != nil {
			return o
		}
		return object.NilV
	})

	// #inspect / #to_s ("ARGF") come from the argfObj ToS/Inspect via Object's
	// default display path.

	// argv: the (mutating) ARGV array ARGF draws its inputs from.
	d("argv", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.consts["ARGV"]
	})
	// close closes the current file and drops any remaining inputs; skip abandons
	// the current file so the next read starts on the following one. binmode is a
	// no-op (rbgo reads bytes already). Each returns ARGF.
	d("close", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		a.cur, a.files, a.started = nil, nil, true
		return v
	})
	d("skip", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).cur = nil
		return v
	})
	d("binmode", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value { return v })

	// getc / readchar: the next character across the files.
	d("getc", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		o := self(v).ensure()
		if o == nil {
			return object.NilV
		}
		return vm.send(o, "getc", nil, nil)
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
		o := self(v).ensure()
		if o == nil {
			return object.NilV
		}
		return vm.send(o, "getbyte", nil, nil)
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
				return enumFor(v, name)
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

	// pos / tell / pos= / seek / fileno / to_i delegate to the current file's IO
	// (which carries the byte cursor and descriptor).
	delegate := func(meth string) NativeFn {
		return func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			o := self(v).curIO()
			if o == nil {
				raise("ArgumentError", "no stream")
			}
			return vm.send(o, meth, args, nil)
		}
	}
	d("pos", delegate("pos"))
	d("tell", delegate("tell"))
	d("pos=", delegate("pos="))
	d("seek", delegate("seek"))
	d("readpartial", delegate("readpartial"))
	d("fileno", delegate("fileno"))
	d("to_i", delegate("fileno"))
	d("set_encoding", delegate("set_encoding"))
	d("external_encoding", delegate("external_encoding"))
	d("internal_encoding", delegate("internal_encoding"))
	// rewind returns the current file to its start and resets the line counter.
	d("rewind", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		o := a.curIO()
		if o == nil {
			raise("ArgumentError", "no stream to rewind")
		}
		o.pos = 0
		a.lineno = 0
		a.vm.globals["$."] = object.IntValue(0)
		return object.IntValue(0)
	})

	// file: the IO of the file currently being read.
	d("file", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		if !a.started {
			a.ensure()
		}
		if a.cur != nil {
			return a.cur
		}
		return object.NilV
	})
}
