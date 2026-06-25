package vm_test

import "testing"

// TestRegexpOptionConstants checks that Regexp::IGNORECASE/EXTENDED/MULTILINE are
// defined with the MRI option-bit values. Asserted against MRI Ruby 4.0.
func TestRegexpOptionConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Regexp::IGNORECASE`, "1\n"},
		{`p Regexp::EXTENDED`, "2\n"},
		{`p Regexp::MULTILINE`, "4\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRegexpNew checks Regexp.new / Regexp.compile across the argument forms:
// no options, integer option masks, the String option-letter form, the legacy
// truthy-second-argument form, and copying an existing Regexp.
func TestRegexpNew(t *testing.T) {
	cases := []struct{ src, want string }{
		// No options.
		{`p Regexp.new("ab")`, "/ab/\n"},
		{`p Regexp.compile("ab")`, "/ab/\n"},
		// Integer option masks.
		{`p Regexp.new("ab", Regexp::IGNORECASE)`, "/ab/i\n"},
		{`p Regexp.new("ab", Regexp::EXTENDED)`, "/ab/x\n"},
		{`p Regexp.new("ab", Regexp::MULTILINE)`, "/ab/m\n"},
		{`p Regexp.new("ab", Regexp::IGNORECASE | Regexp::MULTILINE)`, "/ab/mi\n"},
		{`p Regexp.new("ab", Regexp::IGNORECASE | Regexp::EXTENDED | Regexp::MULTILINE)`, "/ab/mix\n"},
		// Unknown integer bits (encoding flags) are ignored for the i/m/x letters.
		{`p Regexp.new("ab", 0)`, "/ab/\n"},
		{`p Regexp.new("ab", Regexp::EXTENDED | 32)`, "/ab/x\n"},
		// String option-letter form.
		{`p Regexp.new("ab", "i")`, "/ab/i\n"},
		{`p Regexp.new("ab", "mix")`, "/ab/mix\n"},
		{`p Regexp.new("ab", "ii")`, "/ab/i\n"}, // duplicate letters collapse
		{`p Regexp.new("ab", "")`, "/ab/\n"},
		// Legacy truthy-second-argument form selects IGNORECASE.
		{`p Regexp.new("ab", true)`, "/ab/i\n"},
		{`p Regexp.new("ab", 1.5)`, "/ab/i\n"},
		// nil / false select no options.
		{`p Regexp.new("ab", nil)`, "/ab/\n"},
		{`p Regexp.new("ab", false)`, "/ab/\n"},
		// A Regexp argument is copied, reusing its options.
		{`p Regexp.new(/foo/i)`, "/foo/i\n"},
		{`p Regexp.new(/bar/mix)`, "/bar/mix\n"},
		// Extra options on a Regexp argument are ignored (its own options win).
		{`p Regexp.new(/baz/i, Regexp::MULTILINE)`, "/baz/i\n"},
		// A compiled Regexp.new actually matches with the requested options.
		{`p "ABC".match?(Regexp.new("abc", Regexp::IGNORECASE))`, "true\n"},
		{`p "ABC".match?(Regexp.new("abc"))`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRegexpNewErrors checks the error branches of Regexp.new: a non-String,
// non-Regexp argument raises TypeError; an unknown String option letter raises
// ArgumentError; and no arguments raises ArgumentError. Errors are observed via
// begin/rescue so the runtime surfaces them as Ruby exceptions.
func TestRegexpNewErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`begin; Regexp.new(123); rescue => e; p e.class; end`, "TypeError\n"},
		{`begin; Regexp.new(123); rescue => e; p e.message; end`,
			"\"no implicit conversion of Integer into String\"\n"},
		{`begin; Regexp.new("a", "z"); rescue => e; p [e.class, e.message]; end`,
			"[ArgumentError, \"unknown regexp option: z\"]\n"},
		{`begin; Regexp.new("a", "iz"); rescue => e; p e.message; end`,
			"\"unknown regexp option: iz\"\n"},
		{`begin; Regexp.send(:new); rescue => e; p e.class; end`, "ArgumentError\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSubGsubHashReplacement checks the Hash-replacement form of String#sub and
// String#gsub: each matched substring is replaced by hash[match] (a missing key
// or an explicit nil value contributes the empty string). Asserted against MRI.
func TestSubGsubHashReplacement(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "hello".gsub(/[el]/, "e"=>"3","l"=>"1")`, "\"h311o\"\n"},
		{`p "hello".sub(/[el]/, "e"=>"3","l"=>"1")`, "\"h3llo\"\n"},
		{`p "hello".gsub(/[xyz]/, "a"=>"b")`, "\"hello\"\n"}, // no match
		{`p "hello".gsub(/l/, "x"=>"y")`, "\"heo\"\n"},       // missing key → ""
		{`p "hello".gsub(/l/, "l"=>nil)`, "\"heo\"\n"},       // explicit nil → ""
		{`p "aaa".gsub(/a/, "a"=>"X")`, "\"XXX\"\n"},
		{`p "a-b-c".sub(/-/, "-"=>"+")`, "\"a+b-c\"\n"},
		// Empty-match handling (advances one char per Ruby semantics).
		{`p "abc".gsub(/x*/, "x"=>"!")`, "\"abc\"\n"},
		// Non-string values are coerced with to_s.
		{`p "n5".gsub(/\d/, "5"=>5)`, "\"n5\"\n"},
		// sub! / gsub! with a Hash mutate the receiver.
		{`s = "hello"; s.gsub!(/l/, "l"=>"L"); p s`, "\"heLLo\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSubGsubEnumerator checks the enumerator form: gsub(pattern) with no
// replacement and no block returns an Enumerator over the matches, supporting
// to_a / with_index / map. sub with one argument and no block raises
// ArgumentError, and so does sub!; gsub! returns an Enumerator bound to gsub!.
func TestSubGsubEnumerator(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "a1b2".gsub(/\d/).class`, "Enumerator\n"},
		{`p "a1b2".gsub(/\d/).to_a`, "[\"1\", \"2\"]\n"},
		{`p "hello".gsub(/l/).to_a`, "[\"l\", \"l\"]\n"},
		{`p "hello".gsub(/l/).with_index { |m,i| "#{m}#{i}" }`, "\"hel0l1o\"\n"},
		{`p "abcabc".gsub(/a/).with_index { |m, i| "#{i}" }`, "\"0bc1bc\"\n"},
		{`p " foo bar ".gsub(/\w+/).map(&:upcase)`, "[\"FOO\", \"BAR\"]\n"},
		// gsub! with no replacement/block yields an Enumerator bound to gsub!,
		// so materialising it mutates the receiver.
		{`s = "hello"; e = s.gsub!(/l/); p e.class`, "Enumerator\n"},
		{`s = "hello"; e = s.gsub!(/l/); p e.to_a; p s`, "[\"l\", \"l\"]\n\"heo\"\n"},
		// sub / sub! with one argument and no block raise ArgumentError.
		{`begin; "x".sub(/x/); rescue => e; p e.class; end`, "ArgumentError\n"},
		{`begin; "x".sub!(/x/); rescue => e; p e.class; end`, "ArgumentError\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
