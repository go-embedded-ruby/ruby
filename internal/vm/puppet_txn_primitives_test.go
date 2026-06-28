// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestPuppetTxnPrimitives covers the primitive fixes that let Puppet build a
// real catalog from a resource declaration (`notify { ... }`) and turn it into
// the Resource Abstraction Layer (to_ral). Each case was reduced from the apply
// trace to a minimal rbgo-vs-MRI snippet and is asserted against MRI Ruby 4.0.5.
func TestPuppetTxnPrimitives(t *testing.T) {
	const extendSetup = "module M; end\nclass C; extend M; end\nclass D < C; end\n"
	cases := []struct{ src, want string }{
		// Hash#fetch with a block default: Puppet's Pops AST does
		// `init_hash.fetch('form') { "regular" }` while parsing a resource
		// declaration. The block is invoked with the missing key only when absent.
		{`p({"a" => 1}.fetch("zz") { "regular" })`, "\"regular\"\n"},
		{`p({"a" => 1}.fetch("a") { "regular" })`, "1\n"},
		{`p({}.fetch("zz") { |k| "blk-#{k}" })`, "\"blk-zz\"\n"},
		{`p({"a" => 1}.fetch("a", 99))`, "1\n"}, // present key beats positional default
		{`p({}.fetch("a", 99))`, "99\n"},        // positional default when absent and no block

		// Array#replace swaps contents for a copy of another array and returns self.
		// Used by Puppet::Resource#initialize (@sensitive_parameters.replace(...)).
		{`a = [1, 2, 3]; a.replace([4, 5]); p a`, "[4, 5]\n"},
		{`a = []; a.replace([:x, :y]); p a`, "[:x, :y]\n"},
		{`a = [1]; p a.replace([2]).equal?(a)`, "true\n"},              // returns self
		{`a = [1]; b = [2, 3]; a.replace(b); b << 4; p a`, "[2, 3]\n"}, // source copied, not aliased

		// is_a?/kind_of? through `extend`: after `C.extend(M)`, C and every subclass
		// of C are is_a?(M) because M is inserted into the singleton (meta) class
		// ancestry. This is what makes Puppet::Type (which does
		// `extend Puppet::CompilableResourceType`) and its subtypes (notify, file,
		// …) report as compilable resource types in Resource#initialize.
		{extendSetup + `p C.is_a?(M)`, "true\n"},
		{extendSetup + `p D.is_a?(M)`, "true\n"},                               // subclass inherits the extend
		{extendSetup + `p D.kind_of?(M)`, "true\n"},                            // kind_of? is the alias
		{extendSetup + `p D.new.is_a?(M)`, "false\n"},                          // instances are NOT extended
		{extendSetup + `p C.is_a?(Class)`, "true\n"},                           // ordinary class membership still holds
		{`module M; end; o = Object.new; o.extend(M); p o.is_a?(M)`, "true\n"}, // per-object extend
		{`module M; end; p Object.new.is_a?(M)`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`{}.fetch("nope")`, `key not found: "nope"`},
		{`[1].replace(5)`, "no implicit conversion of Integer into Array"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
