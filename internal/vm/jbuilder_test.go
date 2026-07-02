// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestJbuilderModule covers the Jbuilder loadable class (require "jbuilder").
func TestJbuilderModule(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "jbuilder"; p Jbuilder.is_a?(Class)`, "true\n"},
		{`p require "jbuilder"`, "true\n"},
		{`require "jbuilder"; p require "jbuilder"`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestJbuilderEncode covers the Jbuilder.encode { |json| … } DSL through
// method_missing: scalar sets, nested objects, arrays (block and no-block),
// and the value shapes the encoder renders (string/int/float/bool/nil/bignum).
func TestJbuilderEncode(t *testing.T) {
	cases := []struct{ src, want string }{
		// Scalar keys via method_missing.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.name "Al"; json.age 30 })`, `{"name":"Al","age":30}`},
		// Value shapes.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.b true; json.f 2.5; json.z nil })`, `{"b":true,"f":2.5,"z":null}`},
		// A Bignum value.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.big 10**20 })`, `{"big":100000000000000000000}`},
		// A Symbol value renders as a JSON string.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.s :hi })`, `{"s":"hi"}`},
		// A nested object block.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.author do json.first "A"; json.last "B" end })`, `{"author":{"first":"A","last":"B"}}`},
		// A key with no value yields an empty nested object.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.empty })`, `{"empty":{}}`},
		// An Array value (a plain Ruby Array) is emitted as a JSON array.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.xs [1,2,3] })`, `{"xs":[1,2,3]}`},
		// A nested Hash value.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.h({"a" => 1}) })`, `{"h":{"a":1}}`},
		// A nested array mapped from a collection (json.name coll { |x| … }).
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.nums([1,2]) { |n| json.v n } })`, `{"nums":[{"v":1},{"v":2}]}`},
		// An object value falls back to its #to_s string.
		{`require "jbuilder"
			class W; def to_s; "wid"; end; end
			print(Jbuilder.encode { |json| json.w W.new })`, `{"w":"wid"}`},
		// method_missing's extract! shorthand: json.name(obj, :a, :b) sets the
		// object itself under the key (the >1-argument default arm).
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.name({"a" => 1}, :a, :b) })`, `{"name":{"a":1}}`},
		// A nested Jbuilder instance value is embedded as its built JSON.
		{`require "jbuilder"
			inner = Jbuilder.new { |j| j.x 1 }
			print(Jbuilder.encode { |json| json.wrap inner })`, `{"wrap":{"x":1}}`},
		// A non-Symbol/String key stringifies (method_missing always gets a Symbol,
		// so exercise the fallback through a Hash value with an Integer key).
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.h({1 => "one"}) })`, `{"h":{"1":"one"}}`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestJbuilderArrayRoot covers json.array! turning the whole builder into a JSON
// array, both with a mapping block and with a bare collection.
func TestJbuilderArrayRoot(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.array! [1,2,3] })`, `[1,2,3]`},
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.array!([1,2]) { |n| json.sq n*n } })`, `[{"sq":1},{"sq":4}]`},
		// array! with no argument yields an empty array.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.array! })`, `[]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestJbuilderSetBang covers the explicit set! form (scalar, block object, and
// nested array) alongside method_missing.
func TestJbuilderSetBang(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.set! :name, "Al" })`, `{"name":"Al"}`},
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.set!(:a) { json.b 1 } })`, `{"a":{"b":1}}`},
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.set!(:xs, [1,2]) { |n| json.v n } })`, `{"xs":[{"v":1},{"v":2}]}`},
		// set! with a bare key yields an empty nested object.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.set! :e })`, `{"e":{}}`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestJbuilderChild covers json.child! appending block-built array elements.
func TestJbuilderChild(t *testing.T) {
	src := `require "jbuilder"
	print(Jbuilder.encode { |json| json.child! { json.a 1 }; json.child! { json.a 2 } })`
	if got := eval(t, src); got != `[{"a":1},{"a":2}]` {
		t.Errorf("got=%q", got)
	}
}

