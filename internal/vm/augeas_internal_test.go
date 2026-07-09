// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

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
	want := "127.0.0.1\nlocalhost\n" + `"127.0.0.1 localhost\n"`
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
