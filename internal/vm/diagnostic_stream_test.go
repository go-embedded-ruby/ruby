package vm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

// evalSplit runs src on a VM whose program output and diagnostic stream are two
// DISTINCT buffers, the way cmd/rbgo wires os.Stdout and os.Stderr, and returns
// them separately. A runtime error is returned rather than fatal so a program
// that ends in SystemExit (Kernel#abort) can still be inspected.
func evalSplit(t *testing.T, src string) (out, errOut string, runErr error) {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	iseq.Name = "diag.rb"
	var o, e bytes.Buffer
	machine := vm.NewWithStderr(&o, &e)
	// The CLI names the script so a warning's "path:lineno: " prefix matches what
	// MRI prints for the same file; without it the frame label is "(rbgo)".
	machine.SetScriptName("diag.rb")
	_, runErr = machine.Run(iseq)
	return o.String(), e.String(), runErr
}

// TestDiagnosticsReachStderrNotStdout is the regression test for #667: vm.New
// set errOut = out and cmd/rbgo passed os.Stdout for both, so every diagnostic
// landed in the program's DATA stream — `rbgo gen.rb > out.json` got warning
// text in the payload.
//
// Each case asserts BOTH streams. Checking only the text cannot see this defect:
// the message was present either way, only on the wrong descriptor. The wanted
// text was taken from MRI 4.0.5 run as `ruby prog.rb 2>/dev/null` and
// `ruby prog.rb 2>&1 >/dev/null`.
func TestDiagnosticsReachStderrNotStdout(t *testing.T) {
	cases := []struct {
		name             string
		src              string
		wantOut, wantErr string
	}{
		// error.c rb_warn, reached from array.c rb_ary_index's
		// `if (rb_block_given_p()) rb_warn("given block not used")`.
		{
			name:    "rb_warn",
			src:     "x = [1, 2].index(1) { :blk }\nputs \"data\"\n",
			wantOut: "data\n",
			wantErr: "diag.rb:1: warning: given block not used\n",
		},
		// Kernel#warn, which routes through Warning.warn like rb_warn_category.
		{
			name:    "kernel_warn",
			src:     "warn \"careful\"\nputs \"data\"\n",
			wantOut: "data\n",
			wantErr: "careful\n",
		},
		// $stderr and STDERR are the same descriptor in MRI (both start at the C
		// stderr FILE*), and neither is stdout.
		{
			name:    "stderr_writes",
			src:     "$stderr.puts \"via global\"\nSTDERR.puts \"via const\"\nputs \"data\"\n",
			wantOut: "data\n",
			wantErr: "via global\nvia const\n",
		},
		// error.c's quieter rb_warning sibling, gated on $VERBOSE == true.
		{
			name:    "rb_warning_verbose",
			src:     "$VERBOSE = true\nx = Array.new(2, :dflt) { :blk }\nputs \"data\"\n",
			wantOut: "data\n",
			wantErr: "diag.rb:2: warning: block supersedes default value argument\n",
		},
		// The deprecation path: variable.c's rb_const_warn_if_deprecated, emitted
		// through Warning.warn only while Warning[:deprecated] is enabled.
		{
			name:    "deprecated_constant",
			src:     "Warning[:deprecated] = true\nclass Foo; BAR = 1; deprecate_constant :BAR; end\nFoo::BAR\nputs \"data\"\n",
			wantOut: "data\n",
			wantErr: "warning: constant Foo::BAR is deprecated\n",
		},
		// process.c rb_f_abort puts the message on rb_ractor_stderr() before
		// raising SystemExit — the message is a diagnostic, not output.
		{
			name:    "abort_message",
			src:     "puts \"data\"\nabort \"fatal\"\n",
			wantOut: "data\n",
			wantErr: "fatal\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, _ := evalSplit(t, tc.src)
			if out != tc.wantOut {
				t.Errorf("stdout = %q, want %q", out, tc.wantOut)
			}
			if errOut != tc.wantErr {
				t.Errorf("stderr = %q, want %q", errOut, tc.wantErr)
			}
		})
	}
}

// TestNewMergesDiagnosticsIntoOut pins the other half of the contract: vm.New
// keeps writing diagnostics to its single writer, so an embedder capturing a
// program's whole output in one buffer (ruby.Run, cmd/wasm) still sees warnings.
// Splitting them unconditionally would silently drop diagnostics for those
// callers rather than route them.
func TestNewMergesDiagnosticsIntoOut(t *testing.T) {
	prog, err := parser.Parse("x = [1, 2].index(1) { :blk }\nputs \"data\"\n")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	iseq.Name = "diag.rb"
	var buf bytes.Buffer
	machine := vm.New(&buf)
	machine.SetScriptName("diag.rb")
	if _, err := machine.Run(iseq); err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	const want = "diag.rb:1: warning: given block not used\ndata\n"
	if buf.String() != want {
		t.Errorf("merged writer = %q, want %q", buf.String(), want)
	}
}

// TestReassignedStderrCapturesWarnings is the property that hid #667 from the
// conformance suite, and has to keep holding: mspec's `complain` matcher swaps
// the Ruby-level $stderr for a StringIO, and every diagnostic must still go
// there rather than to the real stream — error.c rb_write_error_str writes to
// rb_ractor_stderr(), i.e. to whatever $stderr currently is.
//
// The assertion that matters is the NEGATIVE one: with $stderr swapped, neither
// the program's stdout nor the VM's own stderr sink may receive the warning. A
// test that only checked the StringIO would pass even if the warning were
// duplicated onto the wrong descriptor.
//
// This one passes both before and after the split — measured, by forcing errOut
// back to out: it is a guard on the $stderr routing, not a witness of #667. The
// witnesses are TestDiagnosticsReachStderrNotStdout (all six cases fail under
// the old wiring) and TestSplitStreamsShareNothing.
func TestReassignedStderrCapturesWarnings(t *testing.T) {
	out, errOut, err := evalSplit(t, `require "stringio"
old, $stderr = $stderr, StringIO.new
x = [1, 2].index(1) { :blk }
warn "also captured"
captured = $stderr.string
$stderr = old
puts captured.inspect
`)
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	const want = "\"diag.rb:3: warning: given block not used\\nalso captured\\n\"\n"
	if out != want {
		t.Errorf("captured warnings = %q, want %q", out, want)
	}
	if errOut != "" {
		t.Errorf("stderr sink got %q while $stderr was swapped; it must stay empty", errOut)
	}
}

// TestSplitStreamsShareNothing guards the wiring itself rather than a message:
// the two writers must be independent, so a program writing to both leaves each
// buffer holding only its own half. This is the assertion that fails if errOut
// is ever pointed back at out.
func TestSplitStreamsShareNothing(t *testing.T) {
	out, errOut, err := evalSplit(t, "$stdout.print \"O\"\n$stderr.print \"E\"\n$stdout.print \"O\"\n$stderr.print \"E\"\n")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if out != "OO" {
		t.Errorf("stdout = %q, want %q", out, "OO")
	}
	if errOut != "EE" {
		t.Errorf("stderr = %q, want %q", errOut, "EE")
	}
	if strings.ContainsAny(out, "E") || strings.ContainsAny(errOut, "O") {
		t.Errorf("streams bled into each other: stdout=%q stderr=%q", out, errOut)
	}
}
