// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	ruby "github.com/go-embedded-ruby/ruby"
)

// The README's "Embedding in a Go program" section states its contract "as
// measured rather than intended". These tests are that measurement, so the
// prose cannot drift away from the code silently: each one fails if the
// behaviour changes, which is the signal to rewrite the bullet rather than the
// test. They are deliberately about the embedding API only -- the one exported
// function -- because that is all the section describes.

// A program that stops itself is reported as success. README: "exit, exit! and
// abort stop the program and Run returns nil".
func TestREADMEExitIsReportedAsSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, src, wantOut string
	}{
		{"abort", `puts "before"` + "\n" + `abort "fatal"` + "\n" + `puts "after"`, "fatal"},
		{"exit", `puts "before"` + "\n" + `exit 3` + "\n" + `puts "after"`, ""},
		{"exit!", `puts "before"` + "\n" + `exit!(42)` + "\n" + `puts "after"`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := ruby.Run(tc.src, &out); err != nil {
				t.Fatalf("Run returned %v; the README says a program that exits reports no error", err)
			}
			got := out.String()
			if !strings.Contains(got, "before") {
				t.Fatalf("output %q does not contain %q: the program did not run at all", got, "before")
			}
			if strings.Contains(got, "after") {
				t.Fatalf("output %q contains %q: the program was not stopped", got, "after")
			}
			if tc.wantOut != "" && !strings.Contains(got, tc.wantOut) {
				t.Fatalf("output %q does not contain %q", got, tc.wantOut)
			}
		})
	}
}

// A Ruby exception, by contrast, does become a Go error -- the half of the
// contract that holds. Without this twin the test above would also pass if Run
// swallowed every error, so it is what makes the one above mean something.
func TestREADMERaiseBecomesAnError(t *testing.T) {
	var out bytes.Buffer
	err := ruby.Run(`raise ArgumentError, "bad"`, &out)
	if err == nil {
		t.Fatal("Run returned nil for a raise; the README says a Ruby exception becomes a Go error")
	}
	if got := err.Error(); !strings.Contains(got, "ArgumentError") || !strings.Contains(got, "bad") {
		t.Fatalf("error %q does not name the class and message", got)
	}
}

// The Ruby heap is not shared between calls. README: "a global, a constant and a
// String monkey-patch set in one Run are all gone in the next".
func TestREADMERubyHeapIsNotSharedBetweenCalls(t *testing.T) {
	var first bytes.Buffer
	if err := ruby.Run(`$leak = "x"; LEAKC = "y"; class String; def hijacked?; true; end; end`, &first); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	var second bytes.Buffer
	if err := ruby.Run(`puts $leak.inspect
puts defined?(LEAKC).inspect
puts "".respond_to?(:hijacked?)`, &second); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got, want := second.String(), "nil\nnil\nfalse\n"; got != want {
		t.Fatalf("second Run saw %q, want %q: state leaked across calls", got, want)
	}
}

// Process-global state IS shared, with the host and with every other Run.
// README: a script that assigns to ENV changes it for the host Go program, and
// it stays changed after Run returns. The cwd behaves the same way; it is not
// exercised here because changing the test binary's working directory would
// disturb every other test in the package.
func TestREADMEProcessEnvIsSharedWithTheHost(t *testing.T) {
	const key = "RBGO_README_CONTRACT_PROBE"
	t.Setenv(key, "set-by-the-host") // restored by the framework

	var out bytes.Buffer
	if err := ruby.Run(`ENV[`+rubyStr(key)+`] = "set-inside-run"`, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := os.Getenv(key); got != "set-inside-run" {
		t.Fatalf("host sees %q after Run, want %q: the README says ENV is shared with the host", got, "set-inside-run")
	}

	// And a later Run sees it too, which is what makes the sharing matter.
	var second bytes.Buffer
	if err := ruby.Run(`puts ENV[`+rubyStr(key)+`].inspect`, &second); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got, want := second.String(), "\"set-inside-run\"\n"; got != want {
		t.Fatalf("second Run saw %q, want %q", got, want)
	}
}

// rubyStr renders a Go string as a Ruby string literal. Spelled out rather than
// using %q inside a fmt.Sprintf of Ruby source, so the generated program is
// obvious at the call site.
func rubyStr(s string) string { return `"` + s + `"` }
