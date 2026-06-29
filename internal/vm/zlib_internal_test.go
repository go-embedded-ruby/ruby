// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
	gozlib "github.com/go-ruby-zlib/zlib"
)

// TestZlibWrappersAndFallback exercises the parts of the Zlib binding that are
// not reachable through normal Ruby execution: the object.Value boilerplate on
// the internal streaming wrappers (they live in an ivar and are never inspected
// from Ruby), and raiseZlib's fallback for an error that is not a *zlib.Error.
func TestZlibWrappersAndFallback(t *testing.T) {
	d := zlibDeflater{d: gozlib.NewDeflater(gozlib.DefaultCompression)}
	i := zlibInflater{i: gozlib.NewInflater()}
	for _, w := range []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{d, i} {
		if !w.Truthy() {
			t.Errorf("%T.Truthy() = false, want true", w)
		}
		if w.ToS() != w.Inspect() {
			t.Errorf("%T ToS=%q Inspect=%q, want equal", w, w.ToS(), w.Inspect())
		}
		if !strings.HasPrefix(w.ToS(), "#<Zlib::") {
			t.Errorf("%T.ToS() = %q, want #<Zlib::… form", w, w.ToS())
		}
	}

	// raiseZlib with a *zlib.Error preserves its MRI class; with any other error
	// it falls back to Zlib::Error carrying the error's message.
	wantClass := func(err error, class, msgPart string) {
		t.Helper()
		defer func() {
			r := recover()
			re, ok := r.(RubyError)
			if !ok {
				t.Fatalf("raiseZlib(%v) panicked with %T, want RubyError", err, r)
			}
			if re.Class != class {
				t.Errorf("raiseZlib(%v) class = %q, want %q", err, re.Class, class)
			}
			if !strings.Contains(re.Message, msgPart) {
				t.Errorf("raiseZlib(%v) msg = %q, want contains %q", err, re.Message, msgPart)
			}
		}()
		raiseZlib(err)
	}
	wantClass(gozlib.ErrData, "Zlib::DataError", "header")
	wantClass(errors.New("boom"), "Zlib::Error", "boom")
}

// TestZlibStreamStrictness covers the streaming error branches the library
// raises but MRI tolerates (so they are not asserted as MRI parity in the
// behavioural test): re-finishing a deflater and inflating after an inflater is
// finished both surface the library's Zlib::StreamError.
func TestZlibStreamStrictness(t *testing.T) {
	cases := []string{
		// #finish after #finish -> the library's Deflate Finish returns ErrStream.
		`z = Zlib::Deflate.new; z.finish; z.finish`,
		// #inflate after the inflater is finished -> ErrStream (via #<<).
		`d = Zlib::Deflate.deflate("abc"); inf = Zlib::Inflate.new; inf.inflate(d); inf << d`,
	}
	for _, src := range cases {
		prog, err := parser.Parse("require \"zlib\"\n" + src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		iseq, err := compiler.Compile(prog)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		_, err = New(&bytes.Buffer{}).Run(iseq)
		if err == nil || !strings.Contains(err.Error(), "Zlib::StreamError") {
			t.Errorf("src=%q got %v, want Zlib::StreamError", src, err)
		}
	}
}
