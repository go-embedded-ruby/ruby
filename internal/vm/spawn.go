// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file gives the VM a synchronous, pure-Go process-execution model: IO.pipe
// (buffered reader/writer pair), Process.spawn / Process.waitpid2 / Process.setsid,
// Process::Status, and the Kernel#fork / Kernel#exec idiom that Puppet's
// Puppet::Util::Execution.execute path is built on (safe_posix_fork runs a block
// that reopens the standard streams and then calls Kernel.exec).
//
// There is no OS-level fork; instead Kernel.fork runs its block immediately, and
// the Kernel.exec inside that block runs the command to completion (capturing its
// combined output to wherever STDOUT/STDERR were reopened) and unwinds the block
// via a sentinel — modelling the fact that a real exec never returns. Because
// execution is synchronous, the pipe buffer is fully populated before the parent
// reads it, faithfully reproducing the blocking-read / EOF behaviour the Puppet
// loop depends on while staying CGO=0 and identical across every target OS.

// childStatus records a finished child's exit code, keyed by the synthetic pid
// returned from spawn/fork so Process.waitpid2 can report it.
type childStatus struct {
	pid  int
	code int
}

// execSentinel is raised by Kernel.exec to unwind the enclosing Kernel.fork block
// (a real exec replaces the process and never returns to the block). It carries
// the captured exit code so fork can record the child's status.
type execSentinel struct{ code int }

func (execSentinel) Error() string { return "exec sentinel" }

// runCaptured runs a command, returning its combined stdout+stderr and exit code.
// A single string with shell metacharacters/whitespace goes through the system
// shell (MRI semantics); an explicit argv is run directly. It is defined per-OS
// in spawn_native.go / spawn_windows.go / spawn_wasm.go.

