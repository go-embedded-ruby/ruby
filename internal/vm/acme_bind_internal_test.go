// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	acme "github.com/go-ruby-acme/acme"
)

// acmeMock is an in-process, in-memory ACME (RFC 8555) server sufficient to drive
// the whole acme-client flow — directory, new-nonce, new-account, new-order,
// authz, challenge, finalize and cert download — with no real network. Individual
// endpoints can be told to fail to exercise the binding's error branches. It is a
// trimmed copy of the go-ruby-acme package's own test double.
type acmeMock struct {
	srv     *httptest.Server
	nonce   atomic.Int64
	certPEM []byte

	failAccount   int    // if non-zero, new-account replies with this status + problem
	accountProb   string // full problem type URN for failAccount
	rateLimitAcct bool   // new-account replies 429 rateLimited (with Retry-After)
	failChallenge bool   // challenge validates to "invalid" with an error
	failNewOrder  bool   // new-order replies malformed
	failOrderGet  bool   // GET order replies malformed
	failAuthz     bool   // authz fetch replies malformed
	failFinalize  bool   // finalize replies malformed
	failChallReq  bool   // challenge accept/get replies malformed

	challengeStatus string // status returned by challenge/authz/order gates
	orderNoCertURL  bool   // finalize/order omit the certificate URL
	onlyHTTP01      bool   // authz offers only the http-01 challenge
}

func newACMEMock(t *testing.T) *acmeMock {
	t.Helper()
	m := &acmeMock{challengeStatus: "valid"}
	m.certPEM = acmeSelfSignedChainPEM(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/directory", m.handleDirectory)
	mux.HandleFunc("/new-nonce", m.handleNewNonce)
	mux.HandleFunc("/new-account", m.handleNewAccount)
	mux.HandleFunc("/account/1", m.handleAccount)
	mux.HandleFunc("/new-order", m.handleNewOrder)
	mux.HandleFunc("/order/1", m.handleOrder)
	mux.HandleFunc("/order/1/finalize", m.handleFinalize)
	mux.HandleFunc("/authz/1", m.handleAuthz)
	mux.HandleFunc("/challenge/1", m.handleChallenge)
	mux.HandleFunc("/cert/1", m.handleCert)
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func (m *acmeMock) url(path string) string { return m.srv.URL + path }

func (m *acmeMock) setNonce(w http.ResponseWriter) {
	w.Header().Set("Replay-Nonce", fmt.Sprintf("nonce-%d", m.nonce.Add(1)))
}

func (m *acmeMock) writeProblem(w http.ResponseWriter, status int, problemType, detail string) {
	m.setNonce(w)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": problemType, "detail": detail})
}

const acmeURNPrefix = "urn:ietf:params:acme:error:"

func (m *acmeMock) handleDirectory(w http.ResponseWriter, _ *http.Request) {
	m.setNonce(w)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"newNonce":   m.url("/new-nonce"),
		"newAccount": m.url("/new-account"),
		"newOrder":   m.url("/new-order"),
		"revokeCert": m.url("/revoke-cert"),
		"keyChange":  m.url("/key-change"),
		"meta":       map[string]any{"termsOfService": m.url("/terms")},
	})
}

func (m *acmeMock) handleNewNonce(w http.ResponseWriter, _ *http.Request) {
	m.setNonce(w)
	w.WriteHeader(http.StatusNoContent)
}

func (m *acmeMock) handleNewAccount(w http.ResponseWriter, r *http.Request) {
	if m.rateLimitAcct {
		w.Header().Set("Retry-After", "5")
		m.writeProblem(w, http.StatusTooManyRequests, acmeURNPrefix+"rateLimited", "slow down")
		return
	}
	if m.failAccount != 0 {
		m.writeProblem(w, m.failAccount, m.accountProb, "account rejected")
		return
	}
	status := http.StatusCreated
	if acmeJWSPayloadContains(r, "onlyReturnExisting") {
		status = http.StatusOK
	}
	m.setNonce(w)
	w.Header().Set("Location", m.url("/account/1"))
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "valid", "contact": []string{"mailto:admin@example.com"},
	})
}

func acmeJWSPayloadContains(r *http.Request, sub string) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}
	var jws struct {
		Payload string `json:"payload"`
	}
	if json.Unmarshal(body, &jws) != nil || jws.Payload == "" {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(jws.Payload)
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), sub)
}

