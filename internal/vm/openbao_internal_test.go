// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	openbao "github.com/go-ruby-openbao/openbao"
)

// vaultRouter is the injected in-process stub Doer the Vault suite drives: it
// returns canned Vault JSON keyed on the request method + /v1/ path, so every
// endpoint the binding exercises has a deterministic response — no real Vault
// server, no socket, no goroutine. Special paths drive the error tree (404 →
// nil, 4xx → HTTPClientError, 5xx → HTTPServerError, a transport error →
// HTTPConnectionError, an invalid 2xx body → VaultError).
func vaultRouter(req *openbao.Request) (*openbao.Response, error) {
	u, _ := url.Parse(req.URL)
	p := strings.TrimPrefix(u.Path, "/v1/")
	key := req.Method + " " + p

	jsonResp := func(code int, body string) (*openbao.Response, error) {
		return &openbao.Response{
			StatusCode: code,
			Body:       []byte(body),
			Header:     map[string][]string{"Content-Type": {"application/json"}},
		}, nil
	}
	noContent := func() (*openbao.Response, error) {
		return &openbao.Response{StatusCode: 204, Header: map[string][]string{}}, nil
	}

	// Error / edge paths first (they share verbs with the happy paths).
	switch p {
	case "secret/data/conn":
		return nil, errors.New("connection refused")
	case "secret/data/badjson":
		return jsonResp(200, "{not valid json")
	case "secret/data/forbidden":
		return jsonResp(403, `{"errors":["permission denied"]}`)
	case "secret/data/boom":
		return jsonResp(500, `{"errors":["internal error"]}`)
	case "secret/data/missing", "nope":
		return jsonResp(404, `{"errors":["not found"]}`)
	}

	switch key {
	// --- KV v2 (mount "secret") ---
	case "GET secret/data/foo":
		return jsonResp(200, `{"request_id":"rid-1","lease_id":"lease-9","lease_duration":3600,"renewable":true,"data":{"data":{"password":"s3cr3t"},"metadata":{"version":2}},"warnings":["deprecated path"],"wrap_info":{"token":"wtok"}}`)
	case "PUT secret/data/foo":
		return jsonResp(200, `{"data":{"version":3}}`)
	case "LIST secret/metadata/foo":
		return jsonResp(200, `{"data":{"keys":["alpha","beta"]}}`)
	case "DELETE secret/data/foo":
		return noContent()
	case "POST secret/delete/foo", "POST secret/undelete/foo", "POST secret/destroy/foo":
		return noContent()
	case "GET secret/metadata/foo":
		return jsonResp(200, `{"data":{"current_version":2}}`)
	case "PUT secret/metadata/foo":
		return noContent()
	case "DELETE secret/metadata/foo":
		return noContent()

	// --- KV v1 (mount "kv1") ---
	case "GET kv1/creds":
		return jsonResp(200, `{"data":{"user":"admin"}}`)
	case "PUT kv1/creds":
		return noContent()
	case "LIST kv1/creds":
		return jsonResp(200, `{"data":{"keys":["one"]}}`)
	case "DELETE kv1/creds":
		return noContent()

	// --- Logical (arbitrary paths) ---
	case "PUT cubbyhole/note":
		return jsonResp(200, `{"data":{"stored":true}}`)

	// --- Transit (mount "transit") ---
	case "POST transit/encrypt/mykey":
		return jsonResp(200, `{"data":{"ciphertext":"vault:v1:ABC"}}`)
	case "POST transit/decrypt/mykey":
		return jsonResp(200, `{"data":{"plaintext":"aGVsbG8="}}`)
	case "POST transit/rewrap/mykey":
		return jsonResp(200, `{"data":{"ciphertext":"vault:v1:DEF"}}`)
	case "POST transit/sign/mykey":
		return jsonResp(200, `{"data":{"signature":"vault:v1:SIG"}}`)
	case "POST transit/verify/mykey":
		return jsonResp(200, `{"data":{"valid":true}}`)
	case "POST transit/datakey/plaintext/mykey":
		return jsonResp(200, `{"data":{"ciphertext":"vault:v1:WRAP","plaintext":"cGxhaW4="}}`)

	// --- Sys ---
	case "GET sys/health":
		return jsonResp(200, `{"initialized":true,"sealed":false,"version":"1.15.0"}`)
	case "GET sys/seal-status":
		return jsonResp(200, `{"sealed":false,"t":1,"n":1}`)
	case "GET sys/mounts":
		return jsonResp(200, `{"secret/":{"type":"kv"}}`)
	case "POST sys/mounts/kv2":
		return noContent()
	case "DELETE sys/mounts/kv2":
		return noContent()
	case "GET sys/policies/acl":
		return jsonResp(200, `{"data":{"keys":["default","root"]}}`)
	case "GET sys/policies/acl/default":
		return jsonResp(200, `{"data":{"name":"default","rules":"path \"*\" {}"}}`)
	case "PUT sys/policies/acl/mypol":
		return noContent()
	case "DELETE sys/policies/acl/mypol":
		return noContent()
	case "PUT sys/leases/renew":
		return jsonResp(200, `{"lease_id":"lease-9","lease_duration":100,"renewable":true}`)
	case "PUT sys/leases/revoke":
		return noContent()
	case "PUT sys/leases/lookup":
		return jsonResp(200, `{"data":{"lease_id":"lease-9"}}`)

	// --- Auth ---
	case "GET auth/token/lookup-self":
		return jsonResp(200, `{"data":{"id":"s.root","policies":["root"]}}`)
	case "POST auth/token/renew-self":
		return jsonResp(200, `{"auth":{"client_token":"s.renewed","accessor":"acc-1","policies":["default"],"token_policies":["default"],"metadata":{"user":"me"},"lease_duration":60,"renewable":true}}`)
	case "POST auth/token/revoke-self":
		return noContent()
	case "POST auth/approle/login":
		return jsonResp(200, `{"auth":{"client_token":"s.approle","policies":["p1"]}}`)
	case "POST auth/userpass/login/alice":
		return jsonResp(200, `{"auth":{"client_token":"s.userpass"}}`)
	}
	return jsonResp(200, `{"data":{"unrouted":"`+key+`"}}`)
}