// registerSpawn installs IO.pipe / read_nonblock / reopen / IO.select and the
// Process spawning entry points, plus Kernel#fork / Kernel#exec.
func (vm *VM) registerSpawn() {
	cIO := vm.consts["IO"].(*RClass)

	cIO.smethods["pipe"] = &Method{name: "pipe", owner: cIO, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		// IO.pipe is inherited: when called on a subclass, both ends are instances
		// of that subclass (io.c rb_io_s_pipe uses the receiver class).
		cls := cIO
		if c, ok := self.(*RClass); ok {
			cls = c
		}
		// Encoding arguments (an optional trailing options Hash is ignored — it
		// only carries econv flags) configure the READ end; the write end carries
		// no external/internal encoding. With no arguments the read end captures
		// the current Encoding defaults at creation time.
		pos, _ := splitIOOpts(args)
		ext, intn := vm.encPairFromArgs(pos)
		if len(pos) == 0 {
			if vm.defExternalEnc != nil {
				ext = vm.defExternalEnc.name
			}
			if vm.defInternalEnc != nil {
				intn = vm.defInternalEnc.name
			}
		}
		// An internal encoding equal to the external means no transcoding
		// (io.c rb_io_ext_int_to_enc leaves enc2 NULL), so it is dropped.
		if intn == ext {
			intn = ""
		}
		buf := &pipeBuf{}
		// Both ends of a pipe come back with O_NONBLOCK already set, which is what
		// MRI 4.0.5 reports through io/nonblock on this host.
		reader := &IOObj{cls: cls, pipe: buf, label: "pipe-r", extEnc: ext, intEnc: intn, nonblock: true}
		writer := &IOObj{cls: cls, pipe: buf, isWriteEnd: true, writable: true, label: "pipe-w", nonblock: true}
		pair := object.NewArray(reader, writer)
		if blk != nil {
			// IO.pipe { |r, w| ... } yields the pair and closes both ends after.
			defer func() { reader.closed, writer.closed, buf.wClosed = true, true, true }()
			return vm.callBlock(blk, []object.Value{reader, writer})
		}
		return pair
	}}

	// read_nonblock(len, outbuf = nil, exception: true) — io.c io_read_nonblock.
	// The order matters and is observable: a negative length is ArgumentError
	// before anything else, then the output buffer is coerced (io_setstrbuf), then
	// the stream is checked readable (GetOpenFile / rb_io_check_byte_readable), and
	// only then does a zero length return the emptied buffer. A read that would
	// block raises IO::EAGAINWaitReadable (or returns :wait_readable), and the
	// descriptor is left in non-blocking mode (rb_fd_set_nonblock); at end of file
	// the output buffer is emptied and EOFError raised (or nil returned).
	cIO.define("read_nonblock", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		pos, opts := splitIOOpts(args)
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
		raiseOnBlock := true
		if opts != nil {
			if v, ok := opts.Get(object.Symbol("exception")); ok {
				raiseOnBlock = v.Truthy()
			}
		}
		ioCheckReadable(o)
		if n == 0 {
			return ioReadResult(nil, buf)
		}
		o.pipeRefresh()
		o.nonblock = true // rb_fd_set_nonblock on the descriptor being read
		if avail := len(o.buf) - o.pos; avail <= 0 {
			if o.pipe == nil || o.pipeWriterClosed() { // end of file
				ioReadResult(nil, buf) // io_set_read_length(str, 0) empties the buffer
				if !raiseOnBlock {
					return object.NilV
				}
				raise("EOFError", "end of file reached")
			}
			if !raiseOnBlock {
				return object.Symbol("wait_readable")
			}
			raise("IO::EAGAINWaitReadable", "Resource temporarily unavailable - read would block")
		} else if n > avail {
			n = avail
		}
		data := o.buf[o.pos : o.pos+n]
		o.pos += n
		return ioReadResult(data, buf)
	})

	// readpartial(maxlen, outbuf = nil): a length-limited read that, unlike
	// read_nonblock, blocks for data rather than raising EAGAIN. It returns at most
	// maxlen bytes of whatever is already buffered; at EOF (write end closed, no
	// bytes) it raises EOFError. io.c io_getpartial / rb_io_readpartial: a negative
	// maxlen raises ArgumentError; a closed/unreadable stream raises IOError before
	// the maxlen==0 shortcut; maxlen==0 returns the (cleared) buffer immediately;
	// the output buffer receives the data and is returned (its encoding preserved),
	// and is cleared on the EOF error path.
	cIO.define("readpartial", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*IOObj)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		n := vm.ioOfftArg(args[0]) // NUM2LONG (#to_int); a Bignum raises RangeError
		if n < 0 {
			raise("ArgumentError", "negative length %d given", n)
		}
		var buf *object.String
		if len(args) > 1 && !object.IsNil(args[1]) {
			buf, _ = args[1].(*object.String)
		}
		ioCheckReadable(o) // closed / read half shut → IOError, before the len==0 case
		if n == 0 {
			return ioReadResult(nil, buf) // empty read: the (cleared) buffer, no blocking
		}
		o.pipeRefresh()
		avail := len(o.buf) - o.pos
		if avail <= 0 {
			// A regular stream (File/StringIO) at end-of-data, or a pipe whose write
			// end is closed, is at EOF: clear the output buffer and raise EOFError.
			if o.pipe == nil || o.pipeWriterClosed() {
				ioReadResult(nil, buf) // io_set_read_length(str, 0) clears the buffer, then EOF
				raise("EOFError", "end of file reached")
			}
			// A pipe with the write end still open: a real readpartial would block;
			// the synchronous model has no more bytes coming, so report would-block.
			raise("Errno::EAGAIN", "Resource temporarily unavailable - read would block")
		}
		if n > avail {
			n = avail
		}
		data := o.buf[o.pos : o.pos+n]
		o.pos += n
		return ioReadResult(data, buf)
	})

	// IO#reopen and IO#fcntl live in io_descriptors.go, beside the rest of the
	// descriptor-level surface. Its standard-stream branch is what Puppet's
	// safe_posix_fork STDOUT.reopen(pipe_writer) rides on, and what runForkBlock
	// below snapshots and restores around a forked block.
	defIOReopen(cIO)

	// IO.select(read, write, except, timeout) reports readiness. io.c rb_f_select
	// converts the timeout first (rb_time_interval), then select_internal type-
	// checks each of the three sets with Check_Type(T_ARRAY) and puts every element
	// through rb_io_get_io (#to_io) — so a non-Array set, a non-IO element and a
	// bad timeout all raise before any readiness is examined. The SUPPLIED object
	// is what comes back in the result, not its #to_io conversion.
	//
	// Readiness itself follows this VM's synchronous model: a regular (buffer-
	// backed) stream is always ready, as select(2) reports a regular file; a pipe
	// reader is ready when it has buffered bytes or its write end is closed (so a
	// subsequent read returns EOF rather than blocking) and a pipe writer is never
	// read-ready; writers and exception sets are always reported ready.
	cIO.smethods["select"] = &Method{name: "select", owner: cIO, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..4)")
		}
		if len(args) > 3 && !object.IsNil(args[3]) {
			if f := mutexSleepDur(args[3]); math.IsNaN(f) {
				raise("RangeError", "NaN out of Time range")
			}
		}
		readReady := object.NewArray()
		for _, v := range selectSet(vm, args, 0) {
			o := ioGetIO(vm, v)
			o.pipeRefresh()
			if ioSelectReadable(o) {
				readReady.Elems = append(readReady.Elems, v)
			}
		}
		writeReady := object.NewArray()
		for _, v := range selectSet(vm, args, 1) {
			ioGetIO(vm, v)
			writeReady.Elems = append(writeReady.Elems, v)
		}
		for _, v := range selectSet(vm, args, 2) { // type-checked, never reported ready
			ioGetIO(vm, v)
		}
		if len(readReady.Elems) == 0 && len(writeReady.Elems) == 0 {
			return object.NilV
		}
		return object.NewArray(readReady, writeReady, object.NewArray())
	}}

	vm.registerProcessSpawn()
	vm.registerKernelExec()
}

