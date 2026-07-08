// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	factorybot "github.com/go-ruby-factory-bot/factory-bot"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestFactoryBotHappyPath drives the full Ruby surface — FactoryBot.define with
// static/dynamic attributes, a global sequence, an association, a transient, a
// trait, a nested factory and after(:build)/after(:create) callbacks — through
// build / create / attributes_for / build_list / generate, asserting the
// object-model seams (instantiate + assign, persist, block bodies) end to end.
func TestFactoryBotHappyPath(t *testing.T) {
	src := `
require "factory_bot"

class Account
  attr_accessor :name, :balance
end

class User
  attr_accessor :name, :email, :admin, :account, :full
  def save!; @saved = true; end
  def saved; @saved; end
end

FactoryBot.define do
  sequence(:email) { |n| "user#{n}@example.com" }

  factory :account do
    name "Acct"
    balance 100
  end

  factory :user do
    name "Alice"
    email { |e| "#{e.name.downcase}@example.com" }
    admin false
    association :account
    transient do
      greeting "hi"
    end
    after(:build) { |u, ev| u.full = "#{u.name}:#{ev.greeting}" }
    after(:create) { |u| u.name = u.name + "!" }

    trait :admin do
      admin true
    end

    factory :super_user do
      name "Root"
    end
  end
end

u = FactoryBot.build(:user)
puts u.name
puts u.email
puts u.admin
puts u.account.name
puts u.full
a = FactoryBot.build(:user, :admin, name: "Bob")
puts a.name
puts a.admin
c = FactoryBot.create(:user, name: "Carol")
puts c.saved
puts c.name
su = FactoryBot.build(:super_user)
puts su.name
puts FactoryBot.attributes_for(:account).inspect
puts FactoryBot.build_list(:account, 2).length
puts FactoryBot.create_list(:account, 3).length
puts FactoryBot.generate(:email)
`
	want := strings.Join([]string{
		"Alice", "alice@example.com", "false", "Acct", "Alice:hi",
		"Bob", "true", "true", "Carol!", "Root",
		`{balance: 100, name: "Acct"}`, "2", "3", "user1@example.com",
	}, "\n")
	if got := runSrc(t, src); got != want {
		t.Errorf("factory_bot happy path:\n got=%q\nwant=%q", got, want)
	}
}

// TestFactoryBotSeams covers the remaining seam branches: the instance-variable
// assignment fallback (no writer), both persistence forms (save! and save) and
// the persist no-op, class resolution by explicit name / by Class constant / by
// camelized default, a before(:create) callback, an unresolved sibling read
// yielding nil, a sequence with an explicit start, and a bodyless trait.
func TestFactoryBotSeams(t *testing.T) {
	src := `
require "factory_bot"
class Plain; end
class Saver
  attr_accessor :name
  def save; @saved = true; end
  def saved; @saved; end
end
class Doer
  attr_accessor :name
  def save!; @bang = true; end
  def banged; @bang; end
end
class Account; attr_accessor :n; end

FactoryBot.define do
  sequence :counter
  factory :plain do
    foo "bar"
    trait :empty
  end
  factory :saver do
    name "S"
  end
  factory :doer do
    name "D"
    before(:create) { |u| u.name = "before" }
  end
  factory :acct, class: "Account" do
    n 1
  end
  factory :acct2, class: Account do
    n 2
  end
  factory :nilname, class: "Saver" do
    name { |e| e.missing }
  end
  factory :starter, class: "Saver" do
    sequence(:code, 100) { |n| n }
  end
end

puts FactoryBot.build(:plain).instance_variable_get(:@foo)
puts FactoryBot.create(:saver).saved
FactoryBot.create(:acct)
puts "acct-ok"
puts FactoryBot.create(:doer).name
puts FactoryBot.create(:doer).banged
puts FactoryBot.build(:acct).n
puts FactoryBot.build(:acct2).n
p FactoryBot.build(:nilname).name
puts FactoryBot.build(:starter).instance_variable_get(:@code)
p FactoryBot.generate(:counter)
`
	want := strings.Join([]string{
		"bar", "true", "acct-ok", "before", "true", "1", "2", "nil", "100", "1",
	}, "\n")
	if got := runSrc(t, src); got != want {
		t.Errorf("factory_bot seams:\n got=%q\nwant=%q", got, want)
	}
}

