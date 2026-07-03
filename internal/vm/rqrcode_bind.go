// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"strings"

	rqrcode "github.com/go-ruby-rqrcode/rqrcode"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-rqrcode/rqrcode generator. The
// QR-matrix generation and every renderer live in that library; rbgo only maps
// the keyword-option Hashes to the library's Option / *Options structs, calls the
// matching library method, and returns the module matrix or rendered String.

// rqrcodeNew builds a QRCode from data and a keyword-options Hash (:level, :size,
// :mode, :max_size). A build error (bad version, non-numeric data forced numeric,
// data too large) raises the matching RQRCode error, mirroring the gem.
func rqrcodeNew(data string, opt object.Value) *rqrcode.QRCode {
	var opts []rqrcode.Option
	if h, ok := opt.(*object.Hash); ok {
		for _, k := range h.Keys {
			val, _ := h.Get(k)
			switch rqrcodeKey(k) {
			case "level":
				opts = append(opts, rqrcode.WithLevelSymbol(rqrcodeSym(val)))
			case "size":
				opts = append(opts, rqrcode.WithSize(int(intArg(val))))
			case "max_size":
				opts = append(opts, rqrcode.WithMaxSize(int(intArg(val))))
			case "mode":
				opts = append(opts, rqrcode.WithModeSymbol(rqrcodeSym(val)))
			}
		}
	}
	q, err := rqrcode.New(data, opts...)
	if err != nil {
		rqrcodeRaise(err)
	}
	return q
}

// rqrcodeRaise maps a library error to the matching RQRCode Ruby error, using the
// sentinel it wraps: ErrArgument -> QRCodeArgumentError, otherwise
// QRCodeRunTimeError (the gem's QRCodeRunTimeError family).
func rqrcodeRaise(err error) {
	if errors.Is(err, rqrcode.ErrArgument) {
		raise("RQRCode::QRCodeArgumentError", "%s", rqrcodeErrMsg(err))
	}
	raise("RQRCode::QRCodeRunTimeError", "%s", rqrcodeErrMsg(err))
}

// rqrcodeErrMsg strips the library's "rqrcode: <sentinel>: " prefix from an error
// message so the Ruby error carries the gem-style message ("Unknown error
// correction level ...", "bad version ...") rather than the Go sentinel text.
func rqrcodeErrMsg(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i >= 0 {
		if j := strings.Index(msg[i+2:], ": "); j >= 0 {
			return msg[i+2+j+2:]
		}
		return msg[i+2:]
	}
	return msg
}

// rqrcodeModules returns the module matrix as a Ruby Array of Arrays of booleans,
// matching the gem's `qr.qrcode.modules` / `qr.to_a`.
func rqrcodeModules(q *rqrcode.QRCode) object.Value {
	rows := object.NewArrayFromSlice(make([]object.Value, len(q.Modules)))
	for i, row := range q.Modules {
		cols := object.NewArrayFromSlice(make([]object.Value, len(row)))
		for j, dark := range row {
			cols.Elems[j] = object.Bool(dark)
		}
		rows.Elems[i] = cols
	}
	return rows
}

// rqrcodeAsSVG maps a renderer-options Hash to a rqrcode.SVGOptions and renders.
func rqrcodeAsSVG(q *rqrcode.QRCode, opt *object.Hash) string {
	o := rqrcode.SVGOptions{}
	rqrcodeEachOpt(opt, func(key string, val object.Value) {
		switch key {
		case "offset":
			o.Offset = int(intArg(val))
		case "color":
			o.Color = rqrcodeStr(val)
		case "fill":
			o.Fill = rqrcodeStr(val)
		case "module_size":
			o.ModuleSize = int(intArg(val))
		case "shape_rendering":
			o.ShapeRendering = rqrcodeStr(val)
		case "use_path":
			o.UsePath = val.Truthy()
		case "viewbox":
			o.Viewbox = val.Truthy()
		}
	})
	return q.AsSVG(o)
}

// rqrcodeAsANSI maps a renderer-options Hash to a rqrcode.ANSIOptions and
// renders. With no options the gem default (quiet zone 4) applies.
func rqrcodeAsANSI(q *rqrcode.QRCode, opt *object.Hash) string {
	if opt == nil {
		return q.AsANSIDefault()
	}
	o := rqrcode.ANSIOptions{QuietZoneSize: -1}
	rqrcodeEachOpt(opt, func(key string, val object.Value) {
		switch key {
		case "light":
			o.Light = rqrcodeStr(val)
		case "dark":
			o.Dark = rqrcodeStr(val)
		case "fill_character":
			o.FillCharacter = rqrcodeStr(val)
		case "quiet_zone_size":
			o.QuietZoneSize = int(intArg(val))
		}
	})
	return q.AsANSI(o)
}

// rqrcodeToString maps a renderer-options Hash to a rqrcode.StringOptions and
// renders the text form (dark "x" by default).
func rqrcodeToString(q *rqrcode.QRCode, opt *object.Hash) string {
	o := rqrcode.StringOptions{}
	rqrcodeEachOpt(opt, func(key string, val object.Value) {
		switch key {
		case "dark":
			o.Dark = rqrcodeStr(val)
		case "light":
			o.Light = rqrcodeStr(val)
		case "quiet_zone_size":
			o.QuietZoneSize = int(intArg(val))
		}
	})
	return q.ToString(o)
}

// rqrcodeEachOpt iterates a (possibly nil) options Hash, calling fn with each
// key's bare name and value.
func rqrcodeEachOpt(h *object.Hash, fn func(key string, val object.Value)) {
	if h == nil {
		return
	}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		fn(rqrcodeKey(k), val)
	}
}

// rqrcodeKey renders an option key (a Symbol or String) as its bare name.
func rqrcodeKey(k object.Value) string {
	switch n := k.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return k.ToS()
}

// rqrcodeSym renders an option value expected to be a level/mode symbol as its
// bare name (a Symbol or String), so `level: :h` and `level: "h"` both work.
func rqrcodeSym(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return v.ToS()
}

// rqrcodeStr renders an option value as a string: a String yields its contents,
// any other value its to_s.
func rqrcodeStr(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}
