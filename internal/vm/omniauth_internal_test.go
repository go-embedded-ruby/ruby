// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"

	omniauth "github.com/go-ruby-omniauth/omniauth"
)

// TestOmniAuthWrapperInspect covers ToS / Inspect / Truthy of the OmniAuth value
// wrappers.
func TestOmniAuthWrapperInspect(t *testing.T) {
	checks := []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{
		&OmniAuthBuilder{},
		&OmniAuthConfig{},
		&OmniAuthMockAuth{},
		&OmniAuthStrategy{},
		&OmniAuthHash{},
	}
	want := []string{"#<OmniAuth::Builder>", "#<OmniAuth::Configuration>", "#<OmniAuth::MockAuth>", "#<OmniAuth::Strategy>", "#<OmniAuth::AuthHash>"}
	for i, c := range checks {
		if c.ToS() != want[i] || c.Inspect() != want[i] || !c.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c, c.ToS(), c.Inspect(), c.Truthy())
		}
	}
}

// TestOmniAuthConstants covers the module/class/exception surface and require.
func TestOmniAuthConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "omniauth"`, "true\n"},
		{`require "omniauth"; p require "omniauth"`, "false\n"},
		{`require "omniauth"; p OmniAuth.is_a?(Module)`, "true\n"},
		{`require "omniauth"; p OmniAuth::Builder.class`, "Class\n"},
		{`require "omniauth"; p OmniAuth::Error < StandardError`, "true\n"},
		{`require "omniauth"; p OmniAuth::AuthenticityError < OmniAuth::Error`, "true\n"},
		{`require "omniauth"; p OmniAuth::NoSessionError < OmniAuth::Error`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOmniAuthMockCallback is the headline: a test-mode mock provider callback
// produces the auth hash the downstream app reads.
func TestOmniAuthMockCallback(t *testing.T) {
	src := `require "omniauth"
OmniAuth.config.test_mode = true
OmniAuth.config.mock_auth[:developer] = { uid: "42", info: { name: "Ada" } }
app = ->(env) {
  auth = env["omniauth.auth"]
  [200, {}, ["#{auth.provider}:#{auth.uid}:#{auth.info[:name]}:#{env["omniauth.strategy"]}"]]
}
b = OmniAuth::Builder.new(app) { provider :developer }
s, _, body = b.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/auth/developer/callback", "rack.session" => {} })
puts s
puts body.join
`
	if got := eval(t, src); got != "200\ndeveloper:42:Ada:developer\n" {
		t.Errorf("got=%q", got)
	}
}

// TestOmniAuthMockRequestRedirect covers the test-mode request phase redirecting
// to the callback path.
func TestOmniAuthMockRequestRedirect(t *testing.T) {
	src := `require "omniauth"
OmniAuth.config.test_mode = true
OmniAuth.config.mock_auth[:developer] = { uid: "1" }
b = OmniAuth::Builder.new(->(e){ [200, {}, []] }) { provider :developer }
s, h, _ = b.call({ "REQUEST_METHOD" => "POST", "PATH_INFO" => "/auth/developer", "rack.session" => {} })
puts s
puts h["location"]
`
	if got := eval(t, src); got != "302\n/auth/developer/callback\n" {
		t.Errorf("got=%q", got)
	}
}

// TestOmniAuthMockFailure covers a test-mode mocked failure (a Symbol value)
// taking the failure redirect.
func TestOmniAuthMockFailure(t *testing.T) {
	src := `require "omniauth"
OmniAuth.config.test_mode = true
OmniAuth.config.mock_auth[:developer] = :invalid_credentials
puts OmniAuth.config.mock_auth[:developer]
b = OmniAuth::Builder.new(->(e){ [200, {}, []] }) { provider :developer }
s, h, _ = b.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/auth/developer/callback", "rack.session" => {} })
puts s
puts h["location"]
`
	if got := eval(t, src); got != "invalid_credentials\n302\n/auth/failure?message=invalid_credentials&strategy=developer\n" {
		t.Errorf("got=%q", got)
	}
}

// TestOmniAuthRealProvider covers a registered provider's request-phase redirect
// and its callback assembling an AuthHash from uid/info.
func TestOmniAuthRealProvider(t *testing.T) {
	src := `require "omniauth"
