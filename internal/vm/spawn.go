// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
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
		// MRI 4.0.5 reports through io/nonblock on this host. Each end carries one
		// half of the access mode (rb_io_s_pipe opens FMODE_READABLE on the read end
		// and FMODE_WRITABLE on the write end), so reading the write end or writing
		// the read end raises, and #close_read/#close_write close the right one.
		reader := &IOObj{cls: cls, pipe: buf, label: "pipe-r", extEnc: ext, intEnc: intn, nonblock: true, wrClosed: true}
		writer := &IOObj{cls: cls, pipe: buf, isWriteEnd: true, writable: true, label: "pipe-w", nonblock: true, rdClosed: true}
		pair := object.NewArray(reader, writer)
		if blk != nil {
			// IO.pipe { |r, w| ... } yields the pair and closes both ends after.
			defer func() {
				reader.closed, writer.closed = true, true
				buf.wClosed, buf.rClosed = true, true
			}()
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

	// IO.popen([env,] cmd, mode = "r", **opts) — io.c rb_io_s_popen, which peels a
	// trailing options Hash and then a leading environment Hash before reading the
	// command and the mode, and pipe_open, which gives back a stream whose
	// readable/writable halves follow that mode (so "r" raises IOError on write and
	// "w" raises IOError on read). With a block the stream is yielded and closed
	// afterwards (popen_finish → pipe_close), and $? carries the child's status.
	//
	// The child runs TO COMPLETION here and its output is buffered, which is the
	// process model this whole file is built on (see the header): there is no
	// concurrent child, so nothing the parent writes afterwards can reach its
	// standard input — the child is run with an empty one. Everything else about
	// the stream is real: the write half accepts bytes and reports their count,
	// close_write/close_read shut the halves, and the mode gates both.
	cIO.smethods["popen"] = &Method{name: "popen", owner: cIO, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		cls := cIO
		if c, ok := self.(*RClass); ok {
			cls = c
		}
		o := vm.popenOpen(cls, args)
		if blk == nil {
			return o
		}
		defer func() { o.closed, o.rdClosed, o.wrClosed = true, true, true }()
		return vm.callBlock(blk, []object.Value{o})
	}}

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

	// spawn([env,] command..., [options]) runs the command and returns the pid of
	// the child whose status Process.wait reaps. process.c rb_f_spawn.
	def("spawn", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.IntValue(vm.runExecArg(vm.parseExecArgs(args, true)))
	})

	// wait / wait2 / waitpid / waitpid2 / waitall / last_status, plus
	// Process::Status.wait.
	vm.registerProcessWait(mod, status)

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

	// exec runs the command to completion and raises execSentinel so the enclosing
	// fork block unwinds (a real exec never returns to its caller). At top level —
	// no enclosing fork — it terminates the program with the child's exit code, as
	// a real exec replaces the process. Argument handling is rb_f_exec's, which is
	// rb_execarg_new with accept_shell: a command that cannot be executed raises
	// its errno here rather than in a child.
	execFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		e := vm.parseExecArgs(args, true)
		if e.shell == "" {
			path, class, msg, ok := spawnResolve(e.program(), e.env())
			if !ok {
				raise(class, "%s", msg)
			}
			e.path = path
		}
		panic(execSentinel{code: vm.runPrepared(e)})
	}
	def("exec", execFn)
	// Process.exec is the same function (process.c registers rb_f_exec as both a
	// global function and a Process module function), and must be callable with
	// an explicit receiver — Kernel#exec alone is private.
	procMod := vm.consts["Process"].(*RClass)
	procMod.smethods["exec"] = &Method{name: "exec", owner: procMod, native: execFn}

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

