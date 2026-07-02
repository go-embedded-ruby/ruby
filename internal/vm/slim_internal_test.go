// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"testing"

	slim "github.com/go-ruby-slim/slim"
)

// TestSlimCompileErrorSeam covers the __compile bridge's error arm: a compile
// failure raises Slim::Error. The go-ruby-slim library never fails on a
// well-formed template, so the arm is exercised by swapping the slimCompile seam.
func TestSlimCompileErrorSeam(t *testing.T) {
	orig := slimCompile
	defer func() { slimCompile = orig }()
	slimCompile = func(string, slim.Options) (string, error) {
		return "", errors.New("injected slim compile failure")
	}
	got := eval(t, `require "slim"
begin
  Slim::Template.new("p x")
  puts "no-raise"
rescue Slim::Error => e
  puts "err:#{e.message}"
end`)
	if got != "err:injected slim compile failure\n" {
		t.Fatalf("slim compile-error seam got=%q", got)
	}
}
