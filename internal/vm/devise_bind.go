// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	stdtime "time"

	devise "github.com/go-ruby-devise/devise"
	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the persistence/lookup/Warden seam between rbgo's Ruby object
// graph and the interpreter-independent github.com/go-ruby-devise/devise core.
// The library owns every authentication state machine (credential storage,
// token issuance, lock/remember/confirm/track windows) as plain Go over an
// injectable [devise.Model], and leaves the interpreter-defined pieces behind
// three seams that this file wires:
//
//   - the persistence surface (read attribute / write attribute / save): a Ruby
//     model object answering #[] / #[]= / #save is adapted by deviseModel to
//     devise.Model, converting each Devise attribute between its Go-native form
//     (string / int / time.Time / nil) and the Ruby value stored on the model;
//   - the class-level record lookup (devise.Finder): a Ruby callable is invoked
//     with a Ruby Hash of the query attributes and its returned model object (or
//     nil) is mapped back to a devise.Model, wired through DeviseConfig;
//   - the DatabaseAuthenticatable Warden strategy: it runs the library's
//     [devise.DatabaseAuthenticatableStrategy] and maps its [warden.StrategyResult]
//     onto the same WardenStrategy outcome fields rbgo's own Warden binding
//     records, so Devise's strategy plugs straight into the bound Warden Manager.

// DeviseConfig is the Ruby wrapper around a *devise.Config — the per-resource
// Devise settings (stretches, pepper, the lock/remember/confirm/timeout windows,
// the authentication keys) plus the injectable seams a pure-Go core needs. The
// Ruby finder and notification callbacks are held here as Procs; the library's
// Finder / SendResetPasswordInstructions / SendConfirmationInstructions /
// SendUnlockInstructions hooks are wired to closures over this wrapper so a
// Ruby-level callable drives them.
type DeviseConfig struct {
	vm  *VM
	cfg *devise.Config
	cls *RClass

	finder               *Proc
	now                  *Proc
	onResetPasswordInstr *Proc
	onConfirmationInstr  *Proc
	onUnlockInstr        *Proc
}

func (c *DeviseConfig) ToS() string     { return "#<Devise::Config>" }
func (c *DeviseConfig) Inspect() string { return c.ToS() }
func (c *DeviseConfig) Truthy() bool    { return true }

// DeviseResource is the Ruby wrapper around a *devise.Record — a model bound to
// the config that governs it, the object every Devise module method hangs off
// (valid_password?, remember_me!, confirm, lock_access!, update_tracked_fields!,
// timedout?, …). It is the rbgo equivalent of an ActiveRecord instance whose
// self.class carries the Devise settings.
type DeviseResource struct {
	vm  *VM
	rec *devise.Record
	cls *RClass
}

func (r *DeviseResource) ToS() string     { return "#<Devise::Resource>" }
func (r *DeviseResource) Inspect() string { return r.ToS() }
func (r *DeviseResource) Truthy() bool    { return true }

// DeviseTokenGenerator is the Ruby wrapper around a *devise.TokenGenerator — the
// HMAC-SHA256 token minter/digester Recoverable and Confirmable persist through.
type DeviseTokenGenerator struct {
	gen *devise.TokenGenerator
	cls *RClass
}

func (g *DeviseTokenGenerator) ToS() string     { return "#<Devise::TokenGenerator>" }
func (g *DeviseTokenGenerator) Inspect() string { return g.ToS() }
func (g *DeviseTokenGenerator) Truthy() bool    { return true }

// deviseModel adapts a Ruby model object (any value answering #[] / #[]= / #save)
// to devise.Model, the persistence surface every Devise module operates on. It
// is a value type so that two wrappers over the same underlying Ruby object
// compare equal, which is what lets Validatable's uniqueness check recognise a
// finder hit on the record being validated as "self" rather than "taken".
type deviseModel struct {
	vm  *VM
	obj object.Value
}

// Get reads a Devise attribute off the Ruby model via #[], converting the Ruby
// value back to the Go-native form (string / int / time.Time) the modules expect;
// a nil / absent / unrecognised value is nil.
func (m deviseModel) Get(attr string) any {
	return deviseFromRuby(m.vm.send(m.obj, "[]", []object.Value{object.NewString(attr)}, nil))
}

// Set stages a Devise attribute on the Ruby model via #[]=, converting the
// Go-native value the module wrote (string / int / time.Time / nil) into its
// Ruby form.
func (m deviseModel) Set(attr string, val any) {
	m.vm.send(m.obj, "[]=", []object.Value{object.NewString(attr), deviseToRuby(val)}, nil)
}

