// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"
)

// TestWardenWrapperInspect covers ToS / Inspect / Truthy of the Warden value
// wrappers.
func TestWardenWrapperInspect(t *testing.T) {
	checks := []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{
		&WardenManager{},
		&WardenProxy{},
		&WardenStrategy{},
	}
	want := []string{"#<Warden::Manager>", "#<Warden::Proxy>", "#<Warden::Strategy>"}
	for i, c := range checks {
		if c.ToS() != want[i] || c.Inspect() != want[i] || !c.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c, c.ToS(), c.Inspect(), c.Truthy())
		}
	}
}

// TestWardenConstants covers the module/class surface and require feature.
func TestWardenConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "warden"`, "true\n"},
		{`require "warden"; p require "warden"`, "false\n"},
		{`require "warden"; p Warden.is_a?(Module)`, "true\n"},
		{`require "warden"; p Warden::Manager.class`, "Class\n"},
		{`require "warden"; p Warden::NotAuthenticated < StandardError`, "true\n"},
		{`require "warden"; p Warden::UnknownStrategy < StandardError`, "true\n"},
		{`require "warden"; p Warden::Strategies::Base.class`, "Class\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// wardenPreamble registers a :password strategy and a Rack app that authenticates.
const wardenPreamble = `require "warden"
Warden::Strategies.add(:password) do
  def valid?
    params["username"] && params["password"]
  end
  def authenticate!
    if params["password"] == "secret"
      success!({ name: params["username"] })
    else
      fail!("bad password")
    end
  end
end
APP = ->(env) {
  user = env["warden"].authenticate!(:password)
  [200, { "Content-Type" => "text/plain" }, ["hello #{user[:name]}"]]
}
def env_for(q)
  { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "QUERY_STRING" => q, "rack.session" => {} }
end
`

// TestWardenAuthenticateSuccess is the headline: a strategy authenticating a
// request yields the app's 200 response.
func TestWardenAuthenticateSuccess(t *testing.T) {
	src := wardenPreamble + `
m = Warden::Manager.new(APP) { |mgr| mgr.default_strategies :password }
s, h, b = m.call(env_for("username=amy&password=secret"))
puts s
puts b.join
puts h["content-type"]
puts m.default_strategies.inspect
`
	if got := eval(t, src); got != "200\nhello amy\ntext/plain\n[:password]\n" {
		t.Errorf("got=%q", got)
	}
}

// TestWardenFailureApp covers a throw :warden dispatched to a failure app, which
// reads the throw options out of the env.
func TestWardenFailureApp(t *testing.T) {
	src := wardenPreamble + `
fail_app = ->(e) { [401, {}, ["denied: #{e["warden.options"][:message]} (#{e["warden.options"][:action]})"]] }
m = Warden::Manager.new(APP) { |mgr| mgr.default_strategies :password; mgr.failure_app = fail_app }
s, _, b = m.call(env_for("username=amy&password=wrong"))
puts s
puts b.join
puts m.failure_app.nil?
`
	if got := eval(t, src); got != "401\ndenied: bad password (unauthenticated)\nfalse\n" {
		t.Errorf("got=%q", got)
	}
}

// TestWardenNotAuthenticated covers the no-failure-app raise.
func TestWardenNotAuthenticated(t *testing.T) {
	src := wardenPreamble + `
m = Warden::Manager.new(APP) { |mgr| mgr.default_strategies :password }
m.call(env_for("username=amy&password=wrong"))
`
	class, _ := evalErr(t, src)
	if class != "Warden::NotAuthenticated" {
		t.Errorf("class=%q", class)
	}
}

// TestWardenStrategyRaise covers a strategy raising a Ruby error, which unwinds
// past the throw catch and re-raises out of #call.
func TestWardenStrategyRaise(t *testing.T) {
	src := `require "warden"
Warden::Strategies.add(:boom) { def authenticate!; raise "kaboom"; end }
app = ->(env) { env["warden"].authenticate!(:boom); [200, {}, []] }
m = Warden::Manager.new(app) { |mgr| mgr.default_strategies :boom }
m.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })
`
	class, msg := evalErr(t, src)
	if class != "RuntimeError" || msg != "kaboom" {
		t.Errorf("class=%q msg=%q", class, msg)
	}
}

// TestWardenProxyLifecycle covers set_user / user / authenticated? /
// unauthenticated? / logout / winning_strategy / message directly.
func TestWardenProxyLifecycle(t *testing.T) {
	src := `require "warden"
