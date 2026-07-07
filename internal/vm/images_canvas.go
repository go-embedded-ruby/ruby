// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-ruby-images/images/canvas"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// ImagesCanvas is the Ruby wrapper around a *canvas.Canvas, the chunky_png-style
// mutable RGBA grid. Per-pixel access (#[] / #[]=), primitive drawing (points,
// lines, rectangles, circles), alpha compositing and PNG I/O are exposed as
// Images::Canvas methods; the pixels themselves are plain Integers packed
// 0xRRGGBBAA (see the Images::Color module). The drawing and PNG codec live in
// the github.com/go-ruby-images/images/canvas library.
type ImagesCanvas struct {
	c *canvas.Canvas
}

func (c *ImagesCanvas) ToS() string     { return "#<Images::Canvas>" }
func (c *ImagesCanvas) Inspect() string { return "#<Images::Canvas>" }
func (c *ImagesCanvas) Truthy() bool    { return true }

// canvasTransparent returns the fully transparent packed colour, the Images::New
// / Images::Canvas.new fill default.
func canvasTransparent() canvas.Color { return canvas.Transparent }

// registerImagesCanvas installs Images::Canvas: the chunky_png-style canvas with
// a fresh constructor (Canvas.new), PNG decoders (Canvas.decode / .from_blob and
// Canvas.load / .from_file) and the per-pixel, drawing and output instance
// methods.
func (vm *VM) registerImagesCanvas(mod *RClass) {
	cls := newClass("Images::Canvas", vm.cObject)
	mod.consts["Canvas"] = cls
	vm.consts["Images::Canvas"] = cls

	// Images::Canvas.new(width, height, fill = Images::Color::TRANSPARENT).
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			imagesArity(args, 2)
			fill := canvasTransparent()
			if len(args) > 2 {
				fill = imagesColorArg(args[2])
			}
			return &ImagesCanvas{c: canvas.New(int(intArg(args[0])), int(intArg(args[1])), fill)}
		}}

	// Images::Canvas.decode(blob) / .from_blob(blob) read a PNG String blob.
	decode := &Method{name: "decode", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			imagesArity(args, 1)
			return imagesCanvasWrap(canvas.DecodePNG(imagesBlobReader(args[0])))
		}}
	cls.smethods["decode"] = decode
	cls.smethods["from_blob"] = decode

	// Images::Canvas.load(path) / .from_file(path) read a PNG file.
	load := &Method{name: "load", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			imagesArity(args, 1)
			return imagesCanvasWrap(canvas.LoadPNG(strArg(args[0])))
		}}
	cls.smethods["load"] = load
	cls.smethods["from_file"] = load

	cls.define("width", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(imagesCanvasSelf(self).c.Width()))
	})
	cls.define("height", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(imagesCanvasSelf(self).c.Height()))
	})

	// Per-pixel access. #[] / #at reads (raising Images::OutOfBoundsError when the
	// coordinates fall outside the canvas); #[]= / #set / #point writes.
	at := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 2)
		return object.IntValue(int64(imagesCanvasAt(imagesCanvasSelf(self).c, int(intArg(args[0])), int(intArg(args[1])))))
	}
	cls.define("[]", at)
	cls.define("at", at)

	set := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 3)
		imagesCanvasSet(imagesCanvasSelf(self).c, int(intArg(args[0])), int(intArg(args[1])), imagesColorArg(args[2]))
		return args[2]
	}
	cls.define("[]=", set)
	cls.define("set", set)
	cls.define("point", set)

	// Drawing primitives (return self for chaining, as chunky_png does).
	cls.define("fill", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		imagesCanvasSelf(self).c.Fill(imagesColorArg(args[0]))
		return self
	})
	cls.define("line", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 5)
		imagesCanvasSelf(self).c.Line(int(intArg(args[0])), int(intArg(args[1])), int(intArg(args[2])), int(intArg(args[3])), imagesColorArg(args[4]))
		return self
	})
	cls.define("rect", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 6)
		imagesCanvasSelf(self).c.Rect(int(intArg(args[0])), int(intArg(args[1])), int(intArg(args[2])), int(intArg(args[3])), imagesColorArg(args[4]), imagesColorArg(args[5]))
		return self
	})
	cls.define("circle", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 4)
		imagesCanvasSelf(self).c.Circle(int(intArg(args[0])), int(intArg(args[1])), int(intArg(args[2])), imagesColorArg(args[3]))
		return self
	})
	cls.define("compose", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 3)
		imagesCanvasSelf(self).c.Compose(imagesCanvasSelf(args[0]).c, int(intArg(args[1])), int(intArg(args[2])))
		return self
	})

	// Output. #to_blob encodes the canvas as PNG bytes; #save writes a PNG file.
	cls.define("to_blob", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		blob, _ := imagesCanvasSelf(self).c.ToBlob()
		return object.NewStringBytesEnc(blob, "ASCII-8BIT")
	})
	cls.define("save", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		imagesArity(args, 1)
		if err := imagesCanvasSelf(self).c.SavePNG(strArg(args[0])); err != nil {
			raise("Images::Error", "%s", err.Error())
		}
		return self
	})
}

