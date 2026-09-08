// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build wasm

package vm

import (
	"errors"
	"io/fs"
	"syscall"
)

// chrootFn / lutimesFn are unsupported under GOARCH=wasm: Dir.chroot and
// File.lutime are both guarded `platform_is_not :windows` in the specs and have
// no wasip1/js system call, so these stubs exist only to satisfy the seam the
// shared dir.go / file.go code references.
var (
	chrootFn  = func(string) error { return errors.ErrUnsupported }
	lutimesFn = func(string, int64, int64) error { return errors.ErrUnsupported }
)

// statSys is the GOARCH=wasm counterpart of the Unix syscall.Stat_t extraction.
// Under wasip1 the os package still populates FileInfo.Sys() with a
// *syscall.Stat_t, but that struct is a WASI-preview1 subset: it carries
// Dev/Ino/Nlink and Uid/Gid (the latter always zero on wasip1) yet has no
// Blksize field, so File::Stat#blksize degrades to 0 (matching MRI on platforms
// that cannot report a block size). Under GOOS=js the Sys() value is not a
// *syscall.Stat_t, so the not-ok fallback returns the same defaults as Windows.
func statSys(fi fs.FileInfo) statFields {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return statFields{nlink: 1}
	}
	return statFields{
		uid:    int64(st.Uid),
		gid:    int64(st.Gid),
		ino:    int64(st.Ino),
		dev:    int64(st.Dev),
		nlink:  int64(st.Nlink),
		hasSys: true,
	}
}
