// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestPuma covers the Ruby Puma module (backed by github.com/go-ruby-puma/puma,
// the pure-Go threaded Rack web server): the version constants, the
// Puma::Error/Puma::HttpParserError exception tree, the Puma::Configuration /
// Puma::DSL config-block surface, and the Puma::Server / Puma::ThreadPool
// lifecycle (new, run/start on an ephemeral port, running?, thread_pool, stop /
// halt). The request-serving round-trip is asserted in puma_internal_test.go,
// which needs to release the GVL around an in-process HTTP call.
func TestPuma(t *testing.T) {
	const req = `require "puma"; `
	for _, c := range []struct{ src, want string }{
		// Version constants.
		{`p(Puma::VERSION =~ /\A\d+\.\d+\.\d+\z/ ? :ok : :bad)`, ":ok\n"},
		{`p Puma.version.class`, "String\n"},
		{`p(Puma::Const::VERSION == Puma::VERSION)`, "true\n"},
		{`p(Puma::Const::PUMA_VERSION == Puma::VERSION)`, "true\n"},
		{`p Puma::Const::PUMA_SERVER_STRING.start_with?("puma ")`, "true\n"},

		// Exception tree.
		{`p(Puma::Error.ancestors.include?(StandardError))`, "true\n"},
		{`p(Puma::HttpParserError.ancestors.include?(Puma::Error))`, "true\n"},
		{`begin; raise Puma::Error, "x"; rescue => e; p e.message; end`, "\"x\"\n"},

		// Configuration + DSL.
		{`c = Puma::Configuration.new { |d| d.threads(2,8); d.workers(3); d.environment("production") }; o = c.options; p [o["min_threads"], o["max_threads"], o["workers"], o["environment"]]`, "[2, 8, 3, \"production\"]\n"},
		{`c = Puma::Configuration.new { |d| d.bind("tcp://0.0.0.0:9292"); d.port(3000, "127.0.0.1") }; p c.options["binds"]`, "[\"tcp://0.0.0.0:9292\", \"tcp://127.0.0.1:3000\"]\n"},
		{`c = Puma::Configuration.new { |d| d.port(4000) }; p c.options["binds"]`, "[\"tcp://0.0.0.0:4000\"]\n"},
		{`p Puma::Configuration.new.options["max_threads"]`, "5\n"},

		// DSL arity errors.
		{`begin; Puma::Configuration.new { |d| d.bind }; rescue ArgumentError; p :bind; end`, ":bind\n"},
		{`begin; Puma::Configuration.new { |d| d.port }; rescue ArgumentError; p :port; end`, ":port\n"},
		{`begin; Puma::Configuration.new { |d| d.threads(1) }; rescue ArgumentError; p :threads; end`, ":threads\n"},

		// Server construction + accessors.
		{`p Puma::Server.new(->(e){}).class`, "Puma::Server\n"},
		{`p Puma::Server.new(->(e){}).app.class`, "Proc\n"},
		{`c = Puma::Configuration.new { |d| d.threads(1,2) }; p Puma::Server.new(->(e){}, c).class`, "Puma::Server\n"},
		{`p Puma::Server.new(->(e){}, {min_threads: 1, max_threads: 3}).class`, "Puma::Server\n"},

		// Options-argument validation + unstarted-server errors.
		{`begin; Puma::Server.new; rescue ArgumentError; p :argerr; end`, ":argerr\n"},
		{`begin; Puma::Server.new(->(e){}, 5); rescue TypeError; p :typeerr; end`, ":typeerr\n"},
		{`s = Puma::Server.new(->(e){}); begin; s.port; rescue Puma::Error; p :notrunning; end`, ":notrunning\n"},
		{`begin; Puma::Server.new(->(e){}).run("127.0.0.1", -1); rescue Puma::Error; p :binderr; end`, ":binderr\n"},

		// Lifecycle: bind an ephemeral port, read the address back, tear down.
		{`s = Puma::Server.new(->(e){}); s.run("127.0.0.1", 0); r1 = s.running?; s.halt; r2 = s.running?; p [r1, r2]`, "[true, false]\n"},
		{`s = Puma::Server.new(->(e){}); s.run("127.0.0.1", 0); a = [s.host, s.port > 0, s.url.start_with?("http://127.0.0.1:"), s.address.start_with?("127.0.0.1:")]; s.stop; p a`, "[\"127.0.0.1\", true, true, true]\n"},
		{`s = Puma::Server.new(->(e){}); s.start; tp = s.thread_pool; r = [tp.spawned >= 0, tp.backlog >= 0, tp.class == Puma::ThreadPool]; tp.trim; tp.trim(true); tp.shutdown; s.stop; p r`, "[true, true, true]\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
