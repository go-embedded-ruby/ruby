// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io"
	"testing"

	actionview "github.com/go-ruby-actionview/actionview"
	erb "github.com/go-ruby-erb/erb"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
	"github.com/go-ruby-parser/parser/ast"
)

// avRun compiles and runs src on v, returning the value of its last expression and
// failing the test on any parse/compile/runtime error.
func avRun(t *testing.T, v *VM, src string) object.Value {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	res, rerr := v.Run(iseq)
	if rerr != nil {
		t.Fatalf("run %q: %v", src, rerr)
	}
	return res
}

// avEval runs src and returns its result rendered as a string (a SafeBuffer's
// markup or any value's to_s).
func avEval(t *testing.T, v *VM, src string) string {
	t.Helper()
	return avRun(t, v, src).ToS()
}

// avEq runs src and asserts its string result equals want.
func avEq(t *testing.T, v *VM, src, want string) {
	t.Helper()
	if got := avEval(t, v, src); got != want {
		t.Errorf("src=%s\n got=%q\nwant=%q", src, got, want)
	}
}

// avRunErr runs src expecting a RubyError and returns its class.
func avRunErr(t *testing.T, v *VM, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	_, rerr := v.Run(iseq)
	re, ok := rerr.(RubyError)
	if !ok {
		t.Fatalf("src=%q: expected a RubyError, got %#v", src, rerr)
	}
	return re.Class
}

// TestActionViewSafeBuffer covers the ActiveSupport::SafeBuffer surface and
// String#html_safe / #html_safe?.
func TestActionViewSafeBuffer(t *testing.T) {
	v := New(io.Discard)
	avEq(t, v, `"<b>".html_safe.html_safe?`, "true")
	avEq(t, v, `"<b>".html_safe?`, "false")
	avEq(t, v, `"<b>".html_safe.class.name`, "ActiveSupport::SafeBuffer")
	avEq(t, v, `ActiveSupport::SafeBuffer.new.empty?`, "true")
	avEq(t, v, `ActiveSupport::SafeBuffer.new("hi").length`, "2")
	avEq(t, v, `ActiveSupport::SafeBuffer.new("hi").to_s`, "hi")
	avEq(t, v, `ActiveSupport::SafeBuffer.new("hi").to_str`, "hi")
	avEq(t, v, `s = "hi".html_safe; (s.html_safe.equal?(s)).to_s`, "true")
	// << escapes an unsafe String, appends a SafeBuffer verbatim; safe_concat is
	// verbatim; concat is the << alias.
	avEq(t, v, `b = ActiveSupport::SafeBuffer.new("a"); b << "<x>"; b.to_s`, "a&lt;x&gt;")
	avEq(t, v, `b = ActiveSupport::SafeBuffer.new("a"); b << "<x>".html_safe; b.to_s`, "a<x>")
	avEq(t, v, `b = ActiveSupport::SafeBuffer.new("a"); b.concat("<y>"); b.to_s`, "a&lt;y&gt;")
	avEq(t, v, `b = ActiveSupport::SafeBuffer.new("a"); b.safe_concat("<z>"); b.to_s`, "a<z>")
	// The OutputBuffer alias resolves to the same class.
	avEq(t, v, `ActionView::OutputBuffer.new("q").to_s`, "q")
}

// TestActionViewOutputSafety covers raw / html_escape / h.
func TestActionViewOutputSafety(t *testing.T) {
	v := New(io.Discard)
	avEq(t, v, `ActionView::Base.new.raw("<b>").to_s`, "<b>")
	avEq(t, v, `ActionView::Base.new.h("<b>").to_s`, "&lt;b&gt;")
	avEq(t, v, `ActionView::Base.new.html_escape("<b>").to_s`, "&lt;b&gt;")
	// h on an already-safe buffer returns it unescaped.
	avEq(t, v, `ActionView::Base.new.h("<b>".html_safe).to_s`, "<b>")
}

