// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	stdtime "time"

	devise "github.com/go-ruby-devise/devise"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerDevise installs the Devise module (require "devise"): the module logic
// of Ruby's Devise authentication framework, reimplemented in pure Go (CGO=0) by
// github.com/go-ruby-devise/devise on top of go-ruby-bcrypt and go-ruby-warden.
// The library owns every authentication state machine — password storage and
// verification, reset/confirmation/unlock token issuance and consumption, the
// remember/lock/confirm/timeout windows and sign-in tracking — as plain Go over
// an injectable model, and drives Warden's chain through
// Devise::Strategies::DatabaseAuthenticatable. This file maps that surface onto
// rbgo classes (see devise_bind.go for the persistence / lookup / Warden seams):
//
//	Devise.friendly_token / secure_compare / setup{ |c| … } / config
//	Devise::Encryptor.digest / compare               — the bcrypt password hasher
//	Devise::TokenGenerator#generate / #digest        — the HMAC token minter
//	Devise::Config                                   — the per-resource settings +
//	                                                   the finder / mailer / clock
//	                                                   seams + the class-level flows
//	                                                   (reset/confirm/unlock/cookie)
//	Devise::Resource                                 — a model bound to a config;
//	                                                   the 8 module behaviours hang
//	                                                   off its instance methods
//	Devise::Strategies::DatabaseAuthenticatable      — the Warden password strategy,
//	                                                   registered into the bound
//	                                                   Warden::Strategies registry
//
// Controllers, routes, views, mailers and the Rails engine are not part of the
// library (they are roadmap); the mailer touchpoints are exposed as notification
// callbacks on Devise::Config. registerDevise runs after registerWarden and
// registerBCrypt (dependency order).
func (vm *VM) registerDevise() {
	mod := newClass("Devise", nil)
	mod.isModule = true
	vm.consts["Devise"] = mod

	vm.registerDeviseErrors(mod)
	vm.registerDeviseConfigClass(mod)
	vm.registerDeviseResource(mod)
	vm.registerDeviseEncryptor(mod)
	vm.registerDeviseTokenGenerator(mod)
	vm.registerDeviseModule(mod)
	vm.registerDeviseStrategy(mod)
}

// registerDeviseErrors installs Devise::Error < StandardError, the class a failed
// class-level flow or crypto operation is re-raised as.
func (vm *VM) registerDeviseErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	cls := newClass("Devise::Error", std)
	mod.consts["Error"] = cls
	vm.consts["Devise::Error"] = cls
}

// registerDeviseModule installs the module-level surface: the friendly_token /
// secure_compare helpers and the shared Devise.config accessor and setup block.
func (vm *VM) registerDeviseModule(mod *RClass) {
	mod.smethods["friendly_token"] = &Method{name: "friendly_token", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		length := int(intArgOr(args, int64(devise.DefaultConfig().FriendlyTokenLength)))
		return object.NewString(devise.FriendlyToken(length))
	}}
	mod.smethods["secure_compare"] = &Method{name: "secure_compare", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(devise.SecureCompare(strArg(args[0]), strArg(args[1])))
	}}
	mod.smethods["config"] = &Method{name: "config", owner: mod, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.deviseConfig
	}}
	// Devise.setup{ |config| … } yields the shared config, mirroring the
	// initializer block; it returns nil like Devise.setup.
	mod.smethods["setup"] = &Method{name: "setup", owner: mod, native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk != nil {
			vm.callBlock(blk, []object.Value{vm.deviseConfig})
		}
		return object.NilV
	}}
}

// registerDeviseEncryptor installs Devise::Encryptor: digest (hash a password
// with the pepper at a cost) and compare (constant-time verify), delegating to
// the library's bcrypt-backed Encryptor.
func (vm *VM) registerDeviseEncryptor(mod *RClass) {
	enc := newClass("Devise::Encryptor", nil)
	enc.isModule = true
	mod.consts["Encryptor"] = enc
	vm.consts["Devise::Encryptor"] = enc

	enc.smethods["digest"] = &Method{name: "digest", owner: enc, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pepper := ""
		if len(args) > 1 {
			pepper = strArg(args[1])
		}
		cost := devise.DefaultConfig().Stretches
		if len(args) > 2 {
			cost = int(intArg(args[2]))
		}
		h, err := devise.Encryptor{}.Digest(strArg(args[0]), pepper, cost)
		deviseCheck(err)
		return object.NewString(h)
	}}
	enc.smethods["compare"] = &Method{name: "compare", owner: enc, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pepper := ""
		if len(args) > 2 {
			pepper = strArg(args[2])
		}
		return object.Bool(devise.Encryptor{}.Compare(strArg(args[0]), strArg(args[1]), pepper))
	}}
}

