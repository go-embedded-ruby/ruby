// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	saml "github.com/go-ruby-saml/saml"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerSAML installs the SAML module (require "saml" / require "ruby-saml"):
// the OneLogin::RubySaml-flavoured SP toolkit — Settings, Authrequest, Response,
// Metadata, IdpMetadataParser and the two Single-Logout builders — plus the
// binding/format/algorithm constants and the SAML::Error / SAML::ValidationError
// exception tree. All SAML work (AuthnRequest generation, XML-signature and
// Conditions validation, metadata) is delegated to github.com/go-ruby-saml/saml,
// a pure-Go (no-cgo) port of Ruby's ruby-saml gem, so a document rbgo produces or
// consumes is interoperable with the reference toolkit. The module is registered
// under both the short SAML name and the ruby-saml-faithful OneLogin::RubySaml
// path (the same class objects). The instance value types and conversions live
// in saml_bind.go.
func (vm *VM) registerSAML() {
	mod := newClass("SAML", nil)
	mod.isModule = true
	vm.consts["SAML"] = mod

	// Also expose the classes under the ruby-saml OneLogin::RubySaml path, so
	// `require "ruby-saml"` code that names OneLogin::RubySaml::Settings resolves
	// to the very same class objects.
	one := newClass("OneLogin", nil)
	one.isModule = true
	one.consts["RubySaml"] = mod
	vm.consts["OneLogin"] = one

	vm.registerSAMLErrors(mod)
	vm.registerSAMLConstants(mod)
	vm.registerSAMLSettings(mod)
	vm.registerSAMLAuthrequest(mod)
	vm.registerSAMLResponse(mod)
	vm.registerSAMLMetadata(mod)
	vm.registerSAMLLogout(mod)

	// SAML.decode_saml_request(encoded) reverses a SAMLRequest (base64 +
	// inflate), returning the AuthnRequest XML; a malformed payload raises
	// SAML::Error, mirroring DecodeSAMLRequest.
	mod.smethods["decode_saml_request"] = &Method{name: "decode_saml_request", owner: mod,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			xml, err := saml.DecodeSAMLRequest(strArg(args[0]))
			if err != nil {
				raiseSAMLError(err)
			}
			return object.NewStringBytes(xml)
		}}
}

// registerSAMLErrors installs the SAML exception tree mirroring the ruby-saml
// gem: SAML::Error < StandardError and SAML::ValidationError
// (OneLogin::RubySaml::ValidationError) < SAML::Error. Each class is registered
// both as a nested constant of the module and under its qualified name in the
// top-level table, so a re-raised failure's exception lookup finds the same
// class, exactly as the Age and JSON error trees are.
func (vm *VM) registerSAMLErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Error", "SAML::Error", std)
	reg("ValidationError", "SAML::ValidationError", base)
}

// registerSAMLConstants installs the common SAML binding, name-identifier-format
// and signature/digest-algorithm identifiers as module constants, mirroring the
// constants ruby-saml exposes.
func (vm *VM) registerSAMLConstants(mod *RClass) {
	set := func(name, val string) { mod.consts[name] = object.NewString(val) }
	set("HTTP_REDIRECT", saml.HTTPRedirectBinding)
	set("HTTP_POST", saml.HTTPPostBinding)
	set("NAMEID_EMAIL_ADDRESS", saml.NameIDFormatEmail)
	set("NAMEID_TRANSIENT", saml.NameIDFormatTransient)
	set("NAMEID_PERSISTENT", saml.NameIDFormatPersistent)
	set("NAMEID_UNSPECIFIED", saml.NameIDFormatUnspecified)
	set("RSA_SHA1", saml.SignatureMethodRSASHA1)
	set("RSA_SHA256", saml.SignatureMethodRSASHA256)
	set("SHA1", saml.DigestMethodSHA1)
	set("SHA256", saml.DigestMethodSHA256)
}

