// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rx "github.com/go-ruby-rexml/rexml"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and github.com/go-ruby-rexml/rexml — a pure-Go (CGO=0) reimplementation of the
// core of Ruby's "rexml" standard library (require "rexml/document"). The whole
// XML model lives in that library: the tree (Document / Element / Attribute /
// Text / Comment / CData / Instruction / DocType), the parser, the default
// compact serialiser and Formatters::Pretty, attribute single-quoting, source
// order vs. EachSorted, entity handling, and the supported XPath subset. rbgo
// only wraps the library's nodes in its Ruby REXML::* objects, maps option/index
// arguments and re-raises the library's *ParseError as REXML::ParseException.
//
// XPath subset (the boundary the library supports — documented on REXML::XPath
// below and mirrored in registerREXMLXPath): child/descendant steps, text(),
// and predicates [n] (1-based position), [@attr], [@attr='value'] /
// [@attr="value"], and the boolean attribute-existence test. Out of scope:
// other axes (ancestor, following-sibling, …), functions beyond text()
// (count(), position(), last(), name(), contains(), …), arithmetic and nested
// boolean predicates (and/or). The "/@attr" attribute step is supported and
// yields the attribute's decoded value via REXML's AttrValue convenience.

// REXMLNode is the common interface of every REXML::* wrapper: it carries the
// underlying library node and renders Ruby #to_s / #inspect. classOf reports the
// concrete Ruby class (REXML::Element, REXML::Text, …) for each wrapper.
type REXMLNode interface {
	object.Value
	node() rx.Node
}

// REXMLDocument wraps a *rexml.Document — REXML::Document, the tree root.
type REXMLDocument struct{ d *rx.Document }

func (n *REXMLDocument) node() rx.Node { return n.d }
func (n *REXMLDocument) Truthy() bool  { return true }
func (n *REXMLDocument) ToS() string   { return n.d.ToString() }

// Inspect mirrors MRI's Document#inspect: the root is always shown as the
// placeholder "<UNDEFINED>" — "<UNDEFINED> ... </>" when the document has a root
// element, "<UNDEFINED/>" for an empty document.
func (n *REXMLDocument) Inspect() string {
	if n.d.Root() != nil {
		return "<UNDEFINED> ... </>"
	}
	return "<UNDEFINED/>"
}

// REXMLElement wraps a *rexml.Element — REXML::Element.
type REXMLElement struct{ e *rx.Element }

func (n *REXMLElement) node() rx.Node { return n.e }
func (n *REXMLElement) Truthy() bool  { return true }
func (n *REXMLElement) ToS() string   { return elementString(n.e) }

// Inspect mirrors MRI's Element#inspect: "<qname attrs> ... </>" for an element
// with children, "<qname attrs/>" for an empty one.
func (n *REXMLElement) Inspect() string {
	s := "<" + n.e.QName()
	n.e.Attributes.Each(func(a *rx.Attribute) {
		s += " " + a.QName() + "='" + a.Value + "'"
	})
	if len(n.e.Children) == 0 {
		return s + "/>"
	}
	return s + "> ... </>"
}

// elementString serialises a single element to its REXML compact form. The
// library serialises through a Document, so the element is hung off a fresh
// throwaway Document; this reproduces MRI's Element#to_s byte-for-byte.
func elementString(e *rx.Element) string {
	d := rx.NewDocument()
	d.Add(e)
	return d.ToString()
}

// REXMLText wraps a *rexml.Text — REXML::Text. #to_s yields the escaped (raw)
// form, #value the decoded form, matching MRI.
type REXMLText struct{ t *rx.Text }

func (n *REXMLText) node() rx.Node { return n.t }
func (n *REXMLText) Truthy() bool  { return true }
func (n *REXMLText) ToS() string   { return n.t.String() }
func (n *REXMLText) Inspect() string {
	return "#<REXML::Text: " + n.t.String() + ">"
}

