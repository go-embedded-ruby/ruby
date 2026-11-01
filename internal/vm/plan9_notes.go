// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

// plan9Notes maps a Ruby signal name (SIG prefix removed) to the Plan 9 note that
// stands for it. Plan 9 has no signal numbers: a process is signalled by writing a
// note (a short string) to /proc/<pid>/note, which is what Go's own
// (*os.Process).Signal does on plan9 (src/os/exec_plan9.go, writeProcFile("note",
// sig.String())). The table is the one Go's plan9 syscall package declares
// (src/syscall/errors_plan9.go): SIGABRT "abort", SIGALRM "alarm", SIGHUP
// "hangup", SIGINT "interrupt", SIGKILL "kill", and SIGTERM, which Go also maps to
// "interrupt". Any other signal has no note, and Process.kill with it fails with
// Errno::EINVAL.
//
// It lives in a tag-free file because it is pure data: the mapping is tested on
// every platform, while only spawn_plan9.go (GOOS=plan9) writes the note.
var plan9Notes = map[string]string{
	"ABRT": "abort",
	"ALRM": "alarm",
	"HUP":  "hangup",
	"INT":  "interrupt",
	"KILL": "kill",
	"TERM": "interrupt",
}

// plan9NoteFor returns the note for signal number sig, resolved through the
// canonical signal name (signalNames), and false for a signal Plan 9 cannot send.
func plan9NoteFor(sig int) (string, bool) {
	note, ok := plan9Notes[signalNames[sig]]
	return note, ok
}
