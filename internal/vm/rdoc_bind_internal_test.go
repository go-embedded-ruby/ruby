// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRDocRenderDefault covers rdocRender's default arm, which is unreachable
// from Ruby (kind always comes from a formatter wrapper's fixed set).
func TestRDocRenderDefault(t *testing.T) {
	if got := rdocRender("nope", "x"); got != "" {
		t.Errorf("rdocRender(bogus) = %q, want empty", got)
	}
}

// TestRDocValueProtocol covers the ToS / Inspect / Truthy arms of the RDoc
// wrappers, which Ruby reaches only when the wrapper object is itself inspected.
func TestRDocValueProtocol(t *testing.T) {
	m := &RDocMarkup{}
	if m.ToS() != "#<RDoc::Markup>" || m.Inspect() != "#<RDoc::Markup>" || !m.Truthy() {
		t.Errorf("markup protocol: %q %q %v", m.ToS(), m.Inspect(), m.Truthy())
	}
	f := &RDocFormatter{kind: "html", clsName: "RDoc::Markup::ToHtml"}
	if f.ToS() != "#<RDoc::Markup::ToHtml>" || f.Inspect() != "#<RDoc::Markup::ToHtml>" || !f.Truthy() {
		t.Errorf("formatter protocol: %q %q %v", f.ToS(), f.Inspect(), f.Truthy())
	}
}
