// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	haml "github.com/go-ruby-haml/haml"
)

// TestHamlCompileErrorSeam covers the __compile bridge's error arm: a compile
// failure raises Haml::SyntaxError. A well-formed template compiles, so the arm is
// exercised by swapping the hamlCompile seam to return a *haml.SyntaxError.
func TestHamlCompileErrorSeam(t *testing.T) {
	orig := hamlCompile
	defer func() { hamlCompile = orig }()
	hamlCompile = func(string, haml.Options) (string, error) {
		return "", &haml.SyntaxError{Line: "1", Msg: "injected haml failure"}
	}
	got := eval(t, `require "haml"
begin
  Haml::Template.new("%p x")
  puts "no-raise"
rescue Haml::SyntaxError => e
  puts "err"
end`)
	if got != "err\n" {
		t.Fatalf("haml compile-error seam got=%q", got)
	}
}
