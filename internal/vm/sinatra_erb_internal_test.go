// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	erb "github.com/go-ruby-erb/erb"
)

// sinatraErbApp wraps a single route body in a Sinatra::Base app and dispatches
// GET path through the Rack #call adapter, returning the joined response body.
// The route runs against a SinatraCtx, so `erb` inside it exercises the whole
// bridge (compile via go-ruby-erb, evaluate in the handler's binding).
func sinatraErbApp(route, path, query string) string {
	return `require "sinatra/base"
class A < Sinatra::Base
  disable :protection
  set :host_authorization, { permitted_hosts: [] }
  before { @n = 3; @who = "World" }
` + route + `
end
_, _, b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"` + path + `","QUERY_STRING"=>"` + query + `","rack.url_scheme"=>"http","HTTP_HOST"=>"ex")
print b.join`
}

// TestSinatraErbInlineRender covers the inline-String render paths: <%= %>
// interpolation of a query param and an @ivar set in a before-filter, <% %>
// control flow, and ERB trim behaviour — the core of sinatraErb / sinatraErbEval.
func TestSinatraErbInlineRender(t *testing.T) {
	cases := []struct{ route, path, query, want string }{
		// The headline: param + @ivar interpolation.
		{`get("/inline"){ erb "<h1>Hello <%= params['name'] %> (<%= @n %> visits)</h1>" }`,
			"/inline", "name=amy", "<h1>Hello amy (3 visits)</h1>"},
		// Control flow, single line (trim-neutral).
		{`get("/loop"){ erb "<ul><% %w[a b c].each do |x| %><li><%= x %></li><% end %></ul>" }`,
			"/loop", "", "<ul><li>a</li><li>b</li><li>c</li></ul>"},
		// Trim: code-only lines standing alone have their surrounding newlines cut.
		{`get("/trim"){ erb "line1\n<% if true %>\nkept\n<% end %>\nline2" }`,
			"/trim", "", "line1\nkept\nline2"},
	}
	for _, c := range cases {
		if got := runFS(t, sinatraErbApp(c.route, c.path, c.query)); got != c.want {
			t.Errorf("route %q: got=%q want=%q", c.route, got, c.want)
		}
	}
}

// TestSinatraErbLocals covers sinatraErbLocals' collection arms: positional
// locals, options[:locals], a positional entry overriding an options[:locals]
// entry of the same name, and the non-Hash argument branches (options / locals
// that are not Hashes are ignored, matching the "no locals bound" outcome).
func TestSinatraErbLocals(t *testing.T) {
	cases := []struct{ route, path, query, want string }{
		// Positional locals (3rd argument).
		{`get("/x"){ erb "<p><%= greeting %>, <%= who %>!</p>", {}, greeting: "Hi", who: params['name'] }`,
			"/x", "name=bob", "<p>Hi, bob!</p>"},
		// options[:locals].
		{`get("/x"){ erb "<p><%= who %></p>", locals: { who: params['name'] } }`,
			"/x", "name=eve", "<p>eve</p>"},
		// A positional local overrides an options[:locals] local of the same name.
		{`get("/x"){ erb "<p><%= who %></p>", { locals: { who: "opt" } }, who: "pos" }`,
			"/x", "", "<p>pos</p>"},
		// options[:locals] whose value is not a Hash is ignored (no locals bound).
		{`get("/x"){ erb "ok", locals: 5 }`, "/x", "", "ok"},
		// A non-Hash options argument is ignored.
		{`get("/x"){ erb "ok", "notahash" }`, "/x", "", "ok"},
		// A non-Hash positional locals argument is ignored.
		{`get("/x"){ erb "ok", {}, "notahash" }`, "/x", "", "ok"},
	}
	for _, c := range cases {
		if got := runFS(t, sinatraErbApp(c.route, c.path, c.query)); got != c.want {
			t.Errorf("route %q: got=%q want=%q", c.route, got, c.want)
		}
	}
}

