// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// closedUnavailableFeatures is the roster of require feature names whose gem
// backends are compiled out of a closed-world build (`rbgo build --closed`, which
// builds with -tags rbgo_closed).
//
// There is one entry, and the reason is structural rather than incidental:
// RuboCop is a LINTER, so github.com/go-ruby-rubocop/rubocop imports
// github.com/go-ruby-parser/parser to have an AST to inspect. A closed-world
// binary exists precisely to NOT carry the front-end — its program is frozen
// bytecode and it announces "front-end dropped (no lexer/parser/compiler
// linked)". Linking a linter into it re-imports the parser through the back door
// and makes that announcement false, which is what cmd/rbgo's closed-build
// integration tests caught the moment RBGO_BUILD_IT was finally wired to a CI
// lane (issue #682): 172.9 MiB with the whole parser inside.
//
// Like wasmUnavailableFeatures in backends_unavailable.go, this is declared in a
// shared build-tag-free file so a native test can read the roster and assert the
// LoadError directly, without a closed-world build.
var closedUnavailableFeatures = []string{"rubocop"}

// raiseClosedUnavailable raises the LoadError a closed-world build produces when a
// program requires one of the gems compiled out of it.
//
// It used to be a second copy of raiseWasmUnavailable's body, kept the same SHAPE
// by TestClosedAndWasmUnavailableAgreeOnShape, with a note that a third condition
// should collapse all three. The rbgo_core build was that third condition: all
// three now call raiseBuildUnavailable (core.go) and differ only in the build they
// name. The shape test stays, as the proof the collapse changed no message.
func (vm *VM) raiseClosedUnavailable(feature string) object.Value {
	return vm.raiseBuildUnavailable(feature, "the closed-world build")
}
