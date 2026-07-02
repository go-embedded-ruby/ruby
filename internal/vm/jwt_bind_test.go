// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	jwt "github.com/go-ruby-jwt/jwt"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestJWTEncodeDecodeHS drives JWT.encode + JWT.decode with an HS256 secret
// through rbgo end to end: a token round-trips its payload (String, Integer,
// nested Array/Hash, bool, nil, float) and its header, and a wrong secret is
// rejected with JWT::VerificationError (a JWT::DecodeError, a StandardError).
func TestJWTEncodeDecodeHS(t *testing.T) {
	src := `
require "jwt"
tok = JWT.encode({"user" => "amy", "n" => 7, "f" => 1.5, "b" => true, "z" => nil, "arr" => [1, "x"], "h" => {"k" => 2}}, "secret", "HS256")
payload, header = JWT.decode(tok, "secret", true, algorithm: "HS256")
r = []
r << payload["user"]
r << payload["n"]
r << payload["f"]
r << payload["b"]
r << payload["z"].inspect
r << payload["arr"].inspect
r << payload["h"]["k"]
r << header["alg"]
begin
  JWT.decode(tok, "wrong", true, algorithm: "HS256")
rescue JWT::VerificationError
  r << "verify-fail"
end
begin
  JWT.decode(tok, "wrong", true, algorithm: "HS256")
rescue StandardError
  r << "std"
end
puts r.join("|")
`
	want := `amy|7|1.5|true|nil|[1, "x"]|2|HS256|verify-fail|std`
	if got := runSrc(t, src); got != want {
		t.Fatalf("jwt HS round-trip = %q want %q", got, want)
	}
}

// TestJWTEncodeError covers encode's error path: an unsupported signing
// algorithm raises JWT::EncodeError.
func TestJWTEncodeError(t *testing.T) {
	src := `
require "jwt"
begin
  JWT.encode({"a" => 1}, "s", "NOPE")
rescue JWT::EncodeError
  puts "encode-fail"
end
`
	if got, want := runSrc(t, src), "encode-fail"; got != want {
		t.Fatalf("jwt encode error = %q want %q", got, want)
	}
}

// TestJWTNoVerify covers decode with verify=false (no key needed) and the
// default algorithm ("HS256") + custom header fields on encode.
func TestJWTNoVerify(t *testing.T) {
	src := `
require "jwt"
tok = JWT.encode({"a" => 1}, "s", "HS256", {"kid" => "k1"})
payload, header = JWT.decode(tok, nil, false)
r = ["#{payload["a"]}:#{header["kid"]}:#{header["alg"]}"]
# encode with the algorithm (and header) omitted defaults to HS256 with no
# custom header fields.
tok2 = JWT.encode({"b" => 2}, "s")
payload2, header2 = JWT.decode(tok2, "s", true, algorithm: "HS256")
r << "#{payload2["b"]}:#{header2["alg"]}"
puts r.join("|")
`
	if got, want := runSrc(t, src), "1:k1:HS256|2:HS256"; got != want {
		t.Fatalf("jwt no-verify = %q want %q", got, want)
	}
}

// TestJWTExpired covers the exp claim check: an expired token raises
// JWT::ExpiredSignature under the default verify_expiration.
func TestJWTExpired(t *testing.T) {
	src := `
require "jwt"
tok = JWT.encode({"exp" => 1}, "s", "HS256")
r = []
begin
  JWT.decode(tok, "s", true, algorithm: "HS256")
rescue JWT::ExpiredSignature
  r << "expired"
end
payload, = JWT.decode(tok, "s", true, algorithm: "HS256", verify_expiration: false)
r << payload["exp"]
puts r.join(",")
`
	if got, want := runSrc(t, src), "expired,1"; got != want {
		t.Fatalf("jwt expired = %q want %q", got, want)
	}
}

// TestJWTAlgConfusion covers the algorithms: allow-list guard: a token whose alg
// is not listed is rejected with JWT::IncorrectAlgorithm.
func TestJWTAlgConfusion(t *testing.T) {
	src := `
require "jwt"
tok = JWT.encode({"a" => 1}, "s", "HS256")
begin
  JWT.decode(tok, "s", true, algorithms: ["HS512"])
rescue JWT::IncorrectAlgorithm
  puts "alg-confusion"
end
`
	if got, want := runSrc(t, src), "alg-confusion"; got != want {
		t.Fatalf("jwt alg-confusion = %q want %q", got, want)
	}
}