app = ->(env) {
  w = env["warden"]
  w.set_user({ name: "bob" }, scope: :user)
  out = []
  out << w.authenticated?(:user)
  out << w.unauthenticated?(:user)
  out << w.user(:user)[:name]
  out << w.winning_strategy
  out << w.message
  out << w.authenticate                  # no strategies -> nil
  out << w.user                          # default scope, unset -> nil
  out << w.user({})                      # hash arg -> default scope -> nil
  out << w.user(nil)                     # explicit nil scope -> nil
  w.set_user({ name: "eve" }, scope: :api, store: false)
  w.logout(:user)
  out << w.authenticated?(:user)
  w.logout                               # reset every scope
  [200, {}, [out.inspect]]
}
m = Warden::Manager.new(app)
_, _, b = m.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })
puts b.join
`
	want := "[true, false, \"bob\", nil, \"\", nil, nil, nil, nil, false]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q", got)
	}
}

// TestWardenWinningStrategyMessage covers winning_strategy / message reported
// after a strategy runs and records a message.
func TestWardenWinningStrategyMessage(t *testing.T) {
	src := wardenPreamble + `
app = ->(env) {
  w = env["warden"]
  u = w.authenticate(:password)
  [200, {}, ["#{u[:name]} via #{w.winning_strategy}"]]
}
m = Warden::Manager.new(app)
_, _, b = m.call(env_for("username=zoe&password=secret"))
puts b.join
`
	if got := eval(t, src); got != "zoe via password\n" {
		t.Errorf("got=%q", got)
	}
}

// TestWardenStrategyChain covers pass, the non-bang fail (both continue), an
// invalid strategy (valid? false, skipped) and a final success, plus success!'s
// optional message argument and the winning message.
func TestWardenStrategyChain(t *testing.T) {
	src := `require "warden"
Warden::Strategies.add(:skip)    { def authenticate!; pass; end }
Warden::Strategies.add(:invalid) { def valid?; false; end; def authenticate!; success!("never"); end }
Warden::Strategies.add(:soft)    { def authenticate!; fail("soft-no"); end }
Warden::Strategies.add(:win)     { def authenticate!; success!("winner", "welcome"); end }
app = ->(env) {
  w = env["warden"]
  u = w.authenticate!(:skip, :invalid, :soft, :win)
  [200, {}, ["#{u} / #{w.message}"]]
}
m = Warden::Manager.new(app)
_, _, b = m.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })
puts b.join
`
	if got := eval(t, src); got != "winner / welcome\n" {
		t.Errorf("got=%q", got)
	}
}

// TestWardenRedirectAndCustom covers redirect! (permanent + params + message) and
// custom!, both dispatched as the winning response on a throw.
func TestWardenRedirectAndCustom(t *testing.T) {
	redir := `require "warden"
Warden::Strategies.add(:redir) do
  def authenticate!
    redirect!("/login", { ref: "x" }, permanent: true, message: "please")
  end
end
app = ->(env) { env["warden"].authenticate!(:redir); [200, {}, []] }
m = Warden::Manager.new(app)
s, h, _ = m.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })
puts s
puts h["location"]
`
	if got := eval(t, redir); got != "301\n/login?ref=x\n" {
		t.Errorf("redir got=%q", got)
	}

	plain := `require "warden"
Warden::Strategies.add(:plainredir) { def authenticate!; redirect!("/go"); end }
app = ->(env) { env["warden"].authenticate!(:plainredir); [200, {}, []] }
s, h, _ = Warden::Manager.new(app).call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })
puts s
puts h["location"]
`
	if got := eval(t, plain); got != "302\n/go\n" {
		t.Errorf("plain redir got=%q", got)
	}

	cust := `require "warden"
Warden::Strategies.add(:cust) { def authenticate!; custom!([203, { "X-A" => "y" }, ["custom-body"]]); end }
app = ->(env) { env["warden"].authenticate!(:cust); [200, {}, []] }
s, h, b = Warden::Manager.new(app).call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })
puts s
puts h["x-a"]
puts b.join
`
	if got := eval(t, cust); got != "203\ny\ncustom-body\n" {
		t.Errorf("custom got=%q", got)
	}
}

// TestWardenIntercept401 covers a downstream 401 intercepted as a failure.
func TestWardenIntercept401(t *testing.T) {
	src := `require "warden"
