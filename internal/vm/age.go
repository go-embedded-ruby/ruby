// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	age "github.com/go-ruby-age/age"
	"github.com/go-ruby-age/age/scrypt"
	"github.com/go-ruby-age/age/x25519"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// ageGenerate is the X25519 key-pair source behind Age::X25519::Identity.generate,
// indirected as a package variable so a test can substitute a failing generator
// to drive the otherwise-unreachable randomness-failure branch.
var ageGenerate = x25519.Generate

// registerAge installs the Age module (require "age"): the Age.encrypt /
// Age.decrypt one-shots, the Age::X25519 and Age::Scrypt recipient/identity
// namespaces, the armor: option and the Age::Error exception tree. All
// cryptography is delegated to github.com/go-ruby-age/age — a pure-Go, no-cgo
// façade over filippo.io/age, the reference age implementation — so a file rbgo
// emits is read by the reference age CLI and vice-versa. The instance value
// types and the recipient/identity conversions live in age_bind.go.
func (vm *VM) registerAge() {
	mod := newClass("Age", nil)
	mod.isModule = true
	vm.consts["Age"] = mod

	vm.registerAgeErrors(mod)
	vm.registerAgeX25519(mod)
	vm.registerAgeScrypt(mod)
	vm.registerAgeOneShots(mod)
}

// registerAgeErrors installs the Age::Error exception tree mirroring the gem
// (Error < StandardError; the specific failures < Error). Each class is
// registered both as a nested constant of Age and under its qualified name in
// the top-level table (so a re-raised library sentinel's exception lookup finds
// the same class), exactly as the JSON and BCrypt error trees are.
func (vm *VM) registerAgeErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Error", "Age::Error", std)
	reg("NoIdentityMatchError", "Age::NoIdentityMatchError", base)
	reg("IncorrectPassphraseError", "Age::IncorrectPassphraseError", base)
	reg("ParseError", "Age::ParseError", base)
	reg("FormatError", "Age::FormatError", base)
	reg("EncryptError", "Age::EncryptError", base)
}

// registerAgeX25519 installs the Age::X25519 namespace: the Identity key pair
// (generate / from_string / to_s / to_public) and the Recipient public key
// (from_string / to_s), delegating to go-ruby-age's x25519 package.
func (vm *VM) registerAgeX25519(mod *RClass) {
	ns := newClass("Age::X25519", nil)
	ns.isModule = true
	mod.consts["X25519"] = ns

	rc := newClass("Age::X25519::Recipient", vm.cObject)
	ns.consts["Recipient"] = rc

	ic := newClass("Age::X25519::Identity", vm.cObject)
	ns.consts["Identity"] = ic

	// Age::X25519::Identity.generate → a fresh X25519 key pair. Generation is
	// indirected through ageGenerate so a test can drive the otherwise-unreachable
	// randomness-failure branch.
	ic.smethods["generate"] = &Method{name: "generate", owner: ic,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			id, err := ageGenerate()
			if err != nil {
				raiseAgeError(err)
			}
			return &AgeX25519Identity{cls: ic, id: id}
		}}

	// Age::X25519::Identity.from_string("AGE-SECRET-KEY-1…") parses a secret key,
	// raising Age::ParseError for a malformed one.
	ic.smethods["from_string"] = &Method{name: "from_string", owner: ic,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			id, err := x25519.ParseIdentity(strArg(args[0]))
			if err != nil {
				raiseAgeError(err)
			}
			return &AgeX25519Identity{cls: ic, id: id}
		}}

	// Age::X25519::Identity#to_public → the matching Age::X25519::Recipient.
	ic.define("to_public", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &AgeX25519Recipient{cls: rc, rcp: self.(*AgeX25519Identity).id.ToPublic()}
	})
	ageDefineToS(ic, func(self object.Value) string { return self.(*AgeX25519Identity).id.String() })

	// Age::X25519::Recipient.from_string("age1…") parses a recipient, raising
	// Age::ParseError for a malformed one.
	rc.smethods["from_string"] = &Method{name: "from_string", owner: rc,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			r, err := x25519.ParseRecipient(strArg(args[0]))
			if err != nil {
				raiseAgeError(err)
			}
			return &AgeX25519Recipient{cls: rc, rcp: r}
		}}
	ageDefineToS(rc, func(self object.Value) string { return self.(*AgeX25519Recipient).rcp.String() })
}

