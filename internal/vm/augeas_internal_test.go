// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	"testing"

	augeas "github.com/go-ruby-augeas/augeas"
)

// augeasRun runs a Ruby program with `require "augeas"` prepended.
func augeasRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"augeas\"\n"+body)
}

// TestAugeasTreeEditing drives the in-memory tree surface: set/get, exists?,
// match, setm, insert, rm, mv, defvar, defnode and label.
func TestAugeasTreeEditing(t *testing.T) {
	got := augeasRun(t, `
a = Augeas.create
a.set("/files/foo", "bar")
puts a.get("/files/foo")
puts a.get("/files/nope").inspect
puts a.exists?("/files/foo")
puts a.exists?("/files/nope")
puts a.match("/files/*").inspect
a.set("/a/b/1", "x"); a.set("/a/b/2", "y")
puts a.setm("/a/b", "*", "z")
puts a.get("/a/b/1")
a.set("/ins/foo", "bar")
a.insert("/ins/foo", "baz", true)
puts a.match("/ins/*").inspect
puts a.rm("/files/foo")
a.set("/x", "1"); a.mv("/x", "/y")
puts a.get("/y")
puts a.defvar("v", "/a/b")
puts a.defnode("n", "/newnode", "val")
puts a.defnode("n2", "/newnode", "val")
puts a.label("/a")
puts a.label("/nope").inspect
`)
	want := "bar\nnil\ntrue\nfalse\n" +
		`["/files/foo"]` + "\n2\nz\n" +
		`["/ins/baz", "/ins/foo"]` + "\n1\n1\n1\ntrue\nfalse\na\nnil"
	if got != want {
		t.Fatalf("tree editing:\n got=%q\nwant=%q", got, want)
	}
}

// TestAugeasInsertDefault covers insert with the before argument omitted (true)
// and with an explicit false.
func TestAugeasInsertDefault(t *testing.T) {
	got := augeasRun(t, `
a = Augeas.create
a.set("/n/b", "1")
a.insert("/n/b", "a")
a.insert("/n/b", "c", false)
puts a.match("/n/*").inspect
`)
	want := `["/n/a", "/n/b", "/n/c"]`
	if got != want {
		t.Fatalf("insert default:\n got=%q\nwant=%q", got, want)
	}
}

// TestAugeasTextLens covers the lens seam: text_store parses text with the
// built-in Hosts lens into the tree, and text_retrieve serialises it back.
func TestAugeasTextLens(t *testing.T) {
	got := augeasRun(t, `
a = Augeas.create
a.text_store("Hosts", "/files/etc/hosts", "127.0.0.1 localhost\n")
puts a.get("/files/etc/hosts/1/ipaddr")
puts a.get("/files/etc/hosts/1/canonical")
puts a.text_retrieve("Hosts", "/files/etc/hosts").inspect
`)
	// The separator comes back as a TAB, not as the space that went in, and that
	// is real augeas behaviour rather than a round-trip bug: hosts.aug's record
	// joins ipaddr and canonical with `Sep.tab` (lenses/dist/hosts.aug:8), and
	// sep.aug defines `let tab = del Rx.space "\t"` (lenses/dist/sep.aug:35). In
	// augeas a `del RE VALUE` matches RE on get and REGENERATES VALUE on put, so
	// any run of whitespace parses and a tab is written back.
	//
	// The previous expectation of a space recorded an incomplete put: the
	// dependency bump that wired put for interpreted lenses
	// (go-augeas/augeas#11) is what surfaced it.
	want := "127.0.0.1\nlocalhost\n" + `"127.0.0.1\tlocalhost\n"`
	if got != want {
		t.Fatalf("text lens:\n got=%q\nwant=%q", got, want)
	}
}

// TestAugeasOpen covers the constructor argument handling: create (empty tree),
// open with root/loadpath/flags, open with nil arguments, open with a missing
// loadpath / flags, and open with a non-integer flags argument. It also reads a
// flag constant.
func TestAugeasOpen(t *testing.T) {
	got := augeasRun(t, `
puts Augeas.create.root.inspect
b = Augeas.open("/root", "/lens", 5)
puts b.root
puts b.load_path
puts b.flags
puts Augeas.open(nil, nil).root.inspect
d = Augeas.open("/r")
puts d.load_path.inspect
puts d.flags
puts Augeas.open("/r", "/l", "x").flags
puts Augeas::NO_LOAD
puts Augeas::NONE
`)
	want := `""` + "\n/root\n/lens\n5\n" + `""` + "\n" + `""` + "\n0\n0\n32\n0"
	if got != want {
		t.Fatalf("open:\n got=%q\nwant=%q", got, want)
	}
}

