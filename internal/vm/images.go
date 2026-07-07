// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"

	images "github.com/go-ruby-images/images"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// ImagesImage is the Ruby wrapper around a *images.Image, the go-ruby-images
// processing handle (the pure-Go MiniMagick / ruby-vips analogue). Every
// transform returns a fresh Images::Image, and the scikit-image operations
// (blur, edges, threshold, morphology, colour ops) are exposed as instance
// methods, so `require "images"` presents one coherent Images::Image surface.
// The decoding, transforms and encoders all live in the
// github.com/go-ruby-images/images library (see images_bind.go); this file is
// the thin wiring plus the Images module namespace and its Images::Image class.
type ImagesImage struct {
	im *images.Image
}

func (i *ImagesImage) ToS() string {
	w, h := i.im.Dimensions()
	return fmt.Sprintf("#<Images::Image %dx%d %s>", w, h, i.im.Format())
}
func (i *ImagesImage) Inspect() string { return i.ToS() }
func (i *ImagesImage) Truthy() bool    { return true }

// registerImages installs the Images module (require "images") and its three
// capability surfaces: the Images::Image processing/scikit-image class, the
// chunky_png-style Images::Canvas class and the Images::Color helper module.
// Colours flow as plain Integers packed 0xRRGGBBAA, matching ChunkyPNG's integer
// representation, so no wrapper class is needed for them.
func (vm *VM) registerImages() {
	mod := newClass("Images", nil)
	mod.isModule = true
	vm.consts["Images"] = mod
	vm.registerImagesErrors(mod)

	// Images.supported / Images.encodable report the canonical decode / encode
	// format names, mirroring the library's package functions.
	mod.smethods["supported"] = &Method{name: "supported", owner: mod,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return imagesStrArray(images.Supported())
		}}
	mod.smethods["encodable"] = &Method{name: "encodable", owner: mod,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return imagesStrArray(images.Encodable())
		}}

	vm.registerImagesImage(mod)
	vm.registerImagesCanvas(mod)
	vm.registerImagesColor(mod)
}

// registerImagesErrors installs the Images error tree: Images::Error (the base,
// under StandardError, for a decode / encode / transform failure) and
// Images::OutOfBoundsError (a pixel access outside the raster). Each class is
// registered both as a nested constant of Images and under its qualified name in
// the top-level table so a Ruby `rescue Images::Error` and a re-raised library
// error resolve to the same class, exactly as the RQRCode error tree does.
func (vm *VM) registerImagesErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("Images::Error", std)
	mod.consts["Error"] = base
	vm.consts["Images::Error"] = base

	oob := newClass("Images::OutOfBoundsError", base)
	mod.consts["OutOfBoundsError"] = oob
	vm.consts["Images::OutOfBoundsError"] = oob
}

// registerImagesImage installs Images::Image, the MiniMagick / ruby-vips-style
// processing class with the scikit-image operations layered on as methods. The
// class constructors decode from a file (Image.open), an in-memory blob
// (Image.read / Image.decode) or build a fresh canvas (Image.new); the instance
// methods resize, crop, rotate, filter and encode. Every transform returns a new
// Images::Image and never mutates the receiver.
func (vm *VM) registerImagesImage(mod *RClass) {
	cls := newClass("Images::Image", vm.cObject)
	mod.consts["Image"] = cls
	vm.consts["Images::Image"] = cls

	// Images::Image.open(path) decodes the image file at path.
	cls.smethods["open"] = &Method{name: "open", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			imagesArity(args, 1)
			return imagesWrap(images.Open(strArg(args[0])))
		}}

	// Images::Image.read(blob) / .decode(blob) decode an in-memory String blob,
	// the equivalent of MiniMagick::Image.read.
	read := &Method{name: "read", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			imagesArity(args, 1)
			return imagesWrap(images.Read(imagesBlobArg(args[0])))
		}}
	cls.smethods["read"] = read
	cls.smethods["decode"] = read

	// Images::Image.new(width, height, fill = Images::Color::TRANSPARENT) builds a
	// fresh image filled with the packed colour fill (default transparent).
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			imagesArity(args, 2)
			fill := canvasTransparent()
			if len(args) > 2 {
				fill = imagesColorArg(args[2])
			}
			return &ImagesImage{im: images.New(int(intArg(args[0])), int(intArg(args[1])), fill)}
		}}

	// Query methods.
	cls.define("width", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(imagesSelf(self).im.Width()))
	})
	cls.define("height", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(imagesSelf(self).im.Height()))
	})
	cls.define("dimensions", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		w, h := imagesSelf(self).im.Dimensions()
		return object.NewArrayFromSlice([]object.Value{object.IntValue(int64(w)), object.IntValue(int64(h))})
	})
	cls.define("format", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(imagesSelf(self).im.Format())
	})
	cls.define("metadata", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return imagesMetadata(imagesSelf(self).im.Metadata())
	})
	cls.define("at", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 2)
		c, err := imagesSelf(self).im.At(int(intArg(args[0])), int(intArg(args[1])))
		if err != nil {
			raise("Images::OutOfBoundsError", "%s", err.Error())
		}
		return object.IntValue(int64(imagesPackRGBA(c)))
	})

	// Encoding / output.
	cls.define("to_blob", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		blob, err := imagesSelf(self).im.ToBlob()
		if err != nil {
			raise("Images::Error", "%s", err.Error())
		}
		return object.NewStringBytesEnc(blob, "ASCII-8BIT")
	})
	cls.define("write", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		if err := imagesSelf(self).im.Write(strArg(args[0])); err != nil {
			raise("Images::Error", "%s", err.Error())
		}
		return self
	})

	vm.registerImagesTransforms(cls)
}