// registerDeviseTokenGenerator installs Devise::TokenGenerator: new / generate /
// digest over the library's HMAC-SHA256 TokenGenerator.
func (vm *VM) registerDeviseTokenGenerator(mod *RClass) {
	cls := newClass("Devise::TokenGenerator", vm.cObject)
	mod.consts["TokenGenerator"] = cls
	vm.consts["Devise::TokenGenerator"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &DeviseTokenGenerator{gen: devise.NewTokenGenerator(nil), cls: cls}
	}}

	self := func(v object.Value) *DeviseTokenGenerator { return v.(*DeviseTokenGenerator) }
	// #generate(column) → [raw, enc]. Uniqueness (the finder collision loop) is a
	// class-level concern the resource flows own, so a bare generate never collides.
	cls.define("generate", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		raw, enc := self(v).gen.Generate(strArg(args[0]), nil)
		return object.NewArray(object.NewString(raw), object.NewString(enc))
	})
	cls.define("digest", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).gen.Digest(strArg(args[0]), strArg(args[1])))
	})
}

// registerDeviseConfigClass installs Devise::Config: its constructor, the setting
// accessors, the finder / clock / mailer seams and the class-level flows, and
// seeds the shared Devise.config used by the Warden strategy.
func (vm *VM) registerDeviseConfigClass(mod *RClass) {
	cls := newClass("Devise::Config", vm.cObject)
	mod.consts["Config"] = cls
	vm.consts["Devise::Config"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.newDeviseConfig(cls)
	}}

	// The shared config the DatabaseAuthenticatable strategy authenticates against
	// (Devise.config); a Rails app tweaks it in the initializer.
	vm.deviseConfig = vm.newDeviseConfig(cls)

	vm.registerDeviseConfigSettings(cls)
	vm.registerDeviseConfigSeams(cls)
	vm.registerDeviseConfigFlows(cls)
}

// newDeviseConfig builds a Devise::Config over the library's DefaultConfig, wiring
// the finder and the three notification callbacks to closures over the wrapper so
// a Ruby callable drives them.
func (vm *VM) newDeviseConfig(cls *RClass) *DeviseConfig {
	c := &DeviseConfig{vm: vm, cfg: devise.DefaultConfig(), cls: cls}
	c.cfg.Finder = c.wireFinder()
	c.cfg.SendResetPasswordInstructions = func(r *devise.Record, raw string) {
		if c.onResetPasswordInstr != nil {
			c.vm.callBlock(c.onResetPasswordInstr, []object.Value{c.wrapResource(r), object.NewString(raw)})
		}
	}
	c.cfg.SendConfirmationInstructions = func(r *devise.Record, raw string) {
		if c.onConfirmationInstr != nil {
			c.vm.callBlock(c.onConfirmationInstr, []object.Value{c.wrapResource(r), object.NewString(raw)})
		}
	}
	c.cfg.SendUnlockInstructions = func(r *devise.Record, raw string) {
		if c.onUnlockInstr != nil {
			c.vm.callBlock(c.onUnlockInstr, []object.Value{c.wrapResource(r), object.NewString(raw)})
		}
	}
	return c
}

// wrapResource wraps a library-built record as a Devise::Resource under this
// config's VM.
func (c *DeviseConfig) wrapResource(rec *devise.Record) *DeviseResource {
	return c.vm.wrapResource(rec, c.vm.consts["Devise::Resource"].(*RClass))
}

