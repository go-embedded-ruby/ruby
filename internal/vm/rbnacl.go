// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	sodium "github.com/go-ruby-sodium/sodium"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRbNaCl installs the RbNaCl module (require "rbnacl"): the SecretBox and
// Box authenticated-encryption classes, the PrivateKey/PublicKey and
// SigningKey/VerifyKey Curve25519/Ed25519 pairs, the Hash module, the Auth HMAC
// authenticators, PasswordHash, the AEAD constructions, GroupElement, Random, and
// the RbNaCl::CryptoError exception tree. All cryptography is delegated to
// github.com/go-ruby-sodium/sodium — a pure-Go, no-cgo port of the RbNaCl gem.
func (vm *VM) registerRbNaCl() {
	mod := newClass("RbNaCl", nil)
	mod.isModule = true
	vm.consts["RbNaCl"] = mod

	vm.registerRbNaClErrors(mod)
	vm.registerRbNaClRandom(mod)
	vm.registerRbNaClHash(mod)
	vm.registerRbNaClSecretBox(mod)
	vm.registerRbNaClKeys(mod)
	vm.registerRbNaClBox(mod)
	vm.registerRbNaClSign(mod)
	vm.registerRbNaClAuth(mod)
	vm.registerRbNaClPasswordHash(mod)
	vm.registerRbNaClAEAD(mod)
	vm.registerRbNaClGroupElement(mod)
}

// registerRbNaClErrors installs the RbNaCl exception tree, mirroring the gem:
// CryptoError < StandardError, and LengthError / BadAuthenticatorError /
// BadSignatureError < CryptoError. Each class is registered both as a nested
// constant of RbNaCl and under its qualified name in the top-level table, so a
// re-raised library error's exception lookup finds the same class.
func (vm *VM) registerRbNaClErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	crypto := reg("CryptoError", "RbNaCl::CryptoError", std)
	reg("LengthError", "RbNaCl::LengthError", crypto)
	reg("BadAuthenticatorError", "RbNaCl::BadAuthenticatorError", crypto)
	reg("BadSignatureError", "RbNaCl::BadSignatureError", crypto)
}

// smethod is a small helper: it installs a class ("static") method named name on
// c whose body is fn, matching how the other bindings populate smethods.
func smethod(c *RClass, name string, fn func(*VM, object.Value, []object.Value, *Proc) object.Value) {
	c.smethods[name] = &Method{name: name, owner: c, native: fn}
}

// registerRbNaClRandom installs RbNaCl::Random.random_bytes(n) → n cryptographically
// secure random bytes, delegating to the library's RandomBytes.
func (vm *VM) registerRbNaClRandom(mod *RClass) {
	c := newClass("RbNaCl::Random", nil)
	c.isModule = true
	mod.consts["Random"] = c
	smethod(c, "random_bytes", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return naclString(sodium.RandomBytes(int(intArg(args[0]))))
	})
}

// registerRbNaClHash installs the RbNaCl::Hash module: sha256 / sha512 one-shots
// and the keyword-configurable blake2b (digest_size:/key:/salt:/personal:).
func (vm *VM) registerRbNaClHash(mod *RClass) {
	c := newClass("RbNaCl::Hash", nil)
	c.isModule = true
	mod.consts["Hash"] = c
	smethod(c, "sha256", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return naclString(sodium.SHA256(naclBytes(args[0])))
	})
	smethod(c, "sha512", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return naclString(sodium.SHA512(naclBytes(args[0])))
	})
	smethod(c, "blake2b", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := sodium.Blake2b(naclBytes(args[0]), naclBlake2bOpts(args[1:]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return naclString(out)
	})
}

