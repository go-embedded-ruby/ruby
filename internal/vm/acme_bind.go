// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	acme "github.com/go-ruby-acme/acme"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file holds the Go<->Ruby bridges for the Acme surface installed in
// acme.go: the injectable ACME transport seam, account-key sourcing, CSR
// construction, the error-tree mapping and the small keyword-argument helpers.

// acmeTestOptions is the injectable HTTP transport seam. In production it is nil
// and clients use the go-ruby-acme library's default (real-network) transport.
// Tests set it to a closure returning acme.Options — an injected *http.Client
// pointed at an in-process httptest mock ACME server (plus, where a deterministic
// failure is wanted, a no-retry backoff) — so the whole flow runs with no real CA
// and no network, exactly as faraday's transport seam does for HTTP bindings.
var acmeTestOptions func() []acme.Option

// acmeGenerateKey mints a fresh ECDSA P-256 key for an auto-generated account
// key or a certificate-request key. It is a seam so a test can force the (in
// practice never-taken) generation-failure branch.
var acmeGenerateKey = func() (crypto.Signer, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// newACMEClient builds an *acme.Client for the directory, appending the seam's
// options when the transport seam is installed. acme.NewClient never fails for a
// valid signer (the account key is validated up front by acmeAccountKey), so its
// error return — reserved for future transport validation — is discarded here.
func newACMEClient(key crypto.Signer, directory string) *acme.Client {
	var opts []acme.Option
	if acmeTestOptions != nil {
		opts = acmeTestOptions()
	}
	c, _ := acme.NewClient(key, directory, opts...)
	return c
}

// acmeAccountKey sources the ACME account private key. A String value is parsed
// as a PEM-encoded private key (PKCS#8, SEC1/EC or PKCS#1); anything else (nil
// or an omitted keyword) yields a freshly generated P-256 key, so a client can
// be created without the caller minting a key first.
func acmeAccountKey(v object.Value) (crypto.Signer, error) {
	s, ok := v.(*object.String)
	if !ok {
		return acmeGenerateKey()
	}
	block, _ := pem.Decode([]byte(s.Str()))
	if block == nil {
		return nil, errors.New("private_key: not a PEM-encoded key")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if signer, ok := k.(crypto.Signer); ok {
			return signer, nil
		}
		return nil, errors.New("private_key: not a signing key")
	}
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("private_key: unrecognised key encoding")
	}
	return k, nil
}

// acmeBuildCSR generates a fresh P-256 certificate key and builds a DER-encoded
// PKCS#10 request for the names, mirroring Acme::Client::CertificateRequest.
func acmeBuildCSR(names []string) ([]byte, error) {
	key, err := acmeGenerateKey()
	if err != nil {
		return nil, err
	}
	return acme.NewCSR(key, names...)
}

// acmeRaise maps an error from the go-ruby-acme library onto the Ruby error tree
// and raises it. The problem-document subclasses are matched before the *Error
// base (which they unwrap to); a non-ACME (transport / decode) error surfaces as
// Acme::Error.
func acmeRaise(err error) {
	var un *acme.Unauthorized
	var bn *acme.BadNonce
	var mal *acme.Malformed
	var rl *acme.RateLimited
	var base *acme.Error
	switch {
	case errors.As(err, &un):
		raise("Acme::Client::Error::Unauthorized", "%s", err.Error())
	case errors.As(err, &bn):
		raise("Acme::Client::Error::BadNonce", "%s", err.Error())
	case errors.As(err, &mal):
		raise("Acme::Client::Error::Malformed", "%s", err.Error())
	case errors.As(err, &rl):
		raise("Acme::Client::Error::RateLimited", "%s", err.Error())
	case errors.As(err, &base):
		raise("Acme::Client::Error", "%s", err.Error())
	default:
		raise("Acme::Error", "%s", err.Error())
	}
}

// acmeAccountHash renders an *acme.Account as a Ruby Hash mirroring the
// acme-client account resource: "url", "status" and a "contact" String array.
func acmeAccountHash(a *acme.Account) object.Value {
	h := object.NewHash()
	h.Set(object.NewString("url"), object.NewString(a.URL))
	h.Set(object.NewString("status"), object.NewString(a.Status))
	h.Set(object.NewString("contact"), acmeStringArray(a.Contact))
	return h
}

// acmeStringArray maps a []string to a Ruby Array of Strings.
func acmeStringArray(ss []string) object.Value {
	out := make([]object.Value, len(ss))
	for i, s := range ss {
		out[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(out)
}

// --- keyword-argument helpers ------------------------------------------------

// acmeKwargs returns the trailing keyword Hash of a native call, or an empty
// Hash when none was passed.
func acmeKwargs(args []object.Value) *object.Hash {
	if len(args) > 0 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			return h
		}
	}
	return object.NewHash()
}

// acmeKwGet looks up a keyword by name, accepting either a Symbol or a String
// key, and returns nil (NilV) when absent.
func acmeKwGet(h *object.Hash, name string) object.Value {
	for _, k := range h.Keys {
		if acmeKeyName(k) == name {
			v, _ := h.Get(k)
			return v
		}
	}
	return object.NilV
}

// acmeKwString returns a keyword's value as a String, or "" when absent/nil.
func acmeKwString(h *object.Hash, name string) string {
	v := acmeKwGet(h, name)
	if object.IsNil(v) {
		return ""
	}
	return strArg(v)
}

// acmeKwBool returns a keyword's truthiness (absent/nil/false -> false).
func acmeKwBool(h *object.Hash, name string) bool {
	return acmeKwGet(h, name).Truthy()
}

// acmeKwStrings returns a keyword as a []string: an Array is mapped element-wise,
// a bare String becomes a one-element slice and an absent/nil value an empty one.
func acmeKwStrings(h *object.Hash, name string) []string {
	v := acmeKwGet(h, name)
	switch n := v.(type) {
	case *object.Array:
		out := make([]string, len(n.Elems))
		for i, e := range n.Elems {
			out[i] = strArg(e)
		}
		return out
	case *object.String:
		return []string{n.Str()}
	}
	return nil
}

// acmeKeyName renders a Hash key (Symbol or String) as its bare name.
func acmeKeyName(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return fmt.Sprint(v.ToS())
}
