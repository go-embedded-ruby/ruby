// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"encoding/base64"
	"errors"
	"math/big"
	"testing"

	"github.com/go-ruby-marshal/marshal"
	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestSinatraSessionRoundTrip covers the `enable :sessions` seam end-to-end
// through the DSL: the first request sets session[:count] and emits a signed
// Set-Cookie; feeding that cookie back in restores the value on the next
// request, across a different route.
func TestSinatraSessionRoundTrip(t *testing.T) {
	src := `require "sinatra/base"
class A < Sinatra::Base
  enable :sessions
  set :session_secret, "s3cr3t"
  get("/inc"){ session[:count] = (session[:count]||0)+1; "c=#{session[:count]}" }
  get("/read"){ "c=#{session[:count].inspect}" }
end
def sc(h); v=nil; h.each{|k,x| v=x if k.downcase=="set-cookie"}; v.split(";").first; end
_,h1,b1 = A.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/inc")
_,h2,b2 = A.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/inc","HTTP_COOKIE"=>sc(h1))
_,_,b3 = A.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/read","HTTP_COOKIE"=>sc(h2))
puts b1.join; puts b2.join; puts b3.join`
	if got := eval(t, src); got != "c=1\nc=2\nc=2\n" {
		t.Errorf("session round-trip got=%q want %q", got, "c=1\nc=2\nc=2\n")
	}
}

// TestSinatraSessionTamperRejected covers that a corrupted signed cookie fails
// HMAC verification and is dropped, so the handler sees a fresh empty session.
func TestSinatraSessionTamperRejected(t *testing.T) {
	src := `require "sinatra/base"
class A < Sinatra::Base
  enable :sessions
  set :session_secret, "k"
  get("/inc"){ session[:n] = (session[:n]||0)+1; "n=#{session[:n]}" }
end
def sc(h); v=nil; h.each{|k,x| v=x if k.downcase=="set-cookie"}; v.split(";").first; end
_,h1,_ = A.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/inc")
good = sc(h1)
name,val = good.split("=",2)
_,_,bt = A.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/inc","HTTP_COOKIE"=>"#{name}=#{val}deadbeef")
_,_,bg = A.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/inc","HTTP_COOKIE"=>good)
puts bt.join; puts bg.join`
	if got := eval(t, src); got != "n=1\nn=2\n" {
		t.Errorf("tamper reject got=%q want %q", got, "n=1\nn=2\n")
	}
}

// TestSinatraSessionClear covers session.clear emptying the store within a
// request, and the default (auto-generated) secret path — no session_secret is
// set, so the per-VM default is used and the round-trip still works.
func TestSinatraSessionClear(t *testing.T) {
	src := `require "sinatra/base"
class A < Sinatra::Base
  enable :sessions
  get("/set"){ session[:x] = "v"; "set" }
  get("/clear"){ session.clear; session[:x].inspect }
end
def sc(h); v=nil; h.each{|k,x| v=x if k.downcase=="set-cookie"}; v.split(";").first; end
_,h1,_ = A.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/set")
_,_,b = A.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/clear","HTTP_COOKIE"=>sc(h1))
puts b.join`
	if got := eval(t, src); got != "nil\n" {
		t.Errorf("session clear got=%q", got)
	}
}

