// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strconv"
	"strings"
	"syscall"
)

// This file holds the Errno name table behind Ruby's Errno::Exxx classes and the
// strerror text SystemCallError#message reports.
//
// MRI builds the table in Init_syserr (ruby/ruby v3_4_0 error.c:4204):
//
//	rb_eNOERROR = setup_syserr(0, "NOERROR");
//	#define defined_error(name, num) set_syserr((num), (name));
//	#define undefined_error(name) rb_define_const(rb_mErrno, (name), rb_eNOERROR);
//	#include "known_errors.inc"
//
// Two rules follow from set_syserr (error.c:3064), and rbgo reproduces both:
//
//   - One CLASS PER ERRNO NUMBER, not per name. A name whose number already has a
//     class becomes a second constant naming that same class, which is why
//     Errno::EWOULDBLOCK.equal?(Errno::EAGAIN) is true wherever the two numbers
//     agree, and why the class keeps the name that was registered first.
//   - A name the platform's <errno.h> does not define is still a CONSTANT: MRI's
//     undefined_error binds it to Errno::NOERROR (errno 0). So Errno.constants is
//     the whole known_errors list on every platform — 158 entries — and only the
//     numbers differ.

// THE BUCKET RULE, stated once, here. Every name of MRI's known_errors list is in
// exactly one of three buckets, and which bucket a name is in can depend on the
// GOOS being built for:
//
//  1. errnoNumbers — a number from this target's syscall package.
//  2. errnoAliases — a second spelling of a number bucket 1 already has.
//  3. errnoUndefinedNames — no number on this target, so MRI's undefined_error
//     treatment: still a constant, naming Errno::NOERROR (errno 0).
//
// The buckets' union is errnoKnownNameCount names on EVERY target: a name that
// leaves bucket 1 because this GOOS lacks it MUST arrive in bucket 3, or
// Errno.constants silently shrinks. errnoNumbers and errnoUndefinedNames are
// therefore assembled from a universal part plus a per-target part
// (errno_native.go / errno_wasm.go), and TestErrnoBucketsPartitionKnownNames
// checks the partition on whichever target it runs on.

// errnoUniversalNumbers maps an Errno::Exxx name to its errno number for the
// names EVERY GOOS rbgo builds for defines. The values come from the host's
// syscall table rather than being fixed here, because they genuinely differ
// across platforms (EAGAIN is 11 on Linux and 35 on the BSDs/macOS) and
// Errno::EAGAIN::Errno must match the host MRI's.
//
// "Universal" is measured, not assumed: a probe compiled this set for
// darwin/{amd64,arm64}, linux/{amd64,386,arm64,riscv64,loong64,ppc64le,s390x},
// windows/{amd64,arm64}, wasip1/wasm and js/wasm. The names js/wasm alone lacks
// (ENOTRECOVERABLE, EOWNERDEAD, ETXTBSY) are NOT here — they live in the
// per-target files, which is what issue #682 was about.
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
	"EMULTIHOP":       int64(syscall.EMULTIHOP),
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
	"ENOLINK":         int64(syscall.ENOLINK),
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

// errnoPlatformNames are the MRI known_errors names Go's portable syscall package
// defines on SOME of rbgo's targets and not others — bucket 1 on one GOOS and
// bucket 3 on another. js/wasm is the whole reason this bucket exists: Go's
// js/wasm syscall package has no ENOTRECOVERABLE, EOWNERDEAD or ETXTBSY, while
// darwin, linux, windows and wasip1 all define the three (measured, see
// errnoUniversalNumbers).
//
// This list is the canonical statement of WHICH names are per-target. The numbers
// (or their absence) come from errnoPlatformNumbers / errnoPlatformUndefined in
// errno_native.go and errno_wasm.go, and the partition test asserts those two
// exactly cover this list with no overlap — so a future name added to one file
// and forgotten in the other is a test failure, not a silently shorter
// Errno.constants.
var errnoPlatformNames = []string{"ENOTRECOVERABLE", "EOWNERDEAD", "ETXTBSY"}

// errnoKnownNameCount is the size of MRI's known_errors list (ruby/ruby v3_4_0
// known_errors.inc), which Errno.constants must have on EVERY target. It is
// pinned as a literal on purpose: comparing the registered constants against the
// tables' own lengths — as TestErrnoClassRegistration does — cannot see a name
// that fell out of every bucket, because the expectation shrinks with the
// subject. This number does not.
const errnoKnownNameCount = 158

// errnoNumbers maps an Errno::Exxx name to this target's errno number: bucket 1
// of the rule above, assembled from the universal set plus whatever of
// errnoPlatformNames this GOOS defines.
var errnoNumbers = mergeErrnoNumbers(errnoUniversalNumbers, errnoPlatformNumbers)

// mergeErrnoNumbers returns the union of the two number tables. It takes them as
// arguments rather than reading the package vars so the merge is exercised
// directly by a test on every lane, including the js-shaped case (an empty
// platform table) that no POSIX lane builds.
func mergeErrnoNumbers(universal, platform map[string]int64) map[string]int64 {
	m := make(map[string]int64, len(universal)+len(platform))
	for name, n := range universal {
		m[name] = n
	}
	for name, n := range platform {
		m[name] = n
	}
	return m
}