// TestFactoryBotErrors covers every Ruby-facing error path: define with no
// block, a duplicate factory (ArgumentError), an unknown factory / trait / global
// sequence (KeyError), an unresolvable class (NameError), a non-Integer list
// count (TypeError), and the arity guards on define's DSL and the module methods.
func TestFactoryBotErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "factory_bot"; begin; FactoryBot.define; rescue => e; puts e.class; end`, "LocalJumpError"},
		{`require "factory_bot"; begin; FactoryBot.define { factory :x; factory :x }; rescue => e; puts e.class; end`, "ArgumentError"},
		{`require "factory_bot"; begin; FactoryBot.build(:nope); rescue => e; puts e.class; end`, "KeyError"},
		{`require "factory_bot"; FactoryBot.define { factory(:u, class: "Object") {} }; begin; FactoryBot.build(:u, :ghost); rescue => e; puts e.class; end`, "KeyError"},
		{`require "factory_bot"; begin; FactoryBot.generate(:none); rescue => e; puts e.class; end`, "KeyError"},
		{`require "factory_bot"; FactoryBot.define { factory :ghosty }; begin; FactoryBot.build(:ghosty); rescue => e; puts e.class; end`, "NameError"},
		{`require "factory_bot"; FactoryBot.define { factory(:o, class: "Object") {} }; begin; FactoryBot.build_list(:o, "x"); rescue => e; puts e.class; end`, "TypeError"},
		{`require "factory_bot"; begin; FactoryBot.build; rescue => e; puts e.class; end`, "ArgumentError"},
		{`require "factory_bot"; begin; FactoryBot.build_list(:o); rescue => e; puts e.class; end`, "ArgumentError"},
		{`require "factory_bot"; begin; FactoryBot.build_list(:nope, 2); rescue => e; puts e.class; end`, "KeyError"},
		{`require "factory_bot"; begin; FactoryBot.create_list(:nope, 2); rescue => e; puts e.class; end`, "KeyError"},
		{`require "factory_bot"; begin; FactoryBot.attributes_for(:nope); rescue => e; puts e.class; end`, "KeyError"},
		{`require "factory_bot"; begin; FactoryBot.attributes_for; rescue => e; puts e.class; end`, "ArgumentError"},
		{`require "factory_bot"; begin; FactoryBot.generate; rescue => e; puts e.class; end`, "ArgumentError"},
		{`require "factory_bot"; begin; FactoryBot.define { factory }; rescue => e; puts e.class; end`, "ArgumentError"},
		{`require "factory_bot"; begin; FactoryBot.define { factory(:x) { association } }; rescue => e; puts e.class; end`, "ArgumentError"},
		{`require "factory_bot"; begin; FactoryBot.define { factory(:x) { trait } }; rescue => e; puts e.class; end`, "ArgumentError"},
		{`require "factory_bot"; begin; FactoryBot.define { factory(:x) { factory } }; rescue => e; puts e.class; end`, "ArgumentError"},
		{`require "factory_bot"; begin; FactoryBot.define { factory(:x) { sequence } }; rescue => e; puts e.class; end`, "ArgumentError"},
		{`require "factory_bot"; begin; FactoryBot.define { factory(:x) { after } }; rescue => e; puts e.class; end`, "LocalJumpError"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Errorf("error case %q:\n got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFactoryBotReloadAndAssoc covers reload (a fresh registry drops prior
// factories), a bodyless attribute (method_missing default), an association with
// a factory: target + trait + override, and respond_to? routing through
// respond_to_missing? on both the definition proxy and the evaluator.
func TestFactoryBotReloadAndAssoc(t *testing.T) {
	src := `
require "factory_bot"
class Post; attr_accessor :title, :author, :probe; end
class Person; attr_accessor :name, :role; end
FactoryBot.define do
  factory :person, class: "Person" do
    name "Jo"
    trait :admin do
      role "admin"
    end
  end
  factory :admin_person, parent: :person do
    role "boss"
  end
  factory :post, class: "Post" do
    title
    probe respond_to?(:whatever)
    association :author, factory: :person, :admin, name: "Wole"
  end