func (m *acmeMock) handleAccount(w http.ResponseWriter, _ *http.Request) {
	m.setNonce(w)
	w.Header().Set("Location", m.url("/account/1"))
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "valid", "contact": []string{"mailto:admin@example.com"},
	})
}

func (m *acmeMock) handleNewOrder(w http.ResponseWriter, _ *http.Request) {
	if m.failNewOrder {
		m.writeProblem(w, http.StatusBadRequest, acmeURNPrefix+"malformed", "bad order request")
		return
	}
	m.setNonce(w)
	w.Header().Set("Location", m.url("/order/1"))
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(m.orderBody("pending"))
}

func (m *acmeMock) orderBody(status string) map[string]any {
	body := map[string]any{
		"status":         status,
		"identifiers":    []map[string]string{{"type": "dns", "value": "example.com"}},
		"authorizations": []string{m.url("/authz/1")},
		"finalize":       m.url("/order/1/finalize"),
	}
	if status == "valid" && !m.orderNoCertURL {
		body["certificate"] = m.url("/cert/1")
	}
	return body
}

func (m *acmeMock) handleOrder(w http.ResponseWriter, _ *http.Request) {
	if m.failOrderGet {
		m.writeProblem(w, http.StatusBadRequest, acmeURNPrefix+"malformed", "bad order")
		return
	}
	m.setNonce(w)
	_ = json.NewEncoder(w).Encode(m.orderBody(m.challengeStatus))
}

func (m *acmeMock) handleFinalize(w http.ResponseWriter, _ *http.Request) {
	if m.failFinalize {
		m.writeProblem(w, http.StatusBadRequest, acmeURNPrefix+"malformed", "bad csr")
		return
	}
	m.setNonce(w)
	_ = json.NewEncoder(w).Encode(m.orderBody("valid"))
}

func (m *acmeMock) handleAuthz(w http.ResponseWriter, _ *http.Request) {
	if m.failAuthz {
		m.writeProblem(w, http.StatusBadRequest, acmeURNPrefix+"malformed", "bad authz")
		return
	}
	m.setNonce(w)
	challenges := []map[string]any{
		{"type": "http-01", "url": m.url("/challenge/1"), "token": "tok-http", "status": "pending"},
	}
	if !m.onlyHTTP01 {
		challenges = append(challenges,
			map[string]any{"type": "dns-01", "url": m.url("/challenge/1"), "token": "tok-dns", "status": "pending"},
			map[string]any{"type": "tls-alpn-01", "url": m.url("/challenge/1"), "token": "tok-alpn", "status": "pending"},
		)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     m.challengeStatus,
		"identifier": map[string]string{"type": "dns", "value": "example.com"},
		"challenges": challenges,
	})
}

func (m *acmeMock) handleChallenge(w http.ResponseWriter, _ *http.Request) {
	if m.failChallReq {
		m.writeProblem(w, http.StatusBadRequest, acmeURNPrefix+"malformed", "bad challenge")
		return
	}
	m.setNonce(w)
	ch := map[string]any{
		"type": "http-01", "url": m.url("/challenge/1"), "token": "tok-http", "status": m.challengeStatus,
	}
	if m.failChallenge {
		ch["status"] = "invalid"
		ch["error"] = map[string]any{
			"type": "urn:ietf:params:acme:error:unauthorized", "detail": "invalid response",
		}
	}
	_ = json.NewEncoder(w).Encode(ch)
}

func (m *acmeMock) handleCert(w http.ResponseWriter, _ *http.Request) {
	m.setNonce(w)
	w.Header().Set("Content-Type", "application/pem-certificate-chain")
	_, _ = w.Write(m.certPEM)
}

func acmeSelfSignedChainPEM(t *testing.T) []byte {
	t.Helper()
	var out []byte
	for i, cn := range []string{"example.com", "Mock ACME CA"} {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("gen key: %v", err)
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(int64(i + 1)),
			Subject:      pkix.Name{CommonName: cn},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			IsCA:         i == 1,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			t.Fatalf("create cert: %v", err)
		}
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	return out
}

// installACMESeam points the binding's transport seam at the mock server (with a
// no-retry backoff so injected 4xx/429 problems surface deterministically) for
// the duration of the test.
func installACMESeam(t *testing.T, m *acmeMock) {
	t.Helper()
	prev := acmeTestOptions
	acmeTestOptions = func() []acme.Option {
		return []acme.Option{
			acme.WithHTTPClient(m.srv.Client()),
			acme.WithRetryBackoff(func(int, *http.Request, *http.Response) time.Duration { return 0 }),
		}
	}
	t.Cleanup(func() { acmeTestOptions = prev })
}

