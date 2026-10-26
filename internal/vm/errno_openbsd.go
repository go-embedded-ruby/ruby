// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build openbsd

package vm

import "syscall"

// The per-target half of errno.go's bucket rule for OpenBSD, whose <errno.h>
// (and so Go's syscall package) has no EMULTIHOP or ENOLINK. MRI's
// undefined_error makes both constants naming Errno::NOERROR there.

// errnoPlatformNumbers gives the numbers for the errnoPlatformNames OpenBSD
// defines.
var errnoPlatformNumbers = map[string]int64{
	"ENOTRECOVERABLE": int64(syscall.ENOTRECOVERABLE),
	"EOWNERDEAD":      int64(syscall.EOWNERDEAD),
	"ETXTBSY":         int64(syscall.ETXTBSY),
}

// errnoPlatformUndefined lists the errnoPlatformNames OpenBSD has no number for.
var errnoPlatformUndefined = []string{"EMULTIHOP", "ENOLINK"}
