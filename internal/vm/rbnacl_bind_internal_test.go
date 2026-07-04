// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRbNaClSecretBox drives RbNaCl::SecretBox through rbgo end to end: an
// encrypt/decrypt round-trip recovers the plaintext, a tampered ciphertext raises
// RbNaCl::BadAuthenticatorError, a mis-sized key raises RbNaCl::LengthError, and
// the key/nonce/MAC sizes are exposed both as constants and as class methods.
func TestRbNaClSecretBox(t *testing.T) {
	src := `
require "rbnacl"
key = RbNaCl::Random.random_bytes(RbNaCl::SecretBox::KEYBYTES)
box = RbNaCl::SecretBox.new(key)
nonce = RbNaCl::Random.random_bytes(RbNaCl::SecretBox.nonce_bytes)
ct = box.encrypt(nonce, "attack at dawn")
pt = box.decrypt(nonce, ct)
tampered = false
begin
  box.decrypt(nonce, ct + "!")
rescue RbNaCl::BadAuthenticatorError
  tampered = true
end
lenerr = false
begin
  RbNaCl::SecretBox.new("short")
rescue RbNaCl::LengthError
  lenerr = true
end
noncerr = false
begin
  box.encrypt("short", "x")
rescue RbNaCl::LengthError
  noncerr = true
end
puts [pt, tampered, lenerr, noncerr, RbNaCl::SecretBox.key_bytes, RbNaCl::SecretBox::MACBYTES, RbNaCl::SecretBox::NONCEBYTES].join(",")
`
	if got, want := runSrc(t, src), "attack at dawn,true,true,true,32,16,24"; got != want {
		t.Fatalf("secretbox = %q want %q", got, want)
	}
}

// TestRbNaClBox drives RbNaCl::Box public-key encryption: Alice→Bob round-trips,
// keys survive a to_bytes/new round-trip, a bad nonce length raises LengthError,
// and Box.new rejects non-key arguments with TypeError.
func TestRbNaClBox(t *testing.T) {
	src := `
require "rbnacl"
alice = RbNaCl::PrivateKey.generate
bob = RbNaCl::PrivateKey.generate
abox = RbNaCl::Box.new(bob.public_key, alice)
bbox = RbNaCl::Box.new(alice.public_key, bob)
nonce = RbNaCl::Random.random_bytes(RbNaCl::Box::NONCEBYTES)
ct = abox.encrypt(nonce, "hi bob")
pt = bbox.decrypt(nonce, ct)
tampered = false
begin
  bbox.decrypt(nonce, ct + "!")
rescue RbNaCl::BadAuthenticatorError
  tampered = true
end
nonceerr = false
begin
  abox.encrypt("short", "x")
rescue RbNaCl::LengthError
  nonceerr = true
end
priv2 = RbNaCl::PrivateKey.new(alice.to_bytes)
pub2 = RbNaCl::PublicKey.new(alice.public_key.to_bytes)
tperr = 0
begin; RbNaCl::Box.new("nope", alice); rescue TypeError; tperr += 1; end
begin; RbNaCl::Box.new(bob.public_key, "nope"); rescue TypeError; tperr += 1; end
puts [pt, tampered, nonceerr, priv2.to_bytes == alice.to_bytes, pub2.to_bytes == alice.public_key.to_bytes, tperr, RbNaCl::PrivateKey::BYTES, RbNaCl::PublicKey::BYTES].join(",")
`
	if got, want := runSrc(t, src), "hi bob,true,true,true,true,2,32,32"; got != want {
		t.Fatalf("box = %q want %q", got, want)
	}
}

// TestRbNaClKeyLengthErrors proves the key constructors reject mis-sized inputs
// with RbNaCl::LengthError (which is an RbNaCl::CryptoError, a StandardError).
func TestRbNaClKeyLengthErrors(t *testing.T) {
	src := `
require "rbnacl"
n = 0
[->{ RbNaCl::PrivateKey.new("x") },
 ->{ RbNaCl::PublicKey.new("x") },
 ->{ RbNaCl::SigningKey.new("x") },
 ->{ RbNaCl::VerifyKey.new("x") }].each do |f|
  begin
    f.call
  rescue RbNaCl::LengthError
    n += 1
  end
end
# the tree: LengthError < CryptoError < StandardError
is_crypto = false
begin
  RbNaCl::PublicKey.new("x")
rescue RbNaCl::CryptoError
  is_crypto = true
end
puts [n, is_crypto].join(",")
`
	if got, want := runSrc(t, src), "4,true"; got != want {
		t.Fatalf("key length errors = %q want %q", got, want)
	}
}