// samlAccessor names a SAML::Settings attribute and the get/set projection onto
// the wrapped *saml.Settings that its reader and `name=` writer perform.
type samlAccessor struct {
	name string
	get  func(*saml.Settings) object.Value
	set  func(*saml.Settings, object.Value)
}

// samlStrAccessor builds a String attribute accessor from a function selecting
// the field pointer, so the reader returns the current String and `name=` stores
// a String.
func samlStrAccessor(name string, field func(*saml.Settings) *string) samlAccessor {
	return samlAccessor{name,
		func(s *saml.Settings) object.Value { return object.NewString(*field(s)) },
		func(s *saml.Settings, v object.Value) { *field(s) = strArg(v) }}
}

// samlSettingsAccessors is the full ruby-saml Settings attribute table: the IdP
// and SP string fields, the want_assertions_signed flag and the numeric
// allowed_clock_drift (in seconds), each projecting onto the wrapped
// *saml.Settings.
func samlSettingsAccessors() []samlAccessor {
	return []samlAccessor{
		samlStrAccessor("idp_entity_id", func(s *saml.Settings) *string { return &s.IdPEntityID }),
		samlStrAccessor("idp_sso_target_url", func(s *saml.Settings) *string { return &s.IdPSSOTargetURL }),
		samlStrAccessor("idp_slo_target_url", func(s *saml.Settings) *string { return &s.IdPSLOTargetURL }),
		samlStrAccessor("idp_cert", func(s *saml.Settings) *string { return &s.IdPCert }),
		samlStrAccessor("idp_cert_fingerprint", func(s *saml.Settings) *string { return &s.IdPCertFingerprint }),
		samlStrAccessor("idp_cert_fingerprint_algorithm", func(s *saml.Settings) *string { return &s.IdPCertFingerprintAlgorithm }),
		samlStrAccessor("sp_entity_id", func(s *saml.Settings) *string { return &s.SPEntityID }),
		samlStrAccessor("assertion_consumer_service_url", func(s *saml.Settings) *string { return &s.AssertionConsumerServiceURL }),
		samlStrAccessor("name_identifier_format", func(s *saml.Settings) *string { return &s.NameIdentifierFormat }),
		samlStrAccessor("certificate", func(s *saml.Settings) *string { return &s.Certificate }),
		samlStrAccessor("private_key", func(s *saml.Settings) *string { return &s.PrivateKey }),
		samlStrAccessor("signature_method", func(s *saml.Settings) *string { return &s.SignatureMethod }),
		samlStrAccessor("digest_method", func(s *saml.Settings) *string { return &s.DigestMethod }),
		samlStrAccessor("audience", func(s *saml.Settings) *string { return &s.Audience }),
		{"want_assertions_signed",
			func(s *saml.Settings) object.Value { return object.Bool(s.WantAssertionsSigned) },
			func(s *saml.Settings, v object.Value) { s.WantAssertionsSigned = v.Truthy() }},
		{"allowed_clock_drift",
			func(s *saml.Settings) object.Value { return object.Float(s.AllowedClockDrift.Seconds()) },
			func(s *saml.Settings, v object.Value) {
				s.AllowedClockDrift = time.Duration(samlSeconds(v) * float64(time.Second))
			}},
	}
}

// samlSeconds reads a numeric allowed_clock_drift argument (an Integer or Float
// count of seconds), raising TypeError for anything else.
func samlSeconds(v object.Value) float64 {
	switch n := v.(type) {
	case object.Integer:
		return float64(n)
	case object.Float:
		return float64(n)
	}
	raise("TypeError", "no implicit conversion of %s into a duration", v.Inspect())
	return 0
}

