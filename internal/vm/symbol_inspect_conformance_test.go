package vm_test

import "testing"

// TestSymbolInspectConformance pins Symbol#inspect to MRI 4.0.x byte-for-byte.
// MRI prints a symbol bare (`:name`) when rb_enc_symname_type does not reject it,
// and quotes it (`:"…"`) otherwise. Every expected value here was produced by the
// reference interpreter (ruby 4.0.x, UTF-8 default external encoding).
func TestSymbolInspectConformance(t *testing.T) {
	cases := []struct {
		src  string // a Ruby symbol literal
		want string // its exact #inspect
	}{
		// Plain identifiers and constants.
		{`:fred`, `:fred`},
		{`:Bar`, `:Bar`},
		{`:Constant`, `:Constant`},
		{`:_under`, `:_under`},
		{`:foo_bar`, `:foo_bar`},

		// Identifier with a single trailing ? ! or =.
		{`:fred?`, `:fred?`},
		{`:fred!`, `:fred!`},
		{`:BAD!`, `:BAD!`},
		{`:_BAD!`, `:_BAD!`},
		{`:nil?`, `:nil?`},
		{`:foo=`, `:foo=`},
		{`:Foo=`, `:Foo=`},
		{`:"a=b"`, `:"a=b"`},     // '=' not final -> quoted
		{`:"foo?="`, `:"foo?="`}, // '=' after '?' -> quoted

		// Instance / class / global variable names.
		{`:@ruby`, `:@ruby`},
		{`:@@ruby`, `:@@ruby`},
		{`:$ruby`, `:$ruby`},
		{`:"$ruby!"`, `:"$ruby!"`}, // trailing ! on a global -> quoted
		{`:"$ruby?"`, `:"$ruby?"`},
		{`:"@ruby!"`, `:"@ruby!"`},
		{`:"@ruby?"`, `:"@ruby?"`},
		{`:"@@ruby!"`, `:"@@ruby!"`},
		{`:"@@ruby?"`, `:"@@ruby?"`},
		{`:"@x="`, `:"@x="`}, // '=' on an ivar -> quoted
		{`:"@@x="`, `:"@@x="`},
		{`:"$x="`, `:"$x="`},
		{`:"@"`, `:"@"`},   // bare @ -> quoted
		{`:"@@"`, `:"@@"`}, // bare @@ -> quoted
		{`:"$"`, `:"$"`},   // bare $ -> quoted

		// Special global-variable names.
		{`:$-w`, `:$-w`},
		{`:"$-ww"`, `:"$-ww"`}, // more than one char after $- -> quoted
		{`:"$-"`, `:"$-"`},
		{`:"$+"`, `:$+`},
		{`:"$~"`, `:$~`},
		{`:"$:"`, `:$:`},
		{`:"$?"`, `:$?`},
		{`:"$<"`, `:$<`},
		{`:"$_"`, `:$_`}, // $ + identifier '_'
		{`:"$/"`, `:$/`},
		{`:"$$"`, `:$$`},
		{`:"$."`, `:$.`},
		{`:"$&"`, `:$&`},
		{`:"$@"`, `:$@`},
		{`:"$0"`, `:$0`},
		{`:"$00"`, `:"$00"`},   // leading zero, len>1 -> quoted
		{`:"$012"`, `:"$012"`}, // leading zero -> quoted
		{`:"$1234"`, `:$1234`}, // digits, no leading zero -> bare
		{`:"$ä"`, `:$ä`},       // $ + non-ASCII identifier

		// Operator method names.
		{`:+`, `:+`},
		{`:-`, `:-`},
		{`:*`, `:*`},
		{`:/`, `:/`},
		{`:%`, `:%`},
		{`:**`, `:**`},
		{`:"***"`, `:"***"`},
		{`:==`, `:==`},
		{`:===`, `:===`},
		{`:"===="`, `:"===="`},
		{`:"="`, `:"="`},
		{`:=~`, `:=~`},
		{`:"=>"`, `:"=>"`},
		{`:<`, `:<`},
		{`:<=`, `:<=`},
		{`:<=>`, `:<=>`},
		{`:"<<"`, `:<<`},
		{`:>`, `:>`},
		{`:>=`, `:>=`},
		{`:>>`, `:>>`},
		{`:&`, `:&`},
		{`:|`, `:|`},
		{`:^`, `:^`},
		{`:~`, `:~`},
		{`:"~x"`, `:"~x"`},
		{`:+@`, `:+@`},
		{`:-@`, `:-@`},
		{`:[]`, `:[]`},
		{`:[]=`, `:[]=`},
		{`:"["`, `:"["`},
		{":`", ":`"}, // backtick operator (bare)
		{`:!`, `:!`},
		{`:"!="`, `:!=`},
		{`:"!~"`, `:!~`},
		{`:"!x"`, `:"!x"`}, // '!' + more -> quoted

		// Non-ASCII (valid UTF-8) identifiers print bare.
		{`:äöü`, `:äöü`},
		{`:ê`, `:ê`},
		{`:测`, `:测`},
		{`:🦊`, `:🦊`},
		{`:"±"`, `:±`},
		{`:"²"`, `:²`},
		{`:"①"`, `:①`},
		{`:"a±b"`, `:a±b`},
		{`:"ä="`, `:ä=`},
		{`:"ä?"`, `:ä?`},

		// Names that must be quoted (punctuation, spaces, leading digit, empty).
		{`:"1abc"`, `:"1abc"`},
		{`:"3d"`, `:"3d"`},
		{`:"9"`, `:"9"`},
		{`:"foo bar"`, `:"foo bar"`},
		{`:"*foo"`, `:"*foo"`},
		{`:"foo "`, `:"foo "`},
		{`:" foo"`, `:" foo"`},
		{`:" "`, `:" "`},
		{`:"foo-bar"`, `:"foo-bar"`},
		{`:"++"`, `:"++"`},
		{`:"||"`, `:"||"`},
		{`:","`, `:","`},
		{`:"."`, `:"."`},
		{`:".."`, `:".."`},
		{`:"::"`, `:"::"`},
		{`:""`, `:""`},
	}
	for _, c := range cases {
		got := eval(t, "puts "+c.src+".inspect")
		if got != c.want+"\n" {
			t.Errorf("%s.inspect = %q, want %q", c.src, got, c.want+"\n")
		}
	}
}

