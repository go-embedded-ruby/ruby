package vm_test

import "testing"

// TestWaveETopLevelCPath is the behavioural half of the cpath fix: a leading
// `::` names the TOP-LEVEL constant whatever the enclosing lexical scope is,
// where a bare name names one in that scope. Every `want` below was taken from
// ruby 4.0.5 (2026-05-20 revision 64336ffd0e) on this machine.
//
// The `nested_*` rows are the known-bad control: they exercise the same syntax
// one `::` short, so a compiler that routed EVERY class/module definition to
// Object would pass all the `global_*` rows and fail these.
func TestWaveETopLevelCPath(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{
			"global_class_reopens_top_level",
			"class C; def self.tag; :top; end; end\n" +
				"module M; class ::C; def self.tag2; :reopened; end; end; end\n" +
				"p C.tag, C.tag2, M.const_defined?(:C, false)",
			":top\n:reopened\nfalse\n",
		},
		{
			"nested_class_stays_nested",
			"class C; def self.tag; :top; end; end\n" +
				"module M; class C; def self.tag2; :nested; end; end; end\n" +
				"p M::C.tag2, M.const_defined?(:C, false), C.respond_to?(:tag2)",
			":nested\ntrue\nfalse\n",
		},
		{
			"global_module_reopens_top_level",
			"module N; X = 1; end\n" +
				"module M; module ::N; Y = 2; end; end\n" +
				"p N::X, N::Y, M.const_defined?(:N, false)",
			"1\n2\nfalse\n",
		},
		{
			"nested_module_stays_nested",
			"module N; X = 1; end\n" +
				"module M; module N; Y = 2; end; end\n" +
				"p M::N::Y, M.const_defined?(:N, false), N.const_defined?(:Y, false)",
			"2\ntrue\nfalse\n",
		},
		{
			// Module.nesting read from the class BODY keeps the enclosing scope:
			// MRI pushes the class onto the opening frame's cref chain, it does not
			// restart one. (vm_cref_push; see also compile_cpath's
			// VM_DEFINECLASS_FLAG_SCOPED, vm_core.h-ruby_4_0:1237.)
			"global_class_body_nesting_keeps_the_enclosing_scope",
			"module M; class ::D; p Module.nesting; end; end",
			"[D, M]\n",
		},
		{
			"global_module_body_nesting_keeps_the_enclosing_scope",
			"module M; module ::E; p Module.nesting; end; end",
			"[E, M]\n",
		},
		{
			// A leading `::` is rooted at Object even from a doubly nested scope,
			// and the name it gives the class carries no prefix.
			"global_class_from_a_deep_scope",
			"module M; module N; class ::F; end; end; end\np F.name, defined?(M::N::F)",
			"\"F\"\nnil\n",
		},
		{
			// `class ::C < Super` takes the scoped path with BOTH operands.
			"global_class_with_a_superclass",
			"class Base; end\nmodule M; class ::G < Base; end; end\np G.name, G.superclass",
			"\"G\"\nBase\n",
		},
		{
			// At the top level the two spellings must agree — this is the case
			// the old comment was right about, and it must not regress.
			"at_top_level_the_two_spellings_agree",
			"class ::H; end\nclass H; K = 1; end\np H.name, H::K",
			"\"H\"\n1\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src:\n%s\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}
