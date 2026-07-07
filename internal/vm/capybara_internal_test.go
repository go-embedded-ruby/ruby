// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// capyApp is a small Ruby Rack app (a class responding to #call) that serves the
// pages the Session tests drive: a form page with a text field, checkbox, radio
// buttons, a select and a submit button, a linked "next" page, a POST handler
// that echoes the submitted query, and a self-redirecting "/loop" path for the
// infinite-redirect case. It reads the request body from env['rack.input'] and
// parses the url-encoded form, exercising the App -> Ruby-Rack seam end to end.
const capyApp = `
require "capybara"
class TestApp
  def call(env)
    m = env["REQUEST_METHOD"]
    path = env["PATH_INFO"]
    body = env["rack.input"].read
    if m == "GET" && path == "/"
      html = "<html><body>" +
        "<h1 id='title'>Welcome</h1>" +
        "<p class='greeting'>Hello world</p>" +
        "<a href='/next'>Next page</a>" +
        "<form action='/search' method='post'>" +
        "<label for='q'>Query</label>" +
        "<input type='text' name='q' id='q'/>" +
        "<input type='checkbox' name='agree' id='agree'/>" +
        "<input type='radio' name='color' value='red' id='red'/>" +
        "<input type='radio' name='color' value='blue' id='blue'/>" +
        "<select name='size' id='size'>" +
        "<option value='s'>Small</option>" +
        "<option value='l'>Large</option>" +
        "</select>" +
        "<input type='file' name='doc' id='doc'/>" +
        "<button type='submit'>Go</button>" +
        "</form>" +
        "<div id='scope'><span class='msg'>scoped hi</span></div>" +
        "<p class='dup'>one</p><p class='dup'>two</p>" +
        "</body></html>"
      [200, {"Content-Type" => "text/html"}, [html]]
    elsif m == "GET" && path == "/next"
      [200, {"Content-Type" => "text/html"}, ["<html><body><p>On the next page</p></body></html>"]]
    elsif m == "POST" && path == "/search"
      params = {}
      body.split("&").each do |kv|
        k, v = kv.split("=", 2)
        params[k] = v
      end
      q = params["q"] || ""
      [200, {"Content-Type" => "text/html"}, ["<html><body><p id='result'>You searched: " + q + "</p></body></html>"]]
    elsif path == "/loop"
      [302, {"Location" => "/loop"}, [""]]
    else
      [404, {"Content-Type" => "text/html"}, ["<html><body>Not found</body></html>"]]
    end
  end
end
`

// sess is a preamble that registers the app and opens a Session over it.
const capySess = capyApp + `
app = TestApp.new
page = Capybara::Session.new(:rack_test, app)
`

