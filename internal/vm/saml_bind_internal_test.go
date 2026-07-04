// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"

	saml "github.com/go-ruby-saml/saml"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// SAML fixture identifiers shared by the binding tests, mirroring the ruby-saml
// example values.
const (
	samlTestIssuer       = "https://idp.example.com/metadata"
	samlTestAudience     = "https://sp.example.com/metadata"
	samlTestACS          = "https://sp.example.com/acs"
	samlTestInResponseTo = "_req123"
	samlTestNameID       = "user@example.com"
)

// samlSigningKeyStore is a goxmldsig keystore over a freshly generated RSA key
// and its self-signed certificate, used to sign the fixture assertion.
type samlSigningKeyStore struct {
	key     *rsa.PrivateKey
	certDER []byte
}

func (k samlSigningKeyStore) GetKeyPair() (*rsa.PrivateKey, []byte, error) {
	return k.key, k.certDER, nil
}

// samlGenerateKeyStore generates an RSA key pair and a self-signed certificate,
// returning the goxmldsig keystore and the certificate PEM the IdP publishes.
func samlGenerateKeyStore(t *testing.T) (samlSigningKeyStore, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "saml-test-idp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(87600 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	return samlSigningKeyStore{key: key, certDER: certDER}, certPEM
}

// samlBuildSignedResponse renders a saml:Assertion (valid Conditions window
// spanning the real wall clock, an email/roles AttributeStatement and an
// AuthnStatement), signs it enveloped with the IdP key, wraps it in a
// samlp:Response and returns the base64 payload plus the IdP certificate PEM. It
// runs fully in-process — no network, no external fixtures.
func samlBuildSignedResponse(t *testing.T, tamper bool) (string, string) {
	t.Helper()
	ks, certPEM := samlGenerateKeyStore(t)
	now := time.Now().UTC()
	notBefore := now.Add(-time.Hour).Format(time.RFC3339)
	notOnOrAfter := now.Add(87600 * time.Hour).Format(time.RFC3339)
	instant := now.Format(time.RFC3339)

	assertionXML := fmt.Sprintf(`<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_assert123" Version="2.0" IssueInstant="%s">`+
		`<saml:Issuer>%s</saml:Issuer>`+
		`<saml:Subject><saml:NameID Format="urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress">%s</saml:NameID></saml:Subject>`+
		`<saml:Conditions NotBefore="%s" NotOnOrAfter="%s">`+
		`<saml:AudienceRestriction><saml:Audience>%s</saml:Audience></saml:AudienceRestriction>`+
		`</saml:Conditions>`+
		`<saml:AuthnStatement SessionIndex="_session789" AuthnInstant="%s"><saml:AuthnContext><saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:Password</saml:AuthnContextClassRef></saml:AuthnContext></saml:AuthnStatement>`+
		`<saml:AttributeStatement>`+
		`<saml:Attribute Name="email"><saml:AttributeValue>%s</saml:AttributeValue></saml:Attribute>`+
		`<saml:Attribute Name="roles"><saml:AttributeValue>admin</saml:AttributeValue><saml:AttributeValue>user</saml:AttributeValue></saml:Attribute>`+
		`</saml:AttributeStatement>`+
		`</saml:Assertion>`,
		instant, samlTestIssuer, samlTestNameID, notBefore, notOnOrAfter, samlTestAudience, instant, samlTestNameID)

	adoc := etree.NewDocument()
	if err := adoc.ReadFromString(assertionXML); err != nil {
		t.Fatalf("parse assertion: %v", err)
	}
	signer := dsig.NewDefaultSigningContext(ks)
	signed, err := signer.SignEnveloped(adoc.Root())
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}
	if tamper {
		if nid := signed.FindElement("./Subject/NameID"); nid != nil {
			nid.SetText("attacker@evil.example.com")
		}
	}
	sdoc := etree.NewDocument()
	sdoc.SetRoot(signed.Copy())
	assertionStr, err := sdoc.WriteToString()
	if err != nil {
		t.Fatalf("serialize assertion: %v", err)
	}

	respXML := fmt.Sprintf(`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_resp456" Version="2.0" IssueInstant="%s" Destination="%s" InResponseTo="%s">`+
		`<saml:Issuer>%s</saml:Issuer>`+
		`<samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status>`+
		`%s</samlp:Response>`,
		instant, samlTestACS, samlTestInResponseTo, samlTestIssuer, assertionStr)

	return base64.StdEncoding.EncodeToString([]byte(respXML)), certPEM
}