// REXMLComment wraps a *rexml.Comment — REXML::Comment. #to_s is its verbatim
// content (MRI's Comment#to_s, without the <!-- --> delimiters).
type REXMLComment struct{ c *rx.Comment }

func (n *REXMLComment) node() rx.Node { return n.c }
func (n *REXMLComment) Truthy() bool  { return true }
func (n *REXMLComment) ToS() string   { return n.c.Value }
func (n *REXMLComment) Inspect() string {
	return "#<REXML::Comment: " + n.c.Value + ">"
}

// REXMLCData wraps a *rexml.CData — REXML::CData. #to_s is its verbatim content.
type REXMLCData struct{ c *rx.CData }

func (n *REXMLCData) node() rx.Node { return n.c }
func (n *REXMLCData) Truthy() bool  { return true }
func (n *REXMLCData) ToS() string   { return n.c.Value }
func (n *REXMLCData) Inspect() string {
	return "#<REXML::CData: " + n.c.Value + ">"
}

// REXMLInstruction wraps a *rexml.Instruction — REXML::Instruction (a PI).
type REXMLInstruction struct{ i *rx.Instruction }

func (n *REXMLInstruction) node() rx.Node { return n.i }
func (n *REXMLInstruction) Truthy() bool  { return true }
func (n *REXMLInstruction) ToS() string {
	if n.i.Content == "" {
		return "<?" + n.i.Target + "?>"
	}
	return "<?" + n.i.Target + " " + n.i.Content + "?>"
}
func (n *REXMLInstruction) Inspect() string { return n.ToS() }

// REXMLDocType wraps a *rexml.DocType — REXML::DocType. It is a leaf value node:
// it does not implement REXMLNode's node(), since the supported XPath subset
// does not take a DocType as a context (passing one to XPath raises TypeError).
type REXMLDocType struct{ dt *rx.DocType }

func (n *REXMLDocType) Truthy() bool    { return true }
func (n *REXMLDocType) ToS() string     { return "<!DOCTYPE " + n.dt.Body + ">" }
func (n *REXMLDocType) Inspect() string { return n.ToS() }

// REXMLAttributes wraps an *rexml.Attributes (and its owning element, so [] and
// each can resolve qualified names) — REXML::Attributes.
type REXMLAttributes struct{ a *rx.Attributes }

func (n *REXMLAttributes) Truthy() bool    { return true }
func (n *REXMLAttributes) ToS() string     { return "attributes" }
func (n *REXMLAttributes) Inspect() string { return "#<REXML::Attributes>" }

// REXMLElements is the proxy REXML::Element#elements returns — REXML::Elements.
// It separates child-element navigation (elements[path] / each / add) from the
// element's own [] (which reads an attribute). It carries the owning element.
type REXMLElements struct{ e *rx.Element }

func (n *REXMLElements) Truthy() bool    { return true }
func (n *REXMLElements) ToS() string     { return "elements" }
func (n *REXMLElements) Inspect() string { return "#<REXML::Elements>" }

// wrapNode lifts a library tree node to its Ruby REXML::* wrapper, returning nil
// for anything that is not a wrappable tree node (notably the internal attribute
// holder an XPath "@attr" step yields, which the caller lifts to its value via
// AttrValue instead). Within the XPath subset and the doctype accessor the
// reachable node kinds are Element, Text and DocType; any other input reads as
// nil in Ruby.
func wrapNode(n rx.Node) object.Value {
	switch x := n.(type) {
	case *rx.Element:
		return &REXMLElement{e: x}
	case *rx.Text:
		return &REXMLText{t: x}
	case *rx.DocType:
		return &REXMLDocType{dt: x}
	}
	return object.NilV
}

// raiseREXMLParse re-raises a library parse failure as REXML::ParseException
// (MRI's REXML::ParseException, raised by Document.new on malformed input),
// reproducing the library's message verbatim. It never returns when err != nil.
func raiseREXMLParse(err error) {
	if err == nil {
		return
	}
	raise("REXML::ParseException", "%s", err.Error())
}

