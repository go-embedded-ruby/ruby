// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"testing"

	rqrcode "github.com/go-ruby-rqrcode/rqrcode"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestRQRCodeShell covers the RQRCode value shell's ToS/Inspect/Truthy.
func TestRQRCodeShell(t *testing.T) {
	r := &RQRCode{}
	if r.ToS() != "#<RQRCode::QRCode>" || r.Inspect() != "#<RQRCode::QRCode>" || !r.Truthy() {
		t.Errorf("shell: %q / %q / %v", r.ToS(), r.Inspect(), r.Truthy())
	}
}

// TestRQRCodeKeyBridges covers the to_s-default arms of rqrcodeKey, rqrcodeSym
// and rqrcodeStr (a non-Symbol, non-String value) plus their String/Symbol arms.
func TestRQRCodeKeyBridges(t *testing.T) {
	if got := rqrcodeKey(object.Symbol("s")); got != "s" {
		t.Errorf("sym key -> %q", got)
	}
	if got := rqrcodeKey(object.NewString("k")); got != "k" {
		t.Errorf("str key -> %q", got)
	}
	if got := rqrcodeKey(object.Integer(3)); got != "3" {
		t.Errorf("int key -> %q", got)
	}
	if got := rqrcodeSym(object.Symbol("h")); got != "h" {
		t.Errorf("sym -> %q", got)
	}
	if got := rqrcodeSym(object.NewString("q")); got != "q" {
		t.Errorf("str sym -> %q", got)
	}
	if got := rqrcodeSym(object.Integer(1)); got != "1" {
		t.Errorf("int sym -> %q", got)
	}
	if got := rqrcodeStr(object.NewString("x")); got != "x" {
		t.Errorf("str -> %q", got)
	}
	if got := rqrcodeStr(object.Integer(7)); got != "7" {
		t.Errorf("int str -> %q", got)
	}
}

// TestRQRCodeErrMsg covers rqrcodeErrMsg across its three shapes: a
// double-colon-prefixed library error (both prefixes stripped), a single-colon
// message (one prefix stripped), and a bare message (returned as-is).
func TestRQRCodeErrMsg(t *testing.T) {
	// A wrapped library error carries "rqrcode: <sentinel>: <detail>".
	if got := rqrcodeErrMsg(rqrcode.ErrArgument); got == "" {
		t.Error("wrapped error stripped to empty")
	}
	if got := rqrcodeErrMsg(errors.New("only: one")); got != "one" {
		t.Errorf("single-colon -> %q, want %q", got, "one")
	}
	if got := rqrcodeErrMsg(errors.New("nomarker")); got != "nomarker" {
		t.Errorf("bare -> %q, want %q", got, "nomarker")
	}
}

// TestRQRCodeRaiseRunTime covers rqrcodeRaise's non-argument (runtime) arm: a
// non-ErrArgument error raises RQRCode::QRCodeRunTimeError.
func TestRQRCodeRaiseRunTime(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a raise")
		}
	}()
	rqrcodeRaise(rqrcode.ErrRunTime)
}
