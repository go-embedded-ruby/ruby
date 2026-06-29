// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"testing"

	erb "github.com/go-ruby-erb/erb"
)

// TestERBCompileError covers the otherwise-unreachable Compile-error branch of
// the native __compile hook. go-ruby-erb never fails on a well-formed template,
// so the failure is injected through the erbCompile seam; the binding must
// surface it as a Ruby ArgumentError carrying the error's message.
func TestERBCompileError(t *testing.T) {
	saved := erbCompile
	defer func() { erbCompile = saved }()
	erbCompile = func(string, erb.Options) (string, string, error) {
		return "", "", errors.New("boom")
	}

	got := runFS(t, `require "erb"; begin; ERB.new("x"); rescue => e; p [e.class, e.message]; end`)
	if want := "[ArgumentError, \"boom\"]\n"; got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}