// rubyLit renders s as a double-quoted Ruby string literal, so a multi-line PEM
// certificate can be embedded verbatim in the test's Ruby source.
func rubyLit(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n")
	return `"` + r.Replace(s) + `"`
}

// samlSettingsSrc emits the Ruby that constructs a SAML::Settings for the fixture
// IdP certificate, seeded through the keyword-hash constructor.
func samlSettingsSrc(certPEM string) string {
	return fmt.Sprintf(`SAML::Settings.new(
  idp_cert: %s,
  idp_entity_id: %q,
  sp_entity_id: %q,
  assertion_consumer_service_url: %q,
  name_identifier_format: SAML::NAMEID_EMAIL_ADDRESS)`,
		rubyLit(certPEM), samlTestIssuer, samlTestAudience, samlTestACS)
}

// TestSAMLResponseRoundTrip drives the core SP flow through rbgo: a signed
// assertion is parsed, validated (signature + Conditions + audience +
// destination + InResponseTo + issuer), and its NameID, attributes, session
// index, status, issuers, destination and in-response-to are read back.
func TestSAMLResponseRoundTrip(t *testing.T) {
	b64, certPEM := samlBuildSignedResponse(t, false)
	src := fmt.Sprintf(`
require "saml"
settings = %s
resp = SAML::Response.new(%q, settings)
resp.expected_in_response_to = %q
r = []
r << (resp.is_valid? ? "valid" : "invalid:#{resp.errors.join(",")}")
r << (resp.validate.nil? ? "validate-nil" : "validate-nonnil")
r << resp.name_id
r << resp.nameid
r << resp.attributes["email"].join(",")
r << resp.attributes["roles"].join(",")
r << resp.session_index
r << (resp.status_code.end_with?("Success") ? "success" : resp.status_code)
r << resp.issuers.join(",")
r << resp.destination
r << resp.in_response_to
puts r.join("|")
`, samlSettingsSrc(certPEM), b64, samlTestInResponseTo)
	want := strings.Join([]string{
		"valid", "validate-nil", samlTestNameID, samlTestNameID,
		samlTestNameID, "admin,user", "_session789", "success",
		samlTestIssuer, samlTestACS, samlTestInResponseTo,
	}, "|")
	if got := runSrc(t, src); got != want {
		t.Fatalf("saml round-trip =\n%q\nwant\n%q", got, want)
	}
}

// TestSAMLResponseInvalid drives the failure paths: a tampered assertion is
// is_valid? == false with a populated errors array, and #validate raises
// SAML::ValidationError (a SAML::Error, a StandardError).
func TestSAMLResponseInvalid(t *testing.T) {
	b64, certPEM := samlBuildSignedResponse(t, true)
	src := fmt.Sprintf(`
require "ruby-saml"
settings = %s
resp = OneLogin::RubySaml::Response.new(%q, settings: settings)
r = []
r << (resp.is_valid? ? "valid" : "invalid")
r << (resp.errors.empty? ? "no-errors" : "has-errors")
begin
  resp.validate
rescue SAML::ValidationError => e
  r << (e.is_a?(SAML::Error) && e.is_a?(StandardError) ? "validation-error" : "wrong-class")
end
puts r.join("|")
`, samlSettingsSrc(certPEM), b64)
	if got := runSrc(t, src); got != "invalid|has-errors|validation-error" {
		t.Fatalf("saml invalid = %q", got)
	}
}

