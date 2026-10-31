// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"testing"
	stdtime "time"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// The clock helpers below serve the Time.now tests (core) and the Timecop tests
// (a gem). They lived in timecop_internal_test.go until the rbgo_core build
// stopped compiling it; they moved here unchanged.

// pinnedNow is 2026-06-21 12:34:56 UTC — the same fixed instant the Date clock
// seam test uses, so Timecop's unmocked (real) now is deterministic here too.
const pinnedNow = 1782045296

// withPinnedNow pins the nowWall seam (the real-clock source the VM's Timecop
// Clock reads through) for the duration of fn, restoring it afterwards, so
// "real time" in these tests is the fixed pinnedNow instant.
func withPinnedNow(fn func()) {
	saved := nowWall
	defer func() { nowWall = saved }()
	nowWall = func() stdtime.Time { return stdtime.Unix(pinnedNow, 0).UTC() }
	fn()
}

// evalTC runs a Ruby program under a pinned real clock and returns stdout,
// failing the test on a parse / compile / runtime error.
func evalTC(t *testing.T, src string) string {
	t.Helper()
	var out string
	withPinnedNow(func() { out = runTC(t, src) })
	return out
}

// runTC compiles and runs src through a fresh VM, returning stdout. The caller
// pins the clock; a runtime error is fatal.
func runTC(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	return buf.String()
}