// TestActionViewTagHelpers covers content_tag (positional + block), tag,
// token_list/class_names and cdata_section, plus the avValue/avKey fallbacks.
func TestActionViewTagHelpers(t *testing.T) {
	v := New(io.Discard)
	avEq(t, v, `ActionView::Base.new.content_tag(:p, "hi", class: "x")`, `<p class="x">hi</p>`)
	avEq(t, v, `ActionView::Base.new.content_tag(:div) { "<b>" }`, `<div>&lt;b&gt;</div>`)
	avEq(t, v, `ActionView::Base.new.content_tag(:div) { "<b>".html_safe }`, `<div><b></div>`)
	avEq(t, v, `ActionView::Base.new.tag(:br)`, `<br>`)
	avEq(t, v, `ActionView::Base.new.tag(:circle)`, `<circle />`)
	avEq(t, v, `ActionView::Base.new.token_list("a", "b", "a")`, "a b")
	avEq(t, v, `ActionView::Base.new.class_names(["a", {"b" => true, "c" => false}])`, "a b")
	avEq(t, v, `ActionView::Base.new.cdata_section("x]]>y")`, "<![CDATA[x]]]]><![CDATA[>y]]>")
	// avValue fallback (a Range -> to_s), int/float/bool/symbol attr values.
	avEq(t, v, `ActionView::Base.new.content_tag(:p, (1..3))`, `<p>1..3</p>`)
	avEq(t, v, `ActionView::Base.new.tag(:input, nil, {"data-n" => 5, "checked" => true, "x" => :y})`,
		`<input data-n="5" checked="checked" x="y">`)
	// avKey fallback: a non-String/Symbol attribute key renders via to_s.
	avEq(t, v, `ActionView::Base.new.tag(:span, nil, {1 => "a"})`, `<span 1="a"></span>`)
}

// TestActionViewUrlHelpers covers link_to (positional + block), mail_to, button_to
// and current_page? / url_for, plus the avToS fallback.
func TestActionViewUrlHelpers(t *testing.T) {
	v := New(io.Discard)
	avEq(t, v, `ActionView::Base.new.link_to("Home", "/home", class: "nav")`, `<a class="nav" href="/home">Home</a>`)
	avEq(t, v, `ActionView::Base.new.link_to("/go") { "Go" }`, `<a href="/go">Go</a>`)
	// avToS fallback on a non-String url argument.
	avEq(t, v, `ActionView::Base.new.link_to("t", 123)`, `<a href="123">t</a>`)
	avEq(t, v, `ActionView::Base.new.mail_to("a@b.com")`, `<a href="mailto:a@b.com">a@b.com</a>`)
	avEq(t, v, `ActionView::Base.new.button_to("Del", "/x", method: "delete").include?("_method")`, "true")
	avEq(t, v, `ActionView::Base.new.button_to("Go", "/y").include?("button_to")`, "true")

	// current_page? / url_for against configured request state.
	avEq(t, v, `b = ActionView::Base.new; b.request_path = "/home"; b.request_method = "get"; b.current_page?("/home").to_s`, "true")
	avEq(t, v, `b = ActionView::Base.new; b.request_path = "/home"; b.current_page?("/other").to_s`, "false")
	avEq(t, v, `b = ActionView::Base.new; b.request_path = "/p"; b.request_fullpath = "/p?q=1"; b.current_page?("/p?q=1").to_s`, "true")
	avEq(t, v, `ActionView::Base.new.url_for("/plain")`, "/plain")
}

// TestActionViewUrlForSeam covers the url_for routes seam: a wired Ruby callable
// and the avProc no-op (a non-Proc assignment clears it).
func TestActionViewUrlForSeam(t *testing.T) {
	v := New(io.Discard)
	avEq(t, v, `b = ActionView::Base.new; b.url_for = ->(x){ "/r/#{x}" }; b.url_for("home")`, "/r/home")
	// A non-Proc assignment leaves no callable, so url_for falls back to identity.
	avEq(t, v, `b = ActionView::Base.new; b.url_for = 5; b.url_for("/plain")`, "/plain")
}

