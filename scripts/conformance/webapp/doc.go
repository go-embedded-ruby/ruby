// Package webapp is a reference-app RUN-conformance harness for rbgo: it proves
// that the embedded interpreter can actually *run* real Ruby web apps
// end-to-end (run, not merely parse), by driving the Rack `call(env)` contract
// directly with a synthetic Rack environment — no TCP socket, no network.
//
// Each staged test in webapp_test.go loads a real Ruby app from apps/*.rb,
// executes it through the public ruby.Run API, and asserts the deterministic
// response the app prints. The stages climb from a pure-Rack lambda up to a
// data-backed ActiveRecord route, so the suite is a truthful, self-updating map
// of what runs green versus which binding is still missing:
//
//	Stage 1  Pure Rack lambda                       — runs
//	Stage 2  Rack + ERB view                        — runs
//	Stage 3  Sinatra DSL (require "sinatra/base")   — gap: no sinatra binding
//	Stage 4  sqlite3 + ERB/JSON data route          — runs
//	Stage 4b ActiveRecord ORM data route            — gap until go-ruby-activerecord binds
//
// The Sinatra and ActiveRecord stages feature-detect the require first: they
// assert the real response when the binding is present and otherwise record the
// exact missing feature, so the suite stays green today and lights up
// automatically when the binding lands.
package webapp
