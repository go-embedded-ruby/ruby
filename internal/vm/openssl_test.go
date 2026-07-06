// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// osslEval runs src through parser -> compiler -> VM and returns captured
// stdout. It lives in the internal package (so the lastColonIndex unit test
// below can reach an unexported helper) and mirrors the external eval helper.
func osslEval(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	return buf.String()
}

// osslRunErr returns the runtime (or parse/compile) error for src.
func osslRunErr(t *testing.T, src string) error {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		return err
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		return err
	}
	_, err = New(&bytes.Buffer{}).Run(iseq)
	return err
}

// TestOpenSSLDigest drives the OpenSSL::Digest surface (instance + class
// one-shots + named subclasses) to its full statement/branch coverage, with the
// real-crypto outputs asserted against MRI Ruby 4.0.5 (ruby -r openssl).
func TestOpenSSLDigest(t *testing.T) {
	const sha256abc = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	const sha1abc = "a9993e364706816aba3e25717850c26c9cd0d89d"
	const sha256abcB64 = "ungWv48Bz+pBQUDeXa4iI7ADYaOWF3qctBD/YfIAFa0="

	cases := []struct{ src, want string }{
		// Instance: update + << feed the running hash; hexdigest emits it.
		{`require "openssl"
d = OpenSSL::Digest.new("SHA256"); d.update("ab"); d << "c"; p d.hexdigest`,
			"\"" + sha256abc + "\"\n"},
		// new with the optional seed (data) second argument.
		{`require "openssl"; p OpenSSL::Digest.new("SHA256", "abc").hexdigest`,
			"\"" + sha256abc + "\"\n"},
		// digest (binary) instance form, sized.
		{`require "openssl"; p OpenSSL::Digest.new("SHA256").digest("abc").bytesize`, "32\n"},
		// base64digest instance form.
		{`require "openssl"; p OpenSSL::Digest.new("SHA256").base64digest("abc")`,
			"\"" + sha256abcB64 + "\"\n"},
		// digest(data) resets then feeds: equivalent to a fresh digest of "abc".
		{`require "openssl"
e = OpenSSL::Digest.new("SHA256"); e.update("zzz")
p e.digest("abc") == OpenSSL::Digest.new("SHA256", "abc").digest`, "true\n"},
		// reset clears the running state back to the empty digest.
		{`require "openssl"
f = OpenSSL::Digest.new("SHA256"); f.update("abc"); f.reset
p f.hexdigest == OpenSSL::Digest.new("SHA256").hexdigest`, "true\n"},
		// digest_length / block_length come straight from crypto/*.
		{`require "openssl"; d = OpenSSL::Digest.new("SHA256"); p [d.digest_length, d.block_length]`,
			"[32, 64]\n"},
		// to_s / inspect (ToS + Inspect on *opensslDigest) and truthiness (Truthy).
		{`require "openssl"
d = OpenSSL::Digest.new("SHA256")
puts d.to_s
p d
puts(d ? "t" : "f")`, "#<OpenSSL::Digest>\n#<OpenSSL::Digest>\nt\n"},
		// Class one-shots: digest / hexdigest / base64digest with explicit name.
		{`require "openssl"; p OpenSSL::Digest.digest("SHA256", "abc").bytesize`, "32\n"},
		{`require "openssl"; p OpenSSL::Digest.hexdigest("SHA256", "abc")`,
			"\"" + sha256abc + "\"\n"},
		{`require "openssl"; p OpenSSL::Digest.base64digest("SHA256", "abc")`,
			"\"" + sha256abcB64 + "\"\n"},
		// Named subclass: new with no extra args, and seeded (subclass second arg).
		{`require "openssl"; p OpenSSL::Digest::SHA256.new.update("abc").hexdigest`,
			"\"" + sha256abc + "\"\n"},
		{`require "openssl"; p OpenSSL::Digest::SHA1.new("abc").hexdigest`,
			"\"" + sha1abc + "\"\n"},
		// Named subclass one-shots (no algorithm argument needed).
		{`require "openssl"; p OpenSSL::Digest::SHA256.digest("abc").bytesize`, "32\n"},
		{`require "openssl"; p OpenSSL::Digest::SHA256.hexdigest("abc")`,
			"\"" + sha256abc + "\"\n"},
		// Dashed spelling accepted (digestNewByName tolerance).
		{`require "openssl"; p OpenSSL::Digest.new("SHA-256").hexdigest("abc")`,
			"\"" + sha256abc + "\"\n"},
	}
	for _, c := range cases {
		if got := osslEval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Error / raise branches. Our pure-Go impl raises RuntimeError for an unknown
	// algorithm and ArgumentError for the arity faults (MRI raises a DigestError
	// for the unknown algo, but the surface and recoverability match).
	errCases := []struct{ src, want string }{
		{`require "openssl"; OpenSSL::Digest.new("BOGUS")`, "RuntimeError"},
		{`require "openssl"; OpenSSL::Digest.new`, "ArgumentError"},
		{`require "openssl"; OpenSSL::Digest.digest("SHA256")`, "ArgumentError"},    // one-shot needs data
		{`require "openssl"; OpenSSL::Digest.digest("BOGUS", "x")`, "RuntimeError"}, // one-shot unknown algo
		{`require "openssl"; OpenSSL::Digest::SHA256.digest`, "ArgumentError"},      // subclass one-shot needs data
	}
	for _, c := range errCases {
		if err := osslRunErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestOpenSSLHMAC drives OpenSSL::HMAC.digest/.hexdigest, including the
// name-string selector, the OpenSSL::Digest-instance selector (both the
// algorithm-bearing subclass name path and the base-class digestBySize
// fallback), and the arity / unknown-algorithm raises. Outputs are MRI-checked.
func TestOpenSSLHMAC(t *testing.T) {
	const hmacSha256 = "2d93cbc1be167bcb1637a4a23cbff01a7878f0c50ee833954ea5221bb1b8c628"

	cases := []struct{ src, want string }{
		// Name-string selector (default branch of hmacConstructor).
		{`require "openssl"; p OpenSSL::HMAC.hexdigest("SHA256", "key", "msg")`,
			"\"" + hmacSha256 + "\"\n"},
		{`require "openssl"; p OpenSSL::HMAC.digest("SHA256", "key", "msg").bytesize`, "32\n"},
		// Subclass-instance selector: class name carries the algorithm, so
		// digestCloneCtor resolves it directly (the non-fallback branch).
		{`require "openssl"
p OpenSSL::HMAC.hexdigest(OpenSSL::Digest::SHA256.new, "key", "msg")`,
			"\"" + hmacSha256 + "\"\n"},
	}
	for _, c := range cases {
		if got := osslEval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	// Base-class OpenSSL::Digest.new(alg) instances carry no algorithm in their
	// class name ("OpenSSL::Digest"), so digestCloneCtor falls back to
	// digestBySize — exercise every size branch (and the SHA256 default) against
	// MRI's results.
	sizeCases := []struct{ alg, want string }{
		{"MD5", "18e3548c59ad40dd03907b7aeee71d67"},
		{"SHA1", "102900b72b7bf1031eec76b4804b66052376896b"},
		{"SHA224", "afea596463272cda5a2a49ff64d8e2b7afe35b3604e09a14793041fe"},
		{"SHA256", hmacSha256}, // default branch of digestBySize
		{"SHA384", "3bba95ff38376a129225ec5430dd3aff6ac7b7acdb829a4af35f33f8c6ddbbf9d85fb31f8b20316db93aedd08a816cfa"},
		{"SHA512", "1e4b55b925ccc28ed90d9d18fc2393fcbe164c0d84e67e173cc5aa486b7afc106633c66bdc309076f5f8d9fdbbb62456f894f2c23377fbcc12f4ab2940eb6d70"},
	}
	for _, c := range sizeCases {
		src := `require "openssl"
p OpenSSL::HMAC.hexdigest(OpenSSL::Digest.new("` + c.alg + `"), "key", "msg")`
		want := "\"" + c.want + "\"\n"
		if got := osslEval(t, src); got != want {
			t.Errorf("alg=%s got=%q want=%q", c.alg, got, want)
		}
	}

	errCases := []struct{ src, want string }{
		{`require "openssl"; OpenSSL::HMAC.hexdigest("SHA256", "key")`, "ArgumentError"},  // too few args
		{`require "openssl"; OpenSSL::HMAC.hexdigest("BOGUS", "k", "m")`, "RuntimeError"}, // unknown algo
	}
	for _, c := range errCases {
		if err := osslRunErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestOpenSSLRandom covers OpenSSL::Random.random_bytes (crypto/rand via the
// secureBytes seam), including the ASCII-8BIT (binary) encoding MRI returns.
func TestOpenSSLRandom(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "openssl"; p OpenSSL::Random.random_bytes(8).bytesize`, "8\n"},
		{`require "openssl"; p OpenSSL::Random.random_bytes(8).encoding.name`, "\"ASCII-8BIT\"\n"},
	}
	for _, c := range cases {
		if got := osslEval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOpenSSLPKIShell covers the PKI/TLS class-and-constant shell: the resolving
// constants, the SSLContext that is actually constructible (new/initialize/
// set_params, DEFAULT_PARAMS), and the NotImplementedError raises from every
// shell .new plus SSL.verify_certificate_identity.
func TestOpenSSLPKIShell(t *testing.T) {
	cases := []struct{ src, want string }{
		// Verification-result and verify-mode / TLS-option constants resolve.
		{`require "openssl"; p [OpenSSL::X509::V_OK, OpenSSL::X509::V_ERR_HOSTNAME_MISMATCH]`, "[0, 62]\n"},
		{`require "openssl"; p [OpenSSL::X509::V_ERR_CRL_NOT_YET_VALID, OpenSSL::X509::V_ERR_CRL_HAS_EXPIRED]`, "[11, 12]\n"},
		{`require "openssl"; p [OpenSSL::X509::V_ERR_CERT_NOT_YET_VALID, OpenSSL::X509::V_ERR_CERT_HAS_EXPIRED]`, "[9, 10]\n"},
		{`require "openssl"; p [OpenSSL::SSL::VERIFY_NONE, OpenSSL::SSL::VERIFY_PEER, OpenSSL::SSL::VERIFY_FAIL_IF_NO_PEER_CERT, OpenSSL::SSL::VERIFY_CLIENT_ONCE]`, "[0, 1, 2, 4]\n"},
		{`require "openssl"; p [OpenSSL::SSL::OP_NO_SSLv2, OpenSSL::SSL::OP_NO_SSLv3, OpenSSL::SSL::OP_NO_TLSv1, OpenSSL::SSL::OP_ALL]`,
			"[16777216, 33554432, 67108864, 2147486719]\n"},
		// SSLContext is constructible; new -> initialize (no-op) -> SSLContext.
		{`require "openssl"; p OpenSSL::SSL::SSLContext.new.class.name`, "\"OpenSSL::SSL::SSLContext\"\n"},
		// DEFAULT_PARAMS carries the seeded keys.
		{`require "openssl"; p OpenSSL::SSL::SSLContext::DEFAULT_PARAMS[:verify_mode]`, "1\n"},
		{`require "openssl"; p OpenSSL::SSL::SSLContext::DEFAULT_PARAMS[:ciphers]`, "nil\n"},
		// set_params with an argument stores and returns @params.
		{`require "openssl"; ctx = OpenSSL::SSL::SSLContext.new; p ctx.set_params({a: 1})`, "{a: 1}\n"},
		// set_params with no argument: the len(args)>0 false branch -> @params (nil).
		{`require "openssl"; p OpenSSL::SSL::SSLContext.new.set_params`, "nil\n"},
		// Error classes descend from OpenSSL::OpenSSLError across every namespace.
		{`require "openssl"; p OpenSSL::X509::CertificateError < OpenSSL::OpenSSLError`, "true\n"},
		{`require "openssl"; p OpenSSL::PKey::RSAError < OpenSSL::OpenSSLError`, "true\n"},
		{`require "openssl"; p OpenSSL::SSL::SSLError < OpenSSL::OpenSSLError`, "true\n"},
		{`require "openssl"; p OpenSSL::ASN1::ASN1Error < OpenSSL::OpenSSLError`, "true\n"},
		{`require "openssl"; p OpenSSL::Cipher::CipherError < OpenSSL::OpenSSLError`, "true\n"},
	}
	for _, c := range cases {
		if got := osslEval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	// Every shell .new raises NotImplementedError (the notImpl helper), as does
	// SSL.verify_certificate_identity. Cover one shell per namespace plus the
	// top-level BN / Cipher. (OpenSSL::SSL::SSLSocket is no longer a shell — it is
	// a real crypto/tls transport, exercised in socket_test.go.)
	notImpl := []string{
		`OpenSSL::X509::Certificate.new`,
		`OpenSSL::X509::Name.new`,
		`OpenSSL::PKey::RSA.new`,
		`OpenSSL::ASN1::Sequence.new`,
		`OpenSSL::BN.new`,
		`OpenSSL::Cipher.new`,
		`OpenSSL::SSL.verify_certificate_identity("a", "b")`,
	}
	for _, src := range notImpl {
		full := `require "openssl"; ` + src
		if err := osslRunErr(t, full); err == nil || !strings.Contains(err.Error(), "NotImplementedError") {
			t.Errorf("src=%q got=%v want NotImplementedError", src, err)
		}
	}
}

// TestLastColonIndex covers lastColonIndex directly, including the no-colon
// "return -1" branch that is unreachable through the public OpenSSL API (every
// digest class name is namespaced, hence always contains a colon).
func TestLastColonIndex(t *testing.T) {
	if got := lastColonIndex("a:b:c"); got != 3 {
		t.Errorf("lastColonIndex(a:b:c) = %d, want 3", got)
	}
	if got := lastColonIndex("nocolon"); got != -1 {
		t.Errorf("lastColonIndex(nocolon) = %d, want -1", got)
	}
}
