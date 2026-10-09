// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_closed

package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Every "want" in this file was measured against the oracle, MRI ruby 4.0.5
// (2026-05-20 revision 64336ffd0e) +PRISM [arm64-darwin25], by running the same
// argv through `ruby` with stdout, stderr and the exit status captured
// separately. Where the oracle and the cited v3_4_0 source could disagree the
// oracle decided; the two agree everywhere below.
//
// These are in-process tests of parseOptions and runProgram, so they are coverage
// of THIS package's code. The process-level table (rbgo versus ruby, byte for
// byte) lives in the pull request body: it measures the built binary and cannot
// cover the functions it exercises.

// TestParseOptionsSplitsTheCommandLine is the witness for #708. On origin/main
// runCmd accepted only `-e code` or a single bare word, so every row here with an
// argument after the script was a usage error and exit 2.
//
// A/B: change the loop's guard from `len(arg) < 2 || arg[0] != '-'` to
// `arg[0] != '-'` — it compiles, and rows "dash alone is a script" and "empty
// string is not an option" then fail, because "-" and "" are no longer the
// boundary proc_options makes them.
func TestParseOptionsSplitsTheCommandLine(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		script    string
		eScript   string
		argv      []string
		warnLevel int
		warnSet   bool
		help      bool
		showVer   bool
		verOnly   bool
	}{
		{name: "script plus arguments", args: []string{"t.rb", "alpha", "beta"},
			script: "t.rb", argv: []string{"alpha", "beta"}, warnLevel: 1},
		{name: "script alone", args: []string{"t.rb"},
			script: "t.rb", argv: []string{}, warnLevel: 1},
		// Measured: `ruby t.rb -- -w` prints ["--", "-w"]. Options stopped at the
		// script, so the "--" after it is an ordinary argument. The brief for this
		// work predicted ["-w"]; the oracle says otherwise.
		{name: "dash-dash AFTER the script is just an argument", args: []string{"t.rb", "--", "-w"},
			script: "t.rb", argv: []string{"--", "-w"}, warnLevel: 1},
		{name: "dash-dash BEFORE the script ends the options", args: []string{"--", "t.rb", "a", "b"},
			script: "t.rb", argv: []string{"a", "b"}, warnLevel: 1},
		{name: "a flag-shaped argument after the script is an argument", args: []string{"t.rb", "-w"},
			script: "t.rb", argv: []string{"-w"}, warnLevel: 1},
		{name: "dash alone is a script (stdin), not an option", args: []string{"-", "a"},
			script: "-", argv: []string{"a"}, warnLevel: 1},
		{name: "empty string is not an option", args: []string{"", "a"},
			script: "", argv: []string{"a"}, warnLevel: 1},
		// -e takes no script entry, so everything left is ARGV...
		{name: "-e with arguments", args: []string{"-e", "p ARGV", "a", "b"},
			eScript: "p ARGV\n", argv: []string{"a", "b"}, warnLevel: 1},
		// ...but -e does NOT end option processing: measured, `ruby -e 'p ARGV' -w`
		// prints [] and runs verbose.
		{name: "-e then a flag: still a flag", args: []string{"-e", "p ARGV", "-w"},
			eScript: "p ARGV\n", argv: []string{}, warnLevel: 2, warnSet: true},
		{name: "-e with attached code", args: []string{"-ep ARGV", "a"},
			eScript: "p ARGV\n", argv: []string{"a"}, warnLevel: 1},
		{name: "-e twice concatenates with newlines", args: []string{"-e", "p 1", "-e", "p 2", "a"},
			eScript: "p 1\np 2\n", argv: []string{"a"}, warnLevel: 1},
		{name: "-e then dash-dash", args: []string{"-e", "p ARGV", "--", "-a"},
			eScript: "p ARGV\n", argv: []string{"-a"}, warnLevel: 1},
		// The four -W states, and all of them with script arguments, which is where
		// #708 and #709 meet.
		{name: "-W0 with arguments", args: []string{"-W0", "t.rb", "a"},
			script: "t.rb", argv: []string{"a"}, warnLevel: 0, warnSet: true},
		{name: "-W1 with arguments", args: []string{"-W1", "t.rb", "a"},
			script: "t.rb", argv: []string{"a"}, warnLevel: 1, warnSet: true},
		{name: "-W2 with arguments", args: []string{"-W2", "t.rb", "a"},
			script: "t.rb", argv: []string{"a"}, warnLevel: 2, warnSet: true},
		{name: "-w with arguments", args: []string{"-w", "t.rb", "a"},
			script: "t.rb", argv: []string{"a"}, warnLevel: 2, warnSet: true},
		{name: "bare -W is -W2", args: []string{"-W", "t.rb"},
			script: "t.rb", argv: []string{}, warnLevel: 2, warnSet: true},
		// MRI does NOT clamp: scan_oct gives v == 3 and the switch's `default`
		// arm sets ruby_verbose = Qtrue. The level is kept raw here for the same
		// reason, so the mapping to $VERBOSE lives in one place
		// (vm.SetWarningLevel); what a program can observe is pinned by
		// TestCommandLineReachesTheProgram's -W3 row, where $VERBOSE is true.
		{name: "-W3 is level 3, which maps to verbose", args: []string{"-W3", "t.rb"},
			script: "t.rb", argv: []string{}, warnLevel: 3, warnSet: true},
		// Measured both ways round: within ONE command line the last -w/-W wins,
		// because proc_options only commits opt->warning at switch_end.
		{name: "-W0 then -w: verbose", args: []string{"-W0", "-w", "t.rb"},
			script: "t.rb", argv: []string{}, warnLevel: 2, warnSet: true},
		{name: "-w then -W0: silent", args: []string{"-w", "-W0", "t.rb"},
			script: "t.rb", argv: []string{}, warnLevel: 0, warnSet: true},
		{name: "clustered -wW0 is silent", args: []string{"-wW0", "t.rb"},
			script: "t.rb", argv: []string{}, warnLevel: 0, warnSet: true},
		{name: "-W:category does not change the level", args: []string{"-W:deprecated", "t.rb", "a"},
			script: "t.rb", argv: []string{"a"}, warnLevel: 1},
		{name: "-W:no-category does not change the level", args: []string{"-W:no-performance", "t.rb"},
			script: "t.rb", argv: []string{}, warnLevel: 1},
		// -h stops the scan where it stands (MRI's `goto switch_end`), so nothing
		// after it is even looked at — including an otherwise invalid option.
		{name: "-h stops the scan", args: []string{"-h", "-Z"},
			argv: nil, warnLevel: 1, help: true},
		{name: "--help stops the scan", args: []string{"--help", "-Z"},
			argv: nil, warnLevel: 1, help: true},
		// -v and --version are NOT the same switch. Measured against MRI 4.0.7:
		//   ruby -v -e 'puts 1'        prints the banner, then prints 1
		//   ruby --version -e 'puts 1' prints the banner and stops
		// So -v carries on scanning (and raises $VERBOSE, like -w), while
		// --version ends the scan the way -h does.
		{name: "-v prints the banner AND runs the script", args: []string{"-v", "t.rb", "a"},
			script: "t.rb", argv: []string{"a"}, warnLevel: 2, warnSet: true, showVer: true},
		{name: "-v clusters like any other switch", args: []string{"-vW0", "t.rb"},
			script: "t.rb", argv: []string{}, warnLevel: 0, warnSet: true, showVer: true},
		{name: "--version does not run the script it is given", args: []string{"--version", "t.rb"},
			script: "t.rb", argv: []string{}, warnLevel: 1, showVer: true, verOnly: true},
		// -v alone answers the question and stops. Measured: `echo "puts 42" |
		// ruby -v` prints the banner and NOT 42, where `| ruby -w` prints 42 --
		// so this is about -v, not about an empty command line.
		{name: "-v with nothing to run does not fall back to stdin", args: []string{"-v"},
			argv: []string{}, warnLevel: 2, warnSet: true, showVer: true, verOnly: true},
		{name: "-w with nothing to run still reads stdin", args: []string{"-w"},
			argv: []string{}, warnLevel: 2, warnSet: true},
		{name: "no arguments: stdin, empty ARGV", args: []string{},
			argv: []string{}, warnLevel: 1},
		{name: "options but no script: stdin", args: []string{"-W0"},
			argv: []string{}, warnLevel: 0, warnSet: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o, err := parseOptions(c.args, io.Discard)
			if err != nil {
				t.Fatalf("parseOptions(%q): %v", c.args, err)
			}
			if o.script != c.script {
				t.Errorf("script = %q, want %q", o.script, c.script)
			}
			if o.eScript != c.eScript {
				t.Errorf("eScript = %q, want %q", o.eScript, c.eScript)
			}
			if o.haveE != (c.eScript != "") {
				t.Errorf("haveE = %v, want %v", o.haveE, c.eScript != "")
			}
			// An empty ARGV and a nil ARGV are the same program-visible value, so
			// compare lengths first and contents only when there is something.
			if len(o.argv) != len(c.argv) || (len(c.argv) > 0 && !reflect.DeepEqual(o.argv, c.argv)) {
				t.Errorf("argv = %#v, want %#v", o.argv, c.argv)
			}
			if o.warnLevel != c.warnLevel {
				t.Errorf("warnLevel = %d, want %d", o.warnLevel, c.warnLevel)
			}
			if o.warnSet != c.warnSet {
				t.Errorf("warnSet = %v, want %v", o.warnSet, c.warnSet)
			}
			if o.help != c.help {
				t.Errorf("help = %v, want %v", o.help, c.help)
			}
			if o.showVersion != c.showVer {
				t.Errorf("showVersion = %v, want %v", o.showVersion, c.showVer)
			}
			if o.versionOnly != c.verOnly {
				t.Errorf("versionOnly = %v, want %v", o.versionOnly, c.verOnly)
			}
		})
	}
}