// registerREXML installs the REXML module (require "rexml" / "rexml/document")
// backed by go-ruby-rexml: the Document / Element / Attributes value classes,
// the Text / Comment / CData / Instruction / DocType node classes, the nested
// Formatters::Pretty serialiser, the XPath module, and the REXML::ParseException
// exception. It runs after the exception hierarchy is in place (ParseException <
// StandardError).
func (vm *VM) registerREXML() {
	mod := newClass("REXML", vm.cObject)
	vm.cREXML = mod
	vm.consts["REXML"] = mod

	vm.registerREXMLError(mod)
	vm.registerREXMLDocument(mod)
	vm.registerREXMLElement(mod)
	vm.registerREXMLElements(mod)
	vm.registerREXMLAttributes(mod)
	vm.registerREXMLLeafNodes(mod)
	vm.registerREXMLFormatters(mod)
	vm.registerREXMLXPath(mod)
}

// registerREXMLError installs REXML::ParseException < StandardError, registered
// both as a nested constant of REXML (so Ruby `REXML::ParseException` resolves
// it) and under its qualified top-level name (so a re-raised library error's
// class lookup finds the same class), exactly as CSV::MalformedCSVError is.
func (vm *VM) registerREXMLError(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	pe := newClass("REXML::ParseException", std)
	mod.consts["ParseException"] = pe
	vm.consts["REXML::ParseException"] = pe
	vm.cREXMLParseException = pe
}

// registerREXMLDocument installs REXML::Document: the constructor (parsing a
// String, wrapping an existing Document, or an empty tree), root access, and the
// to_s / write serialisers (compact by default, Pretty when an indent is given).
func (vm *VM) registerREXMLDocument(mod *RClass) {
	cls := newClass("REXML::Document", vm.cObject)
	mod.consts["Document"] = cls
	vm.consts["REXML::Document"] = cls
	vm.cREXMLDocument = cls

	// REXML::Document.new(source=nil): an empty document, or one parsed from an
	// XML String (a malformed String raises REXML::ParseException). A Document or
	// Element source seeds the new tree, as MRI's Document.new accepts.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				return &REXMLDocument{d: rx.NewDocument()}
			}
			switch src := args[0].(type) {
			case object.Nil:
				return &REXMLDocument{d: rx.NewDocument()}
			case *object.String:
				d, err := rx.ParseDocument(src.Str())
				raiseREXMLParse(err)
				return &REXMLDocument{d: d}
			case *REXMLDocument:
				d := rx.NewDocument()
				if r := src.d.Root(); r != nil {
					d.Add(r)
				}
				return &REXMLDocument{d: d}
			case *REXMLElement:
				d := rx.NewDocument()
				d.Add(src.e)
				return &REXMLDocument{d: d}
			}
			raise("TypeError", "wrong argument type %s (expected String)", args[0].Inspect())
			return object.NilV
		}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	doc := func(v object.Value) *rx.Document { return v.(*REXMLDocument).d }

	d("root", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		r := doc(v).Root()
		if r == nil {
			return object.NilV
		}
		return &REXMLElement{e: r}
	})
	// doctype: the REXML::DocType node, or nil when the document has none.
	d("doctype", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		dt := doc(v).DocType()
		if dt == nil {
			return object.NilV
		}
		return wrapNode(dt)
	})
	// add_element(name): create and append a root element, returning it.
	d("add_element", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &REXMLElement{e: doc(v).AddElement(strArg(args[0]))}
	})

	// to_s / write([io], indent): serialise. With no indent (or indent -1) the
	// compact default formatter is used; a non-negative indent selects Pretty.
	// write writes to an io-like object (anything responding to <<) when given.
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(doc(v).ToString())
	})
	// write(output=nil, indent=-1): MRI's Document#write — positional output
	// (an io-like object responding to <<, or a mutable String) then indent.
	// A non-negative indent selects the Pretty formatter; otherwise the compact
	// default. With an output the rendering is appended to it (write returns
	// nil); without one it is returned as a String (a binding convenience, since
	// MRI's default output is $stdout).
	d("write", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := rexmlWriteString(doc(v), args)
		if io := rexmlIOArg(args); !object.IsNil(io) {
			vm.send(io, "<<", []object.Value{object.NewString(s)}, nil)
			return object.NilV
		}
		return object.NewString(s)
	})
}

