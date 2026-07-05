// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// bundlerLock is a real, canonical Gemfile.lock exercising every section: a GEM
// source with a nested-dependency spec (rspec -> rake), PLATFORMS, DEPENDENCIES
// (a pinned "~>" and a bare gem), RUBY VERSION and BUNDLED WITH. It round-trips
// byte-for-byte through the library, so Bundler::LockfileParser#to_lock must
// reproduce it exactly.
const bundlerLock = `GEM
  remote: https://rubygems.org/
  specs:
    ast (2.4.3)
    rake (13.4.2)
    rspec (3.13.0)
      rake (~> 13.0)

PLATFORMS
  ruby

DEPENDENCIES
  rake (~> 13.0)
  rspec

RUBY VERSION
   ruby 3.4.1p0

BUNDLED WITH
   2.6.9
`

// bundlerMinLock is a lock with neither a RUBY VERSION nor a BUNDLED WITH
// section, so the optional accessors return nil.
const bundlerMinLock = `GEM
  remote: https://rubygems.org/
  specs:
    rake (13.4.2)

PLATFORMS
  ruby

DEPENDENCIES
  rake
`

// bundlerHeredoc binds a multi-line lock/Gemfile literal to name via a plain
// (non-interpolated, non-stripping) heredoc so the significant leading
// indentation reaches the parser verbatim.
func bundlerHeredoc(name, body string) string {
	return name + " = <<'BUNDLERDOC'\n" + body + "BUNDLERDOC\n"
}

// TestBundlerFeature covers the require probe and the module / error-tree /
// class shape.
func TestBundlerFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "bundler"`, "true\n"},
		{`require "bundler"; p require "bundler"`, "false\n"},
		{`require "bundler"; p Bundler.is_a?(Module)`, "true\n"},
		{`require "bundler"; p Bundler::VERSION`, "\"2.6.9\"\n"},
		{`require "bundler"; p Bundler::BundlerError < StandardError`, "true\n"},
		{`require "bundler"; p Bundler::GemfileError < Bundler::BundlerError`, "true\n"},
		{`require "bundler"; p Bundler::LockfileError < Bundler::BundlerError`, "true\n"},
		{`require "bundler"; p Bundler::VersionConflict < Bundler::BundlerError`, "true\n"},
		{`require "bundler"; p Bundler::GemNotFound < Bundler::BundlerError`, "true\n"},
		{`require "bundler"; p Bundler::LockfileParser.is_a?(Class)`, "true\n"},
		{`require "bundler"; p Bundler::LazySpecification.is_a?(Class)`, "true\n"},
		{`require "bundler"; p Bundler::Dependency.is_a?(Class)`, "true\n"},
		{`require "bundler"; p Bundler::Dsl.is_a?(Class)`, "true\n"},
		{`require "bundler"; p Bundler::Index.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBundlerLockfileParser covers Bundler::LockfileParser: the byte-exact
