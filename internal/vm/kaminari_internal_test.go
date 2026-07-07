// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"testing"

	kaminari "github.com/go-ruby-kaminari/kaminari"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// kaminariRun runs src on a fresh VM and returns its captured stdout.
func kaminariRun(t *testing.T, src string) string {
	t.Helper()
	var buf bytes.Buffer
	vm := New(&buf)
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := vm.Run(iseq); err != nil {
		t.Fatalf("run %q: %v", src, err)
	}
	return buf.String()
}

// kaminariRaises asserts src raises a Ruby exception of the given class.
func kaminariRaises(t *testing.T, wantClass, src string) {
	t.Helper()
	var buf bytes.Buffer
	vm := New(&buf)
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = vm.Run(iseq)
	if err == nil {
		t.Fatalf("expected raise %s, got none", wantClass)
	}
	re, ok := err.(RubyError)
	if !ok {
		t.Fatalf("expected a RubyError, got %#v", err)
	}
	if re.Class != wantClass {
		t.Fatalf("raised %s, want %s", re.Class, wantClass)
	}
}

const kaminariRequire = `require "kaminari"` + "\n"

// TestKaminariRequire covers the require flag: the feature is provided, and a
// second require reports it already loaded.
func TestKaminariRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "kaminari"`, "true\n"},
		{`require "kaminari"; p require "kaminari"`, "false\n"},
		{kaminariRequire + `p Kaminari.is_a?(Module)`, "true\n"},
		{kaminariRequire + `p Kaminari.paginate_array([1]).class`, "Kaminari::PaginatableArray\n"},
	}
	for _, c := range cases {
		if got := kaminariRun(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKaminariArrayMetadata covers the PaginatableArray scope algebra and every
// page-metadata method across a mid page, the first page, the last page and an
// out-of-range page.
func TestKaminariArrayMetadata(t *testing.T) {
	base := kaminariRequire + `a = Kaminari.paginate_array((1..100).to_a)` + "\n"
	cases := []struct{ src, want string }{
		// mid page (page 3, per 10).
		{base + `x = a.page(3).per(10); p x.current_page`, "3\n"},
		{base + `x = a.page(3).per(10); p x.total_pages`, "10\n"},
		{base + `x = a.page(3).per(10); p x.total_count`, "100\n"},
		{base + `x = a.page(3).per(10); p x.limit_value`, "10\n"},
		{base + `x = a.page(3).per(10); p x.offset_value`, "20\n"},
		{base + `x = a.page(3).per(10); p x.current_per_page`, "10\n"},
		{base + `x = a.page(3).per(10); p x.first_page?`, "false\n"},
		{base + `x = a.page(3).per(10); p x.last_page?`, "false\n"},
		{base + `x = a.page(3).per(10); p x.out_of_range?`, "false\n"},
		{base + `x = a.page(3).per(10); p x.prev_page`, "2\n"},
		{base + `x = a.page(3).per(10); p x.next_page`, "4\n"},
		{base + `x = a.page(3).per(10); p x.records`,
			"[21, 22, 23, 24, 25, 26, 27, 28, 29, 30]\n"},
		{base + `x = a.page(3).per(10); p x.page_entries_info`,
			"\"Displaying entries 21 - 30 of 100 in total\"\n"},
		// first page: prev_page is nil, first_page? true.
		{base + `x = a.page(1).per(10); p x.first_page?`, "true\n"},
		{base + `x = a.page(1).per(10); p x.prev_page`, "nil\n"},
		// last page: next_page is nil, last_page? true.
		{base + `x = a.page(10).per(10); p x.last_page?`, "true\n"},
		{base + `x = a.page(10).per(10); p x.next_page`, "nil\n"},
		// out of range: an empty window past the end.
		{base + `x = a.page(99).per(10); p x.out_of_range?`, "true\n"},
		{base + `x = a.page(99).per(10); p x.records`, "[]\n"},
		// #to_a aliases #records.
		{base + `x = a.page(1).per(3); p x.to_a`, "[1, 2, 3]\n"},
		// #inspect / #to_s render the shell.
		{base + `p a`, "#<Kaminari::PaginatableArray>\n"},
	}
	for _, c := range cases {
		if got := kaminariRun(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKaminariArrayPadding covers #padding (the offset shifts by the padding) and
// #per(nil) (unlimited: a single page holding everything).
func TestKaminariArrayPadding(t *testing.T) {
	base := kaminariRequire + `a = Kaminari.paginate_array((1..20).to_a)` + "\n"
	cases := []struct{ src, want string }{
		{base + `p a.page(1).per(5).padding(2).records`, "[3, 4, 5, 6, 7]\n"},
		{base + `p a.page(1).per(nil).total_pages`, "1\n"},
		{base + `p a.page(1).per(nil).records.length`, "20\n"},
	}
	for _, c := range cases {
		if got := kaminariRun(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKaminariEntriesInfo covers the three shapes of page_entries_info: the empty
// collection, a lone record (entry singular), and a single full page (entries
// plural).
func TestKaminariEntriesInfo(t *testing.T) {
	cases := []struct{ src, want string }{
		{kaminariRequire + `p Kaminari.paginate_array([]).page(1).page_entries_info`,
			"\"No entries found\"\n"},
		{kaminariRequire + `p Kaminari.paginate_array([1]).page(1).page_entries_info`,
			"\"Displaying 1 entry\"\n"},
		{kaminariRequire + `p Kaminari.paginate_array([1, 2, 3]).page(1).page_entries_info`,
			"\"Displaying all 3 entries\"\n"},
	}
	for _, c := range cases {
		if got := kaminariRun(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKaminariArrayOptions covers the paginate_array options Hash (total_count /
// limit / offset / padding and a non-integer value that is skipped), the bare and
// non-Hash-second-argument forms, and Array#page.
func TestKaminariArrayOptions(t *testing.T) {
	cases := []struct{ src, want string }{
		// total_count override drives total_pages; the other keys seed the scope; a
		// non-integer value (note:) is ignored.
		{kaminariRequire + `p Kaminari.paginate_array((1..10).to_a, total_count: 50, limit: 5, offset: 1, padding: 1, note: "x").total_pages`,
			"10\n"},
		// a non-Hash second argument leaves the defaults.
		{kaminariRequire + `p Kaminari.paginate_array((1..10).to_a, 7).total_count`, "10\n"},
		// bare paginate_array: total_count is the array length.
		{kaminariRequire + `p Kaminari.paginate_array((1..10).to_a).total_count`, "10\n"},
		// Array#page: a plain Array paginates itself.
		{kaminariRequire + `p [1, 2, 3, 4, 5].page(2).per(2).records`, "[3, 4]\n"},
		{kaminariRequire + `p [1, 2, 3, 4, 5].page(2).per(2).current_page`, "2\n"},
	}
	for _, c := range cases {
		if got := kaminariRun(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKaminariArgumentCoercion covers the non-integer / absent argument arms of
// page / per / padding (page and padding default, per goes unlimited).
func TestKaminariArgumentCoercion(t *testing.T) {
	base := kaminariRequire + `a = Kaminari.paginate_array((1..30).to_a)` + "\n"
	cases := []struct{ src, want string }{
		{base + `p a.page("x").current_page`, "1\n"},            // non-integer page -> 1
		{base + `p a.page.current_page`, "1\n"},                 // absent page -> 1
		{base + `p a.per("x").total_pages`, "1\n"},              // non-integer per -> unlimited
		{base + `p a.page(1).padding("x").offset_value`, "0\n"}, // non-integer padding -> 0
		{base + `p a.page(1).padding.offset_value`, "0\n"},      // absent padding -> 0
	}
	for _, c := range cases {
		if got := kaminariRun(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// stubRelSrc defines a pair of Ruby classes standing in for an injectable relation
// (respond_to count/offset/limit) so the Relation seam can be driven without a
// database.
const stubRelSrc = kaminariRequire + `
class StubRel
  def initialize(items); @items = items; end
  def count; @items.length; end
  def offset(n); StubWindow.new(@items, n); end