// TestSinatraSessionCookieByteExact is the byte-parity proof: for the session
// {count: 1} signed with "secret", the serialization is byte-identical to MRI's
// default Rack::Session::Cookie coder — base64(Marshal.dump(hash)) is exactly
// the MRI Marshal.dump bytes, and the HMAC-SHA1 hex digest matches OpenSSL's.
// The constants below were computed from ruby's Marshal.dump({count: 1}) =
// "\x04\b{\x06:\ncounti\x06" and OpenSSL::HMAC.hexdigest("SHA1", "secret", data).
func TestSinatraSessionCookieByteExact(t *testing.T) {
	h := object.NewHash()
	h.Set(object.Symbol("count"), object.IntValue(1))

	data := base64.StdEncoding.EncodeToString(marshal.Dump(toMarshalValue(h, map[object.Value]marshal.Value{})))
	const wantData = "BAh7BjoKY291bnRpBg==" // == MRI [Marshal.dump({count: 1})].pack("m0")
	if data != wantData {
		t.Fatalf("marshal/base64 payload=%q want %q (not MRI byte-exact)", data, wantData)
	}
	const wantDigest = "41a93a4ae53bfb87c6064bd9a5f48f28e70c844f" // == OpenSSL::HMAC.hexdigest("SHA1","secret",data)
	if got := signSinatraSession(data, []byte("secret")); got != wantDigest {
		t.Fatalf("hmac digest=%q want %q", got, wantDigest)
	}

	// And the assembled Set-Cookie header is the Rack-escaped signed cookie,
	// HttpOnly + path=/, exactly as Rack::Utils.set_cookie_header emits it.
	vm := newTestVM()
	headers := rack.NewHeaders()
	vm.saveSinatraSession(&sinatraSessionState{hash: h, secret: []byte("secret")}, headers)
	got, _ := headers.GetOK(rack.SetCookie)
	const wantCookie = "rack.session=BAh7BjoKY291bnRpBg%3D%3D--41a93a4ae53bfb87c6064bd9a5f48f28e70c844f; path=/; httponly"
	if s, _ := got.(string); s != wantCookie {
		t.Fatalf("set-cookie=%q want %q", got, wantCookie)
	}
}

// TestSinatraSessionsEnabled covers the enabled / disabled / absent arms.
func TestSinatraSessionsEnabled(t *testing.T) {
	if sinatraSessionsEnabled(map[string]object.Value{}) {
		t.Error("absent should be disabled")
	}
	if sinatraSessionsEnabled(map[string]object.Value{"sessions": object.Bool(false)}) {
		t.Error("false should be disabled")
	}
	if !sinatraSessionsEnabled(map[string]object.Value{"sessions": object.Bool(true)}) {
		t.Error("true should be enabled")
	}
}

// TestSinatraSessionSecret covers each secret-resolution arm: an explicit
// session_secret String, an empty String falling through to the default, an
// absent setting generating the per-VM default (cached across calls), and the
// crypto/rand failure fallback via the sinatraRandRead seam.
func TestSinatraSessionSecret(t *testing.T) {
	vm := newTestVM()
	if got := string(vm.sinatraSessionSecret(map[string]object.Value{"session_secret": object.NewString("abc")})); got != "abc" {
		t.Errorf("explicit secret=%q", got)
	}

	// Empty string is treated as unset -> per-VM default (generated once, cached).
	vm2 := newTestVM()
	d1 := vm2.sinatraSessionSecret(map[string]object.Value{"session_secret": object.NewString("")})
	d2 := vm2.sinatraSessionSecret(map[string]object.Value{})
	if len(d1) != 32 || string(d1) != string(d2) {
		t.Errorf("default secret not stable/32B: len=%d stable=%v", len(d1), string(d1) == string(d2))
	}

	// crypto/rand failure -> the fixed fallback key.
	orig := sinatraRandRead
	sinatraRandRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	defer func() { sinatraRandRead = orig }()
	vm3 := newTestVM()
	if got := string(vm3.sinatraSessionSecret(map[string]object.Value{})); got != "rbgo-sinatra-default-session-secret" {
		t.Errorf("rand-failure fallback=%q", got)
	}
}

