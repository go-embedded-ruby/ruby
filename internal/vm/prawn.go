// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	prawn "github.com/go-ruby-prawn/prawn"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerPrawn installs the Prawn module (require "prawn"): Prawn::Document with
// its .new / .generate constructors and its text / font / color / graphics /
// image / table / page-management DSL, plus the Prawn::Errors exception tree. All
// PDF generation is delegated to github.com/go-ruby-prawn/prawn — a pure-Go,
// no-cgo reimplementation of Ruby's prawn gem over github.com/go-pdf/fpdf — so a
// PDF rbgo emits is a well-formed PDF 1.3+ document (a %PDF- header, cross-
// reference table and trailer) identical on every supported architecture. The
// Document value type and the Ruby↔Go argument conversions live in prawn_bind.go.
func (vm *VM) registerPrawn() {
	mod := newClass("Prawn", nil)
	mod.isModule = true
	vm.consts["Prawn"] = mod

	vm.registerPrawnErrors(mod)
	vm.registerPrawnDocument(mod)
}

// registerPrawnErrors installs the Prawn::Errors module and its exception tree
// mirroring the prawn gem: a Prawn::Errors::Error base (a StandardError) with the
// specific failures beneath it. Each class is registered both as a nested
// constant of Prawn::Errors and under its qualified name in the top-level table,
// so a re-raised library sentinel's exception lookup finds the same class, exactly
// as the Age and JWT error trees are.
func (vm *VM) registerPrawnErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	errs := newClass("Prawn::Errors", nil)
	errs.isModule = true
	mod.consts["Errors"] = errs
	vm.consts["Prawn::Errors"] = errs

	reg := func(name string, super *RClass) *RClass {
		qualified := "Prawn::Errors::" + name
		c := newClass(qualified, super)
		errs.consts[name] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Error", std)
	for _, name := range []string{
		"CannotFit", "UnknownFont", "InvalidPageLayout",
		"UnsupportedImageType", "EmptyGraphicStateStack", "IncompatibleStringEncoding",
	} {
		reg(name, base)
	}
}

