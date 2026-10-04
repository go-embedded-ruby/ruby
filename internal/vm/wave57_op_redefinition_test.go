// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestOperatorOpcodeHonoursARedefinition: the operator opcodes specialise for
// the built-in value types, and MRI guards every such specialisation with
// BASIC_OP_UNREDEFINED_P. Without that guard the same call answered twice over:
//
//	class Integer; def +(o); :HIJACKED; end; end
//	1 + 2            ruby 4.0.5  :HIJACKED    before  3
//	1.send(:+, 2)    ruby 4.0.5  :HIJACKED    before  :HIJACKED
//
// Each row below is a separate eval, because a redefinition is global: measured
// over all 88 (class, operator) pairs the opcodes reach, 60 diverged, and #send
// answered correctly in every one of them.
func TestOperatorOpcodeHonoursARedefinition(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"Integer+", `class Integer; def +(o); :H; end; end
p 1 + 2
p 1.send(:+, 2)`, ":H\n:H\n"},
		{"Integer<", `class Integer; def <(o); :H; end; end
p 1 < 2`, ":H\n"},
		{"Integer==", `class Integer; def ==(o); :H; end; end
p 1 == 2`, ":H\n"},
		{"Float*", `class Float; def *(o); :H; end; end
p 2.0 * 3.0`, ":H\n"},
		{"Float>=", `class Float; def >=(o); :H; end; end
p 1.0 >= 2.0`, ":H\n"},
		{"String+", `class String; def +(o); :H; end; end
p "a" + "b"`, ":H\n"},
		{"String%", `class String; def %(o); :H; end; end
p "a" % "b"`, ":H\n"},
		{"Array+", `class Array; def +(o); :H; end; end
p [1] + [2]`, ":H\n"},
		{"Hash==", `class Hash; def ==(o); :H; end; end
p({a: 1} == {b: 2})`, ":H\n"},
		{"Symbol<=", `class Symbol; def <=(o); :H; end; end
p :a <= :b`, ":H\n"},
		{"NilClass==", `class NilClass; def ==(o); :H; end; end
p nil == nil`, ":H\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestOperatorOpcodeSeesSubclassesAndPrepends: the guard resolves on the
// ORIGINAL receiver through the full dispatch chain, not on the unwrapped
// built-in value, so a subclass's own operator and a prepended module's are
// both found. The middle row is the control -- a subclass that does NOT
// override keeps the inline path and the built-in answer.
func TestOperatorOpcodeSeesSubclassesAndPrepends(t *testing.T) {
	src := `class S < String; def +(o); :SUB; end; end
class T < String; end
module M; def +(o); :MOD; end; end
class U < String; prepend M; end
p(S.new("a") + "b")
p(T.new("a") + "b")
p(U.new("a") + "b")
o = Object.new
def o.+(x); :SING; end
p(o + 1)
`
	const want = ":SUB\n\"ab\"\n:MOD\n:SING\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestOperatorCacheSeesAMidRunRedefinition: the guard caches its answer per
// operator, keyed on the receiver class and globalMethodSerial, so the usual
// case costs no method resolution. The cache must therefore be invalidated by a
// redefinition that happens AFTER the operator has already been used -- which
// the census could not test, since every case there redefines before using.
//
// Two operators and two classes, because an entry is per OPERATOR: a cache
// keyed per class alone would answer String#+ out of Integer's entry.
func TestOperatorCacheSeesAMidRunRedefinition(t *testing.T) {
	src := `p 1 + 2
p "a" + "b"
p 1 * 2
class Integer; def +(o); :I; end; end
class String;  def +(o); :S; end; end
p 1 + 2
p "a" + "b"
p 1 * 2
`
	const want = "3\n\"ab\"\n2\n:I\n:S\n2\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestRemovingABuiltinOperatorRaises: installing the operator records makes
// `remove_method :+` SUCCEED where it used to raise NameError, so the removal
// has to take effect -- otherwise the new records would have replaced a
// NameError with a silent no-op. MRI raises from both the opcode and #send.
func TestRemovingABuiltinOperatorRaises(t *testing.T) {
	src := `class Integer; remove_method :+; end
begin; p 1 + 2; rescue => e; puts "#{e.class}: #{e.message}"; end
begin; p 1.send(:+, 2); rescue => e; puts "#{e.class}: #{e.message}"; end
`
	const want = "NoMethodError: undefined method '+' for an instance of Integer\n" +
		"NoMethodError: undefined method '+' for an instance of Integer\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestOperatorMethodRecordsMatchMRI: #755 reported Integer and Float missing a
// record for + - * /. Measured across nine classes it was fourteen, not eight.
// The rows are ruby 4.0.5's own answers, including the ones that must STAY
// absent -- String has no #- or #/ and Array no #/ or #%, so a blanket
// installation would have been wrong in the other direction.
func TestOperatorMethodRecordsMatchMRI(t *testing.T) {
	src := `ops = %w[+ - * / % < > <= >= == !=]
[Integer, Float, String, Array, Hash, Symbol, NilClass, TrueClass, FalseClass].each do |c|
  have = ops.select { |o| begin; c.instance_method(o.to_sym); true; rescue; false; end }
  puts "#{c}: #{have.join(' ')}"
end
p Integer.instance_method(:+).arity
p Integer.instance_method(:+).owner
p 1.method(:+).call(2)
`
	const want = `Integer: + - * / % < > <= >= == !=
Float: + - * / % < > <= >= == !=
String: + * % < > <= >= == !=
Array: + - * == !=
Hash: < > <= >= == !=
Symbol: < > <= >= == !=
NilClass: == !=
TrueClass: == !=
FalseClass: == !=
1
Integer
3
`
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestInterpolationDoesNotDispatchStringOperators: interpolation compiled to
// `"" + part.to_s + …`, so it depended on String#+ and sent #to_s to every
// segment including the literal ones. MRI emits concatstrings and sends its
// tostring only to the embedded expressions. Both rows were observable: the
// #to_s one on main already (ruby "A1B", rbgo TypeError), the #+ one as soon as
// the operator opcodes started honouring a redefinition.
func TestInterpolationDoesNotDispatchStringOperators(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"to_s", `class String; def to_s; :TOS; end; end
p "A#{1}B"`, "\"A1B\"\n"},
		{"plus", `class String; def +(o); :P; end; end
p "A#{1}B"`, "\"A1B\"\n"},
		{"both", `class String; def to_s; :T; end; def +(o); :P; end; end
p "A#{1}B#{2}C"`, "\"A1B2C\"\n"},
		{"frozen", `p "a#{1}".frozen?`, "false\n"},
		{"encoding", `p "é#{1}".encoding.to_s`, "\"UTF-8\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestInterpolationFallsBackToAnyToS is MRI's anytostring: when #to_s does not
// return a String, the fallback renders the VALUE with rb_any_to_s rather than
// raising. The address is masked, since it is an address.
func TestInterpolationFallsBackToAnyToS(t *testing.T) {
	src := `class Foo; def to_s; :notastring; end; end
class Bar; def to_s; 42; end; end
puts "x#{Foo.new}y".sub(/0x[0-9a-f]+/, "0xA")
puts "p#{Bar.new}q".sub(/0x[0-9a-f]+/, "0xA")
puts "e#{nil}f"
`
	const want = "x#<Foo:0xA>y\np#<Bar:0xA>q\nef\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
