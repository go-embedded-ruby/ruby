// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// ⛔ Every splat form asked nil for #to_a. MRI 4.0's vm_splat_array tests NIL_P
// BEFORE rb_check_to_array (vm_insnhelper.c:5746 at v4.0.9), so it asks nothing.
//
// The visible symptom in ruby/spec is only a forbidden mock call —
// language/array_spec.rb:160 "does not call #to_a on nil" and
// language/variables_spec.rb:367 "converts nil to empty array without calling a
// method", both gated `ruby_version_is "4.0"`. The real one is worse, and the
// commit this test accompanies did not name it: with NilClass#to_a redefined,
// the splat TOOK ITS ANSWER, so `[*nil]` evaluated to [1, 2] here where 4.0.7
// gives []. A wrong value, not a stray call.
//
// Every row was measured against MRI 4.0.7 on this host. Without the NIL_P guard
// nine of them fail and the fourteen controls still pass, which is what makes
// them controls.
func TestSplatOfNilDoesNotAskForToA(t *testing.T) {
	// Counts the sends rather than only the result: the values already agreed
	// before the fix in every form but the redefinition below, which is why two
	// spec examples and nothing else caught it.
	const count = `
$called = 0
class NilClass
  alias_method :orig_to_a, :to_a
  def to_a; $called += 1; orig_to_a; end
end
`
	for _, tc := range []struct{ name, src, want string }{
		{"a literal splat", count + `x = [*nil]; p [x, $called]`, `[[], 0]`},
		{"a bare splat assignment", count + `a = *nil; p [a, $called]`, `[[], 0]`},
		{"a splatted LHS", count + `(*a = *nil); p [a, $called]`, `[[], 0]`},
		{"a splat before other elements", count + `x = [1, *nil]; p [x, $called]`, `[[1], 0]`},
		{"a splat after other elements", count + `x = [*nil, 1]; p [x, $called]`, `[[1], 0]`},
		{"a splatted argument", count + `def m(*a) = a
p [m(*nil), $called]`, `[[], 0]`},
		{"a multiple assignment", count + `x, y = *nil; p [[x, y], $called]`, `[[nil, nil], 0]`},
		{"two splats in one literal", count + `x = [*nil, *nil]; p [x, $called]`, `[[], 0]`},

		// ⛔ The decisive row. Before the fix this answered [1, 2]: the splat read
		// a value out of a redefinable method. A fix that only stopped RECORDING
		// the call would still fail here.
		{"a redefined NilClass#to_a is ignored",
			`class NilClass; def to_a = [1, 2]; end
p [*nil]`, `[]`},

		// ⛔ Kernel#Array is rb_Array, NOT vm_splat_array, and still asks — so the
		// guard must sit in the splat helper and not in anything the two share.
		// Measured on 4.0.7: with the same redefinition, Array(nil) is [1, 2].
		{"Kernel#Array still asks nil", count + `x = Array(nil); p [x, $called]`, `[[], 1]`},
		{"Kernel#Array obeys a redefined #to_a",
			`class NilClass; def to_a = [1, 2]; end
p Array(nil)`, `[1, 2]`},
		{"nil#to_a itself still answers", count + `p [nil.to_a, $called]`, `[[], 1]`},

		// ⛔ The empty Array must be FRESH. MRI's flag-false branch returns the
		// shared rb_cArray_empty_frozen; handing back one package-level Array here
		// would let the first caller's push be seen by the next. No splat form
		// reaches user code with the frozen one — `[*nil].frozen?` is false on
		// 4.0.7 — so the shared instance would buy nothing and cost this.
		{"each splat yields its own Array", `a = [*nil]; a << 1; p [a, [*nil]]`, `[[1], []]`},
		{"two splat results are not the same object", `a = (b = *nil); c = (d = *nil)
p a.equal?(c)`, `false`},
		{"the result is not frozen", `a = *nil; p [a.frozen?, [*nil].frozen?]`, `[false, false]`},

		// Controls: everything that is not nil must still be asked, exactly as before.
		{"an object without #to_a is wrapped",
			`class NoConv; end
p [*NoConv.new].size`, `1`},
		{"an object with #to_a is converted",
			`class Conv; def to_a = [7, 8]; end
p [*Conv.new]`, `[7, 8]`},
		{"a #to_a that answers nil declines",
			`class NilConv; def to_a = nil; end
p [*NilConv.new].size`, `1`},
		{"false is not nil", `p [*false]`, `[false]`},
		{"an Integer is wrapped", `p [*1]`, `[1]`},
		{"a Hash is converted", `p [*{a: 1}]`, `[[:a, 1]]`},
		{"a Range is converted", `p [*(1..3)]`, `[1, 2, 3]`},
		{"an Array subclass still copies to a plain Array",
			`sub = Class.new(Array)
x = *sub.new([1, 2])
p [x, x.instance_of?(Array)]`, `[[1, 2], true]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}