// registerImagesTransforms installs the transform + filter instance methods on
// Images::Image. The geometric transforms and format conversion form the
// MiniMagick / ruby-vips surface; the blur / sharpen / edge / threshold /
// morphology / colour methods are the scikit-image (go-images) surface. The
// error-returning ones funnel through imagesWrap (raising Images::Error on a bad
// argument such as a non-positive size, an out-of-range crop, a non-90-multiple
// rotation or an unencodable target format); the total ones wrap directly.
func (vm *VM) registerImagesTransforms(cls *RClass) {
	// Error-returning geometric transforms + format conversion.
	cls.define("resize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 2)
		return imagesWrap(imagesSelf(self).im.Resize(int(intArg(args[0])), int(intArg(args[1]))))
	})
	cls.define("resize_nearest", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 2)
		return imagesWrap(imagesSelf(self).im.ResizeNearest(int(intArg(args[0])), int(intArg(args[1]))))
	})
	cls.define("thumbnail", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 2)
		return imagesWrap(imagesSelf(self).im.Thumbnail(int(intArg(args[0])), int(intArg(args[1]))))
	})
	cls.define("crop", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 4)
		return imagesWrap(imagesSelf(self).im.Crop(int(intArg(args[0])), int(intArg(args[1])), int(intArg(args[2])), int(intArg(args[3]))))
	})
	cls.define("rotate", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return imagesWrap(imagesSelf(self).im.Rotate(int(intArg(args[0]))))
	})
	cls.define("convert", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return imagesWrap(imagesSelf(self).im.Convert(imagesFormatArg(args[0])))
	})

	// Error-returning scikit-image filters.
	cls.define("blur", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return imagesWrap(imagesSelf(self).im.Blur(int(intArg(args[0]))))
	})
	cls.define("gaussian_blur", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return imagesWrap(imagesSelf(self).im.GaussianBlur(imagesFloatArg(args[0])))
	})
	cls.define("unsharp_mask", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 2)
		return imagesWrap(imagesSelf(self).im.UnsharpMask(imagesFloatArg(args[0]), imagesFloatArg(args[1])))
	})
	cls.define("median", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return imagesWrap(imagesSelf(self).im.Median(int(intArg(args[0]))))
	})
	cls.define("canny", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 3)
		return imagesWrap(imagesSelf(self).im.Canny(imagesFloatArg(args[0]), imagesFloatArg(args[1]), imagesFloatArg(args[2])))
	})
	cls.define("convolve", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 3)
		return imagesWrap(imagesSelf(self).im.Convolve(int(intArg(args[0])), int(intArg(args[1])), imagesFloatSlice(args[2])))
	})

	// Error-returning morphology.
	cls.define("erode", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return imagesWrap(imagesSelf(self).im.Erode(int(intArg(args[0]))))
	})
	cls.define("dilate", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return imagesWrap(imagesSelf(self).im.Dilate(int(intArg(args[0]))))
	})
	cls.define("open", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return imagesWrap(imagesSelf(self).im.Open(int(intArg(args[0]))))
	})
	cls.define("close", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return imagesWrap(imagesSelf(self).im.Close(int(intArg(args[0]))))
	})

	// Total (never-erroring) transforms + colour ops.
	total := func(name string, fn func(*images.Image) *images.Image) {
		cls.define(name, func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return &ImagesImage{im: fn(imagesSelf(self).im)}
		})
	}
	total("flip", (*images.Image).Flip)
	total("flop", (*images.Image).Flop)
	total("grayscale", (*images.Image).Grayscale)
	total("greyscale", (*images.Image).Grayscale)
	total("invert", (*images.Image).Invert)
	total("clone", (*images.Image).Clone)
	total("sharpen", (*images.Image).Sharpen)
	total("sobel", (*images.Image).Sobel)
	total("prewitt", (*images.Image).Prewitt)
	total("scharr", (*images.Image).Scharr)
	total("laplacian", (*images.Image).Laplacian)
	total("otsu", (*images.Image).Otsu)

	// Total transforms taking a scalar argument.
	cls.define("brightness", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return &ImagesImage{im: imagesSelf(self).im.Brightness(imagesFloatArg(args[0]))}
	})
	cls.define("contrast", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return &ImagesImage{im: imagesSelf(self).im.Contrast(imagesFloatArg(args[0]))}
	})
	cls.define("threshold", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		return &ImagesImage{im: imagesSelf(self).im.Threshold(uint8(intArg(args[0])))}
	})
	cls.define("otsu_threshold", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(imagesSelf(self).im.OtsuThreshold()))
	})
}
