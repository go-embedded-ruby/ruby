// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
)

// This file drives the Ruby WebAuthn module (backed by
// github.com/go-ruby-webauthn/webauthn) entirely in-process: a deterministic
// software authenticator fabricates the registration (attestationObject +
// clientDataJSON) and authentication (authenticatorData + signature +
// clientDataJSON) client responses exactly as a browser would, so both ceremony
// verification paths run end to end without a network or a real security key. The
// ceremony challenge is the fixed 32-byte sequence 0x00..0x1f, reconstructed in
// Ruby as (0..31).map { |i| i.chr }.join.

const (
	waOrigin = "https://example.com"
	waRPID   = "example.com"
	waRPName = "Example RP"
)

// waChallenge is the stable 32-byte ceremony challenge (0x00..0x1f).
var waChallenge = func() []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}()

// waCredID is the stable credential ID the authenticator reports.
var waCredID = []byte("go-ruby-webauthn-cid")

// waSeeded is a deterministic reader so the fixture signing key is reproducible.
type waSeeded struct {
	ctr uint64
	buf []byte
}

func (r *waSeeded) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if len(r.buf) == 0 {
			var c [8]byte
			binary.BigEndian.PutUint64(c[:], r.ctr)
			r.ctr++
			sum := sha256.Sum256(append([]byte("go-ruby-webauthn-rbgo-fixture"), c[:]...))
			r.buf = sum[:]
		}
		m := copy(p[n:], r.buf)
		r.buf = r.buf[m:]
		n += m
	}
	return n, nil
}

// waAuthenticator is the deterministic software authenticator.
type waAuthenticator struct{ key *ecdsa.PrivateKey }

func newWaAuthenticator(t *testing.T) *waAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), &waSeeded{})
	if err != nil {
		t.Fatalf("generate fixture key: %v", err)
	}
	return &waAuthenticator{key: key}
}

func waB64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// coseKey returns the COSE_Key encoding of the authenticator public key.
func (a *waAuthenticator) coseKey() []byte {
	x := make([]byte, 32)
	y := make([]byte, 32)
	a.key.PublicKey.X.FillBytes(x)
	a.key.PublicKey.Y.FillBytes(y)
	cose := webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{
			KeyType:   int64(webauthncose.EllipticKey),
			Algorithm: int64(webauthncose.AlgES256),
		},
		Curve:  int64(webauthncose.P256),
		XCoord: x,
		YCoord: y,
	}
	out, err := webauthncbor.Marshal(&cose)
	if err != nil {
		panic(err)
	}
	return out
}

func waClientDataJSON(ceremonyType, challenge, origin string) []byte {
	out, err := json.Marshal(map[string]any{
		"type": ceremonyType, "challenge": challenge, "origin": origin, "crossOrigin": false,
	})
	if err != nil {
		panic(err)
	}
	return out
}

func waFlags(up, uv, at bool) byte {
	var f byte
	if up {
		f |= 0x01
	}
	if uv {
		f |= 0x04
	}
	if at {
		f |= 0x40
	}
	return f
}

// waReg tunes a fabricated registration response.
type waReg struct {
	challenge    []byte
	origin, rpID string
	up, uv       bool
	ceremony     string
	format       string
	extraAttStmt bool
}

func waDefaultReg() waReg {
	return waReg{challenge: waChallenge, origin: waOrigin, rpID: waRPID, up: true, uv: true,
		ceremony: "webauthn.create", format: "none"}
}

// registration returns the JSON client response for a registration ceremony.
func (a *waAuthenticator) registration(o waReg) string {
	cdj := waClientDataJSON(o.ceremony, waB64(o.challenge), o.origin)
	rpHash := sha256.Sum256([]byte(o.rpID))
	cose := a.coseKey()

	authData := append([]byte{}, rpHash[:]...)
	authData = append(authData, waFlags(o.up, o.uv, true))
	authData = append(authData, 0, 0, 0, 0)          // sign count 0
	authData = append(authData, make([]byte, 16)...) // AAGUID
	var cl [2]byte
	binary.BigEndian.PutUint16(cl[:], uint16(len(waCredID)))
	authData = append(authData, cl[:]...)
	authData = append(authData, waCredID...)
	authData = append(authData, cose...)

	attStmt := map[string]any{}
	if o.extraAttStmt {
		attStmt["bogus"] = 1
	}
	attObj, err := webauthncbor.Marshal(map[string]any{"fmt": o.format, "attStmt": attStmt, "authData": authData})
	if err != nil {
		panic(err)
	}
	return waMarshalResponse(map[string]any{"clientDataJSON": waB64(cdj), "attestationObject": waB64(attObj)})
}

