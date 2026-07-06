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
//	Stage 3  Sinatra DSL (require "sinatra/base")   — runs (go-ruby-sinatra binding)
//	Stage 4  sqlite3 + ERB/JSON data route          — runs
//	Stage 4b ActiveRecord ORM data route            — gap until go-ruby-activerecord binds
//
// On top of stage 3, TestSinatraGemOracle (sinatra_oracle_test.go) is the web
// phase's MRI-identity proof: it runs apps/sinatra_oracle.rb through rbgo and
// asserts the [status, headers, body] dump is byte-for-byte the response the
// real `sinatra` gem 4.2.1 produced from the same app and request envs.
//
// TestSinatraErbGemOracle (sinatra_erb_oracle_test.go) is the matching proof for
// the templating half: it runs apps/sinatra_erb_oracle.rb through rbgo and asserts
// the rendered responses are byte-for-byte the `sinatra` gem 4.2.1 output, across
// inline-String and :symbol/file templates, <%= %>/<% %>, @ivars set in a filter,
// positional and options[:locals] locals, and ERB trim behaviour — the `erb`
// helper rendering through the bound go-ruby-erb compiler.
//
// The ActiveRecord stage feature-detects the require first: it asserts the real
// response when the binding is present and otherwise records the exact missing
// feature, so the suite stays green today and lights up automatically when the
// binding lands.
package webapp