OmniAuth.config.test_mode = false
OmniAuth::Strategies.add(:acme) do
  def request_phase
    redirect("https://acme.test/authorize?client=#{name}")
  end
  def uid; "u-1"; end
  def info; { "email" => "a@b.c" }; end
end
app = ->(e) {
  auth = e["omniauth.auth"]
  [200, {}, ["#{auth.uid}/#{auth.info["email"]}"]]
}
b = OmniAuth::Builder.new(app) { provider :acme }
rs, rh, _ = b.call({ "REQUEST_METHOD" => "POST", "PATH_INFO" => "/auth/acme", "rack.session" => {} })
puts rs
puts rh["location"]
cs, _, cb = b.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/auth/acme/callback", "rack.session" => {} })
puts cs
puts cb.join
puts OmniAuth::Strategies[:acme].class
puts OmniAuth::Strategies[:ghost].inspect
`
	want := "302\nhttps://acme.test/authorize?client=acme\n200\nu-1/a@b.c\nClass\nnil\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q", got)
	}
}

// TestOmniAuthProviderFailures covers the request-phase failure branches:
// fail!, a no-response request phase and an unregistered provider — each taking
// the failure redirect — and a callback fail!.
func TestOmniAuthProviderFailures(t *testing.T) {
	cases := []struct{ add, path, wantMsg string }{
		{`OmniAuth::Strategies.add(:x) { def request_phase; fail!(:bad_token); end }`, "/auth/x", "bad_token"},
		{`OmniAuth::Strategies.add(:x) { def request_phase; nil; end }`, "/auth/x", "invalid_request"},
		{``, "/auth/x", "invalid_strategy"},
		{`OmniAuth::Strategies.add(:x) { def uid; fail!(:bad_creds); nil; end }`, "/auth/x/callback", "bad_creds"},
	}
	for _, c := range cases {
		src := `require "omniauth"
OmniAuth.config.test_mode = false
` + c.add + `
b = OmniAuth::Builder.new(->(e){ [200, {}, []] }) { provider :x }
s, h, _ = b.call({ "REQUEST_METHOD" => "POST", "PATH_INFO" => "` + c.path + `", "rack.session" => {} })
puts s
puts h["location"]
`
		want := "302\n/auth/failure?message=" + c.wantMsg + "&strategy=x\n"
		if got := eval(t, src); got != want {
			t.Errorf("add=%q got=%q want=%q", c.add, got, want)
		}
	}
}

// TestOmniAuthPassthrough covers a non-auth path passing through to the wrapped
// app, and the terminal 404 when no app is wrapped.
func TestOmniAuthPassthrough(t *testing.T) {
	src := `require "omniauth"
OmniAuth.config.test_mode = false
OmniAuth::Strategies.add(:acme) { def request_phase; redirect("/x"); end }
b = OmniAuth::Builder.new(->(e){ [200, {}, ["downstream"]] }) { provider :acme }
s, _, body = b.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/other", "rack.session" => {} })
puts s
puts body.join
b2 = OmniAuth::Builder.new { provider :acme }
s2, _, body2 = b2.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/other", "rack.session" => {} })
puts s2
puts body2.join
`
	if got := eval(t, src); got != "200\ndownstream\n404\nNot Found\n" {
		t.Errorf("got=%q", got)
	}
}

// TestOmniAuthNoSession covers the NoSessionError raise when a request arrives
// without a Rack session.
func TestOmniAuthNoSession(t *testing.T) {
	src := `require "omniauth"
OmniAuth.config.test_mode = false
OmniAuth::Strategies.add(:acme) { def request_phase; redirect("/x"); end }
b = OmniAuth::Builder.new(->(e){ [200, {}, []] }) { provider :acme }
b.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/other" })
`
	class, _ := evalErr(t, src)
	if class != "OmniAuth::NoSessionError" {
		t.Errorf("class=%q", class)
	}
}

// TestOmniAuthConfig covers the OmniAuth.config surface (test_mode / path_prefix
// / mock_auth read/write / add_mock) and to_app returning the builder.
func TestOmniAuthConfig(t *testing.T) {
	src := `require "omniauth"
