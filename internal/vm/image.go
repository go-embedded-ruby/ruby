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

	vm.cImage.smethods["new"] = &Method{name: "new", owner: vm.cImage,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			w, h := int(intArg(args[0])), int(intArg(args[1]))
			if w <= 0 || h <= 0 {
				raise("ArgumentError", "image dimensions must be positive")
			}
			return &Image{img: image.NewRGBA(image.Rect(0, 0, w, h))}
		}}
	vm.cImage.smethods["load"] = &Method{name: "load", owner: vm.cImage,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			img, err := goimg.Load(args[0].ToS())
			if err != nil {
				raise("ArgumentError", "%s", err.Error())
			}
			return &Image{img: img}
		}}
	// Image.decode(bytes) decodes an in-memory PNG/JPEG String — no filesystem,
	// so it works in a browser (wasm) where the program is handed image bytes.
	vm.cImage.smethods["decode"] = &Method{name: "decode", owner: vm.cImage,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			s, ok := args[0].(*object.String)
			if !ok {
				raise("TypeError", "Image.decode expects a String of image bytes")
			}
			img, err := goimg.Decode(bytes.NewReader(s.B))
			if err != nil {
				raise("ArgumentError", "%s", err.Error())
			}
			return &Image{img: img}
		}}

	d := func(name string, fn NativeFn) { vm.cImage.define(name, fn) }

	d("width", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(imgOf(v).Bounds().Dx())
	})
	d("height", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(imgOf(v).Bounds().Dy())
	})
	d("get", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		c := imgOf(v).RGBAAt(int(intArg(args[0])), int(intArg(args[1])))
		return &object.Array{Elems: []object.Value{
			object.Integer(c.R), object.Integer(c.G), object.Integer(c.B), object.Integer(c.A)}}
	})
	d("set", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a := uint8(255)
		if len(args) > 5 {
			a = uint8(intArg(args[5]))
		}
		imgOf(v).SetRGBA(int(intArg(args[0])), int(intArg(args[1])),
			color.RGBA{uint8(intArg(args[2])), uint8(intArg(args[3])), uint8(intArg(args[4])), a})
		return object.NilV
	})
	d("save", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := goimg.Save(args[0].ToS(), imgOf(v)); err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return object.NilV
	})
	// to_png / to_jpeg encode to an in-memory byte String (browser-friendly: hand
	// the bytes to JS for a canvas/Blob). Encoding to a buffer cannot fail.
	encode := func(name string, format goimg.Format) {
		d(name, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			var buf bytes.Buffer
			_ = goimg.Encode(&buf, imgOf(v), format)
			return &object.String{B: buf.Bytes()}
		})
	}
	encode("to_png", goimg.PNG)
	encode("to_jpeg", goimg.JPEG)

	// Operations with no error: image → image.
	unary := func(name string, f func(image.Image) *image.RGBA) {
		d(name, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
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
		d(name, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			x, _ := toFloat(args[0])
			return &Image{img: f(imgOf(v), x)}
		})
	}
	scalar("adjust_brightness", goimg.AdjustBrightness)
	scalar("adjust_contrast", goimg.AdjustContrast)

	// Operations that can fail.
	d("gaussian_blur", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s, _ := toFloat(args[0])
		return mustImg(goimg.GaussianBlur(imgOf(v), s))
	})
	d("box_blur", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustImg(goimg.BoxBlur(imgOf(v), int(intArg(args[0]))))
	})
	d("median", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustImg(goimg.Median(imgOf(v), int(intArg(args[0]))))
	})
	d("erode", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustImg(goimg.Erode(imgOf(v), int(intArg(args[0]))))
	})
	d("dilate", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustImg(goimg.Dilate(imgOf(v), int(intArg(args[0]))))
	})
	d("canny", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		sigma, _ := toFloat(args[0])
		low, _ := toFloat(args[1])
		high, _ := toFloat(args[2])
		return mustImg(goimg.Canny(imgOf(v), sigma, low, high))
	})
	d("resize", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustImg(goimg.Resize(imgOf(v), int(intArg(args[0])), int(intArg(args[1])), goimg.Bilinear))
	})
	d("crop", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		x, y := int(intArg(args[0])), int(intArg(args[1]))
		w, h := int(intArg(args[2])), int(intArg(args[3]))
		return mustImg(goimg.Crop(imgOf(v), image.Rect(x, y, x+w, y+h)))
	})

	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*Image).Inspect())
	})
	vm.cImage.define("to_s", vm.cImage.methods["inspect"].native)
}
