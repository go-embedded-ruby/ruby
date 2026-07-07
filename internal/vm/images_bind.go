// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"image/color"

	images "github.com/go-ruby-images/images"
	"github.com/go-ruby-images/images/canvas"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-images/images library: it coerces
// Ruby arguments (blobs, packed colours, floats), wraps the library's
// (result, error) pairs into raises, and maps colours and metadata back into
// Ruby values. All decoding, drawing and encoding live in the library.

// imagesSelf / imagesCanvasSelf recover the Go wrapper from a Ruby receiver.
func imagesSelf(v object.Value) *ImagesImage        { return v.(*ImagesImage) }
func imagesCanvasSelf(v object.Value) *ImagesCanvas { return v.(*ImagesCanvas) }

// imagesArity raises ArgumentError when fewer than n positional arguments were
// supplied, so a native method never indexes past the argument slice.
func imagesArity(args []object.Value, n int) {
	if len(args) < n {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d)", len(args), n)
	}
}

// imagesWrap turns a library (*images.Image, error) pair into a Ruby value,
// raising Images::Error on a failure (a bad decode, a non-positive size, an
// out-of-range crop, a non-90-multiple rotation or an unencodable format).
func imagesWrap(im *images.Image, err error) object.Value {
	if err != nil {
		raise("Images::Error", "%s", err.Error())
	}
	return &ImagesImage{im: im}
}

// imagesCanvasWrap turns a library (*canvas.Canvas, error) pair into a Ruby
// value, raising Images::Error on a decode failure (corrupt PNG, missing file).
func imagesCanvasWrap(c *canvas.Canvas, err error) object.Value {
	if err != nil {
		raise("Images::Error", "%s", err.Error())
	}
	return &ImagesCanvas{c: c}
}

// imagesCanvasAt reads a pixel, raising Images::OutOfBoundsError when (x, y)
// falls outside the canvas.
func imagesCanvasAt(c *canvas.Canvas, x, y int) canvas.Color {
	col, err := c.At(x, y)
	if err != nil {
		raise("Images::OutOfBoundsError", "%s", err.Error())
	}
	return col
}

// imagesCanvasSet writes a pixel, raising Images::OutOfBoundsError when (x, y)
// falls outside the canvas.
func imagesCanvasSet(c *canvas.Canvas, x, y int, col canvas.Color) {
	if err := c.Set(x, y, col); err != nil {
		raise("Images::OutOfBoundsError", "%s", err.Error())
	}
}

// imagesBlobArg coerces an image-blob argument to its raw bytes: a String yields
// its bytes, anything else raises TypeError.
func imagesBlobArg(v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	raise("TypeError", "no implicit conversion of %s into String", v.Inspect())
	return nil
}

// imagesBlobReader wraps a String blob argument in a reader for the canvas PNG
// decoder.
func imagesBlobReader(v object.Value) *bytes.Reader {
	return bytes.NewReader(imagesBlobArg(v))
}

// imagesColorArg coerces a packed-colour argument (a plain Integer 0xRRGGBBAA)
// into a canvas.Color.
func imagesColorArg(v object.Value) canvas.Color {
	return canvas.Color(uint32(intArg(v)))
}

// imagesColorConst renders a canvas.Color as its packed Integer, the Ruby
// representation of a colour throughout the Images surface.
func imagesColorConst(c canvas.Color) object.Value {
	return object.IntValue(int64(c))
}

// imagesPackRGBA packs a color.RGBA (as returned by Images::Image#at) into the
// 0xRRGGBBAA integer the Ruby surface uses for colours.
func imagesPackRGBA(c color.RGBA) uint32 {
	return uint32(c.R)<<24 | uint32(c.G)<<16 | uint32(c.B)<<8 | uint32(c.A)
}

// imagesFloatArg coerces a numeric argument to a float64, raising TypeError for
// a non-numeric value.
func imagesFloatArg(v object.Value) float64 {
	f, ok := toFloat(v)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Float", v.Inspect())
	}
	return f
}

// imagesFloatSlice coerces a Ruby Array of numbers into a []float64 kernel,
// raising TypeError when the argument is not an Array.
func imagesFloatSlice(v object.Value) []float64 {
	arr, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Array", v.Inspect())
	}
	out := make([]float64, len(arr.Elems))
	for i, e := range arr.Elems {
		out[i] = imagesFloatArg(e)
	}
	return out
}

// imagesFormatArg renders a format argument (a String or Symbol) as its bare
// name, so both `convert("png")` and `convert(:png)` work.
func imagesFormatArg(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

// imagesStrArray renders a slice of format names as a Ruby Array of Strings.
func imagesStrArray(ss []string) object.Value {
	out := make([]object.Value, len(ss))
	for i, s := range ss {
		out[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(out)
}

// imagesMetadata maps an images.Metadata into a Ruby Hash keyed by Symbol, the
// shape a MiniMagick-style caller expects from Images::Image#metadata.
func imagesMetadata(m images.Metadata) object.Value {
	h := object.NewHash()
	h.Set(object.Symbol("width"), object.IntValue(int64(m.Width)))
	h.Set(object.Symbol("height"), object.IntValue(int64(m.Height)))
	h.Set(object.Symbol("format"), object.NewString(m.Format))
	h.Set(object.Symbol("color_model"), object.NewString(m.ColorModel))
	return h
}
