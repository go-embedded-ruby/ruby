package compiler

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

func TestMagicSourceEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		{"# encoding: binary\nx = 1\n", "ASCII-8BIT"},
		{"# encoding: BINARY\n", "ASCII-8BIT"},
		{"# coding: ascii-8bit\n", "ASCII-8BIT"},
		{"# -*- coding: binary -*-\n", "ASCII-8BIT"},
		{"# encoding: us-ascii\n", "US-ASCII"},
		{"# encoding: ascii\n", "US-ASCII"},
		{"# encoding: utf-8\n", ""},                                        // UTF-8 is the default: no tag
		{"# encoding: euc-jp\n", ""},                                       // unrecognised for tagging: no tag
		{"x = 1\n", ""},                                                    // no magic comment
		{"puts 1 # encoding: binary\n", ""},                                // must be a comment line, not trailing
		{"# just a plain comment\n", ""},                                   // a comment without a coding: field
		{"# coding:binary-*-\n", "ASCII-8BIT"},                             // value terminated by -*- with no space
		{"# coding: binary;\n", "ASCII-8BIT"},                              // value terminated by a semicolon
		{"#!/usr/bin/env ruby\n# encoding: binary\nx = 1\n", "ASCII-8BIT"}, // 2nd line after shebang
		{"", ""},
	}
	for _, tc := range cases {
		if got := MagicSourceEncoding(tc.src); got != tc.want {
			t.Errorf("MagicSourceEncoding(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

// firstStringConst returns the encoding name of the first String constant in a
// program compiled with the given source encoding.
func firstStringConst(t *testing.T, src, srcEnc string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := CompileWithEncoding(prog, srcEnc)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, c := range iseq.Consts {
		if s, ok := c.(*object.String); ok {
			return s.EncName()
		}
	}
	t.Fatalf("no String constant in %q", src)
	return ""
}

func TestCompileWithEncodingTagsLiterals(t *testing.T) {
	// A plain ASCII literal in a binary source is tagged BINARY.
	if got := firstStringConst(t, `x = "abc"`, "ASCII-8BIT"); got != "ASCII-8BIT" {
		t.Errorf(`"abc" in binary source: %q, want "ASCII-8BIT"`, got)
	}
	// A byte-oriented (\x, high bit) literal in a binary source is tagged BINARY.
	if got := firstStringConst(t, `x = "\xff"`, "ASCII-8BIT"); got != "ASCII-8BIT" {
		t.Errorf(`"\xff" in binary source: %q, want "ASCII-8BIT"`, got)
	}
	// A valid multi-byte UTF-8 literal (as a \u escape produces) stays UTF-8 even in
	// a binary source, matching MRI's rule that \u forces UTF-8.
	if got := firstStringConst(t, `x = "あ"`, "ASCII-8BIT"); got != "UTF-8" {
		t.Errorf(`"あ" in binary source: %q, want "UTF-8"`, got)
	}
	// The default (UTF-8) source leaves every literal untagged / UTF-8.
	if got := firstStringConst(t, `x = "abc"`, ""); got != "UTF-8" {
		t.Errorf(`"abc" in default source: %q, want "UTF-8"`, got)
	}
	// Compile (the UTF-8 default entry point) behaves the same as an empty srcEnc.
	prog, err := parser.Parse(`x = "abc"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Compile(prog); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}