// waGet tunes a fabricated authentication response.
type waGet struct {
	challenge    []byte
	origin, rpID string
	up, uv       bool
	ceremony     string
	signCount    uint32
	badSignature bool
}

func waDefaultGet() waGet {
	return waGet{challenge: waChallenge, origin: waOrigin, rpID: waRPID, up: true, uv: true,
		ceremony: "webauthn.get", signCount: 1}
}

// authentication returns the JSON client response for an authentication ceremony.
func (a *waAuthenticator) authentication(o waGet) string {
	_, jsonResp, _ := a.assertion(o)
	return jsonResp
}

// assertion builds an authentication response and also returns the raw signed
// message and signature (used to exercise WebAuthn::PublicKey#verify directly).
func (a *waAuthenticator) assertion(o waGet) (authData []byte, jsonResp string, _ string) {
	cdj := waClientDataJSON(o.ceremony, waB64(o.challenge), o.origin)
	rpHash := sha256.Sum256([]byte(o.rpID))
	ad := append([]byte{}, rpHash[:]...)
	ad = append(ad, waFlags(o.up, o.uv, false))
	var sc [4]byte
	binary.BigEndian.PutUint32(sc[:], o.signCount)
	ad = append(ad, sc[:]...)

	cdjHash := sha256.Sum256(cdj)
	signed := append(append([]byte{}, ad...), cdjHash[:]...)

	var sig []byte
	if o.badSignature {
		wrong := sha256.Sum256(append(signed, 0xff))
		s, err := ecdsa.SignASN1(rand.Reader, a.key, wrong[:])
		if err != nil {
			panic(err)
		}
		sig = s
	} else {
		h := sha256.Sum256(signed)
		s, err := ecdsa.SignASN1(rand.Reader, a.key, h[:])
		if err != nil {
			panic(err)
		}
		sig = s
	}
	resp := waMarshalResponse(map[string]any{
		"clientDataJSON": waB64(cdj), "authenticatorData": waB64(ad), "signature": waB64(sig),
	})
	return signed, resp, waHex(sig)
}

func waMarshalResponse(response map[string]any) string {
	out, err := json.Marshal(map[string]any{
		"type": "public-key", "id": waB64(waCredID), "rawId": waB64(waCredID),
		"response": response, "clientExtensionResults": map[string]any{},
	})
	if err != nil {
		panic(err)
	}
	return string(out)
}

func waHex(b []byte) string { return hex.EncodeToString(b) }

// waSetup is the Ruby prelude: it requires the module, builds the relying party,
// the fixed challenge, and a verified registration credential (CRED).
func waSetup(a *waAuthenticator) string {
	return fmt.Sprintf(`require "webauthn"
RP = WebAuthn::RelyingParty.new(origin: %q, name: %q, id: %q)
CH = (0..31).map { |i| i.chr }.join
CRED = RP.verify_registration('%s', CH)
`, waOrigin, waRPName, waRPID, a.registration(waDefaultReg()))
}

