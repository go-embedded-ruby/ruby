// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

const toProcPre = `class Specs
  def makeproc(&b); b; end
end
S = Specs.new
def try
  yield
rescue Exception => e
  puts "#{e.class}: #{e.message}"
end
`

// A &block-pass that is neither nil nor a Proc fails with a TypeError — NEVER a
// NoMethodError — and MRI has two distinct messages for the two ways it fails.
// vm_to_proc (vm_args.c at v4.0.9) looks the method up first:
//
//	me = rb_callable_method_entry_with_refinements(CLASS_OF(proc), idTo_proc, NULL);
//	if (me) { b = rb_vm_call0(…); }   else { b = rb_check_convert_type_with_id(…); }
//
// ⛔ The class was never NoMethodError in MRI. 2.6.10 already answered
// `TypeError: wrong argument type Object (expected Proc)`; 4.0 changed the
// WORDING, not the class, so this is a plain defect and only the two messages
// belong to the 4.0 move.
//
// Every expectation was measured on
// `ruby 4.0.7 (2026-09-15 revision 229531a6cf) +PRISM [arm64-darwin25]`
// through scripts/conformance/rubyspec/cmd/oracle.
func TestABlockPassThatIsNotAProcFailsWithTypeError(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// ⛔ The decisive row: language/send_spec.rb "raises TypeError if block
		// object isn't a Proc and doesn't respond to `to_proc`". This answered
		// NoMethodError, which is the wrong class and not a wording question.
		{"an object with no to_proc at all",
			toProcPre + `try { S.makeproc(&Object.new) }`,
			"TypeError: no implicit conversion of Object into Proc"},

		// The other message: a to_proc that exists and answers something else.
		{"a to_proc that returns a non-Proc names what it gave",
			toProcPre + `class RawToProc
  def to_proc; 42; end
end
try { S.makeproc(&RawToProc.new) }`,
			"TypeError: can't convert RawToProc to Proc (RawToProc#to_proc gives Integer)"},

		// ⛔ nil from to_proc is the same failure, not a missing block: the
		// message names NilClass rather than falling back to "no implicit
		// conversion", because a to_proc WAS found and did answer.
		{"a to_proc that returns nil is still the convert message",
			toProcPre + `class NilToProc
  def to_proc; nil; end
end
try { S.makeproc(&NilToProc.new) }`,
			"TypeError: can't convert NilToProc to Proc (NilToProc#to_proc gives NilClass)"},

		// ⛔ The three rows the obvious predicate would have broken. MRI looks up
		// a METHOD ENTRY, not respond_to?, so a private to_proc is used and a
		// to_proc reachable only through method_missing is used — with or
		// without respond_to_missing?. These already worked; gating the new
		// refusal on respondsToDynamic would have made them raise.
		{"a private to_proc is used",
			toProcPre + `class PrivToProc
  private def to_proc; -> { :priv }; end
end
p S.makeproc(&PrivToProc.new).call`, ":priv"},

		{"a to_proc through method_missing is used",
			toProcPre + `class MMToProc
  def method_missing(n, *a) = n == :to_proc ? -> { :mm } : super
  def respond_to_missing?(n, priv = false) = n == :to_proc || super
end
p S.makeproc(&MMToProc.new).call`, ":mm"},

		{"a to_proc through method_missing without respond_to_missing? is used",
			toProcPre + `class MMOnly
  def method_missing(n, *a) = n == :to_proc ? -> { :mmonly } : super
end
p S.makeproc(&MMOnly.new).call`, ":mmonly"},

		{"a private to_proc that answers a non-Proc still names the class",
			toProcPre + `class PrivBad
  private def to_proc; 42; end
end
try { S.makeproc(&PrivBad.new) }`,
			"TypeError: can't convert PrivBad to Proc (PrivBad#to_proc gives Integer)"},

		// ⛔ Controls: the two shapes that must NOT reach the conversion at all.
		{"&nil means no block and raises nothing",
			toProcPre + `p S.makeproc(&nil)`, "nil"},

		{"a Symbol still converts",
			toProcPre + `p S.makeproc(&:upcase).call("ab")`, `"AB"`},

		{"a Proc is passed through untouched",
			toProcPre + `l = -> { :given }
p S.makeproc(&l).call`, ":given"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}