// TestRbNaClSign drives RbNaCl::SigningKey / VerifyKey: a signature verifies for
// the signed message, a forged one raises RbNaCl::BadSignatureError, a
// wrong-length signature raises RbNaCl::LengthError, and seeds/keys survive a
// to_bytes/new round-trip.
func TestRbNaClSign(t *testing.T) {
	src := `
require "rbnacl"
sk = RbNaCl::SigningKey.generate
vk = sk.verify_key
sig = sk.sign("message")
ok = vk.verify(sig, "message")
forged = false
begin
  vk.verify(sig, "MESSAGE")
rescue RbNaCl::BadSignatureError
  forged = true
end
siglen = false
begin
  vk.verify("short", "message")
rescue RbNaCl::LengthError
  siglen = true
end
sk2 = RbNaCl::SigningKey.new(sk.to_bytes)
vk2 = RbNaCl::VerifyKey.new(vk.to_bytes)
puts [ok, forged, siglen, sk2.to_bytes == sk.to_bytes, vk2.to_bytes == vk.to_bytes, sig.bytesize, RbNaCl::SigningKey::BYTES, RbNaCl::SigningKey::SIGNATUREBYTES, RbNaCl::VerifyKey::BYTES].join(",")
`
	if got, want := runSrc(t, src), "true,true,true,true,true,64,32,64,32"; got != want {
		t.Fatalf("sign = %q want %q", got, want)
	}
}

// TestRbNaClHash drives RbNaCl::Hash: sha256/sha512 have the right sizes and are
// deterministic, blake2b honours digest_size: and key:, and the unsupported
// salt:/personal: parameters raise RbNaCl::CryptoError.
func TestRbNaClHash(t *testing.T) {
	src := `
require "rbnacl"
h1 = RbNaCl::Hash.sha256("abc")
h2 = RbNaCl::Hash.sha256("abc")
h3 = RbNaCl::Hash.sha512("abc")
b1 = RbNaCl::Hash.blake2b("abc")
b2 = RbNaCl::Hash.blake2b("abc", digest_size: 16)
b3 = RbNaCl::Hash.blake2b("abc", key: RbNaCl::Random.random_bytes(32))
salterr = false
begin
  RbNaCl::Hash.blake2b("abc", salt: "0123456789abcdef")
rescue RbNaCl::CryptoError
  salterr = true
end
prserr = false
begin
  RbNaCl::Hash.blake2b("abc", personal: "0123456789abcdef")
rescue RbNaCl::CryptoError
  prserr = true
end
puts [h1.bytesize, h1 == h2, h3.bytesize, b1.bytesize, b2.bytesize, b3.bytesize, salterr, prserr].join(",")
`
	if got, want := runSrc(t, src), "32,true,64,32,16,32,true,true"; got != want {
		t.Fatalf("hash = %q want %q", got, want)
	}
}

// TestRbNaClAuth drives the RbNaCl::Auth HMAC authenticators: a tag verifies for
// the authenticated message, a forged one raises RbNaCl::BadAuthenticatorError,
// and the SHA-256/512/512256 variants yield their expected tag sizes.
func TestRbNaClAuth(t *testing.T) {
	src := `
require "rbnacl"
key = RbNaCl::Random.random_bytes(32)
a = RbNaCl::Auth::HMACSHA256.new(key)
tag = a.auth("data")
ok = a.verify(tag, "data")
forged = false
begin
  a.verify(tag, "other")
rescue RbNaCl::BadAuthenticatorError
  forged = true
end
b = RbNaCl::Auth::HMACSHA512.new(key)
c = RbNaCl::Auth::HMACSHA512256.new(key)
puts [tag.bytesize, ok, forged, b.auth("x").bytesize, c.auth("x").bytesize].join(",")
`
	if got, want := runSrc(t, src), "32,true,true,64,32"; got != want {
		t.Fatalf("auth = %q want %q", got, want)
	}
}

