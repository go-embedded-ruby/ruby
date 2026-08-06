// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

// serverGemFeatures is the roster of require feature names for the server / ops /
// testing gems that are compiled out of the GOOS=js GOARCH=wasm build (see
// servergems_native.go for the gems and why). Their register functions are only
// called on the native build, so on wasm the linker drops their Go backends
// (google.golang.org/protobuf, graphql-go, the OpenTelemetry SDK, go-puppet, the
// Rails/Hanami/Jekyll stacks, …); because every name below is still listed in
// require.go's providedFeatures, the wasm build (servergems_wasm.go) wires each
// to a LoadError-raising require hook so `require "puppet"` fails cleanly as a
// missing gem would.
//
// It is declared in a shared (build-tag-free) file so a native test can pin the
// exact set and assert every entry is a real provided feature, and so the
// wasm stub iterates it rather than duplicating the list. Grouped by gem for
// readability; hookWasmUnavailable treats each name independently.
var serverGemFeatures = []string{
	"google/protobuf", "protobuf",
	"graphql",
	"opentelemetry",
	"mail",
	"action_mailer", "actionmailer",
	"faker",
	"redis",
	"hiera",
	"puppet",
	"puppet/resource_api",
	"semantic_puppet",
	"augeas",
	"capybara", "capybara/dsl",
	"active_job", "activejob",
	"rails/railtie", "rails/engine", "rails/application",
	"rails", "rails/all",
	"hanami", "hanami/router", "hanami/action",
	"rake", "rake/dsl_definition", "rake/task", "rake/file_task", "rake/file_list",
	"capistrano", "capistrano/all", "capistrano/setup", "capistrano/deploy",
	"bundler",
	"minitest", "minitest/test", "minitest/unit",
	"minitest/autorun", "minitest/spec",
	"jekyll",
}
