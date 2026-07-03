// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"reflect"
	"testing"

	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestRackWrapperInspect covers ToS / Inspect / Truthy of the Rack value
// wrappers.
func TestRackWrapperInspect(t *testing.T) {
	checks := []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{
		&RackRequest{},
		&RackResponse{},
	}
	wantToS := []string{"#<Rack::Request>", "#<Rack::Response>"}
	for i, c := range checks {
		if c.ToS() != wantToS[i] || c.Inspect() != wantToS[i] || !c.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c, c.ToS(), c.Inspect(), c.Truthy())
		}
	}
}

// TestRackConstants covers the module, RELEASE and the require feature.
func TestRackConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "rack"`, "true\n"},
		{`require "rack"; p require "rack"`, "false\n"},
		{`require "rack"; p Rack.is_a?(Module)`, "true\n"},
		{`require "rack"; puts Rack::RELEASE`, rackRelease + "\n"},
		{`p require "rack/utils"`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRackRequestAccessors covers every string accessor, predicate, port,
// get_header (present/absent), has_header? and the parsed-params methods.
func TestRackRequestAccessors(t *testing.T) {
	pre := `require "rack"; r = Rack::Request.new(` +
		`"REQUEST_METHOD" => "POST", "PATH_INFO" => "/p", "SCRIPT_NAME" => "/s", ` +
		`"QUERY_STRING" => "a=1&b=2", "SERVER_NAME" => "h", "SERVER_PORT" => "80", ` +
		`"CONTENT_TYPE" => "text/html", "rack.url_scheme" => "http", "HTTP_HOST" => "h"); `
	cases := []struct{ expr, want string }{
		{"r.request_method", "POST"},
		{"r.path_info", "/p"},
		{"r.script_name", "/s"},
		{"r.query_string", "a=1&b=2"},
		{"r.server_name", "h"},
		{"r.server_port", "80"},
		{"r.content_type", "text/html"},
		{"r.media_type", "text/html"},
		{"r.scheme", "http"},
		{"r.host", "h"},
		{"r.path", "/s/p"},
		{"r.fullpath", "/s/p?a=1&b=2"},
		{"r.post?", "true"},
		{"r.get?", "false"},
		{"r.put?", "false"},
		{"r.patch?", "false"},
		{"r.delete?", "false"},
		{"r.head?", "false"},
		{"r.options?", "false"},
		{"r.xhr?", "false"},
		{"r.ssl?", "false"},
		{"r.port", "80"},
		{`r.get_header("REQUEST_METHOD")`, "POST"},
		{`r.get_header("NOPE").inspect`, "nil"},
		{`r.has_header?("PATH_INFO")`, "true"},
		{`r.has_header?("NOPE")`, "false"},
		{`r.params["a"]`, "1"},
		{`r.GET["b"]`, "2"},
		{`r.POST.length`, "0"},
		{`r.cookies.class`, "Hash"},
	}
	for _, c := range cases {
		src := pre + "puts (" + c.expr + ")"
		if got := eval(t, src); got != c.want+"\n" {
			t.Errorf("expr=%q got=%q want=%q", c.expr, got, c.want+"\n")
		}
	}
}

// TestRackRequestBaseURLAndIP covers base_url / url / ip, which read a fuller
// env, and Request.new's missing-argument error.
func TestRackRequestBaseURLAndIP(t *testing.T) {
	if got := eval(t, `require "rack"; r = Rack::Request.new("rack.url_scheme"=>"http","HTTP_HOST"=>"ex","PATH_INFO"=>"/x"); puts r.base_url; puts r.url`); got != "http://ex\nhttp://ex/x\n" {
		t.Errorf("base_url/url got=%q", got)
	}
	if got := eval(t, `require "rack"; puts Rack::Request.new("REMOTE_ADDR"=>"1.2.3.4").ip`); got != "1.2.3.4\n" {
		t.Errorf("ip got=%q", got)
	}
	class, _ := evalErr(t, `require "rack"; Rack::Request.new`)
	if class != "ArgumentError" {
		t.Errorf("new no-arg class=%q", class)
	}
	class, _ = evalErr(t, `require "rack"; Rack::Request.new("QUERY_STRING"=>"a=%ZZ","REQUEST_METHOD"=>"GET").GET`)
	if class != "ArgumentError" {
		t.Errorf("GET parse-error class=%q", class)
	}
	class, _ = evalErr(t, `require "rack"; Rack::Request.new("PATH_INFO"=>"/x").get_header`)
	if class != "ArgumentError" {
		t.Errorf("get_header no-arg class=%q", class)
	}
	class, _ = evalErr(t, `require "rack"; Rack::Request.new("PATH_INFO"=>"/x").has_header?`)
	if class != "ArgumentError" {
		t.Errorf("has_header? no-arg class=%q", class)
	}
}

// TestRackResponse covers the full response surface.
func TestRackResponse(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rack"; res = Rack::Response.new; res.write("hi"); puts res.finish[0]`, "200\n"},
		{`require "rack"; res = Rack::Response.new("body", 201); puts res.status`, "201\n"},
		{`require "rack"; res = Rack::Response.new(["a","b"]); puts res.body.join`, "ab\n"},
		{`require "rack"; res = Rack::Response.new(nil, 200, {"x"=>"y"}); puts res["x"]`, "y\n"},
		{`require "rack"; res = Rack::Response.new; res.status = 404; puts res.status`, "404\n"},
		{`require "rack"; res = Rack::Response.new; res["a"] = "b"; puts res.headers["a"]`, "b\n"},
		{`require "rack"; res = Rack::Response.new; res.set_header("a","b"); puts res["a"]`, "b\n"},
		{`require "rack"; res = Rack::Response.new; res.content_type = "text/css"; puts res.content_type`, "text/css\n"},
		{`require "rack"; res = Rack::Response.new; res.redirect("/here", 301); puts res.status; puts res.location`, "301\n/here\n"},
		{`require "rack"; res = Rack::Response.new; res.redirect("/here"); puts res.status`, "302\n"},
		{`require "rack"; res = Rack::Response.new; puts res.empty?`, "true\n"},
		{`require "rack"; res = Rack::Response.new("x"); puts res.empty?`, "false\n"},
		{`require "rack"; res = Rack::Response.new("x", 302); puts res.redirect?`, "true\n"},
		{`require "rack"; res = Rack::Response.new("x", 200); puts res.ok?`, "true\n"},
		{`require "rack"; res = Rack::Response.new("x", 404); puts res.not_found?`, "true\n"},
		{`require "rack"; res = Rack::Response.new("x", 200, {"c"=>"d"}); a = res.to_a; puts a[0]; puts a[2].join`, "200\nx\n"},
		{`require "rack"; res = Rack::Response.new; puts res.write("z")`, "z\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Error arms.
	for _, src := range []string{
		`require "rack"; Rack::Response.new[]`,
		`require "rack"; Rack::Response.new.[]=("a")`,
		`require "rack"; Rack::Response.new.set_header("a")`,
		`require "rack"; Rack::Response.new.redirect`,
	} {
		if class, _ := evalErr(t, src); class != "ArgumentError" {
			t.Errorf("src=%q class=%q want ArgumentError", src, class)
		}
	}
}