// registerRbNaClSecretBox installs RbNaCl::SecretBox (secret-key authenticated
// encryption): new(key), encrypt(nonce, message) / decrypt(nonce, ciphertext),
// the key/nonce size class methods and the KEYBYTES/NONCEBYTES/MACBYTES constants.
func (vm *VM) registerRbNaClSecretBox(mod *RClass) {
	c := newClass("RbNaCl::SecretBox", vm.cObject)
	mod.consts["SecretBox"] = c
	c.consts["KEYBYTES"] = object.IntValue(sodium.SecretBoxKeyBytes)
	c.consts["NONCEBYTES"] = object.IntValue(sodium.SecretBoxNonceBytes)
	c.consts["MACBYTES"] = object.IntValue(sodium.SecretBoxMACBytes)

	smethod(c, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		sb, err := sodium.NewSecretBox(naclBytes(args[0]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return &naclObj{cls: c, v: sb}
	})
	smethod(c, "key_bytes", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(sodium.SecretBoxKeyBytes)
	})
	smethod(c, "nonce_bytes", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(sodium.SecretBoxNonceBytes)
	})
	c.define("encrypt", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := self.(*naclObj).v.(*sodium.SecretBox).Encrypt(naclBytes(args[0]), naclBytes(args[1]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return naclString(out)
	})
	c.define("decrypt", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := self.(*naclObj).v.(*sodium.SecretBox).Decrypt(naclBytes(args[0]), naclBytes(args[1]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return naclString(out)
	})
}

// registerRbNaClKeys installs the Curve25519 key pair used by Box:
// RbNaCl::PrivateKey (generate / new / public_key / to_bytes) and
// RbNaCl::PublicKey (new / to_bytes), plus their BYTES constants.
func (vm *VM) registerRbNaClKeys(mod *RClass) {
	priv := newClass("RbNaCl::PrivateKey", vm.cObject)
	mod.consts["PrivateKey"] = priv
	priv.consts["BYTES"] = object.IntValue(sodium.PrivateKeyBytes)
	smethod(priv, "generate", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &naclObj{cls: priv, v: sodium.GeneratePrivateKey()}
	})
	smethod(priv, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pk, err := sodium.NewPrivateKey(naclBytes(args[0]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return &naclObj{cls: priv, v: pk}
	})

	pub := newClass("RbNaCl::PublicKey", vm.cObject)
	mod.consts["PublicKey"] = pub
	pub.consts["BYTES"] = object.IntValue(sodium.PublicKeyBytes)
	smethod(pub, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pk, err := sodium.NewPublicKey(naclBytes(args[0]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return &naclObj{cls: pub, v: pk}
	})

	priv.define("public_key", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &naclObj{cls: pub, v: self.(*naclObj).v.(*sodium.PrivateKey).PublicKey()}
	})
	priv.define("to_bytes", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return naclString(self.(*naclObj).v.(*sodium.PrivateKey).Bytes())
	})
	pub.define("to_bytes", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return naclString(self.(*naclObj).v.(*sodium.PublicKey).Bytes())
	})
}

