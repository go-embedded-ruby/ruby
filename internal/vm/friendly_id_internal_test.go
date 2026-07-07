// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// fidCase runs one Ruby source through a fresh VM and compares captured stdout.
func fidCase(t *testing.T, src, want string) {
	t.Helper()
	if got := eval(t, src); got != want {
		t.Errorf("src=%q\n got=%q\nwant=%q", src, got, want)
	}
}

// fidModel is the standard sluggable used across the Ruby-driven cases: a PORO
// with title/id accessors, sluggable on :title with the :slugged + :history
// modules.
const fidModel = `require "friendly_id"
class Post
  extend FriendlyId
  attr_accessor :title, :id
  friendly_id :title, use: [:slugged, :history]
end
`

// TestFriendlyIDRequire covers the module surface and the require keys.
func TestFriendlyIDRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "friendly_id"`, "true\n"},
		{`require "friendly_id"; p require "friendly_id"`, "false\n"},
		{`require "friendly_id"; p FriendlyId.is_a?(Module)`, "true\n"},
		{`require "friendly_id"; p FriendlyId::RecordNotFound < StandardError`, "true\n"},
		{`require "friendly_id"; p FriendlyId::FinderMethods.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		fidCase(t, c.src, c.want)
	}
}

// TestFriendlyIDSlugGeneration covers the before-save slug generation, the
// collision-suffixing uniqueness seam and #to_param.
func TestFriendlyIDSlugGeneration(t *testing.T) {
	cases := []struct{ src, want string }{
		// A slug is generated from the base attribute on save.
		{fidModel + `a = Post.new; a.title = "Hello World"; a.save; p a.slug`, "\"hello-world\"\n"},
		// save! generates too.
		{fidModel + `a = Post.new; a.title = "Bang"; a.save!; p a.slug`, "\"bang\"\n"},
		// The explicit hook generates without going through save.
		{fidModel + `a = Post.new; a.title = "Manual"; a.set_friendly_id_slug; p a.slug`, "\"manual\"\n"},
		// A second record with the same base collides and gets a numeric suffix.
		{fidModel + `a = Post.new; a.title = "Dup"; a.save
b = Post.new; b.title = "Dup"; b.save; p b.slug`, "\"dup-2\"\n"},
		// #to_param is the slug once assigned.
		{fidModel + `a = Post.new; a.title = "Param Me"; a.save; p a.to_param`, "\"param-me\"\n"},
		// #to_param falls back to the id when no slug is set.
		{fidModel + `a = Post.new; a.id = 99; p a.to_param`, "\"99\"\n"},
		// A custom slug_column is honoured.
		{`require "friendly_id"
class Doc
  extend FriendlyId
  attr_accessor :name
  friendly_id :name, slug_column: :permalink
end
d = Doc.new; d.name = "My Doc"; d.save; p d.permalink`, "\"my-doc\"\n"},
	}
	for _, c := range cases {
		fidCase(t, c.src, c.want)
	}
}

// TestFriendlyIDRegeneration covers should_generate_new_friendly_id?: the slug is
// kept when the base attribute is unchanged and regenerated when it changes.
func TestFriendlyIDRegeneration(t *testing.T) {
	src := fidModel + `a = Post.new; a.id = 1; a.title = "First"; a.save; print a.slug, " "
a.save; print a.slug, " "
a.title = "Second"; a.save; p a.slug`
	fidCase(t, src, "first first \"second\"\n")
}

// TestFriendlyIDFinder covers Model.friendly.find over a current slug, a
// historical slug, a raw id and the not-found miss.
func TestFriendlyIDFinder(t *testing.T) {
	cases := []struct{ src, want string }{
		// By current slug.
		{fidModel + `a = Post.new; a.id = 1; a.title = "Alpha"; a.save
p Post.friendly.find("alpha").title`, "\"Alpha\"\n"},
		// By historical slug after a base change (:history module).
		{fidModel + `a = Post.new; a.id = 1; a.title = "Alpha"; a.save
a.title = "Beta"; a.save
p Post.friendly.find("alpha").id`, "1\n"},
		// By raw id fallback.
		{fidModel + `a = Post.new; a.id = 7; a.title = "Gamma"; a.save
p Post.friendly.find("7").title`, "\"Gamma\"\n"},
		// A miss raises FriendlyId::RecordNotFound.
		{fidModel + `begin; Post.friendly.find("nope"); rescue FriendlyId::RecordNotFound; p :nf; end`, ":nf\n"},
	}
	for _, c := range cases {
		fidCase(t, c.src, c.want)
	}
}

// TestFriendlyIDSyntheticID covers the synthesized-id path (a model with no id
// column) and its memoization across generations.
func TestFriendlyIDSyntheticID(t *testing.T) {
	src := `require "friendly_id"
class NoId
  extend FriendlyId
  attr_accessor :title
  friendly_id :title, use: :history
end
n = NoId.new; n.title = "Alpha"; print n.to_param, " "
n.save; print n.slug, " "
p NoId.friendly.find("alpha").title`
	fidCase(t, src, "fid-1 alpha \"Alpha\"\n")
}