// TestRackUtils covers every Rack::Utils function and its error arms.
func TestRackUtils(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rack/utils"; puts Rack::Utils.escape("a b")`, "a+b\n"},
		{`require "rack/utils"; puts Rack::Utils.escape_path("a b")`, "a%20b\n"},
		{`require "rack/utils"; puts Rack::Utils.escape_html("<a>")`, "&lt;a&gt;\n"},
		{`require "rack/utils"; puts Rack::Utils.unescape_html("&lt;a&gt;")`, "<a>\n"},
		{`require "rack/utils"; puts Rack::Utils.unescape("a+b")`, "a b\n"},
		{`require "rack/utils"; puts Rack::Utils.unescape_path("a%20b")`, "a b\n"},
		{`require "rack/utils"; puts Rack::Utils.parse_query("a=1&b=2")["a"]`, "1\n"},
		{`require "rack/utils"; puts Rack::Utils.build_query("a"=>"1")`, "a=1\n"},
		{`require "rack/utils"; puts Rack::Utils.build_query("a"=>["1","2"])`, "a=1&a=2\n"},
		{`require "rack/utils"; puts Rack::Utils.parse_nested_query("a[b][c]=1")["a"]["b"]["c"]`, "1\n"},
		{`require "rack/utils"; puts Rack::Utils.parse_nested_query("e[]=x&e[]=y")["e"].inspect`, `["x", "y"]` + "\n"},
		{`require "rack/utils"; puts Rack::Utils.parse_nested_query("a=1;b=2", ";")["b"]`, "2\n"},
		{`require "rack/utils"; puts Rack::Utils.parse_nested_query("a=1", nil)["a"]`, "1\n"},
		{`require "rack/utils"; puts Rack::Utils.build_nested_query("a"=>{"b"=>"1"})`, "a%5Bb%5D=1\n"},
		{`require "rack/utils"; puts Rack::Utils.build_nested_query(["x","y"], "k")`, "k%5B%5D=x&k%5B%5D=y\n"},
		{`require "rack/utils"; puts Rack::Utils.q_values("text/html;q=0.5").inspect`, `[["text/html", 0.5]]` + "\n"},
		{`require "rack/utils"; puts Rack::Utils.best_q_match("text/html", ["text/html","application/json"])`, "text/html\n"},
		{`require "rack/utils"; p Rack::Utils.best_q_match("image/png", ["text/html"])`, "nil\n"},
		{`require "rack/utils"; puts Rack::Utils.parse_cookies_header("a=1; b=2")["b"]`, "2\n"},
		{`require "rack/utils"; puts Rack::Utils.parse_cookies("HTTP_COOKIE"=>"a=1; b=2")["a"]`, "1\n"},
		{`require "rack/utils"; puts Rack::Utils.parse_cookies({})["a"].inspect`, "nil\n"},
		{`require "rack/utils"; puts Rack::Utils.status_code(404)`, "404\n"},
		{`require "rack/utils"; puts Rack::Utils.status_code(:not_found)`, "404\n"},
		{`require "rack/utils"; puts Rack::Utils.status_code(:unprocessable_entity)`, "422\n"},
		{`require "rack/utils"; puts Rack::Utils.status_code("500")`, "500\n"},
		{`require "rack/utils"; puts Rack::Utils::HTTP_STATUS_CODES[404]`, "Not Found\n"},
		{`require "rack/utils"; puts Rack::Utils::HTTP_STATUS_CODES[200]`, "OK\n"},
		// secure_compare / clean_path_info / valid_path? — MRI-parity arms.
		{`require "rack/utils"; puts Rack::Utils.secure_compare("s3cr3t", "s3cr3t")`, "true\n"},
		{`require "rack/utils"; puts Rack::Utils.secure_compare("s3cr3t", "s3cr3x")`, "false\n"},
		{`require "rack/utils"; puts Rack::Utils.secure_compare("abc", "abcd")`, "false\n"},
		{`require "rack/utils"; puts Rack::Utils.clean_path_info("/foo/../bar")`, "/bar\n"},
		{`require "rack/utils"; puts Rack::Utils.clean_path_info("a/./b/../c")`, "a/c\n"},
		{`require "rack/utils"; puts Rack::Utils.clean_path_info("/../../etc/passwd")`, "/etc/passwd\n"},
		{`require "rack/utils"; puts Rack::Utils.clean_path_info("").inspect`, `"/"` + "\n"},
		{`require "rack/utils"; puts Rack::Utils.valid_path?("/ok/path")`, "true\n"},
		{`require "rack/utils"; puts Rack::Utils.valid_path?("bad\x00path")`, "false\n"},
		// select_best_encoding — note the (available, [[name, q], …]) arg order.
		{`require "rack/utils"; puts Rack::Utils.select_best_encoding(["gzip","identity"], [["gzip",1.0],["identity",0.5]])`, "gzip\n"},
		{`require "rack/utils"; puts Rack::Utils.select_best_encoding(["gzip","deflate","identity"], [["*",1.0]])`, "gzip\n"},
		{`require "rack/utils"; puts Rack::Utils.select_best_encoding(["gzip","identity"], []).inspect`, `"identity"` + "\n"},
		{`require "rack/utils"; p Rack::Utils.select_best_encoding(["gzip"], [["identity",0.0],["gzip",0.0]])`, "nil\n"},
		// forwarded_values — symbol keys, header order; nil on falsy/disallowed.
		{`require "rack/utils"; puts Rack::Utils.forwarded_values("for=1.2.3.4;proto=https").inspect`, `{for: ["1.2.3.4"], proto: ["https"]}` + "\n"},
		{`require "rack/utils"; puts Rack::Utils.forwarded_values("proto=https;for=1.2.3.4").inspect`, `{proto: ["https"], for: ["1.2.3.4"]}` + "\n"},
		{`require "rack/utils"; puts Rack::Utils.forwarded_values("host=example.com;by=a;for=b;proto=c").inspect`, `{host: ["example.com"], by: ["a"], for: ["b"], proto: ["c"]}` + "\n"},
		{`require "rack/utils"; puts Rack::Utils.forwarded_values("for=1.1.1.1, for=2.2.2.2").inspect`, `{for: ["1.1.1.1", "2.2.2.2"]}` + "\n"},
		{`require "rack/utils"; puts Rack::Utils.forwarded_values("").inspect`, "{}\n"},
		{`require "rack/utils"; p Rack::Utils.forwarded_values(nil)`, "nil\n"},
		{`require "rack/utils"; p Rack::Utils.forwarded_values(false)`, "nil\n"},
		{`require "rack/utils"; p Rack::Utils.forwarded_values("cookie=x")`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, src := range []string{
		`require "rack/utils"; Rack::Utils.unescape("%ZZ")`,
		`require "rack/utils"; Rack::Utils.parse_query("a=%ZZ")`,
		`require "rack/utils"; Rack::Utils.parse_nested_query("a=%ZZ")`,
		`require "rack/utils"; Rack::Utils.build_nested_query("scalar")`,
		`require "rack/utils"; Rack::Utils.status_code(:bogus)`,
		`require "rack/utils"; Rack::Utils.best_q_match("x")`,
		`require "rack/utils"; Rack::Utils.secure_compare("only-one")`,
		`require "rack/utils"; Rack::Utils.select_best_encoding(["gzip"])`,
		`require "rack/utils"; Rack::Utils.forwarded_values`,
	} {
		if class, _ := evalErr(t, src); class != "ArgumentError" {
			t.Errorf("src=%q class=%q want ArgumentError", src, class)
		}
	}
}

// TestRackStr covers the String / Symbol / default arms.
func TestRackStr(t *testing.T) {
	if rackStr(object.NewString("x")) != "x" || rackStr(object.Symbol("y")) != "y" || rackStr(object.Integer(3)) != "3" {
		t.Error("rackStr arms")
	}
}

// TestRackInt covers Integer / valid-String / invalid-String / other arms.
func TestRackInt(t *testing.T) {
	if rackInt(object.Integer(7), 0) != 7 {
		t.Error("int arm")
	}
	if rackInt(object.NewString("9"), 0) != 9 {
		t.Error("valid string arm")
	}
	if rackInt(object.NewString("no"), 5) != 5 {
		t.Error("invalid string arm")
	}
	if rackInt(object.NilV, 4) != 4 {
		t.Error("other arm")
	}
}

// TestRackArg covers absent / present.
func TestRackArg(t *testing.T) {
	if rackArg(nil) != object.NilV {
		t.Error("absent")
	}
	if rackArg([]object.Value{object.Integer(1)}) != object.Integer(1) {
		t.Error("present")
	}
}

// TestRackEnvValue covers every arm of the env value converter.
func TestRackEnvValue(t *testing.T) {
	checks := []struct {
		in   object.Value
		want any
	}{
		{object.NilV, nil},
		{object.NewString("s"), "s"},
		{object.Symbol("y"), "y"},
		{object.Bool(true), true},
		{object.Integer(3), int64(3)},
		{object.Float(1.5), 1.5},
		{&object.Array{}, "[]"},
	}
	for _, c := range checks {
		if got := rackEnvValue(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("rackEnvValue(%#v)=%#v want %#v", c.in, got, c.want)
		}
	}
}

// TestRackEnvTypeError covers rackEnv's non-Hash raise.
func TestRackEnvTypeError(t *testing.T) {
	if class, _ := evalErr(t, `require "rack"; Rack::Request.new(1)`); class != "TypeError" {
		t.Errorf("class=%q want TypeError", class)
	}
}

// TestRackResponseBody covers absent / Nil / String / Array / default arms.
func TestRackResponseBody(t *testing.T) {
	if rackResponseBody(nil) != nil {
		t.Error("absent")
	}
	if rackResponseBody([]object.Value{object.NilV}) != nil {
		t.Error("nil arm")
	}
	if got := rackResponseBody([]object.Value{object.NewString("x")}); !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("string arm got=%#v", got)
	}
	if got := rackResponseBody([]object.Value{&object.Array{Elems: []object.Value{object.NewString("a"), object.Integer(2)}}}); !reflect.DeepEqual(got, []string{"a", "2"}) {
		t.Errorf("array arm got=%#v", got)
	}
	if got := rackResponseBody([]object.Value{object.Integer(9)}); !reflect.DeepEqual(got, []string{"9"}) {
		t.Errorf("default arm got=%#v", got)
	}
}

