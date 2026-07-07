// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// capReq is the require prelude prepended to every Capistrano DSL snippet. Each
// eval builds a fresh VM, so the DSL install and the deploy env are re-created
// per snippet (and never leak between cases).
const capReq = "require \"capistrano\"\n"

// TestCapistranoFeature covers the require probe (across the aliases), the once
// semantics, and the module / class / error-tree shape installed on require.
func TestCapistranoFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "capistrano"`, "true\n"},
		{`require "capistrano"; p require "capistrano"`, "false\n"},
		{`p require "capistrano/all"`, "true\n"},
		{`p require "capistrano/setup"`, "true\n"},
		{`p require "capistrano/deploy"`, "true\n"},
		// requiring one alias then another installs the DSL exactly once (idempotent).
		{`require "capistrano"; p require "capistrano/all"`, "true\n"},
		{capReq + `p Capistrano.is_a?(Module)`, "true\n"},
		{capReq + `p Capistrano::Server.is_a?(Class)`, "true\n"},
		{capReq + `p Capistrano::Session.is_a?(Class)`, "true\n"},
		{capReq + `p Capistrano::Task.is_a?(Class)`, "true\n"},
		{capReq + `p Capistrano::TestBackend.is_a?(Class)`, "true\n"},
		{capReq + `p Capistrano::Error < StandardError`, "true\n"},
		{capReq + `p Capistrano::TaskNotFoundError < Capistrano::Error`, "true\n"},
		{capReq + `p Capistrano::NoMatchingServersError < Capistrano::Error`, "true\n"},
		{capReq + `p Capistrano::CommandError < Capistrano::Error`, "true\n"},
		{capReq + `p Capistrano.backend.is_a?(Capistrano::TestBackend)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapistranoConfig covers the variable store: set/fetch with a plain value, a