// TestParseOptionsErrorsAreMRIs pins the exact stderr line, because the whole
// point of #708 is that rbgo printed its usage text where ruby raises. The text
// comes from the oracle, with "ruby: " replaced by "rbgo: " (rb_progname) and
// nothing else changed — note the TWO spaces before "(-h".
//
// A/B: drop the second space from the invalid-option format and every
// "invalid option" row fails while the code still compiles.
func TestParseOptionsErrorsAreMRIs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"-Z", "t.rb"}, "rbgo: invalid option -Z  (-h will show valid options) (RuntimeError)"},
		// Measured: ruby names ONE character, not the rest of the cluster.
		{[]string{"-Zabc", "t.rb"}, "rbgo: invalid option -Z  (-h will show valid options) (RuntimeError)"},
		// -W9: scan_oct reads one OCTAL digit, so '9' is not consumed; the level
		// stays 2 and the scan then rejects "-9".
		{[]string{"-W9", "t.rb"}, "rbgo: invalid option -9  (-h will show valid options) (RuntimeError)"},
		{[]string{"--bogus", "t.rb"}, "rbgo: invalid option --bogus  (-h will show valid options) (RuntimeError)"},
		{[]string{"-e"}, "rbgo: no code specified for -e (RuntimeError)"},
		// A switch ruby HAS and rbgo does not: calling it "invalid" would be a
		// false statement about Ruby, so it gets its own wording — and still fails.
		{[]string{"-n", "t.rb"}, "rbgo: -n is a ruby option that rbgo does not implement"},
		// Was `--version` until #XXX implemented it. A test that asserts a refusal
		// goes green exactly while the defect lives, so this case now names a long
		// option rbgo really does not have -- keeping the wording covered without
		// pinning a gap shut.
		{[]string{"--parser=prism"}, "rbgo: --parser is a ruby option that rbgo does not implement"},
		// --version is NOT --help: it does not abandon the rest of the command
		// line, so a bad option after it is still an error. Measured on MRI 4.0.7.
		{[]string{"--version", "--bogus"}, "rbgo: invalid option --bogus  (-h will show valid options) (RuntimeError)"},
		{[]string{"-v", "-Z"}, "rbgo: invalid option -Z  (-h will show valid options) (RuntimeError)"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			_, err := parseOptions(c.args, io.Discard)
			if err == nil {
				t.Fatalf("parseOptions(%q) = no error, want %q", c.args, c.want)
			}
			if err.Error() != c.want {
				t.Errorf("error = %q, want %q", err.Error(), c.want)
			}
		})
	}
}

