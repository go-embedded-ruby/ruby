// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestPathnameSlashIsAnAliasOfPlus: CRuby's pathname.rb writes `alias / +`.
// rbgo's prelude wrote a forwarding `def /(other); self + other; end` instead,
// because the parser refused the alias -- after `alias`, a `/` opened a regexp
// (fixed in go-ruby-parser v0.11.1, which this requires).
//
// The workaround was not only a reflection difference. Three things diverged,
// all measured against ruby 4.0.5:
//
//	Pathname.instance_method(:/).original_name        ruby :+     before :/
//	instance_method(:/) == instance_method(:+)        ruby true   before false
//	after `def +` is redefined, `a / "b"`             ruby /a/b   before HIJACKED
//
// The third is behaviour: an alias keeps the body it was made from, while a
// forwarding def follows whatever #+ becomes.
func TestPathnameSlashIsAnAliasOfPlus(t *testing.T) {
	src := `require "pathname"
im = ->(n) { Pathname.instance_method(n) }
p im.(:/).original_name
p im.(:/) == im.(:+)
p im.(:/).owner
a = Pathname.new("/a")
p (a / "b").to_s
class Pathname
  def +(other); "HIJACKED"; end
end
p (a / "b")
p (a + "b")
`
	const want = ":+\ntrue\nPathname\n\"/a/b\"\n#<Pathname:/a/b>\n\"HIJACKED\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestAliasOfAnOperatorParsesInThePrelude is the narrower guard: the prelude is
// compiled at VM construction, so a parser that refused `alias / +` would fail
// every test in this package rather than this one. It is here to NAME the
// dependency, so a parser downgrade reports the cause instead of a wall of
// failures.
func TestAliasOfAnOperatorParsesInThePrelude(t *testing.T) {
	src := `class Probe
  def +(o); [:plus, o]; end
  alias / +
  def %(o); [:mod, o]; end
  undef %
end
p Probe.new / 1
p Probe.instance_method(:/).original_name
p Probe.new.respond_to?(:%)
`
	const want = "[:plus, 1]\n:+\nfalse\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
