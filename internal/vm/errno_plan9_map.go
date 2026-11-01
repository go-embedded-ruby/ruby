// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "strings"

// This file is the Plan 9 half of the Errno layer, written without a build tag on
// purpose: it is pure Go (strings and tables), so the mapping is unit-tested on
// every platform, while only errno_plan9.go (GOOS=plan9) consumes it.
//
// WHY IT EXISTS. Plan 9 has no errno numbers. Go's plan9 syscall package
// (src/syscall/errors_plan9.go) declares EINVAL, ENOENT, EACCES … as
// syscall.ErrorString values whose text IS the error (EINVAL is "bad arg in system
// call"), declares no E2BIG, EAGAIN, EBADF, ESRCH, ENOTEMPTY …, and its Errno type
// (syscall_plan9.go) is a bare uintptr that only prints "errno N". Every failing
// call returns a string, so a Ruby Errno class can only be chosen by reading the
// string.
//
// MRI has no such platform, but its rule for a name the platform lacks is the one
// followed here for the numbers (error.c, Init_syserr: undefined_error binds the
// name to Errno::NOERROR; set_syserr registers a class per number): every
// Errno::Exxx constant exists on every platform, and where the platform supplies
// no number Errno::Exxx::Errno takes the Linux value, which is what the table
// below carries. The wording of SystemCallError#message is the glibc strerror
// text for that Linux number. Neither the numbers nor the wording have an MRI
// witness on Plan 9 (MRI does not run there); they are the documented fallback.
//
// THE STRING RULES follow the Go standard library's own classification of these
// strings. syscall.ErrorString.Is (syscall_plan9.go:52) answers
// os.ErrPermission for "permission denied", os.ErrExist for "exists" and "is a
// directory", os.ErrNotExist for "does not exist", "not found", "has been
// removed" and "no parent", and errors.ErrUnsupported for "not supported"; the
// os package adds three messages of its own (os/error_plan9.go: "function not
// implemented", "out of range", "cannot allocate memory"). The exact texts of the
// syscall package's own variables are tried first, so the more specific class wins
// (EISDIR for "file is a directory", where the stdlib's substring rule would say
// "exists"); the substring rules then catch kernel and file-server messages that
// merely contain those phrases ("create: file already exists", "fd: file does not
// exist"). Anything else is unknown: the caller raises a plain SystemCallError
// carrying the message.

// errnoLinuxEntry is one row of the Linux errno table.
type errnoLinuxEntry struct {
	name string
	num  int64
	text string
}