// TestSAMLAuthrequest drives SP-initiated SSO: #create returns the IdP redirect
// URL carrying a SAMLRequest that #decode_saml_request round-trips to
// AuthnRequest XML, #uuid exposes the request ID, and a RelayState is carried
// when supplied.
func TestSAMLAuthrequest(t *testing.T) {
	src := `
require "saml"
settings = SAML::Settings.new(
  idp_sso_target_url: "https://idp.example.com/sso",
  sp_entity_id: "https://sp.example.com/metadata",
  assertion_consumer_service_url: "https://sp.example.com/acs")
authn = SAML::Authrequest.new
url = authn.create(settings)
r = []
r << (url.start_with?("https://idp.example.com/sso?SAMLRequest=") ? "url" : url)
r << (authn.uuid.start_with?("_") || !authn.uuid.empty? ? "uuid" : "no-uuid")
enc = url.split("SAMLRequest=")[1]
require "cgi"
xml = SAML.decode_saml_request(CGI.unescape(enc))
r << (xml.include?("AuthnRequest") ? "decoded" : "bad")
url2 = authn.create(settings, "return-to-here")
r << (url2.include?("RelayState=return-to-here") ? "relay" : "no-relay")
puts r.join("|")
`
	if got := runSrc(t, src); got != "url|uuid|decoded|relay" {
		t.Fatalf("saml authrequest = %q", got)
	}
}

// TestSAMLMetadata drives SP metadata generation and IdP metadata parsing: the
// generated EntityDescriptor round-trips through IdpMetadataParser#parse... using
// an IdP descriptor, and the parsed Settings carry the IdP fields.
func TestSAMLMetadata(t *testing.T) {
	src := `
require "saml"
settings = SAML::Settings.new(
  sp_entity_id: "https://sp.example.com/metadata",
  assertion_consumer_service_url: "https://sp.example.com/acs",
  name_identifier_format: SAML::NAMEID_EMAIL_ADDRESS,
  want_assertions_signed: true)
md = SAML::Metadata.new.generate(settings)
r = []
r << (md.include?("SPSSODescriptor") && md.include?("https://sp.example.com/acs") ? "sp-md" : "bad-md")
idp_xml = %q{<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com/metadata"><IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol"><SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/sso"/></IDPSSODescriptor></EntityDescriptor>}
parsed = SAML::IdpMetadataParser.new.parse(idp_xml)
r << parsed.idp_entity_id
r << parsed.idp_sso_target_url
puts r.join("|")
`
	want := "sp-md|https://idp.example.com/metadata|https://idp.example.com/sso"
	if got := runSrc(t, src); got != want {
		t.Fatalf("saml metadata = %q want %q", got, want)
	}
}

// TestSAMLLogout drives the two Single-Logout builders: Logoutrequest#create and
// SloLogoutresponse#create both return an IdP SLO redirect URL carrying a
// SAMLRequest/SAMLResponse, and each exposes its #uuid.
func TestSAMLLogout(t *testing.T) {
	src := `
require "saml"
settings = SAML::Settings.new(
  idp_slo_target_url: "https://idp.example.com/slo",
  sp_entity_id: "https://sp.example.com/metadata")
lr = SAML::Logoutrequest.new
url = lr.create(settings, "user@example.com", "back")
r = []
r << (url.start_with?("https://idp.example.com/slo?") && url.include?("SAMLRequest=") ? "req" : url)
r << (url.include?("RelayState=back") ? "req-relay" : "no-relay")
r << (lr.uuid.empty? ? "no-uuid" : "req-uuid")
sr = SAML::SloLogoutresponse.new
url2 = sr.create(settings, "_inrequest")
r << (url2.include?("SAMLResponse=") ? "resp" : url2)
r << (sr.uuid.empty? ? "no-uuid" : "resp-uuid")
puts r.join("|")
`
	if got := runSrc(t, src); got != "req|req-relay|req-uuid|resp|resp-uuid" {
		t.Fatalf("saml logout = %q", got)
	}
}

