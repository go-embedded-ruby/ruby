// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	rails "github.com/go-ruby-rails/rails"
	railties "github.com/go-ruby-railties/railties"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the value-model + seam half of the `rails` meta-gem binding
// (require "rails/all" and the top-level Rails.* accessors); rails_meta.go holds
// the module accessor + class registration. The meta-gem itself
// (github.com/go-ruby-rails/rails, package `rails`) ships almost no code of its
// own: it vends the top-level Rails module (Rails.application / env / root /
// logger / cache / configuration / …), the Rails::VERSION constant and the
// machine-readable component catalog, delegating application state to the
// railties Application through its App seam. This binding layers those onto the
// Rails module the railties binding already created (registerRailties), never
// re-creating it.

// RailsEnvVal wraps a rails.EnvironmentInquirer (Rails::EnvironmentInquirer, a
// Rails::StringInquirer subclass): the value Rails.env returns so `env.production?`
// and `env.local?` read naturally.
type RailsEnvVal struct {
	e   rails.EnvironmentInquirer
	cls *RClass
}

func (v *RailsEnvVal) ToS() string     { return v.e.String() }
func (v *RailsEnvVal) Inspect() string { return "\"" + v.e.String() + "\"" }
func (v *RailsEnvVal) Truthy() bool    { return true }

// railsAppAdapter adapts a railties Application to the meta-gem's rails.App seam,
// so the top-level Rails.root / Rails.public_path / Rails.configuration /
// Rails.autoloaders / Rails.cache accessors — which the meta-gem resolves through
// rails.SetApplication — delegate to the bound railties application. The
// configuration/autoloaders/cache are opaque to the meta-gem (typed as any);
// railties vends a typed configuration but no autoloader set or cache store, so
// those return nil (mirroring the pre-boot Ruby result).
type railsAppAdapter struct{ app *railties.Application }

func (a railsAppAdapter) Root() string     { return a.app.Paths().Root() }
func (a railsAppAdapter) Config() any      { return a.app.Config() }
func (a railsAppAdapter) Autoloaders() any { return nil }
func (a railsAppAdapter) Cache() any       { return nil }

// PublicPath returns the first expanded entry of paths["public"], mirroring
// Rails.public_path; it is empty when the application declares no public path.
func (a railsAppAdapter) PublicPath() string {
	if p := a.app.Paths().Get("public"); p != nil {
		if ex := p.Expanded(); len(ex) > 0 {
			return ex[0]
		}
	}
	return ""
}

// railsAny converts one of the meta-gem's opaque any-typed slots (logger, cache,
// autoloaders) back to a Ruby value: a stored object.Value passes through, and a
// nil (nothing set, or a slot railties does not vend) becomes Ruby nil.
func railsAny(v any) object.Value {
	if rv, ok := v.(object.Value); ok {
		return rv
	}
	return object.NilV
}

// railsPre renders the Rails::VERSION::PRE constant: the pre-release segment as a
// String, or nil when the release is final (Pre == ""), mirroring Ruby's PRE.
func railsPre(s string) object.Value {
	if s == "" {
		return object.NilV
	}
	return object.NewString(s)
}

// railsComponentFeature maps a catalog component name to the Ruby feature name
// `require "rails/all"` loads for it (the component gem's canonical require path):
// railties is reached through "rails", actionpack through "action_controller",
// and every active*/action* component through its underscored form.
func railsComponentFeature(name string) string {
	switch name {
	case "railties":
		return "rails"
	case "actionpack":
		return "action_controller"
	}
	for _, p := range []string{"active", "action"} {
		if rest := strings.TrimPrefix(name, p); rest != name {
			return p + "_" + rest
		}
	}
	return name
}