// TestRackHeadersFrom covers the Hash and non-Hash arms.
func TestRackHeadersFrom(t *testing.T) {
	if h := rackHeadersFrom(object.NilV); h.Len() != 0 {
		t.Error("non-Hash arm")
	}
	hash := object.NewHash()
	hash.Set(object.NewString("A"), object.NewString("b"))
	if h := rackHeadersFrom(hash); h.Get("a") != "b" {
		t.Errorf("hash arm got=%v", h.Get("a"))
	}
}

// TestRackParamsFromHash covers the Hash and non-Hash arms.
func TestRackParamsFromHash(t *testing.T) {
	if p := rackParamsFromHash(object.Integer(1)); p.Len() != 0 {
		t.Error("non-Hash arm")
	}
	hash := object.NewHash()
	hash.Set(object.NewString("k"), object.NewString("v"))
	if p := rackParamsFromHash(hash); rack.BuildQuery(p) != "k=v" {
		t.Errorf("hash arm got=%q", rack.BuildQuery(p))
	}
}

// TestRackToGoNested covers the Hash → *Params, Array → []any and scalar
// delegation arms of the nested build_nested_query converter.
func TestRackToGoNested(t *testing.T) {
	inner := object.NewHash()
	inner.Set(object.NewString("b"), object.NewString("1"))
	outer := object.NewHash()
	outer.Set(object.NewString("a"), inner)
	got, err := rack.BuildNestedQuery(rackToGoNested(outer), "")
	if err != nil || got != "a%5Bb%5D=1" {
		t.Errorf("hash arm got=%q err=%v", got, err)
	}
	if arr, ok := rackToGoNested(object.NewArray(object.NewString("x"))).([]any); !ok || len(arr) != 1 {
		t.Errorf("array arm got=%#v", rackToGoNested(object.NewArray(object.NewString("x"))))
	}
	if s, ok := rackToGoNested(object.NewString("s")).(string); !ok || s != "s" {
		t.Errorf("scalar arm got=%#v", rackToGoNested(object.NewString("s")))
	}
}

