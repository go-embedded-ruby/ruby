// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// capture installs a Warning.warn that records the message (with the file name
// folded away, so the expectation is about the LINE and the text) and the
// category, which is how MRI delivers rb_category_warn.
const capture = `
$seen = []
module Warning
  def self.warn(msg, category: nil)
    $seen << [msg.sub(/\A.*?:(\d+): /, 'L\1: '), category]
  end
end
`

// ObjectSpace._id2ref is deprecated in Ruby 4.0 — gc.c calls
// rb_category_warn(RB_WARN_CATEGORY_DEPRECATED, "ObjectSpace._id2ref is
// deprecated"). It still answers; it only says so.
//
// ⛔ THE GATE IS TWO CONDITIONS and only one is obvious. MRI's
// deprecation_warning_enabled() wants a NON-NIL $VERBOSE *and*
// Warning[:deprecated]. Measured on MRI 4.0.7: `$VERBOSE = nil` with the
// category on is silent; `$VERBOSE = false` with it on warns; `$VERBOSE = true`
// with the category OFF is silent. Every row below is one of those runs.
func TestId2refIsDeprecated(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"it warns when the category is on",
			capture + `Warning[:deprecated] = true
ObjectSpace._id2ref(nil.object_id)
p $seen`,
			`[["L9: warning: ObjectSpace._id2ref is deprecated\n", :deprecated]]`},

		{"the category reaches a Warning.warn override",
			capture + `Warning[:deprecated] = true
ObjectSpace._id2ref(nil.object_id)
p $seen[0][1]`,
			`:deprecated`},

		{"it is silent when the category is off",
			capture + `Warning[:deprecated] = false
ObjectSpace._id2ref(nil.object_id)
p $seen`,
			`[]`},

		// ⛔ $VERBOSE = nil silences it even with the category on. Checking the
		// category alone — which is what this repository's `iterator?` did
		// before warnDeprecated existed — makes rbgo speak where MRI says
		// nothing, and nil is how a program asks for exactly that silence.
		{"a nil $VERBOSE silences it",
			capture + `Warning[:deprecated] = true
$VERBOSE = nil
ObjectSpace._id2ref(nil.object_id)
p $seen`,
			`[]`},

		{"a false $VERBOSE does not silence it",
			capture + `Warning[:deprecated] = true
$VERBOSE = false
ObjectSpace._id2ref(nil.object_id)
p $seen.size`,
			`1`},

		{"a true $VERBOSE alone is not enough",
			capture + `$VERBOSE = true
ObjectSpace._id2ref(nil.object_id)
p $seen`,
			`[]`},

		// The deprecation does not replace the behaviour.
		{"it still answers",
			capture + `Warning[:deprecated] = true
s = "hi"
p ObjectSpace._id2ref(s.object_id).equal?(s)`,
			`true`},

		// ⛔ The arity check stays FIRST: calling it with no argument raises
		// without warning, as MRI does. A warning emitted before the check
		// would make a plain mistake look like a deprecation.
		{"a missing argument raises without warning",
			capture + `Warning[:deprecated] = true
begin; ObjectSpace._id2ref; rescue ArgumentError; end
p $seen`,
			`[]`},

		// The same gate, on the method warnDeprecated was extracted from.
		{"iterator? follows the same gate",
			capture + `Warning[:deprecated] = true
$VERBOSE = nil
iterator?
$VERBOSE = false
iterator?
p $seen.map { |m, c| [m[/L\d+/], c] }`,
			`[["L12", :deprecated]]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}
