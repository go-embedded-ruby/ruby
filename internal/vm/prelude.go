package vm

import _ "embed"

// preludeSource is the embedded-Ruby standard library loaded at VM startup
// (Comparable, Enumerable, …). See prelude.rb.
//
// In an open build it is parsed and run at startup (prelude_open.go). In a
// closed-world build the front-end is gone, so the prelude is loaded from its
// frozen bytecode instead (embeddedPrelude, prelude_closed.go); the source then
// only feeds the freeze generator and the drift test that keeps the two in sync.
//
//go:generate go run ../../cmd/freeze-prelude
//
//go:embed prelude.rb
var preludeSource string
