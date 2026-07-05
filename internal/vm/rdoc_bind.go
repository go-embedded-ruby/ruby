// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rdoc "github.com/go-ruby-rdoc/rdoc"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-rdoc/rdoc library. The library owns
// the whole markup pipeline — tokenizing, parsing into the RDoc::Markup document
// model, the inline attribute manager and the byte-faithful HTML / Markdown /
// RDoc renderers; rbgo only wraps the driver and the formatters as Ruby objects
// (see rdoc.go for the class + method registration) and hands markup text across
// the boundary to the matching convenience renderer here.

// RDocMarkup wraps an RDoc::Markup driver. It is stateless — RDoc::Markup#convert
// renders through whichever formatter it is given — so the wrapper carries no
// fields, matching the gem where a fresh RDoc::Markup holds only its (default
// here) attribute rules.
type RDocMarkup struct{}

func (v *RDocMarkup) ToS() string     { return "#<RDoc::Markup>" }
func (v *RDocMarkup) Inspect() string { return "#<RDoc::Markup>" }
func (v *RDocMarkup) Truthy() bool    { return true }

// RDocFormatter wraps an RDoc::Markup output formatter. kind selects the target
// syntax the library renders to ("html" / "markdown" / "rdoc"); clsName is the
// Ruby class the wrapper reports (RDoc::Markup::ToHtml / ToMarkdown / ToRdoc), so
// classOf and RDoc::Markup#convert both resolve the formatter by its class.
type RDocFormatter struct {
	kind    string
	clsName string
}

func (v *RDocFormatter) ToS() string     { return "#<" + v.clsName + ">" }
func (v *RDocFormatter) Inspect() string { return "#<" + v.clsName + ">" }
func (v *RDocFormatter) Truthy() bool    { return true }

// rdocRender converts RDoc markup text to a formatter's target syntax through the
// library's convenience entry points, mirroring the gem's markup -> ToHtml /
// ToMarkdown / ToRdoc round trip. kind always comes from a formatter wrapper's
// fixed set, so the default arm is unreachable from Ruby (covered white-box).
func rdocRender(kind, text string) string {
	switch kind {
	case "html":
		return rdoc.ToHTML(text)
	case "markdown":
		return rdoc.ToMarkdownString(text)
	case "rdoc":
		return rdoc.ToRdocString(text)
	}
	return ""
}
