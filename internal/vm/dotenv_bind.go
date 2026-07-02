// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	dotenv "github.com/go-ruby-dotenv/dotenv"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-dotenv/dotenv parser. The parser and
// its `$VAR` / `$(cmd)` resolution live in that library; rbgo only supplies the
// three host seams the library's Env abstracts — ENV read, ENV write, and shell
// command execution — and maps the parsed OrderedMap back to a Ruby Hash.

// dotenvEnv builds the library Env wiring rbgo's ENV seams: Lookup reads through
// envLookup (rbgo's ENV read seam), Set writes through envSetenv (the ENV write
// seam), and RunCommand runs `$(cmd)` through the VM's shell backtick, chomping a
// single trailing newline as the gem does.
func dotenvEnv(vm *VM) *dotenv.Env {
	return &dotenv.Env{
		Lookup: envLookup,
		Set:    func(key, val string) { _ = envSetenv(key, val) },
		RunCommand: func(script string) string {
			return strings.TrimSuffix(vm.runShellCommand(script), "\n")
		},
	}
}

// dotenvParse parses a source String to a Ruby Hash without mutating ENV. A
// malformed line raises a Ruby ArgumentError carrying the library's FormatError
// message (dotenv raises a Dotenv::FormatError, an ArgumentError-family error).
func dotenvParse(vm *VM, src string, overwrite bool) object.Value {
	m, err := dotenv.ParseString(src, overwrite, dotenvEnv(vm))
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	return fromDotenvMap(m)
}

// dotenvLoad parses a source String, sets each pair into ENV via the library's
// Load (honouring the overwrite rule), and returns the parsed Hash.
func dotenvLoad(vm *VM, src string, overwrite bool) object.Value {
	m, _, err := dotenv.Load(src, overwrite, dotenvEnv(vm))
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	return fromDotenvMap(m)
}

// fromDotenvMap maps a library ordered *OrderedMap to a Ruby Hash with String
// keys and String values, preserving insertion order.
func fromDotenvMap(m *dotenv.OrderedMap) object.Value {
	h := object.NewHash()
	m.Each(func(key, val string) {
		h.Set(object.NewString(key), object.NewString(val))
	})
	return h
}
