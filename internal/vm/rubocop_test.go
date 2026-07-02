// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"testing"
)

// TestRuboCopConstants covers the RuboCop module, its Runner / Config classes,
// the Cop::Offense[::Location] value objects and the Error class (require "rubocop").
func TestRuboCopConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rubocop"; p RuboCop.is_a?(Module)`, "true\n"},
		{`p require "rubocop"`, "true\n"},
		{`require "rubocop"; p require "rubocop"`, "false\n"},
		{`require "rubocop"; p RuboCop::Error < StandardError`, "true\n"},
		{`require "rubocop"; p RuboCop::Runner.new.class`, "RuboCop::Runner\n"},
		{`require "rubocop"; p RuboCop::Config.new.class`, "RuboCop::Config\n"},
		{`require "rubocop"; p RuboCop::Cop::Offense.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRuboCopInspect covers Runner#inspect: the returned Offense value objects
// and their cop_name / message / severity / line / column / location / correctable?.
func TestRuboCopInspect(t *testing.T) {
	cases := []struct{ src, want string }{
		// Trailing whitespace is a stable core-cop offense.
		{`require "rubocop"
offs = RuboCop::Runner.new.inspect("x = 1 \n", "t.rb")
o = offs.find { |x| x.cop_name == "Layout/TrailingWhitespace" }
p o.cop_name`, "\"Layout/TrailingWhitespace\"\n"},
		{`require "rubocop"
offs = RuboCop::Runner.new.inspect("x = 1 \n")
o = offs.find { |x| x.cop_name == "Layout/TrailingWhitespace" }
p o.message`, "\"Trailing whitespace detected.\"\n"},
		{`require "rubocop"
offs = RuboCop::Runner.new.inspect("x = 1 \n")
o = offs.find { |x| x.cop_name == "Layout/TrailingWhitespace" }
p o.severity
p o.line
p o.column
p o.location.line
p o.location.column
p o.location.length
p o.correctable?
p o.corrected?`, ":convention\n1\n6\n1\n6\n1\ntrue\nfalse\n"},
		// The default path label ("(string)") is used when no path is given.
		{`require "rubocop"
offs = RuboCop::Runner.new.inspect("x = 1 \n")
p offs.length > 0`, "true\n"},
		// to_s / inspect on an offense render without error.
		{`require "rubocop"
o = RuboCop::Runner.new.inspect("x = 1 \n").first
p o.to_s.is_a?(String)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRuboCopAutocorrect covers Runner#autocorrect returning a corrected source.
func TestRuboCopAutocorrect(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rubocop"; p RuboCop::Runner.new.autocorrect("x = 1 \n").include?("x = 1\n")`, "true\n"},
		{`require "rubocop"; p RuboCop::Runner.new.autocorrect("x = 1 \n", "t.rb").is_a?(String)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRuboCopConfig covers RuboCop::Config.new / .parse and its use by a Runner,
// including the parse-error arm (RuboCop::Error) and the no-arg arm.
func TestRuboCopConfig(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rubocop"; p RuboCop::Config.parse("AllCops:\n  DisabledByDefault: true\n").class`, "RuboCop::Config\n"},
		{`require "rubocop"; cfg = RuboCop::Config.new; p RuboCop::Runner.new(cfg).class`, "RuboCop::Runner\n"},
		// A Runner built with a non-Config argument falls back to the empty config.
		{`require "rubocop"; p RuboCop::Runner.new("nope").class`, "RuboCop::Runner\n"},
		// A malformed .rubocop.yml raises RuboCop::Error.
		{`require "rubocop"; begin; RuboCop::Config.parse("\tbad: [unterminated"); rescue RuboCop::Error; p :cfgerr; end`, ":cfgerr\n"},
		// Config.parse with no argument raises ArgumentError.
		{`require "rubocop"; begin; RuboCop::Config.parse; rescue ArgumentError; p :argerr; end`, ":argerr\n"},
		// inspect / autocorrect with no argument raise ArgumentError.
		{`require "rubocop"; begin; RuboCop::Runner.new.inspect; rescue ArgumentError; p :argerr; end`, ":argerr\n"},
		{`require "rubocop"; begin; RuboCop::Runner.new.autocorrect; rescue ArgumentError; p :argerr; end`, ":argerr\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
