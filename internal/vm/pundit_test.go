// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io"
	"testing"

	"github.com/go-ruby-pundit/pundit"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// punditPreamble is a shared preamble: a Post record, its PostPolicy (with an
// inner Scope), and a Controller mixing in Pundit with a current_user. Asserted
// behaviour matches Pundit 2.x on MRI.
const punditPreamble = `require "pundit"
Post = Struct.new(:owner)
class PostPolicy
  def initialize(user, post) = (@user, @post = user, post)
  def update? = @user == @post.owner
  def destroy? = false
  class Scope
    def initialize(user, scope) = (@user, @scope = user, scope)
    def resolve = ["visible-for-#{@user}"]
  end
end
class Controller
  include Pundit
  attr_reader :current_user
  def initialize(u) = @current_user = u
end
`

func TestPunditAuthorize(t *testing.T) {
	cases := []struct{ src, want string }{
		// allow: authorize returns the record (Pundit's contract).
		{punditPreamble + `c = Controller.new("alice")
p c.authorize(Post.new("alice"), :update?).owner`, `"alice"`},
		// deny: a false predicate raises NotAuthorizedError with MRI's message.
		{punditPreamble + `c = Controller.new("alice")
begin
  c.authorize(Post.new("alice"), :destroy?)
rescue Pundit::NotAuthorizedError => e
  p e.message
end`, `"not allowed to PostPolicy#destroy? this Post"`},
		// query string form is accepted too.
		{punditPreamble + `c = Controller.new("alice")
p c.authorize(Post.new("alice"), "update?").owner`, `"alice"`},
		// not-defined: no policy class for a bare Object raises NotDefinedError.
		{punditPreamble + `c = Controller.new("alice")
begin
  c.authorize(Object.new, :show?)
rescue Pundit::NotDefinedError => e
  p e.class
end`, `Pundit::NotDefinedError`},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPunditAuthorizeArity covers the zero-argument ArgumentError.
func TestPunditAuthorizeArity(t *testing.T) {
	got := runSrc(t, punditPreamble+`c = Controller.new("a")
begin
  c.authorize
rescue ArgumentError => e
  p e.message
end`)
	if want := `"wrong number of arguments (given 0, expected 1..2)"`; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestPunditQueryFromActionName covers deriving the query from action_name when
// none is passed, and the raise when neither is available.
func TestPunditQueryFromActionName(t *testing.T) {
	// action_name -> "update?" so authorize succeeds.
	src := `require "pundit"
Post = Struct.new(:owner)
class PostPolicy
  def initialize(u, p) = (@u, @p = u, p)
  def update? = true
end
class Ctrl
  include Pundit
  def current_user = "x"
  def action_name = "update"
end
p Ctrl.new.authorize(Post.new("x")).owner`
	if got := runSrc(t, src); got != `"x"` {
		t.Fatalf("action_name derive: got=%q", got)
	}
	// no query and no action_name -> ArgumentError.
	src2 := punditPreamble + `c = Controller.new("a")
begin
  c.authorize(Post.new("a"))
rescue ArgumentError => e
  p e.message
end`
	if got := runSrc(t, src2); got != `"no query given and no action_name to derive one from"` {
		t.Fatalf("no-query raise: got=%q", got)
	}
}

// TestPunditUserSources covers pundit_user taking precedence over current_user,
// and the nil fallback when neither is defined.
func TestPunditUserSources(t *testing.T) {
	// pundit_user wins over current_user.
	src := `require "pundit"
Rec = Struct.new(:x)
class RecPolicy
  def initialize(u, r) = (@u, @r = u, r)
  def show? = @u == "pundit"
end
class C
  include Pundit
  def pundit_user = "pundit"
  def current_user = "current"
end
p C.new.authorize(Rec.new(1), :show?).x`
	if got := runSrc(t, src); got != `1` {
		t.Fatalf("pundit_user precedence: got=%q", got)
	}
	// neither user method -> nil user (predicate sees nil).
	src2 := `require "pundit"
Rec = Struct.new(:x)
class RecPolicy
  def initialize(u, r) = (@u, @r = u, r)
  def show? = @u.nil?
end
class C2
  include Pundit
end
p C2.new.authorize(Rec.new(1), :show?).x`
	if got := runSrc(t, src2); got != `1` {
		t.Fatalf("nil user: got=%q", got)
	}
}

// TestPunditPolicy covers the policy instance helper (instance + module forms)
// and the nil-when-undefined contract.
func TestPunditPolicy(t *testing.T) {
	cases := []struct{ src, want string }{
		{punditPreamble + `c = Controller.new("alice")
p c.policy(Post.new("alice")).class`, `PostPolicy`},
		// undefined policy -> nil.
		{punditPreamble + `c = Controller.new("alice")
p c.policy(Object.new)`, `nil`},
		// Pundit.policy(user, record) module form.
		{punditPreamble + `p Pundit.policy("bob", Post.new("bob")).class`, `PostPolicy`},
		// Pundit.policy! raises when undefined.
		{punditPreamble + `begin
  Pundit.policy!("bob", Object.new)
rescue Pundit::NotDefinedError => e
  p e.message
end`, `"unable to find policy ` + "`ObjectPolicy`" + ` for ` + "`Object`" + `"`},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPunditPolicyArity covers the ArgumentError arity guards on the policy
// helpers (instance and module forms).
func TestPunditPolicyArity(t *testing.T) {
	cases := []struct{ src, want string }{
		{punditPreamble + `begin; Controller.new("a").policy; rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 0, expected 1)"`},
		{punditPreamble + `begin; Controller.new("a").policy_scope; rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 0, expected 1)"`},
		{punditPreamble + `begin; Pundit.authorize("u", Post.new("u")); rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 2, expected 3)"`},
		{punditPreamble + `begin; Pundit.policy("u"); rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 1, expected 2)"`},
		{punditPreamble + `begin; Pundit.policy!("u"); rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 1, expected 2)"`},
		{punditPreamble + `begin; Pundit.policy_scope("u"); rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 1, expected 2)"`},
		{punditPreamble + `begin; Pundit.policy_scope!("u"); rescue ArgumentError => e; p e.message; end`,
			`"wrong number of arguments (given 1, expected 2)"`},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPunditModuleAuthorize covers Pundit.authorize allow and deny.
func TestPunditModuleAuthorize(t *testing.T) {
	cases := []struct{ src, want string }{
		{punditPreamble + `p Pundit.authorize("alice", Post.new("alice"), :update?).owner`, `"alice"`},
		{punditPreamble + `begin
  Pundit.authorize("alice", Post.new("alice"), :destroy?)
rescue Pundit::NotAuthorizedError => e
  p e.message
end`, `"not allowed to PostPolicy#destroy? this Post"`},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPunditPolicyScope covers the scope resolution (instance + module + bang
// forms) and the nil-when-undefined contract plus the bang NotDefinedError.
func TestPunditPolicyScope(t *testing.T) {
	cases := []struct{ src, want string }{
		{punditPreamble + `p Controller.new("alice").policy_scope(Post)`, `["visible-for-alice"]`},
		{punditPreamble + `p Pundit.policy_scope("bob", Post)`, `["visible-for-bob"]`},
		{punditPreamble + `p Pundit.policy_scope!("bob", Post)`, `["visible-for-bob"]`},
		// undefined scope, non-bang -> nil.
		{punditPreamble + `p Pundit.policy_scope("bob", Object.new)`, `nil`},
		// undefined scope, bang -> NotDefinedError.
		{punditPreamble + `begin
  Pundit.policy_scope!("bob", Object.new)
rescue Pundit::NotDefinedError => e
  p e.class
end`, `Pundit::NotDefinedError`},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPunditSubjectKinds covers the Subject-building branches: a Class subject
// (Post -> PostPolicy), a Symbol subject (:post -> PostPolicy), a namespaced
// Array subject ([:admin, post] -> Admin::PostPolicy) and a policy_class override.
func TestPunditSubjectKinds(t *testing.T) {
	// Class subject.
	classSrc := `require "pundit"
class Post; end
class PostPolicy
  def initialize(u, r) = @r = r
  def index? = true
end
p Pundit.authorize("u", Post, :index?)`
	if got := runSrc(t, classSrc); got != `Post` {
		t.Fatalf("class subject: got=%q", got)
	}
	// Symbol subject.
	symSrc := `require "pundit"
class PostPolicy
  def initialize(u, r) = @r = r
  def index? = true
end
p Pundit.authorize("u", :post, :index?)`
	if got := runSrc(t, symSrc); got != `:post` {
		t.Fatalf("symbol subject: got=%q", got)
	}
	// Namespaced Array subject.
	arrSrc := `require "pundit"
Post = Struct.new(:owner)
module Admin
  class PostPolicy
    def initialize(u, r) = @r = r
    def edit? = true
  end
end
post = Post.new("a")
p Pundit.authorize("u", [:admin, post], :edit?).owner`
	if got := runSrc(t, arrSrc); got != `"a"` {
		t.Fatalf("array subject: got=%q", got)
	}
	// policy_class override.
	pcSrc := `require "pundit"
class CustomPolicy
  def initialize(u, r) = @r = r
  def show? = true
end
class Widget
  def policy_class = CustomPolicy
end
p Pundit.authorize("u", Widget.new, :show?).class`
	if got := runSrc(t, pcSrc); got != `Widget` {
		t.Fatalf("policy_class subject: got=%q", got)
	}
	// policy_class override returning a non-class value routes through ToS.
	pcNameSrc := `require "pundit"
class CustomPolicy
  def initialize(u, r) = @r = r
  def show? = true
end
class Widget
  def policy_class = :CustomPolicy
end
p Pundit.authorize("u", Widget.new, :show?).class`
	if got := runSrc(t, pcNameSrc); got != `Widget` {
		t.Fatalf("policy_class symbol subject: got=%q", got)
	}
	// policy_class override returning a String name (punditClassName String branch).
	pcStrSrc := `require "pundit"
class CustomPolicy
  def initialize(u, r) = @r = r
  def show? = true
end
class Widget
  def policy_class = "CustomPolicy"
end
p Pundit.authorize("u", Widget.new, :show?).class`
	if got := runSrc(t, pcStrSrc); got != `Widget` {
		t.Fatalf("policy_class string subject: got=%q", got)
	}
	// policy_class override returning some other value routes through ToS
	// (punditClassName default branch); "5" is not a class -> NotDefinedError.
	pcOtherSrc := `require "pundit"
class Widget
  def policy_class = 5
end
begin
  Pundit.authorize("u", Widget.new, :show?)
rescue Pundit::NotDefinedError
  p :nd
end`
	if got := runSrc(t, pcOtherSrc); got != `:nd` {
		t.Fatalf("policy_class other subject: got=%q", got)
	}
}

// TestPunditDefinedNonClass covers the Defined seam rejecting a derived name that
// resolves to a non-class constant (so authorize raises NotDefinedError).
func TestPunditDefinedNonClass(t *testing.T) {
	got := runSrc(t, `require "pundit"
class Thing; end
ThingPolicy = 5
begin
  Pundit.authorize("u", Thing.new, :ok?)
rescue Pundit::NotDefinedError => e
  p e.class
end`)
	if got != `Pundit::NotDefinedError` {
		t.Fatalf("non-class policy: got=%q", got)
	}
}

// TestPunditRequire covers the require contract and the module identity.
func TestPunditRequire(t *testing.T) {
	got := runSrc(t, `p require "pundit"
p require "pundit"
p Pundit.class
p Pundit::Authorization.equal?(Pundit)`)
	if want := "true\nfalse\nModule\ntrue"; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestPunditBadQueryArg covers punditNameArg's TypeError branch.
func TestPunditBadQueryArg(t *testing.T) {
	got := runSrc(t, punditPreamble+`begin
  Controller.new("a").authorize(Post.new("a"), 123)
rescue TypeError => e
  p e.message
end`)
	if want := `"123 is not a symbol nor a string"`; got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// TestPunditHelpersDirect covers the never-Ruby-reachable defensive branches of
// the value helpers directly.
func TestPunditHelpersDirect(t *testing.T) {
	// punditRubyOf: a Subject whose Ruby is not an object.Value -> nil.
	if v := punditRubyOf(pundit.Subject{Name: "X"}); v != object.NilV {
		t.Fatalf("punditRubyOf(bare subject) = %v, want nil", v)
	}
	// punditRubyOf: a bare object.Value (non-Subject) passes through punditVal.
	if v := punditRubyOf(object.Integer(7)); v != object.Integer(7) {
		t.Fatalf("punditRubyOf(int value) = %v, want 7", v)
	}
	// punditVal: a non-object.Value any -> nil.
	if v := punditVal(42); v != object.NilV {
		t.Fatalf("punditVal(int) = %v, want nil", v)
	}
	// constByName: first segment missing, a mid segment that is not a class, and
	// a mid segment missing.
	vm := New(io.Discard)
	if _, ok := vm.constByName("NoSuchTop"); ok {
		t.Fatalf("constByName(missing top) reported ok")
	}
	vm.consts["Leaf"] = object.Integer(1)
	if _, ok := vm.constByName("Leaf::Deeper"); ok {
		t.Fatalf("constByName(non-class mid) reported ok")
	}
	holder := newClass("Holder", vm.cObject)
	vm.consts["Holder"] = holder
	if _, ok := vm.constByName("Holder::Missing"); ok {
		t.Fatalf("constByName(missing mid) reported ok")
	}
	holder.consts["Inner"] = newClass("Holder::Inner", vm.cObject)
	if _, ok := vm.constByName("Holder::Inner"); !ok {
		t.Fatalf("constByName(present nested) reported not ok")
	}
}

// TestPunditRaiseErrDefault covers raisePunditErr's fallback branch: a generic
// engine error maps to Pundit::Error (the base class). No Ruby path produces a
// non-typed engine error, so drive it directly.
func TestPunditRaiseErrDefault(t *testing.T) {
	vm := New(io.Discard)
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok {
			t.Fatalf("expected RubyError, got %#v", r)
		}
		if re.Class != "Pundit::Error" || re.Message != "boom" {
			t.Fatalf("got class=%q msg=%q", re.Class, re.Message)
		}
	}()
	vm.raisePunditErr(errors.New("boom"))
}