// registerDeviseConfigSettings installs the plain setting accessors of
// Devise::Config (stretches, pepper, the lock/remember/confirm/timeout windows,
// the authentication keys).
func (vm *VM) registerDeviseConfigSettings(cls *RClass) {
	self := func(v object.Value) *DeviseConfig { return v.(*DeviseConfig) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("stretches", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).cfg.Stretches))
	})
	d("stretches=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).cfg.Stretches = int(intArg(args[0]))
		return args[0]
	})
	d("pepper", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).cfg.Pepper)
	})
	d("pepper=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).cfg.Pepper = strArg(args[0])
		return args[0]
	})
	d("maximum_attempts", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).cfg.MaximumAttempts))
	})
	d("maximum_attempts=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).cfg.MaximumAttempts = int(intArg(args[0]))
		return args[0]
	})
	d("unlock_strategy=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).cfg.UnlockStrategy = devise.UnlockStrategy(rackStr(args[0]))
		return args[0]
	})
	d("lock_strategy=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).cfg.LockStrategy = devise.LockStrategy(rackStr(args[0]))
		return args[0]
	})
	d("authentication_keys", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		keys := self(v).cfg.AuthenticationKeys
		out := make([]object.Value, len(keys))
		for i, k := range keys {
			out[i] = object.Symbol(k)
		}
		return object.NewArrayFromSlice(out)
	})
	d("authentication_keys=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		arr, ok := args[0].(*object.Array)
		if !ok {
			raise("TypeError", "authentication_keys must be an Array")
		}
		keys := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			keys[i] = rackStr(e)
		}
		self(v).cfg.AuthenticationKeys = keys
		return args[0]
	})
	d("password_length_min=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).cfg.PasswordLengthMin = int(intArg(args[0]))
		return args[0]
	})
	d("password_length_max=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).cfg.PasswordLengthMax = int(intArg(args[0]))
		return args[0]
	})
	d("expire_all_remember_me_on_sign_out=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).cfg.ExpireAllRememberMeOnSignOut = args[0].Truthy()
		return args[0]
	})
	d("reset_password_within=", deviseDurationSetter(func(c *DeviseConfig, dur stdtime.Duration) { c.cfg.ResetPasswordWithin = dur }))
	d("remember_for=", deviseDurationSetter(func(c *DeviseConfig, dur stdtime.Duration) { c.cfg.RememberFor = dur }))
	d("unlock_in=", deviseDurationSetter(func(c *DeviseConfig, dur stdtime.Duration) { c.cfg.UnlockIn = dur }))
	d("allow_unconfirmed_access_for=", deviseDurationPtrSetter(func(c *DeviseConfig, dur *stdtime.Duration) { c.cfg.AllowUnconfirmedAccessFor = dur }))
	d("confirm_within=", deviseDurationPtrSetter(func(c *DeviseConfig, dur *stdtime.Duration) { c.cfg.ConfirmWithin = dur }))
	d("timeout_in=", deviseDurationPtrSetter(func(c *DeviseConfig, dur *stdtime.Duration) { c.cfg.TimeoutIn = dur }))
}

// deviseDurationSetter builds a seconds-taking window setter (the value is stored
// as a duration through set).
func deviseDurationSetter(set func(*DeviseConfig, stdtime.Duration)) NativeFn {
	return func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		set(v.(*DeviseConfig), stdtime.Duration(intArg(args[0]))*stdtime.Second)
		return args[0]
	}
}

// deviseDurationPtrSetter builds a seconds-or-nil window setter: nil disables the
// window (a nil *Duration), a number sets it.
func deviseDurationPtrSetter(set func(*DeviseConfig, *stdtime.Duration)) NativeFn {
	return func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if _, ok := args[0].(object.Nil); ok {
			set(v.(*DeviseConfig), nil)
			return args[0]
		}
		dur := stdtime.Duration(intArg(args[0])) * stdtime.Second
		set(v.(*DeviseConfig), &dur)
		return args[0]
	}
}

// registerDeviseConfigSeams installs the injectable seams of Devise::Config: the
// record finder, the clock and the three mailer notification callbacks. Each
// takes a Proc (a lambda or a block).
func (vm *VM) registerDeviseConfigSeams(cls *RClass) {
	self := func(v object.Value) *DeviseConfig { return v.(*DeviseConfig) }
	// The seams are assignment methods (config.finder = …), so the callable always
	// arrives as a Proc argument (a lambda); a non-Proc is rejected.
	proc := func(args []object.Value) *Proc {
		if len(args) > 0 {
			if p, ok := args[0].(*Proc); ok {
				return p
			}
		}
		raise("ArgumentError", "a Proc is required")
		return nil
	}
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("finder=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).finder = proc(args)
		return object.NilV
	})
	d("now=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		c := self(v)
		c.now = proc(args)
		c.cfg.Now = c.wireNow()
		return object.NilV
	})
	d("send_reset_password_instructions=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).onResetPasswordInstr = proc(args)
		return object.NilV
	})
	d("send_confirmation_instructions=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).onConfirmationInstr = proc(args)
		return object.NilV
	})
	d("send_unlock_instructions=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).onUnlockInstr = proc(args)
		return object.NilV
	})
}