app = ->(env) { [401, {}, ["nope"]] }
m = Warden::Manager.new(app) { |mgr| mgr.intercept_401 = true; mgr.failure_app = ->(e) { [418, {}, ["teapot"]] } }
s, _, b = m.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })
puts s
puts b.join
`
	if got := eval(t, src); got != "418\nteapot\n" {
		t.Errorf("got=%q", got)
	}
}

// TestWardenScopeDefaults covers default_scope (opts + setter + reader) and
// scope_defaults driving per-scope strategies.
func TestWardenScopeDefaults(t *testing.T) {
	src := wardenPreamble + `
m = Warden::Manager.new(APP, default_scope: :user) do |mgr|
  mgr.scope_defaults(:user, strategies: [:password])
end
puts m.default_scope
m.default_scope = :admin
puts m.default_scope
s, _, b = Warden::Manager.new(APP) { |mgr| mgr.scope_defaults(:default, strategies: [:password]) }.call(env_for("username=q&password=secret"))
puts s
puts b.join
`
	if got := eval(t, src); got != "user\nadmin\n200\nhello q\n" {
		t.Errorf("got=%q", got)
	}
}

// TestWardenDefaultScopeReaderDefault covers default_scope's reader default when
// no scope was set.
func TestWardenDefaultScopeReaderDefault(t *testing.T) {
	src := `require "warden"
puts Warden::Manager.new(->(e){[200,{},[]]}).default_scope
`
	if got := eval(t, src); got != "default\n" {
		t.Errorf("got=%q", got)
	}
}

// TestWardenStrategyRegistry covers Strategies.add (class form + wrong type +
// arity), [] and clear!, plus the unregistered-strategy run.
func TestWardenStrategyRegistry(t *testing.T) {
	src := `require "warden"
k = Class.new(Warden::Strategies::Base) do
  def authenticate!; success!("classy"); end
end
Warden::Strategies.add(:classy, k)
app = ->(env) { u = env["warden"].authenticate!(:classy); [200, {}, [u]] }
_, _, b = Warden::Manager.new(app).call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })
puts b.join
puts Warden::Strategies[:classy].class
puts Warden::Strategies[:nope].inspect
Warden::Strategies.clear!
puts Warden::Strategies[:classy].inspect
`
	if got := eval(t, src); got != "classy\nClass\nnil\nnil\n" {
		t.Errorf("got=%q", got)
	}
}

// TestWardenStrategyBaseAccessors covers env / request / session read from a
// strategy body.
func TestWardenStrategyBaseAccessors(t *testing.T) {
	src := `require "warden"
Warden::Strategies.add(:inspector) do
  def valid?
    request.request_method == "GET"
  end
  def authenticate!
    if session["known"] && env["HTTP_X"] == "1"
      success!("ok")
    else
      fail!("no")
    end
  end
end
app = ->(env) { u = env["warden"].authenticate!(:inspector); [200, {}, [u]] }
env = { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "HTTP_X" => "1", "rack.session" => { "known" => true } }
_, _, b = Warden::Manager.new(app).call(env)
puts b.join
`
	if got := eval(t, src); got != "ok\n" {
		t.Errorf("got=%q", got)
	}
}

// TestWardenArgumentErrors covers the ArgumentError / TypeError guards.
func TestWardenArgumentErrors(t *testing.T) {
	cases := []struct{ src, class string }{
		{`require "warden"; Warden::Manager.new`, "ArgumentError"},
		{`require "warden"; Warden::Strategies.add`, "ArgumentError"},
		{`require "warden"; Warden::Strategies.add(:x, 5)`, "TypeError"},
		{`require "warden"; Warden::Manager.new(->(e){[200,{},[]]}).call(5)`, "TypeError"},
		{`require "warden"
Warden::Strategies.add(:noargs) { def authenticate!; redirect!; end }
app = ->(env) { env["warden"].authenticate!(:noargs); [200, {}, []] }
Warden::Manager.new(app).call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })`, "ArgumentError"},
		{`require "warden"