// rexmlWriteString renders a Document per a Document#write argument list: the
// indent is the second positional argument (or the first when it is the only
// one and is an Integer); a non-negative indent selects the Pretty formatter,
// otherwise the compact default.
func rexmlWriteString(d *rx.Document, args []object.Value) string {
	if n, ok := rexmlIndentArg(args); ok && n >= 0 {
		return rx.PrettyString(d, n)
	}
	return d.ToString()
}

// rexmlIndentArg extracts the indent: the second positional argument when given,
// else the first when it is the only argument and an Integer (write(indent)).
func rexmlIndentArg(args []object.Value) (int, bool) {
	if len(args) >= 2 {
		if n, ok := args[1].(object.Integer); ok {
			return int(n), true
		}
		return 0, false
	}
	if len(args) == 1 {
		if n, ok := args[0].(object.Integer); ok {
			return int(n), true
		}
	}
	return 0, false
}

// rexmlIOArg returns the output argument of Document#write — the first
// positional argument when it is an io-like object (not an Integer indent and
// not nil) — or nil when write should return the rendered String.
func rexmlIOArg(args []object.Value) object.Value {
	if len(args) == 0 {
		return object.NilVal()
	}
	switch args[0].(type) {
	case object.Integer, object.Nil:
		return object.NilVal()
	}
	return args[0]
}

// registerREXMLElement installs REXML::Element: name/text access, attribute
// reading and mutation, child-element navigation (elements[path], each_element,
// add_element) and text mutation (add_text).
func (vm *VM) registerREXMLElement(mod *RClass) {
	cls := newClass("REXML::Element", vm.cObject)
	mod.consts["Element"] = cls
	vm.consts["REXML::Element"] = cls
	vm.cREXMLElement = cls

	// REXML::Element.new(name): a new, detached element.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			name := "UNDEFINED"
			if len(args) > 0 {
				name = strArg(args[0])
			}
			return &REXMLElement{e: rx.NewElement(name)}
		}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	elem := func(v object.Value) *rx.Element { return v.(*REXMLElement).e }

	d("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(elem(v).Name)
	})
	d("expanded_name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(elem(v).ExpandedName())
	})
	d("text", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		e := elem(v)
		if !e.HasText() {
			return object.NilV
		}
		return object.NewString(e.Text())
	})
	d("attributes", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &REXMLAttributes{a: elem(v).Attributes}
	})

	// [](key): an Integer index selects the n-th child element (0-based, MRI's
	// Element#[Integer]); a name/Symbol reads the attribute of that name.
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		e := elem(v)
		if n, ok := args[0].(object.Integer); ok {
			// Element#[Integer] is 0-based; the library's ElementAt is 1-based.
			ch := e.ElementAt(int(n) + 1)
			if ch == nil {
				return object.NilV
			}
			return &REXMLElement{e: ch}
		}
		if val, ok := e.Attr(rexmlNameArg(args[0])); ok {
			return object.NewString(val)
		}
		return object.NilV
	})

	// elements: the REXML::Elements proxy for child-element navigation (so
	// element.elements[path] does an XPath lookup, distinct from element[name]
	// which reads an attribute).
	d("elements", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &REXMLElements{e: elem(v)}
	})

	// each_element([path]): yield each matching child element (all children with
	// no path), returning the receiver.
	d("each_element", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		e := elem(v)
		var kids []*rx.Element
		if len(args) > 0 {
			kids = e.Elements(strArg(args[0]))
		} else {
			kids = e.ChildElements()
		}
		for _, ch := range kids {
			vm.callBlock(blk, []object.Value{&REXMLElement{e: ch}})
		}
		return v
	})

	d("add_element", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &REXMLElement{e: elem(v).AddElement(strArg(args[0]))}
	})
	d("add_attribute", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		elem(v).AddAttribute(strArg(args[0]), strArg(args[1]))
		return object.NewString(strArg(args[1]))
	})
	d("add_text", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		elem(v).AddText(strArg(args[0]))
		return v
	})

	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(elementString(elem(v)))
	})
}