// TestACMEHappyFlowViaRuby drives the complete acme-client flow through rbgo
// against the in-process mock CA: account -> order -> authz -> http-01 challenge
// -> finalize -> certificate chain, printing the issued PEM header (the sanity
// check) with no real Let's Encrypt and no network.
func TestACMEHappyFlowViaRuby(t *testing.T) {
	m := newACMEMock(t)
	installACMESeam(t, m)
	src := fmt.Sprintf(`
require "acme"
client = Acme::Client.new(directory: %q)
acct = client.new_account(contact: ["mailto:admin@example.com"], terms_of_service_agreed: true)
r = []
r << acct["status"]
r << acct["contact"][0]
order = client.new_order(identifiers: ["example.com"])
r << order.status
r << order.identifiers.join(",")
r << (order.url.empty? ? "no-url" : "url")
authzs = order.authorizations
r << authzs.length
az = authzs[0]
r << az.domain
r << az.status
r << az.wildcard?
r << az.challenges.length
chal = az.http01
r << chal.type
r << (chal.token.empty? ? "no-tok" : "tok")
r << (az.dns01.nil? ? "nil" : az.dns01.type)
r << (az.tls_alpn01.nil? ? "nil" : "present")
ka = chal.key_authorization
r << (ka.start_with?(chal.token + ".") ? "ka-ok" : "ka-bad")
chal.request_validation
r << chal.status
r << chal.error.inspect
chal.reload
order.reload
csr = Acme::Client::CertificateRequest.new(names: ["example.com", "www.example.com"])
r << csr.names.join(",")
r << csr.common_name
order.finalize(csr: csr)
r << order.status
pem = order.certificate
r << (pem.include?("BEGIN CERTIFICATE") ? "cert-ok" : "cert-bad")
r << pem.lines.first.strip
puts r.join("|")
`, m.url("/directory"))
	got := runSrc(t, src)
	want := strings.Join([]string{
		"valid", "mailto:admin@example.com", "pending", "example.com", "url",
		"1", "example.com", "valid", "false", "3", "http-01", "tok",
		"dns-01", "present", "ka-ok", "valid", "nil",
		"example.com,www.example.com", "example.com", "valid",
		"cert-ok", "-----BEGIN CERTIFICATE-----",
	}, "|")
	if got != want {
		t.Fatalf("acme flow\n got=%q\nwant=%q", got, want)
	}
}

// TestACMEAliasesAndVariants covers register (the new_account alias), a bare
// new_account (no contact/tos), the account lookup, single-String identifiers,
// the onlyHTTP01 authz (dns01/tls_alpn01 return nil) and an integer Hash key
// (the acmeKeyName fallthrough).
func TestACMEAliasesAndVariants(t *testing.T) {
	m := newACMEMock(t)
	m.onlyHTTP01 = true
	installACMESeam(t, m)
	src := fmt.Sprintf(`
require "acme/client"
client = Acme::Client.new(directory: %q)
r = []
a1 = client.register(contact: "mailto:a@b.c", 1 => "ignored")
r << a1["status"]
a2 = client.new_account
r << a2["status"]
a3 = client.account
r << a3["url"].empty?
order = client.new_order(identifiers: "example.com")
r << order.identifiers.join(",")
az = order.authorizations[0]
r << (az.dns01.nil? ? "nil" : "x")
r << (az.tls_alpn01.nil? ? "nil" : "x")
r << az.url.empty?
r << az.http01.url.empty?
puts r.join("|")
`, m.url("/directory"))
	if got, want := runSrc(t, src), "valid|valid|false|example.com|nil|nil|false|false"; got != want {
		t.Fatalf("variants got=%q want=%q", got, want)
	}
}

