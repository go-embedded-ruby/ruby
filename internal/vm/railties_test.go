// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestRailtiesFeature covers the require "rails" feature probe and the module /
// class tree shape (Rails::Railtie < Object, Engine < Railtie, Application <
// Engine, plus the supporting classes).
func TestRailtiesFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "rails"`, "true\n"},
		{`require "rails"; p require "rails"`, "false\n"},
		{`p require "rails/railtie"`, "true\n"},
		{`p require "rails/engine"`, "true\n"},
		{`p require "rails/application"`, "true\n"},
		{`require "rails"; p Rails.is_a?(Module)`, "true\n"},
		{`require "rails"; p Rails::Railtie.is_a?(Class)`, "true\n"},
		{`require "rails"; p Rails::Engine < Rails::Railtie`, "true\n"},
		{`require "rails"; p Rails::Application < Rails::Engine`, "true\n"},
		{`require "rails"; p Rails::Railtie::Configuration.is_a?(Class)`, "true\n"},
		{`require "rails"; p Rails::Paths::Root.is_a?(Class)`, "true\n"},
		{`require "rails"; p Rails::Paths::Path.is_a?(Class)`, "true\n"},
		{`require "rails"; p Rails::Engine::RouteSet.is_a?(Class)`, "true\n"},
		{`require "rails"; p Rails::Initializable::Initializer.is_a?(Class)`, "true\n"},
		{`require "rails"; p Rails::StringInquirer.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailtie covers Rails::Railtie: construction (explicit name, class-name
// default, anonymous subclass), name / railtie_name, the config bag and the
// initializers reader.
func TestRailtie(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rails"; p Rails::Railtie.new("MyEngine::Railtie").name`, "\"MyEngine::Railtie\"\n"},
		{`require "rails"; p Rails::Railtie.new("MyEngine::Railtie").railtie_name`, "\"my_engine\"\n"},
		{`require "rails"; class Foo < Rails::Railtie; end; p Foo.new.name`, "\"Foo\"\n"},
		{`require "rails"; class Foo < Rails::Railtie; end; p Foo.new.class`, "Foo\n"},
		{`require "rails"; p Class.new(Rails::Railtie).new.name`, "\"\"\n"},
		{`require "rails"
rt = Rails::Railtie.new("J")
rt.config["a"] = 1
p rt.config["a"]
p rt.config["missing"]
rt.config.foo = 2
p rt.config.foo
p rt.config.bar`, "1\nnil\n2\nnil\n"},
		{`require "rails"
rt = Rails::Railtie.new("K")
rt.initializer("boot") { }
p rt.initializers.map { |i| i.name }`, "[\"boot\"]\n"},
		// A block-less initializer registers a body-less entry (its body is deferred
		// to the RunInitializer seam, a no-op here).
		{`require "rails"
rt = Rails::Railtie.new("K")
rt.initializer("noblock")
p rt.initializers.map { |i| i.name }`, "[\"noblock\"]\n"},
		{`require "rails"; begin; Rails::Railtie.new("K").initializer; rescue ArgumentError; puts "arity"; end`, "arity\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailtieHooks covers the rake_tasks / console / generators / runner / server
// extension points and their run_* drivers — each runs its captured Ruby block
// inline.
func TestRailtieHooks(t *testing.T) {
	src := `require "rails"
rt = Rails::Railtie.new("H")
rt.rake_tasks { puts "rake" }
rt.console { puts "console" }
rt.generators { puts "gen" }
rt.runner { puts "runner" }
rt.server { puts "server" }
rt.run_rake_tasks
rt.run_console
rt.run_generators
rt.run_runner
rt.run_server`
	if got := eval(t, src); got != "rake\nconsole\ngen\nrunner\nserver\n" {
		t.Errorf("got %q", got)
	}
}

// TestRailtieConfigGuards covers the NoMethodError guards raised when the
// Engine/Application-only configuration accessors are called on a bare railtie
// configuration.
func TestRailtieConfigGuards(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rails"; begin; Rails::Railtie.new("K").config.paths; rescue NoMethodError; puts "no_paths"; end`, "no_paths\n"},
		{`require "rails"; begin; Rails::Railtie.new("K").config.root; rescue NoMethodError; puts "no_root"; end`, "no_root\n"},
		{`require "rails"; begin; Rails::Railtie.new("K").config.root = "/x"; rescue NoMethodError; puts "no_root_set"; end`, "no_root_set\n"},
		{`require "rails"; begin; Rails::Railtie.new("K").config.load_defaults("7.1"); rescue NoMethodError; puts "no_ld"; end`, "no_ld\n"},
		{`require "rails"; begin; Rails::Railtie.new("K").config.loaded_defaults; rescue NoMethodError; puts "no_ldd"; end`, "no_ldd\n"},
		{`require "rails"; begin; Rails::Railtie.new("K").config.eager_load; rescue NoMethodError; puts "no_el"; end`, "no_el\n"},
		{`require "rails"; begin; Rails::Railtie.new("K").config.eager_load = true; rescue NoMethodError; puts "no_el_set"; end`, "no_el_set\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsEngine covers Rails::Engine: construction + root default, the paths
// DSL and route set, namespace isolation (class, symbol and string forms) and the
// engine-name derivation, plus the isolate_namespace arity guard.
func TestRailsEngine(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rails"; p Rails::Engine.new("MyEng").config.root`, "\"\"\n"},
		{`require "rails"; p Rails::Engine.new("MyEng", "/app").config.root`, "\"/app\"\n"},
		{`require "rails"; p Rails::Engine.new("MyEng").engine_name`, "\"my_eng\"\n"},
		{`require "rails"
eng = Rails::Engine.new("MyEng")
eng.isolate_namespace("Blog")
p eng.isolated?
p eng.namespace
p eng.engine_name`, "true\n\"Blog\"\n\"blog\"\n"},
		{`require "rails"
module Blog; end
eng = Rails::Engine.new("MyEng")
eng.isolate_namespace(Blog)
p eng.namespace`, "\"Blog\"\n"},
		{`require "rails"
eng = Rails::Engine.new("MyEng")
eng.isolate_namespace(:Shop)
p eng.namespace`, "\"Shop\"\n"},
		{`require "rails"; begin; Rails::Engine.new("E").isolate_namespace; rescue ArgumentError; puts "arity"; end`, "arity\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsEngineConfig covers the Engine-level configuration: root/root=, the
// paths accessor, and the Application-only accessors raising NoMethodError on an
// engine configuration.
func TestRailsEngineConfig(t *testing.T) {
	src := `require "rails"
eng = Rails::Engine.new("L", "/app")
p eng.config.root
eng.config.root = "/new"
p eng.config.root
p eng.config.paths.class
begin; eng.config.load_defaults("7.1"); rescue NoMethodError; puts "no_ld_eng"; end
begin; eng.config.loaded_defaults; rescue NoMethodError; puts "no_ldd_eng"; end
begin; eng.config.eager_load; rescue NoMethodError; puts "no_el_eng"; end
begin; eng.config.eager_load = true; rescue NoMethodError; puts "no_el_set_eng"; end`
	want := "\"/app\"\n\"/new\"\nRails::Paths::Root\nno_ld_eng\nno_ldd_eng\nno_el_eng\nno_el_set_eng\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestRailsAppConfig covers the Application configuration: load_defaults (Float,
// String and #to_s-fallback version forms, plus the unknown-version error),
// loaded_defaults and eager_load / eager_load=.
func TestRailsAppConfig(t *testing.T) {
	src := `require "rails"
app = Rails::Application.new("M", "/app")
app.config.load_defaults(7.1)
p app.config.loaded_defaults
app.config.load_defaults("8.0")
p app.config.loaded_defaults
p app.config.eager_load
app.config.eager_load = true
p app.config.eager_load
begin; app.config.load_defaults(9); rescue ArgumentError; puts "bad_version"; end`
	want := "\"7.1\"\n\"8.0\"\nfalse\ntrue\nbad_version\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestRailsConfigArity covers the []= and method_missing setter argument guards.
func TestRailsConfigArity(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rails"; begin; Rails::Application.new("M").config.send(:[]=, "k"); rescue ArgumentError; puts "arity1"; end`, "arity1\n"},
		{`require "rails"; begin; Rails::Application.new("M").config.send(:foo=); rescue ArgumentError; puts "arity2"; end`, "arity2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsApplication covers Rails::Application: env, initialized?, the railtie
// collection and the add_railtie guards, plus the full initializers boot chain.
func TestRailsApplication(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rails"; p Rails::Application.new("I").env.class`, "Rails::StringInquirer\n"},
		{`require "rails"; p Rails::Application.new("I").initialized?`, "false\n"},
		{`require "rails"
app = Rails::Application.new("D")
rt = Rails::Railtie.new("Sub")
app.add_railtie(rt)
p app.railties.map { |r| r.name }`, "[\"Sub\"]\n"},
		{`require "rails"; begin; Rails::Application.new("D").add_railtie; rescue ArgumentError; puts "arity"; end`, "arity\n"},
		{`require "rails"; begin; Rails::Application.new("D").add_railtie("nope"); rescue TypeError => e; puts e.message; end`,
			"wrong argument type String (expected a Rails::Railtie)\n"},
		{`require "rails"
names = Rails::Application.new("Q").initializers.map { |i| i.name }
p names.include?("load_environment_hook")
p names.include?("finisher_hook")`, "true\ntrue\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsAppBoot is the seam test: it drives Application#initialize! and asserts
// that the recorded Ruby initializer blocks — the app's own and those of added
// railties/engines — run inline in topological order, that the load hooks fire,
// and that the boot guards (already-initialized, initializer cycle) map to Ruby
// exceptions.
func TestRailsAppBoot(t *testing.T) {
	cases := []struct{ src, want string }{
		// The app's own initializer block runs inline via the RunInitializer seam.
		{`require "rails"
app = Rails::Application.new("A")
app.initializer("first") { puts "1" }
app.initializer("second", after: "first") { puts "2" }
app.initialize!
p app.initialized?`, "1\n2\ntrue\n"},
		// A before: constraint reorders the chain.
		{`require "rails"
app = Rails::Application.new("B")
app.initializer("main") { puts "m" }
app.initializer("pre", before: "main") { puts "p" }
app.initialize!`, "p\nm\n"},
		// A group: :all initializer runs, and initialize! takes an explicit group.
		{`require "rails"
app = Rails::Application.new("C")
app.initializer("g", group: :all) { puts "g" }
app.initialize!(:default)`, "g\n"},
		// Added railties' and engines' initializer blocks run in registration order.
		{`require "rails"
app = Rails::Application.new("D")
rt = Rails::Railtie.new("MyRailtie")
rt.initializer("rt_init") { puts "rt" }
eng = Rails::Engine.new("MyEng")
eng.initializer("eng_init") { puts "eng" }
app.add_railtie(rt)
app.add_railtie(eng)
app.initialize!`, "rt\neng\n"},
		// A bare app (no own initializers, no railties) boots — the bootstrap /
		// finisher initializers dispatch through the seam with no recorded block.
		{`require "rails"; p Rails::Application.new("E").initialize!.initialized?`, "true\n"},
		// before_initialize / after_initialize load hooks fire around the boot.
		{`require "rails"
app = Rails::Application.new("G")
app.on_load(:before_initialize) { puts "B" }
app.on_load(:after_initialize) { puts "A" }
app.initialize!`, "B\nA\n"},
		// A second initialize! is a RuntimeError.
		{`require "rails"
app = Rails::Application.new("F")
app.initialize!
begin
  app.initialize!
rescue RuntimeError => e
  puts e.message
end`, "Application has been already initialized.\n"},
		// An initializer cycle is a RuntimeError.
		{`require "rails"
app = Rails::Application.new("Z")
app.initializer("a", after: "b") { }
app.initializer("b", after: "a") { }
begin
  app.initialize!
rescue RuntimeError => e
  puts e.message.include?("cycle")
end`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsLoadHooks covers the lazy load-hook contract (on_load / run_load_hooks)
// in both orders and the argument guards.
func TestRailsLoadHooks(t *testing.T) {
	cases := []struct{ src, want string }{
		// register-then-fire.
		{`require "rails"
app = Rails::Application.new("A")
app.on_load(:foo) { puts "loaded" }
app.run_load_hooks(:foo)`, "loaded\n"},
		// fire-then-register replays against the remembered base.
		{`require "rails"
app = Rails::Application.new("A")
app.run_load_hooks(:bar)
app.on_load(:bar) { puts "replay" }`, "replay\n"},
		// run_load_hooks accepts an explicit base argument.
		{`require "rails"; p Rails::Application.new("A").run_load_hooks(:baz, 42).class`, "Rails::Application\n"},
		{`require "rails"; begin; Rails::Application.new("A").on_load; rescue ArgumentError; puts "arity1"; end`, "arity1\n"},
		{`require "rails"; begin; Rails::Application.new("A").run_load_hooks; rescue ArgumentError; puts "arity2"; end`, "arity2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsAppEngineSurface covers the Engine accessors resolving an Application
// self (Rails::Application inherits the engine surface): paths, routes and
// isolate_namespace on an application.
func TestRailsAppEngineSurface(t *testing.T) {
	src := `require "rails"
app = Rails::Application.new("App", "/app")
app.paths.add("app/models", autoload: true)
p app.paths["app/models"].autoload?
app.routes.draw { }
p app.routes.draws
app.isolate_namespace("Store")
p app.engine_name`
	want := "true\n1\n\"store\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestRailsPaths covers Rails::Paths::Root and Rails::Paths::Path: add with the
// full option set, the classification predicates and aggregators, the missing-key
// nil, root/root=, and the Path push / << / unshift / to_a / expanded surface.
func TestRailsPaths(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rails"
paths = Rails::Engine.new("N", "/app").paths
paths.add("app/models", eager_load: true, autoload: true, autoload_once: true, load_path: true, with: ["app/models"], glob: "**/*.rb")
p paths["app/models"].eager_load?
p paths["app/models"].autoload?
p paths["app/models"].autoload_once?
p paths["app/models"].load_path?
p paths.keys
p paths.eager_load
p paths.autoload_paths
p paths.autoload_once
p paths.load_paths
p paths["missing"]`,
			"true\ntrue\ntrue\ntrue\n[\"app/models\"]\n[\"/app/app/models\"]\n[\"/app/app/models\"]\n[\"/app/app/models\"]\n[\"/app/app/models\"]\nnil\n"},
		{`require "rails"
paths = Rails::Engine.new("N", "/app").paths
p paths.root
paths.root = "/other"
p paths.root`, "\"/app\"\n\"/other\"\n"},
		{`require "rails"
p2 = Rails::Engine.new("N", "/app").paths.add("config")
p2.push("a", "b")
p2 << "c"
p2.unshift("z")
p p2.to_a
p p2.expanded`,
			"[\"z\", \"config\", \"a\", \"b\", \"c\"]\n[\"/app/z\", \"/app/config\", \"/app/a\", \"/app/b\", \"/app/c\"]\n"},
		{`require "rails"; begin; Rails::Engine.new("N").paths.add; rescue ArgumentError; puts "arity"; end`, "arity\n"},
		// The bang mutators flag a path's classification, read back by the predicates.
		{`require "rails"
p2 = Rails::Engine.new("N", "/app").paths.add("lib")
p2.eager_load!
p2.autoload!
p2.autoload_once!
p2.load_path!
p p2.eager_load?
p p2.autoload?
p p2.autoload_once?
p p2.load_path?`, "true\ntrue\ntrue\ntrue\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailsRouteSet covers Rails::Engine::RouteSet: draw records a block that #run
// replays, plus draws and default_scope (which isolate_namespace sets).
func TestRailsRouteSet(t *testing.T) {
	src := `require "rails"
eng = Rails::Engine.new("O")
r = eng.routes
r.draw { puts "drew" }
p r.draws
p r.default_scope
r.run
eng.isolate_namespace("Blog")
p eng.routes.default_scope`
	want := "1\n\"\"\ndrew\n\"Blog\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestRailsInitializerHandle covers Rails::Initializable::Initializer: name and
// the before/after/group readers (present and absent) plus belongs_to?.
func TestRailsInitializerHandle(t *testing.T) {
	src := `require "rails"
rt = Rails::Railtie.new("P")
rt.initializer("plain") { }
rt.initializer("boot", before: "web", after: "db", group: :all) { }
inits = rt.initializers
p inits.map { |i| i.name }
p inits[0].after
p inits[0].before
p inits[0].group
p inits[1].before
p inits[1].after
p inits[1].group
p inits[1].belongs_to?("all")`
	want := "[\"plain\", \"boot\"]\nnil\nnil\n\"\"\n\"web\"\n\"db\"\n\"all\"\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestRailsStringInquirer covers Rails::StringInquirer: .new, to_s / to_str, ==,
// the dynamic `foo?` predicate and the NoMethodError for a non-predicate missing
// method.
func TestRailsStringInquirer(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rails"; p Rails::StringInquirer.new("development").to_s`, "\"development\"\n"},
		{`require "rails"; p Rails::StringInquirer.new("development").to_str`, "\"development\"\n"},
		{`require "rails"; p Rails::StringInquirer.new("development").development?`, "true\n"},
		{`require "rails"; p Rails::StringInquirer.new("development").production?`, "false\n"},
		{`require "rails"; p Rails::StringInquirer.new("development") == "development"`, "true\n"},
		{`require "rails"; p Rails::StringInquirer.new("development") == "production"`, "false\n"},
		{`require "rails"; p Rails::StringInquirer.new("development")`, "\"development\"\n"},
		{`require "rails"; begin; Rails::StringInquirer.new("dev").frobnicate; rescue NoMethodError; puts "nomethod"; end`, "nomethod\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRailtiesValueReprs covers the ToS / Inspect / Truthy of every wrapper value
// type (inspect via p, truthiness via a ternary that forces Truthy).
func TestRailtiesValueReprs(t *testing.T) {
	src := `require "rails"
rt = Rails::Railtie.new("R")
eng = Rails::Engine.new("E", "/e")
app = Rails::Application.new("A", "/a")
rt.initializer("i") { }
pa = eng.paths.add("x")
si = Rails::StringInquirer.new("dev")
p rt
p eng
p app
p rt.config
p eng.paths
p pa
p eng.routes
p rt.initializers[0]
p si
puts [rt, eng, app, rt.config, eng.paths, pa, eng.routes, rt.initializers[0], si].map { |o| o ? "t" : "f" }.join`
	want := "#<Rails::Railtie:R>\n" +
		"#<Rails::Engine:E>\n" +
		"#<Rails::Application:A>\n" +
		"#<Rails::Railtie::Configuration>\n" +
		"#<Rails::Paths::Root>\n" +
		"#<Rails::Paths::Path>\n" +
		"#<Rails::Engine::RouteSet>\n" +
		"#<Rails::Initializable::Initializer:i>\n" +
		"\"dev\"\n" +
		"ttttttttt\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