// TestSAMLSettingsAttributes exercises every Settings accessor: the string
// readers/writers, the boolean want_assertions_signed and the numeric
// allowed_clock_drift (Integer and Float seconds).
func TestSAMLSettingsAttributes(t *testing.T) {
	src := `
require "saml"
s = SAML::Settings.new
s.idp_entity_id = "e"
s.idp_sso_target_url = "sso"
s.idp_slo_target_url = "slo"
s.idp_cert = "cert"
s.idp_cert_fingerprint = "fp"
s.idp_cert_fingerprint_algorithm = "sha256"
s.sp_entity_id = "sp"
s.assertion_consumer_service_url = "acs"
s.name_identifier_format = "fmt"
s.certificate = "c"
s.private_key = "k"
s.signature_method = "sig"
s.digest_method = "dig"
s.audience = "aud"
s.want_assertions_signed = true
s.allowed_clock_drift = 30
r = []
r << s.idp_entity_id << s.idp_sso_target_url << s.idp_slo_target_url
r << s.idp_cert << s.idp_cert_fingerprint << s.idp_cert_fingerprint_algorithm
r << s.sp_entity_id << s.assertion_consumer_service_url << s.name_identifier_format
r << s.certificate << s.private_key << s.signature_method << s.digest_method << s.audience
r << s.want_assertions_signed.to_s
r << s.allowed_clock_drift.to_s
s.allowed_clock_drift = 1.5
r << s.allowed_clock_drift.to_s
puts r.join("|")
`
	want := "e|sso|slo|cert|fp|sha256|sp|acs|fmt|c|k|sig|dig|aud|true|30.0|1.5"
	if got := runSrc(t, src); got != want {
		t.Fatalf("saml settings = %q want %q", got, want)
	}
}

// TestSAMLErrors covers the binding's error and argument paths: a malformed
// SAMLRequest, response, metadata XML and certificate PEM each raise SAML::Error;
// a non-Settings builder argument and a missing/`settings:`-less Response
// argument raise ArgumentError; and a non-numeric clock drift raises TypeError.
func TestSAMLErrors(t *testing.T) {
	src := `
require "saml"
r = []
begin
  SAML.decode_saml_request("@@@not-base64@@@")
rescue SAML::Error
  r << "decode"
end
settings = SAML::Settings.new(sp_entity_id: "sp")
begin
  SAML::Response.new("@@@not-base64@@@", settings)
rescue SAML::Error
  r << "response"
end
begin
  SAML::IdpMetadataParser.new.parse("<not-metadata")
rescue SAML::Error
  r << "parse"
end
begin
  SAML::Metadata.new.generate(SAML::Settings.new(certificate: "not a pem"))
rescue SAML::Error
  r << "metadata"
end
begin
  SAML::Authrequest.new.create(SAML::Settings.new(idp_sso_target_url: ":"))
rescue SAML::Error
  r << "create-url"
end
begin
  SAML::Logoutrequest.new.create(SAML::Settings.new(idp_slo_target_url: ":"), "u")
rescue SAML::Error
  r << "slo-url"
end
begin
  SAML::SloLogoutresponse.new.create(SAML::Settings.new(idp_slo_target_url: ":"), "id")
rescue SAML::Error
  r << "slo-resp-url"
end
begin
  SAML::Authrequest.new.create("not settings")
rescue ArgumentError
  r << "not-settings"
end
begin
  SAML::Response.new("<x/>", settings: 99)
rescue ArgumentError
  r << "kw-bad"
end
begin
  SAML::Response.new("<x/>", other: 1)
rescue ArgumentError
  r << "kw-missing"
end
begin
  SAML::Response.new("<x/>")
rescue ArgumentError
  r << "no-settings"
end
begin
  SAML::Settings.new.allowed_clock_drift = "nope"
rescue TypeError
  r << "drift-type"
end
puts r.join("|")
`
	want := "decode|response|parse|metadata|create-url|slo-url|slo-resp-url|not-settings|kw-bad|kw-missing|no-settings|drift-type"
	if got := runSrc(t, src); got != want {
		t.Fatalf("saml errors = %q want %q", got, want)
	}
}

