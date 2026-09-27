// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestMethodSourceLocationReadsFirstLine is the unit witness for iseq_location
// (proc.c ruby_4_0:1515): the pair is (path, first_lineno). This reported a
// constant 0 as the line, so every Method and UnboundMethod placed itself on
// line 0 of the right file. A method with no ISeq (native) or an ISeq with no
// path (the prelude) still answers nil.
func TestMethodSourceLocationReadsFirstLine(t *testing.T) {
	loc := func(m *Method) object.Value { return methodSourceLocation(m) }

	withISeq := &Method{iseq: &bytecode.ISeq{File: "f.rb", FirstLine: 42}}
	got, ok := loc(withISeq).(*object.Array)
	if !ok {
		t.Fatalf("methodSourceLocation returned %T, want *object.Array", loc(withISeq))
	}
	if len(got.Elems) != 2 {
		t.Fatalf("source_location has %d elements, want 2", len(got.Elems))
	}
	if s, _ := got.Elems[0].(*object.String); s == nil || s.Str() != "f.rb" {
		t.Errorf("path = %v, want \"f.rb\"", got.Elems[0])
	}
	if n, _ := got.Elems[1].(object.Integer); int(n) != 42 {
		t.Errorf("line = %v, want 42", got.Elems[1])
	}

	// A proc-backed method (define_method) reports the block's ISeq, as
	// method_def_iseq's BMETHOD arm does.
	viaProc := &Method{proc: &Proc{iseq: &bytecode.ISeq{File: "g.rb", FirstLine: 7}}}
	if a, _ := loc(viaProc).(*object.Array); a == nil {
		t.Fatalf("a define_method method reported no location")
	} else if n, _ := a.Elems[1].(object.Integer); int(n) != 7 {
		t.Errorf("define_method line = %v, want 7", a.Elems[1])
	}

	// nil for a native method and for an ISeq with no path.
	if v := loc(&Method{}); !object.IsNil(v) {
		t.Errorf("native method reported %v, want nil", v)
	}
	if v := loc(&Method{iseq: &bytecode.ISeq{FirstLine: 9}}); !object.IsNil(v) {
		t.Errorf("ISeq with no path reported %v, want nil", v)
	}
}

// TestMethodSourceLocationEndToEnd runs the definitions through the VM. The
// harness compiles with no source path, so the file has to be supplied — which is
// where the two halves of this change meet: the definitions live inside an eval'd
// string given an explicit (file, line), and each one reports its own line within
// it. Every line was measured against ruby 4.0.5.
func TestMethodSourceLocationEndToEnd(t *testing.T) {
	// Lines 100..108 of "d.rb": the string's first line is 100.
	const src = `eval(<<~'RB', nil, "d.rb", 100)
	  class SLx
	    def plain; end
	    define_method(:dm) { }
	    def self.smeth; end
	    alias_method :ali, :plain
	  end
	  module SLMx
	    def modm; end
	  end
	RB
	`
	for _, c := range []struct{ expr, want string }{
		{`SLx.instance_method(:plain).source_location`, `["d.rb", 101]`},
		{`SLx.new.method(:plain).source_location`, `["d.rb", 101]`},
		{`SLx.instance_method(:dm).source_location`, `["d.rb", 102]`},
		{`SLx.method(:smeth).source_location`, `["d.rb", 103]`},
		// An alias reports the ORIGINAL definition, method_def_iseq's ALIAS arm.
		{`SLx.instance_method(:ali).source_location`, `["d.rb", 101]`},
		{`SLMx.instance_method(:modm).source_location`, `["d.rb", 107]`},
		// A singleton method defined on a plain object, outside the string.
		{`o = SLx.new; eval("def o.singly; end", binding, "e.rb", 5); o.method(:singly).source_location`, `["e.rb", 5]`},
		// Two elements, not MRI 4.0's five-element doc comment: iseq_location
		// builds rb_ary_new4(2, loc).
		{`SLx.instance_method(:plain).source_location.size`, `2`},
		// A native method has none.
		{`method(:p).source_location`, `nil`},
	} {
		got := eval(t, src+"p ("+c.expr+")")
		if got != c.want+"\n" {
			t.Errorf("%s got=%q want=%q", c.expr, got, c.want+"\n")
		}
	}
}