end
p1 = FactoryBot.build(:post)
p p1.title
puts p1.probe
puts p1.author.name
puts p1.author.role
ap = FactoryBot.build(:admin_person)
puts ap.name
puts ap.role
FactoryBot.reload
begin; FactoryBot.build(:post); rescue => e; puts e.class; end
`
	want := strings.Join([]string{"nil", "true", "Wole", "admin", "Jo", "boss", "KeyError"}, "\n")
	if got := runSrc(t, src); got != want {
		t.Errorf("factory_bot reload/assoc:\n got=%q\nwant=%q", got, want)
	}
}

// TestFactoryBotHelpers covers the pure conversion/parsing helpers and the
// respond_to_missing? predicates directly, including the branches the Ruby tests
// cannot pin precisely (the Go-nil narrowing, the sentinel-match fallthrough, and
// the empty-argument option lists).
func TestFactoryBotHelpers(t *testing.T) {
	vm := New(&bytes.Buffer{})

	// The wrapper values render to their factory_bot-style tags and are truthy.
	for _, w := range []object.Value{&FactoryBotProxy{}, &FactoryBotTopProxy{}, &FactoryBotEvaluator{}} {
		if !strings.HasPrefix(w.ToS(), "#<FactoryBot") || w.Inspect() != w.ToS() || !w.Truthy() {
			t.Errorf("wrapper stringers = %q / %q / %v", w.ToS(), w.Inspect(), w.Truthy())
		}
	}

	// fbToRuby: a Go nil narrows to Ruby nil; a stored value passes through.
	if fbToRuby(nil) != object.NilV {
		t.Errorf("fbToRuby(nil) not NilV")
	}
	if v := fbToRuby(object.Value(object.Integer(7))); v != object.Integer(7) {
		t.Errorf("fbToRuby passthrough = %v", v)
	}

	// fbStore is a plain widening.
	if fbStore(object.Integer(3)).(object.Value) != object.Integer(3) {
		t.Errorf("fbStore round-trip")
	}

	// fbSortedKeys returns keys in a stable order.
	keys := fbSortedKeys(map[string]any{"b": 1, "a": 1, "c": 1})
	if strings.Join(keys, ",") != "a,b,c" {
		t.Errorf("fbSortedKeys = %v", keys)
	}

	// fbIsAny matches a sentinel and rejects an unrelated error.
	if !fbIsAny(factorybot.ErrUnknownTrait, factorybot.ErrUnknownFactory, factorybot.ErrUnknownTrait) {
		t.Errorf("fbIsAny should match ErrUnknownTrait")
	}
	if fbIsAny(errors.New("x"), factorybot.ErrUnknownFactory) {
		t.Errorf("fbIsAny should not match unrelated error")
	}

	// fbInt coerces an Integer and raises TypeError otherwise.
	if fbInt(object.Integer(9)) != 9 {
		t.Errorf("fbInt(9) != 9")
	}
	if cls := expectRaise(t, func() { fbInt(object.NewString("no")) }); cls != "TypeError" {
		t.Errorf("fbInt(string) raised %q, want TypeError", cls)
	}

	// factoryBotClassName resolves a Class to its name and a String/Symbol to text.
	if n := vm.factoryBotClassName(vm.cString); n != "String" {
		t.Errorf("classname(Class) = %q", n)
	}
	if n := vm.factoryBotClassName(object.NewString("Widget")); n != "Widget" {
		t.Errorf("classname(String) = %q", n)
	}

	// factoryBotSeqArgs: default start of 1, and an explicit start.
	if name, start := factoryBotSeqArgs([]object.Value{object.SymVal("s")}); name != "s" || start != 1 {
		t.Errorf("seqArgs default = %q,%d", name, start)
	}
	if _, start := factoryBotSeqArgs([]object.Value{object.SymVal("s"), object.Integer(50)}); start != 50 {
		t.Errorf("seqArgs start = %d", start)
	}

	// factoryBotCallOpts: no args -> no options; a trait + a trailing Hash -> two.
	if opts := factoryBotCallOpts(nil); len(opts) != 0 {
		t.Errorf("callOpts(nil) = %d", len(opts))
	}
	h := object.NewHash()
	h.Set(object.SymVal("name"), object.NewString("x"))
	if opts := factoryBotCallOpts([]object.Value{object.SymVal("admin"), h}); len(opts) != 2 {
		t.Errorf("callOpts(trait+hash) = %d", len(opts))
	}

	// factoryBotAssocOpts: bare name -> defaults; factory:/traits/overrides parsed.
	if fn, tr, ov := factoryBotAssocOpts(nil); fn != "" || len(tr) != 0 || len(ov) != 0 {
		t.Errorf("assocOpts(nil) = %q,%v,%v", fn, tr, ov)
	}
	ah := object.NewHash()
	ah.Set(object.SymVal("factory"), object.SymVal("user"))
	ah.Set(object.SymVal("name"), object.NewString("y"))
	fn, tr, ov := factoryBotAssocOpts([]object.Value{object.SymVal("admin"), ah})
	if fn != "user" || len(tr) != 1 || tr[0] != "admin" || len(ov) != 1 {
		t.Errorf("assocOpts parsed = %q,%v,%v", fn, tr, ov)
	}

	// respond_to_missing? answers true on the definition proxy and the evaluator.
	if v := vm.send(&FactoryBotProxy{}, "respond_to_missing?", []object.Value{object.SymVal("x")}, nil); !v.Truthy() {
		t.Errorf("proxy respond_to_missing? not truthy")
	}
	if v := vm.send(&FactoryBotEvaluator{}, "respond_to_missing?", []object.Value{object.SymVal("x")}, nil); !v.Truthy() {
		t.Errorf("evaluator respond_to_missing? not truthy")
	}
}

// expectRaise runs fn and returns the class of the Ruby exception it raises (or
// "" if none). A non-Ruby panic is re-raised.
func expectRaise(t *testing.T, fn func()) (cls string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			re, ok := r.(RubyError)
			if !ok {
				panic(r)
			}
			cls = re.Class
		}
	}()
	fn()
	return ""
}