// TestSinatraErbSymbolView covers the :symbol template path: an ERB file under the
// app's :views directory is read (through the File binding) and rendered against
// the handler's binding (an @ivar plus positional locals).
func TestSinatraErbSymbolView(t *testing.T) {
	dir := slash(t.TempDir())
	view := "<h2><%= greeting %> <%= @who %></h2>\n<ul>\n<% items.each do |i| %>\n  <li><%= i %></li>\n<% end %>\n</ul>\n"
	if err := os.WriteFile(filepath.FromSlash(dir+"/card.erb"), []byte(view), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `require "sinatra/base"
class A < Sinatra::Base
  disable :protection
  set :host_authorization, { permitted_hosts: [] }
  set :views, "` + dir + `"
  before { @who = "World" }
  get("/v"){ erb :card, {}, greeting: "Hey", items: %w[x y] }
end
_, _, b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/v","QUERY_STRING"=>"","rack.url_scheme"=>"http","HTTP_HOST"=>"ex")
print b.join`
	want := "<h2>Hey World</h2>\n<ul>\n  <li>x</li>\n  <li>y</li>\n</ul>\n"
	if got := runFS(t, src); got != want {
		t.Errorf("symbol view: got=%q want=%q", got, want)
	}
}

// TestSinatraErbViewsDirResolution covers sinatraErbReadView's views-directory
// resolution branches: the default "./views" when :views is unset, and the
// fallback to the default when :views is set to a non-String value. Both resolve
// to a missing file, so File.read raises Errno::ENOENT, proving the path used.
func TestSinatraErbViewsDirResolution(t *testing.T) {
	// No :views set -> default "./views" (the file does not exist -> ENOENT).
	noViews := `require "sinatra/base"
class A < Sinatra::Base
  disable :protection
  set :host_authorization, { permitted_hosts: [] }
  get("/v"){ begin; erb :nope_missing_view; rescue => e; halt 500, e.class.to_s; end }
end
s, _, b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/v","QUERY_STRING"=>"","rack.url_scheme"=>"http","HTTP_HOST"=>"ex")
print "#{s}:#{b.join}"`
	if got := runFS(t, noViews); got != "500:Errno::ENOENT" {
		t.Errorf("default views dir: got=%q want %q", got, "500:Errno::ENOENT")
	}

	// :views set to a non-String (Integer) -> also falls back to the default dir.
	nonStr := `require "sinatra/base"
class A < Sinatra::Base
  disable :protection
  set :host_authorization, { permitted_hosts: [] }
  set :views, 42
  get("/v"){ begin; erb :nope_missing_view; rescue => e; halt 500, e.class.to_s; end }
end
s, _, b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/v","QUERY_STRING"=>"","rack.url_scheme"=>"http","HTTP_HOST"=>"ex")
print "#{s}:#{b.join}"`
	if got := runFS(t, nonStr); got != "500:Errno::ENOENT" {
		t.Errorf("non-string views dir: got=%q want %q", got, "500:Errno::ENOENT")
	}
}

// TestSinatraErbArgErrors covers the argument-validation branches: `erb` with no
// arguments, and a template that is neither a String nor a Symbol.
func TestSinatraErbArgErrors(t *testing.T) {
	cases := []struct{ route, want string }{
		{`get("/x"){ erb }`, "ArgumentError"},
		{`get("/x"){ erb 42 }`, "ArgumentError"},
	}
	for _, c := range cases {
		src := `require "sinatra/base"
class A < Sinatra::Base
  disable :protection
  set :host_authorization, { permitted_hosts: [] }
  ` + c.route + `
end
begin
  A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x","QUERY_STRING"=>"","rack.url_scheme"=>"http","HTTP_HOST"=>"ex")
rescue => e
  print e.class
end`
		if got := runFS(t, src); got != c.want {
			t.Errorf("route %q: got=%q want=%q", c.route, got, c.want)
		}
	}
}

// TestSinatraErbTemplateEvalErrors covers sinatraErbEval's two failure arms: a
// template whose embedded Ruby is malformed (parse error) and one whose embedded
// Ruby parses but does not compile (a bare `retry`), both surfaced as SyntaxError.
func TestSinatraErbTemplateEvalErrors(t *testing.T) {
	cases := []struct{ route, want string }{
		{`get("/x"){ erb "<% ( %>" }`, "SyntaxError"},     // parse failure
		{`get("/x"){ erb "<% retry %>" }`, "SyntaxError"}, // parses, fails to compile
	}
	for _, c := range cases {
		src := `require "sinatra/base"
class A < Sinatra::Base
  disable :protection
  set :host_authorization, { permitted_hosts: [] }
  ` + c.route + `
end
begin
  A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x","QUERY_STRING"=>"","rack.url_scheme"=>"http","HTTP_HOST"=>"ex")
rescue Exception => e
  print e.class
end`
		if got := runFS(t, src); got != c.want {
			t.Errorf("route %q: got=%q want=%q", c.route, got, c.want)
		}
	}
}

// TestSinatraErbCompileError covers sinatraErb's otherwise-unreachable
// compile-error branch: go-ruby-erb never fails on a well-formed template, so the
// failure is injected through the erbCompile seam and must surface as a Ruby
// ArgumentError carrying the error's message.
func TestSinatraErbCompileError(t *testing.T) {
	saved := erbCompile
	defer func() { erbCompile = saved }()
	erbCompile = func(string, erb.Options) (string, string, error) {
		return "", "", errors.New("boom")
	}
	src := `require "sinatra/base"
class A < Sinatra::Base
  disable :protection
  set :host_authorization, { permitted_hosts: [] }
  get("/x"){ erb "anything" }
end
begin
  A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x","QUERY_STRING"=>"","rack.url_scheme"=>"http","HTTP_HOST"=>"ex")
rescue => e
  print [e.class, e.message].inspect
end`
	if got := runFS(t, src); got != `[ArgumentError, "boom"]` {
		t.Errorf("compile-error injection: got=%q", got)
	}
}