// errnoLinuxTable is the Linux (asm-generic/errno-base.h and errno.h) number and
// the glibc strerror text of every Errno name a Plan 9 error string can map to,
// plus the rest of the names MRI's known_errors list defines on Linux that Go's
// portable syscall package also defines on every other target (the
// errnoUniversalNumbers set). EAGAIN and EWOULDBLOCK, and ENOTSUP and
// EOPNOTSUPP, are the same number, as on Linux.
var errnoLinuxTable = []errnoLinuxEntry{
	{"NOERROR", 0, "Success"},
	{"EPERM", 1, "Operation not permitted"},
	{"ENOENT", 2, "No such file or directory"},
	{"ESRCH", 3, "No such process"},
	{"EINTR", 4, "Interrupted system call"},
	{"EIO", 5, "Input/output error"},
	{"ENXIO", 6, "No such device or address"},
	{"E2BIG", 7, "Argument list too long"},
	{"ENOEXEC", 8, "Exec format error"},
	{"EBADF", 9, "Bad file descriptor"},
	{"ECHILD", 10, "No child processes"},
	{"EAGAIN", 11, "Resource temporarily unavailable"},
	{"ENOMEM", 12, "Cannot allocate memory"},
	{"EACCES", 13, "Permission denied"},
	{"EFAULT", 14, "Bad address"},
	{"EBUSY", 16, "Device or resource busy"},
	{"EEXIST", 17, "File exists"},
	{"EXDEV", 18, "Invalid cross-device link"},
	{"ENODEV", 19, "No such device"},
	{"ENOTDIR", 20, "Not a directory"},
	{"EISDIR", 21, "Is a directory"},
	{"EINVAL", 22, "Invalid argument"},
	{"ENFILE", 23, "Too many open files in system"},
	{"EMFILE", 24, "Too many open files"},
	{"ENOTTY", 25, "Inappropriate ioctl for device"},
	{"ETXTBSY", 26, "Text file busy"},
	{"EFBIG", 27, "File too large"},
	{"ENOSPC", 28, "No space left on device"},
	{"ESPIPE", 29, "Illegal seek"},
	{"EROFS", 30, "Read-only file system"},
	{"EMLINK", 31, "Too many links"},
	{"EPIPE", 32, "Broken pipe"},
	{"EDOM", 33, "Numerical argument out of domain"},
	{"ERANGE", 34, "Numerical result out of range"},
	{"EDEADLK", 35, "Resource deadlock avoided"},
	{"ENAMETOOLONG", 36, "File name too long"},
	{"ENOLCK", 37, "No locks available"},
	{"ENOSYS", 38, "Function not implemented"},
	{"ENOTEMPTY", 39, "Directory not empty"},
	{"ELOOP", 40, "Too many levels of symbolic links"},
	{"ENOMSG", 42, "No message of desired type"},
	{"EIDRM", 43, "Identifier removed"},
	{"ENOLINK", 67, "Link has been severed"},
	{"EPROTO", 71, "Protocol error"},
	{"EMULTIHOP", 72, "Multihop attempted"},
	{"EBADMSG", 74, "Bad message"},
	{"EOVERFLOW", 75, "Value too large for defined data type"},
	{"EILSEQ", 84, "Invalid or incomplete multibyte or wide character"},
	{"ENOTSOCK", 88, "Socket operation on non-socket"},
	{"EDESTADDRREQ", 89, "Destination address required"},
	{"EMSGSIZE", 90, "Message too long"},
	{"EPROTOTYPE", 91, "Protocol wrong type for socket"},
	{"ENOPROTOOPT", 92, "Protocol not available"},
	{"EPROTONOSUPPORT", 93, "Protocol not supported"},
	{"EOPNOTSUPP", 95, "Operation not supported"},
	{"ENOTSUP", 95, "Operation not supported"},
	{"EAFNOSUPPORT", 97, "Address family not supported by protocol"},
	{"EADDRINUSE", 98, "Address already in use"},
	{"EADDRNOTAVAIL", 99, "Cannot assign requested address"},
	{"ENETDOWN", 100, "Network is down"},
	{"ENETUNREACH", 101, "Network is unreachable"},
	{"ENETRESET", 102, "Network dropped connection on reset"},
	{"ECONNABORTED", 103, "Software caused connection abort"},
	{"ECONNRESET", 104, "Connection reset by peer"},
	{"ENOBUFS", 105, "No buffer space available"},
	{"EISCONN", 106, "Transport endpoint is already connected"},
	{"ENOTCONN", 107, "Transport endpoint is not connected"},
	{"ETIMEDOUT", 110, "Connection timed out"},
	{"ECONNREFUSED", 111, "Connection refused"},
	{"EHOSTUNREACH", 113, "No route to host"},
	{"EALREADY", 114, "Operation already in progress"},
	{"EINPROGRESS", 115, "Operation now in progress"},
	{"ESTALE", 116, "Stale file handle"},
	{"EDQUOT", 122, "Disk quota exceeded"},
	{"ECANCELED", 125, "Operation canceled"},
	{"EOWNERDEAD", 130, "Owner died"},
	{"ENOTRECOVERABLE", 131, "State not recoverable"},
}

// errnoLinuxPlatformNames are the errnoPlatformNames (errno.go) the Linux table
// carries; on Plan 9 they take their Linux numbers instead of being undefined.
var errnoLinuxPlatformNames = map[string]bool{
	"EMULTIHOP": true, "ENOLINK": true, "ENOTRECOVERABLE": true,
	"EOWNERDEAD": true, "ETXTBSY": true,
}

