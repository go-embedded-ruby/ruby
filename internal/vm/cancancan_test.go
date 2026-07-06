// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// canPreamble is a shared preamble: a Post record and an Ability class mixing in
// CanCan::Ability. Rules are declared per-test in the Ability body. Asserted
// behaviour matches CanCanCan 3.6 on MRI.
const canPreamble = `require "cancancan"
Post = Struct.new(:owner, :status)
`

// TestCanCanBasic covers can/can?/cannot?/cannot and reverse-definition
// precedence (a later cannot overrides an earlier can).
func TestCanCanBasic(t *testing.T) {
	cases := []struct{ src, want string }{
		// a plain can rule on a class, queried by class and by instance.
		{canPreamble + `class A
  include CanCan::Ability
  def initialize = can(:read, Post)
end
a = A.new
p a.can?(:read, Post)
p a.can?(:read, Post.new("x"))
p a.cannot?(:read, Post)
p a.can?(:write, Post)`, "true\ntrue\nfalse\nfalse"},
		// reverse precedence: cannot after can wins.
		{canPreamble + `class A
  include CanCan::Ability
  def initialize
    can :manage, Post
    cannot :destroy, Post
  end
end
a = A.new
p a.can?(:read, Post)
p a.can?(:destroy, Post)`, "true\nfalse"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCanCanManageAll covers the :manage (action wildcard) and :all (subject
// wildcard) rules.
func TestCanCanManageAll(t *testing.T) {
	got := runSrc(t, canPreamble+`class Comment; end
class A
  include CanCan::Ability
  def initialize
    can :manage, Post
    can :read, :all
  end
end
a = A.new
p a.can?(:destroy, Post)
p a.can?(:read, Comment)
p a.can?(:read, Comment.new)
p a.can?(:write, Comment)`)
	if want := "true\ntrue\ntrue\nfalse"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestCanCanAliasAction covers alias_action expansion.
func TestCanCanAliasAction(t *testing.T) {
	got := runSrc(t, canPreamble+`class A
  include CanCan::Ability
  def initialize
    alias_action :update, :destroy, to: :modify
    can :modify, Post
  end
end
a = A.new
p a.can?(:update, Post)
p a.can?(:destroy, Post)
p a.can?(:read, Post)`)
	if want := "true\ntrue\nfalse"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestCanCanHashConditions covers hash conditions, including a []-membership
// value and a nested-hash (associated attribute) value, evaluated against an
// instance via the AttrGet seam.
func TestCanCanHashConditions(t *testing.T) {
	cases := []struct{ src, want string }{
		// scalar hash condition.
		{canPreamble + `class A
  include CanCan::Ability
  def initialize(u) = can(:update, Post, owner: u)
end
a = A.new("alice")
p a.can?(:update, Post.new("alice"))
p a.can?(:update, Post.new("bob"))`, "true\nfalse"},
		// array-membership condition.
		{canPreamble + `class A
  include CanCan::Ability
  def initialize = can(:read, Post, status: [:draft, :published])
end
a = A.new
p a.can?(:read, Post.new("x", :published))
p a.can?(:read, Post.new("x", :archived))`, "true\nfalse"},
		// nested-hash (associated attribute) condition.
		{`require "cancancan"
Author = Struct.new(:name)
Article = Struct.new(:author)
class A
  include CanCan::Ability
  def initialize = can(:read, Article, author: { name: "bob" })
end
a = A.new
p a.can?(:read, Article.new(Author.new("bob")))
p a.can?(:read, Article.new(Author.new("sue")))`, "true\nfalse"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCanCanBlockCondition covers a Ruby condition block evaluated through the
// BlockEval seam.
func TestCanCanBlockCondition(t *testing.T) {
	got := runSrc(t, canPreamble+`class A
  include CanCan::Ability
  def initialize(u)
    @u = u
    can :destroy, Post do |post|
      post.owner == @u
    end
  end
end
a = A.new("alice")
p a.can?(:destroy, Post.new("alice"))
p a.can?(:destroy, Post.new("bob"))`)
	if want := "true\nfalse"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestCanCanSubclassAncestry covers matching a subclass instance against a
// superclass rule via the Classified ancestor chain.
func TestCanCanSubclassAncestry(t *testing.T) {
	got := runSrc(t, canPreamble+`class Article < Post; end
class A
  include CanCan::Ability
  def initialize = can(:read, Post)
end
a = A.new
p a.can?(:read, Article.new("x"))`)
	if want := "true"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestCanCanInstanceRule covers a rule declared on a specific instance, matched
// by value equality.
func TestCanCanInstanceRule(t *testing.T) {
	got := runSrc(t, canPreamble+`p1 = Post.new("a")
p2 = Post.new("b")
$p1, $p2 = p1, p2
class A
  include CanCan::Ability
  def initialize = can(:read, $p1)
end
a = A.new
p a.can?(:read, $p1)
p a.can?(:read, $p2)`)
	if want := "true\nfalse"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestCanCanArraySubjectAndAction covers array-valued action and subject
// arguments to can.
func TestCanCanArraySubjectAndAction(t *testing.T) {
	got := runSrc(t, canPreamble+`class Comment; end
class A
  include CanCan::Ability
  def initialize = can([:read, :update], [Post, Comment])
end
a = A.new
p a.can?(:read, Post)
p a.can?(:update, Comment)
p a.can?(:destroy, Post)`)
	if want := "true\ntrue\nfalse"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestCanCanSymbolSubject covers a Symbol subject token (non-model) on both
// declaration and query.
func TestCanCanSymbolSubject(t *testing.T) {
	got := runSrc(t, canPreamble+`class A
  include CanCan::Ability
  def initialize = can(:read, :dashboard)
end
a = A.new
p a.can?(:read, :dashboard)
p a.can?(:read, :settings)`)
	if want := "true\nfalse"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestCanCanStringActions covers String (rather than Symbol) action arguments on
// both declaration and query.
func TestCanCanStringActions(t *testing.T) {
	got := runSrc(t, canPreamble+`class A
  include CanCan::Ability
  def initialize = can("read", Post)
end
a = A.new
p a.can?("read", Post)`)
	if want := "true"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestCanCanAuthorizeBang covers authorize! returning the subject on success and
// raising CanCan::AccessDenied (with the default message) on denial.
func TestCanCanAuthorizeBang(t *testing.T) {
	cases := []struct{ src, want string }{
		{canPreamble + `class A
  include CanCan::Ability
  def initialize = can(:read, Post)
end
p A.new.authorize!(:read, Post).name`, `"Post"`},
		{canPreamble + `class A
  include CanCan::Ability
  def initialize = can(:read, Post)
end
begin
  A.new.authorize!(:destroy, Post)
rescue CanCan::AccessDenied => e
  p e.message
end`, `"You are not authorized to access this page."`},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCanCanErrorsAndArity covers the argument/type guards on the DSL surface.
func TestCanCanErrorsAndArity(t *testing.T) {
	cases := []struct{ src, want string }{
		// can with too few args.
		{canPreamble + `class A
  include CanCan::Ability
  def initialize = can(:read)
end
begin; A.new; rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 1, expected 2+)"`},
		// can? with too few args.
		{canPreamble + `class A
  include CanCan::Ability
end
begin; A.new.can?(:read); rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 1, expected 2)"`},
		// action of the wrong type on a rule.
		{canPreamble + `class A
  include CanCan::Ability
  def initialize = can(123, Post)
end
begin; A.new; rescue ArgumentError => e; p e.message; end`,
			`"action must be a Symbol, String, or Array"`},
		// action of the wrong type on a query.
		{canPreamble + `class A
  include CanCan::Ability
end
begin; A.new.can?(123, Post); rescue TypeError => e; p e.message; end`,
			`"action must be a Symbol or String"`},
		// alias_action with no to:.
		{canPreamble + `class A
  include CanCan::Ability
  def initialize = alias_action(:a, :b)
end
begin; A.new; rescue ArgumentError => e; p e.message; end`,
			`"alias_action requires a to: target"`},
		// alias_action with a hash but no :to key.
		{canPreamble + `class A
  include CanCan::Ability
  def initialize = alias_action(:a, from: :x)
end
begin; A.new; rescue ArgumentError => e; p e.message; end`,
			`"alias_action requires a to: target"`},
		// non-Symbol/String condition key.
		{canPreamble + `class A
  include CanCan::Ability
  def initialize = can(:read, Post, { 1 => 2 })
end
begin; A.new.can?(:read, Post.new("x")); rescue ArgumentError => e; p e.message; end`,
			`"condition key must be a Symbol or String"`},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCanCanRequire covers the require contract (both "cancancan" and "cancan")
// and the module identity.
func TestCanCanRequire(t *testing.T) {
	got := runSrc(t, `p require "cancancan"
p require "cancancan"
p require "cancan"
p CanCan::Ability.class`)
	if want := "true\nfalse\ntrue\nModule"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestCanCanStringConditionKey covers a String (rather than Symbol) condition
// key via cancanCondKey's String branch.
func TestCanCanStringConditionKey(t *testing.T) {
	got := runSrc(t, canPreamble+`class A
  include CanCan::Ability
  def initialize = can(:update, Post, "owner" => "alice")
end
a = A.new
p a.can?(:update, Post.new("alice"))
p a.can?(:update, Post.new("bob"))`)
	if want := "true\nfalse"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestCanCanBoxAndUnwrap covers the never-user-visible box Value surface and the
// cancanUnwrap fallback for a value that is neither a wrapper nor a bare
// object.Value.
func TestCanCanBoxAndUnwrap(t *testing.T) {
	b := &cancanAbilityBox{}
	if b.ToS() != "#<CanCan::Ability state>" || b.Inspect() != "#<CanCan::Ability state>" || !b.Truthy() {
		t.Fatalf("box surface: ToS=%q Inspect=%q Truthy=%v", b.ToS(), b.Inspect(), b.Truthy())
	}
	if v := cancanUnwrap(42); v != object.NilV {
		t.Fatalf("cancanUnwrap(int) = %v, want nil", v)
	}
	if v := cancanUnwrap(object.Integer(3)); v != object.Integer(3) {
		t.Fatalf("cancanUnwrap(value) = %v, want 3", v)
	}
}
