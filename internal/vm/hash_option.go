// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// hashOption fetches key from a Ruby options Hash, accepting either a Symbol or a
// String key (option hashes are conventionally written with symbol keys, but a
// string key is tolerated). It is a build-tag-free helper shared by the ActiveJob
// binding (always compiled) and the Sidekiq binding (native only), so it must not
// live in a wasm-guarded file.
func hashOption(h *object.Hash, key string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}