// popenOpen builds the stream IO.popen hands back. It splits the arguments the
// way io.c rb_io_s_popen does — a trailing options Hash, then a leading
// environment Hash, then the command and an optional mode — runs the command,
// and returns a buffered stream holding its output whose access halves follow
// the mode (pipe_open passes fmode straight through). $? is set to the child's
// status, and #pid reports it.
func (vm *VM) popenOpen(cls *RClass, args []object.Value) *IOObj {
	pos := args
	if len(pos) > 1 {
		if _, ok := pos[len(pos)-1].(*object.Hash); ok {
			pos = pos[:len(pos)-1] // exec options; none of them are honoured here
		}
	}
	if len(pos) > 1 {
		if _, ok := pos[0].(*object.Hash); ok {
			pos = pos[1:] // leading environment Hash — runCaptured takes no environment
		}
	}
	if len(pos) < 1 || len(pos) > 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(pos))
	}
	mode := "r"
	if len(pos) > 1 && !object.IsNil(pos[1]) {
		mode = modeBase(vm.vmodeString(pos[1]))
	}
	// "-" asks pipe_open to fork the interpreter itself (is_popen_fork), which
	// needs a working fork; MRI raises exactly this without one.
	if s, ok := pos[0].(*object.String); ok && s.Str() == "-" {
		raise("NotImplementedError", "fork() function is unimplemented on this machine")
	}
	out, code := runCaptured(spawnCommand(pos[:1]))
	pid := vm.recordChild(code)
	vm.globals["$?"] = vm.newProcessStatus(pid, code)
	o := &IOObj{cls: cls, isStr: true, buf: []byte(out), label: "popen", nonblock: true,
		popen: &popenProc{pid: pid}, openMode: mode}
	// pipe_open hands back a stream whose halves are exactly the mode's: a bare
	// "r" cannot be written and a bare "w" cannot be read, while any "+" mode is
	// duplex (which is what IO#close_read / #close_write call a duplexed stream).
	plus := strings.Contains(mode, "+")
	o.rdClosed = !plus && mode != "r"
	o.wrClosed = !plus && mode == "r"
	o.duplex = plus // FMODE_DUPLEX: only a "+" popen has two independent halves
	return o
}

// popenProc records the child IO.popen ran, so #pid can report it and writes to
// the stream have somewhere to go that is not the buffer being read.
type popenProc struct {
	pid   int64
	stdin []byte
}

// ---------------------------------------------------------------------------
// Process.spawn / Process.exec argument handling
//
// Read for this: ruby/ruby v3_4_0 process.c — rb_exec_getargs (peel the options
// Hash off the END, then the environment Hash off the FRONT), rb_check_argv
// (the [prog, argv0] command array, StringValue then StringValueCStr on every
// element), check_exec_env_i (the '=' and null-byte rules on an environment
// entry), rb_execarg_addopt and check_exec_options_i (the option keys and the
// two "wrong exec option" errors), check_exec_redirect (:out/:err targets), and
// rb_f_spawn / rb_f_exec.
//
// The execution model is the synchronous one this file is built on (see the
// header): the child runs to completion inside Process.spawn and its status is
// recorded for Process.wait to reap. What IS real is everything the parent can
// observe — the environment the child sees, its working directory, the files
// :out and :err name, and the errno a command that cannot be executed reports.

// execArg is the peeled, validated form of a spawn/exec argument list: MRI's
// struct rb_execarg, reduced to the fields this VM can honour.
type execArg struct {
	argv []string // the command as the child sees it; argv[0] is its own name
	prog string   // the [prog, argv0] command array's FIRST element: the file to
	// execute, when it differs from the name the child is given
	shell string // non-empty when the single-string form goes to the shell
	path  string // the program resolved against the child's PATH
	dir   string // :chdir
	// The environment Hash's own pairs, kept apart from the interpreter's
	// environment until the options have been read: :unsetenv_others decides
	// whether the child inherits anything beyond them.
	envOver     map[string]string
	envUnset    []string
	envGiven    bool
	unsetOthers bool
	out, err    *spawnSink
}

// env returns the environment the child runs with: nil when it simply inherits
// the interpreter's, the Hash's pairs alone under :unsetenv_others, and the two
// merged otherwise.
func (e *execArg) env() []string {
	if !e.envGiven && !e.unsetOthers {
		return nil
	}
	if e.unsetOthers {
		return mergeEnv(nil, e.envOver, nil)
	}
	return mergeEnv(os.Environ(), e.envOver, e.envUnset)
}

// spawnSink is where one of the child's output streams goes. A nil sink means
// "the parent's own stream".
type spawnSink struct {
	io     *IOObj // :out => io / :out => fd
	path   string // :out => "name" / :out => ["name", mode]
	append bool   // the ["name", "a"] form
}

