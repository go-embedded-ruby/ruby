// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"

	dryvalidation "github.com/go-ruby-dry-validation/dry-validation"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestDryValidationConstants covers the module wiring and require idempotence.
func TestDryValidationConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "dry/validation"; p Dry::Schema.is_a?(Module)`, "true\n"},
		{`require "dry/validation"; p Dry::Validation.is_a?(Module)`, "true\n"},
		{`p require "dry/validation"`, "true\n"},
		{`require "dry/validation"; p require "dry/validation"`, "false\n"},
		{`require "dry/schema"; p defined?(Dry::Schema) ? true : false`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDrySchema covers Dry::Schema.Params / .JSON, the required/optional DSL, the
// key macros (filled/maybe/value/array/hash/schema), and the Result surface.
func TestDrySchema(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "dry/validation"
s = Dry::Schema.Params { required(:email).filled(:string) }
p s.call(email: "a@b.c").success?`, "true\n"},
		{`require "dry/validation"
s = Dry::Schema.Params { required(:email).filled(:string) }
p s.call({}).success?`, "false\n"},
		{`require "dry/validation"
s = Dry::Schema.Params { required(:email).filled(:string) }
p s.call({}).failure?`, "true\n"},
		// Errors / to_h / output / messages surfaces.
		{`require "dry/validation"
s = Dry::Schema.Params { required(:email).filled(:string) }
p s.call({}).errors.key?(:email)`, "true\n"},
		{`require "dry/validation"
s = Dry::Schema.Params { required(:age).filled(:integer, gt?: 18) }
p s.call(age: 20).to_h`, "{age: 20}\n"},
		{`require "dry/validation"
s = Dry::Schema.Params { required(:age).filled(:integer) }
p s.call(age: 5).output`, "{age: 5}\n"},
		{`require "dry/validation"
s = Dry::Schema.Params { required(:email).filled(:string) }
p s.call({}).messages.is_a?(Array)`, "true\n"},
		// optional key.
		{`require "dry/validation"
s = Dry::Schema.Params { optional(:nick).maybe(:string) }
p s.call({}).success?`, "true\n"},
		// value macro.
		{`require "dry/validation"
s = Dry::Schema.Params { required(:n).value(:integer) }
p s.call(n: 3).success?`, "true\n"},
		// JSON schema.
		{`require "dry/validation"
s = Dry::Schema.JSON { required(:n).filled(:integer) }
p s.call(n: 4).success?`, "true\n"},
		// array macro with a type.
		{`require "dry/validation"
s = Dry::Schema.Params { required(:tags).array(:string) }
p s.call(tags: ["a", "b"]).success?`, "true\n"},
		// array macro with a nested block.
		{`require "dry/validation"
s = Dry::Schema.Params { required(:xs).array { required(:n).filled(:integer) } }
p s.call(xs: [{n: 1}]).success?`, "true\n"},
		// hash macro with a nested block.
		{`require "dry/validation"
s = Dry::Schema.Params { required(:addr).hash { required(:city).filled(:string) } }
p s.call(addr: {city: "Paris"}).success?`, "true\n"},
		// schema macro with a nested block.
		{`require "dry/validation"
s = Dry::Schema.Params { required(:addr).schema { required(:city).filled(:string) } }
p s.call(addr: {city: "Lyon"}).success?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDryContract covers Dry::Validation::Contract: schema + rule, the RuleContext
// value/values reads, key/base/no-arg failures, and #call.
func TestDryContract(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "dry/validation"
class C < Dry::Validation::Contract
  schema { required(:age).filled(:integer) }
  rule(:age) { key.failure("too young") if value(:age) < 18 }
end
p C.new.call(age: 20).success?`, "true\n"},
		{`require "dry/validation"
class C2 < Dry::Validation::Contract
  schema { required(:age).filled(:integer) }
  rule(:age) { key.failure("too young") if value(:age) < 18 }
end
p C2.new.call(age: 5).errors`, "{age: [\"too young\"]}\n"},
		// key(:other).failure records under an explicit key.
		{`require "dry/validation"
class C3 < Dry::Validation::Contract
  schema { required(:a).filled(:integer) }
  rule(:a) { key(:b).failure("bad b") }
end
p C3.new.call(a: 1).errors`, "{b: [\"bad b\"]}\n"},
		// base.failure records a base error (nil key).
		{`require "dry/validation"
class C4 < Dry::Validation::Contract
  schema { required(:a).filled(:integer) }
  rule(:a) { base.failure("base bad") }
end
p C4.new.call(a: 1).errors[nil]`, "[\"base bad\"]\n"},
		// no-arg failure targets the rule's default key.
		{`require "dry/validation"
class C5 < Dry::Validation::Contract
  schema { required(:a).filled(:integer) }
  rule(:a) { failure("plain") }
end
p C5.new.call(a: 1).errors`, "{a: [\"plain\"]}\n"},
		// values reads the whole coerced input.
		{`require "dry/validation"
class C6 < Dry::Validation::Contract
  schema { required(:a).filled(:integer) }
  rule(:a) { key.failure("v") unless values[:a] == 1 }
end
p C6.new.call(a: 1).success?`, "true\n"},
		// value on a present maybe(nil) key is nil.
		{`require "dry/validation"
class C7 < Dry::Validation::Contract
  schema { required(:a).maybe(:integer) }
  rule(:a) { key.failure("nil") if value(:a).nil? }
end
p C7.new.call(a: nil).errors`, "{a: [\"nil\"]}\n"},
		// params is an alias for schema.
		{`require "dry/validation"
class C8 < Dry::Validation::Contract
  params { required(:a).filled(:integer) }
end
p C8.new.call(a: 1).success?`, "true\n"},
		// A contract with no schema declared validates an empty schema.
		{`require "dry/validation"
class C9 < Dry::Validation::Contract
end
p C9.new.call({}).success?`, "true\n"},
		// A base rule (rule with no keys) runs against the whole input.
		{`require "dry/validation"
class C10 < Dry::Validation::Contract
  schema { required(:a).filled(:integer) }
  rule { base.failure("always") }
end
p C10.new.call(a: 1).success?`, "false\n"},
		// key(nil) targets the default key path.
		{`require "dry/validation"
class C11 < Dry::Validation::Contract
  schema { required(:a).filled(:integer) }
  rule(:a) { key(nil).failure("dk") }
end
p C11.new.call(a: 1).errors`, "{a: [\"dk\"]}\n"},
		// value on a key absent from the coerced values is nil.
		{`require "dry/validation"
class C12 < Dry::Validation::Contract
  schema { required(:a).filled(:integer) }
  rule(:a) { key.failure("no b") if value(:b).nil? }
end
p C12.new.call(a: 1).errors`, "{a: [\"no b\"]}\n"},
		// A String type name (filled("string")) resolves the same as the Symbol.
		{`require "dry/validation"
s = Dry::Schema.Params { required(:e).filled("string") }
p s.call(e: "x").success?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDryValidationArgErrors covers the arity / no-block guards.
func TestDryValidationArgErrors(t *testing.T) {
	raises := []struct{ src, class string }{
		{`require "dry/validation"; Dry::Schema.Params { required }`, "ArgumentError"},
		{`require "dry/validation"; Dry::Schema.Params { required(:a).hash }`, "ArgumentError"},
		{`require "dry/validation"; Dry::Schema.Params { required(:a).schema }`, "ArgumentError"},
		{`require "dry/validation"
s = Dry::Schema.Params { required(:a).filled(:integer) }
s.call`, "ArgumentError"},
		{`require "dry/validation"
class Z < Dry::Validation::Contract
  schema { required(:a).filled(:integer) }
end
Z.new.call`, "ArgumentError"},
		{`require "dry/validation"
class ZZ < Dry::Validation::Contract; rule(:a); end`, "ArgumentError"},
		{`require "dry/validation"
class R < Dry::Validation::Contract
  schema { required(:a).filled(:integer) }
  rule(:a) { value }
end
R.new.call(a: 1)`, "ArgumentError"},
	}
	for _, c := range raises {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestDryValidationValueObjects covers the wrapper display methods and the
// empty-block schema build path.
func TestDryValidationValueObjects(t *testing.T) {
	s := &DrySchema{s: dryvalidation.Params(func(*dryvalidation.Builder) {})}
	if s.ToS() == "" || s.Inspect() == "" || !s.Truthy() {
		t.Error("DrySchema display")
	}
	b := &DrySchemaBuilder{}
	if b.ToS() == "" || b.Inspect() == "" || !b.Truthy() {
		t.Error("DrySchemaBuilder display")
	}
	k := &DryKey{}
	if k.ToS() == "" || k.Inspect() == "" || !k.Truthy() {
		t.Error("DryKey display")
	}
	c := &DryContract{}
	if c.ToS() == "" || c.Inspect() == "" || !c.Truthy() {
		t.Error("DryContract display")
	}
	r := &DryValidationResult{}
	if r.ToS() == "" || r.Inspect() == "" || !r.Truthy() {
		t.Error("DryValidationResult display")
	}
	rc := &DryRuleCtx{}
	if rc.ToS() == "" || rc.Inspect() == "" || !rc.Truthy() {
		t.Error("DryRuleCtx display")
	}
	rk := &DryRuleKey{}
	if rk.ToS() == "" || rk.Inspect() == "" || !rk.Truthy() {
		t.Error("DryRuleKey display")
	}
	m := &dryContractMeta{}
	if m.ToS() == "" || m.Inspect() == "" || !m.Truthy() {
		t.Error("dryContractMeta display")
	}
	// drySchemaBuild with a nil block yields an empty schema builder.
	vm := New(io.Discard)
	build := vm.drySchemaBuild(nil)
	dryvalidation.Params(build) // must not panic

	// dryContractMeta initialises a nil ivar table on first use.
	cls := newClass("Anon", vm.cObject)
	cls.ivars = nil
	cm := vm.dryContractMeta(cls)
	if cm == nil || cls.ivars == nil {
		t.Error("dryContractMeta nil-ivars init")
	}
	// A second call returns the cached meta.
	if vm.dryContractMeta(cls) != cm {
		t.Error("dryContractMeta not cached")
	}
}

// TestDryFailureText covers the dryFailureText fallback for a non-String argument
// and the no-argument case.
func TestDryFailureText(t *testing.T) {
	if dryFailureText(nil) != "" {
		t.Error("no-arg failure text")
	}
	if dryFailureText([]object.Value{object.IntValue(int64(object.Integer(5)))}) != "5" {
		t.Error("non-string failure text")
	}
}

// TestDryPredName covers the trailing-"?" strip and the no-"?" pass-through.
func TestDryPredName(t *testing.T) {
	if dryPredName("gt?") != "gt" {
		t.Error("strip ?")
	}
	if dryPredName("gt") != "gt" {
		t.Error("no ?")
	}
	if dryPredName("") != "" {
		t.Error("empty")
	}
}