Warden::Strategies.add(:badcustom) { def authenticate!; custom!("not-a-triple"); end }
app = ->(env) { env["warden"].authenticate!(:badcustom); [200, {}, []] }
Warden::Manager.new(app).call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })`, "TypeError"},
		{`require "warden"
app = ->(env) { env["warden"].set_user; [200, {}, []] }
Warden::Manager.new(app).call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })`, "ArgumentError"},
		{`require "warden"
app = ->(env) { env["warden"]; [200, {}, []] }
Warden::Manager.new(app) { |m| m.scope_defaults }.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })`, "ArgumentError"},
	}
	for _, c := range cases {
		class, _ := evalErr(t, c.src)
		if class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestWardenParamsAuthOptionsHash covers authenticate! with an explicit
// strategies:/scope: options hash.
func TestWardenParamsAuthOptionsHash(t *testing.T) {
	src := wardenPreamble + `
app = ->(env) {
  u = env["warden"].authenticate!(scope: :user, strategies: [:password])
  [200, {}, [u[:name]]]
}
_, _, b = Warden::Manager.new(APP) { |m| m.scope_defaults(:user, strategies: [:password]) }.call(env_for("username=k&password=secret"))
_, _, b2 = Warden::Manager.new(app).call(env_for("username=k&password=secret"))
puts b.join
puts b2.join
`
	if got := eval(t, src); got != "hello k\nk\n" {
		t.Errorf("got=%q", got)
	}
}

// TestWardenParamsError covers the params accessor raising on a malformed query.
func TestWardenParamsError(t *testing.T) {
	src := `require "warden"
Warden::Strategies.add(:badparams) { def authenticate!; params; success!("x"); end }
app = ->(env) { env["warden"].authenticate!(:badparams); [200, {}, []] }
env = { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "QUERY_STRING" => "%", "rack.session" => {} }
Warden::Manager.new(app).call(env)
`
	class, _ := evalErr(t, src)
	if !strings.Contains(class, "Error") {
		t.Errorf("class=%q, want an error", class)
	}
}

// TestWardenCoverageExtra covers the remaining branches: the failure_app reader
// when unset, build wiring a default scope + per-scope strategies, an
// unregistered strategy label, and the strategy halted?/message readers.
func TestWardenCoverageExtra(t *testing.T) {
	// failure_app reader, unset.
	if got := eval(t, `require "warden"
puts Warden::Manager.new(->(e){[200,{},[]]}).failure_app.inspect`); got != "nil\n" {
		t.Errorf("failure_app unset got=%q", got)
	}

	// build with a default scope + scope_defaults, actually served.
	scoped := `require "warden"
Warden::Strategies.add(:password) do
  def valid?; params["p"]; end
  def authenticate!; params["p"] == "ok" ? success!("u") : fail!("no"); end
end
app = ->(env) { u = env["warden"].authenticate!; [200, {}, [u.to_s]] }
m = Warden::Manager.new(app, default_scope: :user) { |mgr| mgr.scope_defaults(:user, strategies: [:password]) }
_, _, b = m.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "QUERY_STRING" => "p=ok", "rack.session" => {} })
puts b.join`
	if got := eval(t, scoped); got != "u\n" {
		t.Errorf("scoped got=%q", got)
	}

	// strategy halted? / message readers, observed through a failure app.
	probe := `require "warden"
$probe = nil
Warden::Strategies.add(:probe) do
  def authenticate!
    fail!("msg1")
    $probe = "#{halted?}:#{message}"
  end
end
app = ->(env) { env["warden"].authenticate!(:probe); [200, {}, []] }
m = Warden::Manager.new(app) { |mgr| mgr.failure_app = ->(e) { [200, {}, [$probe]] } }
_, _, b = m.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })
puts b.join`
	if got := eval(t, probe); got != "true:msg1\n" {
		t.Errorf("probe got=%q", got)
	}

	// an unregistered strategy label runs to a no-user throw -> NotAuthenticated.
	ghost := `require "warden"
app = ->(env) { env["warden"].authenticate!(:ghost); [200, {}, []] }
Warden::Manager.new(app).call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "rack.session" => {} })`
	if class, _ := evalErr(t, ghost); class != "Warden::NotAuthenticated" {
		t.Errorf("ghost class=%q", class)
	}
}
