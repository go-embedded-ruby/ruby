package vm_test

import "testing"

// TestBuiltinSubclassing covers user subclasses of the built-in value types
// String, Array and Hash: the value type's own methods and operators apply to the
// wrapped value, while class identity, user methods, instance variables and
// object identity stay with the instance. Asserted against MRI Ruby 4.0.5.
func TestBuiltinSubclassing(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- String ---
		// Built-in methods + operators (on either side) + identity.
		{`class S < String; end; s = S.new("hi"); p [s.upcase, s + "!", s * 2, "pre" + s, s.length]`, "[\"HI\", \"hi!\", \"hihi\", \"prehi\", 2]\n"},
		{`class S < String; end; p [S.new("hi").is_a?(String), S.new("hi").class.name, S.new("ab") == "ab", S.new("a") < S.new("b")]`, "[true, \"S\", true, true]\n"},
		// A built-in method returns the base type, not the subclass (as in MRI).
		{`class S < String; end; p S.new("ab").upcase.class`, "String\n"},
		// User method, super to String#initialize, and an instance variable.
		{`class S < String
  def shout; upcase + "!"; end
  def initialize(s); super; @n = s.size; end
  attr_reader :n
end
o = S.new("hey"); p [o.shout, o.n, o.reverse]`, "[\"HEY!\", 3, \"yeh\"]\n"},
		// inspect / to_s / interpolation render as the wrapped value.
		{`class S < String; end; p [S.new("hi"), S.new("hi").to_s, "#{S.new("x")}"]`, "[\"hi\", \"hi\", \"x\"]\n"},
		// puts uses the Go-level to-string of the wrapped value.
		{`class S < String; end; puts S.new("hi")`, "hi\n"},

		// --- Array ---
		{`class A < Array; end; a = A.new([1, 2, 3]); p [a.map { |x| x * 2 }, a.size, a.class.name, a.is_a?(Array)]`, "[[2, 4, 6], 3, \"A\", true]\n"},
		{`class A < Array; end; p [A.new.size, A.new(3, 0), A.new(2) { |i| i * i }]`, "[0, [0, 0, 0], [0, 1]]\n"},
		// A subclass instance renders and concatenates as an Array.
		{`class Stack < Array; def push2(x); push(x); self; end; end
s = Stack.new; s.push2(1).push2(2); p [s, s + [3], s.class.name]`, "[[1, 2], [1, 2, 3], \"Stack\"]\n"},

		// --- Hash ---
		{`class H < Hash; end; h = H.new(0); h[:a] = 1; p [h[:a], h[:missing], h.class.name, h.is_a?(Hash)]`, "[1, 0, \"H\", true]\n"},
		{`class H < Hash; def fetch_or(k); self[k] || :none; end; end
h = H.new; h[:x] = 9; p [h.fetch_or(:x), h.fetch_or(:y), h.keys, h]`, "[9, :none, [:x], {x: 9}]\n"},
		{`class H < Hash; end; h = H.new { |hash, k| k.to_s }; p h[:abc]`, "\"abc\"\n"},

		// Object identity / equal? keep operating on the wrapper, not the value.
		{`class S < String; end; a = S.new("x"); p [a.equal?(a), a.equal?(S.new("x"))]`, "[true, false]\n"},
		// A plain object (no wrapped value) still renders as #<Class>.
		{`class Plain; end; p Plain.new.to_s`, "\"#<Plain>\"\n"},
		// As a Hash key, a String-subclass instance hashes/compares as its content.
		{`class S < String; end; h = {}; h[S.new("a")] = 1; p [h["a"], h[S.new("a")], h.keys.first.class]`, "[1, 1, S]\n"},
		{`class S < String; end; p({"k" => 1}[S.new("k")])`, "1\n"},
		// A plain object keeps object-identity hashing.
		{`class K; end; k = K.new; h = {k => 5}; p [h[k], h[K.new]]`, "[5, nil]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
