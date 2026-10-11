// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// TestNativeFaultBecomesRubyError covers the embedding safety net: a native
// binding that indexes past a missing argument raises a Go runtime fault, which
// callNative converts into a rescuable Ruby ArgumentError rather than crashing
// the host.
//
// This used to be witnessed by NDArray.arange(6), which indexed a second
// argument it never checked for. The binding now checks every argument, so the
// fault is planted here instead, on a native that exists only in this test:
// the safety net must not depend on some binding still being broken.
func TestNativeFaultBecomesRubyError(t *testing.T) {
	run := func(src string) (string, error) {
		t.Helper()
		prog, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		iseq, err := compiler.Compile(prog)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		var buf bytes.Buffer
		machine := New(&buf)
		machine.cObject.define("fault_on_missing_arg", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return args[1] // index out of range with fewer than two arguments
		})
		_, err = machine.Run(iseq)
		return buf.String(), err
	}

	_, err := run(`fault_on_missing_arg(6)`)
	if err == nil {
		t.Fatal("expected an error, got none")
	}
	if re, ok := err.(RubyError); !ok || re.Class != "ArgumentError" {
		t.Fatalf("got %#v, want an ArgumentError", err)
	}
	if !strings.Contains(err.Error(), "index out of range") {
		t.Fatalf("error %q should describe the underlying fault", err.Error())
	}
	// Rescuable from Ruby, and the VM keeps running afterwards.
	out, err := run(`p (begin; fault_on_missing_arg(6); rescue ArgumentError; "caught"; end)`)
	if err != nil || out != "\"caught\"\n" {
		t.Fatalf("rescue got %q, %v; want %q", out, err, "\"caught\"\n")
	}
}
