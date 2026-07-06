// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// This is the Windows counterpart to socket_unix.go: AF_UNIX stream sockets are
// unsupported / unreliable on Windows, so rather than compile the net-"unix"
// code path there, the UNIXSocket / UNIXServer classes still exist (so scripts
// that merely reference the constants load) but their constructors raise a
// clean, rescuable NotImplementedError instead of panicking. The ancestry
// (UNIXSocket < BasicSocket, UNIXServer < UNIXSocket) matches the real build.

// registerUnixSockets installs UNIXSocket / UNIXServer stubs on Windows whose
// .new raises NotImplementedError. It has the same signature as the non-Windows
// implementation in socket_unix.go so registerSocket calls it uniformly.
func (vm *VM) registerUnixSockets(basic *RClass) {
	unsupported := func(name string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "%s (AF_UNIX) is not supported on Windows", name)
		}
	}
	sock := newClass("UNIXSocket", basic)
	vm.consts["UNIXSocket"] = sock
	srv := newClass("UNIXServer", sock)
	vm.consts["UNIXServer"] = srv
	sock.smethods["new"] = &Method{name: "new", owner: sock, native: unsupported("UNIXSocket")}
	sock.smethods["open"] = &Method{name: "open", owner: sock, native: unsupported("UNIXSocket")}
	srv.smethods["new"] = &Method{name: "new", owner: srv, native: unsupported("UNIXServer")}
	srv.smethods["open"] = &Method{name: "open", owner: srv, native: unsupported("UNIXServer")}
}
