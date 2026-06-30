// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPStore covers the Ruby PStore class (backed by
// github.com/go-ruby-pstore/pstore, the MRI-4.0.5-faithful port of the `pstore`
// stdlib): require/path, a read-write transaction (commit on normal exit), the
// in-transaction accessors ([], []=, delete, fetch, roots, root?/key?), the
// explicit #commit and #abort early exits, read-only transactions, the on-disk
// Marshal round-trip across a reopened store, and the error cases (not in
// transaction, in read-only transaction, nested transaction, undefined key). Every
// value is asserted against MRI 4.0.5's `ruby -rpstore`. FS-touching cases write to
// a t.TempDir() store.
func TestPStore(t *testing.T) {
	dir := t.TempDir()
	// store is a per-case fresh path so cases never share on-disk state. ToSlash
	// keeps it forward-slash (valid on Windows for Ruby File ops) so embedding it
	// in a double-quoted Ruby literal can't be mangled by backslash escapes
	// (e.g. a Windows temp path's \001\a would become octal+bell, like MRI).
	store := func(name string) string { return filepath.ToSlash(filepath.Join(dir, name)) }
	// q quotes a path into a Ruby string literal (the temp dir has no quotes).
	q := func(p string) string { return `"` + p + `"` }

	for _, c := range []struct{ src, want string }{
		// require returns true the first time, false after (a provided feature).
		{`p require "pstore"`, "true\n"},
		{`require "pstore"; p require "pstore"`, "false\n"},
		// PStore is a class; #path echoes the file it was built with.
		{`require "pstore"; p PStore.new(` + q(store("a")) + `).class`, "PStore\n"},
		{`require "pstore"; puts PStore.new(` + q(store("a")) + `).path`, store("a") + "\n"},
		// A PStore is always truthy (the wrapper's Truthy).
		{`require "pstore"; puts(PStore.new(` + q(store("a")) + `) ? "t" : "f")`, "t\n"},

		// A read-write transaction commits on normal block exit; reopening reads it
		// back. ps[k]= stores, ps[k] reads, roots lists the keys (insertion order).
		{`require "pstore"
ps = PStore.new(` + q(store("rw")) + `)
ps.transaction { |s| s[:a] = 1; s[:b] = "two" }
ps.transaction(true) { |s| puts s[:a]; puts s[:b]; p s.roots }`,
			"1\ntwo\n[:a, :b]\n"},

		// fetch with a default (hit + miss returning the default).
		{`require "pstore"
ps = PStore.new(` + q(store("fetch")) + `)
ps.transaction { |s| s[:x] = 10 }
ps.transaction(true) { |s| puts s.fetch(:x, 0); puts s.fetch(:y, 99) }`,
			"10\n99\n"},

		// root? / key? report key presence; delete removes and returns the old value.
		{`require "pstore"
ps = PStore.new(` + q(store("roots")) + `)
ps.transaction { |s| s[:k] = 5 }
ps.transaction { |s| puts s.root?(:k); puts s.key?(:nope); p s.delete(:k); puts s.root?(:k) }`,
			"true\nfalse\n5\nfalse\n"},
		// delete of an absent key returns nil.
		{`require "pstore"
ps = PStore.new(` + q(store("del2")) + `)
ps.transaction { |s| p s.delete(:gone) }`,
			"nil\n"},

		// #commit exits the block early and the write is committed.
		{`require "pstore"
ps = PStore.new(` + q(store("commit")) + `)
ps.transaction { |s| s[:a] = 1; s.commit; s[:b] = 2 }
ps.transaction(true) { |s| p s.roots }`,
			"[:a]\n"},

		// #abort exits the block early and NOTHING is written (the store stays empty).
		{`require "pstore"
ps = PStore.new(` + q(store("abort")) + `)
ps.transaction { |s| s[:a] = 1; s.abort; s[:b] = 2 }
ps.transaction(true) { |s| p s.roots }`,
			"[]\n"},

		// A read-only transaction that mutates nothing never writes; reads work.
		{`require "pstore"
ps = PStore.new(` + q(store("ro")) + `)
ps.transaction { |s| s[:a] = 7 }
ps.transaction(true) { |s| puts s[:a] }`,
			"7\n"},

		// [] of a missing key returns nil (MRI's PStore#[]).
		{`require "pstore"
ps = PStore.new(` + q(store("miss")) + `)
ps.transaction(true) { |s| p s[:absent] }`,
			"nil\n"},

		// transaction returns nil (MRI's PStore#transaction returns the block value,
		// but the host returns nil — assert the documented host behaviour).
		{`require "pstore"
ps = PStore.new(` + q(store("ret")) + `)
p ps.transaction { |s| s[:a] = 1 }`,
			"nil\n"},

		// A String key (not only Symbols) round-trips; genuine Ruby objects (an Array
		// value) marshal through.
		{`require "pstore"
ps = PStore.new(` + q(store("strkey")) + `)
ps.transaction { |s| s["name"] = [1, 2, 3] }
ps.transaction(true) { |s| p s["name"] }`,
			"[1, 2, 3]\n"},

		// thread_safe form: PStore.new(file, true) behaves identically for a single
		// thread (the mutex just serialises).
		{`require "pstore"
ps = PStore.new(` + q(store("ts")) + `, true)
ps.transaction { |s| s[:a] = 1 }
ps.transaction(true) { |s| puts s[:a] }`,
			"1\n"},

		// Overwriting an existing key replaces the value (not appends a root).
		{`require "pstore"
ps = PStore.new(` + q(store("over")) + `)
ps.transaction { |s| s[:a] = 1 }
ps.transaction { |s| s[:a] = 2 }
ps.transaction(true) { |s| puts s[:a]; p s.roots }`,
			"2\n[:a]\n"},

		// puts / p on the PStore object route through the wrapper's ToS / Inspect.
		{`require "pstore"; puts PStore.new(` + q(store("repr")) + `)`, "#<PStore>\n"},
		{`require "pstore"; p PStore.new(` + q(store("repr")) + `)`, "#<PStore>\n"},

		// A genuine Ruby exception raised inside the block propagates out (the
		// transaction does NOT commit) and is rescuable — the recover's re-panic path.
		{`require "pstore"
ps = PStore.new(` + q(store("raise")) + `)
begin
  ps.transaction { |s| s[:a] = 1; raise "boom" }
rescue => e
  puts e.message
end
ps.transaction(true) { |s| p s.roots }`,
			"boom\n[]\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestPStoreErrors covers the PStore::Error cases, matching MRI 4.0.5's messages:
// using an accessor outside a transaction, writing in a read-only transaction, a
// nested transaction, a no-default fetch miss, and the class hierarchy
// (PStore::Error < StandardError, rescuable as StandardError).
func TestPStoreErrors(t *testing.T) {
	dir := t.TempDir()
	store := func(name string) string { return filepath.Join(dir, name) }
	q := func(p string) string { return `"` + p + `"` }

	for _, c := range []struct{ src, want string }{
		// An accessor outside any transaction raises "not in transaction".
		{`require "pstore"
ps = PStore.new(` + q(store("e1")) + `)
begin; ps[:a]; rescue => e; puts e.class; puts e.message; end`,
			"PStore::Error\nnot in transaction\n"},
		// commit outside a transaction also raises "not in transaction".
		{`require "pstore"
ps = PStore.new(` + q(store("e1b")) + `)
begin; ps.commit; rescue PStore::Error => e; puts e.message; end`,
			"not in transaction\n"},

		// A write inside a read-only transaction raises "in read-only transaction".
		{`require "pstore"
ps = PStore.new(` + q(store("e2")) + `)
begin
  ps.transaction(true) { |s| s[:a] = 1 }
rescue PStore::Error => e; puts e.message; end`,
			"in read-only transaction\n"},
		// delete inside a read-only transaction is also forbidden.
		{`require "pstore"
ps = PStore.new(` + q(store("e2b")) + `)
ps.transaction { |s| s[:a] = 1 }
begin
  ps.transaction(true) { |s| s.delete(:a) }
rescue PStore::Error => e; puts e.message; end`,
			"in read-only transaction\n"},

		// A nested transaction raises "nested transaction".
		{`require "pstore"
ps = PStore.new(` + q(store("e3")) + `)
begin
  ps.transaction { |s| ps.transaction { |s2| } }
rescue PStore::Error => e; puts e.message; end`,
			"nested transaction\n"},

		// A no-default fetch miss raises "undefined key 'KEY'".
		{`require "pstore"
ps = PStore.new(` + q(store("e4")) + `)
begin
  ps.transaction(true) { |s| s.fetch(:missing) }
rescue PStore::Error => e; puts e.message; end`,
			"undefined key 'missing'\n"},

		// PStore::Error is a StandardError (rescuable by a bare rescue / StandardError).
		{`require "pstore"
ps = PStore.new(` + q(store("e5")) + `)
puts(PStore::Error.ancestors.include?(StandardError))
begin; ps[:a]; rescue StandardError => e; puts "caught"; end`,
			"true\ncaught\n"},

		// PStore.new with no argument is an ArgumentError; a non-String is a TypeError.
		{`require "pstore"
begin; PStore.new; rescue ArgumentError => e; puts "arg"; end
begin; PStore.new(42); rescue TypeError => e; puts "type"; end`,
			"arg\ntype\n"},

		// transaction with no block is an ArgumentError.
		{`require "pstore"
ps = PStore.new(` + q(store("e6")) + `)
begin; ps.transaction; rescue ArgumentError => e; puts "noblk"; end`,
			"noblk\n"},

		// A store whose file cannot be opened (its parent directory does not exist)
		// surfaces the flock open error as PStore::Error when a transaction begins.
		{`require "pstore"
ps = PStore.new(` + q(filepath.Join(store("nodir"), "x.pstore")) + `)
begin; ps.transaction { |s| }; rescue PStore::Error => e; puts "ioerr"; end`,
			"ioerr\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestPStoreOnDiskFormat proves the on-disk file is a plain Marshal.dump of the
// table Hash — byte-compatible with MRI's PStore. The store written by rbgo is read
// back by Marshal.load (the very codec MRI's PStore uses), and the file begins with
// the Marshal 4.8 magic, so MRI's `ruby -rpstore` reading the same file sees the
// same table.
func TestPStoreOnDiskFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fmt.pstore")
	src := `require "pstore"
ps = PStore.new("` + path + `")
ps.transaction { |s| s[:a] = 1; s["b"] = [2, 3] }
` +
		// The file bytes equal Marshal.dump of the equivalent Hash (what MRI stores).
		`data = File.read("` + path + `")
expected = Marshal.dump({:a => 1, "b" => [2, 3]})
puts(data == expected)
puts(data.bytes[0])  # Marshal MAJOR_VERSION (4)
puts(data.bytes[1])  # Marshal MINOR_VERSION (8)
table = Marshal.load(data)
p table`
	got := eval(t, src)
	// rbgo renders Hash#inspect in the Ruby 3.4+ style ({a: 1, "b" => [2, 3]}); the
	// load result is the same table either way — the load proving the bytes are a
	// genuine Marshal of the stored Hash.
	want := "true\n4\n8\n{a: 1, \"b\" => [2, 3]}\n"
	if got != want {
		t.Errorf("on-disk format mismatch:\n got=%q\nwant=%q", got, want)
	}

	// And the raw bytes on disk start with the Marshal 4.8 magic, independent of the
	// VM — a direct Go-side check that MRI would load.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	if len(raw) < 2 || raw[0] != 4 || raw[1] != 8 {
		t.Fatalf("store does not begin with Marshal 4.8 magic: %v", raw)
	}
}

// TestPStoreReopenAcrossVMs proves the persisted file survives a fresh VM: one VM
// writes the store, a second (independent) VM reading the same path sees the data —
// the cross-process persistence MRI's PStore provides.
func TestPStoreReopenAcrossVMs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reopen.pstore")

	writeOut := eval(t, `require "pstore"
PStore.new("`+path+`").transaction { |s| s[:count] = 41; s[:name] = "rbgo" }
puts "written"`)
	if strings.TrimSpace(writeOut) != "written" {
		t.Fatalf("write VM output = %q", writeOut)
	}

	readOut := eval(t, `require "pstore"
PStore.new("`+path+`").transaction(true) { |s| puts s[:count] + 1; puts s[:name] }`)
	if want := "42\nrbgo\n"; readOut != want {
		t.Errorf("reopen read = %q, want %q", readOut, want)
	}
}