// TestACMEErrorTree injects each problem document and asserts the binding raises
// the matching Acme::Client::Error subclass (all StandardError / Acme::Client::Error).
func TestACMEErrorTree(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(m *acmeMock)
		rescue string
		marker string
	}{
		{"unauthorized", func(m *acmeMock) {
			m.failAccount = http.StatusForbidden
			m.accountProb = acmeURNPrefix + "unauthorized"
		}, "Acme::Client::Error::Unauthorized", "unauth"},
		{"malformed", func(m *acmeMock) { m.failAccount = http.StatusBadRequest; m.accountProb = acmeURNPrefix + "malformed" }, "Acme::Client::Error::Malformed", "malf"},
		{"badNonce", func(m *acmeMock) { m.failAccount = http.StatusBadRequest; m.accountProb = acmeURNPrefix + "badNonce" }, "Acme::Client::Error::BadNonce", "nonce"},
		{"rateLimited", func(m *acmeMock) { m.rateLimitAcct = true }, "Acme::Client::Error::RateLimited", "rate"},
		{"base", func(m *acmeMock) {
			m.failAccount = http.StatusBadRequest
			m.accountProb = acmeURNPrefix + "serverInternal"
		}, "Acme::Client::Error", "base"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newACMEMock(t)
			tc.setup(m)
			installACMESeam(t, m)
			src := fmt.Sprintf(`
require "acme"
client = Acme::Client.new(directory: %q)
begin
  client.new_account(contact: ["mailto:a@b.c"], terms_of_service_agreed: true)
  puts "no-raise"
rescue %s
  puts %q
rescue StandardError
  puts "std-only"
end
`, m.url("/directory"), tc.rescue, tc.marker)
			if got := runSrc(t, src); got != tc.marker {
				t.Fatalf("%s: got=%q want=%q", tc.name, got, tc.marker)
			}
		})
	}
}

// TestACMETransportError covers acmeRaise's default arm: a non-ACME transport
// error (a dead directory URL) surfaces as Acme::Error, not a problem subclass.
func TestACMETransportError(t *testing.T) {
	prev := acmeTestOptions
	acmeTestOptions = func() []acme.Option {
		return []acme.Option{acme.WithRetryBackoff(func(int, *http.Request, *http.Response) time.Duration { return 0 })}
	}
	t.Cleanup(func() { acmeTestOptions = prev })
	src := `
require "acme"
client = Acme::Client.new(directory: "http://127.0.0.1:1/directory")
begin
  client.new_account
  puts "no-raise"
rescue Acme::Client::Error
  puts "client-err"
rescue Acme::Error
  puts "acme-err"
end
`
	if got := runSrc(t, src); got != "acme-err" {
		t.Fatalf("transport error got=%q want acme-err", got)
	}
}

// TestACMEChallengeErrorAndArgErrors covers a failed challenge (status invalid +
// mapped #error), the finalize wrong-csr ArgumentError, the new_order and
// CertificateRequest missing-argument errors and the missing :directory error.
func TestACMEChallengeErrorAndArgErrors(t *testing.T) {
	m := newACMEMock(t)
	m.failChallenge = true
	installACMESeam(t, m)
	src := fmt.Sprintf(`
require "acme"
r = []
begin
  Acme::Client.new
rescue ArgumentError
  r << "no-dir"
end
client = Acme::Client.new(directory: %q)
client.new_account
order = client.new_order(identifiers: ["example.com"])
chal = order.authorizations[0].http01
chal.request_validation
r << chal.status
r << (chal.error.nil? ? "nil" : "err")
begin
  order.finalize(csr: "not-a-csr")
rescue ArgumentError
  r << "bad-csr"
end
begin
  client.new_order(identifiers: [])
rescue ArgumentError
  r << "no-ids"
end
begin
  Acme::Client::CertificateRequest.new(names: [])
rescue ArgumentError
  r << "no-names"
end
puts r.join("|")
`, m.url("/directory"))
	if got, want := runSrc(t, src), "no-dir|invalid|err|bad-csr|no-ids|no-names"; got != want {
		t.Fatalf("errors got=%q want=%q", got, want)
	}
}

// TestACMENewOrderError covers new_order's mapError branch (a malformed reply).
func TestACMENewOrderError(t *testing.T) {
	m := newACMEMock(t)
	m.failNewOrder = true
	installACMESeam(t, m)
	src := fmt.Sprintf(`
require "acme"
client = Acme::Client.new(directory: %q)
client.new_account
begin
  client.new_order(identifiers: ["example.com"])
  puts "no-raise"
rescue Acme::Client::Error::Malformed
  puts "malformed"
end
`, m.url("/directory"))
	if got := runSrc(t, src); got != "malformed" {
		t.Fatalf("new_order error got=%q", got)
	}
}

