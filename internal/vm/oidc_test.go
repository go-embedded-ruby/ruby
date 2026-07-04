// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	oidc "github.com/go-ruby-oidc/oidc"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// rsaMaterial generates an RSA key and returns its PKCS#8 PEM (as a Ruby
// double-quoted string literal, newlines escaped) and a single-key JWKS document
// (kid "kid1", alg RS256) whose modulus/exponent match the key.
func rsaMaterial(t *testing.T) (privRubyLit, jwks string) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	privRubyLit = "\"" + strings.ReplaceAll(pemStr, "\n", "\\n") + "\""
	n := base64.RawURLEncoding.EncodeToString(k.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes())
	jwks = fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"kid1","alg":"RS256","use":"sig","n":%q,"e":%q}]}`, n, e)
	return
}

// discDoc is a rich discovery document (every promoted member plus a nested extra)
// used across the discovery/metadata tests.
const discDoc = `{"issuer":"https://issuer.example",` +
	`"authorization_endpoint":"https://issuer.example/authorize",` +
	`"token_endpoint":"https://issuer.example/token",` +
	`"userinfo_endpoint":"https://issuer.example/userinfo",` +
	`"jwks_uri":"https://issuer.example/jwks",` +
	`"registration_endpoint":"https://issuer.example/register",` +
	`"end_session_endpoint":"https://issuer.example/logout",` +
	`"scopes_supported":["openid","email"],` +
	`"response_types_supported":["code"],` +
	`"response_modes_supported":["query"],` +
	`"grant_types_supported":["authorization_code"],` +
	`"subject_types_supported":["public"],` +
	`"id_token_signing_alg_values_supported":["RS256","HS256"],` +
	`"claims_supported":["sub","iss"],` +
	`"code_challenge_methods_supported":["S256"],` +
	`"extra":{"nested":true,"num":3,"frac":1.5,"arr":[1,"a",null]}}`

// TestOIDCModule covers the module wiring, require idempotence (both feature
// names) and the error tree.
func TestOIDCModule(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "openid_connect"; p OpenIDConnect.is_a?(Module)`, "true\n"},
		{`p require "openid_connect"`, "true\n"},
		{`require "openid_connect"; p require "openid_connect"`, "false\n"},
		{`p require "oidc"`, "true\n"},
		{`require "openid_connect"; p OpenIDConnect::Error < StandardError`, "true\n"},
		{`require "openid_connect"; p OpenIDConnect::InvalidTokenError < OpenIDConnect::Error`, "true\n"},
		{`require "openid_connect"; p OpenIDConnect::ExpiredError < OpenIDConnect::Error`, "true\n"},
		{`require "openid_connect"; p OpenIDConnect::JWKSError.ancestors.include?(StandardError)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOIDCProviderMetadata covers ProviderMetadata.parse, every promoted reader,
// [] (present/absent/non-string key) and to_h (which exercises the value bridges).
func TestOIDCProviderMetadata(t *testing.T) {
	pre := "require \"openid_connect\"\nmd=OpenIDConnect::ProviderMetadata.parse('" + discDoc + "')\n"
	cases := []struct{ src, want string }{
		{pre + `p md.issuer`, "\"https://issuer.example\"\n"},
		{pre + `p md.authorization_endpoint`, "\"https://issuer.example/authorize\"\n"},
		{pre + `p md.token_endpoint`, "\"https://issuer.example/token\"\n"},
		{pre + `p md.userinfo_endpoint`, "\"https://issuer.example/userinfo\"\n"},
		{pre + `p md.jwks_uri`, "\"https://issuer.example/jwks\"\n"},
		{pre + `p md.registration_endpoint`, "\"https://issuer.example/register\"\n"},
		{pre + `p md.end_session_endpoint`, "\"https://issuer.example/logout\"\n"},
		{pre + `p md.scopes_supported`, "[\"openid\", \"email\"]\n"},
		{pre + `p md.response_types_supported`, "[\"code\"]\n"},
		{pre + `p md.response_modes_supported`, "[\"query\"]\n"},
		{pre + `p md.grant_types_supported`, "[\"authorization_code\"]\n"},
		{pre + `p md.subject_types_supported`, "[\"public\"]\n"},
		{pre + `p md.id_token_signing_alg_values_supported`, "[\"RS256\", \"HS256\"]\n"},
		{pre + `p md.claims_supported`, "[\"sub\", \"iss\"]\n"},
		{pre + `p md.code_challenge_methods_supported`, "[\"S256\"]\n"},
		// [] reads a raw member (Symbol or String key); a nested object is a Hash.
		{pre + `p md["issuer"]`, "\"https://issuer.example\"\n"},
		{pre + `p md[:extra]["nested"]`, "true\n"},
		{pre + `p md["extra"]["num"]`, "3\n"},
		{pre + `p md["extra"]["frac"]`, "1.5\n"},
		{pre + `p md["extra"]["arr"]`, "[1, \"a\", nil]\n"},
		{pre + `p md["missing"]`, "nil\n"},
		// A non-string/symbol key coerces through to_s (and misses).
		{pre + `p md[5]`, "nil\n"},
		// to_h returns the whole document, keys sorted.
		{pre + `p md.to_h["issuer"]`, "\"https://issuer.example\"\n"},
		{pre + `p md.to_h.is_a?(Hash)`, "true\n"},
		// Display surface.
		{pre + `puts md`, "#<OpenIDConnect::ProviderMetadata issuer=https://issuer.example>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	raises := []struct{ src, class string }{
		{`require "openid_connect"; OpenIDConnect::ProviderMetadata.parse`, "ArgumentError"},
		{`require "openid_connect"; OpenIDConnect::ProviderMetadata.parse("not json")`, "OpenIDConnect::DiscoveryError"},
		{`require "openid_connect"; OpenIDConnect::ProviderMetadata.parse('{"issuer":"x"}')`, "OpenIDConnect::DiscoveryError"},
		{`require "openid_connect"
md=OpenIDConnect::ProviderMetadata.parse('` + discDoc + `'); md.send(:[])`, "ArgumentError"},
	}
	for _, c := range raises {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestOIDCDiscovery covers OpenIDConnect.discover over the Ruby HTTP-seam Doer:
// the happy path and every failure (issuer mismatch, non-200, malformed body, a
// non-Hash doer return, and the header/status-absent seam branches).
func TestOIDCDiscovery(t *testing.T) {
	okDoer := "doer=lambda{|req| {\"status\"=>200,\"headers\"=>{\"Content-Type\"=>\"application/json\"},\"body\"=>'" + discDoc + "'}}\n"
	cases := []struct{ src, want string }{
		{`require "openid_connect"
` + okDoer + `md=OpenIDConnect.discover("https://issuer.example", doer); p md.issuer`, "\"https://issuer.example\"\n"},
		// A doer that omits headers still works (the seam treats them as empty).
		{`require "openid_connect"
doer=lambda{|req| {"status"=>200,"body"=>'` + discDoc + `'}}
md=OpenIDConnect.discover("https://issuer.example", doer); p md.token_endpoint`, "\"https://issuer.example/token\"\n"},
		// A doer whose headers value is not a Hash is tolerated (branch skipped).
		{`require "openid_connect"
doer=lambda{|req| {"status"=>200,"headers"=>"nope","body"=>'` + discDoc + `'}}
md=OpenIDConnect.discover("https://issuer.example", doer); p md.jwks_uri`, "\"https://issuer.example/jwks\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	raises := []struct{ src, class string }{
		{`require "openid_connect"; OpenIDConnect.discover("https://issuer.example")`, "ArgumentError"},
		// issuer mismatch.
		{`require "openid_connect"
doer=lambda{|req| {"status"=>200,"body"=>'{"issuer":"https://other","authorization_endpoint":"a","token_endpoint":"t","jwks_uri":"j"}'}}
OpenIDConnect.discover("https://issuer.example", doer)`, "OpenIDConnect::DiscoveryError"},
		// non-200 (status absent → 0).
		{`require "openid_connect"
doer=lambda{|req| {"body"=>"oops"}}
OpenIDConnect.discover("https://issuer.example", doer)`, "OpenIDConnect::DiscoveryError"},
		// malformed body.
		{`require "openid_connect"
doer=lambda{|req| {"status"=>200,"body"=>"not json"}}
OpenIDConnect.discover("https://issuer.example", doer)`, "OpenIDConnect::DiscoveryError"},
		// A doer that does not return a Hash raises HTTPError.
		{`require "openid_connect"
OpenIDConnect.discover("https://issuer.example", lambda{|req| 42})`, "OpenIDConnect::HTTPError"},
	}
	for _, c := range raises {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestOIDCKeySet covers KeySet.parse, kids/size and the malformed-JWKS error.
func TestOIDCKeySet(t *testing.T) {
	_, jwks := rsaMaterial(t)
	pre := "require \"openid_connect\"\nks=OpenIDConnect::KeySet.parse('" + jwks + "')\n"
	cases := []struct{ src, want string }{
		{pre + `p ks.kids`, "[\"kid1\"]\n"},
		{pre + `p ks.size`, "1\n"},
		{pre + `puts ks`, "#<OpenIDConnect::KeySet>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	raises := []struct{ src, class string }{
		{`require "openid_connect"; OpenIDConnect::KeySet.parse`, "ArgumentError"},
		{`require "openid_connect"; OpenIDConnect::KeySet.parse("not json")`, "OpenIDConnect::JWKSError"},
	}
	for _, c := range raises {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// hsToken is a Ruby preamble building an HS256 ID token `tok` for issuer/client
// "https://issuer.example"/"client123", secret "topsecret", nonce "n0".
const hsPre = `require "openid_connect"
require "jwt"
iss="https://issuer.example"; cid="client123"; sec="topsecret"; now=Time.now.to_i
mk=lambda do |over|
  base={"iss"=>iss,"aud"=>cid,"sub"=>"user1","exp"=>now+3600,"iat"=>now-10,"nonce"=>"n0"}
  JWT.encode(base.merge(over), sec, "HS256")
end
tok=mk.call({})
`

// TestOIDCVerifierHS covers HS256 ID-token verification from rbgo-run Ruby: a
// valid token verifies, and tampered/expired/wrong-iss/wrong-aud/wrong-nonce (and
// the unsigned and missing-secret cases) are rejected with the matching error.
func TestOIDCVerifierHS(t *testing.T) {
	verOK := hsPre + `v=OpenIDConnect::Verifier.new(issuer:iss,client_id:cid,hmac_secret:sec,nonce:"n0")` + "\n"
	cases := []struct{ src, want string }{
		{verOK + `p v.verify(tok).subject`, "\"user1\"\n"},
		{verOK + `p v.verify(tok).issuer`, "\"https://issuer.example\"\n"},
		{verOK + `p v.verify(tok).audience`, "[\"client123\"]\n"},
		{verOK + `p v.verify(tok).nonce`, "\"n0\"\n"},
		{verOK + `p v.verify(tok).expires_at > now`, "true\n"},
		{verOK + `p v.verify(tok).issued_at <= now`, "true\n"},
		// A Float leeway is accepted.
		{hsPre + `v=OpenIDConnect::Verifier.new(issuer:iss,client_id:cid,hmac_secret:sec,leeway:1.5); p v.verify(tok).subject`, "\"user1\"\n"},
		// Display surface.
		{verOK + `puts v`, "#<OpenIDConnect::Verifier>\n"},
		{verOK + `puts v.verify(tok)`, "#<OpenIDConnect::IDTokenClaims sub=user1>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	verOKe := hsPre + `v=OpenIDConnect::Verifier.new(issuer:iss,client_id:cid,hmac_secret:sec,nonce:"n0")` + "\n"
	raises := []struct{ src, class string }{
		// Tampered signature: appending a base64url char always changes the
		// decoded HMAC bytes (or invalidates the encoding), so verification
		// fails deterministically. A last-char *flip* is unreliable because the
		// final base64url char carries unused low bits that can decode to the
		// same signature bytes.
		{verOKe + `bad = tok + "x"; v.verify(bad)`, "OpenIDConnect::InvalidTokenError"},
		// Expired.
		{hsPre + `t2=mk.call({"exp"=>now-3600}); OpenIDConnect::Verifier.new(issuer:iss,client_id:cid,hmac_secret:sec).verify(t2)`, "OpenIDConnect::ExpiredError"},
		// Wrong issuer.
		{verOKe + `OpenIDConnect::Verifier.new(issuer:"https://evil",client_id:cid,hmac_secret:sec).verify(tok)`, "OpenIDConnect::InvalidIssuerError"},
		// Wrong audience.
		{verOKe + `OpenIDConnect::Verifier.new(issuer:iss,client_id:"other",hmac_secret:sec).verify(tok)`, "OpenIDConnect::InvalidAudienceError"},
		// Wrong nonce.
		{verOKe + `OpenIDConnect::Verifier.new(issuer:iss,client_id:cid,hmac_secret:sec,nonce:"different").verify(tok)`, "OpenIDConnect::InvalidNonceError"},
		// Unsigned (alg none) rejected.
		{hsPre + `t3=JWT.encode({"iss"=>iss,"aud"=>cid,"sub"=>"u","exp"=>now+9,"iat"=>now}, nil, "none"); OpenIDConnect::Verifier.new(issuer:iss,client_id:cid,hmac_secret:sec).verify(t3)`, "OpenIDConnect::InvalidTokenError"},
		// HS token with no configured secret is a Config error.
		{hsPre + `OpenIDConnect::Verifier.new(issuer:iss,client_id:cid).verify(tok)`, "OpenIDConnect::ConfigError"},
		// Bad leeway type.
		{hsPre + `OpenIDConnect::Verifier.new(issuer:iss,client_id:cid,hmac_secret:sec,leeway:"x")`, "TypeError"},
		// verify arity.
		{verOKe + `v.verify`, "ArgumentError"},
	}
	for _, c := range raises {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestOIDCVerifierRSA covers the RSA/JWKS path from Ruby: a kid-selected key
// verifies a valid token, a kid miss is rejected, a wrong keys type raises, and
// the algorithms option (single String) is honoured.
func TestOIDCVerifierRSA(t *testing.T) {
	priv, jwks := rsaMaterial(t)
	pre := fmt.Sprintf(`require "openid_connect"
require "jwt"
iss="https://issuer.example"; cid="client123"; now=Time.now.to_i
priv=%s
ks=OpenIDConnect::KeySet.parse('%s')
pay={"iss"=>iss,"aud"=>cid,"sub"=>"user1","exp"=>now+3600,"iat"=>now-10,"nonce"=>"n0"}
tok=JWT.encode(pay,priv,"RS256",{"kid"=>"kid1"})
`, priv, jwks)
	cases := []struct{ src, want string }{
		{pre + `v=OpenIDConnect::Verifier.new(issuer:iss,client_id:cid,keys:ks,nonce:"n0",algorithms:"RS256"); p v.verify(tok).subject`, "\"user1\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	raises := []struct{ src, class string }{
		// kid miss (library wraps the JWKS lookup as an InvalidToken key error).
		{pre + `t2=JWT.encode(pay,priv,"RS256",{"kid"=>"nope"}); OpenIDConnect::Verifier.new(issuer:iss,client_id:cid,keys:ks).verify(t2)`, "OpenIDConnect::InvalidTokenError"},
		// keys of the wrong type.
		{pre + `OpenIDConnect::Verifier.new(issuer:iss,client_id:cid,keys:"notaset")`, "TypeError"},
	}
	for _, c := range raises {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestOIDCClaimsAccessors covers the IDTokenClaims [] reader (Symbol/String key)
// and to_h over a verified token.
func TestOIDCClaimsAccessors(t *testing.T) {
	pre := hsPre + `c=OpenIDConnect::Verifier.new(issuer:iss,client_id:cid,hmac_secret:sec,nonce:"n0").verify(tok)` + "\n"
	cases := []struct{ src, want string }{
		{pre + `p c[:sub]`, "\"user1\"\n"},
		{pre + `p c["nonce"]`, "\"n0\"\n"},
		{pre + `p c["missing"]`, "nil\n"},
		{pre + `p c.to_h["sub"]`, "\"user1\"\n"},
		{pre + `p c.to_h.is_a?(Hash)`, "true\n"},
		{pre + `begin; c.send(:[]); rescue ArgumentError; p "arg"; end`, "\"arg\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// clientPre builds a metadata + HS256 client with a mock doer serving the token
// and userinfo endpoints (the id_token is HS-signed with the client secret).
const clientPre = `require "openid_connect"
require "jwt"
iss="https://issuer.example"; cid="client123"; sec="topsecret"; now=Time.now.to_i
disc='` + discDoc + `'
pay={"iss"=>iss,"aud"=>cid,"sub"=>"user1","exp"=>now+3600,"iat"=>now-10,"nonce"=>"n0"}
idtok=JWT.encode(pay, sec, "HS256")
doer=lambda do |req|
  u=req["url"]
  if u.include?("/token")
    {"status"=>200,"headers"=>{"Content-Type"=>"application/json"},"body"=>'{"access_token":"at1","token_type":"bearer","id_token":"'+idtok+'"}'}
  elsif u.include?("/userinfo")
    {"status"=>200,"headers"=>{"Content-Type"=>"application/json"},"body"=>'{"sub":"user1","email":"u@x","age":42}'}
  else
    {"status"=>200,"headers"=>{"Content-Type"=>"application/json"},"body"=>disc}
  end
end
md=OpenIDConnect::ProviderMetadata.parse(disc)
c=OpenIDConnect::Client.new(metadata:md,client_id:cid,client_secret:sec,redirect_uri:"https://cb",scopes:["email"],doer:doer,leeway:5,jwks_ttl:60)
`

// TestOIDCClientFlow covers the full Authorization-Code + PKCE flow from Ruby:
// the authorization URL, the code exchange (with ID-token verification), userinfo
// and the token/verifier accessors.
func TestOIDCClientFlow(t *testing.T) {
	cases := []struct{ src, want string }{
		// authorization_url with PKCE, state, nonce, extra params, a non-string extra.
		{clientPre + `url=c.authorization_url(state:"s",nonce:"n0",code_verifier:"verifier1234567890",scopes:["profile"],prompt:"login",foo:7)
p url.include?("code_challenge=")`, "true\n"},
		{clientPre + `url=c.authorization_url(state:"s",nonce:"n0",code_verifier:"verifier1234567890")
p url.include?("code_challenge_method=S256")`, "true\n"},
		{clientPre + `url=c.authorization_url(scopes:["profile"])
p url.include?("scope=openid%20email%20profile")`, "true\n"},
		{clientPre + `url=c.authorization_url(prompt:"login",foo:7)
p url.include?("prompt=login")`, "true\n"},
		{clientPre + `url=c.authorization_url(prompt:"login",foo:7)
p url.include?("foo=7")`, "true\n"},
		// A minimal authorization_url (no scopes, no PKCE) still builds.
		{clientPre + `url=c.authorization_url(state:"s")
p url.include?("code_challenge")`, "false\n"},
		// exchange verifies the returned ID token and yields Tokens.
		{clientPre + `t=c.exchange("thecode", nonce:"n0"); p t.claims.subject`, "\"user1\"\n"},
		{clientPre + `t=c.exchange("thecode", nonce:"n0"); p t.access_token`, "\"at1\"\n"},
		{clientPre + `t=c.exchange("thecode", nonce:"n0"); p t.id_token == idtok`, "true\n"},
		// exchange with no kwargs (skips the nonce check).
		{clientPre + `t=c.exchange("thecode"); p t.claims.subject`, "\"user1\"\n"},
		// userinfo.
		{clientPre + `u=c.user_info("at1"); p u.subject`, "\"user1\"\n"},
		{clientPre + `u=c.user_info("at1"); p u["email"]`, "\"u@x\"\n"},
		{clientPre + `u=c.user_info("at1"); p u["age"]`, "42\n"},
		{clientPre + `u=c.user_info("at1"); p u["missing"]`, "nil\n"},
		{clientPre + `u=c.user_info("at1"); p u.to_h["sub"]`, "\"user1\"\n"},
		// verifier accessor verifies the same token.
		{clientPre + `p c.verifier.verify(idtok).subject`, "\"user1\"\n"},
		// Display surfaces.
		{clientPre + `puts c`, "#<OpenIDConnect::Client>\n"},
		{clientPre + `puts c.exchange("thecode")`, "#<OpenIDConnect::Tokens>\n"},
		{clientPre + `puts c.user_info("at1")`, "#<OpenIDConnect::UserInfo sub=user1>\n"},
		// discover builds a configured client from the well-known document.
		{clientPre + `c2=OpenIDConnect::Client.discover(iss, doer:doer, client_id:cid, client_secret:sec, redirect_uri:"https://cb")
p c2.is_a?(OpenIDConnect::Client)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOIDCClientErrors covers the Client arity/type guards and the flow error
// paths (missing doer, wrong metadata type, no id_token, token error, userinfo
// non-200).
func TestOIDCClientErrors(t *testing.T) {
	raises := []struct{ src, class string }{
		// A client with no doer is a Config error (the library requires one).
		{`require "openid_connect"
md=OpenIDConnect::ProviderMetadata.parse('` + discDoc + `')
OpenIDConnect::Client.new(metadata:md,client_id:"c")`, "OpenIDConnect::ConfigError"},
		// A client with no metadata is a Config error.
		{`require "openid_connect"
OpenIDConnect::Client.new(client_id:"c", doer:lambda{|r| {}})`, "OpenIDConnect::ConfigError"},
		// Wrong metadata type.
		{`require "openid_connect"
OpenIDConnect::Client.new(metadata:"notmeta", client_id:"c", doer:lambda{|r| {}})`, "TypeError"},
		// discover arity.
		{`require "openid_connect"; OpenIDConnect::Client.discover`, "ArgumentError"},
		// discover with a failing doer.
		{`require "openid_connect"
OpenIDConnect::Client.discover("https://issuer.example", doer:lambda{|r| {"status"=>404,"body"=>"no"}}, client_id:"c")`, "OpenIDConnect::DiscoveryError"},
		// exchange arity.
		{clientPre + `c.exchange`, "ArgumentError"},
		// A token response with no id_token.
		{`require "openid_connect"
md=OpenIDConnect::ProviderMetadata.parse('` + discDoc + `')
doer=lambda{|r| {"status"=>200,"headers"=>{"Content-Type"=>"application/json"},"body"=>'{"access_token":"a","token_type":"bearer"}'}}
OpenIDConnect::Client.new(metadata:md,client_id:"c",doer:doer).exchange("code")`, "OpenIDConnect::NoIDTokenError"},
		// A token endpoint error response.
		{`require "openid_connect"
md=OpenIDConnect::ProviderMetadata.parse('` + discDoc + `')
doer=lambda{|r| {"status"=>400,"headers"=>{"Content-Type"=>"application/json"},"body"=>'{"error":"invalid_grant"}'}}
OpenIDConnect::Client.new(metadata:md,client_id:"c",doer:doer).exchange("code")`, "OpenIDConnect::TokenError"},
		// user_info arity + non-200.
		{clientPre + `c.user_info`, "ArgumentError"},
		{`require "openid_connect"
md=OpenIDConnect::ProviderMetadata.parse('` + discDoc + `')
doer=lambda{|r| {"status"=>401,"body"=>"nope"}}
OpenIDConnect::Client.new(metadata:md,client_id:"c",doer:doer).user_info("at")`, "OpenIDConnect::UserInfoError"},
	}
	for _, c := range raises {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestOIDCDisplaysAndEdges covers the wrapper Inspect (p) and Truthy (ternary)
// surfaces for every value type, and a few residual guard branches (a zero-arg
// Verifier, a UserInfo [] arity guard).
func TestOIDCDisplaysAndEdges(t *testing.T) {
	cases := []struct{ src, want string }{
		{clientPre + `p md`, "#<OpenIDConnect::ProviderMetadata issuer=https://issuer.example>\n"},
		{clientPre + `p c`, "#<OpenIDConnect::Client>\n"},
		{clientPre + `p c.exchange("thecode")`, "#<OpenIDConnect::Tokens>\n"},
		{clientPre + `p c.verifier`, "#<OpenIDConnect::Verifier>\n"},
		{clientPre + `p c.verifier.verify(idtok)`, "#<OpenIDConnect::IDTokenClaims sub=user1>\n"},
		{clientPre + `p c.user_info("at1")`, "#<OpenIDConnect::UserInfo sub=user1>\n"},
		{`require "openid_connect"; ks=OpenIDConnect::KeySet.parse('{"keys":[]}'); p ks`, "#<OpenIDConnect::KeySet>\n"},
		// Truthy (all wrappers are truthy).
		{clientPre + `puts(md ? "t" : "f")`, "t\n"},
		{clientPre + `puts(c ? "t" : "f")`, "t\n"},
		{clientPre + `puts(c.exchange("thecode") ? "t" : "f")`, "t\n"},
		{clientPre + `puts(c.verifier ? "t" : "f")`, "t\n"},
		{clientPre + `puts(c.verifier.verify(idtok) ? "t" : "f")`, "t\n"},
		{clientPre + `puts(c.user_info("at1") ? "t" : "f")`, "t\n"},
		{`require "openid_connect"; ks=OpenIDConnect::KeySet.parse('{"keys":[]}'); puts(ks ? "t" : "f")`, "t\n"},
		// A zero-arg Verifier constructs (the option Hash is absent).
		{`require "openid_connect"; p OpenIDConnect::Verifier.new.is_a?(OpenIDConnect::Verifier)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// UserInfo [] with no argument is an ArgumentError.
	if class, _ := evalErr(t, clientPre+`c.user_info("at1").send(:[])`); class != "ArgumentError" {
		t.Errorf("userinfo [] arity: %q", class)
	}
}

// TestOIDCBridges covers the Go-only bridge branches not reachable from a decoded
// JSON document: the json.Number / int / int64 / unmapped shapes of oidcAnyToRuby,
// the oidcName / oidcStr to_s fallbacks, the seconds coercions, and the
// non-library-error branch of raiseOIDCError.
func TestOIDCBridges(t *testing.T) {
	if oidcAnyToRuby(json.Number("5")).Inspect() != "5" {
		t.Error("json.Number int")
	}
	if oidcAnyToRuby(json.Number("1.5")).Inspect() != "1.5" {
		t.Error("json.Number float")
	}
	if oidcAnyToRuby(int(3)).Inspect() != "3" {
		t.Error("int")
	}
	if oidcAnyToRuby(int64(4)).Inspect() != "4" {
		t.Error("int64")
	}
	if oidcAnyToRuby(struct{}{}) != object.NilV {
		t.Error("unmapped")
	}
	if oidcName(object.Integer(7)) != "7" {
		t.Error("oidcName fallback")
	}
	if oidcStr(object.Integer(8)) != "8" {
		t.Error("oidcStr fallback")
	}
	if oidcSeconds(object.Integer(2)) == 0 {
		t.Error("oidcSeconds int")
	}
	// raiseOIDCError with a non-library error raises the base OpenIDConnect::Error.
	func() {
		defer func() {
			r := recover()
			re, ok := r.(RubyError)
			if !ok || re.Class != "OpenIDConnect::Error" {
				t.Errorf("raiseOIDCError plain: %#v", r)
			}
		}()
		raiseOIDCError(errors.New("boom"))
	}()

	// Wrapper display for a UserInfo built directly from the library (covers the
	// ToS/Inspect/Truthy of that wrapper independently of the flow).
	ui, err := oidc.ParseUserInfo([]byte(`{"sub":"u"}`))
	if err != nil {
		t.Fatal(err)
	}
	w := &OIDCUserInfo{u: ui}
	if w.ToS() == "" || w.Inspect() == "" || !w.Truthy() {
		t.Error("OIDCUserInfo display")
	}
}
