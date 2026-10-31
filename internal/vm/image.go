//go:build !rbgo_core

package vm

import (
	"bytes"
	"fmt"
	"image"
	"image/color"

	goimg "github.com/go-images/images"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Image binds github.com/go-images/images — a pure-Go (cgo-free) scikit-image
// style library — into Ruby, completing the scientific trio (NDArray + FFT +
// Image). It wraps a *image.RGBA as a Ruby value.

// Image is the Ruby wrapper around an RGBA raster.
type Image struct{ img *image.RGBA }

func (i *Image) ToS() string {
	b := i.img.Bounds()
	return fmt.Sprintf("#<Image %dx%d>", b.Dx(), b.Dy())
}
func (i *Image) Inspect() string { return i.ToS() }
func (i *Image) Truthy() bool    { return true }

// imgOf returns the receiver's raster.
func imgOf(v object.Value) *image.RGBA { return v.(*Image).img }

// mustImg raises a Ruby error when a go-images operation fails (bad radius,
// out-of-bounds crop, …) instead of returning a Go error.
func mustImg(img *image.RGBA, err error) object.Value {
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	return &Image{img: img}
}

// registerImage installs the Image class, its constructors and methods.
func (vm *VM) registerImage() {
	vm.cImage = newClass("Image", vm.cObject)
	vm.consts["Image"] = vm.cImage

	// s registers a module method with its arity, for the same reason as d.
	s := func(name string, min, max int, fn NativeFn) {
		vm.cImage.smethods[name] = &Method{name: name, owner: vm.cImage,
			native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
				ndArgc(args, min, max)
				return fn(vm, self, args, blk)
			}}
	}

	s("new", 2, 2, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		w, h := clampInt(intArg(args[0])), clampInt(intArg(args[1]))
		if w <= 0 || h <= 0 {
			raise("ArgumentError", "image dimensions must be positive")
		}
		return &Image{img: image.NewRGBA(image.Rect(0, 0, w, h))}
	})
	s("load", 1, 1, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		img, err := goimg.Load(args[0].ToS())
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return &Image{img: img}
	})
	// Image.decode(bytes) decodes an in-memory PNG/JPEG String — no filesystem,
	// so it works in a browser (wasm) where the program is handed image bytes.
	s("decode", 1, 1, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s, ok := args[0].(*object.String)
		if !ok {
			raise("TypeError", "Image.decode expects a String of image bytes")
		}
		img, err := goimg.Decode(bytes.NewReader(s.Bytes()))
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return &Image{img: img}
	})

	// d registers an instance method WITH its arity. The counts live in the
	// signature so a method added later cannot be registered without them:
	// the bodies index args[0..3] directly, and a missing argument used to
	// surface as "runtime error: index out of range" -- a recovered Go panic
	// relabelled ArgumentError -- where MRI says "wrong number of
	// arguments" (#820).
	d := func(name string, min, max int, fn NativeFn) {
		vm.cImage.define(name, func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			ndArgc(args, min, max)
			return fn(vm, self, args, blk)
		})
	}

	d("width", 0, 0, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(imgOf(v).Bounds().Dx()))
	})
	d("height", 0, 0, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(imgOf(v).Bounds().Dy()))
	})
	d("get", 2, 2, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		c := imgOf(v).RGBAAt(clampInt(intArg(args[0])), clampInt(intArg(args[1])))
		return object.NewArray(object.IntValue(int64(c.R)), object.IntValue(int64(c.G)), object.IntValue(int64(c.B)), object.IntValue(int64(c.A)))
	})
	d("set", 5, 6, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a := uint8(255)
		if len(args) > 5 {
			a = uint8(intArg(args[5]))
		}
		imgOf(v).SetRGBA(clampInt(intArg(args[0])), clampInt(intArg(args[1])),
			color.RGBA{uint8(intArg(args[2])), uint8(intArg(args[3])), uint8(intArg(args[4])), a})
		return object.NilV
	})
	d("save", 1, 1, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := goimg.Save(args[0].ToS(), imgOf(v)); err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return object.NilV
	})
	// to_png / to_jpeg encode to an in-memory byte String (browser-friendly: hand
	// the bytes to JS for a canvas/Blob). Encoding to a buffer cannot fail.
	encode := func(name string, format goimg.Format) {
		d(name, 0, 0, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			var buf bytes.Buffer
			_ = goimg.Encode(&buf, imgOf(v), format)
			return object.NewStringBytes(buf.Bytes())
		})
	}
	encode("to_png", goimg.PNG)
	encode("to_jpeg", goimg.JPEG)

	// Operations with no error: image → image.
	unary := func(name string, f func(image.Image) *image.RGBA) {
		d(name, 0, 0, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return &Image{img: f(imgOf(v))}
		})
	}
	unary("grayscale", goimg.Grayscale)
	unary("invert", goimg.Invert)
	unary("sobel", goimg.Sobel)
	unary("sobel_mag", goimg.SobelMag)
	unary("prewitt", goimg.Prewitt)
	unary("scharr", goimg.Scharr)
	unary("laplacian", goimg.Laplacian)
	unary("rgb_to_hsv", goimg.RGBToHSV)
	unary("hsv_to_rgb", goimg.HSVToRGB)
	unary("otsu", goimg.Otsu)
	unary("sharpen", goimg.Sharpen)
	unary("rotate90", goimg.Rotate90)
	unary("rotate180", goimg.Rotate180)
	unary("rotate270", goimg.Rotate270)
	unary("flip_horizontal", goimg.FlipHorizontal)
	unary("flip_vertical", goimg.FlipVertical)

	// Operations with a scalar parameter, no error.
	scalar := func(name string, f func(image.Image, float64) *image.RGBA) {
		d(name, 1, 1, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			x, _ := toFloat(args[0])
			return &Image{img: f(imgOf(v), x)}
		})
	}
	scalar("adjust_brightness", goimg.AdjustBrightness)
	scalar("adjust_contrast", goimg.AdjustContrast)

	// Operations that can fail.
	d("gaussian_blur", 1, 1, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, _ := toFloat(args[0])
		return mustImg(goimg.GaussianBlur(imgOf(v), s))
	})
	d("box_blur", 1, 1, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustImg(goimg.BoxBlur(imgOf(v), clampInt(intArg(args[0]))))
	})
	d("median", 1, 1, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustImg(goimg.Median(imgOf(v), clampInt(intArg(args[0]))))
	})
	d("erode", 1, 1, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustImg(goimg.Erode(imgOf(v), clampInt(intArg(args[0]))))
	})
	d("dilate", 1, 1, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustImg(goimg.Dilate(imgOf(v), clampInt(intArg(args[0]))))
	})
	d("canny", 3, 3, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		sigma, _ := toFloat(args[0])
		low, _ := toFloat(args[1])
		high, _ := toFloat(args[2])
		return mustImg(goimg.Canny(imgOf(v), sigma, low, high))
	})
	d("resize", 2, 2, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustImg(goimg.Resize(imgOf(v), clampInt(intArg(args[0])), clampInt(intArg(args[1])), goimg.Bilinear))
	})
	d("crop", 4, 4, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		x, y := clampInt(intArg(args[0])), clampInt(intArg(args[1]))
		w, h := clampInt(intArg(args[2])), clampInt(intArg(args[3]))
		return mustImg(goimg.Crop(imgOf(v), image.Rect(x, y, x+w, y+h)))
	})

	d("inspect", 0, 0, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*Image).Inspect())
	})
	vm.cImage.define("to_s", vm.cImage.methods["inspect"].native)
}