OmniAuth.config.test_mode = true
puts OmniAuth.config.test_mode
puts OmniAuth.config.test_mode?
OmniAuth.config.path_prefix = "/login"
puts OmniAuth.config.path_prefix
OmniAuth.config.add_mock(:foo, { uid: "9" })
puts OmniAuth.config.mock_auth[:foo].uid
puts OmniAuth.config.mock_auth[:missing].inspect
b = OmniAuth::Builder.new(->(e){ [200, {}, []] })
puts b.to_app.equal?(b)
`
	if got := eval(t, src); got != "true\ntrue\n/login\n9\nnil\ntrue\n" {
		t.Errorf("got=%q", got)
	}
}

// TestOmniAuthAuthHash covers OmniAuth::AuthHash construction and its indifferent
// accessor surface, including the nested info/credentials/extra sub-hashes,
// to_h and valid?.
func TestOmniAuthAuthHash(t *testing.T) {
	src := `require "omniauth"
h = OmniAuth::AuthHash.new({ provider: "acme", uid: "1", info: { name: "Ada" } })
puts h.provider
puts h.uid
puts h.info[:name]
puts h.info["name"]
puts h["uid"]
puts h[:provider]
puts h[:missing].inspect
puts h.key?(:uid)
puts h.valid?
h.credentials["token"] = "t0"
h.extra[:raw] = { a: 1 }
puts h.credentials[:token]
puts h.to_h.inspect
puts OmniAuth::AuthHash.new.valid?
puts OmniAuth::AuthHash.new("not-a-hash").valid?
puts OmniAuth::AuthHash.new(h).uid
e = OmniAuth::AuthHash.new({ info: OmniAuth::AuthHash.new({ name: "x" }) })
puts e.info[:name]
`
	want := "acme\n1\nAda\nAda\n1\nacme\nnil\ntrue\ntrue\nt0\n" +
		`{"provider" => "acme", "uid" => "1", "info" => {"name" => "Ada"}, "credentials" => {"token" => "t0"}, "extra" => {"raw" => {"a" => 1}}}` + "\n" +
		"false\nfalse\n1\nx\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestOmniAuthStrategyHelpers covers the strategy base helpers (env / request /
// session / options / callback_url) and the default identity accessors.
func TestOmniAuthStrategyHelpers(t *testing.T) {
	src := `require "omniauth"
OmniAuth.config.test_mode = false
OmniAuth::Strategies.add(:acme) do
  def request_phase
    redirect("/go?m=#{request.request_method}&p=#{env["PATH_INFO"]}&s=#{session["k"]}&o=#{options.inspect}&cb=#{callback_url}&d=#{uid.inspect}#{info.inspect}#{credentials.inspect}#{extra.inspect}")
  end
end
b = OmniAuth::Builder.new(->(e){ [200, {}, []] }) { provider :acme }
_, h, _ = b.call({ "REQUEST_METHOD" => "POST", "PATH_INFO" => "/auth/acme", "rack.session" => { "k" => "v" } })
puts h["location"]
`
	want := "/go?m=POST&p=/auth/acme&s=v&o={}&cb=/auth/acme/callback&d=nil{}{}{}\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestOmniAuthProviderClass covers provider mounting by Class (with a derived
// snake_case name), by Symbol+Class and with a trailing options Hash, plus the
// anonymous-class fallback name.
func TestOmniAuthProviderClass(t *testing.T) {
	src := `require "omniauth"
OmniAuth.config.test_mode = false
class GoogleOauth2 < OmniAuth::Strategy
  def uid; "g"; end