// parseExecArgs performs rb_exec_getargs + rb_check_argv + the option and
// environment checks, returning the command ready to run. acceptShell is
// rb_execarg_new's flag: Process.spawn and Kernel#exec pass true, so a lone
// string with shell metacharacters goes to /bin/sh.
func (vm *VM) parseExecArgs(args []object.Value, acceptShell bool) *execArg {
	rest := args
	var opts, env *object.Hash
	// The options Hash comes off the END first, then the environment Hash off the
	// FRONT — the order rb_exec_getargs uses, and the reason `Process.spawn({})`
	// is an ArgumentError about the COMMAND rather than a lone-Hash command.
	if len(rest) > 0 {
		if h, ok := vm.execHash(rest[len(rest)-1]); ok {
			opts, rest = h, rest[:len(rest)-1]
		}
	}
	if len(rest) > 0 {
		if h, ok := vm.execHash(rest[0]); ok {
			env, rest = h, rest[1:]
		}
	}
	if len(rest) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
	}

	e := &execArg{}
	// rb_check_argv: a first argument that answers #to_ary is the [prog, argv0]
	// command array, which must hold exactly two elements.
	if ary, ok := vm.execAry(rest[0]); ok {
		if len(ary.Elems) != 2 {
			raise("ArgumentError", "wrong first argument")
		}
		// rb_check_argv: the FIRST element is the file to execute, the second is
		// what the child is told its own name is (its argv[0]).
		e.prog = vm.execString(ary.Elems[0])
		rest = append([]object.Value{ary.Elems[1]}, rest[1:]...)
	}
	for _, a := range rest {
		e.argv = append(e.argv, vm.execString(a))
	}
	// A single string with shell metacharacters is handed to the shell; an
	// explicit argv (or a command array) never is.
	if acceptShell && len(e.argv) == 1 && e.prog == "" {
		if s, sh := shellish(e.argv); sh {
			e.shell = s
		}
	}
	vm.execEnv(e, env)
	vm.execOptions(e, opts)
	return e
}

// program is the file a command actually executes: the command array's first
// element when one was given, otherwise argv[0].
func (e *execArg) program() string {
	if e.prog != "" {
		return e.prog
	}
	return e.argv[0]
}

// execHash reports whether v is the environment/options Hash slot's occupant.
// check_hash excludes String and Array outright and otherwise asks #to_hash, so
// a mock that answers #to_hash is accepted as the environment.
func (vm *VM) execHash(v object.Value) (*object.Hash, bool) {
	if h, ok := v.(*object.Hash); ok {
		return h, true
	}
	if _, isStr := v.(*object.String); isStr {
		return nil, false
	}
	if _, isAry := v.(*object.Array); isAry {
		return nil, false
	}
	if vm.respondsToDynamic(v, "to_hash") {
		if h, ok := vm.send(v, "to_hash", nil, nil).(*object.Hash); ok {
			return h, true
		}
	}
	return nil, false
}

// execAry is rb_check_array_type on the first command argument: an Array, or
// anything that answers #to_ary with one.
func (vm *VM) execAry(v object.Value) (*object.Array, bool) {
	if a, ok := v.(*object.Array); ok {
		return a, true
	}
	if vm.respondsToDynamic(v, "to_ary") {
		if a, ok := vm.send(v, "to_ary", nil, nil).(*object.Array); ok {
			return a, true
		}
	}
	return nil, false
}

// execString is StringValue followed by StringValueCStr: #to_str conversion,
// then the null-byte rejection every exec argument goes through.
func (vm *VM) execString(v object.Value) string {
	s, ok := v.(*object.String)
	if !ok {
		if !vm.respondsToDynamic(v, "to_str") {
			raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
		}
		s, ok = vm.send(v, "to_str", nil, nil).(*object.String)
		if !ok {
			raise("TypeError", "can't convert %s to String", classNameOf(v))
		}
	}
	str := s.Str()
	if strings.ContainsRune(str, 0) {
		raise("ArgumentError", "string contains null byte")
	}
	return str
}

// execEnv records a leading environment Hash's pairs on the execArg
// (check_exec_env_i): keys and values are coerced with #to_str, a key may carry
// neither '=' nor a null byte, and a nil value unsets the variable.
func (vm *VM) execEnv(e *execArg, h *object.Hash) {
	e.envOver = map[string]string{}
	if h == nil {
		return
	}
	e.envGiven = true
	for _, k := range h.Keys {
		name := vm.execString(k)
		if strings.ContainsRune(name, '=') {
			raise("ArgumentError", "environment name contains a equal : %s", name)
		}
		v, _ := h.Get(k)
		if object.IsNil(v) {
			e.envUnset = append(e.envUnset, name)
			continue
		}
		e.envOver[name] = vm.execString(v)
	}
}