// registerDeviseConfigFlows installs the resource builder and the class-level
// flows Devise runs off the model class (reset/confirm/unlock by token, remember
// cookie deserialisation), each returning a Devise::Resource or raising
// Devise::Error.
func (vm *VM) registerDeviseConfigFlows(cls *RClass) {
	self := func(v object.Value) *DeviseConfig { return v.(*DeviseConfig) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// build(model) → a Devise::Resource binding the model to this config.
	d("build", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		c := self(v)
		rec := devise.New(c.cfg, deviseModel{vm: vm, obj: args[0]})
		return c.wrapResource(rec)
	})
	d("reset_password_by_token", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		c := self(v)
		rec, err := c.cfg.ResetPasswordByToken(strArg(args[0]), strArg(args[1]), strArg(args[2]))
		deviseCheck(err)
		return c.wrapResource(rec)
	})
	d("confirm_by_token", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		c := self(v)
		rec, err := c.cfg.ConfirmByToken(strArg(args[0]))
		deviseCheck(err)
		return c.wrapResource(rec)
	})
	d("unlock_access_by_token", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		c := self(v)
		rec, err := c.cfg.UnlockAccessByToken(strArg(args[0]))
		deviseCheck(err)
		return c.wrapResource(rec)
	})
	d("serialize_from_cookie", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		c := self(v)
		rec, err := c.cfg.SerializeFromCookie(strArg(args[0]), strArg(args[1]))
		deviseCheck(err)
		return c.wrapResource(rec)
	})
}

// registerDeviseResource installs Devise::Resource: the eight authentication
// module behaviours as instance methods over a *devise.Record.
func (vm *VM) registerDeviseResource(mod *RClass) {
	cls := newClass("Devise::Resource", vm.cObject)
	mod.consts["Resource"] = cls
	vm.consts["Devise::Resource"] = cls

	self := func(v object.Value) *DeviseResource { return v.(*DeviseResource) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// model → the underlying Ruby model object this resource wraps.
	d("model", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return deviseModelObj(self(v).rec.Model())
	})

	// --- DatabaseAuthenticatable ---
	d("valid_password?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.ValidPassword(strArg(args[0])))
	})
	d("password=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		deviseCheck(self(v).rec.SetPassword(strArg(args[0])))
		return args[0]
	})
	d("authenticatable_salt", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).rec.AuthenticatableSalt())
	})

	// --- Validatable ---
	d("validatable_errors", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return deviseValidatableErrors(self(v), args)
	})

	// --- Recoverable ---
	d("set_reset_password_token", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		raw, err := self(v).rec.SetResetPasswordToken()
		deviseCheck(err)
		return object.NewString(raw)
	})
	d("send_reset_password_instructions", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		raw, err := self(v).rec.SendResetPasswordInstructions()
		deviseCheck(err)
		return object.NewString(raw)
	})
	d("reset_password_period_valid?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.ResetPasswordPeriodValid())
	})
	d("reset_password", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		deviseCheck(self(v).rec.ResetPassword(strArg(args[0])))
		return object.NilV
	})

	// --- Rememberable ---
	d("remember_me!", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		deviseCheck(self(v).rec.RememberMe())
		return object.NilV
	})
	d("forget_me!", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		deviseCheck(self(v).rec.ForgetMe())
		return object.NilV
	})
	d("rememberable_value", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).rec.RememberableValue())
	})
	d("remember_expired?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.RememberExpired())
	})

	vm.registerDeviseResourceConfirmLock(cls, self, d)
}

// registerDeviseResourceConfirmLock installs the Confirmable, Lockable, Trackable
// and Timeoutable behaviours (split out to keep each register method small).
func (vm *VM) registerDeviseResourceConfirmLock(cls *RClass, self func(object.Value) *DeviseResource, d func(string, NativeFn)) {
	// --- Confirmable ---
	d("generate_confirmation_token", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).rec.GenerateConfirmationToken())
	})
	d("send_confirmation_instructions", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		raw, err := self(v).rec.SendConfirmationInstructions()
		deviseCheck(err)
		return object.NewString(raw)
	})
	d("confirmed?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.Confirmed())
	})
	d("confirm", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		deviseCheck(self(v).rec.Confirm())
		return object.NilV
	})
	d("confirmation_period_valid?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.ConfirmationPeriodValid())
	})
	d("confirmation_period_expired?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.ConfirmationPeriodExpired())
	})
	d("active_for_authentication?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.ActiveForAuthentication())
	})
	d("inactive_message", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(self(v).rec.InactiveMessage())
	})

	// --- Lockable ---
	d("lock_access!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		send := false
		if len(args) > 0 {
			send = args[0].Truthy()
		}
		deviseCheck(self(v).rec.LockAccess(send))
		return object.NilV
	})
	d("unlock_access!", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		deviseCheck(self(v).rec.UnlockAccess())
		return object.NilV
	})
	d("access_locked?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).rec.AccessLocked())
	})
	d("failed_attempts", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).rec.FailedAttempts()))
	})
	// valid_for_authentication?{ … } runs the password check block through the
	// lock gate, faithful to Lockable#valid_for_authentication?.
	d("valid_for_authentication?", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "valid_for_authentication? requires a block")
		}
		return object.Bool(self(v).rec.ValidForAuthentication(func() bool {
			return vm.callBlock(blk, nil).Truthy()
		}))
	})

	// --- Trackable ---
	d("update_tracked_fields", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).rec.UpdateTrackedFields(strArg(args[0]))
		return object.NilV
	})
	d("update_tracked_fields!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		deviseCheck(self(v).rec.UpdateTrackedFieldsAndSave(strArg(args[0])))
		return object.NilV
	})
	d("sign_in_count", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).rec.SignInCount()))
	})

	// --- Timeoutable ---
	d("timedout?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		t := timeArg(args[0])
		return object.Bool(self(v).rec.TimedOut(stdtime.Unix(t.t.ToUnix(), 0).UTC()))
	})
	d("timeout_in", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		dur, ok := self(v).rec.TimeoutIn()
		if !ok {
			return object.NilV
		}
		return object.IntValue(int64(dur / stdtime.Second))
	})
}

