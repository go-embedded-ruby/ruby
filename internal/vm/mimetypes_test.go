// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestMIMETypesConstants covers the MIME module, its Types registry facade and
// the Type value class (require "mime/types").
func TestMIMETypesConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "mime/types"; p MIME.is_a?(Module)`, "true\n"},
		{`require "mime/types"; p MIME::Types.is_a?(Module)`, "true\n"},
		{`p require "mime/types"`, "true\n"},
		{`require "mime/types"; p require "mime/types"`, "false\n"},
		{`require "mime/types"; p MIME::Type.is_a?(Class)`, "true\n"},
		{`require "mime/types"; p MIME::Types.count > 1000`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMIMETypesLookup covers MIME::Types[], .type_for / .of and the Type readers.
func TestMIMETypesLookup(t *testing.T) {
	pre := `require "mime/types"; t = MIME::Types["text/html"].first; `
	cases := []struct{ src, want string }{
		{`require "mime/types"; p MIME::Types["text/html"].class.name`, "\"Array\"\n"},
		{pre + `p t.content_type`, "\"text/html\"\n"},
		{pre + `p t.to_s`, "\"text/html\"\n"},
		{pre + `p t.media_type`, "\"text\"\n"},
		{pre + `p t.sub_type`, "\"html\"\n"},
		{pre + `p t.simplified`, "\"text/html\"\n"},
		{pre + `p t.extensions.include?("html")`, "true\n"},
		{pre + `p t.preferred_extension`, "\"html\"\n"},
		{pre + `p t.registered?`, "true\n"},
		{pre + `p t.class.name`, "\"MIME::Type\"\n"},
		{pre + `p t.inspect.start_with?("#<MIME::Type")`, "true\n"},
		// type_for / of look up by filename extension.
		{`require "mime/types"; p MIME::Types.type_for("index.html").first.content_type`, "\"text/html\"\n"},
		{`require "mime/types"; p MIME::Types.of("index.html").first.content_type`, "\"text/html\"\n"},
		// An unknown content-type yields an empty Array.
		{`require "mime/types"; p MIME::Types["application/x-nonexistent-xyzzy"]`, "[]\n"},
		// An extension with no registered type yields an empty Array.
		{`require "mime/types"; p MIME::Types.type_for("x.nonexistent-ext-xyzzy")`, "[]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMIMETypePredicates covers the remaining Type predicate/reader readers over
// a real registry entry, asserting types rather than exact registry values.
func TestMIMETypePredicates(t *testing.T) {
	pre := `require "mime/types"; t = MIME::Types["text/html"].first; `
	cases := []struct{ src, want string }{
		{pre + `p [true, false].include?(t.binary?)`, "true\n"},
		{pre + `p [true, false].include?(t.ascii?)`, "true\n"},
		{pre + `p [true, false].include?(t.obsolete?)`, "true\n"},
		{pre + `p [true, false].include?(t.complete?)`, "true\n"},
		{pre + `p [true, false].include?(t.signature?)`, "true\n"},
		{pre + `p [true, false].include?(t.provisional?)`, "true\n"},
		{pre + `p t.friendly.is_a?(String)`, "true\n"},
		{pre + `p t.encoding.is_a?(String)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMIMETypePreferredExtensionNil covers the nil arm of preferred_extension for
// a registry entry that registers no extension.
func TestMIMETypePreferredExtensionNil(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "mime/types"; p MIME::Types["application/1d-interleaved-parityfec"].first.preferred_extension`, "nil\n"},
		{`require "mime/types"; p MIME::Types["application/1d-interleaved-parityfec"].first.extensions`, "[]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMIMETypesErrors covers the arity paths.
func TestMIMETypesErrors(t *testing.T) {
	for _, call := range []string{
		`MIME::Types[]`,
		`MIME::Types.type_for`,
		`MIME::Types.of`,
	} {
		src := `require "mime/types"
begin
  ` + call + `
rescue ArgumentError
  puts "arity"
end`
		if got := eval(t, src); !strings.Contains(got, "arity") {
			t.Errorf("%s no-arg: got %q", call, got)
		}
	}
}
