// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"runtime"
	"sort"
	"testing"
)

// The Plan 9 errno mapping is pure Go (errno_plan9_map.go, plan9_notes.go,
// plan9_stat.go), so these tests run on every platform: there is no Plan 9 runner
// in CI, and the table is the part that can be wrong.

// TestPlan9LinuxTablePartitionsKnownNames is the bucket rule of errno.go checked
// against the Plan 9 tables: the Linux universal names, the Linux platform names,
// the aliases and the undefined-everywhere names are disjoint and together are
// MRI's errnoKnownNameCount names, so Errno.constants has the same 158 entries on
// Plan 9 as everywhere else.
func TestPlan9LinuxTablePartitionsKnownNames(t *testing.T) {
	seen := map[string]string{}
	add := func(name, bucket string) {
		if prev, dup := seen[name]; dup {
			t.Errorf("%s is in both %s and %s", name, prev, bucket)
		}
		seen[name] = bucket
	}
	for name := range errnoLinuxUniversal() {
		add(name, "linux universal")
	}
	for name := range errnoLinuxPlatform() {
		add(name, "linux platform")
	}
	for name := range errnoAliases {
		add(name, "aliases")
	}
	for _, name := range errnoUndefinedEverywhere {
		add(name, "undefined everywhere")
	}
	if len(seen) != errnoKnownNameCount {
		t.Errorf("the Plan 9 buckets hold %d names, want MRI's %d", len(seen), errnoKnownNameCount)
	}
	var plat []string
	for name := range errnoLinuxPlatform() {
		plat = append(plat, name)
	}
	want := append([]string{}, errnoPlatformNames...)
	sort.Strings(plat)
	sort.Strings(want)
	if len(plat) != len(want) {
		t.Fatalf("Linux platform names %v, want errnoPlatformNames %v", plat, want)
	}
	for i := range plat {
		if plat[i] != want[i] {
			t.Fatalf("Linux platform names %v, want errnoPlatformNames %v", plat, want)
		}
	}
}

// TestPlan9LinuxTableMatchesHost proves the table against a source that is not
// the table: the set of names must equal the host's errnoUniversalNumbers on every
// platform that has numbers (all but plan9, where this file's table IS the host),
// and on Linux — whose generic errno numbers the table carries — the values must
// agree too. A name missing from the table, or a mistyped number, fails here.
func TestPlan9LinuxTableMatchesHost(t *testing.T) {
	if runtime.GOOS == "plan9" {
		t.Skip("the table is the host table on plan9")
	}
	linux := errnoLinuxUniversal()
	for name := range errnoUniversalNumbers {
		if _, ok := linux[name]; !ok {
			t.Errorf("host has %s, the Linux table does not", name)
		}
	}
	for name := range linux {
		if _, ok := errnoUniversalNumbers[name]; !ok {
			t.Errorf("the Linux table has %s, the host does not", name)
		}
	}
	generic := runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "386" ||
		runtime.GOARCH == "arm" || runtime.GOARCH == "arm64" || runtime.GOARCH == "riscv64" ||
		runtime.GOARCH == "loong64" || runtime.GOARCH == "s390x")
	if !generic {
		return
	}
	for name, n := range linux {
		if name == "EWOULDBLOCK" {
			continue
		}
		if got := errnoUniversalNumbers[name]; got != n {
			t.Errorf("%s: Linux table %d, host %d", name, n, got)
		}
	}
	for name, n := range errnoLinuxPlatform() {
		if got := errnoPlatformNumbers[name]; got != n {
			t.Errorf("%s: Linux table %d, host %d", name, n, got)
		}
	}
}

// TestPlan9LinuxText checks the strerror lookup: first row wins for a shared
// number, an unnamed number is not found, and every row has text.
func TestPlan9LinuxText(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want string
	}{{22, "Invalid argument"}, {2, "No such file or directory"}, {95, "Operation not supported"}, {0, "Success"}} {
		if got, ok := errnoLinuxText(c.n); !ok || got != c.want {
			t.Errorf("errnoLinuxText(%d) = %q, %v; want %q", c.n, got, ok, c.want)
		}
	}
	if got, ok := errnoLinuxText(9999); ok {
		t.Errorf("errnoLinuxText(9999) = %q, want not found", got)
	}
	for _, e := range errnoLinuxTable {
		if e.text == "" {
			t.Errorf("%s has no text", e.name)
		}
	}
}