// Save persists the staged attributes via #save, mirroring ActiveRecord's
// save(validate: false) that Devise uses throughout. The model's return value is
// discarded; a model that must signal a failure does so by raising.
func (m deviseModel) Save() error {
	m.vm.send(m.obj, "save", nil, nil)
	return nil
}

// deviseFromRuby maps a Ruby attribute value into the Go-native form the Devise
// modules read (string / int / time.Time); anything else — including Ruby nil —
// is nil, matching a SQL-NULL column.
func deviseFromRuby(v object.Value) any {
	switch x := v.(type) {
	case *object.String:
		return x.Str()
	case object.Integer:
		return int(x)
	case *Time:
		return stdtime.Unix(x.t.ToUnix(), 0).UTC()
	default:
		return nil
	}
}

// deviseToRuby maps a Go-native attribute value a Devise module staged (string /
// int / time.Time / nil) into the Ruby value stored on the model.
func deviseToRuby(val any) object.Value {
	switch x := val.(type) {
	case string:
		return object.NewString(x)
	case int:
		return object.IntValue(int64(x))
	case stdtime.Time:
		return goTimeToRuby(x)
	default:
		return object.NilV
	}
}

// deviseModelObj recovers the underlying Ruby model object from a record's
// devise.Model, so a record the library built internally (in a class-level flow
// or a notification callback) can be handed back to Ruby as its model object.
// Every devise.Model in this binding is a deviseModel, so the assertion holds.
func deviseModelObj(m devise.Model) object.Value {
	return m.(deviseModel).obj
}

// wrapResource wraps a library-built *devise.Record as a Ruby Devise::Resource.
func (vm *VM) wrapResource(rec *devise.Record, cls *RClass) *DeviseResource {
	return &DeviseResource{vm: vm, rec: rec, cls: cls}
}

// deviseCheck re-raises a library error as a Ruby Devise::Error, so a failed
// class-level flow (unknown/expired token, confirmation mismatch) or a crypto
// failure (an out-of-range bcrypt cost) surfaces as a rescuable Ruby exception.
func deviseCheck(err error) {
	if err != nil {
		raise("Devise::Error", "%s", err.Error())
	}
}

// deviseAttrsToHash renders a finder query (Go-native attribute map) as a Ruby
// Hash keyed by String, so a Ruby finder callable reads the lookup attributes as
// ordinary Ruby values.
func deviseAttrsToHash(attrs map[string]any) *object.Hash {
	h := object.NewHash()
	for k, v := range attrs {
		h.Set(object.NewString(k), deviseToRuby(v))
	}
	return h
}

// wireFinder returns the devise.Finder seam bound to this config's Ruby finder
// callable: it renders the query as a Ruby Hash, invokes the callable, and maps
// a returned model object to a deviseModel (a falsey / nil return is "no match").
// With no finder configured, every lookup is a miss.
func (c *DeviseConfig) wireFinder() devise.Finder {
	return func(attrs map[string]any) (devise.Model, bool) {
		if c.finder == nil {
			return nil, false
		}
		res := c.vm.callBlock(c.finder, []object.Value{deviseAttrsToHash(attrs)})
		if !res.Truthy() {
			return nil, false
		}
		return deviseModel{vm: c.vm, obj: res}, true
	}
}

// wireNow returns the clock seam bound to this config's Ruby now callable, which
// must yield a Time; it is only installed when a Ruby clock is set, so the
// library keeps its real-UTC-now default otherwise.
func (c *DeviseConfig) wireNow() func() stdtime.Time {
	return func() stdtime.Time {
		v := c.vm.callBlock(c.now, nil)
		if t, ok := v.(*Time); ok {
			return stdtime.Unix(t.t.ToUnix(), 0).UTC()
		}
		return stdtime.Now().UTC()
	}
}

// deviseCreds extracts the authentication hash and password for the database
// strategy from a Rack env, faithful to Devise reading them from the request
// params: each configured authentication key and the password are read from the
// top-level params, mirroring params[key] / params[:password].
func deviseCreds(env rack.Env, keys []string) (map[string]any, string) {
	authHash := map[string]any{}
	password := ""
	p, err := rack.NewRequest(env).Params()
	if err != nil || p == nil {
		return authHash, password
	}
	for _, k := range keys {
		if v, ok := p.Get(k); ok {
			if s, ok := v.(string); ok {
				authHash[k] = s
			}
		}
	}
	if v, ok := p.Get("password"); ok {
		if s, ok := v.(string); ok {
			password = s
		}
	}
	return authHash, password
}