// selectSet returns the elements of IO.select's i-th argument set. A missing or
// nil set is empty; anything that is not an Array is the TypeError
// select_internal's Check_Type(T_ARRAY) raises.
func selectSet(vm *VM, args []object.Value, i int) []object.Value {
	if i >= len(args) || object.IsNil(args[i]) {
		return nil
	}
	arr, ok := args[i].(*object.Array)
	if !ok {
		raise("TypeError", "wrong argument type %s (expected Array)", vm.classOf(args[i]).name)
	}
	return arr.Elems
}

// ioSelectReadable reports whether a read on o would return without blocking. A
// pipe end answers from the shared buffer (its write end never reads); anything
// else is a regular, fully-buffered stream, which select(2) always reports
// readable — including at end of file, where the read returns nil rather than
// blocking.
func ioSelectReadable(o *IOObj) bool {
	if o.pipe != nil {
		return !o.isWriteEnd && (o.pos < len(o.buf) || o.pipe.wClosed)
	}
	return true
}

// registerProcessSpawn adds spawn / waitpid2 / setsid / Status to the Process
// module (already created by registerProcess).
func (vm *VM) registerProcessSpawn() {
	mod := vm.consts["Process"].(*RClass)
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// WNOHANG / WUNTRACED are the wait flags Puppet passes to waitpid2; only their
	// truthiness (non-blocking) matters to the synchronous model.
	mod.consts["WNOHANG"] = object.IntValue(1)
	vm.consts["Process::WNOHANG"] = object.IntValue(1)
	mod.consts["WUNTRACED"] = object.IntValue(2)
	vm.consts["Process::WUNTRACED"] = object.IntValue(2)

	status := newClass("Status", vm.cObject)
	status.name, status.named = "Process::Status", true
	mod.consts["Status"] = status
	vm.consts["Process::Status"] = status
	status.define("exitstatus", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*RObject).ivars["@exitstatus"]
	})
	status.define("success?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*RObject).ivars["@exitstatus"] == object.IntValue(0))
	})
	status.define("pid", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*RObject).ivars["@pid"]
	})

	// spawn(env?, command..., opts?) runs the command synchronously, writing its
	// combined output to the :out / :err redirection targets, and returns a pid.
	def("spawn", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		cmd, opts := parseSpawnArgs(args)
		out, code := runCaptured(cmd)
		writeSpawnOutput(opts, out)
		return object.IntValue(vm.recordChild(code))
	})

	def("waitpid2", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pid := int(intArg(args[0]))
		st, ok := vm.reapChild(pid)
		if !ok {
			// WNOHANG (a non-zero flag) on an unknown/already-reaped child means
			// "not ready / no child" — nil, as MRI returns.
			if len(args) > 1 && intArg(args[1]) != 0 {
				return object.NilV
			}
			raise("Errno::ECHILD", "No child processes")
		}
		so := &RObject{class: vm.consts["Process::Status"].(*RClass), ivars: map[string]object.Value{}}
		so.ivars["@exitstatus"] = object.IntValue(int64(st.code))
		so.ivars["@pid"] = object.IntValue(int64(st.pid))
		return object.NewArray(object.IntValue(int64(pid)), so)
	})

	def("waitpid", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pid := int(intArg(args[0]))
		if _, ok := vm.reapChild(pid); !ok {
			if len(args) > 1 && intArg(args[1]) != 0 {
				return object.NilV
			}
			raise("Errno::ECHILD", "No child processes")
		}
		return object.IntValue(int64(pid))
	})

	// setsid has no meaning without a real session, but Puppet calls it inside the
	// forked block; return a pid so the call succeeds.
	def("setsid", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(processGID()))
	})
}

