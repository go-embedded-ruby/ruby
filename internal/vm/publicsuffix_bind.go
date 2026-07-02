// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	publicsuffix "github.com/go-ruby-public-suffix/public-suffix"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-public-suffix/public-suffix parser.
// The Public-Suffix-List decomposition lives in that library; rbgo only maps a
// hostname String and the ignore_private: / default_rule: keyword options to a
// publicsuffix.Parse / Valid / RegistrableDomain call, so the public_suffix-gem
// faithful behaviour the PublicSuffix module relies on is preserved by
// construction.

// publicSuffixDomain wraps a *publicsuffix.Domain and exposes its three levels as
// methods, so the PublicSuffix::Domain shell (publicsuffix.go) never imports the
// library type directly.
type publicSuffixDomain struct{ d *publicsuffix.Domain }

func (p publicSuffixDomain) GetTLD() string     { return p.d.TLD }
func (p publicSuffixDomain) GetSLD() string     { return p.d.SLD }
func (p publicSuffixDomain) GetTRD() string     { return p.d.TRD }
func (p publicSuffixDomain) HasTLD() bool       { return p.d.HasTLD() }
func (p publicSuffixDomain) HasSLD() bool       { return p.d.HasSLD() }
func (p publicSuffixDomain) HasTRD() bool       { return p.d.HasTRD() }
func (p publicSuffixDomain) Name() string       { return p.d.Name() }
func (p publicSuffixDomain) DomainName() string { return p.d.DomainName() }
func (p publicSuffixDomain) Subdomain() string  { return p.d.Subdomain() }
func (p publicSuffixDomain) IsDomain() bool     { return p.d.IsDomain() }
func (p publicSuffixDomain) IsSubdomain() bool  { return p.d.IsSubdomain() }

// publicSuffixParse parses a hostname into a PublicSuffix::Domain, re-raising a
// library error as the matching Ruby class: an invalid name as
// PublicSuffix::DomainInvalid, a not-allowed (e.g. a bare public suffix) name as
// PublicSuffix::DomainNotAllowed. Both are the gem's DomainInvalid family.
func publicSuffixParse(name string, opts *publicsuffix.Options) object.Value {
	dom, err := publicsuffix.Parse(name, opts)
	if err != nil {
		raise(publicSuffixErrClass(err), "%s", err.Error())
	}
	return &PublicSuffixDomain{d: publicSuffixDomain{d: dom}}
}

// publicSuffixRegistrable returns the registrable-domain String for a name, or
// Ruby nil when the name has none (mirroring PublicSuffix.domain, which returns
// nil rather than raising).
func publicSuffixRegistrable(name string, opts *publicsuffix.Options) object.Value {
	return publicSuffixOptStr(publicsuffix.RegistrableDomain(name, opts))
}

// publicSuffixValid reports whether a name has a valid registrable domain.
func publicSuffixValid(name string, opts *publicsuffix.Options) bool {
	return publicsuffix.Valid(name, opts)
}

// publicSuffixErrClass classifies a library error into the qualified Ruby
// exception name to raise: DomainNotAllowed for the not-allowed sentinel, else
// DomainInvalid for the invalid sentinel (the base for any other error).
func publicSuffixErrClass(err error) string {
	if errors.Is(err, publicsuffix.ErrDomainNotAllowed) {
		return "PublicSuffix::DomainNotAllowed"
	}
	return "PublicSuffix::DomainInvalid"
}

// publicSuffixOpts maps the trailing keyword-options Hash of a PublicSuffix call
// to a *publicsuffix.Options. The recognised keys mirror the gem: ignore_private:
// (skip the PRIVATE section) and default_rule: nil (a nil value disables the
// fallback "*" rule, so an unlisted TLD is invalid). A call with no options Hash
// uses the library default (nil).
func publicSuffixOpts(args []object.Value) *publicsuffix.Options {
	if len(args) < 2 {
		return nil
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok {
		return nil
	}
	o := &publicsuffix.Options{}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		switch publicSuffixKey(k) {
		case "ignore_private":
			o.IgnorePrivate = val.Truthy()
		case "default_rule":
			// default_rule: nil disables the "*" fallback.
			o.NoDefaultRule = !val.Truthy()
		}
	}
	return o
}

// publicSuffixKey renders an options key (a Symbol or String) as its bare name.
func publicSuffixKey(k object.Value) string {
	switch n := k.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return k.ToS()
}

// publicSuffixLevel renders a decomposition level: its String when present, Ruby
// nil when absent (the gem models a missing level as nil).
func publicSuffixLevel(s string, present bool) object.Value {
	if !present {
		return object.NilV
	}
	return object.NewString(s)
}

// publicSuffixOptStr maps a possibly-empty derived string to a Ruby String, or
// Ruby nil when empty (PublicSuffix#domain / #subdomain return nil, not "").
func publicSuffixOptStr(s string) object.Value {
	if s == "" {
		return object.NilV
	}
	return object.NewString(s)
}