// registerSAMLSettings installs SAML::Settings: `.new` (optionally seeded from a
// keyword Hash of the same attribute names) and the ruby-saml attribute
// readers/writers.
func (vm *VM) registerSAMLSettings(mod *RClass) {
	cls := newClass("SAML::Settings", vm.cObject)
	mod.consts["Settings"] = cls

	accessors := samlSettingsAccessors()

	// SAML::Settings.new(attrs = {}) — a trailing keyword Hash seeds any of the
	// attribute writers, so `Settings.new(idp_cert: pem, sp_entity_id: id)` is
	// equivalent to constructing it blank and assigning each field.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			s := &SAMLSettings{cls: cls, s: &saml.Settings{}}
			if h, ok := trailingHash(args); ok {
				for _, acc := range accessors {
					if v, ok := h.Get(object.Symbol(acc.name)); ok {
						acc.set(s.s, v)
					}
				}
			}
			return s
		}}

	for _, acc := range accessors {
		acc := acc
		cls.define(acc.name, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return acc.get(self.(*SAMLSettings).s)
		})
		cls.define(acc.name+"=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			acc.set(self.(*SAMLSettings).s, args[0])
			return args[0]
		})
	}
}

// registerSAMLAuthrequest installs SAML::Authrequest: `.new`, #create (the full
// IdP redirect URL) and the #uuid reader.
func (vm *VM) registerSAMLAuthrequest(mod *RClass) {
	cls := newClass("SAML::Authrequest", vm.cObject)
	mod.consts["Authrequest"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &SAMLAuthrequest{cls: cls, a: &saml.Authrequest{}}
		}}

	// SAML::Authrequest#create(settings, relay_state = "") → the IdP redirect
	// URL carrying the SAMLRequest (and RelayState when non-empty).
	cls.define("create", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*SAMLAuthrequest)
		url, err := a.a.Create(samlSettings(args[0]), samlOptRelayState(args, 1))
		if err != nil {
			raiseSAMLError(err)
		}
		return object.NewString(url)
	})

	// SAML::Authrequest#uuid → the ID of the most recently created request.
	cls.define("uuid", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*SAMLAuthrequest).a.UUID)
	})
}

// registerSAMLResponse installs SAML::Response: `.new(input, settings)` (or the
// `settings:` keyword), the #is_valid? / #validate validators and the assertion
// readers (#name_id, #attributes, #session_index, #status_code, #issuers,
// #destination, #in_response_to, #errors, #expected_in_response_to=).
func (vm *VM) registerSAMLResponse(mod *RClass) {
	cls := newClass("SAML::Response", vm.cObject)
	mod.consts["Response"] = cls

	// SAML::Response.new(input, settings) — input is the base64-encoded response
	// (HTTP-POST binding) or the raw XML; settings is a SAML::Settings, given
	// positionally or as the `settings:` keyword.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			r, err := saml.NewResponse(strArg(args[0]), samlSettingsFrom(args[1:]))
			if err != nil {
				raiseSAMLError(err)
			}
			return &SAMLResponse{cls: cls, r: r}
		}}

	// SAML::Response#is_valid? → true when the response passes every validation,
	// accumulating each failure into #errors (never raising), mirroring
	// ruby-saml's is_valid?.
	cls.define("is_valid?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*SAMLResponse).r.IsValid())
	})

	// SAML::Response#validate → nil when valid, else raises SAML::ValidationError
	// with the first failure (the fail-fast counterpart of #is_valid?).
	cls.define("validate", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self.(*SAMLResponse).r.Validate(); err != nil {
			raiseSAMLError(err)
		}
		return object.NilV
	})

	// SAML::Response#expected_in_response_to = id — the AuthnRequest ID the
	// response's InResponseTo is validated against (ruby-saml's
	// matches_request_id).
	cls.define("expected_in_response_to=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*SAMLResponse).r.ExpectedInResponseTo = strArg(args[0])
		return args[0]
	})

	samlString := func(name string, f func(*saml.Response) string) {
		cls.define(name, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewString(f(self.(*SAMLResponse).r))
		})
	}
	samlString("name_id", (*saml.Response).NameID)
	samlString("nameid", (*saml.Response).NameID)
	samlString("session_index", (*saml.Response).SessionIndex)
	samlString("status_code", (*saml.Response).StatusCode)
	samlString("destination", (*saml.Response).Destination)
	samlString("in_response_to", (*saml.Response).InResponseTo)

	cls.define("attributes", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return samlAttributesHash(self.(*SAMLResponse).r.Attributes())
	})
	cls.define("issuers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return samlStringArray(self.(*SAMLResponse).r.Issuers())
	})
	cls.define("errors", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return samlStringArray(self.(*SAMLResponse).r.Errors)
	})
}