// TestSymbolHashLabelConformance pins the Ruby 3.4+ hash short-form (`name:`)
// used when a Symbol is a hash key. It is stricter than Symbol#inspect: only a
// local/constant identifier with an optional single trailing ? or ! prints as a
// bare label; everything else uses the quoted label form `"name":`.
func TestSymbolHashLabelConformance(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{`:foo`, `{foo: 1}`},
		{`:Bar`, `{Bar: 1}`},
		{`:foo?`, `{foo?: 1}`},
		{`:foo!`, `{foo!: 1}`},
		{`:BAD!`, `{BAD!: 1}`},
		{`:_x`, `{_x: 1}`},
		{`:äöü`, `{äöü: 1}`},
		{`:"あ?"`, `{あ?: 1}`},     // identifier + trailing ? -> bare label
		{`:foo=`, `{"foo=": 1}`}, // attrset -> quoted label
		{`:"@x"`, `{"@x": 1}`},   // ivar -> quoted
		{`:"@@x"`, `{"@@x": 1}`},
		{`:"$x"`, `{"$x": 1}`},
		{`:+`, `{"+": 1}`}, // operator -> quoted
		{`:[]`, `{"[]": 1}`},
		{":`", "{\"`\": 1}"},
		{`:!`, `{"!": 1}`},
		{`:"0"`, `{"0": 1}`},
		{`:"a?b"`, `{"a?b": 1}`}, // '?' not final -> quoted
		{`:"a??"`, `{"a??": 1}`},
	}
	for _, c := range cases {
		got := eval(t, "p({"+c.key+" => 1})")
		if got != c.want+"\n" {
			t.Errorf("{%s => 1}.inspect = %q, want %q", c.key, got, c.want+"\n")
		}
	}
}