// TestAugeasErrors covers every Augeas::Error raising path (bad path on set /
// mv / insert / defvar, an unknown lens, a text_store parse failure and a
// text_retrieve failure), the error accessor after a failure, and the
// wrong-number-of-arguments ArgumentErrors.
func TestAugeasErrors(t *testing.T) {
	errCases := []string{
		`a.set("/foo[", "x")`,
		`a.mv("/nope[", "/y")`,
		`a.insert("/nope[", "l", true)`,
		`a.defvar("v", "/foo[")`,
		`a.text_store("Nope", "/files/etc/hosts", "x")`,
		`a.text_store("Hosts", "/files/x", "bad\n")`,
		`a.text_retrieve("Hosts", "/nope")`,
	}
	for _, expr := range errCases {
		got := augeasRun(t, "a = Augeas.create\nbegin; "+expr+"; rescue => e; puts e.class; end")
		if got != "Augeas::Error" {
			t.Fatalf("%s expected Augeas::Error, got %q", expr, got)
		}
	}

	argCases := []string{
		"a.get", "a.set(\"/x\")", "a.setm(\"/a\", \"b\")",
		"a.insert(\"/x\")", "a.mv(\"/x\")", "a.defvar(\"v\")",
		"a.defnode(\"n\", \"/e\")", "a.text_store(\"Hosts\", \"/p\")",
		"a.text_retrieve(\"Hosts\")",
	}
	for _, expr := range argCases {
		got := augeasRun(t, "a = Augeas.create\nbegin; "+expr+"; rescue => e; puts e.class; end")
		if got != "ArgumentError" {
			t.Fatalf("%s expected ArgumentError, got %q", expr, got)
		}
	}

	// error is nil on a fresh handle and a String after a failed operation.
	got := augeasRun(t, `
a = Augeas.create
puts a.error.inspect
begin; a.set("/foo[", "x"); rescue; end
puts a.error.class
`)
	if got != "nil\nString" {
		t.Fatalf("error accessor: got=%q", got)
	}
}

// TestAugeasStringers covers the object.Value marker methods on the wrapper.
func TestAugeasStringers(t *testing.T) {
	o := &AugeasObj{a: augeas.New()}
	if o.ToS() != "#<Augeas>" || o.Inspect() != o.ToS() || !o.Truthy() {
		t.Errorf("augeas stringers = %q / %q / %v", o.ToS(), o.Inspect(), o.Truthy())
	}
}

// TestAugeasOpenAndCreateHonourTheirBlock covers the two constructors' block
// form, which both discarded before #950.
//
// The gem's contract, from lib/augeas.rb's `self.open` and
// lib/augeas/facade.rb's `self.create`: `yield aug` inside `begin`, `aug.close`
// in `ensure`, and the method answers the BLOCK's value, not the handle. The
// third part is the one a yield-only fix would still get wrong, so each case
// returns something the handle could not be mistaken for.
func TestAugeasOpenAndCreateHonourTheirBlock(t *testing.T) {
	got := augeasRun(t, `
puts Augeas.open("/r", "/l", 0) { |a| a.set("/x", "1"); "open-block-value" }.inspect
puts Augeas.create(:root => "/r") { |a| a.set("/x", "1"); 42 }.inspect

# Without a block each still answers the handle itself, open on both.
puts Augeas.open("/r").get("/anything").inspect
puts Augeas.create(:root => "/r").get("/anything").inspect

# ⛔ The handle is closed on the way out, and closed is OBSERVABLE: the gem's
# #close NULLs the C handle, after which every method raises from aug_handle.
h = nil
Augeas.open { |a| h = a }
begin; h.get("/x"); rescue => e; puts "#{e.class}: #{e.message}"; end

# ensure, not a trailing call: a block that raises still closes.
g = nil
begin
  Augeas.create { |a| g = a; raise "boom" }
rescue RuntimeError
end
begin; g.get("/x"); rescue => e; puts "#{e.class} after a raising block"; end

# And the block's own close is not a double-close fault: the helper closes
# through the class's #close, which raises on a handle the block already shut.
begin
  Augeas.open { |a| a.close }
rescue => e
  puts "#{e.class} when the block closed it first"
end
`)
	want := `"open-block-value"
42
nil
nil
SystemCallError: Failed to retrieve connection
SystemCallError after a raising block
SystemCallError when the block closed it first`
	if got != want {
		t.Fatalf("block forms:\n got=%q\nwant=%q", got, want)
	}
}