// TestPlan9ErrnoName drives the string classification: every exact text the plan9
// syscall and os packages declare, the stdlib's substring rules, wrapped and
// padded messages, and strings it does not classify.
func TestPlan9ErrnoName(t *testing.T) {
	known := map[string]bool{}
	for _, e := range errnoLinuxTable {
		known[e.name] = true
	}
	for msg, want := range map[string]string{
		// src/syscall/errors_plan9.go
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
		// src/os/error_plan9.go
		"function not implemented": "ENOSYS",
		"out of range":             "ERANGE",
		"cannot allocate memory":   "ENOMEM",
		// rbgo's own sentinel (spawn_plan9.go)
		plan9ESRCHText: "ESRCH",
		// the stdlib's substring rules (syscall.ErrorString.Is)
		"open /x: permission denied":      "EACCES",
		"create: file already exists now": "EEXIST",
		"mkdir: x is a directory":         "EEXIST",
		"fd: file does not exist":         "ENOENT",
		"walk: x not found":               "ENOENT",
		"x has been removed":              "ENOENT",
		"x no parent":                     "ENOENT",
		"thing not supported here":        "ENOTSUP",
		// trimmed
		"  file does not exist\n": "ENOENT",
	} {
		got, ok := plan9ErrnoName(msg)
		if !ok || got != want {
			t.Errorf("plan9ErrnoName(%q) = %q, %v; want %q", msg, got, ok, want)
		}
		if !known[got] {
			t.Errorf("plan9ErrnoName(%q) = %s, which is not in the Linux table", msg, got)
		}
	}
	for _, msg := range []string{"", "something else entirely", "errno 5"} {
		if got, ok := plan9ErrnoName(msg); ok {
			t.Errorf("plan9ErrnoName(%q) = %q, want unclassified", msg, got)
		}
	}
	// Every name the rules can return exists in the Linux table.
	for _, name := range plan9ExactErrors {
		if !known[name] {
			t.Errorf("exact rule names %s, not in the Linux table", name)
		}
	}
	for _, r := range plan9ContainsRules {
		if !known[r.name] {
			t.Errorf("contains rule names %s, not in the Linux table", r.name)
		}
	}
}

// TestPlan9Notes checks the signal-to-note table: each entry is a real signal name,
// the six notes are Go's, and a signal without a note is refused.
func TestPlan9Notes(t *testing.T) {
	for name := range plan9Notes {
		if _, ok := signalNumbers[name]; !ok {
			t.Errorf("plan9Notes names %s, which is not a Ruby signal", name)
		}
	}
	for name, want := range map[string]string{
		"KILL": "kill", "INT": "interrupt", "HUP": "hangup",
		"ALRM": "alarm", "ABRT": "abort", "TERM": "interrupt",
	} {
		note, ok := plan9NoteFor(signalNumbers[name])
		if !ok || note != want {
			t.Errorf("plan9NoteFor(%s) = %q, %v; want %q", name, note, ok, want)
		}
	}
	if note, ok := plan9NoteFor(signalNumbers["USR1"]); ok {
		t.Errorf("USR1 has note %q, want none", note)
	}
	if note, ok := plan9NoteFor(0); ok {
		t.Errorf("signal 0 has note %q, want none", note)
	}
}

// TestPlan9StatFields checks the Dir-to-stat mapping and the owner comparison.
func TestPlan9StatFields(t *testing.T) {
	f := plan9StatFields(0x1234, 0x5, 0x6)
	if f.ino != 0x1234 || f.dev != 0x5<<32|0x6 || f.nlink != 1 || !f.hasSys || f.uid != 0 || f.gid != 0 {
		t.Errorf("plan9StatFields = %+v", f)
	}
	// A Qid path above the int64 range wraps, but is stable and not an error.
	if big := plan9StatFields(^uint64(0), 0xffff, 0xffffffff); big.ino != -1 || big.dev != 0xffff<<32|0xffffffff {
		t.Errorf("plan9StatFields(max) = %+v", big)
	}
	for _, c := range []struct {
		owner, user string
		want        bool
	}{
		{"glenda", "glenda", true},
		{"glenda", "glenda\n", true},
		{"glenda", "bootes", false},
		{"", "", false},
		{"glenda", "", false},
		{"", "glenda", false},
	} {
		if got := plan9Owner(c.owner, c.user); got != c.want {
			t.Errorf("plan9Owner(%q, %q) = %v, want %v", c.owner, c.user, got, c.want)
		}
	}
}