end
class StubWindow
  def initialize(items, off); @items = items; @off = off; end
  def limit(n); @items[@off, n]; end
end
`

// TestKaminariRelationSeam covers Kaminari.paginate over an injected stub relation:
// the Count seam (total_count), the Slice seam with a limit (records) and the
// scope metadata, plus the unlimited Slice arm (per(nil) skips #limit and returns
// the offset window whole).
func TestKaminariRelationSeam(t *testing.T) {
	cases := []struct{ src, want string }{
		{stubRelSrc + `r = Kaminari.paginate(StubRel.new((1..30).to_a)).page(2).per(10); p r.total_count`, "30\n"},
		{stubRelSrc + `r = Kaminari.paginate(StubRel.new((1..30).to_a)).page(2).per(10); p r.current_page`, "2\n"},
		{stubRelSrc + `r = Kaminari.paginate(StubRel.new((1..30).to_a)).page(2).per(10); p r.total_pages`, "3\n"},
		{stubRelSrc + `r = Kaminari.paginate(StubRel.new((1..30).to_a)).page(2).per(10); p r.records`,
			"[11, 12, 13, 14, 15, 16, 17, 18, 19, 20]\n"},
		{stubRelSrc + `r = Kaminari.paginate(StubRel.new((1..30).to_a)).page(2).per(10); p r.to_a`,
			"[11, 12, 13, 14, 15, 16, 17, 18, 19, 20]\n"},
		{stubRelSrc + `p Kaminari.paginate(StubRel.new((1..30).to_a)).padding(3).per(5).page(1).offset_value`, "3\n"},
		// per(nil): unlimited, so Slice takes the negative-limit arm and returns the
		// offset window object directly (StubWindow), not a materialised Array.
		{stubRelSrc + `p Kaminari.paginate(StubRel.new((1..30).to_a)).per(nil).records.class`, "StubWindow\n"},
		// #inspect renders the shell.
		{stubRelSrc + `p Kaminari.paginate(StubRel.new([]))`, "#<Kaminari::PaginatableRelation>\n"},
	}
	for _, c := range cases {
		if got := kaminariRun(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKaminariRaises covers the argument-guard raises of paginate_array (no
// argument / a non-Array argument) and paginate (no argument).
func TestKaminariRaises(t *testing.T) {
	kaminariRaises(t, "ArgumentError", kaminariRequire+`Kaminari.paginate_array`)
	kaminariRaises(t, "TypeError", kaminariRequire+`Kaminari.paginate_array(5)`)
	kaminariRaises(t, "ArgumentError", kaminariRequire+`Kaminari.paginate`)
}

// arKaminariSrc seeds an in-memory sqlite database and declares a User model plus
// an Account ActiveRecord::Base subclass, so the ActiveRecord-wired #page entry
// points (Model instance, Relation and Base class method) can be exercised against
// a live relation whose Count/Slice run real SQL.
const arKaminariSrc = `require "active_record"
require "kaminari"
ActiveRecord::Base.establish_connection(database: ":memory:")
c = ActiveRecord::Base.connection
c.execute("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
c.execute("INSERT INTO users (name) VALUES ('a'),('b'),('c'),('d'),('e')")
c.execute("CREATE TABLE accounts (id INTEGER PRIMARY KEY, name TEXT)")
c.execute("INSERT INTO accounts (name) VALUES ('x'),('y'),('z')")
User = ActiveRecord::Model.new("User", "users") do
  column :id, :integer
  column :name, :string
end
class Account < ActiveRecord::Base
end
`

// TestKaminariActiveRecord covers the ActiveRecord-wired #page: the Model factory
// instance (User.page), a chained Relation (User.all.page) and a Base subclass
// class method (Account.page), each reading the count and materialising the page
// through the seam's #count / #offset.#limit against a real sqlite relation.
func TestKaminariActiveRecord(t *testing.T) {
	cases := []struct{ src, want string }{
		// Model instance #page.
		{arKaminariSrc + `p User.page(2).per(2).total_count`, "5\n"},
		{arKaminariSrc + `p User.page(2).per(2).total_pages`, "3\n"},
		{arKaminariSrc + `p User.page(2).per(2).current_page`, "2\n"},
		{arKaminariSrc + `p User.page(2).per(2).records.to_a.map { |r| r[:name] }`,
			"[\"c\", \"d\"]\n"},
		// Relation #page (an already-chained query).
		{arKaminariSrc + `p User.all.page(1).per(2).records.to_a.map { |r| r[:name] }`,
			"[\"a\", \"b\"]\n"},
		// Base subclass class-method #page.
		{arKaminariSrc + `p Account.page(1).per(2).total_count`, "3\n"},
		{arKaminariSrc + `p Account.page(1).per(2).records.to_a.map { |r| r[:name] }`,
			"[\"x\", \"y\"]\n"},
	}
	for _, c := range cases {
		if got := kaminariRun(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// kaminariNilRel is a Go-level Relation whose Slice returns a non-object.Value
// (Go nil), reaching KaminariRelation#records' defensive arm that a Ruby-driven
// seam (always returning a Ruby value) never triggers.
type kaminariNilRel struct{}

func (kaminariNilRel) Count() int         { return 3 }
func (kaminariNilRel) Slice(_, _ int) any { return nil }

// TestKaminariShells covers the value shells (ToS/Inspect/Truthy) of both
// paginators directly.
func TestKaminariShells(t *testing.T) {
	a := &KaminariArray{}
	if a.ToS() != "#<Kaminari::PaginatableArray>" || a.Inspect() != a.ToS() || !a.Truthy() {
		t.Errorf("array shell: %q / %q / %v", a.ToS(), a.Inspect(), a.Truthy())
	}
	r := &KaminariRelation{}
	if r.ToS() != "#<Kaminari::PaginatableRelation>" || r.Inspect() != r.ToS() || !r.Truthy() {
		t.Errorf("relation shell: %q / %q / %v", r.ToS(), r.Inspect(), r.Truthy())
	}
}

// TestKaminariHelperArms covers the Go-only arms of the argument/result coercers:
// a non-integer #count degrades to 0, non-integer page/per/padding arguments take
// their defaults, and a nil metadata pointer maps to Ruby nil.
func TestKaminariHelperArms(t *testing.T) {
	if got := kaminariToInt(object.NewString("x")); got != 0 {
		t.Errorf("kaminariToInt non-integer got=%d", got)
	}
	if got := kaminariPageNum([]object.Value{object.NewString("x")}); got != 1 {
		t.Errorf("kaminariPageNum non-integer got=%d", got)
	}
	if got := kaminariPerPtr([]object.Value{object.NewString("x")}); got != nil {
		t.Errorf("kaminariPerPtr non-integer got=%v", got)
	}
	if got := kaminariPaddingNum([]object.Value{object.NewString("x")}); got != 0 {
		t.Errorf("kaminariPaddingNum non-integer got=%d", got)
	}
	if got := kaminariIntpValue(nil); got != object.NilV {
		t.Errorf("kaminariIntpValue nil got=%v", got)
	}
}

// TestKaminariRelationRecordsNonValue covers KaminariRelation#records' defensive
// arm: when the seam's Slice yields a non-Ruby value, #records returns nil.
func TestKaminariRelationRecordsNonValue(t *testing.T) {
	vm := New(&bytes.Buffer{})
	wrap := &KaminariRelation{p: kaminari.Paginate(kaminariNilRel{}).Page(1), vm: vm}
	if got := vm.send(wrap, "records", nil, nil); got != object.NilV {
		t.Errorf("records non-value arm got=%v", got)
	}
}
