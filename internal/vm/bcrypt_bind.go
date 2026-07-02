// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	bcrypt "github.com/go-ruby-bcrypt/bcrypt"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent bcrypt core of github.com/go-ruby-bcrypt/bcrypt (a
// pure-Go, no-cgo port of Ruby's bcrypt gem). registerBCrypt wires the BCrypt
// module — the Password value class, the Engine low-level surface, its cost
// constants, and the BCrypt::Errors exception tree — onto the library's
// Create / NewPassword / Equal / Engine functions and its sentinel errors. All
// hashing is delegated to go-ruby-bcrypt; a hash rbgo emits verifies under the
// gem and vice-versa.

// BCryptPassword is an instance of BCrypt::Password (which subclasses String in
// the gem): a parsed "$2a$NN$...." hash backed by a go-ruby-bcrypt *Password.
// Its readers expose the parsed fields (cost / salt / version / checksum) and its
// #== compares a candidate secret against the stored hash in constant time.
type BCryptPassword struct {
	cls *RClass
	p   *bcrypt.Password
}

// ToS renders the stored hash, matching BCrypt::Password#to_s (a String subclass).
func (p *BCryptPassword) ToS() string { return p.p.String() }

// Inspect renders the stored hash as its Ruby inspect form (a quoted String, as
// the gem's Password inherits String#inspect).
func (p *BCryptPassword) Inspect() string { return object.NewString(p.p.String()).Inspect() }

func (p *BCryptPassword) Truthy() bool { return true }

// registerBCrypt installs the BCrypt module (require "bcrypt"): BCrypt::Password
// with create / new / == and the cost/salt/version/checksum/to_s readers,
// BCrypt::Engine with generate_salt / hash_secret / valid_secret? / valid_salt? /
// autodetect_cost and its DEFAULT_COST / MIN_COST / MAX_COST constants, and the
// BCrypt::Errors exception tree. All work is delegated to go-ruby-bcrypt.
func (vm *VM) registerBCrypt() {
	mod := newClass("BCrypt", nil)
	mod.isModule = true
	vm.consts["BCrypt"] = mod

	vm.registerBCryptErrors(mod)
	vm.registerBCryptPassword(mod)
	vm.registerBCryptEngine(mod)
}

// registerBCryptErrors installs the BCrypt::Errors module and its exception
// classes, mirroring the gem (BCryptError < StandardError; InvalidSecret /
// InvalidCost / InvalidSalt / InvalidHash < BCryptError). Each class is
// registered both as a nested constant (so Ruby BCrypt::Errors::InvalidHash
// resolves it) and under its qualified name in the top-level table (so a
// re-raised library sentinel's exception lookup finds the same class), exactly as
// the JSON error tree is.
func (vm *VM) registerBCryptErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	errs := newClass("BCrypt::Errors", nil)
	errs.isModule = true
	mod.consts["Errors"] = errs

	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		errs.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("BCryptError", "BCrypt::Errors::BCryptError", std)
	reg("InvalidSecret", "BCrypt::Errors::InvalidSecret", base)
	reg("InvalidCost", "BCrypt::Errors::InvalidCost", base)
	reg("InvalidSalt", "BCrypt::Errors::InvalidSalt", base)
	reg("InvalidHash", "BCrypt::Errors::InvalidHash", base)
}

// registerBCryptPassword installs BCrypt::Password: the create/new constructors,
// the parsed-field readers, and the constant-time secret comparison.
func (vm *VM) registerBCryptPassword(mod *RClass) {
	c := newClass("BCrypt::Password", vm.cString)
	mod.consts["Password"] = c

	// BCrypt::Password.create(secret, cost: n) hashes a secret with a fresh salt,
	// returning a Password. cost: is the gem's option (via the trailing keyword
	// Hash); an over-MaxCost cost raises ArgumentError, as the gem does.
	c.smethods["create"] = &Method{name: "create", owner: c,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			var opts []bcrypt.CreateOption
			if h := bcryptOptsHash(args[1:]); h != nil {
				if v, ok := h.Get(object.Symbol("cost")); ok {
					opts = append(opts, bcrypt.WithCost(int(intArg(v))))
				}
			}
			p, err := bcrypt.CreateString(strArg(args[0]), opts...)
			if err != nil {
				raiseBCryptError(err)
			}
			return &BCryptPassword{cls: c, p: p}
		}}

	// BCrypt::Password.new(hash) parses a stored "$2a$NN$...." hash, raising
	// BCrypt::Errors::InvalidHash for a malformed one.
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			p, err := bcrypt.NewPassword(strArg(args[0]))
			if err != nil {
				raiseBCryptError(err)
			}
			return &BCryptPassword{cls: c, p: p}
		}}

	// == compares a candidate secret against the stored hash in constant time
	// (password == "secret"), matching the gem's Password#== / #is_password?.
	eq := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*BCryptPassword).p.EqualString(strArg(args[0])))
	}
	c.define("==", eq)
	c.define("is_password?", eq)

	c.define("cost", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*BCryptPassword).p.Cost()))
	})
	c.define("salt", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*BCryptPassword).p.Salt())
	})
	c.define("version", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*BCryptPassword).p.Version())
	})
	c.define("checksum", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*BCryptPassword).p.Checksum())
	})
	toS := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*BCryptPassword).p.String())
	}
	c.define("to_s", toS)
	c.define("to_str", toS)
	c.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*BCryptPassword).Inspect())
	})
}

