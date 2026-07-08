// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	pagy "github.com/go-ruby-pagy/pagy"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestPagyShell covers the Pagy value shell's ToS/Inspect/Truthy.
func TestPagyShell(t *testing.T) {
	p, err := pagy.New(pagy.Vars{Count: 100, Page: 3, Items: 20})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := &Pagy{p: p}
	want := "#<Pagy count=100 page=3 items=20>"
	if w.ToS() != want || w.Inspect() != want || !w.Truthy() {
		t.Errorf("shell: %q / %q / %v", w.ToS(), w.Inspect(), w.Truthy())
	}
}

// TestPagyConstants covers the Pagy class and its error tree (require "pagy").
func TestPagyConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "pagy"; p Pagy.is_a?(Class)`, "true\n"},
		{`p require "pagy"`, "true\n"},
		{`require "pagy"; p require "pagy"`, "false\n"},
		{`require "pagy"; p Pagy::OverflowError < StandardError`, "true\n"},
		{`require "pagy"; p Pagy::VariableError < StandardError`, "true\n"},
		{`require "pagy"; p Pagy.new(count: 100, page: 3, items: 20).class.name`, "\"Pagy\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPagyReaders covers the reader surface of a mid-range page.
func TestPagyReaders(t *testing.T) {
	pre := `require "pagy"; pg = Pagy.new(count: 1000, page: 6, items: 20); `
	cases := []struct{ src, want string }{
		{pre + `p pg.count`, "1000\n"},
		{pre + `p pg.page`, "6\n"},
		{pre + `p pg.items`, "20\n"},
		{pre + `p pg.limit`, "20\n"},
		{pre + `p pg.offset`, "100\n"},
		{pre + `p pg.outset`, "0\n"},
		{pre + `p pg.pages`, "50\n"},
		{pre + `p pg.last`, "50\n"},
		{pre + `p pg.from`, "101\n"},
		{pre + `p pg.to`, "120\n"},
		{pre + `p pg.in`, "20\n"},
		{pre + `p pg.prev`, "5\n"},
		{pre + `p pg.next`, "7\n"},
		{pre + `p pg.series`, "[1, :gap, 5, \"6\", 7, :gap, 50]\n"},
		{pre + `p pg.inspect`, "\"#<Pagy count=1000 page=6 items=20>\"\n"},
		{pre + `p pg.to_s`, "\"#<Pagy count=1000 page=6 items=20>\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPagyEdges covers the nil prev/next at the first and last page, and a
// custom-size series.
func TestPagyEdges(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "pagy"; p Pagy.new(count: 100, page: 1, items: 20).prev`, "nil\n"},
		{`require "pagy"; p Pagy.new(count: 100, page: 5, items: 20).next`, "nil\n"},
		// A short series (size 3) elides the ends.
		{`require "pagy"; p Pagy.new(count: 1000, page: 6, items: 20).series(3)`, "[5, \"6\", 7]\n"},
		// An empty count clamps to a single page with an empty window.
		{`require "pagy"; pg = Pagy.new(count: 0, page: 1, items: 20); p [pg.pages, pg.in, pg.from, pg.to]`, "[1, 0, 0, 0]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPagyVars covers each supported keyword variable being read into pagy.Vars.
func TestPagyVars(t *testing.T) {
	cases := []struct{ src, want string }{
		// items: default (20).
		{`require "pagy"; p Pagy.new(count: 100).items`, "20\n"},
		// limit: is an alias of items:.
		{`require "pagy"; p Pagy.new(count: 100, limit: 25).items`, "25\n"},
		// outset: shifts the zero-based offset into the data source.
		{`require "pagy"; p Pagy.new(count: 100, outset: 10, items: 20).offset`, "10\n"},
		// size: sets the default series width.
		{`require "pagy"; p Pagy.new(count: 1000, page: 6, items: 20, size: 3).series`, "[5, \"6\", 7]\n"},
		// max_pages: caps the last page.
		{`require "pagy"; p Pagy.new(count: 1000, page: 5, items: 20, max_pages: 5).last`, "5\n"},
		// cycle: wraps next from the last page back to 1.
		{`require "pagy"; p Pagy.new(count: 100, page: 5, items: 20, cycle: true).next`, "1\n"},
		// ends: false drops the forced first/last pages (and their gaps) from the series.
		{`require "pagy"; p Pagy.new(count: 1000, page: 6, items: 20, ends: false).series`, "[3, 4, 5, \"6\", 7, 8, 9]\n"},
		// A String keyword key resolves the same as a Symbol.
		{`require "pagy"; p Pagy.new("count" => 100, "items" => 25).items`, "25\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPagyHelper covers the top-level pagy(collection, **vars) helper.
func TestPagyHelper(t *testing.T) {
	cases := []struct{ src, want string }{
		// Count defaults to the collection length; the window is sliced out.
		{`require "pagy"; pg, items = pagy((1..10).to_a, page: 2, items: 3); p [pg.count, pg.page, items]`,
			"[10, 2, [4, 5, 6]]\n"},
		// The last (partial) page yields the remaining tail.
		{`require "pagy"; pg, items = pagy((1..10).to_a, page: 4, items: 3); p items`, "[10]\n"},
		// An explicit count: overrides the collection length.
		{`require "pagy"; pg, items = pagy((1..3).to_a, count: 99, page: 1, items: 3); p pg.count`, "99\n"},
		// No keyword vars at all (single page, whole collection).
		{`require "pagy"; pg, items = pagy((1..3).to_a); p [pg.pages, items]`, "[1, [1, 2, 3]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPagyErrors covers the raising paths: an overflow page, a negative series
// size, the helper's arity/type errors.
func TestPagyErrors(t *testing.T) {
	if class, msg := evalErr(t, `require "pagy"; Pagy.new(count: 100, page: 99, items: 20)`); class != "Pagy::OverflowError" {
		t.Errorf("overflow: class=%q msg=%q", class, msg)
	}
	if class, _ := evalErr(t, `require "pagy"; Pagy.new(count: 100, page: 1, items: 20).series(-1)`); class != "Pagy::VariableError" {
		t.Errorf("negative size: class=%q", class)
	}
	if class, _ := evalErr(t, `require "pagy"; pagy`); class != "ArgumentError" {
		t.Errorf("no args: class=%q", class)
	}
	if class, _ := evalErr(t, `require "pagy"; pagy(42)`); class != "TypeError" {
		t.Errorf("non-array: class=%q", class)
	}
}

// TestPagyKeyBridges covers pagyKey's arms: a Symbol, a String and the
// to_s-fallback for any other key type.
func TestPagyKeyBridges(t *testing.T) {
	if got := pagyKey(object.Symbol("count")); got != "count" {
		t.Errorf("sym key -> %q", got)
	}
	if got := pagyKey(object.NewString("page")); got != "page" {
		t.Errorf("str key -> %q", got)
	}
	if got := pagyKey(object.Integer(7)); got != "7" {
		t.Errorf("int key -> %q", got)
	}
}

// TestPagySeriesBridge covers pagySeries mapping each element kind (int link,
// current-page string, gap symbol) directly.
func TestPagySeriesBridge(t *testing.T) {
	arr, ok := pagySeries([]any{1, "2", pagy.SeriesGap{}}).(*object.Array)
	if !ok || len(arr.Elems) != 3 {
		t.Fatalf("series -> %#v", arr)
	}
	if arr.Elems[0] != object.IntValue(1) {
		t.Errorf("int link -> %v", arr.Elems[0])
	}
	if s, ok := arr.Elems[1].(*object.String); !ok || s.Str() != "2" {
		t.Errorf("current page -> %v", arr.Elems[1])
	}
	if arr.Elems[2] != object.Symbol("gap") {
		t.Errorf("gap -> %v", arr.Elems[2])
	}
}