// lazy Proc value, a lazy block value, a positional default, a block default, a
// missing key, and set?.
func TestCapistranoConfig(t *testing.T) {
	cases := []struct{ src, want string }{
		{capReq + `set :application, "shop"; p fetch(:application)`, "\"shop\"\n"},
		{capReq + `set("repo", "git@x"); p fetch("repo")`, "\"git@x\"\n"},
		// a Proc value is stored lazily and memoized on first fetch.
		{capReq + `set :deploy_to, ->{ "/srv/" + fetch(:application) }; set :application, "shop"; p fetch(:deploy_to)`, "\"/srv/shop\"\n"},
		// a block value is the same lazy seam.
		{capReq + `set(:answer) { 6 * 7 }; p fetch(:answer)`, "42\n"},
		{capReq + `p fetch(:missing)`, "nil\n"},
		{capReq + `p fetch(:missing, "fallback")`, "\"fallback\"\n"},
		{capReq + `p fetch(:missing) { "from_block" }`, "\"from_block\"\n"},
		{capReq + `set :x, 1; p [set?(:x), set?(:y)]`, "[true, false]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapistranoServers covers the server/role registry and the Server surface:
// role/server declaration, roles/release_roles/primary filtering, the connection
// triple parse, roles/has_role?, the primary? / no_release? flags, and per-server
// set/fetch (a boolean, a kept Ruby value, and an unset key).
func TestCapistranoServers(t *testing.T) {
	cases := []struct{ src, want string }{
		{capReq + `role :web, %w[web1 web2]; p roles(:web).length`, "2\n"},
		{capReq + `role :web, %w[web1 web2]; p roles(:web).map(&:hostname)`, "[\"web1\", \"web2\"]\n"},
		{capReq + `server "db1", roles: %w[db], primary: true; p primary(:db).to_s`, "\"db1\"\n"},
		{capReq + `server "db1", roles: %w[db], primary: true; p primary(:db).primary?`, "true\n"},
		// primary(role) with no explicit primary flag falls back to the first host.
		{capReq + `role :web, %w[web1 web2]; p primary(:web).hostname`, "\"web1\"\n"},
		{capReq + `p primary(:ghost)`, "nil\n"},
		{capReq + `server "deploy@web1:2222", roles: %w[web]; s = roles(:web).first; p [s.user, s.port]`, "[\"deploy\", 2222]\n"},
		{capReq + `server "web2", roles: %w[web]; s = roles(:web).first; p [s.user, s.port]`, "[nil, nil]\n"},
		{capReq + `server "web1", roles: %w[web app]; p roles(:web).first.roles`, "[:app, :web]\n"},
		{capReq + `server "web1", roles: %w[web]; p roles(:web).first.has_role?(:web)`, "true\n"},
		{capReq + `server "a", roles: %w[app], no_release: true; p roles(:app).first.no_release?`, "true\n"},
		{capReq + `server "a", roles: %w[app], no_release: true; server "b", roles: %w[app]; p release_roles(:app).map(&:hostname)`, "[\"b\"]\n"},
		// per-server set/fetch: a boolean, a kept Ruby value, an unset key.
		{capReq + `s = server "x", roles: %w[web]; s.set(:flag, true); p s.fetch(:flag)`, "true\n"},
		{capReq + `s = server "x", roles: %w[web]; s.set(:note, "hi"); p s.fetch(:note)`, "\"hi\"\n"},
		{capReq + `s = server "x", roles: %w[web]; p s.fetch(:absent)`, "nil\n"},
		// role with a shared property bag (the third positional Hash).
		{capReq + `role :web, %w[w1], primary: true; p primary(:web).primary?`, "true\n"},
		// a bare server with no roles is registered but plays no role.
		{capReq + `server "solo"; p roles(:web).length`, "0\n"},
		// a scalar (non-Array) host list is accepted as a single host (capStrList).
		{capReq + `role :web, "web1"; p roles(:web).length`, "1\n"},
		// a non-Symbol / non-String role name coerces via to_s (capName default).
		{capReq + `role :web, %w[w1]; p roles(5).length`, "0\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapistranoTasks covers the task graph: the three task-argument forms, the
// action-block seam and invoke ordering, before/after hooks (two-name and
// define-from-block), invoke! re-running, namespace scoping, an empty task, and
// the Task metadata readers.
func TestCapistranoTasks(t *testing.T) {
	cases := []struct{ src, want string }{
		// invoke runs the block; a before-hook prepends its prerequisite.
		{capReq + `o = []; task :a do o << "a" end; task :b do o << "b" end; before :b, :a; invoke :b; p o`, "[\"a\", \"b\"]\n"},
		// after with a define-from-block hook.
		{capReq + `o = []; task :build do o << "build" end; after :build, :notify do o << "notify" end; invoke :build; p o`, "[\"build\", \"notify\"]\n"},
		// the once-guard: invoke runs once, invoke! forces a re-run.
		{capReq + `o = []; task :t do o << "t" end; invoke :t; invoke! :t; p o.length`, "2\n"},
		// task :name => :dep form (a sole dependency Hash).
		{capReq + `task :y; t = task(:x => :y); p t.prerequisites`, "[\"y\"]\n"},
		// task :name, [deps] form.
		{capReq + `t = task :z, [:y]; p t.prerequisites`, "[\"y\"]\n"},
		// a nil dependency (the sole-Hash form) yields no prerequisites (capStrList nil).
		{capReq + `t = task(:x => nil); p t.prerequisites`, "[]\n"},
		// plain task: name / to_s.
		{capReq + `t = task :solo; p [t.name, t.to_s]`, "[\"solo\", \"solo\"]\n"},
		// desc attaches to the next task; an undescribed task reads nil.
		{capReq + `desc "Restart it"; t = task :r; p t.description`, "\"Restart it\"\n"},
		{capReq + `t = task :r; p t.description`, "nil\n"},
		// namespace scopes the task name.
		{capReq + `o = []; namespace :deploy do task :restart do o << "r" end end; invoke "deploy:restart"; p o`, "[\"r\"]\n"},
		// an empty task (no block) invokes cleanly.
		{capReq + `task :empty; invoke :empty; p "ok"`, "\"ok\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapistranoExecution covers the on(hosts) execution context and the
// recording backend: execute over multiple hosts, on a single host (primary),
// capture of scripted stdout, test, upload!/download!, and the backend command /
// upload / download logs and script forms.
func TestCapistranoExecution(t *testing.T) {
	cases := []struct{ src, want string }{
		// on(roles) runs the block once per host with the Session as self and the
		// host as the block argument; execute is recorded through the backend.
		{capReq + `role :web, %w[web1 web2]; out = []; on(roles(:web)) do |host| execute :uptime; out << host.to_s end; p out`, "[\"web1\", \"web2\"]\n"},
		{capReq + `role :web, %w[web1 web2]; on(roles(:web)) { execute :uptime }; p Capistrano.backend.commands`, "[\"uptime\", \"uptime\"]\n"},
		// on a single Server (primary) — the non-Array host form.
		{capReq + `role :web, %w[web1]; on(primary(:web)) { execute :ls }; p Capistrano.backend.commands`, "[\"ls\"]\n"},
		// capture returns the scripted, stripped stdout.
		{capReq + `role :web, %w[w1]; Capistrano.backend.script("readlink /current", stdout: "  /rel/1  "); r = nil; on(roles(:web)) { r = capture("readlink", "/current") }; p r`, "\"/rel/1\"\n"},
		// test reports a zero exit as true (unscripted commands succeed).
		{capReq + `role :web, %w[w1]; r = nil; on(roles(:web)) { r = test("[", "-d", "/x", "]") }; p r`, "true\n"},
		// execute returns true on success.
		{capReq + `role :web, %w[w1]; r = nil; on(roles(:web)) { r = execute(:true) }; p r`, "true\n"},
		// upload! / download! transfer and are logged.
		{capReq + `role :web, %w[w1]; on(roles(:web)) { upload! "l", "r"; download! "r", "l" }; p [Capistrano.backend.uploads, Capistrano.backend.downloads]`, "[[\"l -> r\"], [\"r -> l\"]]\n"},
		// the session's host reader.
		{capReq + `role :web, %w[web1]; h = nil; on(roles(:web)) { h = host }; p h.hostname`, "\"web1\"\n"},
		// script with a non-Hash second argument is ignored (defaults kept); an
		// unscripted command returns empty stdout.
		{capReq + `role :web, %w[w1]; Capistrano.backend.script("noop", "ignored"); r = nil; on(roles(:web)) { r = capture("noop") }; p r`, "\"\"\n"},
		// script honours stderr / a non-integer exit_status (ignored, stays 0).
		{capReq + `role :web, %w[w1]; Capistrano.backend.script("q", stderr: "e", exit_status: "bad"); r = nil; on(roles(:web)) { r = execute("q") }; p r`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapistranoValueReprs covers the Go ToS / Inspect / Truthy of every wrapper
// value: to_s (Object#to_s → Go ToS), inspect via p (Go Inspect), and the
// truthiness a Ruby conditional reads (Go Truthy).
func TestCapistranoValueReprs(t *testing.T) {
	cases := []struct{ src, want string }{
		// Server
		{capReq + `role :web, %w[web1]; puts roles(:web).first`, "web1\n"},
		{capReq + `role :web, %w[web1]; p roles(:web).first`, "#<Capistrano::Server web1>\n"},
		{capReq + `role :web, %w[web1]; p(roles(:web).first ? 1 : 2)`, "1\n"},
		// Task
		{capReq + `t = task :zz; puts t`, "zz\n"},
		{capReq + `t = task :zz; p t`, "#<Capistrano::Task zz>\n"},
		{capReq + `t = task :zz; p(t ? 1 : 2)`, "1\n"},
		// Backend
		{capReq + `puts Capistrano.backend`, "#<Capistrano::TestBackend>\n"},
		{capReq + `p Capistrano.backend`, "#<Capistrano::TestBackend>\n"},
		{capReq + `p(Capistrano.backend ? 1 : 2)`, "1\n"},
		// Session (self inside an on-block)
		{capReq + `role :web, %w[web1]; on(roles(:web)) { puts self }`, "#<Capistrano::Session web1>\n"},
		{capReq + `role :web, %w[web1]; on(roles(:web)) { p self }`, "#<Capistrano::Session web1>\n"},
		{capReq + `role :web, %w[web1]; on(roles(:web)) { p(self ? 1 : 2) }`, "1\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCapistranoErrors covers every raising branch: the four Capistrano error
// classes (command non-zero exit, transport failure, no matching servers, task
// not found, circular dependency, transfer failure) and the ArgumentError arity
// guards across the DSL, the Session, the Server and the backend.
func TestCapistranoErrors(t *testing.T) {
	cases := []struct{ src, class string }{
		// a non-zero exit raises Capistrano::CommandError (execute and capture).
		{capReq + `role :web, %w[w1]; Capistrano.backend.script("false", exit_status: 1); on(roles(:web)) { execute "false" }`, "Capistrano::CommandError"},
		{capReq + `role :web, %w[w1]; Capistrano.backend.script("boom", exit_status: 2); on(roles(:web)) { capture "boom" }`, "Capistrano::CommandError"},
		// a transport failure raises the base Capistrano::Error (execute / test).
		{capReq + `role :web, %w[w1]; Capistrano.backend.fail_transport("dial down"); on(roles(:web)) { execute "ls" }`, "Capistrano::Error"},
		{capReq + `role :web, %w[w1]; Capistrano.backend.fail_transport("dial down"); on(roles(:web)) { test "ok" }`, "Capistrano::Error"},
		{capReq + `role :web, %w[w1]; Capistrano.backend.fail_transport("dial down"); on(roles(:web)) { capture "x" }`, "Capistrano::Error"},
		// a transfer failure raises Capistrano::Error (upload! / download!).
		{capReq + `role :web, %w[w1]; Capistrano.backend.fail_uploads("no space"); on(roles(:web)) { upload! "l", "r" }`, "Capistrano::Error"},
		{capReq + `role :web, %w[w1]; Capistrano.backend.fail_downloads("gone"); on(roles(:web)) { download! "r", "l" }`, "Capistrano::Error"},
		// no matching servers (empty role and an empty array).
		{capReq + `on(roles(:none)) { execute :ls }`, "Capistrano::NoMatchingServersError"},
		{capReq + `on([]) { execute :ls }`, "Capistrano::NoMatchingServersError"},
		// a non-server, non-array host selects nothing.
		{capReq + `on(nil) { execute :ls }`, "Capistrano::NoMatchingServersError"},
		// task-graph failures.
		{capReq + `invoke "ghost"`, "Capistrano::TaskNotFoundError"},
		{capReq + `before "ghost", "x"`, "Capistrano::TaskNotFoundError"},
		{capReq + `invoke! "ghost"`, "Capistrano::TaskNotFoundError"},
		{capReq + `task :a => :b; task :b => :a; invoke :a`, "Capistrano::Error"},
		// ArgumentError arity guards — top-level DSL.
		{capReq + `set`, "ArgumentError"},
		{capReq + `fetch`, "ArgumentError"},
		{capReq + `set?`, "ArgumentError"},
		{capReq + `role :web`, "ArgumentError"},
		{capReq + `server`, "ArgumentError"},
		{capReq + `primary`, "ArgumentError"},
		{capReq + `desc`, "ArgumentError"},
		{capReq + `namespace`, "ArgumentError"},
		{capReq + `task`, "ArgumentError"},
		{capReq + `invoke`, "ArgumentError"},
		{capReq + `invoke!`, "ArgumentError"},
		{capReq + `on`, "ArgumentError"},
		{capReq + `before :only`, "ArgumentError"},
		{capReq + `after :only`, "ArgumentError"},
		// ArgumentError arity guards — Server / Session / backend instance methods.
		{capReq + `role :web, %w[w1]; roles(:web).first.has_role?`, "ArgumentError"},
		{capReq + `role :web, %w[w1]; roles(:web).first.fetch`, "ArgumentError"},
		{capReq + `role :web, %w[w1]; roles(:web).first.set(:k)`, "ArgumentError"},
		{capReq + `role :web, %w[w1]; on(roles(:web)) { execute }`, "ArgumentError"},
		{capReq + `role :web, %w[w1]; on(roles(:web)) { test }`, "ArgumentError"},
		{capReq + `role :web, %w[w1]; on(roles(:web)) { capture }`, "ArgumentError"},
		{capReq + `role :web, %w[w1]; on(roles(:web)) { upload! "l" }`, "ArgumentError"},
		{capReq + `role :web, %w[w1]; on(roles(:web)) { download! "r" }`, "ArgumentError"},
		{capReq + `Capistrano.backend.script`, "ArgumentError"},
		{capReq + `Capistrano.backend.fail_transport`, "ArgumentError"},
		{capReq + `Capistrano.backend.fail_uploads`, "ArgumentError"},
		{capReq + `Capistrano.backend.fail_downloads`, "ArgumentError"},
	}
	for _, c := range cases {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q got class=%q want=%q", c.src, class, c.class)
		}
	}
}