// TestActionViewFormTagHelpers covers the object-independent form helpers.
func TestActionViewFormTagHelpers(t *testing.T) {
	v := New(io.Discard)
	avEq(t, v, `ActionView::Base.new.text_field_tag("q", "hi")`, `<input type="text" name="q" id="q" value="hi" />`)
	avEq(t, v, `ActionView::Base.new.password_field_tag("pw")`, `<input type="password" name="pw" id="pw" />`)
	avEq(t, v, `ActionView::Base.new.hidden_field_tag("h", "1").include?("hidden")`, "true")
	avEq(t, v, `ActionView::Base.new.text_area_tag("body", "txt")`, "<textarea name=\"body\" id=\"body\">\ntxt</textarea>")
	avEq(t, v, `ActionView::Base.new.check_box_tag("ok", "1", true).include?("checked")`, "true")
	avEq(t, v, `ActionView::Base.new.check_box_tag("ok").include?("checked")`, "false")
	avEq(t, v, `ActionView::Base.new.radio_button_tag("c", "r", true).include?("checked")`, "true")
	avEq(t, v, `ActionView::Base.new.radio_button_tag("c", "r").include?("checked")`, "false")
	avEq(t, v, `ActionView::Base.new.label_tag("name")`, `<label for="name">Name</label>`)
	avEq(t, v, `ActionView::Base.new.submit_tag.include?("Save changes")`, "true")
	avEq(t, v, `ActionView::Base.new.button_tag { "Go" }`, `<button name="button" type="submit">Go</button>`)
	avEq(t, v, `ActionView::Base.new.button_tag("Save")`, `<button name="button" type="submit">Save</button>`)

	// options_for_select: string elements and [text, value] pairs, with a selected
	// value; then select_tag wrapping option markup (String path) with include_blank.
	avEq(t, v, `ActionView::Base.new.options_for_select(["a", "b"], "b")`,
		"<option value=\"a\">a</option>\n<option selected=\"selected\" value=\"b\">b</option>")
	avEq(t, v, `ActionView::Base.new.options_for_select([["Text", "v"]])`, `<option value="v">Text</option>`)
	// avChoices nil branch (non-Array container) yields no options.
	avEq(t, v, `ActionView::Base.new.options_for_select("x").to_s`, "")
	avEq(t, v, `ActionView::Base.new.select_tag("s", "<option>a</option>", include_blank: true).include?("label")`, "true")

	// form_tag: block (complete form) and non-block (open tag only), and the
	// utf8/method/CSRF hidden fields toggled by configuration.
	avEq(t, v, `b = ActionView::Base.new; b.form_tag("/y") { b.text_field_tag("q") }.include?("</form>")`, "true")
	avEq(t, v, `b = ActionView::Base.new; b.protect_against_forgery = true; b.authenticity_token = "tok"; b.form_tag("/x", method: "patch").include?("authenticity_token")`, "true")
	avEq(t, v, `b = ActionView::Base.new; b.suppress_utf8_enforcer = true; b.form_tag("/x").include?("utf8")`, "false")
}