// TestWebAuthn covers the successful ceremony surface: the registration and
// authentication round-trip, the credential/public-key readers, the RelyingParty
// readers, and the options_for_create / options_for_get option objects.
func TestWebAuthn(t *testing.T) {
	a := newWaAuthenticator(t)
	setup := waSetup(a)
	authJSON := a.authentication(waDefaultGet())

	rp2 := fmt.Sprintf(`require "webauthn"
RP2 = WebAuthn::RelyingParty.new(origin: %q, name: %q, id: %q, algorithms: [:ES256, "RS256"], timeout: 60000)
CH = (0..31).map { |i| i.chr }.join
`, waOrigin, waRPName, waRPID)

	au := fmt.Sprintf(`AU = RP.verify_authentication('%s', CH, public_key: CRED.public_key, sign_count: 0); `, authJSON)

	for _, c := range []struct{ src, want string }{
		// Registration credential readers.
		{setup + `p CRED.id == "go-ruby-webauthn-cid"`, "true\n"},
		{setup + `p CRED.sign_count`, "0\n"},
		{setup + `p CRED.attestation_format`, "\"none\"\n"},
		{setup + `p CRED.attestation_type.class`, "String\n"},
		{setup + `p CRED.public_key.class`, "WebAuthn::PublicKey\n"},
		{setup + `p CRED.public_key.cose_key.bytesize > 0`, "true\n"},
		{setup + `p CRED.class`, "WebAuthn::Credential\n"},
		// Authentication round-trip (credential public key object).
		{setup + au + `p AU.sign_count`, "1\n"},
		{setup + au + `p AU.id == "go-ruby-webauthn-cid"`, "true\n"},
		{setup + au + `p AU.public_key`, "nil\n"},
		{setup + au + `p AU.attestation_format`, "nil\n"},
		{setup + au + `p AU.attestation_type`, "nil\n"},
		// Authentication accepting the COSE key as a raw String.
		{setup + fmt.Sprintf(`puts RP.verify_authentication('%s', CH, public_key: CRED.public_key.cose_key, sign_count: 0).sign_count`, authJSON), "1\n"},
		// WebAuthn::PublicKey#verify (true and false) over the stored key.
		{setup + waVerifyCase(a), "true\nfalse\n"},
		// PublicKey.new from raw COSE bytes.
		{setup + `p WebAuthn::PublicKey.new(CRED.public_key.cose_key).class`, "WebAuthn::PublicKey\n"},

		// RelyingParty readers + wrapper to_s/inspect/truthiness.
		{rp2 + `p RP2.origin`, "\"https://example.com\"\n"},
		{rp2 + `p RP2.name`, "\"Example RP\"\n"},
		{rp2 + `p RP2.id`, "\"example.com\"\n"},
		{rp2 + `p RP2.algorithms`, "[\"ES256\", \"RS256\"]\n"},
		{rp2 + `puts RP2`, "#<WebAuthn::RelyingParty example.com>\n"},
		{rp2 + `p RP2`, "#<WebAuthn::RelyingParty example.com>\n"},
		{rp2 + `p(RP2 ? true : false)`, "true\n"},

		// options_for_create option object.
		{rp2 + `O = RP2.options_for_create(user: {id: "u1", name: "alice", display_name: "Alice"}, exclude: "c1", user_verification: "required", attestation: "none", challenge: CH); p O.challenge == CH`, "true\n"},
		{rp2 + `O = RP2.options_for_create(user: {id: "u1"}, challenge: CH); p O.rp_id`, "\"example.com\"\n"},
		{rp2 + `O = RP2.options_for_create(user: {id: "u1"}, challenge: CH); p O.rp_name`, "\"Example RP\"\n"},
		{rp2 + `O = RP2.options_for_create(user: {id: "u1"}, challenge: CH); p O.user_id`, "\"u1\"\n"},
		{rp2 + `O = RP2.options_for_create(user: {id: "u1"}, challenge: CH); p O.timeout`, "60000\n"},
		{rp2 + `O = RP2.options_for_create(user: {id: "u1"}, challenge: CH); p O.algorithms`, "[-7, -257]\n"},
		{rp2 + `O = RP2.options_for_create(user: {id: "u1"}, exclude: "c1", challenge: CH); p O.exclude_credentials`, "[\"c1\"]\n"},
		{rp2 + `O = RP2.options_for_create(user: {}, exclude: ["a", "bb"], challenge: CH); p O.exclude_credentials`, "[\"a\", \"bb\"]\n"},
		{rp2 + `O = RP2.options_for_create(user: {id: "u1"}, challenge: CH); p O.class`, "WebAuthn::PublicKeyCredentialCreationOptions\n"},
		{rp2 + `O = RP2.options_for_create(user: {id: "u1"}, challenge: CH); puts O`, "#<WebAuthn::PublicKeyCredentialCreationOptions>\n"},
		{rp2 + `O = RP2.options_for_create(user: {id: "u1"}, challenge: CH); p O`, "#<WebAuthn::PublicKeyCredentialCreationOptions>\n"},
		{rp2 + `O = RP2.options_for_create(user: {id: "u1"}, challenge: CH); p(O ? true : false)`, "true\n"},
		// Minimal create with a fresh random challenge (absent-option branches).
		{rp2 + `puts RP2.options_for_create(user: {}).challenge.bytesize`, "32\n"},

		// options_for_get option object.
		{rp2 + `G = RP2.options_for_get(allow: ["c1", "c2"], user_verification: "preferred", challenge: CH); p G.challenge == CH`, "true\n"},
		{rp2 + `G = RP2.options_for_get(challenge: CH); p G.rp_id`, "\"example.com\"\n"},
		{rp2 + `G = RP2.options_for_get(challenge: CH); p G.timeout`, "60000\n"},
		{rp2 + `G = RP2.options_for_get(allow: ["c1", "c2"], challenge: CH); p G.allow_credentials`, "[\"c1\", \"c2\"]\n"},
		{rp2 + `G = RP2.options_for_get(allow: "solo", challenge: CH); p G.allow_credentials`, "[\"solo\"]\n"},
		{rp2 + `G = RP2.options_for_get(user_verification: "preferred", challenge: CH); p G.user_verification`, "\"preferred\"\n"},
		{rp2 + `G = RP2.options_for_get(challenge: CH); p G.class`, "WebAuthn::PublicKeyCredentialRequestOptions\n"},
		{rp2 + `G = RP2.options_for_get(challenge: CH); puts G`, "#<WebAuthn::PublicKeyCredentialRequestOptions>\n"},
		{rp2 + `G = RP2.options_for_get(challenge: CH); p G`, "#<WebAuthn::PublicKeyCredentialRequestOptions>\n"},
		{rp2 + `G = RP2.options_for_get(challenge: CH); p(G ? true : false)`, "true\n"},
		// Minimal get (no arguments): empty allow list, absent-option branches.
		{rp2 + `p RP2.options_for_get.allow_credentials`, "[]\n"},

		// Credential/PublicKey wrapper to_s/inspect/truthiness.
		{setup + `puts CRED`, "#<WebAuthn::Credential>\n"},
		{setup + `p CRED`, "#<WebAuthn::Credential>\n"},
		{setup + `p(CRED ? true : false)`, "true\n"},
		{setup + `puts CRED.public_key`, "#<WebAuthn::PublicKey>\n"},
		{setup + `p CRED.public_key`, "#<WebAuthn::PublicKey>\n"},
		{setup + `p(CRED.public_key ? true : false)`, "true\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// waVerifyCase builds a script that exercises WebAuthn::PublicKey#verify with a
// matching (true) and a tampered (false) signature over the assertion's signed
// message, using pack("H*") to carry the raw bytes into Ruby.
func waVerifyCase(a *waAuthenticator) string {
	signed, _, sigHex := a.assertion(waDefaultGet())
	return fmt.Sprintf(`PK = CRED.public_key
DATA = [%q].pack("H*")
SIG = [%q].pack("H*")
p PK.verify(DATA, SIG)
p PK.verify(DATA + "x", SIG)
`, waHex(signed), sigHex)
}

// TestWebAuthnErrors covers the argument-validation and verification failure
// branches: the ArgumentError/TypeError guards and the WebAuthn::Error subclass
// tree re-raised from tampered ceremony responses.
func TestWebAuthnErrors(t *testing.T) {
	a := newWaAuthenticator(t)
	req := fmt.Sprintf(`require "webauthn"
RP = WebAuthn::RelyingParty.new(origin: %q, name: %q, id: %q)
CH = (0..31).map { |i| i.chr }.join
OTHER = (1..32).map { |i| i.chr }.join
`, waOrigin, waRPName, waRPID)

	reg := func(o waReg) string { return a.registration(o) }
	getj := func(o waGet) string { return a.authentication(o) }

	tamper := func(mut func(*waReg)) string {
		o := waDefaultReg()
		mut(&o)
		return reg(o)
	}
	tamperGet := func(mut func(*waGet)) string {
		o := waDefaultGet()
		mut(&o)
		return getj(o)
	}

	authOK := getj(waDefaultGet())

	for _, c := range []struct{ src, want string }{
		// Argument validation.
		{`require "webauthn"; WebAuthn::RelyingParty.new(name: "n", id: "i")`, "ArgumentError"},
		{`require "webauthn"; WebAuthn::RelyingParty.new(origin: "o", name: "n", id: "i", algorithms: "ES256")`, "TypeError"},
		{`require "webauthn"; WebAuthn::RelyingParty.new(origin: "o", name: "n", id: "i", algorithms: [1])`, "TypeError"},
		{req + `RP.verify_registration("only-one")`, "ArgumentError"},
		{req + `RP.verify_authentication("only-one")`, "ArgumentError"},
		{req + `RP.verify_authentication("a", "b", sign_count: 0)`, "ArgumentError"},
		{req + `RP.verify_authentication("a", "b", public_key: "k")`, "ArgumentError"},
		{req + `RP.verify_registration(123, CH)`, "TypeError"},
		{req + `WebAuthn::PublicKey.new`, "ArgumentError"},
		{req + `WebAuthn::PublicKey.new("garbage")`, "WebAuthn::ClientDataMissingError"},
		{req + `RP.options_for_create(challenge: CH)`, "ArgumentError"},
		{req + `RP.options_for_create(user: "x")`, "TypeError"},
		{req + `RP.options_for_create(user: {}, exclude: 123)`, "TypeError"},
		{req + fmt.Sprintf(`c = RP.verify_registration('%s', CH); c.public_key.verify("only-one")`, reg(waDefaultReg())), "ArgumentError"},

		// Registration verification failures (WebAuthn::Error subclasses).
		{req + `RP.verify_registration("not valid json", CH)`, "WebAuthn::ClientDataMissingError"},
		{req + fmt.Sprintf(`RP.verify_registration('%s', OTHER)`, reg(waDefaultReg())), "WebAuthn::ChallengeVerificationError"},
		{req + fmt.Sprintf(`RP.verify_registration('%s', CH)`, tamper(func(o *waReg) { o.origin = "https://evil.example" })), "WebAuthn::OriginVerificationError"},
		{req + fmt.Sprintf(`RP.verify_registration('%s', CH)`, tamper(func(o *waReg) { o.ceremony = "webauthn.get" })), "WebAuthn::TypeVerificationError"},
		{req + fmt.Sprintf(`RP.verify_registration('%s', CH)`, tamper(func(o *waReg) { o.rpID = "other.example" })), "WebAuthn::RpIdVerificationError"},
		{req + fmt.Sprintf(`RP.verify_registration('%s', CH)`, tamper(func(o *waReg) { o.up = false })), "WebAuthn::UserPresenceVerificationError"},
		{req + fmt.Sprintf(`RP.verify_registration('%s', CH, user_verification: true)`, tamper(func(o *waReg) { o.uv = false })), "WebAuthn::UserVerificationError"},
		{req + fmt.Sprintf(`RP.verify_registration('%s', CH)`, tamper(func(o *waReg) { o.extraAttStmt = true })), "WebAuthn::AttestationStatementVerificationError"},

		// Authentication verification failures.
		{req + fmt.Sprintf(`RP.verify_authentication('%s', CH, public_key: "bad-cose", sign_count: 0)`, authOK), "WebAuthn::ClientDataMissingError"},
		{req + fmt.Sprintf(`c = RP.verify_registration('%s', CH); RP.verify_authentication('%s', CH, public_key: c.public_key, sign_count: 0)`,
			reg(waDefaultReg()), tamperGet(func(o *waGet) { o.badSignature = true })), "WebAuthn::SignatureVerificationError"},
		{req + fmt.Sprintf(`c = RP.verify_registration('%s', CH); RP.verify_authentication('%s', CH, public_key: c.public_key, sign_count: 5)`,
			reg(waDefaultReg()), tamperGet(func(o *waGet) { o.signCount = 0 })), "WebAuthn::SignCountVerificationError"},
		// Trailing non-Hash argument to verify_registration (waKwargs fallback).
		{req + fmt.Sprintf(`p RP.verify_registration('%s', CH, "extra").sign_count`, reg(waDefaultReg())), ""},
	} {
		err := runErr(t, c.src)
		if c.want == "" {
			if err != nil {
				t.Errorf("src=%q: unexpected error %v", c.src, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q: got err=%v, want containing %q", c.src, err, c.want)
		}
	}
}