// TestRbNaClAEAD drives the RbNaCl::AEAD constructions: an encrypt/decrypt
// round-trip with and without additional data recovers the plaintext, a tampered
// ciphertext raises RbNaCl::BadAuthenticatorError, a mis-sized key raises
// RbNaCl::LengthError, and the two nonce sizes are reported.
func TestRbNaClAEAD(t *testing.T) {
	src := `
require "rbnacl"
key = RbNaCl::Random.random_bytes(32)
aead = RbNaCl::AEAD::ChaCha20Poly1305IETF.new(key)
nonce = RbNaCl::Random.random_bytes(aead.nonce_bytes)
ct = aead.encrypt(nonce, "secret", "header")
pt = aead.decrypt(nonce, ct, "header")
ct2 = aead.encrypt(nonce, "plain2")
pt2 = aead.decrypt(nonce, ct2)
forged = false
begin
  aead.decrypt(nonce, ct + "!", "header")
rescue RbNaCl::BadAuthenticatorError
  forged = true
end
xa = RbNaCl::AEAD::XChaCha20Poly1305IETF.new(key)
lenerr = false
begin
  RbNaCl::AEAD::ChaCha20Poly1305IETF.new("short")
rescue RbNaCl::LengthError
  lenerr = true
end
noncerr = false
begin
  aead.encrypt("short", "x", "header")
rescue RbNaCl::LengthError
  noncerr = true
end
puts [pt, pt2, forged, xa.nonce_bytes, aead.nonce_bytes, lenerr, noncerr].join(",")
`
	if got, want := runSrc(t, src), "secret,plain2,true,24,12,true,true"; got != want {
		t.Fatalf("aead = %q want %q", got, want)
	}
}

// TestRbNaClPasswordHash drives RbNaCl::PasswordHash: scrypt/argon2i/argon2id
// return digests of the requested size, and invalid scrypt parameters (a cost
// that is not a power of two) raise RbNaCl::CryptoError.
func TestRbNaClPasswordHash(t *testing.T) {
	src := `
require "rbnacl"
salt = RbNaCl::Random.random_bytes(16)
h = RbNaCl::PasswordHash.scrypt("password", salt, 16384, 8, 1, 32)
scerr = false
begin
  RbNaCl::PasswordHash.scrypt("password", salt, 3, 8, 1, 32)
rescue RbNaCl::CryptoError
  scerr = true
end
a1 = RbNaCl::PasswordHash.argon2i("password", RbNaCl::Random.random_bytes(16), 3, 4096, 1, 32)
a2 = RbNaCl::PasswordHash.argon2id("password", RbNaCl::Random.random_bytes(16), 3, 4096, 1, 32)
puts [h.bytesize, scerr, a1.bytesize, a2.bytesize].join(",")
`
	if got, want := runSrc(t, src), "32,true,32,32"; got != want {
		t.Fatalf("passwordhash = %q want %q", got, want)
	}
}

// TestRbNaClGroupElement drives RbNaCl::GroupElement scalar multiplication: a
// Diffie-Hellman shared secret agrees from both sides, scalar_mult_base derives a
// public key, and mis-sized inputs raise RbNaCl::LengthError.
func TestRbNaClGroupElement(t *testing.T) {
	src := `
require "rbnacl"
a = RbNaCl::Random.random_bytes(32)
b = RbNaCl::Random.random_bytes(32)
apub = RbNaCl::GroupElement.scalar_mult_base(a)
bpub = RbNaCl::GroupElement.scalar_mult_base(b)
sa = RbNaCl::GroupElement.new(bpub).mult(a)
sb = RbNaCl::GroupElement.new(apub).mult(b)
agree = sa.to_bytes == sb.to_bytes
geerr = false
begin
  RbNaCl::GroupElement.new("short")
rescue RbNaCl::LengthError
  geerr = true
end
mulerr = false
begin
  RbNaCl::GroupElement.new(apub).mult("short")
rescue RbNaCl::LengthError
  mulerr = true
end
smberr = false
begin
  RbNaCl::GroupElement.scalar_mult_base("short")
rescue RbNaCl::LengthError
  smberr = true
end
puts [agree, apub.bytesize, sa.to_bytes.bytesize, geerr, mulerr, smberr, RbNaCl::GroupElement::BYTES].join(",")
`
	if got, want := runSrc(t, src), "true,32,32,true,true,true,32"; got != want {
		t.Fatalf("groupelement = %q want %q", got, want)
	}
}

// TestRbNaClRequireAndInspect proves require "rbnacl" reports the feature as
// provided (true on first load, false after) and that a value object renders its
// class-tagged to_s / inspect form through Object's defaults.
func TestRbNaClRequireAndInspect(t *testing.T) {
	src := `
r = []
r << require("rbnacl")
r << require("rbnacl")
r << RbNaCl::SigningKey.generate.to_s
r << RbNaCl::SigningKey.generate.inspect
r << (RbNaCl::SigningKey.generate ? "truthy" : "falsey")
puts r.join(",")
`
	if got, want := runSrc(t, src), "true,false,#<RbNaCl::SigningKey>,#<RbNaCl::SigningKey>,truthy"; got != want {
		t.Fatalf("require/inspect = %q want %q", got, want)
	}
}