// TestACMECertNotReady covers certificate's not-ready error path (order stays
// pending with no certificate URL), surfaced as Acme::Error.
func TestACMECertNotReady(t *testing.T) {
	m := newACMEMock(t)
	m.challengeStatus = "pending"
	m.orderNoCertURL = true
	installACMESeam(t, m)
	src := fmt.Sprintf(`
require "acme"
client = Acme::Client.new(directory: %q)
client.new_account
order = client.new_order(identifiers: ["example.com"])
begin
  order.certificate
  puts "no-raise"
rescue Acme::Client::Error
  puts "not-ready"
end
`, m.url("/directory"))
	if got := runSrc(t, src); got != "not-ready" {
		t.Fatalf("cert not-ready got=%q", got)
	}
}

// TestACMEKeySources covers acmeAccountKey across every PEM branch (PKCS#8 EC,
// SEC1 EC, PKCS#1 RSA), the non-PEM and unrecognised-DER errors, the PKCS#8
// non-signer (X25519) error, and the ed25519 key that parses yet fails
// key_authorization (an unsupported JWS key).
func TestACMEKeySources(t *testing.T) {
	m := newACMEMock(t)
	installACMESeam(t, m)

	pkcs8EC := acmePEM(t, "PRIVATE KEY", func() ([]byte, error) {
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		return x509.MarshalPKCS8PrivateKey(k)
	})
	sec1EC := acmePEM(t, "EC PRIVATE KEY", func() ([]byte, error) {
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		return x509.MarshalECPrivateKey(k)
	})
	pkcs1RSA := acmePEM(t, "RSA PRIVATE KEY", func() ([]byte, error) {
		k, _ := rsa.GenerateKey(rand.Reader, 2048)
		return x509.MarshalPKCS1PrivateKey(k), nil
	})
	x25519 := acmePEM(t, "PRIVATE KEY", func() ([]byte, error) {
		k, _ := ecdh.X25519().GenerateKey(rand.Reader)
		return x509.MarshalPKCS8PrivateKey(k)
	})
	garbage := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not-der")}))

	// The good keys build a working client (round-trips new_account).
	for _, keyPEM := range []string{pkcs8EC, sec1EC, pkcs1RSA} {
		src := fmt.Sprintf(`
require "acme"
client = Acme::Client.new(directory: %q, private_key: %q)
puts client.new_account["status"]
`, m.url("/directory"), keyPEM)
		if got := runSrc(t, src); got != "valid" {
			t.Fatalf("key source: got=%q want valid", got)
		}
	}

	// The bad keys raise Acme::Error at construction.
	for name, keyPEM := range map[string]string{"non-pem": "nope", "unrecognised": garbage, "x25519": x25519} {
		src := fmt.Sprintf(`
require "acme"
begin
  Acme::Client.new(directory: %q, private_key: %q)
  puts "no-raise"
rescue Acme::Error
  puts "key-err"
end
`, m.url("/directory"), keyPEM)
		if got := runSrc(t, src); got != "key-err" {
			t.Fatalf("%s: got=%q want key-err", name, got)
		}
	}
}

// TestACMEGenerateKeyErrors covers the generation-failure branches of the account
// key (auto-generated) and the certificate-request key via the key seam.
func TestACMEGenerateKeyErrors(t *testing.T) {
	m := newACMEMock(t)
	installACMESeam(t, m)
	prev := acmeGenerateKey
	acmeGenerateKey = func() (crypto.Signer, error) { return nil, errors.New("no entropy") }
	t.Cleanup(func() { acmeGenerateKey = prev })
	src := fmt.Sprintf(`
require "acme"
r = []
begin
  Acme::Client.new(directory: %q)
rescue Acme::Error
  r << "acct-key-err"
end
begin
  Acme::Client::CertificateRequest.new(names: ["example.com"])
rescue Acme::Error
  r << "csr-key-err"
end
puts r.join("|")
`, m.url("/directory"))
	if got, want := runSrc(t, src), "acct-key-err|csr-key-err"; got != want {
		t.Fatalf("gen-key errors got=%q want=%q", got, want)
	}
}

// TestACMEProductionSeam covers newACMEClient's nil-seam (production) branch:
// with no transport seam installed a client still constructs (no request is made
// at construction time).
func TestACMEProductionSeam(t *testing.T) {
	prev := acmeTestOptions
	acmeTestOptions = nil
	t.Cleanup(func() { acmeTestOptions = prev })
	src := `
require "acme"
client = Acme::Client.new(directory: "https://acme-v02.api.example/directory")
puts client.class
`
	if got := runSrc(t, src); got != "Acme::Client" {
		t.Fatalf("production seam got=%q", got)
	}
}