// deviseValidatableErrors runs Validatable against the record and the transient
// params Hash, returning the failures as [attribute, reason] pairs (attribute a
// String, reason a Symbol matching ActiveModel's error keys).
func deviseValidatableErrors(r *DeviseResource, args []object.Value) object.Value {
	p := devise.ValidateParams{}
	if h, ok := rackArg(args).(*object.Hash); ok {
		if v, ok := h.Get(object.Symbol("password")); ok {
			p.Password = rackStr(v)
		}
		if v, ok := h.Get(object.Symbol("password_confirmation")); ok {
			p.PasswordConfirmation = rackStr(v)
		}
		if v, ok := h.Get(object.Symbol("password_provided")); ok {
			p.PasswordProvided = v.Truthy()
		}
		if v, ok := h.Get(object.Symbol("email_changed")); ok {
			p.EmailChanged = v.Truthy()
		}
	}
	errs := r.rec.ValidatableErrors(p)
	out := make([]object.Value, len(errs))
	for i, e := range errs {
		out[i] = object.NewArray(object.NewString(e.Attribute), object.Symbol(e.Reason))
	}
	return object.NewArrayFromSlice(out)
}

// registerDeviseStrategy installs Devise::Strategies::DatabaseAuthenticatable and
// registers it as the :database_authenticatable Warden strategy on the bound
// Warden::Strategies registry, so a Warden::Manager configured with that label
// authenticates against Devise.config through the library's strategy. The native
// valid? / authenticate! read the request credentials from the Rack env and map
// the library's warden.StrategyResult onto the same WardenStrategy outcome fields
// rbgo's own Warden binding records.
func (vm *VM) registerDeviseStrategy(mod *RClass) {
	strategies := newClass("Devise::Strategies", nil)
	strategies.isModule = true
	mod.consts["Strategies"] = strategies
	vm.consts["Devise::Strategies"] = strategies

	base := vm.consts["Warden::Strategies::Base"].(*RClass)
	cls := newClass("Devise::Strategies::DatabaseAuthenticatable", base)
	strategies.consts["DatabaseAuthenticatable"] = cls
	vm.consts["Devise::Strategies::DatabaseAuthenticatable"] = cls

	cls.define("valid?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := v.(*WardenStrategy)
		cfg := vm.deviseConfig.cfg
		authHash, password := deviseCreds(s.env, cfg.AuthenticationKeys)
		return object.Bool(devise.DatabaseAuthenticatableStrategy{Cfg: cfg}.Valid(authHash, password))
	})
	cls.define("authenticate!", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := v.(*WardenStrategy)
		cfg := vm.deviseConfig.cfg
		authHash, password := deviseCreds(s.env, cfg.AuthenticationKeys)
		res := devise.DatabaseAuthenticatableStrategy{Cfg: cfg}.Run(authHash, password)
		s.result = res.Result
		s.message = res.Message
		s.halted = res.Halted
		s.response = res.Response
		if res.User != nil {
			s.user = deviseModelObj(res.User.(deviseModel))
		}
		return object.NilV
	})

	// Register into the Warden strategy registry (which registerWarden already
	// initialised, since registerDevise runs after it) so a Warden::Manager
	// configured with default_strategies :database_authenticatable finds it.
	vm.wardenStrategies[devise.StrategyDatabaseAuthenticatable] = cls
}
