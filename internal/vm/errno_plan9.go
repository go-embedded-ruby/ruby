// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build plan9

package vm

import (
	"errors"
	"syscall"
)

// This is the GOOS=plan9 half of the errno bucket rule stated in errno.go. Plan 9
// has no errno numbers (see errno_plan9_map.go for the whole account), so every
// bucket-1 number is the Linux one, and the five per-target names (EMULTIHOP,
// ENOLINK, ENOTRECOVERABLE, EOWNERDEAD, ETXTBSY) take their Linux numbers too
// rather than becoming undefined: MRI defines every name on every platform, and a
// Plan 9 program has no reason to see fewer classes than a Linux one.

// errnoUniversalNumbers is the Linux number table (errno_plan9_map.go).
var errnoUniversalNumbers = errnoLinuxUniversal()

// errnoPlatformNumbers gives the per-target names their Linux numbers.
var errnoPlatformNumbers = errnoLinuxPlatform()

// errnoPlatformUndefined is empty: every per-target name has a number here.
var errnoPlatformUndefined []string

// errnoGoText is the text errnoStrerror starts from: the glibc wording of the
// Linux number, since syscall.Errno.Error() prints only "errno N" on Plan 9. A
// number the table does not name keeps that "errno N" form, which errnoStrerror
// replaces with the C library's "Unknown error: N".
func errnoGoText(n int64) string {
	if s, ok := errnoLinuxText(n); ok {
		return s
	}
	return syscall.Errno(n).Error()
}

// errnoText is the message sysFail embeds for an errno: the strerror text, since
// syscall.Errno.Error() prints only "errno N" here.
func errnoText(eno int64) string { return errnoStrerror(eno) }

// errIsErrno reports whether err is the Errno class `name`, by the same string
// classification errnoOfError uses.
func errIsErrno(err error, name string) bool {
	n, ok := errnoOfError(err)
	return ok && n == errnoNumbers[name]
}

// errnoOfError returns the Errno number a failed call's error stands for: the
// number of the class plan9ErrnoName chooses for its text. ok is false for an
// error the Go standard library's rules do not classify, which the caller raises
// as a plain SystemCallError carrying the message.
func errnoOfError(err error) (int64, bool) {
	for u := errors.Unwrap(err); u != nil; u = errors.Unwrap(err) {
		err = u
	}
	if err == nil {
		return 0, false
	}
	name, ok := plan9ErrnoName(err.Error())
	if !ok {
		return 0, false
	}
	n, ok := errnoNumbers[name]
	return n, ok
}
