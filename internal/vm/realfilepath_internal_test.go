// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm

package vm

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// runScriptFile runs a file the way cmd/rbgo does -- SetScriptPath then Run --
// which is the only shape that exercises the main script's own path pair. The
// path is passed as SPELLED, because for these assertions the spelling of the
// invocation IS the measurement.
func runScriptFile(t *testing.T, path string) string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	prog, perr := parser.Parse(string(src))
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	iseq, cerr := compiler.Compile(prog)
	if cerr != nil {
		t.Fatalf("compile: %v", cerr)
	}
	var buf bytes.Buffer
	machine := New(&buf)
	machine.SetScriptPath(path)
	if _, rerr := machine.Run(iseq); rerr != nil {
		t.Fatalf("run: %v\noutput:\n%s", rerr, buf.String())
	}
	return buf.String()
}

// realTempDir is t.TempDir() with its own symlinks resolved, so the ONLY link in
// the fixture is the one the test makes. Without it the assertions would be
// measuring /var -> private/var on macOS instead of the tree under test, and the
// expectations would differ per platform for a reason that has nothing to do
// with what is being tested.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir := filepath.ToSlash(t.TempDir())
	real, err := realpathResolve(featurePath(dir), true, false)
	if err != nil {
		t.Fatalf("cannot resolve temp dir %q: %v", dir, err)
	}
	return real
}

