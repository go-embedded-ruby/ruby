// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestYAMLConstants covers the YAML/Psych loadable shell's constant and error
// tree (require "yaml"). YAML is an alias of Psych, matching MRI.
func TestYAMLConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		// YAML is the very same module object as Psych.
		{`require "yaml"; p YAML.equal?(Psych)`, "true\n"},
		{`require "yaml"; p Psych::VERSION`, "\"5.0.0\"\n"},
		// Psych::Nodes is a module.
		{`require "yaml"; p Psych::Nodes.is_a?(Module)`, "true\n"},
		// Error tree: SyntaxError and DisallowedClass descend from Psych::Exception,
		// which descends from StandardError.
		{`require "yaml"; p Psych::Exception < StandardError`, "true\n"},
		{`require "yaml"; p Psych::SyntaxError < Psych::Exception`, "true\n"},
		{`require "yaml"; p Psych::DisallowedClass < Psych::Exception`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLNotImplemented covers every Psych/YAML method that raises
// NotImplementedError until a real pure-Go YAML engine lands.
func TestYAMLNotImplemented(t *testing.T) {
	// dump/load/safe_load/parse/parse_stream/dump_tags take args; load_file takes
	// a path. Each must raise NotImplementedError naming the method.
	calls := map[string]string{
		"dump":         `YAML.dump(1)`,
		"load":         `YAML.load("a")`,
		"safe_load":    `YAML.safe_load("a")`,
		"load_file":    `YAML.load_file("x.yml")`,
		"parse":        `YAML.parse("a")`,
		"parse_stream": `YAML.parse_stream("a")`,
		"dump_tags":    `YAML.dump_tags`,
	}
	for name, call := range calls {
		src := `require "yaml"; ` + call
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "NotImplementedError") {
			t.Errorf("%s: expected NotImplementedError, got %v", name, err)
		}
		if err != nil && !strings.Contains(err.Error(), name) {
			t.Errorf("%s: message should name the method, got %v", name, err)
		}
	}
}