// registerREXMLElements installs REXML::Elements, the proxy Element#elements
// returns: [] (first match of an XPath path, or the n-th 1-based child for an
// Integer), each([path]), size and add_element.
func (vm *VM) registerREXMLElements(mod *RClass) {
	cls := newClass("REXML::Elements", vm.cObject)
	mod.consts["Elements"] = cls
	vm.consts["REXML::Elements"] = cls
	vm.cREXMLElements = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	owner := func(v object.Value) *rx.Element { return v.(*REXMLElements).e }

	// [](path|index): an XPath path returns its first match; an Integer index
	// (1-based, MRI's Elements#[]) returns the n-th child element; nil when none.
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		e := owner(v)
		if n, ok := args[0].(object.Integer); ok {
			// Elements#[Integer] is 1-based, matching the library's ElementAt.
			ch := e.ElementAt(int(n))
			if ch == nil {
				return object.NilV
			}
			return &REXMLElement{e: ch}
		}
		ch := e.FirstElement(strArg(args[0]))
		if ch == nil {
			return object.NilV
		}
		return &REXMLElement{e: ch}
	})

	// each([path]): yield each matching child element (all children with no path).
	d("each", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		e := owner(v)
		var kids []*rx.Element
		if len(args) > 0 {
			kids = e.Elements(strArg(args[0]))
		} else {
			kids = e.ChildElements()
		}
		for _, ch := range kids {
			vm.callBlock(blk, []object.Value{&REXMLElement{e: ch}})
		}
		return v
	})

	d("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(owner(v).ChildElements())))
	})
	d("add_element", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &REXMLElement{e: owner(v).AddElement(strArg(args[0]))}
	})
	d("add", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &REXMLElement{e: owner(v).AddElement(strArg(args[0]))}
	})
}

// rexmlNameArg coerces an attribute-name argument (String or Symbol) to its text.
func rexmlNameArg(v object.Value) string {
	switch x := v.(type) {
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	}
	return ""
}

// registerREXMLAttributes installs REXML::Attributes: by-name read ([]), set,
// delete, size and iteration (each / each_attribute).
func (vm *VM) registerREXMLAttributes(mod *RClass) {
	cls := newClass("REXML::Attributes", vm.cObject)
	mod.consts["Attributes"] = cls
	vm.consts["REXML::Attributes"] = cls
	vm.cREXMLAttributes = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	attrs := func(v object.Value) *rx.Attributes { return v.(*REXMLAttributes).a }

	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if val, ok := attrs(v).Get(rexmlNameArg(args[0])); ok {
			return object.NewString(val)
		}
		return object.NilV
	})
	d("[]=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		attrs(v).Set(rexmlNameArg(args[0]), strArg(args[1]))
		return args[1]
	})
	d("delete", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		attrs(v).Delete(rexmlNameArg(args[0]))
		return v
	})
	sizeFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(attrs(v).Len()))
	}
	d("size", sizeFn)
	d("length", sizeFn)

	// each / each_attribute: yield [name, value] pairs in source order, returning
	// the receiver. (each_attribute yields the attribute name/value as MRI does.)
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		attrs(v).Each(func(a *rx.Attribute) {
			pair := &object.Array{Elems: []object.Value{
				object.NewString(a.QName()), object.NewString(a.Value)}}
			vm.callBlock(blk, []object.Value{pair})
		})
		return v
	})
	d("each_attribute", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		attrs(v).Each(func(a *rx.Attribute) {
			vm.callBlock(blk, []object.Value{object.NewString(a.Value)})
		})
		return v
	})
}