// TestRackStrArray covers the Array and non-Array arms.
func TestRackStrArray(t *testing.T) {
	if rackStrArray(object.NilV) != nil {
		t.Error("non-Array arm")
	}
	got := rackStrArray(object.NewArray(object.NewString("a"), object.Integer(2)))
	if !reflect.DeepEqual(got, []string{"a", "2"}) {
		t.Errorf("array arm got=%#v", got)
	}
}

// TestRackCookieHeader covers the present-key, absent-key and non-Hash arms.
func TestRackCookieHeader(t *testing.T) {
	if rackCookieHeader(object.Integer(1)) != "" {
		t.Error("non-Hash arm")
	}
	if rackCookieHeader(object.NewHash()) != "" {
		t.Error("absent-key arm")
	}
	h := object.NewHash()
	h.Set(object.NewString("HTTP_COOKIE"), object.NewString("a=1"))
	if rackCookieHeader(h) != "a=1" {
		t.Errorf("present-key arm got=%q", rackCookieHeader(h))
	}
}

// TestRackParamsAndHeadersNil covers the nil-guard arms.
func TestRackParamsAndHeadersNil(t *testing.T) {
	if rackParamsToHash(nil).Len() != 0 {
		t.Error("nil params")
	}
	if rackHeadersToHash(nil).Len() != 0 {
		t.Error("nil headers")
	}
}