// registerRbNaClBox installs RbNaCl::Box (public-key authenticated encryption):
// new(public_key, private_key) and encrypt/decrypt, plus the NONCEBYTES constant.
func (vm *VM) registerRbNaClBox(mod *RClass) {
	c := newClass("RbNaCl::Box", vm.cObject)
	mod.consts["Box"] = c
	c.consts["NONCEBYTES"] = object.IntValue(sodium.BoxNonceBytes)

	smethod(c, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		box := sodium.NewBox(naclPublicKey(args[0]), naclPrivateKey(args[1]))
		return &naclObj{cls: c, v: box}
	})
	c.define("encrypt", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := self.(*naclObj).v.(*sodium.Box).Encrypt(naclBytes(args[0]), naclBytes(args[1]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return naclString(out)
	})
	c.define("decrypt", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := self.(*naclObj).v.(*sodium.Box).Decrypt(naclBytes(args[0]), naclBytes(args[1]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return naclString(out)
	})
}

// registerRbNaClSign installs the Ed25519 signing pair: RbNaCl::SigningKey
// (generate / new(seed) / sign / verify_key / to_bytes) and RbNaCl::VerifyKey
// (new / verify / to_bytes), plus their BYTES / SIGNATUREBYTES constants.
func (vm *VM) registerRbNaClSign(mod *RClass) {
	sk := newClass("RbNaCl::SigningKey", vm.cObject)
	mod.consts["SigningKey"] = sk
	sk.consts["BYTES"] = object.IntValue(sodium.SignSeedBytes)
	sk.consts["SIGNATUREBYTES"] = object.IntValue(sodium.SignatureBytes)

	vk := newClass("RbNaCl::VerifyKey", vm.cObject)
	mod.consts["VerifyKey"] = vk
	vk.consts["BYTES"] = object.IntValue(sodium.VerifyKeyBytes)

	smethod(sk, "generate", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &naclObj{cls: sk, v: sodium.GenerateSigningKey()}
	})
	smethod(sk, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		k, err := sodium.NewSigningKey(naclBytes(args[0]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return &naclObj{cls: sk, v: k}
	})
	sk.define("sign", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return naclString(self.(*naclObj).v.(*sodium.SigningKey).Sign(naclBytes(args[0])))
	})
	sk.define("verify_key", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &naclObj{cls: vk, v: self.(*naclObj).v.(*sodium.SigningKey).VerifyKey()}
	})
	sk.define("to_bytes", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return naclString(self.(*naclObj).v.(*sodium.SigningKey).Seed())
	})

	smethod(vk, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		k, err := sodium.NewVerifyKey(naclBytes(args[0]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return &naclObj{cls: vk, v: k}
	})
	// VerifyKey#verify(signature, message) returns true on success and raises
	// RbNaCl::BadSignatureError on a forged signature (a wrong-length one raises
	// RbNaCl::LengthError), matching the gem.
	vk.define("verify", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self.(*naclObj).v.(*sodium.VerifyKey).Verify(naclBytes(args[0]), naclBytes(args[1])); err != nil {
			return raiseSodiumError(err)
		}
		return object.Bool(true)
	})
	vk.define("to_bytes", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return naclString(self.(*naclObj).v.(*sodium.VerifyKey).Bytes())
	})
}

// registerRbNaClAuth installs the RbNaCl::Auth HMAC authenticators as the
// HMACSHA256 / HMACSHA512 / HMACSHA512256 classes: new(key), auth(message) and
// verify(tag, message), each backed by the like-named library constructor.
func (vm *VM) registerRbNaClAuth(mod *RClass) {
	auth := newClass("RbNaCl::Auth", nil)
	auth.isModule = true
	mod.consts["Auth"] = auth

	define := func(simple string, ctor func([]byte) *sodium.Authenticator) {
		c := newClass("RbNaCl::Auth::"+simple, vm.cObject)
		auth.consts[simple] = c
		smethod(c, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &naclObj{cls: c, v: ctor(naclBytes(args[0]))}
		})
		c.define("auth", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return naclString(self.(*naclObj).v.(*sodium.Authenticator).Auth(naclBytes(args[0])))
		})
		// verify(tag, message) returns true on a valid tag and raises
		// RbNaCl::BadAuthenticatorError on a forged one.
		c.define("verify", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			if err := self.(*naclObj).v.(*sodium.Authenticator).Verify(naclBytes(args[0]), naclBytes(args[1])); err != nil {
				return raiseSodiumError(err)
			}
			return object.Bool(true)
		})
	}
	define("HMACSHA256", sodium.NewHMACSHA256)
	define("HMACSHA512", sodium.NewHMACSHA512)
	define("HMACSHA512256", sodium.NewHMACSHA512256)
}