// registerAgeScrypt installs the Age::Scrypt namespace: the passphrase-based
// Recipient (new + work_factor:) and Identity (new + max_work_factor:),
// delegating to go-ruby-age's scrypt package. An scrypt recipient must be the
// sole recipient of a message.
func (vm *VM) registerAgeScrypt(mod *RClass) {
	ns := newClass("Age::Scrypt", nil)
	ns.isModule = true
	mod.consts["Scrypt"] = ns

	rc := newClass("Age::Scrypt::Recipient", vm.cObject)
	ns.consts["Recipient"] = rc

	ic := newClass("Age::Scrypt::Identity", vm.cObject)
	ns.consts["Identity"] = ic

	// Age::Scrypt::Recipient.new(passphrase, work_factor: nil) — work_factor: is
	// the log2 of the scrypt cost (larger is slower/stronger).
	rc.smethods["new"] = &Method{name: "new", owner: rc,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			r, err := scrypt.NewRecipient(strArg(args[0]))
			if err != nil {
				raiseAgeError(err)
			}
			if h := ageOptsHash(args[1:]); h != nil {
				if v, ok := h.Get(object.Symbol("work_factor")); ok {
					r.SetWorkFactor(int(intArg(v)))
				}
			}
			return &AgeScryptRecipient{cls: rc, rcp: r}
		}}

	// Age::Scrypt::Identity.new(passphrase, max_work_factor: nil) — max_work_factor:
	// caps the work factor the identity will accept when decrypting.
	ic.smethods["new"] = &Method{name: "new", owner: ic,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			id, err := scrypt.NewIdentity(strArg(args[0]))
			if err != nil {
				raiseAgeError(err)
			}
			if h := ageOptsHash(args[1:]); h != nil {
				if v, ok := h.Get(object.Symbol("max_work_factor")); ok {
					id.SetMaxWorkFactor(int(intArg(v)))
				}
			}
			return &AgeScryptIdentity{cls: ic, id: id}
		}}
}

// registerAgeOneShots installs Age.encrypt and Age.decrypt, the whole-message
// entry points, delegating to go-ruby-age's Encrypt / Decrypt.
func (vm *VM) registerAgeOneShots(mod *RClass) {
	// Age.encrypt(plaintext, recipients:, armor: false) → the age ciphertext (a
	// binary String, or an ASCII-armored String when armor: is truthy). The
	// recipients: keyword is required; omitting it raises ArgumentError, as the
	// gem does.
	mod.smethods["encrypt"] = &Method{name: "encrypt", owner: mod,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			h := ageOptsHash(args[1:])
			v, ok := ageHashGet(h, "recipients")
			if !ok {
				raise("ArgumentError", "missing keyword: :recipients")
			}
			var opts []age.Option
			if a, ok := ageHashGet(h, "armor"); ok && a.Truthy() {
				opts = append(opts, age.WithArmor())
			}
			ct, err := age.Encrypt([]byte(strArg(args[0])), ageRecipients(v), opts...)
			if err != nil {
				raiseAgeError(err)
			}
			return object.NewStringBytes(ct)
		}}

	// Age.decrypt(ciphertext, identities:) → the recovered plaintext (a binary
	// String). Armored and binary ciphertexts are both accepted. The identities:
	// keyword is required; when none of them unwrap the file key,
	// Age::NoIdentityMatchError (or Age::IncorrectPassphraseError for an scrypt
	// file) is raised.
	mod.smethods["decrypt"] = &Method{name: "decrypt", owner: mod,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			h := ageOptsHash(args[1:])
			v, ok := ageHashGet(h, "identities")
			if !ok {
				raise("ArgumentError", "missing keyword: :identities")
			}
			pt, err := age.Decrypt([]byte(strArg(args[0])), ageIdentities(v))
			if err != nil {
				raiseAgeError(err)
			}
			return object.NewStringBytes(pt)
		}}
}

// ageDefineToS defines #to_s and #inspect on cls from a function that renders
// the receiver's string form (the "AGE-SECRET-KEY-1…"/"age1…" key).
func ageDefineToS(cls *RClass, str func(object.Value) string) {
	cls.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(str(self))
	})
	cls.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(object.NewString(str(self)).Inspect())
	})
}

// ageOptsHash returns the trailing keyword Hash of an Age entry point (the
// recipients:/armor:/work_factor: options), or nil when the last argument is not
// a Hash.
func ageOptsHash(rest []object.Value) *object.Hash {
	if len(rest) == 0 {
		return nil
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}

// ageHashGet fetches a symbol-keyed option from an options Hash, reporting
// ok=false when the Hash is absent or the key is missing.
func ageHashGet(h *object.Hash, key string) (object.Value, bool) {
	if h == nil {
		return object.NilV, false
	}
	return h.Get(object.Symbol(key))
}
