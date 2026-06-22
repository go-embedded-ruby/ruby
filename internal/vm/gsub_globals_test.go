package vm_test

import "testing"

// TestGsubBlockMatchGlobals checks that a sub/gsub replacement block sees the
// current match through $~ / $1.. (previously they were stale, so $1 was nil),
// and that $~ reflects the last match afterwards. Asserted against MRI Ruby
// 4.0.5.
func TestGsubBlockMatchGlobals(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "Hello World".gsub(/(\w+)/) { $1.upcase }`, "\"HELLO WORLD\"\n"},
		{`p "a1b2c3".gsub(/([a-z])(\d)/) { $2 + $1 }`, "\"1a2b3c\"\n"},
		{`p "hello".gsub(/l/) { $~[0].upcase }`, "\"heLLo\"\n"},
		{`p "foo bar".sub(/(\w+)/) { $1.reverse }`, "\"oof bar\"\n"},
		{`p "x=1, y=2".gsub(/(\w)=(\d)/) { "#{$1}:#{$2}" }`, "\"x:1, y:2\"\n"},
		{`p "Hello".gsub(/(?<c>l)/) { $~[:c].upcase }`, "\"HeLLo\"\n"},
		// The block-argument form (whole match) still works alongside the globals.
		{`p "abc".gsub(/./) { |m| m.next }`, "\"bcd\"\n"},
		// $~ reflects the last match after the gsub.
		{`"hello world".gsub(/\w+/) { }; p $~[0]`, "\"world\"\n"},
		// Replacement-template form is unaffected.
		{`p "hello".gsub(/(l)/, "[\\1]")`, "\"he[l][l]o\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
