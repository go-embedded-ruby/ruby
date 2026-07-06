// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestErubiRequire covers that `require "erubi"` (and "erubi/capture_end")
// returns true on the first load and false thereafter, matching MRI's
// provided-feature contract.
func TestErubiRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "erubi"`, "true\n"},
		{"require \"erubi\"\np require \"erubi\"", "false\n"},
		{`p require "erubi/capture_end"`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestErubiEngineReaders covers Erubi::Engine#src / #filename / #bufvar: the
// compiled Ruby source, the reported filename (nil when unset, the option value
// otherwise), and the output-buffer name (defaulting to "_buf").
func TestErubiEngineReaders(t *testing.T) {
	cases := []struct{ src, want string }{
		// src carries the compiled buffer plumbing.
		{`require "erubi"; p Erubi::Engine.new("a<%= 1 %>").src.include?("_buf")`, "true\n"},
		// the literal text and expression both appear in the source.
		{`require "erubi"; p Erubi::Engine.new("Hi <%= x %>").src.include?("'Hi '.freeze")`, "true\n"},
		// filename defaults to nil, and reflects the :filename option.
		{`require "erubi"; p Erubi::Engine.new("x").filename`, "nil\n"},
		{`require "erubi"; p Erubi::Engine.new("x", filename: "t.erb").filename`, "\"t.erb\"\n"},
		// bufvar defaults to "_buf" and reflects the :bufvar option.
		{`require "erubi"; p Erubi::Engine.new("x").bufvar`, "\"_buf\"\n"},
		{`require "erubi"; p Erubi::Engine.new("x<%= 1 %>", bufvar: "buf").bufvar`, "\"buf\"\n"},
		// class reports Erubi::Engine.
		{`require "erubi"; p Erubi::Engine.new("x").class`, "Erubi::Engine\n"},
		// a trailing non-Hash argument is not treated as options (default bufvar).
		{`require "erubi"; p Erubi::Engine.new("x", 5).bufvar`, "\"_buf\"\n"},
		// the object is truthy, and to_s / inspect render the default object form.
		{`require "erubi"; p(Erubi::Engine.new("x") ? "y" : "n")`, "\"y\"\n"},
		{`require "erubi"; puts "#{Erubi::Engine.new("x")}"`, "#<Erubi::Engine>\n"},
		{`require "erubi"; p Erubi::Engine.new("x")`, "#<Erubi::Engine>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestErubiOptions covers the keyword-option mapping onto the library's Options:
// the tri-state booleans (escape / escape_html / trim / freeze_template_literals,
// including a nil value treated as unset), the plain booleans (freeze /
// chain_appends / ensure), and the string overrides (escapefunc / bufval /
// preamble / postamble / literal_prefix / literal_postfix), each asserted by its
// observable effect on the emitted source.
func TestErubiOptions(t *testing.T) {
	cases := []struct{ src, want string }{
		// escape:true routes <%= %> through the ::Erubi.h helper.
		{`require "erubi"; p Erubi::Engine.new("<%= x %>", escape: true).src.include?("__erubi.h")`, "true\n"},
		// escape:nil is treated as unset (no key): the default is no escaping.
		{`require "erubi"; p Erubi::Engine.new("<%= x %>", escape: nil).src.include?(".h(")`, "false\n"},
		// escape_html is the lower-priority alias, also selecting the escape helper.
		{`require "erubi"; p Erubi::Engine.new("<%= x %>", escape_html: true).src.include?("__erubi.h")`, "true\n"},
		// trim:false disables erubi's newline trimming, so the compiled source
		// differs from the default (trim:true) form for a line-ending tag.
		{`require "erubi"; p Erubi::Engine.new("<% x %>\nb", trim: false).src != Erubi::Engine.new("<% x %>\nb").src`, "true\n"},
		// freeze:true prepends the frozen_string_literal magic comment.
		{`require "erubi"; p Erubi::Engine.new("hi", freeze: true).src.start_with?("# frozen_string_literal: true")`, "true\n"},
		// freeze_template_literals:false drops the .freeze suffix on literal chunks.
		{`require "erubi"; p Erubi::Engine.new("hi", freeze_template_literals: false).src.include?(".freeze")`, "false\n"},
		// chain_appends:true chains the buffer appends (_buf << a << b).
		{`require "erubi"; p Erubi::Engine.new("a<%= 1 %>b", chain_appends: true).src.include?("<< ( 1 ).to_s <<")`, "true\n"},
		// ensure:true wraps the body in begin/ensure.
		{`require "erubi"; p Erubi::Engine.new("hi", ensure: true).src.include?("ensure")`, "true\n"},
		// escapefunc overrides the escape function name.
		{`require "erubi"; p Erubi::Engine.new("<%= x %>", escape: true, escapefunc: "myh").src.include?("myh((")`, "true\n"},
		// bufval sets the buffer's initial value expression.
		{`require "erubi"; p Erubi::Engine.new("hi", bufval: "[]").src.include?("_buf = []")`, "true\n"},
		// preamble and postamble replace the emitted prologue/epilogue.
		{`require "erubi"; p Erubi::Engine.new("hi", preamble: "PRE;").src.start_with?("PRE;")`, "true\n"},
		{`require "erubi"; p Erubi::Engine.new("hi", postamble: "POST\n").src.include?("POST")`, "true\n"},
		// literal_prefix / literal_postfix render the escaped <%% / %%> delimiters.
		{`require "erubi"; p Erubi::Engine.new("<%% x %%>", literal_prefix: "LP", literal_postfix: "RP").src.include?("LP x %RP")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestErubiH covers Erubi.h, the escape helper the compiled source calls: it
