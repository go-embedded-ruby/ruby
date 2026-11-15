// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import "testing"

// ⛔ #909: MRI 4.0 warns when Enumerable#to_set is passed arguments, and rbgo was
// silent. Measured through cmd/oracle against MRI 4.0.7, and THREE of the six
// cases are not what "with arguments warns" would lead one to write:
//
//	[1,2].to_set            silent
//	[1,2].to_set(Set)       warns     <- even though Set IS the default value
//	[1,2].to_set { … }      SILENT    <- a block alone is not "arguments"
//	(1..3).to_set           silent
//	(1..3).to_set(Set)      warns     <- through Range#to_set
//	Set[1,2].to_set(Set)    SILENT    <- Set#to_set does not warn at all
//
// A default parameter cannot express the first two: inside
// `def to_set(klass = Set, …)`, `to_set` and `to_set(Set)` are identical. The
// test is arity.
//
// ⛔ AND THE LINE NUMBER IS PART OF THE CONTRACT, in a way that is worse than
// "the wrong frame". With the warning at `uplevel: 1` the prefix mixes the
// CALLER'S PATH with the PRELUDE'S LINE:
//
//	MRI   probe.rb:15     and probe.rb:16
//	rbgo  probe.rb:214    and probe.rb:1208        <- a 16-line file
//
// So the message names a real file at an impossible line. In MRI both to_set
// are C and add no Ruby frame; rbgo's prelude is Ruby and does. Same shape as
// [a line number that belongs to the parser, not the document].
//
// ⚠ The first version of this test asserted `msg.include?("prelude") == false`
// and PASSED under that mutation, because the message never says "prelude" —
// it says the user's filename with the prelude's line. A test written for a
// property it cannot see is worse than no test, so the assertion is now on the
// NUMBER, taken from `__LINE__` at the call site.
func TestEnumerableToSetWarnsOnlyWhereMRIDoes(t *testing.T) {
	got := runSrc(t, `require "set"
require "stringio"

class MySet < Set; end

def warned(&blk)
  prev = $stderr
  $stderr = StringIO.new
  begin
    blk.call
    $stderr.string
  ensure
    captured = $stderr.string
    $stderr = prev
    captured
  end
end

p warned { [1, 2].to_set }.empty?
p warned { [1, 2].to_set(Set) }.include?("passing arguments to Enumerable#to_set is deprecated")
p warned { [1, 2].to_set(MySet) }.empty? == false
p warned { [1, 2].to_set { |x| x } }.empty?
p warned { (1..3).to_set }.empty?
p warned { (1..3).to_set(Set) }.empty? == false
p warned { Set[1, 2].to_set(Set) }.empty?
p warned { Set[1, 2].to_set(MySet) }.empty?

# ⛔ The prefix must name the CALLER'S line. Asserting on the NUMBER, because
# the filename is the caller's either way -- that is what made the first
# version of this check pass under a broken uplevel.
direct_line = nil
msg = warned { direct_line = __LINE__; [1, 2].to_set(Set) }
p msg.include?(":#{direct_line}:")

range_line = nil
msg = warned { range_line = __LINE__; (1..3).to_set(Set) }
p msg.include?(":#{range_line}:")
`)
	want := "true\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue"
	if got != want {
		t.Errorf("Enumerable#to_set deprecation:\ngot  %q\nwant %q", got, want)
	}
}