// TestFriendlyIDActiveRecordSeam covers the ActiveRecord uniqueness seam: when
// the model responds to `where`, uniqueness is `where(slug:).exists?`.
func TestFriendlyIDActiveRecordSeam(t *testing.T) {
	src := `require "friendly_id"
class Rel
  def initialize(taken); @taken = taken; end
  def exists?; @taken; end
end
class ARish
  extend FriendlyId
  attr_accessor :title
  def self.where(h); Rel.new(h[:slug] == "taken"); end
  friendly_id :title
end
a = ARish.new; a.title = "Taken"; a.save; p a.slug`
	// "taken" is reported taken by the where seam, so Resolve suffixes to "taken-2"
	// (which the seam reports free).
	fidCase(t, src, "\"taken-2\"\n")
}

// TestFriendlyIDErrors covers the blank- and reserved-slug generation errors.
func TestFriendlyIDErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{fidModel + `a = Post.new; a.title = ""; begin; a.save; rescue ArgumentError; p :blank; end`, ":blank\n"},
		{fidModel + `a = Post.new; a.title = "new"; begin; a.save; rescue ArgumentError; p :reserved; end`, ":reserved\n"},
	}
	for _, c := range cases {
		fidCase(t, c.src, c.want)
	}
}

// TestFriendlyIDMisuse covers the argument/receiver guards.
func TestFriendlyIDMisuse(t *testing.T) {
	cases := []struct{ src, want string }{
		// friendly_id with no base attribute.
		{`require "friendly_id"
class Bad; extend FriendlyId; end
begin; Bad.friendly_id; rescue ArgumentError; p :a; end`, ":a\n"},
		// #find with no argument.
		{fidModel + `begin; Post.friendly.find; rescue ArgumentError; p :a; end`, ":a\n"},
		// friendly_id / friendly called on an instance (via include) rather than a
		// class raise TypeError.
		{`require "friendly_id"
class Inc; include FriendlyId; end
obj = Inc.new
begin; obj.friendly_id(:x); rescue TypeError; print :t1, " "; end
begin; obj.friendly; rescue TypeError; p :t2; end`, "t1 :t2\n"},
	}
	for _, c := range cases {
		fidCase(t, c.src, c.want)
	}
}

// TestFriendlyIDPriorSave covers the save / save! wrappers delegating to a prior
// definition after generating the slug.
func TestFriendlyIDPriorSave(t *testing.T) {
	src := `require "friendly_id"
class Wrapped
  extend FriendlyId
  attr_accessor :title
  def save; "ORIG"; end
  def save!; "ORIG!"; end
  friendly_id :title
end
w = Wrapped.new; w.title = "Wrap"; print w.save, " "; print w.slug, " "; p w.save!`
	fidCase(t, src, "ORIG wrap \"ORIG!\"\n")
}

// TestFriendlyIDScopeHandle covers the Go-side ToS / Inspect / Truthy of the
// friendlyIDScope handle, which the VM reaches only through its default
// formatting fallbacks (the scope's Ruby surface is just #find).
func TestFriendlyIDScopeHandle(t *testing.T) {
	s := &friendlyIDScope{}
	if s.ToS() != "#<FriendlyId::FinderMethods>" || s.Inspect() != "#<FriendlyId::FinderMethods>" || !s.Truthy() {
		t.Errorf("scope handle: to_s=%q inspect=%q truthy=%v", s.ToS(), s.Inspect(), s.Truthy())
	}
}

// TestFriendlyIDStr covers fidStr across its arms, including the nil and #to_s
// fallbacks the seam readers never happen to hit from Ruby.
func TestFriendlyIDStr(t *testing.T) {
	if fidStr(object.NilV) != "" {
		t.Error("nil arm")
	}
	if fidStr(object.NewString("x")) != "x" {
		t.Error("string arm")
	}
	if fidStr(object.Symbol("sym")) != "sym" {
		t.Error("symbol arm")
	}
	if fidStr(object.IntValue(7)) != "7" {
		t.Error("to_s fallback arm")
	}
}

// TestFriendlyIDUseFlags covers fidUseFlags for an Array of module names and a
// single Symbol.
func TestFriendlyIDUseFlags(t *testing.T) {
	arr := object.NewArray(object.Symbol("slugged"), object.Symbol("history"), object.Symbol("other"))
	if slugged, history := fidUseFlags(arr); !slugged || !history {
		t.Errorf("array: slugged=%v history=%v", slugged, history)
	}
	if slugged, history := fidUseFlags(object.Symbol("slugged")); !slugged || history {
		t.Errorf("single: slugged=%v history=%v", slugged, history)
	}
}

// TestFriendlyIDShouldGenerate covers the regeneration predicate.
func TestFriendlyIDShouldGenerate(t *testing.T) {
	if !fidShouldGenerate("", false) {
		t.Error("blank slug should generate")
	}
	if !fidShouldGenerate("slug", true) {
		t.Error("changed base should regenerate")
	}
	if fidShouldGenerate("slug", false) {
		t.Error("unchanged base with slug should not regenerate")
	}
}
