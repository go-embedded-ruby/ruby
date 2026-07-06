// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	rails "github.com/go-ruby-rails/rails"
	railties "github.com/go-ruby-railties/railties"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestRailsAllFeature covers the `require "rails/all"` provided feature and its
// aggregate hook: the first require returns true, a second returns false, and it
// triggers a require of every shipped component whose feature the VM provides, so
// those features report already-loaded afterwards.
func TestRailsAllFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "rails/all"`, "true\n"},
		{`require "rails/all"; p require "rails/all"`, "false\n"},
		// rails/all pulled the available components in, so re-requiring each of the
		// provided-feature components now returns false (already loaded).
		{`require "rails/all"
p require "active_support"
p require "active_model"
p require "active_job"
p require "active_storage"
p require "action_cable"`, "false\nfalse\nfalse\nfalse\nfalse\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsVersion covers Rails::VERSION and Rails.version / Rails.gem_version:
// the meta-gem targets Rails 8.1.x.
func TestRailsVersion(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rails/all"; p Rails::VERSION::STRING`, "\"8.1.3\"\n"},
		{`require "rails/all"; p Rails::VERSION::MAJOR`, "8\n"},
		{`require "rails/all"; p Rails::VERSION::MINOR`, "1\n"},
		{`require "rails/all"; p Rails::VERSION::TINY`, "3\n"},
		{`require "rails/all"; p Rails::VERSION::PRE`, "nil\n"},
		{`require "rails/all"; p Rails.version`, "\"8.1.3\"\n"},
		{`require "rails/all"; p Rails.gem_version`, "\"8.1.3\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsEnvironmentInquirer covers Rails.env and Rails::EnvironmentInquirer:
// the class hierarchy (< Rails::StringInquirer), the dynamic `name?` predicate,
// the environment-specific local?, the String coercions (to_str / ==), and the
// non-predicate method_missing failure.
func TestRailsEnvironmentInquirer(t *testing.T) {
	rails.SetEnv("development")
	cases := []struct{ src, want string }{
		{`require "rails/all"; p Rails::EnvironmentInquirer < Rails::StringInquirer`, "true\n"},
		{`require "rails/all"; p Rails.env.class`, "Rails::EnvironmentInquirer\n"},
		{`require "rails/all"; p Rails.env.to_s`, "\"development\"\n"},
		{`require "rails/all"; p Rails.env.development?`, "true\n"},
		{`require "rails/all"; p Rails.env.production?`, "false\n"},
		{`require "rails/all"; p Rails.env.local?`, "true\n"},
		{`require "rails/all"; p Rails.env == "development"`, "true\n"},
		{`require "rails/all"; p Rails.env.to_str`, "\"development\"\n"},
		{`require "rails/all"; p("env is #{Rails.env}")`, "\"env is development\"\n"},
		{`require "rails/all"; begin; Rails.env.bogus; rescue NoMethodError; puts "nomethod"; end`, "nomethod\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsEnvSetterAndProduction covers Rails.env= (setting a non-local
// environment) and its local? / predicate behaviour, then restores development.
func TestRailsEnvSetterAndProduction(t *testing.T) {
	defer rails.SetEnv("development")
	cases := []struct{ src, want string }{
		{`require "rails/all"; Rails.env = "production"; p Rails.env.production?`, "true\n"},
		{`require "rails/all"; Rails.env = "production"; p Rails.env.local?`, "false\n"},
		{`require "rails/all"; Rails.env = "test"; p Rails.env.local?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsGroups covers Rails.groups: the ordered, de-duplicated group list for
// the current environment, with and without positional extras.
func TestRailsGroups(t *testing.T) {
	rails.SetEnv("development")
	cases := []struct{ src, want string }{
		{`require "rails/all"; p Rails.groups`, "[\"default\", \"development\"]\n"},
		{`require "rails/all"; p Rails.groups(:assets)`, "[\"default\", \"development\", \"assets\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsApplicationAccessors covers the application-state accessors: before an
// application is registered they are nil; once Rails.application= binds a railties
// Application they resolve against it (application / root / public_path /
// configuration); assigning nil clears it again.
func TestRailsApplicationAccessors(t *testing.T) {
	cases := []struct{ src, want string }{
		// Before boot: nil / nil / nil / nil.
		{`require "rails/all"
p Rails.application
p Rails.root
p Rails.public_path
p Rails.configuration`, "nil\nnil\nnil\nnil\n"},
		// Bound application: the accessors resolve against it.
		{`require "rails/all"
class Blog < Rails::Application; end
app = Blog.new("Blog", "/srv/blog")
app.paths.add "public", with: "public"
Rails.application = app
p Rails.application.equal?(app)
p Rails.root
p Rails.public_path
p Rails.configuration.is_a?(Rails::Railtie::Configuration)`,
			"true\n\"/srv/blog\"\n\"/srv/blog/public\"\ntrue\n"},
		// The bound configuration is the application's own config (round-trips a
		// value written through it).
		{`require "rails/all"
app = Rails::Application.new("Shop", "/srv/shop")
Rails.application = app
Rails.configuration.foo = 42
p app.config.foo`, "42\n"},
		// Assigning nil clears the registration.
		{`require "rails/all"
app = Rails::Application.new("Tmp", "/tmp/app")
Rails.application = app
Rails.application = nil
p Rails.application
p Rails.root`, "nil\nnil\n"},
		// A non-Application value is rejected.
		{`require "rails/all"; begin; Rails.application = 5; rescue TypeError; puts "type"; end`, "type\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsLoggerCacheAndStubs covers Rails.logger / Rails.cache (opaque any-typed
// slots that round-trip a Ruby value through their setters), the autoloaders
// accessor (nil — railties vends none), and the backtrace_cleaner / error stubs.
func TestRailsLoggerCacheAndStubs(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rails/all"; l = Object.new; Rails.logger = l; p Rails.logger.equal?(l)`, "true\n"},
		{`require "rails/all"; c = Object.new; Rails.cache = c; p Rails.cache.equal?(c)`, "true\n"},
		{`require "rails/all"; p Rails.autoloaders`, "nil\n"},
		{`require "rails/all"; p Rails.backtrace_cleaner`, "nil\n"},
		{`require "rails/all"; p Rails.error`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsSetterArity covers the arity guard on the module setters: invoking one
// with no argument (via send) raises ArgumentError.
func TestRailsSetterArity(t *testing.T) {
	setters := []string{"application=", "env=", "logger=", "cache="}
	for _, s := range setters {
		src := `require "rails/all"; begin; Rails.send(:"` + s + `"); rescue ArgumentError; puts "arity"; end`
		if got := eval(t, src); got != "arity\n" {
			t.Errorf("setter %q: got=%q want=%q", s, got, "arity\n")
		}
	}
}

// TestRailsComponentFeature covers the catalog-component → require-feature mapping
// for every shape: the railties and actionpack special cases, the active*/action*
// underscore split, and the pass-through fallback.
func TestRailsComponentFeature(t *testing.T) {
	cases := map[string]string{
		"railties":      "rails",
		"actionpack":    "action_controller",
		"activesupport": "active_support",
		"activemodel":   "active_model",
		"actionview":    "action_view",
		"actioncable":   "action_cable",
		"foobar":        "foobar",
	}
	for in, want := range cases {
		if got := railsComponentFeature(in); got != want {
			t.Errorf("railsComponentFeature(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRailsPreAndAny covers the two small value helpers: railsPre (empty → nil,
// non-empty → String) and railsAny (a stored Ruby value passes through, nil →
// Ruby nil).
func TestRailsPreAndAny(t *testing.T) {
	if v := railsPre(""); !object.IsNil(v) {
		t.Errorf("railsPre(\"\") = %v, want nil", v)
	}
	if v := railsPre("rc1"); v.ToS() != "rc1" {
		t.Errorf("railsPre(\"rc1\") = %q, want \"rc1\"", v.ToS())
	}
	s := object.NewString("x")
	if v := railsAny(s); v != s {
		t.Errorf("railsAny(str) did not pass through")
	}
	if v := railsAny(nil); !object.IsNil(v) {
		t.Errorf("railsAny(nil) = %v, want nil", v)
	}
	// A non-object.Value any (e.g. a bare Go value) also degrades to nil.
	if v := railsAny(42); !object.IsNil(v) {
		t.Errorf("railsAny(42) = %v, want nil", v)
	}
}

// TestRailsAppAdapter covers the App-seam adapter directly: Root / Config /
// Autoloaders / Cache and both PublicPath branches (a declared public path, and
// an application without one).
func TestRailsAppAdapter(t *testing.T) {
	app := railties.NewApplication("Blog", "/srv/blog")
	app.Paths().Add("public", railties.PathOpts{With: []string{"public"}})
	ad := railsAppAdapter{app: app}
	if ad.Root() != "/srv/blog" {
		t.Errorf("Root() = %q, want /srv/blog", ad.Root())
	}
	if ad.Config() == nil {
		t.Error("Config() = nil, want non-nil")
	}
	if ad.Autoloaders() != nil {
		t.Errorf("Autoloaders() = %v, want nil", ad.Autoloaders())
	}
	if ad.Cache() != nil {
		t.Errorf("Cache() = %v, want nil", ad.Cache())
	}
	if got := ad.PublicPath(); got != "/srv/blog/public" {
		t.Errorf("PublicPath() = %q, want /srv/blog/public", got)
	}
	bare := railsAppAdapter{app: railties.NewApplication("X", "/x")}
	if got := bare.PublicPath(); got != "" {
		t.Errorf("PublicPath() without public path = %q, want empty", got)
	}
}

// TestRailsEnvValFuncs covers the RailsEnvVal display methods (ToS / Inspect /
// Truthy).
func TestRailsEnvValFuncs(t *testing.T) {
	v := &RailsEnvVal{e: rails.NewEnvironmentInquirer("staging")}
	if v.ToS() != "staging" {
		t.Errorf("ToS() = %q", v.ToS())
	}
	if v.Inspect() != "\"staging\"" {
		t.Errorf("Inspect() = %q", v.Inspect())
	}
	if !v.Truthy() {
		t.Error("Truthy() = false")
	}
}