// TestJWTClaimOptions covers the remaining decode-option arms (leeway,
// verify_not_before/nbf, verify_iat, iss/verify_iss, aud/verify_aud,
// sub/verify_sub, required_claims) so jwtDecodeOpts is fully exercised.
func TestJWTClaimOptions(t *testing.T) {
	src := `
require "jwt"
now = Time.now.to_i
tok = JWT.encode({"iss" => "me", "aud" => ["x", "y"], "sub" => "s1", "jti" => "j", "nbf" => now - 10, "iat" => now - 5}, "k", "HS256")
payload, = JWT.decode(tok, "k", true,
  algorithm: "HS256", leeway: 5,
  verify_not_before: true, verify_iat: true,
  iss: ["me", "other"], verify_iss: true,
  aud: "x", verify_aud: true,
  sub: "s1", verify_sub: true,
  required_claims: ["iss", "sub"])
puts "#{payload["iss"]}:#{payload["sub"]}"
`
	if got, want := runSrc(t, src), "me:s1"; got != want {
		t.Fatalf("jwt claim options = %q want %q", got, want)
	}
}

// TestJWTIssuerMismatch covers the iss check with a single-string expected issuer
// (the scalar arm of jwtGoScalarOrList) raising JWT::InvalidIssuerError.
func TestJWTIssuerMismatch(t *testing.T) {
	src := `
require "jwt"
tok = JWT.encode({"iss" => "me"}, "k", "HS256")
begin
  JWT.decode(tok, "k", true, algorithm: "HS256", iss: "other", verify_iss: true)
rescue JWT::InvalidIssuerError
  puts "bad-iss"
end
`
	if got, want := runSrc(t, src), "bad-iss"; got != want {
		t.Fatalf("jwt issuer mismatch = %q want %q", got, want)
	}
}

// TestJWTRS256 drives an RS256 round-trip through rbgo with PEM keys written to
// disk (the PKIX public / PKCS#8 private encodings), covering the RSA arm of the
// key parsers.
func TestJWTRS256(t *testing.T) {
	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	writePEM(t, filepath.Join(dir, "priv.pem"), "PRIVATE KEY", privDER)
	writePEM(t, filepath.Join(dir, "pub.pem"), "PUBLIC KEY", pubDER)
	src := `
require "jwt"
priv = File.read("` + filepath.Join(dir, "priv.pem") + `")
pub  = File.read("` + filepath.Join(dir, "pub.pem") + `")
tok = JWT.encode({"u" => "amy"}, priv, "RS256")
payload, header = JWT.decode(tok, pub, true, algorithm: "RS256")
puts "#{payload["u"]}:#{header["alg"]}"
`
	if got, want := runSrc(t, src), "amy:RS256"; got != want {
		t.Fatalf("jwt RS256 = %q want %q", got, want)
	}
}

// TestJWTES256 drives an ES256 round-trip with SEC1 private / PKIX public PEM
// keys, covering the ECDSA arms of the key parsers.
func TestJWTES256(t *testing.T) {
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	writePEM(t, filepath.Join(dir, "priv.pem"), "EC PRIVATE KEY", privDER)
	writePEM(t, filepath.Join(dir, "pub.pem"), "PUBLIC KEY", pubDER)
	src := `
require "jwt"
priv = File.read("` + filepath.Join(dir, "priv.pem") + `")
pub  = File.read("` + filepath.Join(dir, "pub.pem") + `")
tok = JWT.encode({"u" => "bob"}, priv, "ES256")
payload, header = JWT.decode(tok, pub, true, algorithm: "ES256")
puts "#{payload["u"]}:#{header["alg"]}"
`
	if got, want := runSrc(t, src), "bob:ES256"; got != want {
		t.Fatalf("jwt ES256 = %q want %q", got, want)
	}
}

// TestJWTBadPEM covers the malformed-PEM error path: an RS256 decode with a key
// that is not PEM raises a JWT error (mapped to JWT::DecodeError).
func TestJWTBadPEM(t *testing.T) {
	src := `
require "jwt"
begin
  JWT.decode("a.b.c", "not-a-pem", true, algorithm: "RS256")
rescue JWT::DecodeError
  puts "bad-pem"
end
`
	if got, want := runSrc(t, src), "bad-pem"; got != want {
		t.Fatalf("jwt bad PEM = %q want %q", got, want)
	}
}

// TestJWTKeyFamilies covers jwtKey's algorithm-family routing directly, including
// the "none" pass-through and the default arm, plus jwtRawKey's nil and non-string
// coercion arms not reached through the Ruby tests.
func TestJWTKeyFamilies(t *testing.T) {
	if got := jwtKey(object.NilV, "", false); got != nil {
		t.Errorf(`jwtKey(nil, "") = %v want nil`, got)
	}
	if got := jwtKey(object.NilV, "none", false); got != nil {
		t.Errorf(`jwtKey(nil, "none") = %v want nil`, got)
	}
	if got := jwtKey(object.NewString("sec"), "HS256", false); got != "sec" {
		t.Errorf("jwtKey HS = %v want sec", got)
	}
	if got := jwtRawKey(object.NilV); got != nil {
		t.Errorf("jwtRawKey(nil) = %v want nil", got)
	}
	// A non-nil, non-string key coerces through strArg — an Integer raises TypeError.
	func() {
		defer func() {
			if re, ok := recover().(RubyError); !ok || re.Class != "TypeError" {
				t.Errorf("jwtRawKey(Integer): got %v want TypeError", recover())
			}
		}()
		jwtRawKey(object.IntValue(1))
	}()
}

