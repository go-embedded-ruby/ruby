// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import (
	"bytes"
	binpkg "encoding/binary"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This is the non-Windows half of the sockaddr utilities: the AF_UNIX
// pack_sockaddr_un / unpack_sockaddr_un helpers. AF_UNIX is unsupported on
// Windows (matching the UNIXSocket transport in socket_unix.go), so these are
// registered only here; the Windows build supplies NotImplementedError stubs
// (socket_addrinfo_windows.go).

// sunPathLen is the sun_path capacity of a Linux sockaddr_un (108 bytes); the
// packed form is the 2-byte family followed by the NUL-terminated path padded to
// this length.
const sunPathLen = 108

// registerSockaddrUn installs Socket.pack_sockaddr_un / .unpack_sockaddr_un on
// non-Windows platforms.
func registerSockaddrUn(sock *RClass) {
	// Socket.pack_sockaddr_un(path) / Socket.sockaddr_un(path) → the packed
	// sockaddr_un bytes (family + NUL-terminated path, sun_path-padded).
	packUn := &Method{name: "pack_sockaddr_un", owner: sock,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 1 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
			}
			path := strArg(args[0])
			if len(path) >= sunPathLen {
				raise("ArgumentError", "too long unix socket path (%d bytes given but %d bytes max)", len(path), sunPathLen-1)
			}
			buf := make([]byte, 2+sunPathLen)
			binpkg.NativeEndian.PutUint16(buf[0:2], uint16(afUNIX))
			copy(buf[2:], path)
			return object.NewStringBytesEnc(buf, "ASCII-8BIT")
		}}
	sock.smethods["pack_sockaddr_un"] = packUn
	sock.smethods["sockaddr_un"] = &Method{name: "sockaddr_un", owner: sock, native: packUn.native}

	// Socket.unpack_sockaddr_un(sockaddr) → the path (bytes up to the first NUL).
	sock.smethods["unpack_sockaddr_un"] = &Method{name: "unpack_sockaddr_un", owner: sock,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 1 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
			}
			sa := sockaddrBytes(args[0])
			if len(sa) < 2 {
				raise("ArgumentError", "not an AF_UNIX sockaddr")
			}
			path := sa[2:]
			if i := bytes.IndexByte(path, 0); i >= 0 {
				path = path[:i]
			}
			return object.NewString(string(path))
		}}
}