// registerREXMLLeafNodes installs the REXML::Text / Comment / CData / Instruction
// / DocType value classes with their constructors and #to_s / #value readers.
func (vm *VM) registerREXMLLeafNodes(mod *RClass) {
	// REXML::Text
	text := newClass("REXML::Text", vm.cObject)
	mod.consts["Text"] = text
	vm.consts["REXML::Text"] = text
	vm.cREXMLText = text
	text.smethods["new"] = &Method{name: "new", owner: text,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &REXMLText{t: rx.NewText(strArg(args[0]))}
		}}
	text.define("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*REXMLText).t.String())
	})
	textValue := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*REXMLText).t.Val())
	}
	text.define("value", textValue)
	text.define("to_string", textValue)

	// REXML::Comment
	comment := newClass("REXML::Comment", vm.cObject)
	mod.consts["Comment"] = comment
	vm.consts["REXML::Comment"] = comment
	vm.cREXMLComment = comment
	comment.smethods["new"] = &Method{name: "new", owner: comment,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &REXMLComment{c: &rx.Comment{Value: strArg(args[0])}}
		}}
	commentVal := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*REXMLComment).c.Value)
	}
	comment.define("to_s", commentVal)
	comment.define("string", commentVal)

	// REXML::CData
	cdata := newClass("REXML::CData", vm.cObject)
	mod.consts["CData"] = cdata
	vm.consts["REXML::CData"] = cdata
	vm.cREXMLCData = cdata
	cdata.smethods["new"] = &Method{name: "new", owner: cdata,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &REXMLCData{c: &rx.CData{Value: strArg(args[0])}}
		}}
	cdataVal := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*REXMLCData).c.Value)
	}
	cdata.define("to_s", cdataVal)
	cdata.define("value", cdataVal)

	// REXML::Instruction
	instr := newClass("REXML::Instruction", vm.cObject)
	mod.consts["Instruction"] = instr
	vm.consts["REXML::Instruction"] = instr
	vm.cREXMLInstruction = instr
	instr.smethods["new"] = &Method{name: "new", owner: instr,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			target := strArg(args[0])
			content := ""
			if len(args) > 1 {
				content = strArg(args[1])
			}
			return &REXMLInstruction{i: &rx.Instruction{Target: target, Content: content}}
		}}
	instr.define("target", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*REXMLInstruction).i.Target)
	})
	instr.define("content", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*REXMLInstruction).i.Content)
	})
	instr.define("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*REXMLInstruction).ToS())
	})

	// REXML::DocType
	doctype := newClass("REXML::DocType", vm.cObject)
	mod.consts["DocType"] = doctype
	vm.consts["REXML::DocType"] = doctype
	vm.cREXMLDocType = doctype
	doctype.define("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*REXMLDocType).ToS())
	})
}

