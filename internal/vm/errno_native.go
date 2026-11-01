// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !(js && wasm) && !openbsd && !netbsd && !dragonfly && !plan9

package vm

import "syscall"

// This is the per-target half of the errno bucket rule stated in errno.go, for
// every target EXCEPT js/wasm and the BSDs below: darwin, linux, windows,
// wasip1, freebsd, solaris, illumos and aix all define the five
// errnoPlatformNames, so all five take their real numbers here and none of them
// is an undefined_error name.
//
// The tag is spelled !(js && wasm) to match the file pair this package already
// uses for the js-only split (jsbridge_native.go, servergems_native.go), and
// because Go pairs GOOS=js with GOARCH=wasm only. It is NOT the plain !wasm tag
// the gem backends use: wasip1 belongs on this side.

// errnoPlatformNumbers gives the numbers for the errnoPlatformNames this target
// defines — all of them here.
var errnoPlatformNumbers = map[string]int64{
	"EMULTIHOP":       int64(syscall.EMULTIHOP),
	"ENOLINK":         int64(syscall.ENOLINK),
	"ENOTRECOVERABLE": int64(syscall.ENOTRECOVERABLE),
	"EOWNERDEAD":      int64(syscall.EOWNERDEAD),
	"ETXTBSY":         int64(syscall.ETXTBSY),
}

// errnoPlatformUndefined lists the errnoPlatformNames this target has NO number
// for. Empty here, which is why the whole mechanism was invisible until js/wasm
// stopped compiling.
var errnoPlatformUndefined []string
