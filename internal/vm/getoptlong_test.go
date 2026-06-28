// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestGetoptLongConstants covers the GetoptLong loadable shell's argument-kind
// constants, its error tree, and #new (require "getoptlong"). The constant
// values match MRI Ruby 4.0.5.
func TestGetoptLongConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "getoptlong"; p GetoptLong::NO_ARGUMENT`, "0\n"},
		{`require "getoptlong"; p GetoptLong::REQUIRED_ARGUMENT`, "1\n"},
		{`require "getoptlong"; p GetoptLong::OPTIONAL_ARGUMENT`, "2\n"},
		// Error tree: Error < StandardError; InvalidOption and MissingArgument < Error.
		{`require "getoptlong"; p GetoptLong::Error < StandardError`, "true\n"},
		{`require "getoptlong"; p GetoptLong::InvalidOption < GetoptLong::Error`, "true\n"},
		{`require "getoptlong"; p GetoptLong::MissingArgument < GetoptLong::Error`, "true\n"},
		// #new builds a GetoptLong instance.
		{`require "getoptlong"; p GetoptLong.new.is_a?(GetoptLong)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestGetoptLongNotImplemented covers every scanning method that raises
// NotImplementedError until the option-parser round lands.
func TestGetoptLongNotImplemented(t *testing.T) {
	for _, m := range []string{"each", "get", "get_option", "set_options", "each_option"} {
		src := `require "getoptlong"; g = GetoptLong.new; g.` + m
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "NotImplementedError") {
			t.Errorf("GetoptLong#%s: expected NotImplementedError, got %v", m, err)
		}
		if err != nil && !strings.Contains(err.Error(), m) {
			t.Errorf("GetoptLong#%s: message should name the method, got %v", m, err)
		}
	}
}