// TestSAMLRaiseError maps a *saml.ValidationError onto SAML::ValidationError and
// every other error onto the SAML::Error base, covering both arms of
// raiseSAMLError directly.
func TestSAMLRaiseError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&saml.ValidationError{Message: "bad"}, "SAML::ValidationError"},
		{errors.New("boom"), "SAML::Error"},
	}
	for _, c := range cases {
		func() {
			defer func() {
				re, ok := recover().(RubyError)
				if !ok {
					t.Fatalf("raiseSAMLError(%v): did not raise a RubyError", c.err)
				}
				if re.Class != c.want {
					t.Errorf("raiseSAMLError(%v): class %q want %q", c.err, re.Class, c.want)
				}
			}()
			raiseSAMLError(c.err)
		}()
	}
}

// TestSAMLValueMethods covers the object.Value surface (Truthy / ToS / Inspect)
// of every SAML wrapper type, reached directly rather than through dispatch.
func TestSAMLValueMethods(t *testing.T) {
	values := []struct {
		v   object.Value
		tos string
	}{
		{&SAMLSettings{}, "#<SAML::Settings>"},
		{&SAMLAuthrequest{}, "#<SAML::Authrequest>"},
		{&SAMLResponse{}, "#<SAML::Response>"},
		{&SAMLMetadata{}, "#<SAML::Metadata>"},
		{&SAMLIdpMetadataParser{}, "#<SAML::IdpMetadataParser>"},
		{&SAMLLogoutrequest{}, "#<SAML::Logoutrequest>"},
		{&SAMLSloLogoutresponse{}, "#<SAML::SloLogoutresponse>"},
	}
	for _, c := range values {
		if !c.v.Truthy() {
			t.Errorf("%T should be truthy", c.v)
		}
		if c.v.ToS() != c.tos {
			t.Errorf("%T ToS = %q want %q", c.v, c.v.ToS(), c.tos)
		}
		if c.v.Inspect() != c.tos {
			t.Errorf("%T Inspect = %q want %q", c.v, c.v.Inspect(), c.tos)
		}
	}
}

// TestSAMLClassOf proves each wrapper type reports its bound class through the
// interpreter's classOf dispatch basis.
func TestSAMLClassOf(t *testing.T) {
	vm := New(&bytes.Buffer{})
	mod := vm.consts["SAML"].(*RClass)
	cls := func(name string) *RClass { return mod.consts[name].(*RClass) }
	pairs := []struct {
		v    object.Value
		want *RClass
	}{
		{&SAMLSettings{cls: cls("Settings")}, cls("Settings")},
		{&SAMLAuthrequest{cls: cls("Authrequest")}, cls("Authrequest")},
		{&SAMLResponse{cls: cls("Response")}, cls("Response")},
		{&SAMLMetadata{cls: cls("Metadata")}, cls("Metadata")},
		{&SAMLIdpMetadataParser{cls: cls("IdpMetadataParser")}, cls("IdpMetadataParser")},
		{&SAMLLogoutrequest{cls: cls("Logoutrequest")}, cls("Logoutrequest")},
		{&SAMLSloLogoutresponse{cls: cls("SloLogoutresponse")}, cls("SloLogoutresponse")},
	}
	for _, p := range pairs {
		if got := vm.classOf(p.v); got != p.want {
			t.Errorf("classOf(%T) = %v want %v", p.v, got, p.want)
		}
	}
	// The OneLogin::RubySaml alias resolves to the same class objects.
	one := vm.consts["OneLogin"].(*RClass).consts["RubySaml"].(*RClass)
	if one != mod {
		t.Error("OneLogin::RubySaml should alias the SAML module")
	}
}
