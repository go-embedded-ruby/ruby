// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	erubi "github.com/go-ruby-erubi/erubi"
)

// TestErubiInvalidIndicator covers the otherwise-unreachable InvalidIndicatorError
// path of buildErubiEngine. go-ruby-erubi only panics with that error when a
// caller-supplied scanning Regexp yields an unknown tag indicator — which this
// binding never passes — so the panic is injected through the ctor seam; the
// binding must translate it to a Ruby ArgumentError carrying the error's message.
func TestErubiInvalidIndicator(t *testing.T) {
	saved := erubiEngineCtor
	defer func() { erubiEngineCtor = saved }()
	erubiEngineCtor = func(string, erubi.Options) erubiEngineLike {
		panic(&erubi.InvalidIndicatorError{Indicator: "@"})
	}

	got := runFS(t, `require "erubi"; begin; Erubi::Engine.new("x"); rescue => e; p [e.class, e.message]; end`)
	if want := "[ArgumentError, \"erubi: invalid indicator: @\"]\n"; got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}