// TestActionViewFormBuilder covers form_with and the yielded FormBuilder methods,
// plus the avModelMap variants.
func TestActionViewFormBuilder(t *testing.T) {
	v := New(io.Discard)
	src := `
b = ActionView::Base.new
b.form_with(scope: "user", url: "/users", model: {"email" => "a@b.com", "admin" => true}) do |f|
  f.label(:email) + f.text_field(:email) + f.password_field(:password) +
    f.hidden_field(:token) + f.text_area(:bio) + f.check_box(:admin) +
    f.radio_button(:role, "x") + f.select(:role, ["a", "b"]) + f.submit +
    f.object_name + f.field_name(:email) + f.field_id(:email)
end`
	out := avEval(t, v, src)
	for _, want := range []string{
		`name="user[email]"`, `id="user_email"`, `value="a@b.com"`, `type="password"`,
		`checked="checked"`, `<select`, `type="submit"`, "useruser[email]user_email",
	} {
		if !containsAV(out, want) {
			t.Errorf("form_with output missing %q\ngot: %s", want, out)
		}
	}
	// avModelMap: a missing :model and a non-Hash :model both yield an empty model.
	avEq(t, v, `ActionView::Base.new.form_with(scope: "u", url: "/u") { |f| f.text_field(:x) }.include?("u[x]")`, "true")
	avEq(t, v, `ActionView::Base.new.form_with(scope: "u", url: "/u", model: 5) { |f| f.text_field(:x) }.include?("u[x]")`, "true")
	// form_with with no block yields an empty form body.
	avEq(t, v, `ActionView::Base.new.form_with(scope: "u", url: "/u").include?("</form>")`, "true")
}

// TestActionViewTextHelpers covers the TextHelper family.
func TestActionViewTextHelpers(t *testing.T) {
	v := New(io.Discard)
	avEq(t, v, `ActionView::Base.new.truncate("Hello World", length: 8)`, "Hello...")
	// avHashInt non-Integer branch (length is a String) falls back to the default 30.
	avEq(t, v, `ActionView::Base.new.truncate("short", length: "bad")`, "short")
	// avHashVal String-key lookup (Symbol key absent, String key present).
	avEq(t, v, `ActionView::Base.new.truncate("Hello World", "length" => 8)`, "Hello...")
	avEq(t, v, `ActionView::Base.new.pluralize(2, "person", "people")`, "2 people")
	avEq(t, v, `ActionView::Base.new.pluralize(1, "person")`, "1 person")
	avEq(t, v, `ActionView::Base.new.simple_format("a\n\nb", {}, wrapper_tag: "div").include?("<div>")`, "true")
	avEq(t, v, `ActionView::Base.new.word_wrap("aa bb cc", line_width: 5)`, "aa bb\ncc")
	avEq(t, v, `ActionView::Base.new.highlight("hello world", "world")`, "hello <mark>world</mark>")
	avEq(t, v, `ActionView::Base.new.highlight("a b c", ["a", "c"], highlighter: "[\\1]")`, "[a] b [c]")
	avEq(t, v, `ActionView::Base.new.excerpt("this is a test", "is", radius: 3)`, "this is...")
	// excerpt not-found returns nil.
	avEq(t, v, `ActionView::Base.new.excerpt("abc", "zzz").inspect`, "nil")
}

// TestActionViewNumberHelpers covers all eight NumberHelper methods and the number
// options mapping.
func TestActionViewNumberHelpers(t *testing.T) {
	v := New(io.Discard)
	avEq(t, v, `ActionView::Base.new.number_to_currency(1234.5)`, "$1,234.50")
	avEq(t, v, `ActionView::Base.new.number_to_currency(9.5, unit: "€", precision: 0, format: "%n%u", separator: ",", delimiter: ".", negative_format: "(%n)")`, "10€")
	avEq(t, v, `ActionView::Base.new.number_with_delimiter(12345)`, "12,345")
	avEq(t, v, `ActionView::Base.new.number_to_delimited(12345)`, "12,345")
	avEq(t, v, `ActionView::Base.new.number_with_precision(3.14159, precision: 2)`, "3.14")
	avEq(t, v, `ActionView::Base.new.number_to_rounded(3.14159, precision: 1)`, "3.1")
	avEq(t, v, `ActionView::Base.new.number_to_percentage(66.667, precision: 1)`, "66.7%")
	avEq(t, v, `ActionView::Base.new.number_to_human_size(1536)`, "1.5 KB")
	avEq(t, v, `ActionView::Base.new.number_to_human(1234567, significant: true, strip_insignificant_zeros: true)`, "1.23 Million")
}