// TestUnknownWarningCategoryIsGatedLikeRbWarn: proc_W_option reports an unknown
// -W:name through rb_warn, which is silent when $VERBOSE is nil. Measured:
// `ruby -W:bogus t.rb` warns and `ruby -W0 -W:bogus t.rb` does not.
//
// A/B: remove the `if o.warnLevel > 0` guard in procW — it compiles, and the
// second row fails.
func TestUnknownWarningCategoryIsGatedLikeRbWarn(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown category warns", []string{"-W:bogus", "t.rb"},
			"rbgo: warning: unknown warning category: 'bogus'\n"},
		{"silent under -W0", []string{"-W0", "-W:bogus", "t.rb"}, ""},
		{"known category says nothing", []string{"-W:experimental", "t.rb"}, ""},
		{"known category with no- prefix says nothing", []string{"-W:no-experimental", "t.rb"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			var sb strings.Builder
			if _, err := parseOptions(c.args, &sb); err != nil {
				t.Fatalf("parseOptions(%q): %v", c.args, err)
			}
			if sb.String() != c.want {
				t.Errorf("stderr = %q, want %q", sb.String(), c.want)
			}
		})
	}
}

// TestLoadResolvesTheProgram covers options.load: the three sources a program can
// come from and the name each runs under ($0 / __FILE__ / the backtrace label).
func TestLoadResolvesTheProgram(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "s.rb")
	if err := os.WriteFile(script, []byte("puts 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("a file keeps its path and is require-relative-able", func(t *testing.T) {
		o, err := parseOptions([]string{script, "a"}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		src, name, fromFile, err := o.load()
		if err != nil {
			t.Fatal(err)
		}
		if src != "puts 1\n" || name != script || !fromFile {
			t.Errorf("load = (%q, %q, %v), want (%q, %q, true)", src, name, fromFile, "puts 1\n", script)
		}
	})

	t.Run("-e is named -e and has no script directory", func(t *testing.T) {
		o, err := parseOptions([]string{"-e", "p 1"}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		src, name, fromFile, err := o.load()
		if err != nil {
			t.Fatal(err)
		}
		if src != "p 1\n" || name != "-e" || fromFile {
			t.Errorf("load = (%q, %q, %v), want (%q, \"-e\", false)", src, name, fromFile, "p 1\n")
		}
	})

	t.Run("dash reads stdin and is named -", func(t *testing.T) {
		src, name, fromFile := loadWithStdin(t, []string{"-", "a"}, "p ARGV\n")
		if src != "p ARGV\n" || name != "-" || fromFile {
			t.Errorf("load = (%q, %q, %v), want (%q, \"-\", false)", src, name, fromFile, "p ARGV\n")
		}
	})

	t.Run("no script left also reads stdin", func(t *testing.T) {
		src, name, fromFile := loadWithStdin(t, []string{"-W0"}, "p 2\n")
		if src != "p 2\n" || name != "-" || fromFile {
			t.Errorf("load = (%q, %q, %v), want (%q, \"-\", false)", src, name, fromFile, "p 2\n")
		}
	})

	t.Run("a missing file is an error, not a panic", func(t *testing.T) {
		o, err := parseOptions([]string{filepath.Join(dir, "nope.rb")}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := o.load(); err == nil {
			t.Error("load of a missing file returned no error")
		}
	})
}

// loadWithStdin points os.Stdin at text for the duration of one load().
func loadWithStdin(t *testing.T, args []string, text string) (src, name string, fromFile bool) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = saved; r.Close() }()
	go func() { _, _ = w.WriteString(text); w.Close() }()

	o, err := parseOptions(args, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	src, name, fromFile, err = o.load()
	if err != nil {
		t.Fatal(err)
	}
	return src, name, fromFile
}

// TestCommandLineReachesTheProgram is the end of the wire: the parsed options
// actually change what the interpreted program sees. It runs in-process with
// os.Stdout and os.Stderr pointed at pipes, so it covers runProgram's seeding
// calls rather than a subprocess's behaviour.
//
// Each want was measured with the same argv through MRI 4.0.5.
//
// A/B: delete the `machine.SetARGV(o.argv)` line and the argv rows fail; delete
// the `machine.SetWarningLevel` call and the -W0 row fails. Both still compile.
func TestCommandLineReachesTheProgram(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "probe.rb")
	// $0, __FILE__ and $PROGRAM_NAME are one value in MRI and must be the path as
	// given; ARGV's members are frozen (ruby_set_argv's OBJ_FREEZE); ARGV and $*
	// are the same object.
	program := "p [$0 == __FILE__, $0 == $PROGRAM_NAME, ARGV, ARGV.map(&:frozen?), ARGV.equal?($*), $VERBOSE]\n" +
		"K = 1\nK = 2\n"
	if err := os.WriteFile(script, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name    string
		args    []string
		wantOut string
		wantErr bool // a redefinition warning on fd 2
	}{
		{"arguments reach ARGV", []string{script, "alpha", "beta"},
			"[true, true, [\"alpha\", \"beta\"], [true, true], true, false]\n", true},
		{"-W0 silences and is visible as nil", []string{"-W0", script, "a"},
			"[true, true, [\"a\"], [true], true, nil]\n", false},
		{"-w with arguments is verbose AND keeps ARGV", []string{"-w", script, "a"},
			"[true, true, [\"a\"], [true], true, true]\n", true},
		{"-W3 is verbose too", []string{"-W3", script, "a"},
			"[true, true, [\"a\"], [true], true, true]\n", true},
		{"-W1 is the default", []string{"-W1", script, "a"},
			"[true, true, [\"a\"], [true], true, false]\n", true},
		{"-- before the script", []string{"--", script, "a"},
			"[true, true, [\"a\"], [true], true, false]\n", true},
		{"a -- after the script is an argument", []string{script, "--", "-w"},
			"[true, true, [\"--\", \"-w\"], [true, true], true, false]\n", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			o, err := parseOptions(c.args, io.Discard)
			if err != nil {
				t.Fatalf("parseOptions: %v", err)
			}
			src, name, fromFile, err := o.load()
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			gotOut, gotErr := captureRun(t, func() {
				if _, err := runProgram(src, name, fromFile, o); err != nil {
					t.Errorf("runProgram: %v", err)
				}
			})
			if gotOut != c.wantOut {
				t.Errorf("fd 1 = %q, want %q", gotOut, c.wantOut)
			}
			if warned := strings.Contains(gotErr, "already initialized constant K"); warned != c.wantErr {
				t.Errorf("fd 2 warned = %v (%q), want %v", warned, gotErr, c.wantErr)
			}
		})
	}
}

