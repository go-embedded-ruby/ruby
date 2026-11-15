// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import "testing"

// ⛔ #817: Object.constants answered 1620 where MRI 4.0.7 answers 120, and
// 1303 of those 1620 carried "::" — AASM::Error, ActionCable::Channel::Base,
// Acme::Client::Error::BadNonce. MRI has none, because a nested constant
// belongs to its enclosing module.
//
// The issue's title says "Object.constants is empty", which is the opposite of
// what it does; measured through cmd/oracle with reference, control and
// treatment:
//
//	                  MRI   before   after
//	total             120    1620     317
//	containing "::"     0    1303       0
//	core missing     none    none    none
//
// ⛔ THE FIX IS IN THE ENUMERATION, NOT THE 52 REGISTRATION SITES, and that is
// the opposite of what counting suggests. `vm.consts[qualified] = c` appears in
// 52 files, but those keys are READ: vm.kafkaClass("Kafka::Producer") and three
// siblings in kafka alone, plus actioncable_bind.go, io.go and embed.go. They
// are an index from qualified name to class, not constants — so the table keeps
// them and `constants` stops reporting them.
//
// ⚠ What this does NOT close: 317 against MRI's 120. The remainder is bound gem
// top-levels (AASM, Acme, ActionCable…) present before any `require`, which is a
// separate decision with a separate cost.
func TestObjectConstantsReportsNoQualifiedNames(t *testing.T) {
	got := runSrc(t, `
all = Object.constants.map(&:to_s)
p all.count { |n| n.include?("::") }

# ⛔ The core set must survive the filter. A guard that empties the list would
# also report zero qualified names.
core = %w[Object BasicObject Module Class Kernel Comparable Enumerable
          String Symbol Integer Float Array Hash Range Proc Exception
          StandardError RuntimeError Thread Mutex Queue File IO Dir
          Encoding Regexp MatchData Struct Math Process GC ObjectSpace]
p core.reject { |n| all.include?(n) }
p all.size > 100

# The tree is untouched: a nested constant is still reachable through its
# parent, which is where MRI keeps it.
p AASM.constants.include?(:Error)
p AASM.constants.map(&:to_s).any? { |n| n.include?("::") }
p AASM::Error.to_s
p Object.const_get("AASM::Error").to_s

# ⛔ And the bindings' own lookups, which are what made editing the 52 sites the
# wrong fix: these resolve through the qualified key the table still holds.
p Kafka::Producer.to_s
p JSON::ParserError.to_s
p Acme::Client::Error::BadNonce.to_s

# Module.constants at the top level is Object.constants in MRI, so it must be
# filtered too -- it is a different code path into the same helper.
p Module.constants.map(&:to_s).count { |n| n.include?("::") }
`)
	want := "0\n[]\ntrue\ntrue\nfalse\n\"AASM::Error\"\n\"AASM::Error\"\n" +
		"\"Kafka::Producer\"\n\"JSON::ParserError\"\n\"Acme::Client::Error::BadNonce\"\n0"
	if got != want {
		t.Errorf("Object.constants qualified-name filter:\ngot  %q\nwant %q", got, want)
	}
}