// registerREXMLFormatters installs the nested REXML::Formatters module and its
// Pretty serialiser (REXML::Formatters::Pretty), mirroring the CSV::Row nested
// const pattern. Pretty.new(indent) keeps the indent; #write(doc, io) writes the
// pretty rendering to io (anything responding to <<) and #write(doc) returns it.
func (vm *VM) registerREXMLFormatters(mod *RClass) {
	formatters := newClass("REXML::Formatters", vm.cObject)
	mod.consts["Formatters"] = formatters
	vm.consts["REXML::Formatters"] = formatters

	pretty := newClass("REXML::Formatters::Pretty", vm.cObject)
	formatters.consts["Pretty"] = pretty
	vm.consts["REXML::Formatters::Pretty"] = pretty
	vm.cREXMLPretty = pretty

	// Pretty.new(indentation=2): store the indent in @indentation. A new Pretty
	// is always a plain RObject of this class (so Pretty#write reads @indentation
	// directly); a non-Integer indentation defaults to 2.
	pretty.smethods["new"] = &Method{name: "new", owner: pretty,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			indent := 2
			if len(args) > 0 {
				if n, ok := args[0].(object.Integer); ok {
					indent = int(n)
				}
			}
			obj := &RObject{class: pretty, ivars: map[string]object.Value{}}
			obj.ivars["@indentation"] = object.IntValue(int64(indent))
			return obj
		}}

	// Pretty#write(doc, output=nil): render doc with the stored indent. With an
	// output (responding to <<) the rendering is appended and output returned;
	// without one the String is returned. A non-Document argument raises
	// ArgumentError, matching MRI's Formatters::Pretty#write.
	pretty.define("write", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// @indentation is always the Integer the constructor stored.
		indent := int(self.(*RObject).ivars["@indentation"].(object.Integer))
		dv, ok := args[0].(*REXMLDocument)
		if !ok {
			raise("ArgumentError", "Formatters::Pretty#write expects a REXML::Document")
		}
		s := rx.PrettyString(dv.d, indent)
		if len(args) > 1 {
			vm.send(args[1], "<<", []object.Value{object.NewString(s)}, nil)
			return args[1]
		}
		return object.NewString(s)
	})
}

// registerREXMLXPath installs the nested REXML::XPath module — first / each /
// match over the library's supported XPath subset (see the boundary documented
// at the top of this file). An "@attr" attribute step yields the decoded value
// (a String) via REXML's AttrValue; an element step yields the wrapped element.
func (vm *VM) registerREXMLXPath(mod *RClass) {
	xpath := newClass("REXML::XPath", vm.cObject)
	mod.consts["XPath"] = xpath
	vm.consts["REXML::XPath"] = xpath
	vm.cREXMLXPath = xpath

	sm := func(name string, fn NativeFn) { xpath.smethods[name] = &Method{name: name, owner: xpath, native: fn} }

	// XPath.first(node, path): the first match (an Element wrapper, or a String
	// for an @attr step), or nil when nothing matches.
	sm("first", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ctx := rexmlCtx(args[0])
		path := strArg(args[1])
		n := rx.XPathFirst(ctx, path)
		if n == nil {
			return object.NilV
		}
		return rexmlMatchValue(n)
	})
	// XPath.match(node, path): an Array of every match.
	sm("match", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ctx := rexmlCtx(args[0])
		path := strArg(args[1])
		out := make([]object.Value, 0)
		for _, n := range rx.XPathMatch(ctx, path) {
			out = append(out, rexmlMatchValue(n))
		}
		return &object.Array{Elems: out}
	})
	// XPath.each(node, path) { |n| ... }: yield each match, returning nil.
	sm("each", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		ctx := rexmlCtx(args[0])
		path := strArg(args[1])
		rx.XPathEach(ctx, path, func(n rx.Node) {
			vm.callBlock(blk, []object.Value{rexmlMatchValue(n)})
		})
		return object.NilV
	})
}

// rexmlCtx extracts the library node a Ruby REXML::* wrapper holds, to serve as
// an XPath context. A non-node raises TypeError.
func rexmlCtx(v object.Value) rx.Node {
	if n, ok := v.(REXMLNode); ok {
		return n.node()
	}
	raise("TypeError", "XPath context must be a REXML node")
	return nil
}

// rexmlMatchValue lifts an XPath match to a Ruby value: a tree node (Element /
// Text / DocType in the supported subset) is wrapped, while an attribute step
// ("@attr") result — which wrapNode does not wrap — is lifted to its decoded
// value String via REXML's AttrValue.
func rexmlMatchValue(n rx.Node) object.Value {
	if w := wrapNode(n); w != object.NilV {
		return w
	}
	return object.NewString(rx.AttrValue(n))
}
