// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestDotenvConstants covers the Dotenv loadable module and its Parser
// (require "dotenv").
func TestDotenvConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "dotenv"; p Dotenv.is_a?(Module)`, "true\n"},
		{`require "dotenv"; p Dotenv::Parser.is_a?(Module)`, "true\n"},
		{`p require "dotenv"`, "true\n"},
		{`require "dotenv"; p require "dotenv"`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDotenvParse covers Dotenv.parse and Dotenv::Parser.call: plain pairs,
// quoting, `$VAR` interpolation against an earlier pair, and key order.
func TestDotenvParse(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "dotenv"; p Dotenv.parse("A=1\nB=2")`, "{\"A\" => \"1\", \"B\" => \"2\"}\n"},
		{`require "dotenv"; p Dotenv.parse("Q=\"quoted value\"")`, "{\"Q\" => \"quoted value\"}\n"},
		// $VAR interpolation of an earlier assignment.
		{`require "dotenv"; p Dotenv.parse("FOO=bar\nBAZ=${FOO}baz")`, "{\"FOO\" => \"bar\", \"BAZ\" => \"barbaz\"}\n"},
		// Parser.call is the same parse.
		{`require "dotenv"; p Dotenv::Parser.call("X=y")`, "{\"X\" => \"y\"}\n"},
		// A non-String source is coerced via to_s ("42", read as a bare key).
		{`require "dotenv"; p Dotenv.parse(42)`, "{\"42\" => \"\"}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDotenvLoad covers the Dotenv.load / Dotenv.overload success paths through
// the Ruby dispatch: load sets a fresh key into ENV and returns the parsed Hash,
// and overload replaces an existing key. Uniquely-named keys keep the real
// process ENV clean.
func TestDotenvLoad(t *testing.T) {
	// load returns the parsed Hash and sets a fresh ENV key.
	if got := eval(t, `require "dotenv"; h = Dotenv.load("RBGO_DOTENV_T1=v1"); [h["RBGO_DOTENV_T1"], ENV["RBGO_DOTENV_T1"]].each { |x| p x }`); got != "\"v1\"\n\"v1\"\n" {
		t.Errorf("load got=%q", got)
	}
	// overload replaces an existing ENV value.
	if got := eval(t, `require "dotenv"; ENV["RBGO_DOTENV_T2"] = "old"; Dotenv.overload("RBGO_DOTENV_T2=new"); p ENV["RBGO_DOTENV_T2"]`); got != "\"new\"\n" {
		t.Errorf("overload got=%q", got)
	}
	// load does NOT overwrite an existing key.
	if got := eval(t, `require "dotenv"; ENV["RBGO_DOTENV_T3"] = "keep"; Dotenv.load("RBGO_DOTENV_T3=ignored"); p ENV["RBGO_DOTENV_T3"]`); got != "\"keep\"\n" {
		t.Errorf("load no-overwrite got=%q", got)
	}
}

// TestDotenvParseError covers a malformed line raising a Ruby ArgumentError and
// the wrong-argument-count guards.
func TestDotenvParseError(t *testing.T) {
	// An `export KEY` naming an unset variable is a FormatError, surfaced as a
	// Ruby ArgumentError.
	if got := eval(t, `require "dotenv"; begin; Dotenv.parse("export NOPE"); rescue ArgumentError; puts "rescued"; end`); !strings.Contains(got, "rescued") {
		t.Errorf("format error got=%q", got)
	}
	for _, src := range []string{
		`require "dotenv"; Dotenv.parse`,
		`require "dotenv"; Dotenv.load`,
		`require "dotenv"; Dotenv.overload`,
	} {
		if got := eval(t, `begin; `+src+`; rescue ArgumentError => e; p e.class; end`); !strings.Contains(got, "ArgumentError") {
			t.Errorf("src=%q got=%q", src, got)
		}
	}
}