// TestJWTFromRuby covers the encode-side value bridge across every arm, including
// the Symbol, Float, bool, nil, Bignum-less scalar and the to_s-of-unknown default
// (a Range), which the document round-trips do not all reach directly.
func TestJWTFromRuby(t *testing.T) {
	if v := jwtFromRuby(nil); v != nil {
		t.Errorf("go-nil -> %v want nil", v)
	}
	if v := jwtFromRuby(object.NilV); v != nil {
		t.Errorf("nil -> %v want nil", v)
	}
	if v := jwtFromRuby(object.Bool(true)); v != true {
		t.Errorf("bool -> %v want true", v)
	}
	if v := jwtFromRuby(object.IntValue(3)); v != int64(3) {
		t.Errorf("int -> %v want 3", v)
	}
	if v := jwtFromRuby(object.Float(2.5)); v != 2.5 {
		t.Errorf("float -> %v want 2.5", v)
	}
	if v := jwtFromRuby(object.NewString("s")); v != "s" {
		t.Errorf("string -> %v want s", v)
	}
	if v := jwtFromRuby(object.Symbol("sym")); v != "sym" {
		t.Errorf("symbol -> %v want sym", v)
	}
	arr := jwtFromRuby(&object.Array{Elems: []object.Value{object.IntValue(1)}}).([]any)
	if len(arr) != 1 || arr[0] != int64(1) {
		t.Errorf("array -> %v", arr)
	}
	h := object.NewHash()
	h.Set(object.NewString("k"), object.IntValue(9))
	m := jwtFromRuby(h).(*jwt.OrderedMap)
	if v, _ := m.Get("k"); v != int64(9) {
		t.Errorf("hash -> %v", v)
	}
	// The default arm renders an unknown value's Ruby to_s.
	rng := &object.Range{Lo: object.IntValue(1), Hi: object.IntValue(2)}
	if v := jwtFromRuby(rng); v != rng.ToS() {
		t.Errorf("range -> %v want %v", v, rng.ToS())
	}
}

// TestJWTToRuby covers the decode-side value bridge across every arm, including
// the raw Go scalar types the library never emits through Ruby (int, float64,
// whole-number float, non-integer json.Number) and the unknown-type default.
func TestJWTToRuby(t *testing.T) {
	if v := jwtToRuby(nil); v != object.NilV {
		t.Errorf("nil -> %v", v)
	}
	if v := jwtToRuby(true); v != object.Bool(true) {
		t.Errorf("bool -> %v", v)
	}
	if v, ok := jwtToRuby("s").(*object.String); !ok || v.Str() != "s" {
		t.Errorf("string -> %v", v)
	}
	if v := jwtToRuby(float64(4)); v != object.IntValue(4) {
		t.Errorf("whole float -> %v want Integer 4", v)
	}
	if v := jwtToRuby(float64(4.5)); v != object.Float(4.5) {
		t.Errorf("float -> %v want 4.5", v)
	}
	if v := jwtToRuby(int64(5)); v != object.IntValue(5) {
		t.Errorf("int64 -> %v", v)
	}
	if v := jwtToRuby(7); v != object.IntValue(7) {
		t.Errorf("int -> %v", v)
	}
	arr := jwtToRuby([]any{int64(1)}).(*object.Array)
	if len(arr.Elems) != 1 {
		t.Errorf("array -> %v", arr)
	}
	m := jwt.NewOrderedMap()
	m.Set("k", int64(2))
	h := jwtToRuby(m).(*object.Hash)
	if v, _ := h.Get(object.NewString("k")); v != object.IntValue(2) {
		t.Errorf("ordered-map -> %v", v)
	}
	// An unknown Go type maps to nil.
	if v := jwtToRuby(struct{}{}); v != object.NilV {
		t.Errorf("unknown -> %v want nil", v)
	}
}

// TestJWTRaiseError covers raiseJWTError's plain-error fall-through (a non
// jwt.Error maps to JWT::DecodeError); the jwt.Error arm is covered by the
// end-to-end verification/expiry tests.
func TestJWTRaiseError(t *testing.T) {
	defer func() {
		if re, ok := recover().(RubyError); !ok || re.Class != "JWT::DecodeError" {
			t.Errorf("raiseJWTError(plain): got %v want JWT::DecodeError", recover())
		}
	}()
	raiseJWTError(errors.New("boom"))
}

// writePEM writes a PEM file with the given block type for a JWT key test.
func writePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}
