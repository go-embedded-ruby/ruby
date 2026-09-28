// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestNameErrorRecordsItsReceiver covers the paths where ruby 4.0.5 raises a
// NameError through rb_name_err_raise, whose FIRST argument is the exception's
// receiver. rbgo built the same message on each and left #receiver nil.
//
// Every want below is the byte-for-byte output of the local MRI 4.0.5
// (arm64-darwin25) on that exact source.
func TestNameErrorRecordsItsReceiver(t *testing.T) {
	const probe = "rescue NameError => e; p [e.message, e.name, e.receiver]; end\n"
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			// rb_const_get_0 -> rb_const_missing -> the default
			// Module#const_missing -> uninitialized_constant (variable.c
			// r4:3085/2342/2282). The receiver is the module the lookup
			// started from.
			name: "const_get",
			src:  "module Q; end\nbegin; Q.const_get(:Nope); " + probe,
			want: "[\"uninitialized constant Q::Nope\", :Nope, Q]\n",
		},
		{
			// The hook called DIRECTLY must answer the same way: it is the
			// same C function, so a second message-building path here would
			// be a place for the two to drift.
			name: "const_missing called directly",
			src:  "module Q; end\nbegin; Q.const_missing(:Nope); " + probe,
			want: "[\"uninitialized constant Q::Nope\", :Nope, Q]\n",
		},
		{
			// uninitialized_constant qualifies only when rb_class_real(klass)
			// is not rb_cObject, so the top level is NOT spelled out -- but
			// the receiver is still recorded, and is Object.
			name: "Object omits the qualifier but keeps the receiver",
			src:  "begin; Object.const_get(:Zap); " + probe,
			want: "[\"uninitialized constant Zap\", :Zap, Object]\n",
		},
		{
			// A SINGLETON scope has an empty .name and still qualifies,
			// through rb_class_path.
			name: "singleton scope",
			src:  "class S; end\nbegin; S.singleton_class.const_get(:Nope); " + probe,
			want: "[\"uninitialized constant #<Class:S>::Nope\", :Nope, #<Class:S>]\n",
		},
		{
			// rb_mod_instance_method -> rb_method_entry_without_refinements
			// miss -> rb_name_err_raise with the module.
			name: "instance_method",
			src:  "module M; end\nbegin; M.instance_method(:nope); " + probe,
			want: "[\"undefined method 'nope' for module 'M'\", :nope, M]\n",
		},
		{
			// Module#private on a name that is not there.
			name: "visibility change",
			src:  "class K2; end\nbegin; K2.send(:private, :nope); " + probe,
			want: "[\"undefined method 'nope' for class 'K2'\", :nope, K2]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestNameErrorRecordsNoReceiverWhereMRIRecordsNone is the other direction, and
// it is the half that keeps the fix honest: "record a receiver everywhere" would
// be wrong. On ruby 4.0.5 the global-variable NameErrors carry NONE, which
// #receiver reports by raising ArgumentError("no receiver is available") --
// measured, not assumed. rbgo answers nil rather than raising (see the
// #receiver reader for the count of sites that still block it), so what is
// pinned here is that these paths do not acquire a receiver they should not
// have.
func TestNameErrorRecordsNoReceiverWhereMRIRecordsNone(t *testing.T) {
	src := "begin; untrace_var(:$nope_uv); rescue NameError => e; p [e.message, e.name, e.receiver]; end\n"
	const want = "[\"undefined global variable $nope_uv\", :$nope_uv, nil]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestConstMissingMessageSharesOneRenderer: the default hook and the bytecode
// path must build the same message, because MRI builds both with
// uninitialized_constant. They used to be built two ways, and the second way
// dropped the qualifier for a scope whose .name is empty.
func TestConstMissingMessageSharesOneRenderer(t *testing.T) {
	// A qualified name through the BYTECODE path and through const_get must
	// agree, for a named and for a singleton scope.
	src := `module Q; def self.bare; Nope; end; end
begin; Q.bare; rescue NameError => e; print e.message, "\n"; end
begin; Q.const_get(:Nope); rescue NameError => e; print e.message, "\n"; end
class S; end
begin; S.singleton_class.const_get(:Nope); rescue NameError => e; print e.message, "\n"; end
`
	const want = "uninitialized constant Q::Nope\nuninitialized constant Q::Nope\nuninitialized constant #<Class:S>::Nope\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
