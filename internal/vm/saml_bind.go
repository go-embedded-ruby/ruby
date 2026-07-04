// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	saml "github.com/go-ruby-saml/saml"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent SAML core of github.com/go-ruby-saml/saml — a pure-Go
// (no-cgo), MRI-faithful port of Ruby's ruby-saml gem (OneLogin::RubySaml) that
// stands on github.com/crewjam/saml for the SAML schema types and
// github.com/russellhaering/goxmldsig for XML digital-signature validation. It
// carries the instance value types the SAML module wraps — a Settings bag, the
// Authrequest / Response / Metadata / IdpMetadataParser objects and the two
// Single-Logout builders — plus the argument conversions (settings resolution,
// attribute-map and string-slice bridges) and the error bridge that re-raises a
// library *saml.ValidationError as SAML::ValidationError and every other failure
// as SAML::Error. All SAML work is delegated to go-ruby-saml. See saml.go for
// the module wiring.

// SAMLSettings is an instance of SAML::Settings (OneLogin::RubySaml::Settings):
// the SP and IdP configuration bag that drives request generation and response
// validation. Its attribute readers/writers project directly onto the wrapped
// *saml.Settings.
type SAMLSettings struct {
	cls *RClass
	s   *saml.Settings
}

func (s *SAMLSettings) ToS() string     { return "#<SAML::Settings>" }
func (s *SAMLSettings) Inspect() string { return s.ToS() }
func (s *SAMLSettings) Truthy() bool    { return true }

// SAMLAuthrequest is an instance of SAML::Authrequest
// (OneLogin::RubySaml::Authrequest): the builder for an SP-initiated SSO
// AuthnRequest. Its #uuid reader exposes the ID of the most recently created
// request, for InResponseTo correlation.
type SAMLAuthrequest struct {
	cls *RClass
	a   *saml.Authrequest
}

func (a *SAMLAuthrequest) ToS() string     { return "#<SAML::Authrequest>" }
func (a *SAMLAuthrequest) Inspect() string { return a.ToS() }
func (a *SAMLAuthrequest) Truthy() bool    { return true }

// SAMLResponse is an instance of SAML::Response
// (OneLogin::RubySaml::Response): a parsed, validatable SAML 2.0 authentication
// response received at the SP Assertion Consumer Service.
type SAMLResponse struct {
	cls *RClass
	r   *saml.Response
}

func (r *SAMLResponse) ToS() string     { return "#<SAML::Response>" }
func (r *SAMLResponse) Inspect() string { return r.ToS() }
func (r *SAMLResponse) Truthy() bool    { return true }

// SAMLMetadata is an instance of SAML::Metadata
// (OneLogin::RubySaml::Metadata): the SP metadata generator.
type SAMLMetadata struct {
	cls *RClass
	m   saml.Metadata
}

func (m *SAMLMetadata) ToS() string     { return "#<SAML::Metadata>" }
func (m *SAMLMetadata) Inspect() string { return m.ToS() }
func (m *SAMLMetadata) Truthy() bool    { return true }

// SAMLIdpMetadataParser is an instance of SAML::IdpMetadataParser
// (OneLogin::RubySaml::IdpMetadataParser): the ingester that turns IdP metadata
// XML into a SAML::Settings.
type SAMLIdpMetadataParser struct {
	cls *RClass
	p   saml.IdpMetadataParser
}

func (p *SAMLIdpMetadataParser) ToS() string     { return "#<SAML::IdpMetadataParser>" }
func (p *SAMLIdpMetadataParser) Inspect() string { return p.ToS() }
func (p *SAMLIdpMetadataParser) Truthy() bool    { return true }

// SAMLLogoutrequest is an instance of SAML::Logoutrequest
// (OneLogin::RubySaml::Logoutrequest): the builder for an SP-initiated Single
// Logout request.
type SAMLLogoutrequest struct {
	cls *RClass
	l   *saml.Logoutrequest
}

func (l *SAMLLogoutrequest) ToS() string     { return "#<SAML::Logoutrequest>" }
func (l *SAMLLogoutrequest) Inspect() string { return l.ToS() }
func (l *SAMLLogoutrequest) Truthy() bool    { return true }

// SAMLSloLogoutresponse is an instance of SAML::SloLogoutresponse
// (OneLogin::RubySaml::SloLogoutresponse): the builder for the SP's response to
// an IdP-initiated logout request.
type SAMLSloLogoutresponse struct {
	cls *RClass
	l   *saml.SloLogoutresponse
}

func (l *SAMLSloLogoutresponse) ToS() string     { return "#<SAML::SloLogoutresponse>" }
func (l *SAMLSloLogoutresponse) Inspect() string { return l.ToS() }
func (l *SAMLSloLogoutresponse) Truthy() bool    { return true }

// samlSettings coerces a positional settings argument to the wrapped
// *saml.Settings, raising ArgumentError when it is anything other than a
// SAML::Settings — the object every builder and Response consumes.
func samlSettings(v object.Value) *saml.Settings {
	s, ok := v.(*SAMLSettings)
	if !ok {
		raise("ArgumentError", "expected a SAML::Settings, got %s", v.Inspect())
	}
	return s.s
}

// samlSettingsFrom resolves the SAML::Settings a Response is built against. It
// accepts either a positional SAML::Settings or a trailing `settings:` keyword
// (mirroring ruby-saml's `Response.new(doc, settings: settings)`), raising
// ArgumentError when neither is supplied.
func samlSettingsFrom(args []object.Value) *saml.Settings {
	for _, a := range args {
		if s, ok := a.(*SAMLSettings); ok {
			return s.s
		}
	}
	if h, ok := trailingHash(args); ok {
		if v, ok := h.Get(object.Symbol("settings")); ok {
			if s, ok := v.(*SAMLSettings); ok {
				return s.s
			}
		}
	}
	raise("ArgumentError", "missing keyword: :settings")
	return nil
}

// samlOptRelayState returns the RelayState argument of a builder call — the
// optional string following the required arguments — defaulting to "" when it
// is omitted, matching the ruby-saml builders' default empty relay state.
func samlOptRelayState(args []object.Value, idx int) string {
	if len(args) > idx {
		return strArg(args[idx])
	}
	return ""
}

// samlStringArray turns a Go string slice (Issuers, Errors) into a Ruby Array of
// String, the shape ruby-saml's array-valued readers return.
func samlStringArray(ss []string) *object.Array {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(elems)
}

// samlAttributesHash turns Response#attributes' multi-valued map into a Ruby
// Hash whose String keys map to an Array of the attribute's String values,
// mirroring ruby-saml's multi-valued OneLogin::RubySaml::Attributes.
func samlAttributesHash(m map[string][]string) *object.Hash {
	h := object.NewHashCap(len(m))
	for k, vs := range m {
		h.Set(object.NewString(k), samlStringArray(vs))
	}
	return h
}

// raiseSAMLError re-raises a go-ruby-saml failure as the matching SAML exception:
// a *saml.ValidationError (the fail-fast validation failure) becomes
// SAML::ValidationError, and every other error becomes the SAML::Error base. It
// never returns (raise panics); it is typed to return any so a caller can write
// `return raiseSAMLError(err)` in a value position and leave no dead code.
func raiseSAMLError(err error) any {
	if ve, ok := err.(*saml.ValidationError); ok {
		raise("SAML::ValidationError", "%s", ve.Error())
	}
	return raise("SAML::Error", "%s", err.Error())
}