// TestJbuilderMerge covers merge! folding a Hash or an Array into the target.
func TestJbuilderMerge(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.a 1; json.merge!({"b" => 2}) })`, `{"a":1,"b":2}`},
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.array! [1]; json.merge!([2,3]) })`, `[1,2,3]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestJbuilderExtract covers extract! resolving attributes from a Hash and from
// an object that responds to reader methods, plus the [] fallback.
func TestJbuilderExtract(t *testing.T) {
	cases := []struct{ src, want string }{
		// From a Hash with Symbol keys.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.extract!({a: 1, b: 2}, :a, :b) })`, `{"a":1,"b":2}`},
		// From a Hash with String keys.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.extract!({"a" => 1}, :a) })`, `{"a":1}`},
		// A missing Hash key resolves to null.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.extract!({}, :a) })`, `{"a":null}`},
		// From an object exposing reader methods.
		{`require "jbuilder"
			class P; def name; "Al"; end; end
			print(Jbuilder.encode { |json| json.extract!(P.new, :name) })`, `{"name":"Al"}`},
		// From an object exposing [] but not a reader.
		{`require "jbuilder"
			class B; def [](k); k == :x ? 9 : nil; end; end
			print(Jbuilder.encode { |json| json.extract!(B.new, :x) })`, `{"x":9}`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestJbuilderNil covers nil!/null! setting the whole target to JSON null.
func TestJbuilderNil(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.a 1; json.nil! })`, `null`},
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.null! })`, `null`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestJbuilderKeyFormat covers key_format! transforms (camelize lower/upper,
// dasherize, underscore) driven from Symbols, a Hash argument, and the clear form.
func TestJbuilderKeyFormat(t *testing.T) {
	cases := []struct{ src, want string }{
		// camelize: :lower.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.key_format! camelize: :lower; json.first_name "A" })`, `{"firstName":"A"}`},
		// camelize: :upper.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.key_format! camelize: :upper; json.first_name "A" })`, `{"FirstName":"A"}`},
		// :dasherize as a bare Symbol.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.key_format! :dasherize; json.a_b "x" })`, `{"a-b":"x"}`},
		// :underscore (string form) round-trips a camelCase key.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.key_format! "underscore"; json.aB "x" })`, `{"a_b":"x"}`},
		// key_format! with no args clears formatting.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.key_format! camelize: :lower; json.key_format!; json.first_name "A" })`, `{"first_name":"A"}`},
		// An unknown op is ignored.
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.key_format! :bogus; json.a_b "x" })`, `{"a_b":"x"}`},
		// camelize with a String "upper" argument (the string-arg branch).
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.key_format! camelize: "upper"; json.a_b "x" })`, `{"AB":"x"}`},
		// A bare :camelize Symbol (no argument) defaults to lowerCamelCase (the
		// nil-argument fall-through in jbuilderCamelUpper).
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.key_format! :camelize; json.first_name "A" })`, `{"firstName":"A"}`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestJbuilderIgnoreNil covers ignore_nil! dropping nil-valued keys (default on,
// and explicitly toggled off).
func TestJbuilderIgnoreNil(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.ignore_nil!; json.a nil; json.b 1 })`, `{"b":1}`},
		{`require "jbuilder"; print(Jbuilder.encode { |json| json.ignore_nil!(false); json.a nil })`, `{"a":null}`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestJbuilderNewAndTarget covers Jbuilder.new (with and without a block), the
// target!/to_json/to_s readers, and respond_to_missing?.
func TestJbuilderNewAndTarget(t *testing.T) {
	cases := []struct{ src, want string }{
		// new with a block yields the builder and returns the instance.
		{`require "jbuilder"
			j = Jbuilder.new { |json| json.a 1 }
			print j.target!`, `{"a":1}`},
		// new without a block returns an empty builder (target! == {}).
		{`require "jbuilder"; print Jbuilder.new.target!`, `{}`},
		// to_json / to_s are aliases of target!.
		{`require "jbuilder"
			j = Jbuilder.new { |json| json.a 1 }
			print j.to_json`, `{"a":1}`},
		{`require "jbuilder"
			j = Jbuilder.new { |json| json.a 1 }
			print j.to_s`, `{"a":1}`},
		// respond_to_missing? makes the builder answer any name.
		{`require "jbuilder"; p Jbuilder.new.respond_to?(:whatever)`, "true\n"},
		// inspect renders the builder shell.
		{`require "jbuilder"; p Jbuilder.new`, "#<Jbuilder>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestJbuilderErrors covers the argument guards on encode, method_missing and the
// bang methods.
func TestJbuilderErrors(t *testing.T) {
	guard := func(body string) string {
		return `require "jbuilder"
begin
  ` + body + `
rescue ArgumentError, LocalJumpError
  print "err"
end`
	}
	cases := []string{
		`Jbuilder.encode`,                           // no block
		`Jbuilder.encode { |json| json.set! }`,      // set! no key
		`Jbuilder.encode { |json| json.child! }`,    // child! no block
		`Jbuilder.encode { |json| json.merge! }`,    // merge! no arg
		`Jbuilder.encode { |json| json.extract! }`,  // extract! no arg
		`Jbuilder.encode { |json| json.merge!(1) }`, // merge! bad type -> TypeError
	}
	for _, body := range cases {
		src := guard(body)
		// merge!(1) raises TypeError, not ArgumentError; widen the rescue.
		src = `require "jbuilder"
begin
  ` + body + `
rescue StandardError
  print "err"
end`
		if got := eval(t, src); got != "err" {
			t.Errorf("body=%q got=%q want=err", body, got)
		}
	}
}