// TestRackFromGo covers every arm of the Go->object converter.
func TestRackFromGo(t *testing.T) {
	params := rack.NewParams()
	params.Set("k", "v")
	checks := []struct {
		in   any
		want string
	}{
		{nil, "nil"},
		{"s", `"s"`},
		{true, "true"},
		{int(3), "3"},
		{int64(4), "4"},
		{1.5, "1.5"},
		{[]any{"a", 1}, `["a", 1]`},
		{[]string{"x", "y"}, `["x", "y"]`},
		{map[string]any{"k": "v"}, `{"k" => "v"}`},
		{params, `{"k" => "v"}`},
		{struct{}{}, "nil"},
	}
	for _, c := range checks {
		if got := rackFromGo(c.in).Inspect(); got != c.want {
			t.Errorf("rackFromGo(%#v)=%q want %q", c.in, got, c.want)
		}
	}
}

// TestRackFloat covers the Float / Integer / default arms of the quality
// coercion used by select_best_encoding.
func TestRackFloat(t *testing.T) {
	if rackFloat(object.Float(0.5)) != 0.5 {
		t.Error("float arm")
	}
	if rackFloat(object.Integer(1)) != 1.0 {
		t.Error("integer arm")
	}
	if rackFloat(object.NewString("x")) != 0 {
		t.Error("default arm")
	}
}