// TestActionViewRenderInline covers the default inline-ERB render seam: locals, a
// helper call inside the template, an @ivar, and a collection with the
// PartialIteration and counter locals.
func TestActionViewRenderInline(t *testing.T) {
	v := New(io.Discard)
	avEq(t, v, `ActionView::Base.new.render(inline: "Hi <%= name %>", locals: {name: "Bob"}).to_s`, "Hi Bob")
	// A helper method dispatches on self inside the template.
	avEq(t, v, `ActionView::Base.new.render(inline: "<%= content_tag(:b, name) %>", locals: {name: "x"}).to_s`, "<b>x</b>")
	// An @ivar set on the context resolves in the template (ivarTable seam).
	avEq(t, v, `b = ActionView::Base.new; b.instance_variable_set(:@g, "Hey"); b.render(inline: "<%= @g %>").to_s`, "Hey")
	// A collection binds each element plus its _counter and _iteration; spacer joins.
	avEq(t, v, `ActionView::Base.new.render(partial: "<%= item %>@<%= item_iteration.index %>/<%= item_iteration.size %>:<%= item_iteration.first? %>:<%= item_iteration.last? %>:<%= item_counter %>", collection: ["a", "b"], as: "item", spacer_template: "|").to_s`,
		"a@0/2:true:false:0|b@1/2:false:true:1")
	// A locals-less inline render.
	avEq(t, v, `ActionView::Base.new.render(inline: "plain").to_s`, "plain")
}

// TestActionViewRenderSeam covers the render_template callable seam and the render
// error branches.
func TestActionViewRenderSeam(t *testing.T) {
	v := New(io.Discard)
	// A wired render_template resolver receives the identifier and locals Hash.
	avEq(t, v, `b = ActionView::Base.new; b.render_template = ->(id, locals){ "R:#{id}:#{locals["x"]}" }; b.render(template: "show", locals: {x: 7}).to_s`, "R:show:7")
	// A partial identifier routes through the same seam.
	avEq(t, v, `b = ActionView::Base.new; b.render_template = ->(id, _){ "P:#{id}" }; b.render(partial: "row").to_s`, "P:row")
	// A non-Proc render_template assignment clears the seam (falls back to inline).
	avEq(t, v, `b = ActionView::Base.new; b.render_template = nil; b.render(inline: "ok").to_s`, "ok")
	// render with no source key and with a non-Hash argument both raise ArgumentError.
	if c := avRunErr(t, v, `ActionView::Base.new.render({})`); c != "ArgumentError" {
		t.Errorf("render({}) class=%q want ArgumentError", c)
	}
	if c := avRunErr(t, v, `ActionView::Base.new.render(5)`); c != "ArgumentError" {
		t.Errorf("render(5) class=%q want ArgumentError", c)
	}
}

// TestActionViewInlineRenderErrors covers the three inline-render error branches
// via fault injection (an ERB compile failure and the two front-end failures).
func TestActionViewInlineRenderErrors(t *testing.T) {
	v := New(io.Discard)

	origCompile := erbCompile
	erbCompile = func(string, erb.Options) (string, string, error) {
		return "", "", errors.New("boom")
	}
	if c := avRunErr(t, v, `ActionView::Base.new.render(inline: "x").to_s`); c != "ArgumentError" {
		t.Errorf("erb-compile failure class=%q want ArgumentError", c)
	}
	erbCompile = origCompile

	origParse := avParse
	avParse = func(string) (*ast.Program, error) { return nil, errors.New("bad parse") }
	if c := avRunErr(t, v, `ActionView::Base.new.render(inline: "x").to_s`); c != "SyntaxError" {
		t.Errorf("parse failure class=%q want SyntaxError", c)
	}
	avParse = origParse

	origCompileL := avCompileWithLocals
	avCompileWithLocals = func(*ast.Program, []string) (*bytecode.ISeq, error) {
		return nil, errors.New("bad compile")
	}
	if c := avRunErr(t, v, `ActionView::Base.new.render(inline: "x").to_s`); c != "SyntaxError" {
		t.Errorf("compile failure class=%q want SyntaxError", c)
	}
	avCompileWithLocals = origCompileL
}