// TestISeqPathAndRealpathDivide is the witness for issue #741: MRI keeps two
// paths per compiled file and the path APIs divide between them, where rbgo kept
// one and answered it everywhere.
//
// Every expectation below was READ OFF ruby 4.0.5 (arm64-darwin25) running this
// same fixture, not derived: the engine printed the thirteen lines and the
// oracle printed the same thirteen. The ones worth naming, because they are the
// ones that look wrong until you see MRI do it:
//
//   - the main script's __FILE__ is the SPELLING (…/link/main.rb) while its
//     __dir__ is the RESOLVED directory (…/real) -- the same frame, two fields;
//   - require_relative resolves its BASE only: from …/link/main.rb it loads
//     …/real/lib.rb, and that is what lands in $LOADED_FEATURES;
//   - a require of an absolute path whose LEAF is a symlink keeps the symlink in
//     __FILE__, in Location#path and in $LOADED_FEATURES, and resolves it in
//     Location#absolute_path alone;
//   - the same file required by TWO spellings that differ only through a symlink
//     loads ONCE: the second require answers false and $LOADED_FEATURES keeps a
//     single entry, although the spelling it holds is not the one asked for the
//     second time. That is MRI's loaded_features_realpaths, and it is the only
//     thing standing between a resolved require_relative base and a file that
//     silently loads twice;
//   - an eval frame answers nil for #absolute_path (MRI stores [path, Qnil] and
//     defines rb_iseq_from_eval_p as NIL_P(rb_iseq_realpath), iseq.c-ruby_4_0
//     :1480) while __dir__ on that same frame answers the LEXICAL dirname of the
//     filename -- rb_current_realfilepath falls back to the path, #absolute_path
//     does not.
func TestISeqPathAndRealpathDivide(t *testing.T) {
	root := realTempDir(t)
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(real, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("lib.rb", `puts "lib.__FILE__=#{__FILE__.sub(BASE,'')}"`+"\n")
	write("leaf.rb", strings.Join([]string{
		`puts "leaf.__FILE__=#{__FILE__.sub(ROOT,'')}"`,
		`puts "leaf.path=#{caller_locations(0,1)[0].path.sub(ROOT,'')}"`,
		`puts "leaf.abs=#{caller_locations(0,1)[0].absolute_path.to_s.sub(ROOT,'')}"`,
		"",
	}, "\n"))
	if err := os.Symlink("leaf.rb", filepath.Join(real, "leaflink.rb")); err != nil {
		t.Fatal(err)
	}
	write("main.rb", strings.Join([]string{
		`BASE = File.dirname(File.realpath(__FILE__))`,
		`ROOT = File.dirname(BASE)`,
		`puts "main.__FILE__=#{__FILE__.sub(ROOT,'')}"`,
		`puts "main.__dir__=#{__dir__.sub(ROOT,'')}"`,
		`l = caller_locations(0,1)[0]`,
		`puts "main.path=#{l.path.sub(ROOT,'')}"`,
		`puts "main.abs=#{l.absolute_path.to_s.sub(ROOT,'')}"`,
		`require_relative 'lib'`,
		`puts "LF.lib=#{$LOADED_FEATURES.last.sub(ROOT,'')}"`,
		`require ROOT + "/link/leaflink.rb"`,
		`puts "LF.leaf=#{$LOADED_FEATURES.last.sub(ROOT,'')}"`,
		`puts "requeue=#{require(ROOT + "/real/leaflink.rb")}"`,
		`puts "LF.count=#{$LOADED_FEATURES.count { |f| f.end_with?('leaflink.rb') }}"`,
		`puts "eval.abs=#{eval('caller_locations(0)[0].absolute_path', nil, 'foo.rb').inspect}"`,
		`puts "eval.__dir__=#{eval('__dir__', nil, 'foo/bar.rb').inspect}"`,
		"",
	}, "\n"))

	got := runScriptFile(t, filepath.Join(root, "link", "main.rb"))
	want := strings.Join([]string{
		"main.__FILE__=/link/main.rb", // as SPELLED
		"main.__dir__=/real",          // RESOLVED
		"main.path=/link/main.rb",     // as SPELLED
		"main.abs=/real/main.rb",      // RESOLVED
		"lib.__FILE__=/lib.rb",        // require_relative's base was resolved
		"LF.lib=/real/lib.rb",
		"leaf.__FILE__=/link/leaflink.rb", // the leaf symlink SURVIVES here…
		"leaf.path=/link/leaflink.rb",
		"leaf.abs=/real/leaf.rb", // …and is resolved only here
		"LF.leaf=/link/leaflink.rb",
		// The SAME file by a second spelling: already loaded, and $" keeps one entry.
		// The leaf lines above do not repeat, which is the half of this that says the
		// body did not run again -- "returns false" and "did not re-run" are two
		// claims and a bare `false` only makes one of them.
		"requeue=false",
		"LF.count=1",
		"eval.abs=nil",       // no realpath at all for an eval unit
		`eval.__dir__="foo"`, // …but __dir__ falls back to the path
		"",
	}, "\n")
	if got != want {
		t.Fatalf("path/realpath division diverged from ruby 4.0.5\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestRealFilePathFallbacks covers the two answers the registry gives when it
// has nothing recorded, both of which are MRI's answers rather than conveniences.
func TestRealFilePathFallbacks(t *testing.T) {
	machine := New(&bytes.Buffer{})

	// Never loaded, and not an eval filename: its own canonical path, because
	// that is what MRI's single-String pathobj means. A Location a program builds
	// itself -- set_backtrace(["a:1:in 'm'"]) -- lands here, and #absolute_path
	// answers "a" rather than nil.
	if got, ok := machine.realFilePath("a"); !ok || got != "a" {
		t.Fatalf(`realFilePath("a") = (%q, %v), want ("a", true)`, got, ok)
	}

	// An eval filename is the one thing with NO realpath: MRI compiles it with
	// pathobj [path, Qnil], and #absolute_path reads that Qnil.
	machine.noteEvalFile("foo.rb")
	if got, ok := machine.realFilePath("foo.rb"); ok {
		t.Fatalf(`realFilePath("foo.rb") after noteEvalFile = (%q, true), want not-loaded`, got)
	}
	// …unless a real file of that name is loaded, whose realpath wins: this
	// registry is keyed by path string where MRI keys it per ISeq.
	machine.noteRealFilePathAs("bar.rb", "/real/bar.rb")
	machine.noteEvalFile("bar.rb")
	if got, ok := machine.realFilePath("bar.rb"); !ok || got != "/real/bar.rb" {
		t.Fatalf(`realFilePath("bar.rb") = (%q, %v), want ("/real/bar.rb", true)`, got, ok)
	}

	// Loaded but unresolvable -- the file went away between the read and the
	// resolve -- keeps its absolute spelling rather than dropping the record. MRI
	// does the same when rb_check_realpath fails: the ISeq still has a path.
	missing := filepath.ToSlash(filepath.Join(realTempDir(t), "gone", "x.rb"))
	machine.noteRealFilePathAs(missing, missing)
	got, ok := machine.realFilePath(missing)
	if !ok || got != missing {
		t.Fatalf("unresolvable path: got (%q, %v), want (%q, true)", got, ok, missing)
	}

	// No file executing at all -- no script path, nothing on the require stack --
	// so there is no directory to answer, and Kernel#__dir__ returns nil.
	if d := machine.currentRealDir(); d != "" {
		t.Fatalf("currentRealDir with no script = %q, want empty", d)
	}
}
