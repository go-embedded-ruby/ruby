// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package object

import "testing"

// TestStringOwnedAndViewAccessors exercises every accessor on both the owned and
// the view representation, so Bytes/Str/Len each cover their isView branch.
func TestStringOwnedAndViewAccessors(t *testing.T) {
	owned := NewString("hello")
	if owned.isView {
		t.Fatal("NewString must be owned")
	}
	if got := string(owned.Bytes()); got != "hello" {
		t.Fatalf("owned Bytes = %q", got)
	}
	if got := owned.Str(); got != "hello" {
		t.Fatalf("owned Str = %q", got)
	}
	if got := owned.Len(); got != 5 {
		t.Fatalf("owned Len = %d", got)
	}

	view := NewStringView("hello")
	if !view.isView {
		t.Fatal("NewStringView must be a view")
	}
	if got := string(view.Bytes()); got != "hello" {
		t.Fatalf("view Bytes = %q", got)
	}
	if got := view.Str(); got != "hello" {
		t.Fatalf("view Str = %q", got)
	}
	if got := view.Len(); got != 5 {
		t.Fatalf("view Len = %d", got)
	}

	// An empty view must not panic in Bytes (unsafe.Slice over a nil StringData).
	empty := NewStringView("")
	if len(empty.Bytes()) != 0 || empty.Len() != 0 || empty.Str() != "" {
		t.Fatalf("empty view mishandled: %v", empty.Bytes())
	}
}

// TestStringViewToOwnedTransition proves the first mutation materializes a
// private copy and that the immutable source is never observed to change.
func TestStringViewToOwnedTransition(t *testing.T) {
	src := "hello world"
	view := NewStringView(src[:5]) // "hello", sharing src's bytes

	// MutableBytes triggers ensureOwned: the view becomes owned.
	mb := view.MutableBytes()
	if view.isView {
		t.Fatal("MutableBytes must materialize an owned slice")
	}
	mb[0] = 'H'
	if view.Str() != "Hello" {
		t.Fatalf("mutation not visible: %q", view.Str())
	}
	// The immutable source string is untouched (Go strings never mutate).
	if src != "hello world" {
		t.Fatalf("source corrupted: %q", src)
	}

	// ensureOwned is idempotent: a second MutableBytes on the now-owned string
	// takes the no-op branch and keeps the same backing array.
	mb2 := view.MutableBytes()
	mb2[1] = 'E'
	if view.Str() != "HEllo" {
		t.Fatalf("second mutation wrong: %q", view.Str())
	}
}

// TestStringSetBytes covers replacing contents on both representations.
func TestStringSetBytes(t *testing.T) {
	view := NewStringView("abc")
	view.SetBytes([]byte("xyz"))
	if view.isView || view.Str() != "xyz" {
		t.Fatalf("SetBytes on view failed: view=%v str=%q", view.isView, view.Str())
	}
	owned := NewString("abc")
	owned.SetBytes([]byte("q"))
	if owned.Str() != "q" {
		t.Fatalf("SetBytes on owned failed: %q", owned.Str())
	}
}

// TestStringTakeFrom covers adopting both a view and an owned source.
func TestStringTakeFrom(t *testing.T) {
	dst := NewString("orig")
	dst.TakeFrom(NewStringView("view-src"))
	if !dst.isView || dst.Str() != "view-src" {
		t.Fatalf("TakeFrom view: isView=%v str=%q", dst.isView, dst.Str())
	}
	// Now adopt an owned source; dst switches back to owned.
	dst.TakeFrom(NewString("owned-src"))
	if dst.isView || dst.Str() != "owned-src" {
		t.Fatalf("TakeFrom owned: isView=%v str=%q", dst.isView, dst.Str())
	}
}

// TestStringDup covers Dup on both representations, and that the copy is
// independent and owned.
func TestStringDup(t *testing.T) {
	for _, s := range []*String{NewString("dup"), NewStringView("dup")} {
		d := s.Dup()
		if d.isView {
			t.Fatal("Dup must return an owned string")
		}
		d.MutableBytes()[0] = 'D'
		if s.Str() != "dup" {
			t.Fatalf("Dup shares storage: source now %q", s.Str())
		}
		if d.Str() != "Dup" {
			t.Fatalf("Dup copy wrong: %q", d.Str())
		}
	}
}

// TestStringViewInspectAndToS routes Inspect/ToS through the view representation.
func TestStringViewInspectAndToS(t *testing.T) {
	v := NewStringView("a\"b")
	if got := v.Inspect(); got != `"a\"b"` {
		t.Fatalf("view Inspect = %q", got)
	}
	if got := v.ToS(); got != `a"b` {
		t.Fatalf("view ToS = %q", got)
	}
}

// TestStringBytesEncAndFrozenView covers the byte-taking and frozen-view
// constructors.
func TestStringBytesEncAndFrozenView(t *testing.T) {
	b := NewStringBytes([]byte("raw"))
	if b.isView || b.Str() != "raw" {
		t.Fatalf("NewStringBytes wrong")
	}
	be := NewStringBytesEnc([]byte{0xff}, "ASCII-8BIT")
	if be.Enc != "ASCII-8BIT" || !be.IsBinary() {
		t.Fatalf("NewStringBytesEnc enc wrong")
	}
	fv := NewFrozenStringView("lit")
	if !fv.isView || !fv.Frozen || fv.Str() != "lit" {
		t.Fatalf("NewFrozenStringView wrong: view=%v frozen=%v", fv.isView, fv.Frozen)
	}
}

// TestStringViewAsHashKey exercises strKey on a view (kk.Bytes()) so a view and
// an owned string with equal bytes collapse to the same Hash entry.
func TestStringViewAsHashKey(t *testing.T) {
	h := NewHash()
	h.Set(Wrap(NewStringView("k")), IntValue(int64(Integer(1))))
	if v, ok := h.Get(Wrap(NewString("k"))); !ok || v != IntValue(int64(Integer(1))) {
		t.Fatalf("view/owned key mismatch: v=%v ok=%v", v, ok)
	}
}