end
b = OmniAuth::Builder.new(->(e){ [200, {}, [e["omniauth.auth"].uid]] }) { provider GoogleOauth2 }
_, _, body = b.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/auth/google_oauth2/callback", "rack.session" => {} })
puts body.join
kls = Class.new(OmniAuth::Strategy) { def uid; "anon"; end }
b2 = OmniAuth::Builder.new(->(e){ [200, {}, [e["omniauth.auth"].uid]] }) { provider :anon, kls, scope: "email" }
_, _, body2 = b2.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/auth/anon/callback", "rack.session" => {} })
puts body2.join
`
	if got := eval(t, src); got != "g\nanon\n" {
		t.Errorf("got=%q", got)
	}
}

// TestOmniAuthNamespacedClass covers omniProviderName stripping a "::"-qualified
// class name down to its final snake_cased segment.
func TestOmniAuthNamespacedClass(t *testing.T) {
	src := `require "omniauth"
OmniAuth.config.test_mode = false
module Providers
  class FancyThing < OmniAuth::Strategy
    def uid; "ft"; end
  end
end
b = OmniAuth::Builder.new(->(e){ [200, {}, [e["omniauth.auth"].uid]] }) { provider Providers::FancyThing }
_, _, body = b.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/auth/fancy_thing/callback", "rack.session" => {} })
puts body.join
`
	if got := eval(t, src); got != "ft\n" {
		t.Errorf("got=%q", got)
	}
}

// TestOmniAuthArgumentErrors covers the guard clauses.
func TestOmniAuthArgumentErrors(t *testing.T) {
	cases := []struct{ src, class string }{
		{`require "omniauth"; OmniAuth::Builder.new { provider }`, "ArgumentError"},
		{`require "omniauth"; OmniAuth::Strategies.add`, "ArgumentError"},
		{`require "omniauth"; OmniAuth::Strategies.add(:x, 5)`, "TypeError"},
		{`require "omniauth"; OmniAuth.config.add_mock(:x)`, "ArgumentError"},
		{`require "omniauth"; OmniAuth.config.mock_auth.[]=(:x)`, "ArgumentError"},
		{`require "omniauth"; OmniAuth::AuthHash.new.[]=(:x)`, "ArgumentError"},
		{`require "omniauth"
OmniAuth.config.test_mode = false
OmniAuth::Strategies.add(:acme) { def request_phase; redirect; end }
OmniAuth::Builder.new(->(e){[200,{},[]]}) { provider :acme }.call({ "REQUEST_METHOD" => "POST", "PATH_INFO" => "/auth/acme", "rack.session" => {} })`, "ArgumentError"},
		{`require "omniauth"
OmniAuth.config.test_mode = false
OmniAuth::Strategies.add(:acme) { def request_phase; redirect("/x"); end }
OmniAuth::Builder.new(->(e){ "not-a-triple" }) { provider :acme }.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/nope", "rack.session" => {} })`, "TypeError"},
	}
	for _, c := range cases {
		class, _ := evalErr(t, c.src)
		if class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestOmniAuthStrategiesAddClass covers OmniAuth::Strategies.add with a passed
// strategy Class (rather than a block).
func TestOmniAuthStrategiesAddClass(t *testing.T) {
	src := `require "omniauth"
OmniAuth.config.test_mode = false
k = Class.new(OmniAuth::Strategy) { def uid; "kk"; end }
OmniAuth::Strategies.add(:k, k)
b = OmniAuth::Builder.new(->(e){ [200, {}, [e["omniauth.auth"].uid]] }) { provider :k }
_, _, body = b.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/auth/k/callback", "rack.session" => {} })
puts body.join
`
	if got := eval(t, src); got != "kk\n" {
		t.Errorf("got=%q", got)
	}
}

// TestOmniAuthRaiseBranches white-box-covers omniRaise's error-type mapping,
// including the AuthenticityError branch unreachable through the Ruby surface.
func TestOmniAuthRaiseBranches(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{omniauth.NewNoSessionError("no session"), "OmniAuth::NoSessionError"},
		{omniauth.NewAuthenticityError("csrf"), "OmniAuth::AuthenticityError"},
		{omniauth.NewError("k", "generic"), "OmniAuth::Error"},
	}
	for _, c := range cases {
		func() {
			defer func() {
				r := recover()
				re, ok := r.(RubyError)
				if !ok || re.Class != c.want {
					t.Errorf("err=%v recovered=%#v want class %q", c.err, r, c.want)
				}
			}()
			omniRaise(c.err)
		}()
	}
}