// round-trip and every read accessor, including the spec set with a nested
// dependency and the optional-section nil results.
func TestBundlerLockfileParser(t *testing.T) {
	pre := "require \"bundler\"\n" + bundlerHeredoc("lock", bundlerLock)
	min := "require \"bundler\"\n" + bundlerHeredoc("lock", bundlerMinLock)
	cases := []struct{ src, want string }{
		// Byte-for-byte re-emission of a real lock.
		{pre + `p Bundler::LockfileParser.new(lock).to_lock == lock`, "true\n"},
		{pre + `p Bundler::LockfileParser.new(lock).to_s == lock`, "true\n"},
		{pre + `p Bundler::LockfileParser.new(lock).platforms`, "[\"ruby\"]\n"},
		{pre + `p Bundler::LockfileParser.new(lock).bundler_version`, "\"2.6.9\"\n"},
		{pre + `p Bundler::LockfileParser.new(lock).ruby_version`, "\"ruby 3.4.1p0\"\n"},
		{pre + `p Bundler::LockfileParser.new(lock).dependencies.map(&:name)`, "[\"rake\", \"rspec\"]\n"},
		{pre + `p Bundler::LockfileParser.new(lock).dependencies.map(&:requirement)`, "[\"~> 13.0\", \">= 0\"]\n"},
		{pre + `p Bundler::LockfileParser.new(lock).specs.map(&:name)`, "[\"ast\", \"rake\", \"rspec\"]\n"},
		{pre + `p Bundler::LockfileParser.new(lock).specs.map(&:version)`, "[\"2.4.3\", \"13.4.2\", \"3.13.0\"]\n"},
		{pre + `p Bundler::LockfileParser.new(lock).specs.map(&:platform).uniq`, "[\"ruby\"]\n"},
		{pre + `p Bundler::LockfileParser.new(lock).specs.map(&:full_name)`, "[\"ast-2.4.3\", \"rake-13.4.2\", \"rspec-3.13.0\"]\n"},
		{pre + `p Bundler::LockfileParser.new(lock).specs.map(&:to_s)`, "[\"ast-2.4.3\", \"rake-13.4.2\", \"rspec-3.13.0\"]\n"},
		// The nested dependency of the rspec spec.
		{pre + `s = Bundler::LockfileParser.new(lock).specs.find { |x| x.name == "rspec" }; p s.dependencies.map(&:name)`, "[\"rake\"]\n"},
		{pre + `s = Bundler::LockfileParser.new(lock).specs.find { |x| x.name == "rspec" }; p s.dependencies.map(&:requirement)`, "[\"~> 13.0\"]\n"},
		// Optional sections absent -> nil.
		{min + `p Bundler::LockfileParser.new(lock).ruby_version`, "nil\n"},
		{min + `p Bundler::LockfileParser.new(lock).bundler_version`, "nil\n"},
		// Inspect / to_s of the parser wrapper.
		{pre + `p Bundler::LockfileParser.new(lock)`, "#<Bundler::LockfileParser>\n"},
		// Malformed lock -> Bundler::LockfileError.
		{`require "bundler"; begin; Bundler::LockfileParser.new("GEM\n<<<<<<< HEAD\n"); rescue Bundler::LockfileError => e; p e.is_a?(Bundler::BundlerError); end`, "true\n"},
		// Missing argument -> ArgumentError.
		{`require "bundler"; begin; Bundler::LockfileParser.new; rescue ArgumentError; p :argerr; end`, ":argerr\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBundlerDsl covers Bundler::Dsl.evaluate: reading the canonical Gemfile
// forms and reporting a Bundler::GemfileError on an arbitrary-Ruby form.
func TestBundlerDsl(t *testing.T) {
	gemfile := "source \"https://rubygems.org\"\n" +
		"ruby \"3.4.1\"\n" +
		"gem \"rake\", \"~> 13.0\"\n" +
		"gem \"nokogiri\", platforms: [:ruby]\n" +
		"group :test do\n  gem \"rspec\"\nend\n"
	pre := "require \"bundler\"\n" + bundlerHeredoc("gf", gemfile)
	noruby := "require \"bundler\"\n" + bundlerHeredoc("gf", "source \"https://rubygems.org\"\ngem \"rake\"\n")
	cases := []struct{ src, want string }{
		{pre + `p Bundler::Dsl.evaluate(gf).sources`, "[\"https://rubygems.org\"]\n"},
		{pre + `p Bundler::Dsl.evaluate(gf).ruby_version`, "\"3.4.1\"\n"},
		{pre + `p Bundler::Dsl.evaluate(gf).dependencies.map(&:name)`, "[\"rake\", \"nokogiri\", \"rspec\"]\n"},
		{pre + `p Bundler::Dsl.evaluate(gf).dependencies.map(&:requirement)`, "[\"~> 13.0\", \">= 0\", \">= 0\"]\n"},
		{pre + `d = Bundler::Dsl.evaluate(gf).dependencies.find { |x| x.name == "rspec" }; p d.groups`, "[:test]\n"},
		{pre + `d = Bundler::Dsl.evaluate(gf).dependencies.find { |x| x.name == "nokogiri" }; p d.platforms`, "[\"ruby\"]\n"},
		{pre + `p Bundler::Dsl.evaluate(gf).dependencies.first.to_s`, "\"rake (~> 13.0)\"\n"},
		{pre + `p Bundler::Dsl.evaluate(gf)`, "#<Bundler::Dsl>\n"},
		{noruby + `p Bundler::Dsl.evaluate(gf).ruby_version`, "nil\n"},
		// Arbitrary-Ruby form -> Bundler::GemfileError naming the line.
		{`require "bundler"; begin; Bundler::Dsl.evaluate("gem foo"); rescue Bundler::GemfileError => e; p e.is_a?(Bundler::BundlerError); end`, "true\n"},
		{`require "bundler"; begin; Bundler::Dsl.evaluate; rescue ArgumentError; p :argerr; end`, ":argerr\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBundlerDependency covers the Bundler::Dependency constructor and readers.
func TestBundlerDependency(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "bundler"; p Bundler::Dependency.new("rake", "~> 13.0").name`, "\"rake\"\n"},
		{`require "bundler"; p Bundler::Dependency.new("rake", "~> 13.0").requirement`, "\"~> 13.0\"\n"},
		{`require "bundler"; p Bundler::Dependency.new("rake", ">= 1.0", "< 2.0").requirement`, "\">= 1.0, < 2.0\"\n"},
		{`require "bundler"; p Bundler::Dependency.new("rake").requirement`, "\">= 0\"\n"},
		{`require "bundler"; p Bundler::Dependency.new("rake").groups`, "[]\n"},
		{`require "bundler"; p Bundler::Dependency.new("rake").platforms`, "[]\n"},
		{`require "bundler"; p Bundler::Dependency.new("rake", "~> 13.0").to_s`, "\"rake (~> 13.0)\"\n"},
		{`require "bundler"; p Bundler::Dependency.new("rake")`, "#<Bundler::Dependency rake>\n"},
		{`require "bundler"; d = Bundler::Dependency.new("rake"); p(d ? 1 : 0)`, "1\n"},
		// Malformed constraint -> ArgumentError.
		{`require "bundler"; begin; Bundler::Dependency.new("rake", "notaversion"); rescue ArgumentError; p :argerr; end`, ":argerr\n"},
		{`require "bundler"; begin; Bundler::Dependency.new; rescue ArgumentError; p :argerr; end`, ":argerr\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBundlerResolver covers the backtracking resolver over an in-memory
// Bundler::Index: a successful resolution and a VersionConflict, plus the
// argument-shape guards.
func TestBundlerResolver(t *testing.T) {
	build := "require \"bundler\"\n" +
		"idx = Bundler::Index.new\n" +
		"idx.add_gem(\"rake\", \"13.4.2\")\n" +
		"idx.add_gem(\"rspec\", \"3.13.0\", \"rake\" => \"~> 13.0\")\n"
	conflict := "require \"bundler\"\n" +
		"idx = Bundler::Index.new\n" +
		"idx.add_gem(\"rake\", \"13.4.2\")\n" +
		"idx.add_gem(\"rspec\", \"3.13.0\", \"rake\" => \"~> 12.0\")\n"
	cases := []struct{ src, want string }{
		{build + `p Bundler.resolve([Bundler::Dependency.new("rspec")], idx).map(&:name)`, "[\"rake\", \"rspec\"]\n"},
		{build + `p Bundler.resolve([Bundler::Dependency.new("rspec")], idx).map(&:version)`, "[\"13.4.2\", \"3.13.0\"]\n"},
		{build + `p Bundler.resolve([Bundler::Dependency.new("rake")], idx).map(&:name)`, "[\"rake\"]\n"},
		// Adding a gem returns the index (chainable) and the wrapper inspects.
		{build + `p idx`, "#<Bundler::Index>\n"},
		// A conflicting nested requirement -> Bundler::VersionConflict.
		{conflict + `begin; Bundler.resolve([Bundler::Dependency.new("rspec"), Bundler::Dependency.new("rake", "~> 13.0")], idx); rescue Bundler::VersionConflict => e; p e.is_a?(Bundler::BundlerError); end`, "true\n"},
		// add_gem with a malformed version -> ArgumentError.
		{`require "bundler"; idx = Bundler::Index.new; begin; idx.add_gem("rake", "notaversion"); rescue ArgumentError; p :argerr; end`, ":argerr\n"},
		{`require "bundler"; idx = Bundler::Index.new; begin; idx.add_gem("rake"); rescue ArgumentError; p :argerr; end`, ":argerr\n"},
		// resolve argument-shape guards.
		{`require "bundler"; begin; Bundler.resolve([]); rescue ArgumentError; p :argerr; end`, ":argerr\n"},
		{`require "bundler"; idx = Bundler::Index.new; begin; Bundler.resolve("nope", idx); rescue TypeError; p :typeerr; end`, ":typeerr\n"},
		{`require "bundler"; begin; Bundler.resolve([], "nope"); rescue TypeError; p :typeerr; end`, ":typeerr\n"},
		{`require "bundler"; idx = Bundler::Index.new; begin; Bundler.resolve([1], idx); rescue TypeError; p :typeerr; end`, ":typeerr\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