// TestEveryMethodOfAClosedAugeasRaises derives the population from the
// interpreter instead of listing it.
//
// ⛔ A HAND-WRITTEN LIST OF METHODS IS THE ARTEFACT THIS REPOSITORY KEEPS GETTING
// BURNED BY — #931's prose had lost UNIXServer, and #951 had to parse the package
// to find a fourth `open`. The guard being tested here lives at ONE site (the
// `self` accessor in augeas_bind.go), so the risk is not that a method is missed
// today but that a method added LATER reaches the handle another way. Asking
// `Augeas.instance_methods(false)` means such a method is in this test the day it
// exists, with no list to update.
//
// What it asserts is the weakest true thing: no method of a closed handle
// SUCCEEDS. It cannot assert the class, because arity is checked before the
// handle — which is also the gem's order, since rb_define_method's argc is
// enforced by Ruby before the C body runs — so `a.set` with no arguments is an
// ArgumentError there too.
func TestEveryMethodOfAClosedAugeasRaises(t *testing.T) {
	got := augeasRun(t, `
a = Augeas.create
a.close
bad = []
Augeas.instance_methods(false).sort.each do |m|
  begin
    a.send(m)
    bad << m
  rescue ArgumentError, SystemCallError
  end
end
puts Augeas.instance_methods(false).length
puts bad.inspect
`)
	want := "18\n[]"
	if got != want {
		t.Fatalf("closed handle:\n got=%q\nwant=%q\n(a non-empty list is a method that reached the handle without self(); a count below 18 means instance_methods stopped answering and this test measured nothing)", got, want)
	}
}

// TestAugeasCloseAnswersNilThenPoisonsTheHandle pins #close itself, including
// the ordering a plain flag check would get wrong.
func TestAugeasCloseAnswersNilThenPoisonsTheHandle(t *testing.T) {
	got := augeasRun(t, `
a = Augeas.create
a.set("/x", "1")
puts a.close.inspect

# ⛔ A SECOND close RAISES, it does not quietly succeed: augeas_close calls
# aug_handle before aug_close, so the nil check fires first.
begin; a.close; rescue => e; puts "second close: #{e.class}"; end

# close takes no arguments.
b = Augeas.create
begin; b.close(1); rescue => e; puts "#{e.class}: #{e.message}"; end
# and that refusal left it usable.
puts b.get("/nothing").inspect
`)
	want := `nil
second close: SystemCallError
ArgumentError: wrong number of arguments (given 1, expected 0)
nil`
	if got != want {
		t.Fatalf("close:\n got=%q\nwant=%q", got, want)
	}
}

