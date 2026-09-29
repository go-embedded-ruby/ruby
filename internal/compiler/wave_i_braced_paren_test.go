// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package compiler

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-ruby-parser/parser/ast"
)

// TestWaveIVCallName drives the predicate directly over the shapes the Ruby
// sources cannot reach. gettable makes a VCALL only under case ID_LOCAL
// (parse.y-ruby_4_0:13086); a '!' or '?' suffix lexes as tFID and
// `primary: tFID` makes an FCALL (parse.y-ruby_4_0:4370-4374).
func TestWaveIVCallName(t *testing.T) {
	for name, want := range map[string]bool{
		"nope": true, "_x": true, "nope!": false, "nope?": false,
		"nope=": false, "": false, "+": true,
	} {
		if got := vcallName(name); got != want {
			t.Errorf("vcallName(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestWaveIBracedDecidesTheKeywordFlags pins both call-site flags onto
// ast.HashLit.Braced. The pairs differ only by the braces, so a compiler that
// ignored the bit again would fail exactly the braced rows.
func TestWaveIBracedDecidesTheKeywordFlags(t *testing.T) {
	for _, tc := range []struct {
		src   string
		noKW  bool
		kwspl bool
	}{
		{src: "f({k: 42})", noKW: true},                // braced: a positional Hash
		{src: "f(k: 42)", noKW: false},                 // bare: keywords
		{src: "f({})", noKW: true},                     // an empty POSITIONAL Hash
		{src: "f({**h})", noKW: true},                  // braced, so NOT a keyword splat
		{src: "f(**h)", noKW: false, kwspl: true},      // bare: a keyword splat
		{src: "f(1, {k: 42})", noKW: true},             //
		{src: "f(*a, {k: 42})", noKW: true},            //
		{src: "f(*a, k: 42)", noKW: false},             //
		{src: "f({k: 42}, &blk)", noKW: true},          // a block-pass is not an argument
		{src: "o.m({k: 42})", noKW: true},              // an explicit receiver changes nothing
		{src: "yield({k: 42})", noKW: true},            // yield is an ordinary call site
		{src: "yield(k: 42)", noKW: false},             //
		{src: "def m; super({k: 1}); end", noKW: true}, // and so is an explicit super
		{src: "def m; super(k: 1); end", noKW: false},  //
		{src: "def m; super({**h}); end", noKW: true},  //
		{src: "def m; super(**h); end", kwspl: true},   //
	} {
		src := tc.src
		if len(src) > 6 && src[:6] == "def m;" {
			src = "class K\n" + src + "\nend"
		}
		flags := sendFlagsOf(t, src)
		if got := flags&bytecode.FlagSendNoKW != 0; got != tc.noKW {
			t.Errorf("%q: FlagSendNoKW = %v, want %v", tc.src, got, tc.noKW)
		}
		if got := flags&bytecode.FlagSendKWSplat != 0; got != tc.kwspl {
			t.Errorf("%q: FlagSendKWSplat = %v, want %v", tc.src, got, tc.kwspl)
		}
	}
}

// TestWaveIHasTrailingKwSplatConsultsBraced drives the helper directly: `{**h}`
// and `**h` build HashLits that differ only in Braced, and only the second is a
// keyword splat.
func TestWaveIHasTrailingKwSplatConsultsBraced(t *testing.T) {
	kw := func(braced bool) []ast.Node {
		return []ast.Node{&ast.HashLit{
			Keys:   []ast.Node{nil},
			Values: []ast.Node{&ast.VarRef{Name: "h"}},
			Braced: braced,
		}}
	}
	if !hasTrailingKwSplat(kw(false)) {
		t.Error("`**h`: want a trailing keyword splat")
	}
	if hasTrailingKwSplat(kw(true)) {
		t.Error("`{**h}`: want NO keyword splat — it is a positional Hash")
	}
}

// TestWaveIVCallFlagIsNotSetForATFID: a name ending in '!' or '?' lexes as tFID
// and `primary: tFID` builds NEW_FCALL (parse.y-ruby_4_0:4370-4374), so the flag
// must stay clear even though the call site's SHAPE is the VCALL shape. The
// plain name beside them is the control: it must still be set.
func TestWaveIVCallFlagIsNotSetForATFID(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{"nope_xyz!", false},
		{"nope_xyz?", false},
		{"nope_xyz", true},
	} {
		iseq := compileSrc(t, tc.src)
		found := false
		for _, in := range iseq.Insns {
			switch in.Op {
			case bytecode.OpSend, bytecode.OpSendArray, bytecode.OpSendBlockArg, bytecode.OpSendArrayBlockArg:
				if in.A < len(iseq.Names) && iseq.Names[in.A] == tc.src {
					found = true
					if got := in.Flags&bytecode.FlagSendVCall != 0; got != tc.want {
						t.Errorf("%s: FlagSendVCall = %v, want %v", tc.src, got, tc.want)
					}
				}
			}
		}
		if !found {
			t.Fatalf("%s: no send of %s was emitted", tc.src, tc.src)
		}
	}
}