// TestRackQValues covers the non-Array, skip (non-pair / short) and valid arms
// of the accept_encoding converter.
func TestRackQValues(t *testing.T) {
	if rackQValues(object.NilV) != nil {
		t.Error("non-Array arm")
	}
	in := object.NewArray(
		object.NewString("scalar"),                                   // skipped: not a pair
		object.NewArray(object.NewString("short")),                   // skipped: < 2 elems
		object.NewArray(object.NewString("gzip"), object.Float(0.7)), // kept
	)
	got := rackQValues(in)
	if len(got) != 1 || got[0].Value != "gzip" || got[0].Quality != 0.7 {
		t.Errorf("valid arm got=%#v", got)
	}
}

// TestRackForwardedArg covers the nil, false and truthy arms.
func TestRackForwardedArg(t *testing.T) {
	if _, ok := rackForwardedArg(object.NilV); ok {
		t.Error("nil arm")
	}
	if _, ok := rackForwardedArg(nil); ok {
		t.Error("go-nil arm")
	}
	if _, ok := rackForwardedArg(object.Bool(false)); ok {
		t.Error("false arm")
	}
	s, ok := rackForwardedArg(object.NewString("for=x"))
	if !ok || s != "for=x" {
		t.Errorf("truthy arm got=%q ok=%v", s, ok)
	}
	// A truthy non-string is stringified (MRI applies #to_s).
	if s, ok := rackForwardedArg(object.Bool(true)); !ok || s != "true" {
		t.Errorf("truthy-bool arm got=%q ok=%v", s, ok)
	}
}

// TestRackForwardedOrder covers every arm of the header-order reconstruction:
// terminating quote, escaped quote, unterminated quote, separator, end-of-input,
// duplicate keys (seen), and a disallowed name (dropped).
func TestRackForwardedOrder(t *testing.T) {
	cases := []struct {
		header string
		want   []string
	}{
		{"", nil},
		{"for=b;host=h", []string{"for", "host"}},
		{`for="a\"b";proto=x`, []string{"for", "proto"}},
		{`for="unterminated`, []string{"for"}},
		{"for=a;for=b;host=h", []string{"for", "host"}},
		{"cookie=x;for=y", []string{"for"}},
		{"\nfor=z", []string{"for"}},
	}
	for _, c := range cases {
		if got := rackForwardedOrder(c.header); !reflect.DeepEqual(got, c.want) {
			t.Errorf("rackForwardedOrder(%q)=%#v want %#v", c.header, got, c.want)
		}
	}
}

// TestRackToGo covers every arm of the object->Go converter.
func TestRackToGo(t *testing.T) {
	arr := &object.Array{Elems: []object.Value{object.Integer(1)}}
	h := object.NewHash()
	h.Set(object.NewString("k"), object.Integer(2))
	checks := []struct {
		in   object.Value
		want any
	}{
		{object.NilV, nil},
		{object.Bool(true), true},
		{object.Integer(5), int64(5)},
		{object.Float(2.5), 2.5},
		{object.NewString("s"), "s"},
		{object.Symbol("y"), "y"},
		{arr, []any{int64(1)}},
		{h, map[string]any{"k": int64(2)}},
	}
	for _, c := range checks {
		if got := rackToGo(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("rackToGo(%#v)=%#v want %#v", c.in, got, c.want)
		}
	}
	// default arm: a wrapper value falls back to its to_s.
	if got := rackToGo(&RackResponse{}); got != "#<Rack::Response>" {
		t.Errorf("default arm got=%#v", got)
	}
}