// registerKernelExec installs Kernel#fork and Kernel#exec on Object so they are
// available to the main program and to library code (Puppet's safe_posix_fork).
func (vm *VM) registerKernelExec() {
	def := func(name string, fn NativeFn) {
		vm.cObject.methods[name] = &Method{name: name, owner: vm.cObject, native: fn}
	}

	// fork runs its block in the current process (no OS fork). Kernel.exec inside
	// the block unwinds it via execSentinel, carrying the child's exit code; fork
	// records the child status and returns a synthetic pid. A block that returns
	// normally (no exec) is treated as a child that exited 0.
	def("fork", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("NotImplementedError", "fork without a block is not supported (no OS-level fork)")
		}
		code := vm.runForkBlock(blk)
		return object.IntValue(vm.recordChild(code))
	})

	// exec runs the command to completion (capturing combined output to the
	// current $stdout) and raises execSentinel so the enclosing fork block unwinds.
	// At top level (no enclosing fork) it terminates the program with the child's
	// exit code, as a real exec replaces the process.
	def("exec", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cmd := spawnCommand(args)
		out, code := runCaptured(cmd)
		vm.curStdout().writeStr(out)
		panic(execSentinel{code: code})
	})

	// system(env?, command..., opts?) runs the command synchronously, writing its
	// combined output to the current $stdout, and returns true when it exits 0,
	// false when it exits non-zero, and nil when the command could not be spawned
	// at all (MRI semantics). The `exception: true` option raises instead of
	// returning false/nil. $? is set to the child's Process::Status.
	def("system", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cmd, opts := parseSpawnArgs(args)
		raiseOnFail := false
		if opts != nil {
			if v, ok := opts.Get(object.Symbol("exception")); ok {
				raiseOnFail = v.Truthy()
			}
		}
		out, code, spawned := systemCommand(cmd)
		vm.curStdout().writeStr(out)
		if !spawned {
			// A command that never started leaves $? unset (nil), as MRI does.
			vm.globals["$?"] = object.NilV
			if raiseOnFail {
				raise("Errno::ENOENT", "No such file or directory - %s", strings.Join(cmd, " "))
			}
			return object.NilV
		}
		vm.globals["$?"] = vm.newProcessStatus(vm.recordChild(code), code)
		if code == 0 {
			return object.Bool(true)
		}
		if raiseOnFail {
			raise("RuntimeError", "Command failed with exit %d: %s", code, strings.Join(cmd, " "))
		}
		return object.Bool(false)
	})
}

