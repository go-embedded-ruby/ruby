// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"runtime"
	"testing"
)

// `~` expansion consults $HOME and falls back to the passwd database only when
// HOME is ABSENT. MRI's rb_default_home_dir branches on `if (!dir)` — getenv
// returning NULL — so an empty HOME is a value, not an absence, and
// expand_path's own absoluteness test is what rejects it.
//
// Measured on `ruby 4.0.7 (2026-09-15 revision 229531a6cf) +PRISM
// [arm64-darwin25]` through scripts/conformance/rubyspec/cmd/oracle:
//
//	HOME                  Dir.home          File.expand_path("~")
//	""                    ""                ArgumentError: non-absolute home
//	"relative/home"       "relative/home"   ArgumentError: non-absolute home
//	unset                 passwd home       passwd home
//
// ⛔ Two defects met here. expand_path RAISED for an unset HOME where MRI
// answers (#901), because it kept its own HOME-only copy of a rule dirHomeStr
// already encoded for Dir.home. And `HOME=""` reached the passwd fallback in
// BOTH, because the seam was os.UserHomeDir, which reports an error for unset
// and empty alike and so cannot express MRI's test.
//
// ⛔ The empty-HOME case used to raise with "couldn't find HOME environment --
// expanding '~'" — the right CLASS for the wrong REASON, so the pre-existing
// guard test passed while the message disagreed with MRI. The messages are
// asserted here for that reason.
func TestTildeExpansionConsultsHomeThenThePasswdDatabase(t *testing.T) {
	// ⛔ EVERY ROW BELOW MUTATES THE PROCESS ENVIRONMENT. `ENV["HOME"] = …` in
	// Ruby is setenv, and runSrc builds a VM inside THIS process, so a row that
	// left HOME empty or deleted would hand it to every test that runs after
	// this one — in a package whose tests share one process. t.Setenv captures
	// HOME now and restores it at cleanup whatever the rows did to it in
	// between; it also makes this test refuse to run in parallel, which is
	// correct for something that writes a global.
	t.Setenv("HOME", os.Getenv("HOME"))

	const pre = `def show
  p File.expand_path("~")
rescue => e
  puts "#{e.class}: #{e.message}"
end
`
	// ⛔ posix marks the rows that name the FALLBACK'S DESTINATION. The passwd
	// database is a POSIX concept and Process.uid is -1 on Windows, where
	// Etc.getpwuid(-1) raises "can't find user for -1" — which is how CI failed
	// this test, not how the fix failed. MRI's chain differs there too (HOME,
	// then HOMEDRIVE+HOMEPATH, then USERPROFILE), so what an absent HOME
	// resolves TO is not assertable cross-platform; that an absent HOME does not
	// RAISE is, and that row is not marked. file_path_internal_test.go marks its
	// ~user row the same way, for the same reason.
	for _, tc := range []struct {
		name, src, want string
		posix           bool
	}{
		{
			name: "an empty HOME is a non-absolute home, not an absent one",
			src: pre + `ENV["HOME"] = ""
show`,
			want: `ArgumentError: non-absolute home`,
		},
		{
			name: "a relative HOME is rejected the same way",
			src: pre + `ENV["HOME"] = "relative/home"
show`,
			want: `ArgumentError: non-absolute home`,
		},
		{
			name: "an absolute HOME is used",
			src: pre + `ENV["HOME"] = "/an/absolute/home"
show`,
			want: `"/an/absolute/home"`,
		},
		// ⛔ The #901 row, in the form that holds on every platform: an absent
		// HOME must not RAISE. Where it resolves to is the posix row below.
		{
			name: "an unset HOME does not raise",
			src: `ENV.delete("HOME")
p File.expand_path("~").is_a?(String)`,
			want: `true`,
		},
		{
			name: "an unset HOME resolves to the passwd home",
			src: `require "etc"
ENV.delete("HOME")
p File.expand_path("~") == Etc.getpwuid(Process.uid).dir`,
			want:  `true`,
			posix: true,
		},
		{
			name: "the tail is kept across the fallback",
			src: `ENV.delete("HOME")
p File.expand_path("~/x").end_with?("/x")`,
			want:  `true`,
			posix: true,
		},
		// ⛔ HOME is read on EVERY call, not captured at startup. Without this
		// control the fallback row cannot be attributed to the passwd lookup: a
		// value remembered from process start would answer identically.
		{
			name: "HOME is consulted live, so the fallback is attributable",
			src: pre + `ENV["HOME"] = "/first/home"
show
ENV["HOME"] = "/second/home"
show`,
			want: "\"/first/home\"\n\"/second/home\"",
		},
		// Dir.home answers getenv verbatim — it does NOT apply expand_path's
		// absoluteness test. Same helper, different contract.
		{
			name: "Dir.home answers a set HOME verbatim, empty included",
			src: `ENV["HOME"] = ""
p Dir.home
ENV["HOME"] = "relative/home"
p Dir.home`,
			want: "\"\"\n\"relative/home\"",
		},
		{
			name: "~user is unaffected",
			src: `begin
  File.expand_path("~no_such_user_rbgo_xyz_123")
rescue ArgumentError => e
  puts e.message
end`,
			want: `user no_such_user_rbgo_xyz_123 doesn't exist`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.posix && runtime.GOOS == "windows" {
				t.Skip("the passwd database (Process.uid, Etc.getpwuid) is POSIX-only")
			}
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}