// TestAugeasCreateReadsTheGemsOptionKeys covers Augeas.create's options hash,
// which this binding used to get wrong wholesale: `create` was the same
// positional native as `open`, while the gem's is `create(opts={})` through
// Augeas::Facade.
//
// The flag arithmetic is asserted as a NUMBER rather than by re-deriving it from
// the constants, so a test that read the same wrong constant as the code cannot
// agree with it. ⛔ And it earned that on its first run: these numbers were
// written from memory as [2, 8, 16] for the three save modes, the test went red,
// and the SOURCE settled it against the test — go-ruby-augeas has SaveBackup
// 1<<0, SaveNewFile 1<<1, TypeCheck 1<<2, NoStdinc 1<<3, SaveNoop 1<<4, NoLoad
// 1<<5, NoModlAutoload 1<<6, EnableSpan 1<<7, which is augeas.h's AUG_* order.
// Re-deriving them from the same constants the code uses would have agreed with
// a wrong binding in silence. 4|16 is AUG_TYPE_CHECK|AUG_SAVE_NOOP = 20.
func TestAugeasCreateReadsTheGemsOptionKeys(t *testing.T) {
	got := augeasRun(t, `
puts Augeas.create.flags
puts [Augeas.create.root, Augeas.create.load_path].inspect
a = Augeas.create(:root => "/r", :loadpath => "/l")
puts [a.root, a.load_path, a.flags].inspect

# ⛔ A FLAG IS SET BY ITS KEY'S PRESENCE, NOT ITS VALUE. facade.rb tests
# "flags.key? key" and never looks at the value, so false arms it too.
puts Augeas.create(:type_check => false).flags
puts Augeas.create(:type_check => true).flags
puts Augeas.create(:type_check => false, :save_mode => :noop).flags

puts [Augeas.create(:save_mode => :backup).flags,
      Augeas.create(:save_mode => :newfile).flags,
      Augeas.create(:save_mode => :noop).flags].inspect
puts [Augeas.create(:no_stdinc => 1).flags,
      Augeas.create(:no_load => 1).flags,
      Augeas.create(:no_modl_autoload => 1).flags,
      Augeas.create(:enable_span => 1).flags].inspect

# nil is the same as no hash at all, and a non-Hash is a TypeError.
puts Augeas.create(nil).flags
begin; Augeas.create("/r"); rescue => e; puts e.class; end
begin; Augeas.create({}, {}); rescue => e; puts "#{e.class}: #{e.message}"; end

# ⛔ open's OWN arity guard, which create's does not cover. The gem's open takes
# three positional arguments and no more; a fourth is an ArgumentError, not a
# silently ignored argument. This line is here because the CI coverage ratchet
# named it: registerAugeas read 94.4% "(was fully covered)" with the guard
# written and never executed — the second time in one session that a branch I
# added went unmeasured because I did not run covratchet locally.
begin; Augeas.open("/r", "/l", 0, 1); rescue => e; puts "#{e.class}: #{e.message}"; end
`)
	want := `0
["", ""]
["/r", "/l", 0]
4
4
20
[1, 2, 16]
[8, 32, 64, 128]
0
TypeError
ArgumentError: wrong number of arguments (given 2, expected 0..1)
ArgumentError: wrong number of arguments (given 4, expected 0..3)`
	if got != want {
		t.Fatalf("create options:\n got=%q\nwant=%q", got, want)
	}
}

// TestAugeasCreateRaisesOnTheFIRSTBadKey pins the consequence of facade.rb
// validating and assembling in ONE `opts.each_key` pass: the first offending key
// raises and the rest are never looked at, so WHICH error you get depends on the
// order the caller wrote the keys in.
//
// ⛔ This is the case a reimplementation gets wrong by being tidier than the
// reference — validating all keys, or checking save_mode before the scan, gives
// a stable answer where the gem's is order-dependent. Both orders are asserted
// here precisely because agreeing with one of them is not agreeing with the gem.
func TestAugeasCreateRaisesOnTheFIRSTBadKey(t *testing.T) {
	got := augeasRun(t, `
[[:save_mode, :bogus, :nonsense, 1], [:nonsense, 1, :save_mode, :bogus]].each do |k1, v1, k2, v2|
  begin
    Augeas.create({k1 => v1, k2 => v2})
  rescue => e
    puts "#{e.class}: #{e.message}"
  end
end

# The two messages on their own, including nil's empty interpolation.
begin; Augeas.create(:save_mode => :bogus); rescue => e; puts e.message; end
begin; Augeas.create(:save_mode => nil); rescue => e; puts e.message; end
begin; Augeas.create(:save_mode => "backup"); rescue => e; puts e.message; end
begin; Augeas.create(:nonsense => 1); rescue => e; puts e.message; end
begin; Augeas.create("root" => "/r"); rescue => e; puts e.message; end

# A good key before a bad one still raises on the bad one.
begin; Augeas.create(:root => "/r", :nope => 1); rescue => e; puts e.message; end
`)
	want := `ArgumentError: Invalid save mode bogus.
ArgumentError: Unknown argument nonsense.
Invalid save mode bogus.
Invalid save mode .
Invalid save mode backup.
Unknown argument nonsense.
Unknown argument root.
Unknown argument nope.`
	if got != want {
		t.Fatalf("create key errors:\n got=%q\nwant=%q", got, want)
	}
}
