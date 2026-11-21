// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestClosingAStreamWritesOnlyWhatWasWritten pins #969: a flush or close puts
// PENDING bytes on disk and nothing else. Every want is MRI 4.0.7's output for
// the same program (arm64-darwin25, rubydiff, 2026-11-21). Before the fix rbgo
// diverged on the first six rows, which wrote the stream's open-time snapshot
// (or a byte #ungetc pushed back) over the file. The rows after them are the
// writes that MUST still land, so a gate that stops too much goes red too.
func TestClosingAStreamWritesOnlyWhatWasWritten(t *testing.T) {
	p := filepath.ToSlash(filepath.Join(t.TempDir(), "f"))
	reset := fmt.Sprintf("File.write(%q, \"0123456789\"); ", p)
	read := fmt.Sprintf("p File.read(%q)", p)
	cases := []struct{ name, src, want string }{
		{"untouched r+ keeps another writer's bytes",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); File.write(%q, "NEW"); f.close; `, p, p) + read, "\"NEW\"\n"},
		{"untouched a keeps another writer's bytes",
			reset + fmt.Sprintf(`f = File.open(%q, "a"); File.write(%q, "NEW"); f.close; `, p, p) + read, "\"NEW\"\n"},
		{"untouched a+ keeps another writer's bytes",
			reset + fmt.Sprintf(`f = File.open(%q, "a+"); File.write(%q, "NEW"); f.close; `, p, p) + read, "\"NEW\"\n"},
		{"untouched w+ keeps another writer's bytes",
			reset + fmt.Sprintf(`f = File.open(%q, "w+"); File.write(%q, "NEW"); f.close; `, p, p) + read, "\"NEW\"\n"},
		{"untouched r+ does not bring back an unlinked file",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); File.unlink(%q); f.close; p File.exist?(%q)`, p, p, p), "false\n"},
		{"ungetc is never written to the file",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); f.ungetc("X"); f.close; `, p) + read, "\"0123456789\"\n"},
		{"a write still lands at close",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); f.write("AB"); f.close; `, p) + read, "\"AB23456789\"\n"},
		{"a sync write lands before close",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); f.sync = true; f.write("CD"); `, p) + read, "\"CD23456789\"\n"},
		{"an append lands at flush",
			reset + fmt.Sprintf(`f = File.open(%q, "a"); f.write("Z"); f.flush; `, p) + read, "\"0123456789Z\"\n"},
		{"w truncates with no write",
			reset + fmt.Sprintf(`File.open(%q, "w").close; `, p) + read, "\"\"\n"},
		{"truncate lands with no write",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); f.truncate(4); f.close; `, p) + read, "\"0123\"\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}