// registerRbNaClPasswordHash installs RbNaCl::PasswordHash: scrypt (raising
// RbNaCl::CryptoError for invalid parameters) and the argon2i / argon2id
// password-hashing functions, delegating to the library.
func (vm *VM) registerRbNaClPasswordHash(mod *RClass) {
	c := newClass("RbNaCl::PasswordHash", nil)
	c.isModule = true
	mod.consts["PasswordHash"] = c

	smethod(c, "scrypt", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := sodium.Scrypt(naclBytes(args[0]), naclBytes(args[1]),
			int(intArg(args[2])), int(intArg(args[3])), int(intArg(args[4])), int(intArg(args[5])))
		if err != nil {
			return raiseSodiumError(err)
		}
		return naclString(out)
	})
	smethod(c, "argon2i", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return naclString(sodium.Argon2i(naclBytes(args[0]), naclBytes(args[1]),
			uint32(intArg(args[2])), uint32(intArg(args[3])), uint8(intArg(args[4])), uint32(intArg(args[5]))))
	})
	smethod(c, "argon2id", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return naclString(sodium.Argon2id(naclBytes(args[0]), naclBytes(args[1]),
			uint32(intArg(args[2])), uint32(intArg(args[3])), uint8(intArg(args[4])), uint32(intArg(args[5]))))
	})
}

// registerRbNaClAEAD installs the RbNaCl::AEAD constructions as the
// ChaCha20Poly1305IETF / XChaCha20Poly1305IETF classes: new(key),
// encrypt(nonce, message, additional_data = nil), decrypt(nonce, ciphertext,
// additional_data = nil) and the nonce_bytes reader.
func (vm *VM) registerRbNaClAEAD(mod *RClass) {
	aead := newClass("RbNaCl::AEAD", nil)
	aead.isModule = true
	mod.consts["AEAD"] = aead

	define := func(simple string, ctor func([]byte) (*sodium.AEAD, error)) {
		c := newClass("RbNaCl::AEAD::"+simple, vm.cObject)
		aead.consts[simple] = c
		smethod(c, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			a, err := ctor(naclBytes(args[0]))
			if err != nil {
				return raiseSodiumError(err)
			}
			return &naclObj{cls: c, v: a}
		})
		c.define("encrypt", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			out, err := self.(*naclObj).v.(*sodium.AEAD).Encrypt(naclBytes(args[0]), naclBytes(args[1]), naclOptBytes(args, 2))
			if err != nil {
				return raiseSodiumError(err)
			}
			return naclString(out)
		})
		c.define("decrypt", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			out, err := self.(*naclObj).v.(*sodium.AEAD).Decrypt(naclBytes(args[0]), naclBytes(args[1]), naclOptBytes(args, 2))
			if err != nil {
				return raiseSodiumError(err)
			}
			return naclString(out)
		})
		c.define("nonce_bytes", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.IntValue(int64(self.(*naclObj).v.(*sodium.AEAD).NonceBytes()))
		})
	}
	define("ChaCha20Poly1305IETF", sodium.NewChaCha20Poly1305IETF)
	define("XChaCha20Poly1305IETF", sodium.NewXChaCha20Poly1305IETF)
}

// registerRbNaClGroupElement installs RbNaCl::GroupElement (Curve25519 scalar
// multiplication): new(point), mult(scalar) → GroupElement, to_bytes, and the
// scalar_mult_base(scalar) class method (crypto_scalarmult_base, the public-key
// derivation), plus the BYTES constant.
func (vm *VM) registerRbNaClGroupElement(mod *RClass) {
	c := newClass("RbNaCl::GroupElement", vm.cObject)
	mod.consts["GroupElement"] = c
	c.consts["BYTES"] = object.IntValue(sodium.GroupElementBytes)

	smethod(c, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ge, err := sodium.NewGroupElement(naclBytes(args[0]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return &naclObj{cls: c, v: ge}
	})
	smethod(c, "scalar_mult_base", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := sodium.ScalarMultBase(naclBytes(args[0]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return naclString(out)
	})
	c.define("mult", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ge, err := self.(*naclObj).v.(*sodium.GroupElement).Mult(naclBytes(args[0]))
		if err != nil {
			return raiseSodiumError(err)
		}
		return &naclObj{cls: c, v: ge}
	})
	c.define("to_bytes", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return naclString(self.(*naclObj).v.(*sodium.GroupElement).Bytes())
	})
}
