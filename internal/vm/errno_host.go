// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !plan9

package vm

import (
	"errors"
	"syscall"
)

// errnoOfError returns the errno number a failed call's error carries: the
// syscall.Errno errors.As digs out of the wrapping (*os.PathError,
// *os.SyscallError, *net.OpError …). ok is false for an error with no errno
// behind it. The Plan 9 counterpart (errno_plan9.go) has no numbers to dig out and
// derives one from the error string instead.
//
// The number is the one of the class MRI raises, which on Windows is not the
// code the call failed with: a Windows syscall.Errno is the Win32 code itself,
// and MRI maps it through rb_w32_map_errno before anything raises it, so
// ERROR_PATH_NOT_FOUND (3) is Errno::ENOENT. mapHostErrno does that mapping
// there and is the identity elsewhere. Doing it here, once, means no raiser can
// look a raw Win32 code up in errnoClasses (go-embedded-ruby/ruby#980).
//
// ⚠ Windows: Go defines syscall.ENOENT as ERROR_FILE_NOT_FOUND (2) and
// syscall.ENOTDIR as ERROR_PATH_NOT_FOUND (3), so an ENOTDIR that Go builds
// itself (os.MkdirAll through a file, os.ReadDir on one, filepath.EvalSymlinks)
// is the very number Windows uses for a missing path, and is mapped to ENOENT
// as MRI maps that code. No raiser here takes its error from those three
// today; one that does must decide the class itself.
func errnoOfError(err error) (int64, bool) {
	var eno syscall.Errno
	if errors.As(err, &eno) {
		return mapHostErrno(int64(eno)), true
	}
	return 0, false
}

// errIsErrno reports whether err is the Errno class `name` (an errnoNumbers key):
// whether errnoOfError maps it to that class's number. Off Windows that is
// errors.Is(err, syscall.<name>) for the first errno in the chain; on Windows
// it also sees that ERROR_DIR_NOT_EMPTY is ENOTEMPTY, which errors.Is cannot.
// It exists so the call sites compile on Plan 9, whose syscall package lacks
// most of those constants.
func errIsErrno(err error, name string) bool {
	eno, ok := errnoOfError(err)
	return ok && eno == errnoNumbers[name]
}

// errnoText is the message sysFail embeds for an errno: errnoMessage of the host's
// syscall.Errno, i.e. Go's own text with its first letter restored.
func errnoText(eno int64) string { return errnoMessage(syscall.Errno(eno)) }

// errnoGoText is the text errnoStrerror starts from: Go's own, from
// syscall.Errno.Error(). Plan 9 supplies glibc's instead (errno_plan9.go).
func errnoGoText(n int64) string { return syscall.Errno(n).Error() }

