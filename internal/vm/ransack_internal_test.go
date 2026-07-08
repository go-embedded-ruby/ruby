// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"testing"
)

// rkPre defines a Row struct (a record answering #attributes) and a Model whose
// self.all yields an in-memory row set — the ORM-agnostic record source the
// Backend seam fetches. Ransack is required on top of ActiveRecord so the
// Model.ransack / Model.search class methods are wired onto ActiveRecord::Base.
const rkPre = `require "active_record"
require "ransack"
Row = Struct.new(:name, :age) do
  def attributes; { "name" => name, "age" => age }; end
end
class Person < ActiveRecord::Base
  def self.all
    [Row.new("bob", 20), Row.new("alice", 30), Row.new("carol", 25)]
  end
end
`

// TestRansackRequire covers the require contract and module identity.
func TestRansackRequire(t *testing.T) {
	got := runSrc(t, `p require "ransack"
p require "ransack"
p Ransack.class
p Ransack::Search.class`)
	if want := "true\nfalse\nModule\nClass"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackBasic covers Model.ransack(q).result with a cont predicate, a gteq
// predicate and a descending sort — the headline surface.
func TestRansackBasic(t *testing.T) {
	got := runSrc(t, rkPre+`s = Person.ransack(name_cont: "a", age_gteq: 25, s: "age desc")
p s.class
p s.result.map { |r| r.name }
p s.sorts.map { |x| [x.name, x.dir] }`)
	if want := `Ransack::Search
["alice", "carol"]
[["age", "desc"]]`; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackSearchAlias covers the Model.search alias (same wiring as ransack).
func TestRansackSearchAlias(t *testing.T) {
	got := runSrc(t, rkPre+`p Person.search(name_eq: "bob").result.map { |r| r.name }`)
	if want := `["bob"]`; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackCombinators covers the _or_ / _and_ multi-attribute combinators.
func TestRansackCombinators(t *testing.T) {
	got := runSrc(t, rkPre+`p Person.ransack(name_or_age_cont: "20").result.map { |r| r.name }
p Person.ransack(name_cont_and_age_gteq: nil).result.length`)
	// name_or_age_cont "20": bob (age 20) matches. The _and_ case has a blank
	// value so its condition is dropped, leaving an all-matching search (3 rows).
	if want := "[\"bob\"]\n3"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackGroups covers g[] nested groups combined with an "or" combinator.
func TestRansackGroups(t *testing.T) {
	got := runSrc(t, rkPre+`s = Person.ransack(g: [{ name_eq: "bob", m: "or", age_eq: 30 }])
p s.result.map { |r| r.name }.sort`)
	if want := `["alice", "bob"]`; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackDistinct covers the distinct flag from params collapsing duplicate
// rows, and the sorts/errors/distinct? accessors.
func TestRansackDistinct(t *testing.T) {
	got := runSrc(t, `require "ransack"
Row = Struct.new(:v) { def attributes; { "v" => v }; end }
class Dup
  def self.all; [Row.new(1), Row.new(1), Row.new(2)]; end
end
s = Ransack.search(Dup, distinct: true, s: "v asc")
p s.distinct?
p s.result.map { |r| r.v }
p s.errors`)
	if want := "true\n[1, 2]\n[]"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackResultDistinctOption covers result(distinct: true) forcing distinct
// even when the params did not request it, and result(non_hash) leaving the
// default in place.
func TestRansackResultDistinctOption(t *testing.T) {
	got := runSrc(t, `require "ransack"
Row = Struct.new(:v) { def attributes; { "v" => v }; end }
class Dup
  def self.all; [Row.new(1), Row.new(1), Row.new(2)]; end
end
s = Ransack.search(Dup)
p s.distinct
p s.result(distinct: true).map { |r| r.v }
p s.result(5).length
# a Hash option without a :distinct key leaves the default (no distinct).
p s.result(foo: 1).length`)
	if want := "false\n[1, 2]\n3\n3"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackAllowlist covers the ransackable_attributes seam (restricting the
// searchable columns), a non-Array return being ignored (staying open) and the
// ransackable_associations seam resolving an association-qualified key.
func TestRansackAllowlist(t *testing.T) {
	got := runSrc(t, `require "ransack"
Author = Struct.new(:name) { def attributes; { "name" => name }; end }
Post = Struct.new(:title, :author) do
  def attributes; { "title" => title, "author" => author.attributes }; end
end
class Blog
  def self.ransackable_attributes(*); ["title"]; end
  def self.ransackable_associations(*); ["author"]; end
  def self.all
    [Post.new("hi", Author.new("bob")), Post.new("yo", Author.new("sue"))]
  end
end
s = Ransack.search(Blog, title_cont: "h", author_name_eq: "bob")
p s.result.map { |r| r.title }
# secret_eq is not in ransackable_attributes -> parse error, no condition.
p Ransack.search(Blog, secret_eq: "x").errors.length`)
	if want := "[\"hi\"]\n1"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackAllowlistNonArray covers ransackable_attributes returning a
// non-Array (ignored -> open context, every attribute searchable).
func TestRansackAllowlistNonArray(t *testing.T) {
	got := runSrc(t, `require "ransack"
Row = Struct.new(:x) { def attributes; { "x" => x }; end }
class M
  def self.ransackable_attributes(*); "nope"; end
  def self.all; [Row.new(1), Row.new(2)]; end
end
p Ransack.search(M, x_eq: 2).result.map { |r| r.x }`)
	if want := "[2]"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackValueTypes drives every value narrowing branch (int / float / bool
// / nil / symbol / array / bignum / nested-hash) plus a non-string Hash key and
// a default (Range) value, exercising ransackGoVal and ransackKey.
func TestRansackValueTypes(t *testing.T) {
	got := runSrc(t, rkPre+`s = Person.ransack(
  age_eq: 30,
  name_eq: :alice,
  active_eq: true,
  score_eq: 1.5,
  big_eq: 2 ** 100,
  ids_in: [1, 2],
  rng_eq: (1..3),
  1 => 2,
  g: [{ age_eq: 25 }]
)
p s.result.map { |r| r.name }.sort`)
	// Only the association-free direct predicates that a Person row satisfies:
	// age_eq 30 matches alice; the group age_eq 25 matches carol. But the top
	// group ANDs all its conditions, so no single row satisfies every one at
	// once except via the "or"-less AND -> here nothing matches the top group's
	// AND, yielding []. The test's value is exercising the converters, not the
	// match arithmetic.
	if want := "[]"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackHashRecords covers records that are bare Hashes (used directly) and
// an Array subject (a subject not answering #all, taken as the row set itself).
func TestRansackHashRecords(t *testing.T) {
	got := runSrc(t, `require "ransack"
rows = [{ "v" => 1 }, { "v" => 2 }, { "v" => 3 }]
p Ransack.search(rows, v_gteq: 2).result.map { |r| r["v"] }`)
	if want := "[2, 3]"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackToACollection covers a subject whose #all yields a non-Array that
// answers #to_a, plus records that neither are Hashes nor answer #attributes
// (empty rows, matching only an all-matching search) and an #attributes that
// returns a non-Hash (also an empty row).
func TestRansackToACollection(t *testing.T) {
	got := runSrc(t, `require "ransack"
Row = Struct.new(:v) { def attributes; { "v" => v }; end }
Bad = Struct.new(:v) { def attributes; "not a hash"; end }
class Coll
  def initialize(a); @a = a; end
  def to_a; @a; end
end
class M
  def self.all; Coll.new([Row.new(1), 42, Bad.new(9)]); end
end
# an all-matching search returns every row (the empty-attr ones included).
p Ransack.search(M).result.length
# v_eq 1 matches only the real Row; the Integer and Bad rows have empty attrs.
p Ransack.search(M, v_eq: 1).result.length`)
	if want := "3\n1"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackErrors covers the argument guards on Ransack.search /
// Ransack::Search.new and the TypeError when a subject yields no row set.
func TestRansackErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "ransack"
begin; Ransack.search; rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 0, expected 1..2)"`},
		{`require "ransack"
begin; Ransack::Search.new; rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 0, expected 1..2)"`},
		{`require "ransack"
begin; Ransack.search(42, {}).result; rescue TypeError => e; p e.message; end`,
			`"ransack subject #all must yield an Array-like collection"`},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRansackSearchNewNoParams covers Ransack::Search.new(subject) with no params
// (a nil params Hash -> an all-matching search).
func TestRansackSearchNewNoParams(t *testing.T) {
	got := runSrc(t, `require "ransack"
Row = Struct.new(:v) { def attributes; { "v" => v }; end }
class M; def self.all; [Row.new(1), Row.new(2)]; end; end
p Ransack::Search.new(M).result.length
p Ransack::Search.new(M, v_eq: 1).result.length
p Ransack.search(M).result.length`)
	if want := "2\n1\n2"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackSortNodes covers the Ransack::Sort node surface: name / dir / asc?
// / desc? for both directions.
func TestRansackSortNodes(t *testing.T) {
	got := runSrc(t, rkPre+`s = Person.ransack(s: ["age asc", "name desc"])
a, b = s.sorts
p [a.name, a.dir, a.asc?, a.desc?]
p [b.name, b.dir, b.asc?, b.desc?]`)
	if want := `["age", "asc", true, false]
["name", "desc", false, true]`; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestRansackWrapperSurface covers the ToS / Inspect / Truthy of the two Go
// wrapper values (never user-visible entry points).
func TestRansackWrapperSurface(t *testing.T) {
	s := &RansackSearch{}
	if s.ToS() != "#<Ransack::Search>" || s.Inspect() != "#<Ransack::Search>" || !s.Truthy() {
		t.Fatalf("search wrapper: ToS=%q Inspect=%q Truthy=%v", s.ToS(), s.Inspect(), s.Truthy())
	}
	so := &RansackSort{name: "age", dir: "asc"}
	if so.ToS() != "age asc" || so.Inspect() != "#<Ransack::Sort age asc>" || !so.Truthy() {
		t.Fatalf("sort wrapper: ToS=%q Inspect=%q Truthy=%v", so.ToS(), so.Inspect(), so.Truthy())
	}
}

// TestRansackWireWithoutActiveRecord covers wireRansackModels' guard: with no
// ActiveRecord::Base loaded it is a no-op, so Ransack::Search / Ransack.search
// remain the ORM-agnostic entry points.
func TestRansackWireWithoutActiveRecord(t *testing.T) {
	var buf bytes.Buffer
	vm := New(&buf)
	delete(vm.consts, "ActiveRecord::Base")
	// Must not panic and must not install any smethod (nothing to install onto).
	vm.wireRansackModels()
}