// registerImagesColor installs Images::Color, the chunky_png-style colour helper
// module. Colours are plain Integers packed 0xRRGGBBAA; the module builds them
// (rgb / rgba / from_hex), decomposes them (r / g / b / a / hex) and composites
// them (compose), and exposes the named-colour constants.
func (vm *VM) registerImagesColor(mod *RClass) {
	col := newClass("Images::Color", nil)
	col.isModule = true
	mod.consts["Color"] = col
	vm.consts["Images::Color"] = col

	col.consts["TRANSPARENT"] = imagesColorConst(canvas.Transparent)
	col.consts["BLACK"] = imagesColorConst(canvas.Black)
	col.consts["WHITE"] = imagesColorConst(canvas.White)
	col.consts["RED"] = imagesColorConst(canvas.Red)
	col.consts["GREEN"] = imagesColorConst(canvas.Green)
	col.consts["BLUE"] = imagesColorConst(canvas.Blue)

	col.smethods["rgb"] = &Method{name: "rgb", owner: col,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			imagesArity(args, 3)
			return imagesColorConst(canvas.RGB(uint8(intArg(args[0])), uint8(intArg(args[1])), uint8(intArg(args[2]))))
		}}
	col.smethods["rgba"] = &Method{name: "rgba", owner: col,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			imagesArity(args, 4)
			return imagesColorConst(canvas.RGBA(uint8(intArg(args[0])), uint8(intArg(args[1])), uint8(intArg(args[2])), uint8(intArg(args[3]))))
		}}
	col.smethods["from_hex"] = &Method{name: "from_hex", owner: col,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			imagesArity(args, 1)
			c, err := canvas.FromHex(strArg(args[0]))
			if err != nil {
				raise("ArgumentError", "%s", err.Error())
			}
			return imagesColorConst(c)
		}}
	col.smethods["compose"] = &Method{name: "compose", owner: col,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			imagesArity(args, 2)
			return imagesColorConst(canvas.Compose(imagesColorArg(args[0]), imagesColorArg(args[1])))
		}}

	comp := func(name string, fn func(canvas.Color) uint8) {
		col.smethods[name] = &Method{name: name, owner: col,
			native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				imagesArity(args, 1)
				return object.IntValue(int64(fn(imagesColorArg(args[0]))))
			}}
	}
	comp("r", canvas.Color.R)
	comp("g", canvas.Color.G)
	comp("b", canvas.Color.B)
	comp("a", canvas.Color.A)

	hex := &Method{name: "hex", owner: col,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			imagesArity(args, 1)
			return object.NewString(imagesColorArg(args[0]).Hex())
		}}
	col.smethods["hex"] = hex
	col.smethods["to_hex"] = hex
}
