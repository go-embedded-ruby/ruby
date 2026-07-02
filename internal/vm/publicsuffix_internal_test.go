// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	publicsuffix "github.com/go-ruby-public-suffix/public-suffix"
)

// TestPublicSuffixShell covers the Go-only arms of the PublicSuffix::Domain
// value shell — ToS and Truthy — which the Ruby-level to_s override does not
// reach (the class defines its own to_s / truthiness dispatch).
func TestPublicSuffixShell(t *testing.T) {
	dom, err := publicsuffix.Parse("www.example.com", nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d := &PublicSuffixDomain{d: publicSuffixDomain{d: dom}}
	if got := d.ToS(); got != "www.example.com" {
		t.Errorf("ToS() = %q", got)
	}
	if !d.Truthy() {
		t.Error("a Domain must be truthy")
	}
}