// mergeEnv applies an environment Hash's overrides and removals to a base
// environment, preserving the base's order and appending new names.
func mergeEnv(base []string, over map[string]string, unset []string) []string {
	drop := make(map[string]bool, len(unset))
	for _, n := range unset {
		drop[n] = true
	}
	out := make([]string, 0, len(base)+len(over))
	seen := make(map[string]bool, len(base))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if drop[name] {
			continue
		}
		seen[name] = true
		if v, ok := over[name]; ok {
			out = append(out, name+"="+v)
			continue
		}
		out = append(out, kv)
	}
	names := make([]string, 0, len(over))
	for name := range over {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names) // deterministic order for the names the base did not carry
	for _, name := range names {
		out = append(out, name+"="+over[name])
	}
	return out
}

// execOptions applies the trailing options Hash (rb_execarg_addopt): the option
// keys this VM honours, the two argument errors MRI raises for an unknown key,
// and the :out/:err redirections.
func (vm *VM) execOptions(e *execArg, h *object.Hash) {
	if h == nil {
		return
	}
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		sym, ok := k.(object.Symbol)
		if !ok {
			// A Fixnum / IO / Array key is a file-descriptor redirection, which this
			// VM does not model; anything else is MRI's bare "wrong exec option".
			raise("ArgumentError", "wrong exec option")
		}
		switch string(sym) {
		case "out":
			e.out = vm.execRedirect(v)
		case "err":
			e.err = vm.execRedirect(v)
		case "chdir":
			e.dir = vm.execString(v)
		case "pgroup":
			vm.execPgroup(v)
		case "unsetenv_others":
			e.unsetOthers = v.Truthy()
		case "umask", "close_others", "exception", "in", "new_pgroup", "uid", "gid", "rlimit_core", "rlimit_cpu", "rlimit_nofile":
			// Accepted and ignored: these change the child's own state, which the
			// synchronous model shares with the parent, so honouring them would
			// change the interpreter rather than a child.
		default:
			raise("ArgumentError", "wrong exec option symbol: %s", string(sym))
		}
	}
}

// execPgroup validates the :pgroup option. The process group a synchronous child
// joins is the interpreter's own, so the value is only checked, never applied —
// but the checks are observable: false/nil mean "stay in this group", true and 0
// mean "a new one", and anything else is a pid that may not be negative.
func (vm *VM) execPgroup(v object.Value) {
	if !v.Truthy() || v == object.Bool(true) {
		return
	}
	if pgid := vm.procToInt(v); pgid < 0 {
		raise("ArgumentError", "negative process group ID : %d", pgid)
	}
}

// execRedirect resolves an :out / :err option to a sink (check_exec_redirect):
// a String is a file name opened for writing, a two-element [name, mode] Array
// names the mode, an IO (or an object with #to_io) is written directly, and a
// [:child, :out] pair or a file descriptor folds onto the other stream.
func (vm *VM) execRedirect(v object.Value) *spawnSink {
	switch t := v.(type) {
	case *object.String:
		return &spawnSink{path: t.Str()}
	case *IOObj:
		return &spawnSink{io: t}
	case object.Symbol:
		// :close / :out / :err / :in — a fold onto another of the child's own
		// streams, which the combined-capture model already merges.
		return nil
	case *object.Array:
		return vm.execRedirectArray(t)
	}
	if vm.respondsToDynamic(v, "to_io") {
		if o, ok := vm.send(v, "to_io", nil, nil).(*IOObj); ok {
			return &spawnSink{io: o}
		}
	}
	if _, isInt := v.(object.Integer); isInt {
		return nil // a bare descriptor number: nothing this VM can reopen
	}
	raise("ArgumentError", "wrong exec redirect action")
	return nil
}

// execRedirectArray handles the Array forms of a redirection value: the
// [:child, fd] fold and the [path, mode, perm] open.
func (vm *VM) execRedirectArray(a *object.Array) *spawnSink {
	if len(a.Elems) == 2 && a.Elems[0] == object.Symbol("child") {
		return nil
	}
	if len(a.Elems) == 0 {
		raise("ArgumentError", "wrong exec redirect action")
	}
	sink := &spawnSink{path: vm.execString(a.Elems[0])}
	if len(a.Elems) > 1 {
		if m, ok := a.Elems[1].(*object.String); ok {
			sink.append = strings.HasPrefix(m.Str(), "a")
		}
	}
	return sink
}

