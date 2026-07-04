// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// pbPrelude builds a fresh, self-contained DescriptorPool with an enum, a
// scalar-rich message (every field-value shape the binding bridges), a repeated
// field, a map field and a self-referential sub-message. Each eval runs on a new
// VM, so every case that needs the schema prepends this. A fresh pool per VM keeps
// the tests isolated from the process-wide generated pool.
const pbPrelude = `require "google/protobuf"
pool = Google::Protobuf::DescriptorPool.new
pool.build do
  add_enum "Color" do
    value :RED, 0
    value :GREEN, 1
    value :BLUE, 2
  end
  add_message "Person" do
    optional :name, :string, 1
    optional :id, :int32, 2
    optional :active, :bool, 3
    optional :score, :double, 4
    optional :data, :bytes, 5
    optional :big, :uint64, 6
    optional :color, :enum, 7, "Color"
    repeated :emails, :string, 8
    map :attrs, :string, :int32, 9
    optional :spouse, :message, 10, "Person"
  end
end
Person = pool.lookup("Person").msgclass
Color = pool.lookup("Color")
`

// eachEval runs each (src, want) case with the protobuf schema prelude prepended.
func eachEval(t *testing.T, cases []struct{ src, want string }) {
	t.Helper()
	for _, c := range cases {
		if got := eval(t, pbPrelude+c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestProtobufModule covers the module identity, require idempotency and the
// exception tree (require "google/protobuf").
func TestProtobufModule(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "google/protobuf"; p Google::Protobuf.is_a?(Module)`, "true\n"},
		{`p require "google/protobuf"`, "true\n"},
		{`require "google/protobuf"; p require "google/protobuf"`, "false\n"},
		// The gem's alternate feature name is accepted too.
		{`p require "protobuf"`, "true\n"},
		// Error tree: TypeError subclasses the core TypeError; ParseError a RuntimeError.
		{`require "google/protobuf"; p Google::Protobuf::TypeError < TypeError`, "true\n"},
		{`require "google/protobuf"; p Google::Protobuf::ParseError < RuntimeError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestProtobufDescriptors covers pool.lookup and the descriptor introspection
// surface: Descriptor#name/#msgclass/#lookup/#each, FieldDescriptor
// name/type/label/number, EnumDescriptor name/lookup_name/lookup_value, and the
// message class's name/descriptor.
func TestProtobufDescriptors(t *testing.T) {
	eachEval(t, []struct{ src, want string }{
		{`p Person.name`, "\"Person\"\n"},
		{`p Person.descriptor.name`, "\"Person\"\n"},
		{`p Person.descriptor.class.name`, "\"Google::Protobuf::Descriptor\"\n"},
		{`p pool.lookup("Person").lookup("name").name`, "\"name\"\n"},
		{`p pool.lookup("Person").lookup("name").type`, ":string\n"},
		{`p pool.lookup("Person").lookup("id").type`, ":int32\n"},
		{`p pool.lookup("Person").lookup("name").number`, "1\n"},
		{`p pool.lookup("Person").lookup("emails").label`, ":repeated\n"},
		{`p pool.lookup("Person").lookup("name").label`, ":optional\n"},
		{`p pool.lookup("Person").lookup("nope")`, "nil\n"},
		// Descriptor#each yields every field.
		{`names = []; pool.lookup("Person").each { |f| names << f.name }; p names.first(3)`,
			"[\"name\", \"id\", \"active\"]\n"},
		// EnumDescriptor.
		{`p Color.name`, "\"Color\"\n"},
		{`p Color.class.name`, "\"Google::Protobuf::EnumDescriptor\"\n"},
		{`p Color.lookup_name("GREEN")`, "1\n"},
		{`p Color.lookup_name("MAUVE")`, "nil\n"},
		{`p Color.lookup_value(2)`, ":BLUE\n"},
		{`p Color.lookup_value(9)`, "nil\n"},
		// A name that is neither message nor enum is nil.
		{`p pool.lookup("Nonexistent")`, "nil\n"},
		// Descriptor / field / enum inspect + truthy.
		{`p pool.lookup("Person").inspect.include?("Descriptor")`, "true\n"},
		{`p !!pool.lookup("Person")`, "true\n"},
		{`p !!Color`, "true\n"},
		{`p !!pool.lookup("Person").lookup("name")`, "true\n"},
		{`p pool.lookup("Person").lookup("name").inspect.include?("name")`, "true\n"},
		{`p Color.to_s.include?("EnumDescriptor")`, "true\n"},
		// message-class and enum-descriptor #inspect.
		{`p Person.inspect`, "\"Person\"\n"},
		{`p Color.inspect.include?("EnumDescriptor")`, "true\n"},
	})
}

// TestProtobufMoreErrors covers the remaining raise/bridge paths: a bad
// constructor value, container element/value type checks, encode of an
// invalid-UTF-8 string, a corrupt Any unpack, a non-Hash constructor argument and
// a non-name lookup argument.
func TestProtobufMoreErrors(t *testing.T) {
	eachEval(t, []struct{ src, want string }{
		// MyMsg.new with a wrong-typed field value raises through New.
		{`begin; Person.new(id: "x"); rescue Google::Protobuf::TypeError => e; p e.class; end`,
			"Google::Protobuf::TypeError\n"},
		// a wrong-typed element pushed via << to a repeated field.
		{`begin; Person.new.emails << 5; rescue Google::Protobuf::TypeError => e; p e.class; end`,
			"Google::Protobuf::TypeError\n"},
		// a wrong-typed value assigned to a map entry.
		{`begin; Person.new.attrs["k"] = "not an int"; rescue Google::Protobuf::TypeError => e; p e.class; end`,
			"Google::Protobuf::TypeError\n"},
		// encoding an invalid-UTF-8 proto3 string field fails (ArgumentError).
		{`m = Person.new; m.name = "\xff"
begin; Google::Protobuf.encode(m); rescue ArgumentError => e; p e.class; end`, "ArgumentError\n"},
		// Any.pack of such a message surfaces the same encode failure.
		{`m = Person.new; m.name = "\xff"
begin; Google::Protobuf::Any.pack(m); rescue ArgumentError => e; p e.class; end`, "ArgumentError\n"},
		// unpacking a corrupt Any (matching type_url, garbage value) is a ParseError.
		{`any = Google::Protobuf::Any.new(type_url: "type.googleapis.com/Person", value: "\xff\xff".b)
begin; any.unpack(Person); rescue Google::Protobuf::ParseError => e; p e.class; end`,
			"Google::Protobuf::ParseError\n"},
		// a non-Hash constructor argument is ignored (an empty message).
		{`p Person.new(42).name`, "\"\"\n"},
		// a non-name argument to lookup is a TypeError.
		{`begin; pool.lookup(5); rescue TypeError => e; p e.class; end`, "TypeError\n"},
		// encode_json of an invalid-UTF-8 proto3 string field fails (ArgumentError).
		{`m = Person.new; m.name = "\xff"
begin; Google::Protobuf.encode_json(m); rescue ArgumentError => e; p e.class; end`, "ArgumentError\n"},
	})
}

// TestProtobufFields covers per-field set/get across every value shape the
// binding bridges, plus a binary encode -> decode round-trip that recovers them.
func TestProtobufFields(t *testing.T) {
	eachEval(t, []struct{ src, want string }{
		{`m = Person.new(name: "Ada", id: 5); p [m.name, m.id]`, "[\"Ada\", 5]\n"},
		{`m = Person.new; m.name = "Bo"; p m.name`, "\"Bo\"\n"},
		{`m = Person.new; m.active = true; p m.active`, "true\n"},
		{`m = Person.new; m.score = 2.5; p m.score`, "2.5\n"},
		{`m = Person.new; m.color = :BLUE; p m.color`, ":BLUE\n"},
		// bytes field: a binary String round-trips as a binary String.
		{`m = Person.new; m.data = "\xff\x00".b; p m.data == "\xff\x00".b`, "true\n"},
		{`m = Person.new; m.data = "\xff\x00".b; p m.data.encoding.name`, "\"ASCII-8BIT\"\n"},
		// uint64 beyond int64 round-trips through the uint64 bridge as a Bignum.
		{`m = Person.new; n = 2 ** 63 + 1; m.big = n; p m.big == n`, "true\n"},
		// an int64-range Bignum assigned to a uint64 field.
		{`m = Person.new; m.big = 2 ** 40; p m.big`, "1099511627776\n"},
		// unset scalar reads its proto3 default; an unset sub-message is nil.
		{`p [Person.new.id, Person.new.name, Person.new.spouse]`, "[0, \"\", nil]\n"},
		// a sub-message field.
		{`m = Person.new; m.spouse = Person.new(name: "Zed"); p m.spouse.name`, "\"Zed\"\n"},
		// clearing a message field with nil.
		{`m = Person.new; m.spouse = Person.new; m.spouse = nil; p m.spouse`, "nil\n"},
		// full binary round-trip.
		{`m = Person.new(name: "Ada", id: 7, active: true, score: 1.5, color: :GREEN)
bytes = Google::Protobuf.encode(m)
p bytes.encoding.name`, "\"ASCII-8BIT\"\n"},
		{`m = Person.new(name: "Ada", id: 7, active: true, score: 1.5, color: :GREEN)
back = Google::Protobuf.decode(Person, Google::Protobuf.encode(m))
p [back.name, back.id, back.active, back.score, back.color]`,
			"[\"Ada\", 7, true, 1.5, :GREEN]\n"},
		// encode/decode preserve message equality.
		{`m = Person.new(name: "Ada", id: 7)
p Google::Protobuf.decode(Person, Google::Protobuf.encode(m)) == m`, "true\n"},
	})
}

// TestProtobufMessageProtocol covers to_h, ==, dup/clone, inspect/to_s,
// respond_to?, and method_missing errors.
func TestProtobufMessageProtocol(t *testing.T) {
	eachEval(t, []struct{ src, want string }{
		{`m = Person.new(name: "Ada", id: 3); h = m.to_h; p [h[:name], h[:id]]`, "[\"Ada\", 3]\n"},
		// nested sub-message renders as a nested hash; repeated as an array; map as a hash.
		{`m = Person.new(name: "A"); m.emails.push("x"); m.attrs["k"] = 1
h = m.to_h; p [h[:emails], h[:attrs]]`, "[[\"x\"], {\"k\" => 1}]\n"},
		{`m = Person.new(name: "A"); m.spouse = Person.new(name: "B"); p m.to_h[:spouse][:name]`, "\"B\"\n"},
		{`p Person.new(id: 1) == Person.new(id: 1)`, "true\n"},
		{`p Person.new(id: 1) == Person.new(id: 2)`, "false\n"},
		{`p Person.new(id: 1) == "not a message"`, "false\n"},
		{`m = Person.new(id: 1); d = m.dup; d.id = 9; p [m.id, d.id]`, "[1, 9]\n"},
		{`m = Person.new(id: 1); c = m.clone; p c.id`, "1\n"},
		{`p Person.new(name: "Ada").inspect.include?("Person")`, "true\n"},
		{`p Person.new(name: "Ada").to_s.include?("name")`, "true\n"},
		{`p Person.new.respond_to?(:name)`, "true\n"},
		{`p Person.new.respond_to?(:name=)`, "true\n"},
		{`p Person.new.respond_to?(:not_a_field)`, "false\n"},
		{`p !!Person.new`, "true\n"},
		{`p Person.to_s`, "\"Person\"\n"},
		// method_missing: an unknown reader/writer raises NoMethodError.
		{`begin; Person.new.bogus; rescue NoMethodError => e; p e.class; end`, "NoMethodError\n"},
		{`begin; Person.new.bogus = 1; rescue NoMethodError => e; p e.class; end`, "NoMethodError\n"},
	})
}

// TestProtobufRepeated covers the RepeatedField surface, standalone and as a
// message field.
func TestProtobufRepeated(t *testing.T) {
	eachEval(t, []struct{ src, want string }{
		{`m = Person.new; m.emails.push("a", "b"); m.emails << "c"; p m.emails.to_a`,
			"[\"a\", \"b\", \"c\"]\n"},
		{`m = Person.new; m.emails.push("a", "b"); p m.emails[1]`, "\"b\"\n"},
		{`m = Person.new; m.emails.push("a"); m.emails[0] = "z"; p m.emails[0]`, "\"z\"\n"},
		{`m = Person.new; m.emails.push("a", "b"); out = []; m.emails.each { |e| out << e }; p out`,
			"[\"a\", \"b\"]\n"},
		{`m = Person.new; m.emails.push("a", "b"); p [m.emails.length, m.emails.size]`, "[2, 2]\n"},
		{`m = Person.new; p m.emails.empty?`, "true\n"},
		{`m = Person.new; m.emails.push("a", "b"); p [m.emails.first, m.emails.last]`,
			"[\"a\", \"b\"]\n"},
		{`m = Person.new; m.emails.push("a", "b"); p m.emails.include?("b")`, "true\n"},
		{`m = Person.new; m.emails.push("a"); p m.emails.include?("zzz")`, "false\n"},
		{`m = Person.new; m.emails.push("a"); m.emails.clear; p m.emails.to_a`, "[]\n"},
		{`m = Person.new; m.emails.push("a"); d = m.emails.dup; d.push("b"); p [m.emails.to_a, d.to_a]`,
			"[[\"a\"], [\"a\", \"b\"]]\n"},
		{`m = Person.new; m.emails.push("a"); m.emails.concat(["b", "c"]); p m.emails.to_a`,
			"[\"a\", \"b\", \"c\"]\n"},
		{`m = Person.new; m.emails.push("a"); r = m.emails + ["b"]; p [r.to_a, m.emails.to_a]`,
			"[[\"a\", \"b\"], [\"a\"]]\n"},
		{`m = Person.new; m.emails.push("a"); puts m.emails.inspect`, "[\"a\"]\n"},
		{`m = Person.new; m.emails.push("a"); puts m.emails.to_s`, "[\"a\"]\n"},
		{`p !!Person.new.emails`, "true\n"},
		// == between repeated fields.
		{`a = Person.new; a.emails.push("x"); b = Person.new; b.emails.push("x")
p a.emails == b.emails`, "true\n"},
		{`a = Person.new; a.emails.push("x"); p a.emails == "notrf"`, "false\n"},
		// assigning an Array replaces the field; assigning a RepeatedField too.
		{`m = Person.new; m.emails = ["p", "q"]; p m.emails.to_a`, "[\"p\", \"q\"]\n"},
		{`s = Person.new; s.emails.push("z"); m = Person.new; m.emails = s.emails; p m.emails.to_a`,
			"[\"z\"]\n"},
		// standalone RepeatedField.new (with and without an initial array).
		{`r = Google::Protobuf::RepeatedField.new(:int32, [1, 2]); r.push(3); p r.to_a`, "[1, 2, 3]\n"},
		{`r = Google::Protobuf::RepeatedField.new(:int32); p r.to_a`, "[]\n"},
		{`r = Google::Protobuf::RepeatedField.new(:int32, 5); p r.to_a`, "[]\n"},
	})
}

// TestProtobufMap covers the Map surface, standalone and as a message field.
func TestProtobufMap(t *testing.T) {
	eachEval(t, []struct{ src, want string }{
		{`m = Person.new; m.attrs["a"] = 1; m.attrs["b"] = 2; p m.attrs["a"]`, "1\n"},
		{`m = Person.new; p m.attrs["missing"]`, "nil\n"},
		{`m = Person.new; m.attrs["a"] = 1; m.attrs["b"] = 2; p [m.attrs.length, m.attrs.size]`, "[2, 2]\n"},
		{`m = Person.new; m.attrs["a"] = 1; m.attrs["b"] = 2; p m.attrs.keys`, "[\"a\", \"b\"]\n"},
		{`m = Person.new; m.attrs["a"] = 1; m.attrs["b"] = 2; p m.attrs.values`, "[1, 2]\n"},
		{`m = Person.new; m.attrs["a"] = 1; p m.attrs.to_h`, "{\"a\" => 1}\n"},
		{`m = Person.new; m.attrs["a"] = 1; out = []; m.attrs.each { |k, v| out << [k, v] }; p out`,
			"[[\"a\", 1]]\n"},
		{`m = Person.new; m.attrs["a"] = 1; p [m.attrs.has_key?("a"), m.attrs.key?("a"), m.attrs.include?("a")]`,
			"[true, true, true]\n"},
		{`m = Person.new; p m.attrs.has_key?("a")`, "false\n"},
		{`m = Person.new; m.attrs["a"] = 1; p m.attrs.delete("a")`, "true\n"},
		{`m = Person.new; p m.attrs.delete("a")`, "false\n"},
		{`m = Person.new; m.attrs["a"] = 1; m.attrs.clear; p m.attrs.length`, "0\n"},
		{`m = Person.new; p m.attrs.empty?`, "true\n"},
		{`m = Person.new; m.attrs["a"] = 1; d = m.attrs.dup; d["b"] = 2; p [m.attrs.length, d.length]`,
			"[1, 2]\n"},
		{`m = Person.new; m.attrs["a"] = 1; puts m.attrs.inspect`, "{\"a\"=>1}\n"},
		{`m = Person.new; m.attrs["a"] = 1; puts m.attrs.to_s`, "{\"a\"=>1}\n"},
		{`p !!Person.new.attrs`, "true\n"},
		{`a = Person.new; a.attrs["x"] = 1; b = Person.new; b.attrs["x"] = 1; p a.attrs == b.attrs`, "true\n"},
		{`a = Person.new; a.attrs["x"] = 1; p a.attrs == "notmap"`, "false\n"},
		// assigning a Hash replaces the map; assigning a Map too.
		{`m = Person.new; m.attrs = {"p" => 9}; p m.attrs["p"]`, "9\n"},
		{`s = Person.new; s.attrs["z"] = 3; m = Person.new; m.attrs = s.attrs; p m.attrs["z"]`, "3\n"},
		// standalone Map.new.
		{`mp = Google::Protobuf::Map.new(:string, :int32); mp["k"] = 7; p mp["k"]`, "7\n"},
	})
}

// TestProtobufJSON covers encode_json / decode_json and their keyword options.
func TestProtobufJSON(t *testing.T) {
	eachEval(t, []struct{ src, want string }{
		{`m = Person.new(name: "Ada", id: 4)
back = Google::Protobuf.decode_json(Person, Google::Protobuf.encode_json(m))
p [back.name, back.id]`, "[\"Ada\", 4]\n"},
		// emit_defaults includes zero-valued fields; without it they are omitted.
		{`m = Person.new(name: "Ada")
p Google::Protobuf.encode_json(m).include?("id")`, "false\n"},
		{`m = Person.new(name: "Ada")
p Google::Protobuf.encode_json(m, emit_defaults: true).include?("id")`, "true\n"},
		// preserve_proto_fieldnames keeps snake_case names (all fields here are already
		// single-word, so this exercises the option path).
		{`m = Person.new(name: "Ada")
p Google::Protobuf.encode_json(m, preserve_proto_fieldnames: true).include?("name")`, "true\n"},
		// ignore_unknown_fields skips a field the schema does not know.
		{`back = Google::Protobuf.decode_json(Person, "{\"ghost\":1}", ignore_unknown_fields: true)
p back.name`, "\"\"\n"},
		// a non-Hash trailing argument is ignored (zero options).
		{`m = Person.new(name: "Ada")
p Google::Protobuf.encode_json(m, "extra").include?("Ada")`, "true\n"},
	})
}

// TestProtobufWellKnown covers a well-known-type round-trip and the Any helpers.
func TestProtobufWellKnown(t *testing.T) {
	eachEval(t, []struct{ src, want string }{
		{`ts = Google::Protobuf::Timestamp.new(seconds: 5, nanos: 7)
back = Google::Protobuf.decode(Google::Protobuf::Timestamp, Google::Protobuf.encode(ts))
p [back.seconds, back.nanos]`, "[5, 7]\n"},
		{`d = Google::Protobuf::Duration.new(seconds: 3); p d.seconds`, "3\n"},
		{`p !!Google::Protobuf::Struct`, "true\n"},
		// Any pack / is? / unpack round-trip.
		{`m = Person.new(id: 9)
any = Google::Protobuf::Any.pack(m)
p any.is?(Person)`, "true\n"},
		{`m = Person.new(id: 9)
any = Google::Protobuf::Any.pack(m)
p any.unpack(Person).id`, "9\n"},
		// is?/unpack against a non-matching class.
		{`m = Person.new(id: 9)
any = Google::Protobuf::Any.pack(m)
p any.is?(Google::Protobuf::Timestamp)`, "false\n"},
		{`m = Person.new(id: 9)
any = Google::Protobuf::Any.pack(m)
p any.unpack(Google::Protobuf::Timestamp)`, "nil\n"},
	})
}

// TestProtobufErrors covers every error path: type/range/parse/argument raising,
// the bignum-overflow and unmapped-value bridge failures, and a malformed builder
// spec, all surfaced as the gem's exceptions.
func TestProtobufErrors(t *testing.T) {
	eachEval(t, []struct{ src, want string }{
		// a wrong-typed field value is a Google::Protobuf::TypeError.
		{`begin; Person.new.id = "not an int"; rescue Google::Protobuf::TypeError => e; p e.class; end`,
			"Google::Protobuf::TypeError\n"},
		// an integer out of the field's range is a RangeError.
		{`begin; Person.new.id = 2 ** 40; rescue RangeError => e; p e.class; end`, "RangeError\n"},
		// an unknown enum symbol is a RangeError.
		{`begin; Person.new.color = :MAUVE; rescue RangeError => e; p e.class; end`, "RangeError\n"},
		// decoding garbage bytes is a ParseError.
		{`begin; Google::Protobuf.decode(Person, "\xff\xff\xff".b); rescue Google::Protobuf::ParseError => e; p e.class; end`,
			"Google::Protobuf::ParseError\n"},
		// decoding malformed JSON is a ParseError.
		{`begin; Google::Protobuf.decode_json(Person, "{bad"); rescue Google::Protobuf::ParseError => e; p e.class; end`,
			"Google::Protobuf::ParseError\n"},
		// a value beyond uint64 (the bignum bridge cannot map it) is a TypeError.
		{`begin; Person.new.big = 2 ** 65; rescue Google::Protobuf::TypeError => e; p e.class; end`,
			"Google::Protobuf::TypeError\n"},
		// an unmapped Ruby value handed to a field is a TypeError.
		{`begin; Person.new.id = (1..2); rescue Google::Protobuf::TypeError => e; p e.class; end`,
			"Google::Protobuf::TypeError\n"},
		// encode/decode/encode_json/decode_json type-check their receiver/class.
		{`begin; Google::Protobuf.encode("x"); rescue TypeError => e; puts e.message; end`,
			"encode expects a message\n"},
		{`begin; Google::Protobuf.decode("x", ""); rescue TypeError => e; puts e.message; end`,
			"decode expects a message class\n"},
		{`begin; Google::Protobuf.encode_json("x"); rescue TypeError => e; puts e.message; end`,
			"encode_json expects a message\n"},
		{`begin; Google::Protobuf.decode_json("x", ""); rescue TypeError => e; puts e.message; end`,
			"decode_json expects a message class\n"},
		// decode with a non-String payload.
		{`begin; Google::Protobuf.decode(Person, 5); rescue TypeError => e; puts e.message; end`,
			"expected a String\n"},
		// Any.pack / unpack type-check their argument.
		{`begin; Google::Protobuf::Any.pack("x"); rescue TypeError => e; puts e.message; end`,
			"Any.pack expects a message\n"},
		{`begin; Person.new.unpack("x"); rescue TypeError => e; puts e.message; end`,
			"Any#unpack expects a message class\n"},
		{`m = Person.new(id: 9); any = Google::Protobuf::Any.pack(m); p any.is?("x")`, "false\n"},
		// out-of-range repeated index on write is a RangeError.
		{`begin; Person.new.emails[5] = "x"; rescue RangeError => e; p e.class; end`, "RangeError\n"},
		// a bad type in a repeated field push.
		{`begin; Person.new.emails.push(5); rescue Google::Protobuf::TypeError => e; p e.class; end`,
			"Google::Protobuf::TypeError\n"},
		// concat / + with a non-list.
		{`begin; Person.new.emails.concat(5); rescue Google::Protobuf::TypeError => e; p e.class; end`,
			"Google::Protobuf::TypeError\n"},
		{`begin; Person.new.emails + 5; rescue Google::Protobuf::TypeError => e; p e.class; end`,
			"Google::Protobuf::TypeError\n"},
		// build requires a block.
		{`begin; Google::Protobuf::DescriptorPool.new.build; rescue ArgumentError => e; puts e.message; end`,
			"DescriptorPool#build requires a block\n"},
		// a malformed builder spec (unknown field type) is an ArgumentError.
		{`begin
  Google::Protobuf::DescriptorPool.new.build { add_message("X") { optional :y, :bogus, 1 } }
rescue ArgumentError => e
  p e.class
end`, "ArgumentError\n"},
		// a bad standalone container element type.
		{`begin; Google::Protobuf::RepeatedField.new(:bogus); rescue ArgumentError => e; p e.class; end`,
			"ArgumentError\n"},
		{`begin; Google::Protobuf::Map.new(:bogus, :int32); rescue ArgumentError => e; p e.class; end`,
			"ArgumentError\n"},
	})
}

// TestProtobufBuilderReflection covers the transient builder wrappers' own
// to_s/inspect/truthy and the oneof / add_message-without-block / generated_pool
// paths, and a message-typed oneof field (the typeName branch of a oneof field).
func TestProtobufBuilderReflection(t *testing.T) {
	eachEval(t, []struct{ src, want string }{
		// generated_pool returns a usable pool.
		{`p !!Google::Protobuf::DescriptorPool.generated_pool`, "true\n"},
		// a pool is truthy and inspects to its class name.
		{`p Google::Protobuf::DescriptorPool.new.inspect.include?("DescriptorPool")`, "true\n"},
		{`p Google::Protobuf::DescriptorPool.new.to_s.include?("DescriptorPool")`, "true\n"},
		// the builder wrappers expose to_s/inspect/truthy inside their blocks.
		{`Google::Protobuf::DescriptorPool.new.build do |b|
  raise unless b
  raise unless b.to_s.include?("Builder")
  raise unless b.inspect.include?("Builder")
  b.add_message("Q") do |m|
    raise unless m
    raise unless m.to_s.include?("MessageBuilder")
    raise unless m.inspect.include?("MessageBuilder")
    m.optional :a, :int32, 1
    m.oneof :choice do |o|
      raise unless o
      raise unless o.to_s.include?("OneofBuilder")
      raise unless o.inspect.include?("OneofBuilder")
      o.optional :s, :string, 2
    end
  end
  b.add_enum("E") do |e|
    raise unless e
    raise unless e.to_s.include?("EnumBuilder")
    raise unless e.inspect.include?("EnumBuilder")
    e.value :ZERO, 0
  end
end
p :ok`, ":ok\n"},
		// a message-typed oneof field exercises the oneof optional typeName branch;
		// a repeated message field exercises the repeated typeName branch; a
		// message-valued map exercises the map typeName branch.
		{`pool2 = Google::Protobuf::DescriptorPool.new
pool2.build do
  add_message("Inner") { optional :v, :int32, 1 }
  add_message("Outer") do
    oneof :pick do
      optional :inner, :message, 1, "Inner"
    end
    repeated :items, :message, 2, "Inner"
    map :by_name, :string, :message, 3, "Inner"
  end
end
o = pool2.lookup("Outer").msgclass.new
o.inner = pool2.lookup("Inner").msgclass.new(v: 4)
o.items.push(pool2.lookup("Inner").msgclass.new(v: 5))
o.by_name["k"] = pool2.lookup("Inner").msgclass.new(v: 6)
p [o.inner.v, o.items[0].v, o.by_name["k"].v]`, "[4, 5, 6]\n"},
		// add_message with no field block is a legal empty message (nil-block path).
		{`pool3 = Google::Protobuf::DescriptorPool.new
pool3.build { add_message("Empty2") }
p pool3.lookup("Empty2").msgclass.new.class.name`, "\"Google::Protobuf::Message\"\n"},
	})
}
