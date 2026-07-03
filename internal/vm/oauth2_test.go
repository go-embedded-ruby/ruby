// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"

	oauth2 "github.com/go-ruby-oauth2/oauth2"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestOAuth2Constants covers the module wiring, require idempotence and the error
// class.
func TestOAuth2Constants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "oauth2"; p OAuth2.is_a?(Module)`, "true\n"},
		{`p require "oauth2"`, "true\n"},
		{`require "oauth2"; p require "oauth2"`, "false\n"},
		{`require "oauth2"; p OAuth2::Error < StandardError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOAuth2Client covers Client.new (with every recognised option), the id /
// authorize_url / token_url readers, and the grant-strategy accessors.
func TestOAuth2Client(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "oauth2"; c = OAuth2::Client.new("id", "sec", site: "https://ex.com"); p c.id`, "\"id\"\n"},
		{`require "oauth2"; c = OAuth2::Client.new("id", "sec", site: "https://ex.com"); p c.token_url`, "\"https://ex.com/oauth/token\"\n"},
		{`require "oauth2"; c = OAuth2::Client.new("id", "sec", site: "https://ex.com"); p c.authorize_url`, "\"https://ex.com/oauth/authorize\"\n"},
		// Every option key.
		{`require "oauth2"
c = OAuth2::Client.new("id", "sec", site: "https://ex.com", authorize_url: "/a", token_url: "/t", auth_scheme: "request_body", token_method: "get")
p c.token_url`, "\"https://ex.com/t\"\n"},
		{`require "oauth2"; c = OAuth2::Client.new("id", "sec"); p c.auth_code.is_a?(OAuth2::Strategy::AuthCode)`, "true\n"},
		{`require "oauth2"; c = OAuth2::Client.new("id", "sec"); p c.password.is_a?(OAuth2::Strategy::Password)`, "true\n"},
		{`require "oauth2"; c = OAuth2::Client.new("id", "sec"); p c.client_credentials.is_a?(OAuth2::Strategy::ClientCredentials)`, "true\n"},
		{`require "oauth2"; c = OAuth2::Client.new("id", "sec"); p c.refresh.is_a?(OAuth2::Strategy::Refresh)`, "true\n"},
		{`require "oauth2"; c = OAuth2::Client.new("id", "sec"); p c.assertion.is_a?(OAuth2::Strategy::Assertion)`, "true\n"},
		// A client with no options still has a usable token endpoint (relative).
		{`require "oauth2"; c = OAuth2::Client.new("id", "sec"); p c.to_s.include?("Client")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOAuth2Strategies covers authorize_url (auth_code) and get_token request
// building for every grant.
func TestOAuth2Strategies(t *testing.T) {
	pre := `require "oauth2"
c = OAuth2::Client.new("id", "sec", site: "https://ex.com")
`
	cases := []struct{ src, want string }{
		{pre + `p c.auth_code.authorize_url(redirect_uri: "https://cb").start_with?("https://ex.com/oauth/authorize")`, "true\n"},
		{pre + `req = c.auth_code.get_token("thecode"); p req.method`, "\"POST\"\n"},
		{pre + `req = c.auth_code.get_token("thecode"); p req.url`, "\"https://ex.com/oauth/token\"\n"},
		{pre + `req = c.auth_code.get_token("thecode"); p req.body.include?("code=thecode")`, "true\n"},
		{pre + `req = c.password.get_token("bob", "pw"); p req.body.include?("username=bob")`, "true\n"},
		{pre + `req = c.client_credentials.get_token; p req.body.include?("grant_type=client_credentials")`, "true\n"},
		{pre + `req = c.refresh.get_token("rtok"); p req.body.include?("refresh_token=rtok")`, "true\n"},
		{pre + `req = c.assertion.get_token("urn:grant", "assn"); p req.body.include?("assertion=assn")`, "true\n"},
		// get_token with extra params (trailing Hash).
		{pre + `req = c.auth_code.get_token("code", scope: "read"); p req.body.include?("scope=read")`, "true\n"},
		// Request accessor surface.
		{pre + `req = c.auth_code.get_token("code"); p req.params.is_a?(Hash)`, "true\n"},
		{pre + `req = c.auth_code.get_token("code"); p req.headers.is_a?(Hash)`, "true\n"},
		{pre + `p c.auth_code.token_url`, "\"https://ex.com/oauth/token\"\n"},
		// authorize_url on a non-auth_code strategy raises NoMethodError.
		{pre + `begin; c.password.authorize_url; rescue NoMethodError; puts "nm"; end`, "nm\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOAuth2AccessToken covers AccessToken.new / .from_hash and the token
// accessors plus refresh request building.
func TestOAuth2AccessToken(t *testing.T) {
	pre := `require "oauth2"
c = OAuth2::Client.new("id", "sec", site: "https://ex.com")
`
	cases := []struct{ src, want string }{
		{pre + `t = OAuth2::AccessToken.new(c, "abc"); p t.token`, "\"abc\"\n"},
		{pre + `t = OAuth2::AccessToken.new(c, "abc"); p t.to_s`, "\"abc\"\n"},
		{pre + `t = OAuth2::AccessToken.new(c, "abc"); p t.expired?`, "false\n"},
		{pre + `t = OAuth2::AccessToken.new(c, "abc"); p t.expires?`, "false\n"},
		{pre + `t = OAuth2::AccessToken.new(c, "abc"); p t.expires_at`, "nil\n"},
		// A token carrying expires_at reports it as an Integer and is not expired
		// for a far-future value.
		{pre + `t = OAuth2::AccessToken.from_hash(c, "access_token" => "a", "expires_at" => 9999999999); p t.expires_at`, "9999999999\n"},
		{pre + `t = OAuth2::AccessToken.new(c, "abc"); p t.refresh_token`, "nil\n"},
		{pre + `t = OAuth2::AccessToken.new(c, "abc", "token_type" => "bearer"); p t.token_type`, "\"bearer\"\n"},
		{pre + `t = OAuth2::AccessToken.new(c, "abc", "scope" => "read"); p t.scope`, "\"read\"\n"},
		{pre + `t = OAuth2::AccessToken.new(c, "abc", "scope" => "read"); p t["scope"]`, "\"read\"\n"},
		{pre + `t = OAuth2::AccessToken.new(c, "abc"); p t["missing"]`, "nil\n"},
		{pre + `t = OAuth2::AccessToken.new(c, "abc"); p t.to_hash.is_a?(Hash)`, "true\n"},
		// from_hash rebuilds a token.
		{pre + `t = OAuth2::AccessToken.from_hash(c, "access_token" => "z", "refresh_token" => "r"); p t.token`, "\"z\"\n"},
		{pre + `t = OAuth2::AccessToken.from_hash(c, "access_token" => "z", "refresh_token" => "r"); p t.refresh_token`, "\"r\"\n"},
		// refresh builds a refresh Request from the token's refresh_token.
		{pre + `t = OAuth2::AccessToken.from_hash(c, "access_token" => "z", "refresh_token" => "r"); p t.refresh.method`, "\"POST\"\n"},
		{pre + `t = OAuth2::AccessToken.from_hash(c, "access_token" => "z", "refresh_token" => "r"); p t.refresh(scope: "s").body.include?("scope=s")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOAuth2Response covers Response.new / content_type / parsed, including the
// nil-parsed body and the get_token/parse_token error path.
func TestOAuth2Response(t *testing.T) {
	pre := `require "oauth2"
c = OAuth2::Client.new("id", "sec", site: "https://ex.com")
`
	cases := []struct{ src, want string }{
		{`require "oauth2"; r = OAuth2::Response.new(200, {"Content-Type" => "application/json"}, "{}"); p r.content_type`, "\"application/json\"\n"},
		// JSON numbers decode as Float (the gem's JSON parser), booleans and strings
		// map directly.
		{`require "oauth2"; r = OAuth2::Response.new(200, {"Content-Type" => "application/json"}, '{"a":1,"b":true,"c":"x"}'); p r.parsed`, "{\"a\" => 1.0, \"b\" => true, \"c\" => \"x\"}\n"},
		// Nested + array parse shapes.
		{`require "oauth2"; r = OAuth2::Response.new(200, {"Content-Type" => "application/json"}, '{"n":{"m":1},"arr":[1,2]}'); p r.parsed`, "{\"arr\" => [1.0, 2.0], \"n\" => {\"m\" => 1.0}}\n"},
		// A form-encoded body without JSON content-type still parses to a Hash.
		{`require "oauth2"; r = OAuth2::Response.new(200, {"Content-Type" => "application/x-www-form-urlencoded"}, "a=1&b=2"); p r.parsed`, "{\"a\" => \"1\", \"b\" => \"2\"}\n"},
		// get_token on a successful token response yields an AccessToken.
		{pre + `r = OAuth2::Response.new(200, {"Content-Type" => "application/json"}, '{"access_token":"tok"}'); p c.get_token(r).token`, "\"tok\"\n"},
		{pre + `r = OAuth2::Response.new(200, {"Content-Type" => "application/json"}, '{"access_token":"tok"}'); p c.parse_token(r).token`, "\"tok\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOAuth2PKCE covers the code-challenge helper for both methods.
func TestOAuth2PKCE(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "oauth2"; p OAuth2::PKCE.code_challenge("verifier", :S256)`, "\"iMnq5o6zALKXGivsnlom_0F5_WYda32GHkxlV7mq7hQ\"\n"},
		{`require "oauth2"; p OAuth2::PKCE.code_challenge("verifier", :plain)`, "\"verifier\"\n"},
		// Default method is S256.
		{`require "oauth2"; p OAuth2::PKCE.code_challenge("verifier") == OAuth2::PKCE.code_challenge("verifier", :S256)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOAuth2ArgErrors covers the arity / type guards and the get_token error path.
func TestOAuth2ArgErrors(t *testing.T) {
	pre := `require "oauth2"
c = OAuth2::Client.new("id", "sec", site: "https://ex.com")
`
	raises := []struct{ src, class string }{
		{`require "oauth2"; OAuth2::Client.new("id")`, "ArgumentError"},
		{`require "oauth2"; OAuth2::PKCE.code_challenge`, "ArgumentError"},
		{pre + `c.get_token`, "ArgumentError"},
		{pre + `c.get_token(5)`, "TypeError"},
		{pre + `OAuth2::AccessToken.new(c)`, "ArgumentError"},
		{pre + `OAuth2::AccessToken.new("notclient", "t")`, "TypeError"},
		{pre + `OAuth2::AccessToken.from_hash(c)`, "ArgumentError"},
		{pre + `OAuth2::AccessToken.from_hash("x", {})`, "TypeError"},
		{pre + `OAuth2::AccessToken.from_hash(c, 5)`, "TypeError"},
		{pre + `t = OAuth2::AccessToken.new(c, "a"); t.send(:[])`, "ArgumentError"},
		{`require "oauth2"; OAuth2::Response.new`, "ArgumentError"},
		// An error token response raises OAuth2::Error.
		{pre + `r = OAuth2::Response.new(400, {"Content-Type" => "application/json"}, '{"error":"invalid_grant"}'); c.get_token(r)`, "OAuth2::Error"},
		// Refreshing a token with no refresh_token raises OAuth2::Error.
		{pre + `OAuth2::AccessToken.new(c, "a").refresh`, "OAuth2::Error"},
	}
	for _, c := range raises {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestOAuth2ValueObjects covers the wrapper display methods and the Go-only
// bridge branches (option hash guards, non-hash option arg, oauth2Name/Str
// fallbacks, and the AccessToken.new params-hash path).
func TestOAuth2ValueObjects(t *testing.T) {
	client := &OAuth2Client{c: oauth2.NewClient("id", "sec", oauth2.Options{})}
	if client.ToS() == "" || client.Inspect() == "" || !client.Truthy() {
		t.Error("OAuth2Client display")
	}
	st := &OAuth2Strategy{className: "OAuth2::Strategy::AuthCode"}
	if st.ToS() == "" || st.Inspect() == "" || !st.Truthy() {
		t.Error("OAuth2Strategy display")
	}
	tok := &OAuth2AccessToken{t: oauth2.NewAccessToken(client.c, "x")}
	if tok.ToS() == "" || tok.Inspect() == "" || !tok.Truthy() {
		t.Error("OAuth2AccessToken display")
	}
	resp := &OAuth2Response{r: oauth2.NewResponse(200, oauth2.NewMap(), "")}
	if resp.ToS() == "" || resp.Inspect() == "" || !resp.Truthy() {
		t.Error("OAuth2Response display")
	}
	req := &OAuth2Request{r: &oauth2.Request{Method: "GET", URL: "https://x"}}
	if req.ToS() == "" || req.Inspect() == "" || !req.Truthy() {
		t.Error("OAuth2Request display")
	}

	// oauth2Options: a non-hash argument yields defaults.
	if o := oauth2Options([]object.Value{object.IntValue(int64(object.Integer(5)))}); o.Site != "" {
		t.Error("oauth2Options non-hash")
	}
	// oauth2Name / oauth2Str fall back to to_s for a non-Symbol/String value.
	if oauth2Name(object.IntValue(int64(object.Integer(7)))) != "7" {
		t.Error("oauth2Name fallback")
	}
	if oauth2Str(object.IntValue(int64(object.Integer(8)))) != "8" {
		t.Error("oauth2Str fallback")
	}
	// oauth2AnyToRuby covers every decoded-value shape.
	if oauth2AnyToRuby(nil) != object.NilV {
		t.Error("nil")
	}
	if oauth2AnyToRuby(int(3)).Inspect() != "3" {
		t.Error("int")
	}
	if oauth2AnyToRuby(int64(4)).Inspect() != "4" {
		t.Error("int64")
	}
	if oauth2AnyToRuby(struct{}{}) != object.NilV {
		t.Error("unmapped")
	}
	if !strings.Contains(oauth2AnyToRuby([]any{"a"}).Inspect(), "\"a\"") {
		t.Error("array")
	}
	// oauth2ParsedToHash on a nil map yields nil.
	if oauth2ParsedToHash(nil, nil) != object.NilV {
		t.Error("nil parsed")
	}
}

// TestOAuth2ArgAtSkipsHash covers oauth2ArgAt skipping a trailing options Hash and
// returning "" for a missing positional argument.
func TestOAuth2ArgAtSkipsHash(t *testing.T) {
	h := object.NewHash()
	args := []object.Value{object.Wrap(h), object.Wrap(object.NewString("first"))}
	if got := oauth2ArgAt(args, 0); got != "first" {
		t.Errorf("argAt(0) = %q", got)
	}
	if got := oauth2ArgAt(args, 5); got != "" {
		t.Errorf("argAt(missing) = %q", got)
	}
}