// newProcessStatus builds a Process::Status object recording a finished child's
// pid and exit code, used to populate $? after Kernel#system.
func (vm *VM) newProcessStatus(pid int64, code int) object.Value {
	so := &RObject{class: vm.consts["Process::Status"].(*RClass), ivars: map[string]object.Value{}}
	so.ivars["@exitstatus"] = object.IntValue(int64(code))
	so.ivars["@pid"] = object.IntValue(pid)
	return so
}

// runForkBlock calls blk, catching the execSentinel that Kernel.exec raises and
// returning the captured exit code; a normal return means exit code 0.
//
// A real fork runs the block in a separate process, so any standard-stream
// redirection it performs (Puppet's safe_posix_fork does STDOUT.reopen(pipe))
// is private to the child. Without an OS fork we run the block in-process, so we
// snapshot and restore the STDOUT/STDERR reopen state around it; otherwise the
// redirection would leak into the parent and swallow its later output.
func (vm *VM) runForkBlock(blk *Proc) (code int) {
	stdout, stderr := vm.curStdout(), vm.curStderr()
	savedOut, savedErr := stdout.reopened, stderr.reopened
	defer func() {
		stdout.reopened, stderr.reopened = savedOut, savedErr
		if r := recover(); r != nil {
			if s, ok := r.(execSentinel); ok {
				code = s.code
				return
			}
			panic(r)
		}
	}()
	vm.callBlock(blk, nil)
	return 0
}

// recordChild stores a finished child's exit code under a fresh synthetic pid and
// returns that pid.
func (vm *VM) recordChild(code int) int64 {
	vm.childPidSeq++
	pid := 100000 + vm.childPidSeq
	vm.children = append(vm.children, childStatus{pid: int(pid), code: code})
	return pid
}

// reapChild removes and returns the recorded status for pid.
func (vm *VM) reapChild(pid int) (childStatus, bool) {
	for i, c := range vm.children {
		if c.pid == pid {
			vm.children = append(vm.children[:i], vm.children[i+1:]...)
			return c, true
		}
	}
	return childStatus{}, false
}

// spawnArgEnv splits a leading environment Hash and a trailing options Hash off a
// spawn/exec argument list, returning the bare command argv.
func parseSpawnArgs(args []object.Value) (cmd []string, opts *object.Hash) {
	rest := args
	if len(rest) > 0 {
		if _, ok := rest[0].(*object.Hash); ok {
			rest = rest[1:] // leading env Hash — ignored (custom_environment handled by caller)
		}
	}
	if len(rest) > 0 {
		if h, ok := rest[len(rest)-1].(*object.Hash); ok {
			opts = h
			rest = rest[:len(rest)-1]
		}
	}
	for _, a := range rest {
		cmd = append(cmd, a.ToS())
	}
	return cmd, opts
}

// spawnCommand reduces a Kernel.exec argument list (no options) to argv.
func spawnCommand(args []object.Value) []string {
	cmd, _ := parseSpawnArgs(args)
	return cmd
}

// writeSpawnOutput sends captured output to the :out (and :err, when distinct)
// redirection IO targets named in a spawn options Hash.
func writeSpawnOutput(opts *object.Hash, out string) {
	if opts == nil {
		return
	}
	if v, ok := opts.Get(object.Symbol("out")); ok {
		if o, ok := v.(*IOObj); ok {
			o.writeStr(out)
		}
	}
}

// shellish reports whether a single command string should be run through the
// system shell (it contains shell metacharacters or whitespace), as MRI decides
// for a one-string command.
func shellish(cmd []string) (string, bool) {
	if len(cmd) != 1 {
		return "", false
	}
	s := cmd[0]
	if strings.ContainsAny(s, " \t\n*?{}[]<>()~&|^$;'\"\\`") {
		return s, true
	}
	return s, false
}
