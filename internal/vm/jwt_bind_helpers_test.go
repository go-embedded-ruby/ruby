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
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestJWTStringListScalar covers the single-String arm of jwtStringList and the
// no-algorithm arm of jwtAlgorithms, neither reached by the end-to-end tests
// (which pass Arrays / an algorithm: key).
func TestJWTStringListScalar(t *testing.T) {
	if got := jwtStringList(object.Wrap(object.NewString("HS256"))); len(got) != 1 || got[0] != "HS256" {
		t.Errorf("jwtStringList(string) = %v want [HS256]", got)
	}
	if got := jwtAlgorithms(object.NewHash()); got != nil {
		t.Errorf("jwtAlgorithms(empty) = %v want nil", got)
	}
}

// TestJWTParsePKCS1 covers the PKCS#1 private and public key parser arms (the
// end-to-end RSA test uses PKCS#8 / PKIX), plus the both-parses-fail nil returns.
func TestJWTParsePKCS1(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	priv := &pem.Block{Bytes: x509.MarshalPKCS1PrivateKey(key)}
	if k, ok := jwtParsePrivate(priv).(*rsa.PrivateKey); !ok || k.N.Cmp(key.N) != 0 {
		t.Errorf("jwtParsePrivate(PKCS1) = %v want the RSA key", k)
	}
	pub := &pem.Block{Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)}
	if k, ok := jwtParsePublic(pub).(*rsa.PublicKey); !ok || k.N.Cmp(key.N) != 0 {
		t.Errorf("jwtParsePublic(PKCS1) = %v want the RSA public key", k)
	}
	garbage := &pem.Block{Bytes: []byte("not-a-key")}
	if got := jwtParsePrivate(garbage); got != nil {
		t.Errorf("jwtParsePrivate(garbage) = %v want nil", got)
	}
	if got := jwtParsePublic(garbage); got != nil {
		t.Errorf("jwtParsePublic(garbage) = %v want nil", got)
	}
}

// TestJWTParsePKCS8EC covers the ECDSA arm of jwtParsePrivate's PKCS#8 branch,
// which the SEC1-encoded end-to-end ES test does not reach.
func TestJWTParsePKCS8EC(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	block := &pem.Block{Bytes: der}
	if k, ok := jwtParsePrivate(block).(*ecdsa.PrivateKey); !ok || k.X.Cmp(key.X) != 0 {
		t.Errorf("jwtParsePrivate(PKCS8 EC) = %v want the ECDSA key", k)
	}
}

// TestJWTPEMKeyVerifyPrivateFallback covers jwtPEMKey's verify path where only a
// private key is available (the public parse fails, the private parse succeeds).
func TestJWTPEMKeyVerifyPrivateFallback(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if k, ok := jwtPEMKey(privPEM, true).(*rsa.PrivateKey); !ok || k.N.Cmp(key.N) != 0 {
		t.Errorf("jwtPEMKey(priv, verify) = %v want the private key", k)
	}
}

// TestJWTPEMKeyBad covers jwtPEMKey's two failure exits: a non-PEM string (block
// == nil) and a PEM block whose bytes parse as neither a key on sign nor verify.
func TestJWTPEMKeyBad(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pem    string
		verify bool
	}{
		{"not-pem", "garbage", false},
		{"bad-body-sign", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("x")})), false},
		{"bad-body-verify", string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("x")})), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if re, ok := recover().(RubyError); !ok || re.Class != "JWT::DecodeError" {
					t.Errorf("jwtPEMKey(%s): got %v want JWT::DecodeError", tc.name, recover())
				}
			}()
			jwtPEMKey(tc.pem, tc.verify)
		})
	}
}

// TestBCryptOptsHashNil covers bcryptOptsHash's no-Hash arms: an empty rest and a
// non-Hash trailing argument both yield nil (Password.create without a cost:).
func TestBCryptOptsHashNil(t *testing.T) {
	if got := bcryptOptsHash(nil); got != nil {
		t.Errorf("bcryptOptsHash(nil) = %v want nil", got)
	}
	if got := bcryptOptsHash([]object.Value{object.Wrap(object.NewString("x"))}); got != nil {
		t.Errorf("bcryptOptsHash(non-hash) = %v want nil", got)
	}
}