// TestLoadSinatraSession covers loadSinatraSession's arms: disabled -> nil,
// enabled with no cookie -> empty, enabled with a valid cookie -> restored, a
// non-string cookie value (bare "rack.session" with no '=') -> empty.
func TestLoadSinatraSession(t *testing.T) {
	vm := newTestVM()
	enabled := map[string]object.Value{"sessions": object.Bool(true), "session_secret": object.NewString("k")}

	if vm.loadSinatraSession(map[string]object.Value{}, rack.Env{}) != nil {
		t.Error("disabled should load nil")
	}

	empty := vm.loadSinatraSession(enabled, rack.Env{})
	if empty == nil || len(empty.hash.Keys) != 0 {
		t.Errorf("no-cookie should be empty, got %#v", empty)
	}

	// Round-trip a real cookie: save one, then load it back.
	src := object.NewHash()
	src.Set(object.Symbol("k"), object.NewString("v"))
	h := rack.NewHeaders()
	vm.saveSinatraSession(&sinatraSessionState{hash: src, secret: []byte("k")}, h)
	cookie, _ := h.GetOK(rack.SetCookie)
	// A stored cookie the client echoes back is name=value; reconstruct that.
	env := rack.Env{"HTTP_COOKIE": firstCookiePair(cookie.(string))}
	loaded := vm.loadSinatraSession(enabled, env)
	if v, ok := loaded.hash.Get(object.Symbol("k")); !ok || v.(*object.String).Str() != "v" {
		t.Errorf("cookie round-trip: k=%#v", v)
	}

	// A bare cookie name (no '=') parses to a nil value -> empty session.
	bare := vm.loadSinatraSession(enabled, rack.Env{"HTTP_COOKIE": "rack.session"})
	if bare == nil || len(bare.hash.Keys) != 0 {
		t.Errorf("bare cookie should be empty, got %#v", bare)
	}
}

// firstCookiePair extracts the "name=value" segment (as a browser stores it)
// from a Set-Cookie header value, dropping the attributes.
func firstCookiePair(setCookie string) string {
	for i := 0; i < len(setCookie); i++ {
		if setCookie[i] == ';' {
			return setCookie[:i]
		}
	}
	return setCookie
}

// TestDecodeSinatraSession covers every failure arm of the decoder plus the
// success arm: no "--" separator, a bad HMAC, invalid base64, a valid signature
// over non-Marshal bytes, a valid signature over a non-Hash payload, and a
// well-formed Hash cookie.
func TestDecodeSinatraSession(t *testing.T) {
	secret := []byte("k")
	sign := func(data string) string { return data + "--" + signSinatraSession(data, secret) }
	b64 := func(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

	if decodeSinatraSession("no-separator", secret) != nil {
		t.Error("missing -- should be nil")
	}
	if decodeSinatraSession("data--badmac", secret) != nil {
		t.Error("bad hmac should be nil")
	}
	if decodeSinatraSession(sign("!!!not-base64!!!"), secret) != nil {
		t.Error("bad base64 should be nil")
	}
	if decodeSinatraSession(sign(b64([]byte{0x00, 0x01, 0x02})), secret) != nil {
		t.Error("non-marshal payload should be nil")
	}
	// Valid signature over a Marshalled Array (not a Hash) -> nil.
	arr := marshal.Dump(&marshal.Array{Elems: []marshal.Value{marshal.Int{I: big.NewInt(1)}}})
	if decodeSinatraSession(sign(b64(arr)), secret) != nil {
		t.Error("non-hash payload should be nil")
	}
	// Valid Hash cookie -> decoded Hash.
	src := object.NewHash()
	src.Set(object.Symbol("a"), object.IntValue(7))
	good := b64(marshal.Dump(toMarshalValue(src, map[object.Value]marshal.Value{})))
	h := decodeSinatraSession(sign(good), secret)
	if h == nil {
		t.Fatal("valid cookie decoded to nil")
	}
	if v, ok := h.Get(object.Symbol("a")); !ok || int64(v.(object.Integer)) != 7 {
		t.Errorf("decoded hash a=%#v", v)
	}
}

// TestVerifySinatraSession covers the sign/verify pair and the constant-time
// rejection of a wrong signature.
func TestVerifySinatraSession(t *testing.T) {
	secret := []byte("key")
	sig := signSinatraSession("payload", secret)
	if !verifySinatraSession("payload", sig, secret) {
		t.Error("valid signature should verify")
	}
	if verifySinatraSession("payload", sig, []byte("other")) {
		t.Error("wrong secret should not verify")
	}
	if verifySinatraSession("payload", "deadbeef", secret) {
		t.Error("wrong digest should not verify")
	}
}