// TestCapybaraRequire covers the module, VERSION and the two require features.
func TestCapybaraRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "capybara"`, "true\n"},
		{`require "capybara"; p require "capybara"`, "false\n"},
		{`p require "capybara/dsl"`, "true\n"},
		{`require "capybara"; p Capybara.is_a?(Module)`, "true\n"},
		{`require "capybara"; puts Capybara::VERSION`, capybaraVersion + "\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapybaraAppAccessor covers Capybara.app= / Capybara.app.
func TestCapybaraAppAccessor(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "capybara"; p Capybara.app`, "nil\n"},
		{capyApp + `p (Capybara.app = TestApp.new).is_a?(TestApp)`, "true\n"},
		{capyApp + `Capybara.app = TestApp.new; p Capybara.app.is_a?(TestApp)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapybaraNavigation covers visit / current_path / current_url / body / html /
// status_code and click navigation (click_link / click_on / Node#click).
func TestCapybaraNavigation(t *testing.T) {
	cases := []struct{ src, want string }{
		{capySess + `page.visit "/"; p page.current_path`, "\"/\"\n"},
		{capySess + `page.visit "/"; p page.current_url`, "\"http://www.example.com/\"\n"},
		{capySess + `page.visit "/"; p page.status_code`, "200\n"},
		{capySess + `page.visit "/"; p page.body.include?("Welcome")`, "true\n"},
		{capySess + `page.visit "/"; p page.html.include?("Welcome")`, "true\n"},
		{capySess + `page.visit "/"; page.click_link "Next page"; p page.current_path`, "\"/next\"\n"},
		{capySess + `page.visit "/"; page.click_on "Next page"; p page.current_path`, "\"/next\"\n"},
		{capySess + `page.visit "/"; page.find_link("Next page").click; p page.current_path`, "\"/next\"\n"},
		{capySess + `page.visit "/missing"; p page.status_code`, "404\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapybaraForms covers fill_in / check / uncheck / choose / select / the form
// submit through click_button, and the resulting POST body parsed by the app.
func TestCapybaraForms(t *testing.T) {
	cases := []struct{ src, want string }{
		{capySess + `page.visit "/"; page.fill_in "q", with: "hello"; page.click_button "Go"; p page.has_content?("You searched: hello")`, "true\n"},
		{capySess + `page.visit "/"; page.check "agree"; p page.find("#agree").checked?`, "true\n"},
		{capySess + `page.visit "/"; page.check "agree"; page.uncheck "agree"; p page.find("#agree").checked?`, "false\n"},
		{capySess + `page.visit "/"; page.choose "red"; p page.find("#red").checked?`, "true\n"},
		{capySess + `page.visit "/"; page.select "Large", from: "size"; p page.find("#size").value`, "\"l\"\n"},
		{capySess + `page.visit "/"; page.fill_in "q", with: "hi"; p page.find_field("q").value`, "\"hi\"\n"},
		{capySess + `page.visit "/"; page.attach_file "doc", "/tmp/x.txt"; p page.find_field("doc").value`, "\"/tmp/x.txt\"\n"},
		{capySess + `page.visit "/"; page.fill_in "q", with: 42; p page.find_field("q").value`, "\"42\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapybaraKwargAbsent covers the fill_in kwarg paths: with a matching key
// (above), with a trailing hash whose key is absent, and with no options hash at
// all — both of which fill the field with "".
func TestCapybaraKwargAbsent(t *testing.T) {
	cases := []struct{ src, want string }{
		{capySess + `page.visit "/"; page.fill_in "q", foo: "x"; p page.find_field("q").value`, "\"\"\n"},
		{capySess + `page.visit "/"; page.fill_in "q"; p page.find_field("q").value`, "\"\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapybaraFinders covers find / all / first / find_field / find_button and
// attach_file, plus the Node accessors (text / value / tag_name / [] present and
// absent / visible? / selected? / set).
func TestCapybaraFinders(t *testing.T) {
	cases := []struct{ src, want string }{
		{capySess + `page.visit "/"; p page.find("#title").text`, "\"Welcome\"\n"},
		{capySess + `page.visit "/"; p page.find("p.greeting").text`, "\"Hello world\"\n"},
		{capySess + `page.visit "/"; p page.find("#title").tag_name`, "\"h1\"\n"},
		{capySess + `page.visit "/"; p page.find("#q")["name"]`, "\"q\"\n"},
		{capySess + `page.visit "/"; p page.find("#q")["data-nope"]`, "nil\n"},
		{capySess + `page.visit "/"; p page.find("#title").visible?`, "true\n"},
		{capySess + `page.visit "/"; p page.find("#red").selected?`, "false\n"},
		{capySess + `page.visit "/"; page.find_field("q").set("typed"); p page.find_field("q").value`, "\"typed\"\n"},
		{capySess + `page.visit "/"; p page.find_button("Go").tag_name`, "\"button\"\n"},
		{capySess + `page.visit "/"; p page.all(".dup").length`, "2\n"},
		{capySess + `page.visit "/"; p page.all(".dup").map(&:text)`, "[\"one\", \"two\"]\n"},
		{capySess + `page.visit "/"; p page.first(".dup").text`, "\"one\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapybaraMatchers covers the positive and negative predicate matchers plus
// the assert_* helpers on success.
func TestCapybaraMatchers(t *testing.T) {
	cases := []struct{ src, want string }{
		{capySess + `page.visit "/"; p page.has_selector?("h1#title")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_css?("p.greeting")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_xpath?("//h1")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_content?("Hello world")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_text?("Welcome")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_link?("Next page")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_button?("Go")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_field?("q")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_no_selector?("h2")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_no_css?("h2")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_no_xpath?("//h2")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_no_content?("Goodbye")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_no_text?("Goodbye")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_no_link?("Prev page")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_no_button?("Stop")`, "true\n"},
		{capySess + `page.visit "/"; p page.has_no_field?("password")`, "true\n"},
		{capySess + `page.visit "/"; p page.assert_selector("h1#title")`, "true\n"},
		{capySess + `page.visit "/"; p page.assert_text("Welcome")`, "true\n"},
		{capySess + `page.visit "/"; p page.assert_no_selector("h2")`, "true\n"},
		{capySess + `page.visit "/"; p page.assert_no_text("Goodbye")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapybaraWithin covers within(scope){ } scoping and the returned block value.
func TestCapybaraWithin(t *testing.T) {
	cases := []struct{ src, want string }{
		{capySess + `page.visit "/"; p(page.within("#scope") { page.find("span.msg").text })`, "\"scoped hi\"\n"},
		{capySess + `page.visit "/"; page.within("#scope") { p page.has_selector?("span.msg") }`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapybaraClassOf covers the classOf cases for the Session / Node wrappers.
func TestCapybaraClassOf(t *testing.T) {
	cases := []struct{ src, want string }{
		{capySess + `p page.class`, "Capybara::Session\n"},
		{capySess + `page.visit "/"; p page.find("#title").class`, "Capybara::Node::Element\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapybaraDSL covers Capybara::DSL: page delegation through method_missing,
// respond_to_missing?, the memoised current_session and its invalidation when
// Capybara.app changes.
func TestCapybaraDSL(t *testing.T) {
	dsl := capyApp + `
require "capybara/dsl"
Capybara.app = TestApp.new
class Driver
  include Capybara::DSL
end
d = Driver.new
`
	cases := []struct{ src, want string }{
		{dsl + `d.visit "/"; p d.has_content?("Hello world")`, "true\n"},
		{dsl + `p d.respond_to?(:visit)`, "true\n"},
		{dsl + `p d.page.equal?(Capybara.current_session)`, "true\n"},
		{dsl + `p Capybara.current_session.equal?(Capybara.current_session)`, "true\n"},
		{dsl + `s1 = Capybara.current_session; Capybara.app = TestApp.new; p s1.equal?(Capybara.current_session)`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapybaraErrors covers the error mapping: ElementNotFound (the finder
// default), Ambiguous, ExpectationNotMet, UnselectableError, InfiniteRedirectError
// and the malformed-triple / driver / missing-app / no-block cases.
func TestCapybaraErrors(t *testing.T) {
	cases := []struct{ src, class string }{
		{capySess + `page.visit "/"; page.find("#missing")`, "Capybara::ElementNotFound"},
		{capySess + `page.visit "/"; page.find(".dup")`, "Capybara::Ambiguous"},
		{capySess + `page.visit "/"; page.assert_selector("h2")`, "Capybara::ExpectationNotMet"},
		{capySess + `page.visit "/"; page.assert_text("Goodbye")`, "Capybara::ExpectationNotMet"},
		{capySess + `page.visit "/"; page.find("#scope").set("x")`, "Capybara::UnselectableError"},
		{capySess + `page.visit "/loop"`, "Capybara::InfiniteRedirectError"},
		{capyApp + `bad = ->(env) { "oops" }; s = Capybara::Session.new(:rack_test, bad); s.visit "/"`, "Capybara::CapybaraError"},
		{capyApp + `Capybara::Session.new(:selenium, TestApp.new)`, "ArgumentError"},
		{`require "capybara"; Capybara::Session.new(:rack_test)`, "ArgumentError"},
		{capySess + `page.visit "/"; page.within("#scope")`, "LocalJumpError"},
		{`require "capybara/dsl"; Capybara.current_session`, "ArgumentError"},
		{capyApp + `Capybara::Session.new(:rack_test, TestApp.new).attach_file("q")`, "ArgumentError"},
	}
	for _, c := range cases {
		class, _ := evalErr(t, c.src)
		if class != c.class {
			t.Errorf("src=%q got class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestCapybaraErrorHierarchy covers that the Capybara error classes descend from
// Capybara::CapybaraError < StandardError, so `rescue Capybara::CapybaraError`
// and `rescue StandardError` both catch them.
func TestCapybaraErrorHierarchy(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "capybara"; p Capybara::ElementNotFound.ancestors.include?(Capybara::CapybaraError)`, "true\n"},
		{`require "capybara"; p Capybara::CapybaraError.ancestors.include?(StandardError)`, "true\n"},
		{capySess + `page.visit "/"; begin; page.find("#missing"); rescue Capybara::CapybaraError => e; p e.is_a?(StandardError); end`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapybaraDefaultAppSession covers Session.new falling back to Capybara.app
// when no explicit app is passed.
func TestCapybaraDefaultAppSession(t *testing.T) {
	src := capyApp + `
Capybara.app = TestApp.new
page = Capybara::Session.new(:rack_test)
page.visit "/"
p page.has_content?("Hello world")
`
	if got := eval(t, src); got != "true\n" {
		t.Errorf("got=%q want=%q", got, "true\n")
	}
}

// TestCapybaraWrapperInspect covers ToS / Inspect / Truthy of the value wrappers.
func TestCapybaraWrapperInspect(t *testing.T) {
	checks := []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{
		&CapybaraSession{},
		&CapybaraNode{},
	}
	wantToS := []string{"#<Capybara::Session>", "#<Capybara::Node::Element>"}
	for i, c := range checks {
		if c.ToS() != wantToS[i] || c.Inspect() != wantToS[i] || !c.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c, c.ToS(), c.Inspect(), c.Truthy())
		}
	}
}