// spawnResolve reports the errno a command in argv form would fail with before
// it ever runs. MRI learns this from the child, which reports the failed execve
// back through its error pipe; the message it raises names the command as
// written, not the resolved path.
//
// A program whose name contains a separator is used as given: a missing path is
// ENOENT, a directory or a file without an execute bit is EACCES. A bare name is
// looked up in the child's PATH and only an executable counts, so a
// non-executable file of the same name earlier in PATH does not shadow it — the
// lookup simply fails with ENOENT.
var spawnResolve = func(prog string, env []string) (path, class, msg string, ok bool) {
	if strings.ContainsRune(prog, os.PathSeparator) {
		return spawnResolveOne(prog, prog)
	}
	for _, dir := range filepath.SplitList(spawnEnvGet(env, "PATH")) {
		if dir == "" {
			dir = "."
		}
		if found, _, _, okOne := spawnResolveOne(filepath.Join(dir, prog), prog); okOne {
			return found, "", "", true
		}
	}
	return "", "Errno::ENOENT", "No such file or directory - " + prog, false
}

// spawnResolveOne checks one candidate path, reporting the failure under the
// name the caller wrote.
func spawnResolveOne(path, prog string) (resolved, class, msg string, ok bool) {
	st, err := os.Stat(path)
	if err != nil {
		return "", "Errno::ENOENT", "No such file or directory - " + prog, false
	}
	if st.IsDir() || st.Mode().Perm()&0o111 == 0 {
		return "", "Errno::EACCES", "Permission denied - " + prog, false
	}
	return path, "", "", true
}

// spawnEnvGet reads one variable out of an environment slice, falling back to
// the interpreter's own environment when the child inherits it.
func spawnEnvGet(env []string, name string) string {
	if env == nil {
		return os.Getenv(name)
	}
	for _, kv := range env {
		if n, v, _ := strings.Cut(kv, "="); n == name {
			return v
		}
	}
	return ""
}

// spawnReq is one prepared child for the platform runner. The command is either
// a shell line or an argv whose [0] is the name the child is told — which the
// [prog, argv0] command-array form makes differ from the file at path.
type spawnReq struct {
	argv   []string // argv[0] is the name the child is given, not the file run
	shell  string
	path   string // the file to execute, already resolved against the child's PATH
	dir    string
	env    []string // nil ⇒ inherit the interpreter's environment
	stdout io.Writer
	stderr io.Writer
}

// runExecArg runs a prepared command to completion, delivers its output to the
// requested sinks, records the child's status and returns the synthetic pid
// Process.wait reaps.
func (vm *VM) runExecArg(e *execArg) int64 {
	vm.checkChdir(e)
	if e.shell == "" {
		path, class, msg, ok := spawnResolve(e.program(), e.env())
		if !ok {
			vm.spawnFailed(class, msg)
		}
		e.path = path
	}
	return vm.recordChild(vm.runPrepared(e))
}

// checkChdir rejects a :chdir directory that does not exist. MRI's child fails
// the chdir(2) before the exec and reports the errno back to the parent, so the
// caller sees Errno::ENOENT naming the directory rather than a child that
// silently failed to start.
func (vm *VM) checkChdir(e *execArg) {
	if e.dir == "" {
		return
	}
	if _, err := os.Stat(e.dir); err != nil {
		vm.spawnFailed("Errno::ENOENT", "No such file or directory - "+e.dir)
	}
}

// spawnFailed reports a command that could not be started. By the time MRI's
// parent learns of it the child it forked has already exited 127, so $? carries
// that status even though spawn itself raises — but the child is ALREADY reaped
// (process.c rb_spawn_internal waits for it), so it must not be left for
// Process.wait to find.
func (vm *VM) spawnFailed(class, msg string) {
	vm.childPidSeq++
	vm.globals["$?"] = vm.newProcessStatus(100000+vm.childPidSeq, 127)
	raise(class, "%s", msg)
}

// runPrepared hands one prepared command to the platform runner and delivers
// its two streams to their sinks, returning the child's exit code.
func (vm *VM) runPrepared(e *execArg) int {
	var out, errOut strings.Builder
	code := runSpawnProc(&spawnReq{argv: e.argv, shell: e.shell, path: e.path,
		dir: e.dir, env: e.env(), stdout: &out, stderr: &errOut})
	vm.writeSink(e.out, out.String(), vm.curStdout())
	vm.writeSink(e.err, errOut.String(), vm.curStderr())
	return code
}