// errnoLinuxUniversal returns the Linux numbers of every table row that is not a
// per-target name: the Plan 9 value of errnoUniversalNumbers.
func errnoLinuxUniversal() map[string]int64 {
	m := make(map[string]int64, len(errnoLinuxTable))
	for _, e := range errnoLinuxTable {
		if !errnoLinuxPlatformNames[e.name] {
			m[e.name] = e.num
		}
	}
	return m
}

// errnoLinuxPlatform returns the Linux numbers of the per-target names.
func errnoLinuxPlatform() map[string]int64 {
	m := make(map[string]int64, len(errnoLinuxPlatformNames))
	for _, e := range errnoLinuxTable {
		if errnoLinuxPlatformNames[e.name] {
			m[e.name] = e.num
		}
	}
	return m
}

// errnoLinuxText returns the glibc strerror text of Linux errno number n. The
// first row for a number wins, so 95 reads "Operation not supported" for both of
// its names.
func errnoLinuxText(n int64) (string, bool) {
	for _, e := range errnoLinuxTable {
		if e.num == n {
			return e.text, true
		}
	}
	return "", false
}

// plan9ExactErrors are the texts of the error variables Go's plan9 syscall
// package declares (src/syscall/errors_plan9.go) and of the three the os package
// adds (src/os/error_plan9.go), each with the Errno class of the same name.
// EPLAN9, "not supported by plan 9", has no POSIX namesake; the stdlib classifies
// it as errors.ErrUnsupported, so it takes ENOTSUP.
var plan9ExactErrors = map[string]string{
	"bad arg in system call":                   "EINVAL",
	"not a directory":                          "ENOTDIR",
	"file is a directory":                      "EISDIR",
	"file does not exist":                      "ENOENT",
	"file already exists":                      "EEXIST",
	"no free file descriptors":                 "EMFILE",
	"i/o error":                                "EIO",
	"file name too long":                       "ENAMETOOLONG",
	"interrupted":                              "EINTR",
	"permission denied":                        "EPERM",
	"no free devices":                          "EBUSY",
	"connection timed out":                     "ETIMEDOUT",
	"not supported by plan 9":                  "ENOTSUP",
	"access permission denied":                 "EACCES",
	"address family not supported by protocol": "EAFNOSUPPORT",
	"illegal seek":                             "ESPIPE",
	"function not implemented":                 "ENOSYS",
	"out of range":                             "ERANGE",
	"cannot allocate memory":                   "ENOMEM",

	// plan9ESRCHText is the text of the one sentinel rbgo invents: Plan 9 has no
	// ESRCH, so Process.kill on a pid with no /proc entry reports this error
	// (spawn_plan9.go) and it reads back as Errno::ESRCH.
	plan9ESRCHText: "ESRCH",
}

// plan9ESRCHText is the message of spawn_plan9.go's "no such process" error.
const plan9ESRCHText = "no such process"

// plan9ContainsRules are the substring classifications of syscall.ErrorString.Is,
// in the order the stdlib tests them (permission, exist, not-exist, unsupported).
// The order cannot misfire: no not-exist phrase contains "exists". A message that
// matches none of the exact texts or these phrases is unknown.
var plan9ContainsRules = []struct {
	phrases []string
	name    string
}{
	{[]string{"permission denied"}, "EACCES"},
	{[]string{"exists", "is a directory"}, "EEXIST"},
	{[]string{"does not exist", "not found", "has been removed", "no parent"}, "ENOENT"},
	{[]string{"not supported"}, "ENOTSUP"},
}

// plan9ErrnoName maps a Plan 9 error string to the Errno name MRI would raise for
// the equivalent POSIX failure, or "" and false for a string the stdlib does not
// classify. The text is trimmed first: Plan 9 file servers return
// "create: file already exists"-style prefixed messages, and wrapped errors end
// in newlines.
func plan9ErrnoName(msg string) (string, bool) {
	msg = strings.TrimSpace(msg)
	if name, ok := plan9ExactErrors[msg]; ok {
		return name, true
	}
	for _, r := range plan9ContainsRules {
		for _, p := range r.phrases {
			if strings.Contains(msg, p) {
				return r.name, true
			}
		}
	}
	return "", false
}