// HTML-escapes the five significant characters (with "'" -> &#39;), MRI-faithful,
// coercing a non-String via #to_s first (nil -> "", 123 -> "123").
func TestErubiH(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "erubi"; puts Erubi.h("<a>&\"'")`, "&lt;a&gt;&amp;&quot;&#39;\n"},
		{`require "erubi"; p Erubi.h(nil)`, "\"\"\n"},
		{`require "erubi"; p Erubi.h(123)`, "\"123\"\n"},
		{`require "erubi"; p Erubi.h("plain")`, "\"plain\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestErubiCaptureEnd covers Erubi::CaptureEndEngine — the Rails form-capture
// variant. It subclasses Engine (so it inherits #src / #filename / #bufvar) and
// compiles the <%|= ... %> ... <%| end %> block-capture tags.
func TestErubiCaptureEnd(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "erubi/capture_end"; p Erubi::CaptureEndEngine.new("<%|= form do %>x<%| end %>").src.class`, "String\n"},
		{`require "erubi/capture_end"; p Erubi::CaptureEndEngine.new("x").class`, "Erubi::CaptureEndEngine\n"},
		// inherits from Erubi::Engine.
		{`require "erubi/capture_end"; p Erubi::CaptureEndEngine.new("x").is_a?(Erubi::Engine)`, "true\n"},
		// the inherited bufvar reader works on the subclass.
		{`require "erubi/capture_end"; p Erubi::CaptureEndEngine.new("x", bufvar: "z").bufvar`, "\"z\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestErubiRender is the end-to-end proof: the compiled #src, eval'd against a
// binding, renders the template — and its ::Erubi.h escape calls resolve to the
// Erubi.h wired here, so an escaping <%= %> produces MRI-faithful output.
func TestErubiRender(t *testing.T) {
	cases := []struct{ src, want string }{
		// escaped interpolation renders "<b>" as "&lt;b&gt;".
		{`require "erubi"; src = Erubi::Engine.new("Hello <%= name %>!", escape: true).src; name = "<b>"; puts eval(src, binding)`, "Hello &lt;b&gt;!\n"},
		// unescaped interpolation passes the value through verbatim.
		{`require "erubi"; src = Erubi::Engine.new("<%= a %>+<%= b %>").src; a = 2; b = 3; puts eval(src, binding)`, "2+3\n"},
		// literal text with a <% %> code tag emitting nothing.
		{`require "erubi"; src = Erubi::Engine.new("a<% y = 1 %>b<%= y %>").src; puts eval(src, binding)`, "ab1\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestErubiTypeError covers the non-String template path: strArg raises a
// TypeError, which buildErubiEngine's recover re-raises unchanged (only an
// InvalidIndicatorError is translated to ArgumentError).
func TestErubiTypeError(t *testing.T) {
	got := eval(t, `require "erubi"; begin; Erubi::Engine.new(123); rescue => e; p e.class; end`)
	if want := "TypeError\n"; got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}