// errnoUniversalNumbers maps an Errno::Exxx name to its errno number for the
// names EVERY GOOS rbgo builds for (except plan9, see errno_plan9.go) defines. The
// values come from the host's syscall table rather than being fixed here, because
// they genuinely differ across platforms (EAGAIN is 11 on Linux and 35 on the
// BSDs/macOS) and Errno::EAGAIN::Errno must match the host MRI's.
//
// "Universal" is measured, not assumed: a probe compiled this set for
// darwin/{amd64,arm64}, linux/{amd64,386,arm64,riscv64,loong64,ppc64le,s390x},
// windows/{amd64,arm64}, wasip1/wasm and js/wasm, and later for every other
// GOOS Go supports except plan9 (whose syscall package has no errno numbers). The
// names some of those lack (ENOTRECOVERABLE, EOWNERDEAD and ETXTBSY on js/wasm,
// EMULTIHOP and ENOLINK on OpenBSD, ENOTRECOVERABLE and EOWNERDEAD on NetBSD
// and DragonFly) are NOT here — they live in the per-target files, which is what
// issue #682 was about.
//
// The names outside this set are in errnoUndefinedNames, where rbgo reports 0.
// That is a DECLARED conformance gap, not an absence: the same probe found that
// Go's portable syscall package does define many of them per-GOOS with no build
// tag at all (57 of the 81 on linux/amd64, 56 on windows, 31 on darwin/amd64, 2
// on wasip1 — macOS EAUTH at 80 is only the best-known one). Closing it needs a
// per-platform MRI witness for each number, which is the open per-platform errno
// issue and deliberately not done here.
var errnoUniversalNumbers = map[string]int64{
	"E2BIG":           int64(syscall.E2BIG),
	"EACCES":          int64(syscall.EACCES),
	"EADDRINUSE":      int64(syscall.EADDRINUSE),
	"EADDRNOTAVAIL":   int64(syscall.EADDRNOTAVAIL),
	"EAFNOSUPPORT":    int64(syscall.EAFNOSUPPORT),
	"EAGAIN":          int64(syscall.EAGAIN),
	"EALREADY":        int64(syscall.EALREADY),
	"EBADF":           int64(syscall.EBADF),
	"EBADMSG":         int64(syscall.EBADMSG),
	"EBUSY":           int64(syscall.EBUSY),
	"ECANCELED":       int64(syscall.ECANCELED),
	"ECHILD":          int64(syscall.ECHILD),
	"ECONNABORTED":    int64(syscall.ECONNABORTED),
	"ECONNREFUSED":    int64(syscall.ECONNREFUSED),
	"ECONNRESET":      int64(syscall.ECONNRESET),
	"EDEADLK":         int64(syscall.EDEADLK),
	"EDESTADDRREQ":    int64(syscall.EDESTADDRREQ),
	"EDOM":            int64(syscall.EDOM),
	"EDQUOT":          int64(syscall.EDQUOT),
	"EEXIST":          int64(syscall.EEXIST),
	"EFAULT":          int64(syscall.EFAULT),
	"EFBIG":           int64(syscall.EFBIG),
	"EHOSTUNREACH":    int64(syscall.EHOSTUNREACH),
	"EIDRM":           int64(syscall.EIDRM),
	"EILSEQ":          int64(syscall.EILSEQ),
	"EINPROGRESS":     int64(syscall.EINPROGRESS),
	"EINTR":           int64(syscall.EINTR),
	"EINVAL":          int64(syscall.EINVAL),
	"EIO":             int64(syscall.EIO),
	"EISCONN":         int64(syscall.EISCONN),
	"EISDIR":          int64(syscall.EISDIR),
	"ELOOP":           int64(syscall.ELOOP),
	"EMFILE":          int64(syscall.EMFILE),
	"EMLINK":          int64(syscall.EMLINK),
	"EMSGSIZE":        int64(syscall.EMSGSIZE),
	"ENAMETOOLONG":    int64(syscall.ENAMETOOLONG),
	"ENETDOWN":        int64(syscall.ENETDOWN),
	"ENETRESET":       int64(syscall.ENETRESET),
	"ENETUNREACH":     int64(syscall.ENETUNREACH),
	"ENFILE":          int64(syscall.ENFILE),
	"ENOBUFS":         int64(syscall.ENOBUFS),
	"ENODEV":          int64(syscall.ENODEV),
	"ENOENT":          int64(syscall.ENOENT),
	"ENOEXEC":         int64(syscall.ENOEXEC),
	"ENOLCK":          int64(syscall.ENOLCK),
	"ENOMEM":          int64(syscall.ENOMEM),
	"ENOMSG":          int64(syscall.ENOMSG),
	"ENOPROTOOPT":     int64(syscall.ENOPROTOOPT),
	"ENOSPC":          int64(syscall.ENOSPC),
	"ENOSYS":          int64(syscall.ENOSYS),
	"ENOTCONN":        int64(syscall.ENOTCONN),
	"ENOTDIR":         int64(syscall.ENOTDIR),
	"ENOTEMPTY":       int64(syscall.ENOTEMPTY),
	"ENOTSOCK":        int64(syscall.ENOTSOCK),
	"ENOTSUP":         int64(syscall.ENOTSUP),
	"ENOTTY":          int64(syscall.ENOTTY),
	"ENXIO":           int64(syscall.ENXIO),
	"EOPNOTSUPP":      int64(syscall.EOPNOTSUPP),
	"EOVERFLOW":       int64(syscall.EOVERFLOW),
	"EPERM":           int64(syscall.EPERM),
	"EPIPE":           int64(syscall.EPIPE),
	"EPROTO":          int64(syscall.EPROTO),
	"EPROTONOSUPPORT": int64(syscall.EPROTONOSUPPORT),
	"EPROTOTYPE":      int64(syscall.EPROTOTYPE),
	"ERANGE":          int64(syscall.ERANGE),
	"EROFS":           int64(syscall.EROFS),
	"ESPIPE":          int64(syscall.ESPIPE),
	"ESRCH":           int64(syscall.ESRCH),
	"ESTALE":          int64(syscall.ESTALE),
	"ETIMEDOUT":       int64(syscall.ETIMEDOUT),
	"EXDEV":           int64(syscall.EXDEV),

	// NOERROR is errno 0. MRI registers it first, so every name the platform does
	// not define (errnoUndefinedNames) binds to this very class.
	"NOERROR": 0,
}
