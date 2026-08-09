// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !(js && wasm)

// This file is the native half of the server-gem seam. registerServerGems runs
// the register* calls for the gems that are useless in a browser WebAssembly
// build: server/RPC/telemetry (Protobuf, GraphQL, OpenTelemetry), config
// management and deploy (Puppet + ResourceAPI, Hiera, SemanticPuppet, Augeas,
// Capistrano, Bundler), server/site frameworks and mail (Railties, Rails,
// Hanami, ActiveJob, Mail, ActionMailer, Jekyll), the Redis client, and
// acceptance/unit testing (Capybara, Minitest + Spec, Faker). On the native
// build every one is registered exactly as before; the calls were only hoisted
// out of bootstrap() into this seam so the GOOS=js GOARCH=wasm half
// (servergems_wasm.go) can compile them out — their heavy Go backends
// (google.golang.org/protobuf, graphql-go, the OpenTelemetry SDK, go-puppet, …)
// then drop from the browser binary via dead-code elimination.
//
// It runs LAST in bootstrap (after every other gem is registered), so each gem
// here still sees the classes it layers on (Rack/Sinatra for Capybara/Hanami,
// Mail for ActionMailer, the exception tree, …); the relative order within the
// block is preserved from bootstrap(). Sidekiq and Resque are NOT here — their
// go-redis backends are already guarded by the network-backend seam
// (backends_wasm.go / wasmUnavailableFeatures).

package vm

func (vm *VM) registerServerGems() {
	vm.registerProtobuf()          // Google::Protobuf (require "google/protobuf"), backed by go-ruby-protobuf; needs TypeError/RuntimeError
	vm.registerGraphQL()           // GraphQL module (require "graphql"), backed by go-ruby-graphql; needs StandardError (GraphQL::Error)
	vm.registerOpenTelemetry()     // OpenTelemetry module (require "opentelemetry"), backed by go-ruby-opentelemetry; needs Object (the SDK/API class tree)
	vm.registerMail()              // Mail module (go-ruby-mail backend)
	vm.registerActionMailer()      // ActionMailer::Base subclass DSL (default/delivery_method/register_interceptor/register_observer) + MessageDelivery proxy (deliver_now/deliver_later/message) + mail/attachments/headers (require "action_mailer"), backed by go-ruby-actionmailer; the mailer-action body, RenderBody (Action View), delivery method and EnqueueJob (Active Job) are Ruby-dispatch seams run INLINE under the GVL; needs the Mail message surface, so registered after registerMail
	vm.registerFaker()             // Faker module (go-ruby-faker backend); needs Random for the seed contract
	vm.registerRedis()             // Redis client (require "redis"), backed by go-ruby-redis RESP codec; socket = injected IO seam; needs StandardError for Redis::BaseError
	vm.registerHiera()             // Hiera.new(config:, scope:) + #lookup (require "hiera"), backed by go-ruby-hiera over go-hiera; each instance binds one hiera.yaml + scope (Hiera 5 style)
	vm.registerPuppet()            // Puppet.parse + Puppet.compile -> Puppet::Resource::Catalog (require "puppet"), backed by go-ruby-puppet over go-puppet (types via go-pcore, data via go-hiera, facts via provider); needs StandardError for Puppet::Error/ParseError
	vm.registerPuppetResourceAPI() // Puppet::ResourceApi.register_type + type validation + provider protocol (require "puppet/resource_api"), backed by go-ruby-puppet-resource-api; the provider get/set hooks run INLINE under the GVL; needs registerPuppet first (Puppet module) + StandardError for DevError/ResourceError
	vm.registerSemanticPuppet()    // SemanticPuppet::Version/VersionRange (require "semantic_puppet"), backed by go-ruby-semantic-puppet; stateless, immutable value types; needs ArgumentError for ValidationFailure/InvalidRangeFormat
	vm.registerAugeas()            // Augeas config-tree editing (require "augeas"), backed by go-ruby-augeas over go-augeas; per-VM tree; needs StandardError for Augeas::Error
	vm.registerCapybara()          // Capybara::Session rack_test acceptance-test driver + Capybara.app= + Capybara::DSL (require "capybara" / "capybara/dsl"), backed by go-ruby-capybara; the App seam sends #call(env) to the Ruby Rack app and converts the [status, headers, body] triple back, run inline under the GVL; the HTML parse / selector engine / form + redirect handling is the library; needs Rack + StandardError (Capybara::CapybaraError), so registered after registerRack/registerSinatra
	vm.registerActiveJob()         // ActiveJob::Base subclass DSL (queue_as/retry_on/callbacks/perform_later/perform_now/set) + Arguments (require "active_job"), backed by go-ruby-activejob; #perform is a Ruby seam run INLINE under the GVL (default inline adapter); needs ArgumentError for SerializationError
	vm.registerRailties()          // Rails::Railtie/Engine/Application boot framework (require "rails"), backed by go-ruby-railties; the initializer/hook/routes blocks are the rbgo seams (run inline on initialize!); a later rails meta-gem layers the top-level Rails.* accessors
	vm.registerRails()             // the `rails` meta-gem (require "rails/all"), backed by go-ruby-rails: extends railties' Rails module with the top-level singleton accessors (Rails.application/env/root/…), Rails::VERSION, Rails::EnvironmentInquirer and the component catalog; MUST run after registerRailties (extends the Rails module it created)
	vm.registerHanami()            // Hanami::Router (hanami-router) + Hanami::Action (hanami-controller) (require "hanami" / "hanami/router" / "hanami/action"), backed by go-ruby-hanami over go-ruby-rack; the endpoint Resolver, the action #handle body (ActionCall), before/after callbacks, handle_exception, params validation and session loading are the rbgo seams; needs Rack, so registered after registerRack
	vm.registerRake()              // Rake task-graph core + top-level task/file/namespace/desc DSL (require "rake"), backed by go-ruby-rake; a task's action block is the rbgo seam, run INLINE on the VM goroutine under the GVL when the task is invoked (the depth-first prerequisite-first invoke order, once-guard, circular detection, FileTask up-to-date logic and namespace/scope resolution are the library); the FileTask mtime + FileList glob seams are wired to the real filesystem
	vm.registerCapistrano()        // Capistrano deploy DSL core (require "capistrano"): set/fetch/role/server/task/namespace/before/after/invoke + on(hosts){execute/capture/test}, backed by go-ruby-capistrano; the task/hook block bodies and on-blocks run INLINE under the GVL as the rbgo seams, and the command backend is wired to the library's in-process FakeBackend so execute/capture are recorded and never reach a real host (hermetic). The DSL installs lazily on require (a feature hook) so it never clobbers rake's always-on top-level task/namespace/desc; registered after registerRake. Needs StandardError for the Capistrano::Error tree
	vm.registerBundler()           // Bundler: Gemfile/Gemfile.lock codec + resolver (require "bundler"), backed by go-ruby-bundler; needs StandardError for Bundler::BundlerError
	vm.registerMinitest()          // Minitest::Assertions + Test lifecycle (require "minitest"), backed by go-ruby-minitest; needs StandardError for Minitest::Assertion
	vm.registerMinitestSpec()      // Minitest::Spec + spec DSL (describe/it/before/after/let), the must_*/wont_* expectations, and the autorun runner/reporter (require "minitest/autorun" / "minitest/spec"); layers on registerMinitest's Test + assertions
	vm.registerJekyll()            // Jekyll.configuration / build + Jekyll::Site (require "jekyll"), backed by go-ruby-jekyll (static-site build/render over go-liquid); needs StandardError for Jekyll::Error
}
