// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestKernelTest covers Kernel#test: the boolean/size/time file tests delegated
// to File, the ?x-character (Integer) and one-character-String command forms, the
// private visibility (not in respond_to?), and the error paths. Files are created
// under Dir.mktmpdir. Asserted against MRI Ruby 4.0.6.
func TestKernelTest(t *testing.T) {
	pre := `require "tmpdir"
Dir.mktmpdir do |d|
  f = File.join(d, "a"); File.write(f, "hi")
  e = File.join(d, "e"); File.write(e, "")
  `
	c := func(body string) string { return pre + body + "\nend" }
	cases := []struct{ src, want string }{
		{c(`p [test(?e, f), test(?f, f), test(?d, d), test(?d, f)]`), "[true, true, true, false]\n"},
		{c(`p [test(?r, f), test(?w, f), test(?R, f), test(?W, f)]`), "[true, true, true, true]\n"},
		{c(`p [test(?s, f), test(?s, e), test(?z, f), test(?z, e)]`), "[2, nil, false, true]\n"},
		{c(`p [test(?M, f).class, test(?A, f).class, test(?C, f).class]`), "[Time, Time, Time]\n"},
		{c(`p test("e", f)`), "true\n"}, // a one-character String command
		{c(`p test(?e, "/no/such/path")`), "false\n"},
		{c(`p respond_to?(:test)`), "false\n"}, // Kernel#test is private
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
	errs := []struct{ src, want string }{
		{`test(?e)`, "wrong number of arguments"},
		{`test(?Q, "x")`, "unknown command"},
		{`test("", "x")`, "not a valid command"},
		{`test(Object.new, "x")`, "no implicit conversion of Object into Integer"},
	}
	for _, tc := range errs {
		if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("src=%q err=%v, want substring %q", tc.src, err, tc.want)
		}
	}
}