// TestActionViewEdgeCases covers the remaining option-mapping branches: a
// non-Hash :locals, an options Hash carrying both a consumed key and an extra HTML
// attribute (the avOptsExcept append path), and form_with's html option and
// non-Hash argument.
func TestActionViewEdgeCases(t *testing.T) {
	v := New(io.Discard)
	// A non-Hash :locals is ignored (no locals bound).
	avEq(t, v, `ActionView::Base.new.render(inline: "x", locals: 5).to_s`, "x")
	// form_tag with method: (consumed) and class: (kept) exercises avOptsExcept's
	// skip-and-append branches together.
	avEq(t, v, `ActionView::Base.new.form_tag("/x", method: "patch", class: "f").include?("class=\"f\"")`, "true")
	// form_with with an :html attribute Hash.
	avEq(t, v, `ActionView::Base.new.form_with(scope: "u", url: "/u", html: {class: "f"}) { |f| f.text_field(:x) }.include?("class=\"f\"")`, "true")
	// form_with with a non-Hash argument raises ArgumentError.
	if c := avRunErr(t, v, `ActionView::Base.new.form_with(5)`); c != "ArgumentError" {
		t.Errorf("form_with(5) class=%q want ArgumentError", c)
	}
}

// TestActionViewValueMethods covers the object.Value method set (ToS/Inspect/
// Truthy) on the binding's value structs directly, and the truthiness path a
// conditional exercises.
func TestActionViewValueMethods(t *testing.T) {
	v := New(io.Discard)
	b := v.newActionViewBase()
	if b.ToS() != "#<ActionView::Base>" || b.Inspect() != "#<ActionView::Base>" || !b.Truthy() {
		t.Errorf("ActionViewBase value methods: %q / %q / %v", b.ToS(), b.Inspect(), b.Truthy())
	}
	sb := v.newSafeBuffer(actionview.Raw("hi"))
	if sb.ToS() != "hi" || sb.Inspect() != `"hi"` || !sb.Truthy() {
		t.Errorf("SafeBufferVal value methods: %q / %q / %v", sb.ToS(), sb.Inspect(), sb.Truthy())
	}
	pi := &AVPartialIter{cls: avPartialIterClass, p: actionview.PartialIteration{Index: 0, Size: 1}}
	if pi.ToS() != "#<ActionView::PartialIteration>" || pi.Inspect() != pi.ToS() || !pi.Truthy() {
		t.Errorf("AVPartialIter value methods: %q / %q / %v", pi.ToS(), pi.Inspect(), pi.Truthy())
	}
	fb := &FormBuilderVal{cls: v.consts["ActionView::Helpers::FormBuilder"].(*RClass), fb: actionview.FormBuilderFor("u", nil)}
	if fb.ToS() != "#<ActionView::Helpers::FormBuilder>" || fb.Inspect() != fb.ToS() || !fb.Truthy() {
		t.Errorf("FormBuilderVal value methods: %q / %q / %v", fb.ToS(), fb.Inspect(), fb.Truthy())
	}
	// The Truthy path through a Ruby conditional.
	avEq(t, v, `("x".html_safe) ? "y" : "n"`, "y")
}

// TestActionViewProvidedFeatures covers the require registration (require returns
// true and the constant is available with no .rb file).
func TestActionViewProvidedFeatures(t *testing.T) {
	v := New(io.Discard)
	avEq(t, v, `require "action_view"`, "true")
	avEq(t, v, `require "actionview"`, "true")
	avEq(t, v, `defined?(ActionView::Base)`, "constant")
}

// containsAV reports whether s contains sub (a tiny helper to keep the form_with
// assertions readable without importing strings in every arm).
func containsAV(s, sub string) bool {
	return len(sub) == 0 || indexAV(s, sub) >= 0
}

func indexAV(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