// TestACMECommonNamePrepend covers CertificateRequest.new's common_name branch,
// which prepends the CN to the names list.
func TestACMECommonNamePrepend(t *testing.T) {
	m := newACMEMock(t)
	installACMESeam(t, m)
	src := `
require "acme"
csr = Acme::Client::CertificateRequest.new(common_name: "example.com", names: ["www.example.com"])
puts csr.names.join(",")
`
	if got := runSrc(t, src); got != "example.com,www.example.com" {
		t.Fatalf("common_name prepend got=%q", got)
	}
}

// TestACMEMethodErrorPaths covers the mapError arm of each order / authorization
// / challenge method by injecting an endpoint failure, each surfacing an
// Acme::Client::Error.
func TestACMEMethodErrorPaths(t *testing.T) {
	cases := []struct {
		name  string
		setup func(m *acmeMock)
		body  string
	}{
		{"account", func(m *acmeMock) { m.failAccount = http.StatusBadRequest; m.accountProb = acmeURNPrefix + "malformed" },
			`client.account`},
		{"reload", func(m *acmeMock) { m.failOrderGet = true },
			`client.new_account; client.new_order(identifiers: ["example.com"]).reload`},
		{"authorizations", func(m *acmeMock) { m.failAuthz = true },
			`client.new_account; client.new_order(identifiers: ["example.com"]).authorizations`},
		{"finalize", func(m *acmeMock) { m.failFinalize = true },
			`client.new_account
o = client.new_order(identifiers: ["example.com"])
o.finalize(csr: Acme::Client::CertificateRequest.new(names: ["example.com"]))`},
		{"request_validation", func(m *acmeMock) { m.failChallReq = true },
			`client.new_account
o = client.new_order(identifiers: ["example.com"])
o.authorizations[0].http01.request_validation`},
		{"challenge_reload", func(m *acmeMock) { m.failChallReq = true },
			`client.new_account
o = client.new_order(identifiers: ["example.com"])
o.authorizations[0].http01.reload`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newACMEMock(t)
			tc.setup(m)
			installACMESeam(t, m)
			src := fmt.Sprintf(`
require "acme"
client = Acme::Client.new(directory: %q)
begin
  %s
  puts "no-raise"
rescue Acme::Client::Error
  puts "err"
end
`, m.url("/directory"), tc.body)
			if got := runSrc(t, src); got != "err" {
				t.Fatalf("%s: got=%q want err", tc.name, got)
			}
		})
	}
}

// TestACMEValueStringify covers the ToS / Inspect / Truthy methods of every Acme
// value wrapper (client, order, authorization, challenge, certificate request).
func TestACMEValueStringify(t *testing.T) {
	m := newACMEMock(t)
	installACMESeam(t, m)
	src := fmt.Sprintf(`
require "acme"
client = Acme::Client.new(directory: %q)
client.new_account
order = client.new_order(identifiers: ["example.com"])
az = order.authorizations[0]
chal = az.http01
csr = Acme::Client::CertificateRequest.new(names: ["example.com"])
r = []
[client, order, az, chal, csr].each do |v|
  r << (v ? "t" : "f")   # Truthy
  r << v.to_s            # ToS
  r << v.inspect         # Inspect
end
puts r.join("|")
`, m.url("/directory"))
	got := runSrc(t, src)
	for _, want := range []string{
		"#<Acme::Client>", "#<Acme::Client::Order url=", "#<Acme::Client::Authorization domain=example.com>",
		"#<Acme::Client::Challenge type=http-01>", "#<Acme::Client::CertificateRequest>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stringify output %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "|f|") || strings.HasPrefix(got, "f|") {
		t.Fatalf("a value was falsy: %q", got)
	}
}

// TestACMEStringKey covers acmeKeyName's String-key arm: a keyword passed with a
// String (rather than Symbol) key still resolves.
func TestACMEStringKey(t *testing.T) {
	m := newACMEMock(t)
	installACMESeam(t, m)
	src := fmt.Sprintf(`
require "acme"
client = Acme::Client.new("directory" => %q)
puts client.new_account("contact" => ["mailto:a@b.c"])["status"]
`, m.url("/directory"))
	if got := runSrc(t, src); got != "valid" {
		t.Fatalf("string-key got=%q want valid", got)
	}
}

// acmePEM builds a PEM block of the given type from a DER-producing function.
func acmePEM(t *testing.T, typ string, der func() ([]byte, error)) string {
	t.Helper()
	b, err := der()
	if err != nil {
		t.Fatalf("marshal %s: %v", typ, err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b}))
}