// installVaultStub points the openbaoTransport seam at the in-process router for
// the duration of the test, restoring the production Net::HTTP transport after.
func installVaultStub(t *testing.T) {
	t.Helper()
	prev := openbaoTransport
	openbaoTransport = func(_ *VM) openbao.Doer { return openbao.DoerFunc(vaultRouter) }
	t.Cleanup(func() { openbaoTransport = prev })
}

// vaultPrelude requires vault and builds a configured client bound to C.
const vaultPrelude = `require "vault"
C = Vault::Client.new(address: "http://vault.test:8200", token: "s.root", namespace: "ns1")
`

// TestVaultFeatureAndModule covers the require probe, the OpenBao alias and the
// class / error tree.
func TestVaultFeatureAndModule(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "vault"`, "true\n"},
		{`require "vault"; p require "vault"`, "false\n"},
		{`p require "openbao"`, "true\n"},
		{`require "vault"; p Vault.is_a?(Module)`, "true\n"},
		{`require "openbao"; p OpenBao.is_a?(Module)`, "true\n"},
		{`require "vault"; require "openbao"; p OpenBao::Client == Vault::Client`, "true\n"},
		{`require "vault"; p Vault::Client.is_a?(Class)`, "true\n"},
		{`require "vault"; p Vault::Secret.is_a?(Class)`, "true\n"},
		{`require "vault"; p Vault::VaultError < StandardError`, "true\n"},
		{`require "vault"; p Vault::HTTPError < Vault::VaultError`, "true\n"},
		{`require "vault"; p Vault::HTTPClientError < Vault::HTTPError`, "true\n"},
		{`require "vault"; p Vault::HTTPServerError < Vault::HTTPError`, "true\n"},
		{`require "vault"; p Vault::HTTPConnectionError < Vault::HTTPError`, "true\n"},
		{`require "vault"; p Vault::MissingRequiredStateError < Vault::VaultError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestVaultClientConfig covers the client constructor keyword handling, the env
// defaults path, the non-Hash trailing argument, and the token / namespace /
// address accessors and setters.
func TestVaultClientConfig(t *testing.T) {
	installVaultStub(t)
	cases := []struct{ src, want string }{
		{`p C.address`, "\"http://vault.test:8200\"\n"},
		{`p C.token`, "\"s.root\"\n"},
		{`p C.namespace`, "\"ns1\"\n"},
		{`C.token = "s.new"; p C.token`, "\"s.new\"\n"},
		{`C.namespace = "ns2"; p C.namespace`, "\"ns2\"\n"},
		// no kwargs → library resolves the default address
		{`p Vault::Client.new.address`, "\"https://127.0.0.1:8200\"\n"},
		// a non-Hash trailing argument is ignored (kwHash → nil)
		{`p Vault::Client.new("ignored").is_a?(Vault::Client)`, "true\n"},
		// timeout: keyword is accepted (ignored by the injected Doer)
		{`p Vault::Client.new(address: "http://x", timeout: 5).address`, "\"http://x\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, vaultPrelude+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestVaultLogical covers the generic read/write/list/delete verbs, the Secret
// accessors and the 404 → nil translation.
func TestVaultLogical(t *testing.T) {
	installVaultStub(t)
	cases := []struct{ src, want string }{
		{`s = C.logical.read("secret/data/foo"); p s.data["data"]["password"]`, "\"s3cr3t\"\n"},
		{`s = C.logical.read("secret/data/foo"); p s.request_id`, "\"rid-1\"\n"},
		{`s = C.logical.read("secret/data/foo"); p s.lease_id`, "\"lease-9\"\n"},
		{`s = C.logical.read("secret/data/foo"); p s.lease_duration`, "3600\n"},
		{`s = C.logical.read("secret/data/foo"); p s.renewable?`, "true\n"},
		{`s = C.logical.read("secret/data/foo"); p s.renewable`, "true\n"},
		{`s = C.logical.read("secret/data/foo"); p s.warnings`, "[\"deprecated path\"]\n"},
		{`s = C.logical.read("secret/data/foo"); p s.wrap_info["token"]`, "\"wtok\"\n"},
		{`s = C.logical.read("secret/data/foo"); p s.auth`, "nil\n"},
		{`s = C.logical.read("secret/data/foo"); p s.token`, "\"\"\n"},
		{`p C.logical.read("nope")`, "nil\n"},
		{`p C.logical.list("nope")`, "nil\n"},
		{`s = C.logical.write("cubbyhole/note", {"k" => "v"}); p s.data["stored"]`, "true\n"},
		// write with no data argument → empty map body
		{`p C.logical.write("cubbyhole/note").data["stored"]`, "true\n"},
		{`s = C.logical.list("secret/metadata/foo"); p s.data["keys"]`, "[\"alpha\", \"beta\"]\n"},
		{`p C.logical.delete("secret/data/foo")`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, vaultPrelude+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestVaultKV covers the KV v1 and KV v2 secrets-engine helpers.
func TestVaultKV(t *testing.T) {
	installVaultStub(t)
	cases := []struct{ src, want string }{
		// KV v1
		{`p C.kv_v1("kv1").read("creds").data["user"]`, "\"admin\"\n"},
		{`p C.kv_v1("kv1").write("creds", {"user" => "admin"})`, "nil\n"},
		{`p C.kv_v1("kv1").list("creds").data["keys"]`, "[\"one\"]\n"},
		{`p C.kv_v1("kv1").delete("creds")`, "nil\n"},
		// KV v2 (kv and kv_v2 are the same helper; default mount "secret")
		{`p C.kv.read("foo").data["data"]["password"]`, "\"s3cr3t\"\n"},
		{`p C.kv_v2.read_version("foo", 1).data["metadata"]["version"]`, "2\n"},
		{`p C.kv.write("foo", {"password" => "n"}).data["version"]`, "3\n"},
		{`p C.kv.list("foo").data["keys"]`, "[\"alpha\", \"beta\"]\n"},
		{`p C.kv.delete("foo")`, "nil\n"},
		{`p C.kv.delete_versions("foo", 1, 2)`, "nil\n"},
		{`p C.kv.undelete("foo", 1)`, "nil\n"},
		{`p C.kv.destroy("foo", 1)`, "nil\n"},
		{`p C.kv.read_metadata("foo").data["current_version"]`, "2\n"},
		{`p C.kv.write_metadata("foo", {"max_versions" => 3})`, "nil\n"},
		{`p C.kv.delete_metadata("foo")`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, vaultPrelude+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestVaultTransit covers the transit cryptographic-operation surface, including
// the optional context argument (present and absent).
func TestVaultTransit(t *testing.T) {
	installVaultStub(t)
	cases := []struct{ src, want string }{
		{`p C.transit("transit").encrypt("mykey", "hello").data["ciphertext"]`, "\"vault:v1:ABC\"\n"},
		{`p C.transit.encrypt("mykey", "hello", "ctx").data["ciphertext"]`, "\"vault:v1:ABC\"\n"},
		{`p C.transit.decrypt("mykey", "vault:v1:ABC").data["plaintext"]`, "\"aGVsbG8=\"\n"},
		{`p C.transit.rewrap("mykey", "vault:v1:ABC").data["ciphertext"]`, "\"vault:v1:DEF\"\n"},
		{`p C.transit.sign("mykey", "hello").data["signature"]`, "\"vault:v1:SIG\"\n"},
		{`p C.transit.verify("mykey", "hello", "vault:v1:SIG").data["valid"]`, "true\n"},
		{`p C.transit.generate_data_key("mykey", "plaintext").data["ciphertext"]`, "\"vault:v1:WRAP\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, vaultPrelude+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestVaultSys covers the system-backend surface (health / seal status / mounts /
// policies / leases), including the raw-map and boolean-return endpoints.
func TestVaultSys(t *testing.T) {
	installVaultStub(t)
	cases := []struct{ src, want string }{
		{`p C.sys.health["version"]`, "\"1.15.0\"\n"},
		{`p C.sys.seal_status["sealed"]`, "false\n"},
		{`p C.sys.mounts["secret/"]["type"]`, "\"kv\"\n"},
		{`p C.sys.enable_mount("kv2", "kv", {"description" => "d"})`, "true\n"},
		{`p C.sys.disable_mount("kv2")`, "true\n"},
		{`p C.sys.policies.data["keys"]`, "[\"default\", \"root\"]\n"},
		{`p C.sys.policy("default").data["name"]`, "\"default\"\n"},
		{`p C.sys.put_policy("mypol", "path \"*\" {}")`, "true\n"},
		{`p C.sys.delete_policy("mypol")`, "true\n"},
		{`p C.sys.renew_lease("lease-9", 100).lease_duration`, "100\n"},
		{`p C.sys.revoke_lease("lease-9")`, "true\n"},
		{`p C.sys.lookup_lease("lease-9").data["lease_id"]`, "\"lease-9\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, vaultPrelude+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestVaultAuth covers the token self-management helpers, the AppRole / Userpass
// logins, the Secret auth block and Client#adopt_token.
func TestVaultAuth(t *testing.T) {
	installVaultStub(t)
	cases := []struct{ src, want string }{
		{`p C.auth.token.lookup_self.data["id"]`, "\"s.root\"\n"},
		{`p C.auth.token.renew_self.token`, "\"s.renewed\"\n"},
		{`p C.auth.token.renew_self(120).auth[:client_token]`, "\"s.renewed\"\n"},
		{`p C.auth.token.renew_self.auth[:policies]`, "[\"default\"]\n"},
		{`p C.auth.token.renew_self.auth[:renewable]`, "true\n"},
		{`p C.auth.token.revoke_self`, "true\n"},
		{`p C.auth.app_role("approle").login("rid", "sid").token`, "\"s.approle\"\n"},
		{`p C.auth.app_role.login("rid", "sid").token`, "\"s.approle\"\n"},
		{`p C.auth.userpass("userpass").login("alice", "pw").token`, "\"s.userpass\"\n"},
		{`s = C.auth.app_role.login("rid", "sid"); C.adopt_token(s); p C.token`, "\"s.approle\"\n"},
		// adopt_token with a non-Secret argument is a no-op
		{`C.adopt_token(nil); p C.token`, "\"s.root\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, vaultPrelude+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestVaultErrorTree covers the whole error tree: a 4xx HTTPClientError (with the
// #code / #errors context and the superclass predicates), a 5xx HTTPServerError,
// a transport failure HTTPConnectionError and an invalid 2xx body VaultError.
func TestVaultErrorTree(t *testing.T) {
	installVaultStub(t)
	cases := []struct{ src, want string }{
		{`begin
  C.logical.read("secret/data/forbidden")
rescue Vault::HTTPClientError => e
  p e.code
  p e.errors
  p e.is_a?(Vault::HTTPError)
  p e.is_a?(Vault::VaultError)
end`, "403\n[\"permission denied\"]\ntrue\ntrue\n"},
		{`begin
  C.logical.read("secret/data/boom")
rescue Vault::HTTPServerError => e
  p e.code
end`, "500\n"},
		{`begin
  C.logical.read("secret/data/conn")
rescue Vault::HTTPConnectionError => e
  p e.code
  p e.errors
end`, "0\n[]\n"},
		{`begin
  C.logical.read("secret/data/badjson")
rescue Vault::VaultError => e
  p e.is_a?(Vault::VaultError)
end`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, vaultPrelude+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestVaultValueSurface covers the wrapper stringification / truthiness of every
// Vault::* value, and the nil-map branch of #data / #wrap_info (a secret carrying
// no data or wrap-info block).
func TestVaultValueSurface(t *testing.T) {
	installVaultStub(t)
	cases := []struct{ src, want string }{
		{`vals = [C, C.logical, C.kv_v1("kv1"), C.kv, C.transit, C.sys, C.auth,
  C.auth.token, C.auth.app_role, C.auth.userpass, C.logical.read("secret/data/foo")]
vals.each { |o| o.to_s; o.inspect }
p vals.all? { |o| !!o }`, "true\n"},
		{`p C.to_s.start_with?("#<Vault::Client")`, "true\n"},
		// a login secret carries an auth block but no data / wrap_info
		{`p C.auth.app_role.login("rid", "sid").data`, "nil\n"},
		{`p C.kv.write("foo", {"k" => "v"}).wrap_info`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, vaultPrelude+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestVaultDefaultTransport covers the production openbaoTransport seam: it
// yields the Net::HTTP-backed Doer.
func TestVaultDefaultTransport(t *testing.T) {
	d := openbaoTransport(New(io.Discard))
	if _, ok := d.(*vaultNetHTTPDoer); !ok {
		t.Fatalf("default transport = %T, want *vaultNetHTTPDoer", d)
	}
}

// TestVaultNetHTTPDoer covers the production Doer wired to rbgo's bound Net::HTTP,
// driving it against an in-process httptest server (loopback only): a JSON
// response with a body and headers, a bodyless (HEAD) response, and a malformed
// URL surfaced as a transport error.
func TestVaultNetHTTPDoer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Vault-Token", "echoed")
		io.WriteString(w, `{"data":{"k":"v"}}`)
	}))
	defer srv.Close()

	d := &vaultNetHTTPDoer{vm: New(io.Discard)}

	resp, err := d.Do(&openbao.Request{
		Method:  "GET",
		URL:     srv.URL + "/v1/secret/data/foo",
		Headers: map[string]string{"X-Vault-Token": "t"},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if string(resp.Body) != `{"data":{"k":"v"}}` {
		t.Fatalf("body = %q", resp.Body)
	}
	if len(resp.Header["content-type"]) == 0 {
		t.Fatalf("missing content-type header: %v", resp.Header)
	}

	// A HEAD request carries no body, exercising the nil-body branch.
	head, err := d.Do(&openbao.Request{
		Method:  "HEAD",
		URL:     srv.URL + "/v1/secret/data/foo",
		Headers: map[string]string{},
	})
	if err != nil {
		t.Fatalf("HEAD Do: %v", err)
	}
	if head.Body != nil {
		t.Fatalf("HEAD body = %q, want nil", head.Body)
	}

	// A malformed URL is returned as a transport error.
	if _, err := d.Do(&openbao.Request{
		Method:  "GET",
		URL:     "http://\x7f/v1/x",
		Headers: map[string]string{},
	}); err == nil {
		t.Fatal("expected an error for a malformed URL")
	}
}