// registerBCryptEngine installs BCrypt::Engine: the low-level salt/hash surface
// and the cost constants, delegating to go-ruby-bcrypt's Engine functions.
func (vm *VM) registerBCryptEngine(mod *RClass) {
	e := newClass("BCrypt::Engine", vm.cObject)
	mod.consts["Engine"] = e

	e.consts["DEFAULT_COST"] = object.IntValue(bcrypt.DefaultCost)
	e.consts["MIN_COST"] = object.IntValue(bcrypt.MinCost)
	e.consts["MAX_COST"] = object.IntValue(bcrypt.MaxCost)

	// BCrypt::Engine.generate_salt(cost = DEFAULT_COST) → a fresh "$2a$NN$...."
	// salt. A non-positive cost raises BCrypt::Errors::InvalidCost, as the gem does.
	e.smethods["generate_salt"] = &Method{name: "generate_salt", owner: e,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			cost := bcrypt.DefaultCost
			if len(args) > 0 {
				cost = int(intArg(args[0]))
			}
			salt, err := bcrypt.GenerateSalt(cost)
			if err != nil {
				raiseBCryptError(err)
			}
			return object.NewString(salt)
		}}

	// BCrypt::Engine.hash_secret(secret, salt) → the "$2a$NN$...." hash of secret
	// under salt. A malformed salt raises BCrypt::Errors::InvalidSalt.
	e.smethods["hash_secret"] = &Method{name: "hash_secret", owner: e,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			h, err := bcrypt.HashSecret([]byte(strArg(args[0])), strArg(args[1]))
			if err != nil {
				raiseBCryptError(err)
			}
			return object.NewString(h)
		}}

	e.smethods["valid_secret?"] = &Method{name: "valid_secret?", owner: e,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.Bool(bcrypt.ValidSecret([]byte(strArg(args[0]))))
		}}
	e.smethods["valid_salt?"] = &Method{name: "valid_salt?", owner: e,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.Bool(bcrypt.ValidSalt(strArg(args[0])))
		}}
	e.smethods["autodetect_cost"] = &Method{name: "autodetect_cost", owner: e,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.IntValue(int64(bcrypt.AutodetectCost(strArg(args[0]))))
		}}
}

// bcryptOptsHash returns the trailing keyword Hash of a BCrypt entry point (the
// cost: option to Password.create), or nil when the last argument is not a Hash.
func bcryptOptsHash(rest []object.Value) *object.Hash {
	if len(rest) == 0 {
		return nil
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}

// raiseBCryptError re-raises a go-ruby-bcrypt sentinel as the matching Ruby
// exception: the InvalidSecret / InvalidCost / InvalidSalt / InvalidHash sentinels
// map to their BCrypt::Errors classes, and ErrCostTooHigh to ArgumentError (the
// gem raises a bare ArgumentError for an over-max cost).
func raiseBCryptError(err error) {
	switch {
	case errors.Is(err, bcrypt.ErrCostTooHigh):
		raise("ArgumentError", "%s", err.Error())
	case errors.Is(err, bcrypt.ErrInvalidSecret):
		raise("BCrypt::Errors::InvalidSecret", "%s", err.Error())
	case errors.Is(err, bcrypt.ErrInvalidCost):
		raise("BCrypt::Errors::InvalidCost", "%s", err.Error())
	case errors.Is(err, bcrypt.ErrInvalidSalt):
		raise("BCrypt::Errors::InvalidSalt", "%s", err.Error())
	case errors.Is(err, bcrypt.ErrInvalidHash):
		raise("BCrypt::Errors::InvalidHash", "%s", err.Error())
	default:
		raise("BCrypt::Errors::BCryptError", "%s", err.Error())
	}
}