// errnoUndefinedNames are the Errno constants of MRI's known_errors list that
// have no number on THIS target: bucket 3 of the rule above. They are registered
// the way MRI's undefined_error does it — as extra constants naming Errno::NOERROR
// — so Errno.constants keeps MRI's full errnoKnownNameCount entries, which
// core/exception/errno_spec.rb and system_call_error_spec.rb both lean on (the
// latter takes Errno.constants.size as an errno number no class can claim).
//
// It is the names undefined on every target plus the per-target remainder, which
// is empty everywhere except js/wasm.
var errnoUndefinedNames = append(append([]string{}, errnoUndefinedEverywhere...), errnoPlatformUndefined...)

// errnoUndefinedEverywhere are the known_errors names rbgo has no number for on
// any target it builds for. See errnoUniversalNumbers on why some of these are a
// declared conformance gap rather than a genuine absence.
var errnoUndefinedEverywhere = []string{
	"EADV", "EAUTH", "EBADARCH", "EBADE",
	"EBADEXEC", "EBADFD", "EBADMACHO", "EBADR",
	"EBADRPC", "EBADRQC", "EBADSLT", "EBFONT",
	"ECAPMODE", "ECHRNG", "ECOMM", "EDEADLOCK",
	"EDEVERR", "EDOOFUS", "EDOTDOT", "EFTYPE",
	"EHOSTDOWN", "EHWPOISON", "EIPSEC", "EISNAM",
	"EKEYEXPIRED", "EKEYREJECTED", "EKEYREVOKED", "EL2HLT",
	"EL2NSYNC", "EL3HLT", "EL3RST", "ELAST",
	"ELIBACC", "ELIBBAD", "ELIBEXEC", "ELIBMAX",
	"ELIBSCN", "ELNRNG", "EMEDIUMTYPE", "ENAVAIL",
	"ENEEDAUTH", "ENOANO", "ENOATTR", "ENOCSI",
	"ENODATA", "ENOKEY", "ENOMEDIUM", "ENONET",
	"ENOPKG", "ENOPOLICY", "ENOSR", "ENOSTR",
	"ENOTBLK", "ENOTCAPABLE", "ENOTNAM", "ENOTUNIQ",
	"EPFNOSUPPORT", "EPROCLIM", "EPROCUNAVAIL", "EPROGMISMATCH",
	"EPROGUNAVAIL", "EPWROFF", "EQFULL", "EREMCHG",
	"EREMOTE", "EREMOTEIO", "ERESTART", "ERFKILL",
	"ERPCMISMATCH", "ESHLIBVERS", "ESHUTDOWN", "ESOCKTNOSUPPORT",
	"ESRMNT", "ESTRPIPE", "ETIME", "ETOOMANYREFS",
	"EUCLEAN", "EUNATCH", "EUSERS", "EXFULL",
}

// errnoAliases are names Go's portable syscall package does not expose that are
// nevertheless NOT undefined: the platform defines them as a spelling of another
// errno. POSIX explicitly permits EWOULDBLOCK and EAGAIN to be the same value,
// and every platform rbgo builds for makes them so — darwin and the BSDs at 35,
// Linux at 11, Winsock's WSAEWOULDBLOCK at Go's syscall.EWOULDBLOCK, and WASI,
// whose errno enum has no separate EWOULDBLOCK at all. Registering EWOULDBLOCK
// as a second name for EAGAIN's number reproduces what MRI's set_syserr does
// with them, which core/exception/errno_spec.rb asserts directly.
var errnoAliases = map[string]string{"EWOULDBLOCK": "EAGAIN"}

// errnoStrerror returns the message MRI's syserr_initialize builds from
// strerror(errno) (ruby/ruby v3_4_0 error.c:3128). Go's syscall.Errno.Error()
// carries the same table with a lower-case first letter ("invalid argument"), so
// the leading letter is restored; a number the table does not name comes back in
// Go's "errno N" placeholder form, which is replaced by the one the C library
// produces. Witnessed against MRI 4.0.5 on darwin: 22 -> "Invalid argument",
// 0 -> "Undefined error: 0", 2**28 -> "Unknown error: 268435456".
//
// The placeholder wording is darwin's. glibc words the same two "Unknown error
// 268435456" (no colon) and "Success" for errno 0, so rbgo reports darwin's
// phrasing on Linux; no spec asserts the text (ruby/spec only requires it to be
// [[:graph:]]+), and settling it properly needs a Linux MRI witness — see the
// per-platform errno issue.
func errnoStrerror(n int64) string {
	if s, ok := errnoUnknownText(syscall.Errno(n).Error(), n); ok {
		return s
	}
	return capitalizeASCII(syscall.Errno(n).Error())
}

// errnoUnknownText recognises Go's "errno N" placeholder — what
// syscall.Errno.Error() returns for a number its table does not name — and
// substitutes the C library's wording for it. errno 0 is "Undefined error: 0"
// rather than "Unknown error: 0"; both spellings are the host MRI's.
func errnoUnknownText(goText string, n int64) (string, bool) {
	if !strings.HasPrefix(goText, "errno ") {
		return "", false
	}
	if n == 0 {
		return "Undefined error: 0", true
	}
	return "Unknown error: " + strconv.FormatInt(n, 10), true
}

// capitalizeASCII upper-cases a leading ASCII letter, leaving everything else
// (including a non-ASCII first byte) untouched. Go's errno strings are ASCII.
func capitalizeASCII(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-('a'-'A')) + s[1:]
}