// TestOmniAuthAssembleError white-box-covers the assemble path raising when a
// mounted provider was never registered (unreachable via the Ruby provider DSL,
// which always registers a phase).
func TestOmniAuthAssembleError(t *testing.T) {
	strategies := omniauth.NewStrategies()
	builder := omniauth.NewBuilder(omniauth.DefaultConfig(), strategies)
	builder.Provider("ghost", nil)
	b := &OmniAuthBuilder{vm: New(io.Discard), builder: builder, strategies: strategies}
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok || re.Class != "OmniAuth::Error" {
			t.Errorf("recovered=%#v, want OmniAuth::Error", r)
		}
	}()
	b.assemble()
}

// TestOmniAuthCoverageExtra covers the remaining branches: the provider/uid
// readers when unset, to_hash, reading InfoHash/AuthHash sub-hashes back through
// [], a provider carrying option args surfaced as #options, a keyless fail!
// defaulting its message, and the anonymous-class provider name fallback.
func TestOmniAuthCoverageExtra(t *testing.T) {
	// empty provider/uid readers + to_hash.
	if got := eval(t, `require "omniauth"
puts OmniAuth::AuthHash.new.provider.inspect
puts OmniAuth::AuthHash.new.uid.inspect
puts OmniAuth::AuthHash.new({ uid: "1" }).to_hash.inspect`); got != "nil\nnil\n{\"uid\" => \"1\"}\n" {
		t.Errorf("empty/to_hash got=%q", got)
	}

	// sub-hashes read back through [] exercise omniValueToRuby's InfoHash/AuthHash.
	subs := `require "omniauth"
h = OmniAuth::AuthHash.new({ uid: "1" })
h.info[:x] = "y"
h.credentials[:t] = "z"
puts h[:info].class
puts h[:info][:x]
puts h[:credentials][:t]`
	if got := eval(t, subs); got != "OmniAuth::AuthHash\ny\nz\n" {
		t.Errorf("subs got=%q", got)
	}

	// a provider carrying an option arg, surfaced as #options.
	opt := `require "omniauth"
OmniAuth.config.test_mode = false
OmniAuth::Strategies.add(:opt) { def request_phase; redirect("/o?s=#{options["scope"]}"); end }
b = OmniAuth::Builder.new(->(e){ [200, {}, []] }) { provider :opt, scope: "email" }
_, h, _ = b.call({ "REQUEST_METHOD" => "POST", "PATH_INFO" => "/auth/opt", "rack.session" => {} })
puts h["location"]`
	if got := eval(t, opt); got != "/o?s=email\n" {
		t.Errorf("opt got=%q", got)
	}

	// a keyless fail! defaults its message key.
	nokey := `require "omniauth"
OmniAuth.config.test_mode = false
OmniAuth::Strategies.add(:nokey) { def request_phase; fail!; end }
b = OmniAuth::Builder.new(->(e){ [200, {}, []] }) { provider :nokey }
_, h, _ = b.call({ "REQUEST_METHOD" => "POST", "PATH_INFO" => "/auth/nokey", "rack.session" => {} })
puts h["location"]`
	if got := eval(t, nokey); got != "/auth/failure?message=invalid_credentials&strategy=nokey\n" {
		t.Errorf("nokey got=%q", got)
	}

	// an anonymous strategy class mounted directly derives the "strategy" name.
	anon := `require "omniauth"
OmniAuth.config.test_mode = false
kls = Class.new(OmniAuth::Strategy) { def uid; "an"; end }
b = OmniAuth::Builder.new(->(e){ [200, {}, [e["omniauth.auth"].uid]] }) { provider kls }
_, _, body = b.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/auth/strategy/callback", "rack.session" => {} })
puts body.join`
	if got := eval(t, anon); got != "an\n" {
		t.Errorf("anon got=%q", got)
	}
}
