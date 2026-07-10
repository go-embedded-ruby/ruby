// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"testing"
)

// confdRun runs a Ruby program with `require "confd"` prepended.
func confdRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"confd\"\n"+body)
}

// TestConfdRender proves Confd.render drives confd's real template engine over an
// in-memory backend seeded from the Ruby vars Hash: nested Hashes flatten into
// "/"-paths, flat and already-"/"-prefixed keys both resolve, non-String values
// stringify, and confd's function map (getv/getvs/exists/base64Encode/json/…) is
// reachable. A nil vars renders against an empty backend.
func TestConfdRender(t *testing.T) {
	got := confdRun(t, `
puts Confd.render('port={{getv "/web/port"}} host={{getv "/web/host"}}', {"web" => {"port" => 80, "host" => "ex.com"}})
puts Confd.render('{{getv "/a"}}-{{getv "/b/c"}}', {"a" => "1", "/b/c" => "2"})
puts Confd.render('{{if exists "/k"}}yes{{else}}no{{end}} {{base64Encode "hi"}}', {"k" => "v"})
puts Confd.render('{{range getvs "/list/*"}}{{.}};{{end}}', {"list" => {"a" => "1", "b" => "2"}})
puts Confd.render("static text", nil)
puts Confd.render("no-vars-arg")
`)
	want := "port=80 host=ex.com\n" +
		"1-2\n" +
		"yes aGk=\n" +
		"1;2;\n" +
		"static text\n" +
		"no-vars-arg"
	if got != want {
		t.Fatalf("render:\n got=%q\nwant=%q", got, want)
	}
}

// TestConfdOptions covers the render options: :prefix (WithPrefix) shifts every
// getv key, :keys (WithKeys) as an Array or a single String scopes the fetched
// prefixes, and :format (WithOutputFormat) validates the rendered document. A
// trailing Hash is only read as options when it carries a recognised option key.
func TestConfdOptions(t *testing.T) {
	got := confdRun(t, `
puts Confd.render('{{getv "/port"}}', {"app" => {"port" => "9"}}, prefix: "/app")
puts Confd.render('{{getv "/web/port"}}', {"web" => {"port" => "7"}}, keys: ["/web"])
puts Confd.render('{{getv "/web/port"}}', {"web" => {"port" => "5"}}, keys: "/web")
puts Confd.render('{"port":{{getv "/port"}}}', {"port" => "8"}, format: "json")
puts Confd.render('{{getv "/greeting"}}', {"greeting" => "data-not-options"})
`)
	want := "9\n7\n5\n{\"port\":8}\ndata-not-options"
	if got != want {
		t.Fatalf("options:\n got=%q\nwant=%q", got, want)
	}
}

// TestConfdErrors covers the failure surface: a missing key and a malformed
// template both raise Confd::Error (a StandardError), a non-String template a
// TypeError, a non-Hash vars a TypeError, and no arguments an ArgumentError.
func TestConfdErrors(t *testing.T) {
	got := confdRun(t, `
begin
  Confd.render('{{getv "/nope"}}', {})
rescue Confd::Error => e
  puts e.class
  puts(e.is_a?(StandardError))
  puts e.message.include?("key does not exist")
end
begin
  Confd.render('{{ getv ', {})
rescue Confd::Error
  puts "syntax"
end
`)
	want := "Confd::Error\ntrue\ntrue\nsyntax"
	if got != want {
		t.Fatalf("errors:\n got=%q\nwant=%q", got, want)
	}
}

// TestConfdArgErrors covers the argument guards: no arguments raises
// ArgumentError, a non-String template and a non-Hash positional vars each raise
// TypeError.
func TestConfdArgErrors(t *testing.T) {
	cases := map[string]string{
		"arg":   "begin; Confd.send(:render); rescue ArgumentError; puts \"ok\"; end",
		"type":  "begin; Confd.render(42); rescue TypeError; puts \"ok\"; end",
		"type2": "begin; Confd.render(\"x\", 5); rescue TypeError; puts \"ok\"; end",
	}
	for name, expr := range cases {
		if got := confdRun(t, expr); got != "ok" {
			t.Fatalf("%s: got=%q, want %q", name, got, "ok")
		}
	}
}

// TestConfdHelpers covers the pure Go seams directly: confdCleanErr strips confd's
// staged-file prefix when present and passes a plain message through untouched,
// and confdOptGet reports false on a nil Hash.
func TestConfdHelpers(t *testing.T) {
	if got := confdCleanErr(errors.New("/tmp/go-ruby-confd-1/rendered.out: template: boom")); got != "template: boom" {
		t.Fatalf("cleanErr strip: got=%q", got)
	}
	if got := confdCleanErr(errors.New("no staged prefix here")); got != "no staged prefix here" {
		t.Fatalf("cleanErr passthrough: got=%q", got)
	}
	if _, ok := confdOptGet(nil, "prefix"); ok {
		t.Fatalf("confdOptGet(nil) should report false")
	}
}
