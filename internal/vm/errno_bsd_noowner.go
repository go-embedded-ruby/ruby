// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build netbsd || dragonfly

package vm

import "syscall"

// The per-target half of errno.go's bucket rule for NetBSD and DragonFly, whose
// syscall packages have no ENOTRECOVERABLE or EOWNERDEAD. MRI's undefined_error
// makes both constants naming Errno::NOERROR there.

// errnoPlatformNumbers gives the numbers for the errnoPlatformNames these
// targets define.
var errnoPlatformNumbers = map[string]int64{
	"EMULTIHOP": int64(syscall.EMULTIHOP),
	"ENOLINK":   int64(syscall.ENOLINK),
	"ETXTBSY":   int64(syscall.ETXTBSY),
}

// errnoPlatformUndefined lists the errnoPlatformNames these targets have no
// number for.
var errnoPlatformUndefined = []string{"ENOTRECOVERABLE", "EOWNERDEAD"}
