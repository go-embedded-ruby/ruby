// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestURIBinding exercises the URI module backed by the go-ruby-uri library
// (internal/vm/uri.go): every component getter (including the nil branches), the
// validated setters, to_s / inspect / ==, the reference-resolution family, the
// module functions (parse / join / split / encode_www_form / decode_www_form /
// escape / unescape), URI::Generic.build, the scheme registry and the
// DEFAULT_PARSER facade. Each expectation is pinned against MRI 4.0.5.
func TestURIBinding(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// --- component getters, present and absent (nil) ---------------------
		{"scheme", `require "uri"; p URI.parse("http://a:b@h:9/p?q#f").scheme`, "\"http\"\n"},
		{"userinfo", `require "uri"; p URI.parse("http://a:b@h:9/p?q#f").userinfo`, "\"a:b\"\n"},
		{"userinfo_nil", `require "uri"; p URI.parse("http://h/p").userinfo`, "nil\n"},
		{"host", `require "uri"; p URI.parse("http://h/p").host`, "\"h\"\n"},
		{"host_nil", `require "uri"; p URI.parse("/p").host`, "nil\n"},
		{"port_explicit", `require "uri"; p URI.parse("http://h:9/p").port`, "9\n"},
		{"port_default", `require "uri"; p URI.parse("http://h/p").port`, "80\n"},
		{"port_nil", `require "uri"; p URI.parse("//h/p").port`, "nil\n"},
		{"path", `require "uri"; p URI.parse("http://h/a/b").path`, "\"/a/b\"\n"},
		{"query", `require "uri"; p URI.parse("http://h/?q=1").query`, "\"q=1\"\n"},
		{"query_nil", `require "uri"; p URI.parse("http://h/").query`, "nil\n"},
		{"query_empty", `require "uri"; p URI.parse("http://h/?").query`, "\"\"\n"},
		{"fragment", `require "uri"; p URI.parse("http://h/#f").fragment`, "\"f\"\n"},
		{"fragment_nil", `require "uri"; p URI.parse("http://h/").fragment`, "nil\n"},
		{"opaque", `require "uri"; p URI.parse("mailto:a@b.com").opaque`, "\"a@b.com\"\n"},
		{"opaque_nil", `require "uri"; p URI.parse("http://h/p").opaque`, "nil\n"},
		{"hostname", `require "uri"; p URI.parse("http://h/").hostname`, "\"h\"\n"},
		{"hostname_nil", `require "uri"; p URI.parse("/p").hostname`, "nil\n"},
		{"default_port", `require "uri"; p URI.parse("https://h/").default_port`, "443\n"},
		{"default_port_nil", `require "uri"; p URI.parse("//h/").default_port`, "nil\n"},

		// --- class / is_a? ---------------------------------------------------
		{"class_http", `require "uri"; p URI.parse("http://h/").class`, "URI::HTTP\n"},
		{"class_generic", `require "uri"; p URI.parse("foo://h/").class`, "URI::Generic\n"},
		{"class_noscheme", `require "uri"; p URI.parse("/p").class`, "URI::Generic\n"},
		{"is_a_uri", `require "uri"; p URI.parse("http://h/").is_a?(URI)`, "true\n"},
		{"is_a_generic", `require "uri"; p URI.parse("http://h/").is_a?(URI::Generic)`, "true\n"},

		// --- to_s / to_str / inspect / == ------------------------------------
		{"to_s", `require "uri"; p URI.parse("https://u:p@h:8080/x?q=1#f").to_s`, "\"https://u:p@h:8080/x?q=1#f\"\n"},
		{"to_s_default_port", `require "uri"; p URI.parse("http://h:80/x").to_s`, "\"http://h/x\"\n"},
		{"to_str", `require "uri"; p URI.parse("http://h/").to_str.class`, "String\n"},
		{"inspect", `require "uri"; p URI.parse("http://h/").inspect`, "\"#<URI::HTTP http://h/>\"\n"},
		{"eq_true", `require "uri"; p(URI.parse("http://h/") == URI.parse("http://h/"))`, "true\n"},
		{"eq_false_other", `require "uri"; p(URI.parse("http://h/") == URI.parse("http://x/"))`, "false\n"},
		{"eq_nonuri", `require "uri"; p(URI.parse("http://h/") == 5)`, "false\n"},
		{"eq_method", `require "uri"; p URI.parse("http://h/").send(:==, URI.parse("http://h/"))`, "true\n"},
		{"puts_to_s", `require "uri"; puts URI.parse("http://h/p")`, "http://h/p\n"},
		{"truthy", `require "uri"; p(URI.parse("http://h/") ? 1 : 0)`, "1\n"},

		// --- reference resolution: merge / + / route_to / normalize ----------
		{"merge_query", `require "uri"; p URI.parse("http://a.com/x").merge("?q=2").to_s`, "\"http://a.com/x?q=2\"\n"},
		{"merge_rel", `require "uri"; p URI.parse("http://a.com/foo/x").merge("bar").to_s`, "\"http://a.com/foo/bar\"\n"},
		{"plus_op", `require "uri"; p (URI.parse("http://a.com/foo/") + "bar").to_s`, "\"http://a.com/foo/bar\"\n"},
		{"merge_uri_arg", `require "uri"; p URI.parse("http://a.com/foo/").merge(URI.parse("bar")).to_s`, "\"http://a.com/foo/bar\"\n"},
		{"route_to", `require "uri"; p URI.parse("http://a.com/a/b").route_to("http://a.com/a/c").to_s`, "\"c\"\n"},
		{"normalize", `require "uri"; p URI.parse("HTTP://H.com").normalize.to_s`, "\"http://h.com/\"\n"},
		{"absolute", `require "uri"; p URI.parse("http://h/").absolute?`, "true\n"},
		{"absolute_false", `require "uri"; p URI.parse("/p").absolute?`, "false\n"},
		{"relative", `require "uri"; p URI.parse("/p").relative?`, "true\n"},

		// --- validated setters -----------------------------------------------
		{"set_scheme", `require "uri"; u = URI.parse("http://h/"); u.scheme = "https"; p u.to_s`, "\"https://h/\"\n"},
		{"set_host", `require "uri"; u = URI.parse("http://h/"); u.host = "x.com"; p u.host`, "\"x.com\"\n"},
		{"set_port", `require "uri"; u = URI.parse("http://h/"); u.port = "8080"; p u.to_s`, "\"http://h:8080/\"\n"},
		{"set_userinfo", `require "uri"; u = URI.parse("http://h/"); u.userinfo = "a:b"; p u.userinfo`, "\"a:b\"\n"},
		{"set_userinfo_nil", `require "uri"; u = URI.parse("http://a:b@h/"); u.userinfo = nil; p u.userinfo`, "nil\n"},
		{"set_returns_rhs", `require "uri"; u = URI.parse("http://h/"); p(u.host = "y.com")`, "\"y.com\"\n"},

		// --- module functions ------------------------------------------------
		{"parse", `require "uri"; p URI.parse("http://h/p").path`, "\"/p\"\n"},
		{"join", `require "uri"; p URI.join("http://a.com/foo/", "bar").to_s`, "\"http://a.com/foo/bar\"\n"},
		{"join_abs_path", `require "uri"; p URI.join("http://a.com/foo/x", "/bar").to_s`, "\"http://a.com/bar\"\n"},
		{"join_abs_ref", `require "uri"; p URI.join("http://a.com/", "http://b.com/").to_s`, "\"http://b.com/\"\n"},
		{"split", `require "uri"; p URI.split("http://h/p?q#f")`, "[\"http\", nil, \"h\", nil, nil, \"/p\", nil, \"q\", \"f\"]\n"},
		{"split_port", `require "uri"; p URI.split("http://h:80/p")[3]`, "\"80\"\n"},
		{"split_opaque", `require "uri"; p URI.split("mailto:a@b.com")[6]`, "\"a@b.com\"\n"},
		{"encode_www_form", `require "uri"; p URI.encode_www_form([["a","b c"],["x","y"]])`, "\"a=b+c&x=y\"\n"},
		{"decode_www_form", `require "uri"; p URI.decode_www_form("a=b+c&x=y")`, "[[\"a\", \"b c\"], [\"x\", \"y\"]]\n"},
		{"escape_default", `require "uri"; p URI.escape("a b/c")`, "\"a%20b/c\"\n"},
		{"escape_unsafe_str", `require "uri"; p URI.escape("a/b", "/")`, "\"a%2Fb\"\n"},
		{"escape_unsafe_re", `require "uri"; p URI.escape("a/b", /\//)`, "\"a%2Fb\"\n"},
		{"unescape", `require "uri"; p URI.unescape("a%20b")`, "\"a b\"\n"},

		// --- Kernel#URI ------------------------------------------------------
		{"kernel_uri", `require "uri"; p URI("http://x.com").host`, "\"x.com\"\n"},
		{"kernel_passthrough", `require "uri"; u = URI("http://x.com"); p URI(u).equal?(u)`, "true\n"},

		// --- build -----------------------------------------------------------
		{"build_hash_sym", `require "uri"; p URI::Generic.build(scheme: "http", host: "h.com", port: 8080, path: "/p", query: "q=1", fragment: "f").to_s`, "\"http://h.com:8080/p?q=1#f\"\n"},
		{"build_hash_str", `require "uri"; p URI::Generic.build("scheme" => "http", "host" => "h.com").to_s`, "\"http://h.com\"\n"},
		{"build_hash_port_str", `require "uri"; p URI::Generic.build("scheme" => "http", "host" => "h", "port" => "8080").to_s`, "\"http://h:8080\"\n"},
		{"build_subclass", `require "uri"; p URI::HTTP.build(host: "x.com").class`, "URI::HTTP\n"},
		{"build_array", `require "uri"; p URI::Generic.build([nil, nil, "h.com", 80, "/p", "q", "f"]).to_s`, "\"//h.com:80/p?q#f\"\n"},
		{"build_query_empty", `require "uri"; p URI::Generic.build(scheme: "http", host: "h", query: "").to_s`, "\"http://h?\"\n"},
		{"build_port_str_digit", `require "uri"; p URI::Generic.build("scheme" => "http", "host" => "h", "port" => "9").to_s`, "\"http://h:9\"\n"},
		{"build_port_str_empty", `require "uri"; p URI::Generic.build("scheme" => "http", "host" => "h", "port" => "").to_s`, "\"http://h\"\n"},
		{"build_port_str_nondigit", `require "uri"; p URI::Generic.build("scheme" => "http", "host" => "h", "port" => "8x").to_s`, "\"http://h\"\n"},
		{"build_port_other", `require "uri"; p URI::Generic.build("scheme" => "http", "host" => "h", "port" => 1.5).to_s`, "\"http://h\"\n"},

		// --- argument coercion through #to_s (uriStrOf / uriUnsafe defaults) ---
		{"parse_to_s_arg", `require "uri"; o = Object.new; def o.to_s; "http://h/p"; end; p URI.parse(o).host`, "\"h\"\n"},
		{"make_regexp_two_schemes", `require "uri"; re = URI::DEFAULT_PARSER.make_regexp(["ftp", "http"]); p(re =~ "http://h/" ? true : false)`, "true\n"},

		// --- YAML port-nil branch (no default, no explicit) ------------------
		{"to_yaml_port_nil", `require "uri"; require "yaml"; p URI.parse("//h/p").to_yaml.include?("port:")`, "true\n"},
		{"to_yaml_shared", `require "uri"; require "yaml"; u = URI.parse("http://h/p"); p [u, u].to_yaml.scan("URI::HTTP").length`, "1\n"},

		// --- scheme registry -------------------------------------------------
		{"scheme_list_http", `require "uri"; p URI.scheme_list["HTTP"]`, "URI::HTTP\n"},
		{"scheme_list_missing", `require "uri"; p URI.scheme_list["NOPE"]`, "nil\n"},
		{"default_ports_http", `require "uri"; p URI::DEFAULT_PORTS["http"]`, "80\n"},

		// --- DEFAULT_PARSER facade -------------------------------------------
		{"parser_parse", `require "uri"; p URI::DEFAULT_PARSER.parse("http://h/p").host`, "\"h\"\n"},
		{"parser_split", `require "uri"; p URI::DEFAULT_PARSER.split("http://h/p")[0]`, "\"http\"\n"},
		{"parser_escape", `require "uri"; p URI::DEFAULT_PARSER.escape("a b")`, "\"a%20b\"\n"},
		{"parser_escape_unsafe", `require "uri"; p URI::DEFAULT_PARSER.escape("a b", " ")`, "\"a%20b\"\n"},
		{"parser_unescape", `require "uri"; p URI::DEFAULT_PARSER.unescape("a%20b")`, "\"a b\"\n"},
		{"parser_make_regexp", `require "uri"; p(URI::DEFAULT_PARSER.make_regexp =~ "see http://h/x here")`, "4\n"},
		{"parser_make_regexp_schemes", `require "uri"; re = URI::DEFAULT_PARSER.make_regexp(["ftp"]); p(re =~ "http://h/")`, "nil\n"},
		{"parser_make_regexp_schemes_match", `require "uri"; re = URI::DEFAULT_PARSER.make_regexp(["ftp"]); p(re =~ "ftp://h/" ? true : false)`, "true\n"},
		{"rfc3986_parser_same", `require "uri"; p(URI::RFC3986_PARSER.equal?(URI::DEFAULT_PARSER))`, "true\n"},

		// --- YAML round-trip (covers convURI / uriUserPassword) --------------
		{"to_yaml_class", `require "uri"; require "yaml"; p URI.parse("http://u:pw@h:8/p?q#f").to_yaml.include?("URI::HTTP")`, "true\n"},
		{"to_yaml_user_only", `require "uri"; require "yaml"; p URI.parse("http://u@h/p").to_yaml.include?("user: u")`, "true\n"},
		{"require_uri", `p require("uri")`, "true\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := eval(t, tt.src)
			if out != tt.want {
				t.Fatalf("src=%q\n got %q\nwant %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestURIErrors covers the URI exception branches: a parse failure
// (InvalidURIError), a setter rejecting a malformed component
// (InvalidComponentError) and resolving a reference against a relative base
// (BadURIError), plus the argument-validation raises in the module functions and
// build. Each asserts the Ruby class and a fragment of the message.
func TestURIErrors(t *testing.T) {
	tests := []struct{ name, src, class, msgPart string }{
		{"parse_invalid", `require "uri"; URI.parse("http://h:notaport/x")`, "URI::InvalidURIError", "is not URI"},
		{"set_scheme_bad", `require "uri"; URI.parse("http://h/").scheme = "1bad"`, "URI::InvalidComponentError", "bad component"},
		{"set_host_bad", `require "uri"; URI.parse("http://h/").host = "a b"`, "URI::InvalidComponentError", "bad component"},
		{"set_port_bad", `require "uri"; URI.parse("http://h/").port = "nope"`, "URI::InvalidComponentError", "bad component"},
		{"set_userinfo_bad", `require "uri"; URI.parse("http://h/").userinfo = "a/b"`, "URI::InvalidComponentError", "bad component"},
		{"route_to_relative", `require "uri"; URI.parse("/rel").route_to("http://h/x")`, "URI::BadURIError", "relative"},
		{"merge_both_relative", `require "uri"; URI.parse("/rel").merge("/x")`, "URI::BadURIError", "relative"},
		{"join_zero_args", `require "uri"; URI.join`, "ArgumentError", "given 0"},
		{"build_array_arity", `require "uri"; URI::Generic.build([1, 2])`, "ArgumentError", "expected Array"},
		{"build_bad_type", `require "uri"; URI::Generic.build(5)`, "ArgumentError", "expected Array"},
		{"encode_www_form_nonarray", `require "uri"; URI.encode_www_form(5)`, "TypeError", "into Array"},
		{"encode_www_form_badpair", `require "uri"; URI.encode_www_form([[1]])`, "TypeError", "expected array"},
		{"escape_unsafe_nonstr", `require "uri"; URI.escape("a", :sym)`, "TypeError", "into String"},
		{"invalid_uri_error_qualified", `require "uri"; raise URI::InvalidURIError, "bad"`, "URI::InvalidURIError", "bad"},
		{"bad_uri_error_qualified", `require "uri"; raise URI::BadURIError, "boom"`, "URI::BadURIError", "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, msg := evalErr(t, tt.src)
			if class != tt.class {
				t.Fatalf("src=%q: got class %q, want %q", tt.src, class, tt.class)
			}
			if !strings.Contains(msg, tt.msgPart) {
				t.Fatalf("src=%q: msg %q missing %q", tt.src, msg, tt.msgPart)
			}
		})
	}
}