// registerSAMLMetadata installs SAML::Metadata (#generate → SP metadata XML) and
// SAML::IdpMetadataParser (#parse → a SAML::Settings seeded from IdP metadata).
func (vm *VM) registerSAMLMetadata(mod *RClass) {
	meta := newClass("SAML::Metadata", vm.cObject)
	mod.consts["Metadata"] = meta
	meta.smethods["new"] = &Method{name: "new", owner: meta,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &SAMLMetadata{cls: meta}
		}}
	// SAML::Metadata#generate(settings) → the SP EntityDescriptor XML.
	meta.define("generate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		xml, err := self.(*SAMLMetadata).m.Generate(samlSettings(args[0]))
		if err != nil {
			raiseSAMLError(err)
		}
		return object.NewStringBytes(xml)
	})

	parser := newClass("SAML::IdpMetadataParser", vm.cObject)
	mod.consts["IdpMetadataParser"] = parser
	settingsCls := mod.consts["Settings"].(*RClass)
	parser.smethods["new"] = &Method{name: "new", owner: parser,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &SAMLIdpMetadataParser{cls: parser}
		}}
	// SAML::IdpMetadataParser#parse(metadata_xml) → a SAML::Settings with the
	// IdP fields filled in from the first IDPSSODescriptor.
	parser.define("parse", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self.(*SAMLIdpMetadataParser).p.Parse([]byte(strArg(args[0])))
		if err != nil {
			raiseSAMLError(err)
		}
		return &SAMLSettings{cls: settingsCls, s: s}
	})
}

// registerSAMLLogout installs the SP-initiated Single-Logout builders:
// SAML::Logoutrequest (#create → the IdP SLO redirect URL for a logout request)
// and SAML::SloLogoutresponse (#create → the SLO redirect URL responding to an
// IdP-initiated logout).
func (vm *VM) registerSAMLLogout(mod *RClass) {
	req := newClass("SAML::Logoutrequest", vm.cObject)
	mod.consts["Logoutrequest"] = req
	req.smethods["new"] = &Method{name: "new", owner: req,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &SAMLLogoutrequest{cls: req, l: &saml.Logoutrequest{}}
		}}
	// SAML::Logoutrequest#create(settings, name_id, relay_state = "") → the IdP
	// SLO redirect URL carrying the deflate+base64 SAMLRequest.
	req.define("create", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		l := self.(*SAMLLogoutrequest)
		url, err := l.l.Create(samlSettings(args[0]), strArg(args[1]), samlOptRelayState(args, 2))
		if err != nil {
			raiseSAMLError(err)
		}
		return object.NewString(url)
	})
	req.define("uuid", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*SAMLLogoutrequest).l.UUID)
	})

	resp := newClass("SAML::SloLogoutresponse", vm.cObject)
	mod.consts["SloLogoutresponse"] = resp
	resp.smethods["new"] = &Method{name: "new", owner: resp,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &SAMLSloLogoutresponse{cls: resp, l: &saml.SloLogoutresponse{}}
		}}
	// SAML::SloLogoutresponse#create(settings, request_id, relay_state = "") →
	// the IdP SLO redirect URL carrying the deflate+base64 SAMLResponse.
	resp.define("create", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		l := self.(*SAMLSloLogoutresponse)
		url, err := l.l.Create(samlSettings(args[0]), strArg(args[1]), samlOptRelayState(args, 2))
		if err != nil {
			raiseSAMLError(err)
		}
		return object.NewString(url)
	})
	resp.define("uuid", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*SAMLSloLogoutresponse).l.UUID)
	})
}