// TestRunKeepsAnEmptyARGV pins the embedded/test entry point: run() must not
// invent arguments for a program that was given none.
func TestRunKeepsAnEmptyARGV(t *testing.T) {
	gotOut, _ := captureRun(t, func() {
		if _, err := run("p [ARGV, $VERBOSE, $0]", "-e"); err != nil {
			t.Errorf("run: %v", err)
		}
	})
	if want := "[[], false, \"-e\"]\n"; gotOut != want {
		t.Errorf("fd 1 = %q, want %q", gotOut, want)
	}
}

// captureRun points os.Stdout and os.Stderr at pipes for the duration of fn and
// returns what each received. Both pipes are drained concurrently: a pipe holds
// only a buffer's worth, and a blocked write would hang the run rather than fail
// the test.
func captureRun(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	outCh, errCh := make(chan string, 1), make(chan string, 1)
	go func() { b, _ := io.ReadAll(outR); outCh <- string(b) }()
	go func() { b, _ := io.ReadAll(errR); errCh <- string(b) }()

	fn()

	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = savedOut, savedErr
	return <-outCh, <-errCh
}

// TestLoadErrorIsMRIsLoadErrorLine: a script rbgo cannot read must produce the
// line ruby produces. Measured, all four against MRI 4.0.5 with the same argv:
//
//	ruby: No such file or directory -- nosuch.rb (LoadError)   exit 1
//	ruby: Is a directory -- adir (LoadError)                   exit 1
//	ruby: Permission denied -- noperm.rb (LoadError)           exit 1
//
// Before this, rbgo printed Go's *os.PathError — "rbgo: open nosuch.rb: no such
// file or directory" — with the same exit status. The status was never the
// defect; the wording was.
//
// A/B: drop the capitalise() call and every row fails, because Go's errno strings
// are strerror's text with a lower-case first letter.
func TestLoadErrorIsMRIsLoadErrorLine(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "adir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	noperm := filepath.Join(dir, "noperm.rb")
	if err := os.WriteFile(noperm, []byte("p 1\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "nosuch.rb")

	notdir := filepath.Join(noperm, "inner.rb") // a path THROUGH a regular file

	for _, c := range []struct {
		name, path, want string
	}{
		{"missing", missing, "rbgo: No such file or directory -- " + missing + " (LoadError)"},
		{"a directory", sub, "rbgo: Is a directory -- " + sub + " (LoadError)"},
		{"unreadable", noperm, "rbgo: Permission denied -- " + noperm + " (LoadError)"},
		// The fallback arm: no OS-independent predicate names ENOTDIR, so this one
		// comes out of the operating system's own text. Measured:
		// `ruby t.rb/x` prints "ruby: Not a directory -- t.rb/x (LoadError)".
		{"a path through a file", notdir, "rbgo: Not a directory -- " + notdir + " (LoadError)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "unreadable" && !readDenied(t, noperm) {
				// Windows does not deny reads on a 0000 file (nor does root on
				// POSIX), so the PRECONDITION cannot be arranged here. Skipping is
				// reported only after checking that the file really is readable, so
				// this is a measurement rather than an assumption about the host.
				t.Skipf("%s is readable here, so EACCES cannot be arranged", noperm)
			}
			if c.name == "a path through a file" && runtime.GOOS == "windows" {
				// ENOTDIR has no Windows equivalent that Go surfaces with this
				// wording, and this row's "want" is MRI's POSIX strerror text. It is
				// skipped rather than guessed at: the Windows behaviour was NOT
				// measured (no Windows host here), and the CI lane that runs it is
				// where such a claim would have to come from.
				t.Skip("ENOTDIR's strerror text is POSIX; Windows behaviour not measured")
			}
			o, err := parseOptions([]string{c.path}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, lerr := o.load()
			if lerr == nil {
				t.Fatalf("load(%q) returned no error", c.path)
			}
			var le *loadError
			if !errors.As(lerr, &le) {
				t.Fatalf("load error is %T, want *loadError", lerr)
			}
			if got := le.Error(); got != c.want {
				t.Errorf("Error() = %q, want %q", got, c.want)
			}
			// Unwrap must still reach the fs.PathError, so a caller can ask
			// errors.Is(err, fs.ErrNotExist).
			var pe *fs.PathError
			if !errors.As(lerr, &pe) {
				t.Errorf("the underlying *fs.PathError is not reachable through Unwrap")
			}
		})
	}
}

// TestCapitaliseLeavesNonASCIIAlone pins the guard rather than the happy path:
// every errno string Go carries is ASCII, and a first rune that is not an ASCII
// lower-case letter must come back byte-identical rather than half-encoded.
func TestCapitaliseLeavesNonASCIIAlone(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"no such file", "No such file"},
		{"Already capital", "Already capital"},
		{"é accented", "é accented"},
		{"1 leading digit", "1 leading digit"},
	} {
		if got := capitalise(c.in); got != c.want {
			t.Errorf("capitalise(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestErrnoTextFallsBackToTheWholeError: an error that is not a *fs.PathError has
// no errno to strip, and inventing a message would be worse than an unfamiliar
// one. The path argument names something that does not exist, so isDirectory
// cannot claim the case.
func TestErrnoTextFallsBackToTheWholeError(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent")
	if got := errnoText(errors.New("something else entirely"), absent); got != "Something else entirely" {
		t.Errorf("errnoText = %q, want the error's own text, capitalised", got)
	}
	// A PathError whose Err is nil has no errno at all, and (*PathError).Error()
	// would panic on it. os.ReadFile never builds one; the answer is still defined.
	if got := errnoText(&fs.PathError{Op: "open", Path: "x"}, absent); got != "Open x" {
		t.Errorf("errnoText of a PathError with no Err = %q, want %q", got, "Open x")
	}
}

// readDenied reports whether path really cannot be read, which is the precondition
// the EACCES row needs. It is asked rather than assumed: chmod 0000 denies nothing
// to root, and on Windows it denies nothing at all.
func readDenied(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.Open(path)
	if err == nil {
		f.Close()
		return false
	}
	return errors.Is(err, fs.ErrPermission)
}