// registerPrawnDocument installs Prawn::Document: the .new / .generate class
// methods and the whole instance DSL. The instance value type (PrawnDocument)
// wraps a *prawn.Document; each method threads the receiver's wrapped document
// into the library after converting its Ruby arguments (see prawn_bind.go).
func (vm *VM) registerPrawnDocument(mod *RClass) {
	cls := newClass("Prawn::Document", vm.cObject)
	mod.consts["Document"] = cls
	vm.consts["Prawn::Document"] = cls

	// Prawn::Document.new(page_size:, page_layout:, margin:, margins:) — a fresh
	// document with a single blank page and prawn's defaults (US Letter, portrait,
	// 36pt margins, Helvetica 12pt black).
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &PrawnDocument{cls: cls, d: prawn.New(prawnOptions(args))}
		}}

	// Prawn::Document.generate(path = nil, **opts) { |pdf| … } — the block-form
	// generator: build a document, yield it to the block so the block draws on it,
	// then (when a path String is given) write the rendered PDF to that path. The
	// document is returned so a caller can also #render it to bytes.
	cls.smethods["generate"] = &Method{name: "generate", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			path := ""
			var opts []object.Value
			for _, a := range args {
				if s, ok := a.(*object.String); ok {
					path = s.Str()
					continue
				}
				opts = append(opts, a)
			}
			doc := &PrawnDocument{cls: cls, d: prawn.New(prawnOptions(opts))}
			if blk != nil {
				vm.callBlock(blk, []object.Value{doc})
			}
			if path != "" {
				if err := doc.d.RenderFile(path); err != nil {
					raisePrawnError(err)
				}
			}
			return doc
		}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *PrawnDocument { return v.(*PrawnDocument) }

	// text(string, **opts) — flowing text that wraps to the margin box and advances
	// the cursor. opts mirror prawn's size:/style:/align:/leading:/color:/font:.
	d("text", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).d.Text(strArg(args[0]), prawnTextOptions(args[1:]))
		return object.NilV
	})

	// draw_text(string, at: [x, y], **opts) — draw the string once at an absolute
	// bounds-relative point, without wrapping and without moving the cursor.
	d("draw_text", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		h := prawnOptsHash(args[1:])
		x, y := prawnPointOpt(h, "at")
		self(v).d.DrawText(strArg(args[0]), x, y, prawnTextOptionsFromHash(h))
		return object.NilV
	})

	// font(name = nil, style: :normal) — set the font family (one of the 14 standard
	// PDF fonts) and optional style; with no name it reads the current family.
	d("font", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		doc := self(v)
		if len(args) == 0 || object.IsNil(args[0]) {
			return object.NewString(doc.d.FontFamily())
		}
		doc.d.Font(strArg(args[0]), prawnStyleFromHash(prawnOptsHash(args[1:])))
		return object.NilV
	})

	// font_size(n = nil) — set the current font size in points, or read it when
	// called with no argument (prawn's font_size setter/reader pair).
	d("font_size", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		doc := self(v)
		if len(args) == 0 {
			return object.Float(doc.d.FontSizeValue())
		}
		doc.d.FontSize(prawnFloat(args[0]))
		return object.NilV
	})

	// fill_color(hex = nil) — set the fill (text/shape) color as "RRGGBB", or read
	// the current one when called with no argument.
	d("fill_color", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		doc := self(v)
		if len(args) == 0 {
			return object.NewString(doc.d.FillColorValue())
		}
		doc.d.FillColor(strArg(args[0]))
		return object.NilV
	})

	// stroke_color(hex = nil) — set the stroke (line/outline) color as "RRGGBB", or
	// read the current one when called with no argument.
	d("stroke_color", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		doc := self(v)
		if len(args) == 0 {
			return object.NewString(doc.d.StrokeColorValue())
		}
		doc.d.StrokeColor(strArg(args[0]))
		return object.NilV
	})

	// line_width(n = nil) — set the stroke width in points, or read it when called
	// with no argument.
	d("line_width", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		doc := self(v)
		if len(args) == 0 {
			return object.Float(doc.d.LineWidthValue())
		}
		doc.d.LineWidth(prawnFloat(args[0]))
		return object.NilV
	})

	// rectangle([x, y], width, height) — add a rectangle whose top-left corner is
	// the bounds-relative point (x, y) to the pending path.
	d("rectangle", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		x, y := prawnPoint(args[0])
		self(v).d.Rectangle(x, y, prawnFloat(args[1]), prawnFloat(args[2]))
		return object.NilV
	})

	// line([x1, y1], [x2, y2]) — add a line segment between two bounds-relative
	// points to the pending path.
	d("line", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		x1, y1 := prawnPoint(args[0])
		x2, y2 := prawnPoint(args[1])
		self(v).d.Line(x1, y1, x2, y2)
		return object.NilV
	})

	// circle([x, y], radius) — add a circle centred on the bounds-relative point
	// (x, y) to the pending path.
	d("circle", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		x, y := prawnPoint(args[0])
		self(v).d.Circle(x, y, prawnFloat(args[1]))
		return object.NilV
	})

	// stroke(&blk) — outline the pending path; a block, when given, is run first so
	// `stroke { rectangle(...) }` builds and paints in one call.
	d("stroke", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk != nil {
			vm.callBlock(blk, nil)
		}
		self(v).d.Stroke()
		return object.NilV
	})

	// fill(&blk) — fill the pending path; a block, when given, is run first so
	// `fill { rectangle(...) }` builds and paints in one call.
	d("fill", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk != nil {
			vm.callBlock(blk, nil)
		}
		self(v).d.Fill()
		return object.NilV
	})

	// stroke_rectangle([x, y], width, height) — add a rectangle and outline it.
	d("stroke_rectangle", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		x, y := prawnPoint(args[0])
		self(v).d.StrokeRectangle(x, y, prawnFloat(args[1]), prawnFloat(args[2]))
		return object.NilV
	})

	// fill_rectangle([x, y], width, height) — add a rectangle and fill it.
	d("fill_rectangle", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		x, y := prawnPoint(args[0])
		self(v).d.FillRectangle(x, y, prawnFloat(args[1]), prawnFloat(args[2]))
		return object.NilV
	})

	// image(path, at:/width:/height:/fit:) — embed a PNG or JPEG image from a file.
	d("image", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).d.Image(strArg(args[0]), prawnImageOptions(prawnOptsHash(args[1:])))
		return object.NilV
	})

	// table(data, column_widths:/header:/cell_padding:/border_width:/font_size:/
	// row_height:) — draw a grid of string cells and advance the cursor past it.
	d("table", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).d.Table(prawnTableData(args[0]), prawnTableOptions(prawnOptsHash(args[1:])))
		return object.NilV
	})

	// move_down(n) / move_up(n) — lower / raise the cursor by n points.
	d("move_down", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).d.MoveDown(prawnFloat(args[0]))
		return object.NilV
	})
	d("move_up", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).d.MoveUp(prawnFloat(args[0]))
		return object.NilV
	})

	// move_cursor_to(y) — place the cursor at an absolute height within the margin
	// box.
	d("move_cursor_to", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).d.MoveCursorTo(prawnFloat(args[0]))
		return object.NilV
	})

	// cursor — the vertical cursor position measured from the bottom of the margin
	// box.
	d("cursor", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).d.Cursor())
	})

	// bounds — the current margin box as a Hash of {width:, height:, left:, bottom:}
	// in points (prawn exposes a bounds object; here its geometry is returned).
	d("bounds", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self(v).d.Bounds()
		h := object.NewHash()
		h.Set(object.Symbol("width"), object.Float(b.Width))
		h.Set(object.Symbol("height"), object.Float(b.Height))
		h.Set(object.Symbol("left"), object.Float(b.Left))
		h.Set(object.Symbol("bottom"), object.Float(b.Bottom))
		return h
	})

	// page_count — the number of pages in the document.
	d("page_count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).d.PageCount())
	})

	// start_new_page — begin a fresh page and reset the cursor to the top of the
	// margin box.
	d("start_new_page", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).d.StartNewPage()
		return object.NilV
	})

	// render — the finished PDF as a (binary) String. Any error accumulated while
	// building the document is raised here as the matching Prawn::Errors exception.
	d("render", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		b, err := self(v).d.Render()
		if err != nil {
			raisePrawnError(err)
		}
		return object.NewStringBytes(b)
	})

	// render_file(path) — render the PDF and write it to path.
	d("render_file", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).d.RenderFile(pathArg(vm, args[0])); err != nil {
			raisePrawnError(err)
		}
		return object.NilV
	})

	toS := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ToS())
	}
	d("to_s", toS)
	d("inspect", toS)
}
