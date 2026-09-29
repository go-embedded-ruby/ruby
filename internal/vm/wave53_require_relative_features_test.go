// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRequireRelativeHonoursLoadedFeatures: an entry the PROGRAM puts in
// $LOADED_FEATURES counts as loaded, for require_relative as much as for
// require. MRI's rb_f_require_relative expands the name against the requiring
// file's directory and hands it to require_internal, which asks rb_feature_p
// like any other require -- there is no relative exemption. rbgo gated its
// $LOADED_FEATURES pre-check on `!relative` and then consulted only its own
// loaded map, so require_relative re-ran the file.
//
// `require` with the same absolute path was already right, which is what said
// the gap belonged to require_relative and not to the bookkeeping.
//
// It matters beyond the odd-looking program: ruby/spec saves and restores $"
// around every example, which is the same mechanism seen from the other side.
//
// Every expectation is the byte-for-byte answer of ruby 4.0.5.
func TestRequireRelativeHonoursLoadedFeatures(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "sub"), "lib.rb", "puts \"body ran\"\n")

	for _, tc := range []struct{ name, src, want string }{
		{
			"a hand-pushed entry suppresses the load",
			"$LOADED_FEATURES << File.join(__dir__, \"sub\", \"lib.rb\")\np require_relative(\"sub/lib\")\n",
			"false\n",
		},
		{
			// Without this row the one above would pass for a require_relative
			// that never runs anything at all.
			"an unrelated entry does not",
			"$LOADED_FEATURES << \"/nowhere/at/all/lib.rb\"\np require_relative(\"sub/lib\")\n",
			"body ran\ntrue\n",
		},
		{
			"an ordinary double require_relative is unchanged",
			"p require_relative(\"sub/lib\")\np require_relative(\"sub/lib\")\n",
			"body ran\ntrue\nfalse\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := runInDir(t, dir, tc.src)
			if err != nil {
				t.Fatalf("run: %v (out %q)", err, got)
			}
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}
