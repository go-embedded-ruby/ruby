// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// This is the Windows counterpart to socket_addrinfo_unix.go: AF_UNIX is
// unsupported on Windows (matching the UNIXSocket transport stub in
// socket_unix_windows.go), so Socket.pack_sockaddr_un / .unpack_sockaddr_un exist
// but raise a clean, rescuable NotImplementedError rather than compiling the
// AF_UNIX byte layout there.

// registerSockaddrUn installs Socket.pack_sockaddr_un / .unpack_sockaddr_un stubs
// on Windows whose bodies raise NotImplementedError. It has the same signature as
// the non-Windows implementation so registerSocketAddr calls it uniformly.
func registerSockaddrUn(sock *RClass) {
	unsupported := func(name string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "%s (AF_UNIX) is not supported on Windows", name)
		}
	}
	for _, name := range []string{"pack_sockaddr_un", "sockaddr_un", "unpack_sockaddr_un"} {
		sock.smethods[name] = &Method{name: name, owner: sock, native: unsupported("Socket." + name)}
	}
}