// writeSink delivers one of the child's streams to its redirection target: a
// named file (created, or appended to for the ["name", "a"] form), an IO, or —
// with no redirection — the interpreter's own stream, which is what a real child
// inherits.
func (vm *VM) writeSink(sink *spawnSink, data string, dflt *IOObj) {
	if sink == nil {
		dflt.writeStr(data)
		return
	}
	if sink.io != nil {
		sink.io.writeStr(data)
		return
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if sink.append {
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(sink.path, flags, 0o644)
	if err != nil {
		// MRI opens a redirection target in the CHILD, before the exec, and reports
		// the failure back to the parent — so a bad :out path raises here, named
		// the way rb_sys_fail names it. A later short write would be the child's
		// own failure, which no parent ever sees.
		sysFail(err, sink.path)
	}
	defer f.Close()
	_, _ = f.WriteString(data)
}

// reapAny removes and returns a recorded child: the one with the given pid, or
// the oldest still unreaped when pid is -1 or 0 (wait's "any child" form).
func (vm *VM) reapAny(pid int) (childStatus, bool) {
	if pid > 0 {
		return vm.reapChild(pid)
	}
	if len(vm.children) == 0 {
		return childStatus{}, false
	}
	c := vm.children[0]
	vm.children = vm.children[1:]
	return c, true
}

// waitForChild is the body shared by Process.wait / wait2 / waitpid / waitpid2:
// it coerces the optional pid and flags, reaps a child, and sets $?. A missing
// child is Errno::ECHILD, unless a non-zero flag (WNOHANG) was passed, which
// reports "nothing ready" instead.
func (vm *VM) waitForChild(args []object.Value) (childStatus, bool) {
	pid := -1
	if len(args) > 0 && !object.IsNil(args[0]) {
		pid = int(vm.procToInt(args[0]))
	}
	flags := 0
	if len(args) > 1 && !object.IsNil(args[1]) {
		flags = int(vm.procToInt(args[1]))
	}
	st, ok := vm.reapAny(pid)
	if !ok {
		if flags != 0 {
			return childStatus{}, false
		}
		raise("Errno::ECHILD", "No child processes")
	}
	vm.globals["$?"] = vm.newProcessStatus(int64(st.pid), st.code)
	return st, true
}

// registerProcessWait installs the wait family, Process.last_status and
// Process::Status.wait. process.c: proc_wait / proc_wait2 / proc_waitall are
// the same three entry points, and "waitpid" / "waitpid2" are MRI's aliases of
// the first two (rb_define_module_function twice over the same C function), so
// they share one Method object here and compare equal through Method#==, which
// is what core/process/waitpid_spec.rb asserts.
func (vm *VM) registerProcessWait(mod *RClass, status *RClass) {
	wait := &Method{name: "wait", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		st, ok := vm.waitForChild(args)
		if !ok {
			return object.NilV
		}
		return object.IntValue(int64(st.pid))
	}}
	wait2 := &Method{name: "wait2", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		st, ok := vm.waitForChild(args)
		if !ok {
			return object.NilV
		}
		return object.NewArray(object.IntValue(int64(st.pid)), vm.newProcessStatus(int64(st.pid), st.code))
	}}
	mod.smethods["wait"], mod.smethods["waitpid"] = wait, wait
	mod.smethods["wait2"], mod.smethods["waitpid2"] = wait2, wait2

	mod.smethods["waitall"] = &Method{name: "waitall", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 0 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
		}
		pairs := object.NewArray()
		for len(vm.children) > 0 {
			st, _ := vm.reapAny(-1)
			vm.globals["$?"] = vm.newProcessStatus(int64(st.pid), st.code)
			pairs.Elems = append(pairs.Elems,
				object.NewArray(object.IntValue(int64(st.pid)), vm.newProcessStatus(int64(st.pid), st.code)))
		}
		return pairs
	}}

	// Process.last_status is $? for the current thread; it takes no arguments.
	mod.smethods["last_status"] = &Method{name: "last_status", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 0 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
		}
		if st, ok := vm.globals["$?"]; ok {
			return st
		}
		return object.NilV
	}}

	// Process::Status.wait has the same signature as Process.wait but answers
	// with a Status rather than a pid, and — unlike Process.wait — reports "no
	// children" as a Status for pid -1 instead of raising.
	status.smethods["wait"] = &Method{name: "wait", owner: status, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		st, ok := vm.reapAny(waitPidArg(vm, args))
		if !ok {
			return vm.newProcessStatus(-1, 0)
		}
		vm.globals["$?"] = vm.newProcessStatus(int64(st.pid), st.code)
		return vm.globals["$?"]
	}}
}

// waitPidArg reads the optional pid argument of a wait-style call.
func waitPidArg(vm *VM, args []object.Value) int {
	if len(args) > 0 && !object.IsNil(args[0]) {
		return int(vm.procToInt(args[0]))
	}
	return -1
}
