// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	nokogiri "github.com/go-ruby-nokogiri/nokogiri"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// NokogiriDocument is the Ruby wrapper around a *nokogiri.Document — a parsed
// HTML or XML tree (Nokogiri::HTML::Document / Nokogiri::XML::Document). The HTML
// and XML parsers, the CSS-to-XPath translator and the XPath engine all live in
// the github.com/go-ruby-nokogiri/nokogiri library (pure-Go, no cgo, no libxml2);
// this shell is the thin wiring that maps a Ruby markup String to a Document and
// exposes the node-navigation and query surface (see nokogiri_bind.go).
type NokogiriDocument struct {
	doc *nokogiri.Document
}

func (d *NokogiriDocument) ToS() string     { return d.doc.ToS() }
func (d *NokogiriDocument) Inspect() string { return "#<Nokogiri::XML::Document>" }
func (d *NokogiriDocument) Truthy() bool    { return true }

// NokogiriNode is the Ruby wrapper around a *nokogiri.Node — one DOM node
// (Nokogiri::XML::Node): an element, text, comment, CDATA or PI.
type NokogiriNode struct {
	n *nokogiri.Node
}

func (n *NokogiriNode) ToS() string     { return n.n.ToS() }
func (n *NokogiriNode) Inspect() string { return "#<Nokogiri::XML::Node>" }
func (n *NokogiriNode) Truthy() bool    { return true }

// NokogiriNodeSet is the Ruby wrapper around a *nokogiri.NodeSet — an ordered set
// of nodes (Nokogiri::XML::NodeSet), the result of a #css / #xpath query.
type NokogiriNodeSet struct {
	set *nokogiri.NodeSet
}

func (s *NokogiriNodeSet) ToS() string     { return "#<Nokogiri::XML::NodeSet>" }
func (s *NokogiriNodeSet) Inspect() string { return "#<Nokogiri::XML::NodeSet>" }
func (s *NokogiriNodeSet) Truthy() bool    { return true }

// registerNokogiri installs the Nokogiri module and its Document / Node / NodeSet
// classes (require "nokogiri"): Nokogiri::HTML(str) / Nokogiri::XML(str) parse a
// String into a Document; a Node answers #css / #at_css / #xpath / #at_xpath,
// #text / #content / #inner_html / #to_html / #to_xml, #[] attribute access,
// #name, and tree navigation; a NodeSet is Enumerable-ish (#each / #[] / #length /
// #first / #last / #text). The Nokogiri::SyntaxError tree is registered so a
// malformed-document error rescues as the gem class.
func (vm *VM) registerNokogiri() {
	mod := newClass("Nokogiri", nil)
	mod.isModule = true
	vm.consts["Nokogiri"] = object.Wrap(mod)
	vm.registerNokogiriErrors(mod)
	vm.registerNokogiriDocument(mod)
	vm.registerNokogiriNode(mod)
	vm.registerNokogiriNodeSet(mod)

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// Nokogiri::HTML(str) / Nokogiri.HTML(str) parses an HTML document.
	def("HTML", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return nokogiriParseHTML(nokogiriSourceArg(args))
	})
	// Nokogiri::XML(str) / Nokogiri.XML(str) parses an XML document.
	def("XML", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return nokogiriParseXML(nokogiriSourceArg(args))
	})
}

// registerNokogiriErrors installs Nokogiri::SyntaxError < StandardError (the gem
// raises it for a malformed document). It is registered both nested under
// Nokogiri and under its qualified name so a raised error resolves to the same
// class.
func (vm *VM) registerNokogiriErrors(mod *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	synErr := newClass("Nokogiri::SyntaxError", std)
	mod.consts["SyntaxError"] = object.Wrap(synErr)
	vm.consts["Nokogiri::SyntaxError"] = object.Wrap(synErr)

	// Nokogiri::XML::SyntaxError is the same class re-exposed under the XML module
	// namespace, matching the gem.
	xml := newClass("Nokogiri::XML", nil)
	xml.isModule = true
	mod.consts["XML"] = object.Wrap(xml)
	vm.consts["Nokogiri::XML"] = object.Wrap(xml)
	xml.consts["SyntaxError"] = object.Wrap(synErr)
	vm.consts["Nokogiri::XML::SyntaxError"] = object.Wrap(synErr)

	html := newClass("Nokogiri::HTML", nil)
	html.isModule = true
	mod.consts["HTML"] = object.Wrap(html)
	vm.consts["Nokogiri::HTML"] = object.Wrap(html)
}

// registerNokogiriDocument installs Nokogiri::XML::Document (also the class of an
// HTML document) and its query / navigation methods.
func (vm *VM) registerNokogiriDocument(mod *RClass) {
	xml := object.Kind[*RClass](mod.consts["XML"])
	cls := newClass("Nokogiri::XML::Document", vm.cObject)
	xml.consts["Document"] = object.Wrap(cls)
	vm.consts["Nokogiri::XML::Document"] = object.Wrap(cls)

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *nokogiri.Document { return object.Kind[*NokogiriDocument](v).doc }

	// #css / #at_css run a CSS query rooted at the document.
	d("css", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		set, err := self(v).CSS(nokogiriSelectorArg(args))
		if err != nil {
			raiseNokogiriError(err)
		}
		return object.Wrap(&NokogiriNodeSet{set: set})
	})
	d("at_css", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		node, err := self(v).AtCSS(nokogiriSelectorArg(args))
		if err != nil {
			raiseNokogiriError(err)
		}
		return nokogiriNodeOrNil(node)
	})
	// #xpath / #at_xpath run an XPath query rooted at the document.
	d("xpath", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		set, err := self(v).XPath(nokogiriSelectorArg(args))
		if err != nil {
			raiseNokogiriError(err)
		}
		return object.Wrap(&NokogiriNodeSet{set: set})
	})
	d("at_xpath", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		node, err := self(v).AtXPath(nokogiriSelectorArg(args))
		if err != nil {
			raiseNokogiriError(err)
		}
		return nokogiriNodeOrNil(node)
	})
	// #root returns the document's root element (or nil for an empty document).
	d("root", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nokogiriNodeOrNil(self(v).Root())
	})
	// #text / #content / #to_html / #to_xml / #to_s serialise the document.
	d("text", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Text()))
	})
	d("content", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Text()))
	})
	d("to_html", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).ToHTML()))
	})
	d("to_xml", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).ToXML()))
	})
	d("to_s", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).ToS()))
	})
}

// registerNokogiriNode installs Nokogiri::XML::Node and its navigation, query,
// serialization and attribute methods.
func (vm *VM) registerNokogiriNode(mod *RClass) {
	xml := object.Kind[*RClass](mod.consts["XML"])
	cls := newClass("Nokogiri::XML::Node", vm.cObject)
	xml.consts["Node"] = object.Wrap(cls)
	vm.consts["Nokogiri::XML::Node"] = object.Wrap(cls)

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *nokogiri.Node { return object.Kind[*NokogiriNode](v).n }

	// #css / #at_css / #xpath / #at_xpath run rooted at this node.
	d("css", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		set, err := self(v).CSS(nokogiriSelectorArg(args), nil)
		if err != nil {
			raiseNokogiriError(err)
		}
		return object.Wrap(&NokogiriNodeSet{set: set})
	})
	d("at_css", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		node, err := self(v).AtCSS(nokogiriSelectorArg(args), nil)
		if err != nil {
			raiseNokogiriError(err)
		}
		return nokogiriNodeOrNil(node)
	})
	d("xpath", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		set, err := self(v).XPath(nokogiriSelectorArg(args), nil)
		if err != nil {
			raiseNokogiriError(err)
		}
		return object.Wrap(&NokogiriNodeSet{set: set})
	})
	d("at_xpath", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		node, err := self(v).AtXPath(nokogiriSelectorArg(args), nil)
		if err != nil {
			raiseNokogiriError(err)
		}
		return nokogiriNodeOrNil(node)
	})

	// #text / #content / #inner_text return the descendant character data.
	text := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Text()))
	}
	d("text", text)
	d("content", text)
	d("inner_text", text)

	// #name / #node_name return the element/PI name.
	name := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).NodeName()))
	}
	d("name", name)
	d("node_name", name)

	// Serialisation.
	d("to_html", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).ToHTML()))
	})
	d("to_xml", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).ToXML()))
	})
	d("to_s", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).ToS()))
	})
	d("inner_html", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).InnerHTML()))
	})
	d("inner_xml", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).InnerXML()))
	})

	// #[] reads an attribute value (nil when absent); #[]= sets one.
	d("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		key := nokogiriStr(args[0])
		if val, ok := self(v).Get(key); ok {
			return object.Wrap(object.NewString(val))
		}
		return object.NilVal()
	})
	d("[]=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).SetAttribute(nokogiriStr(args[0]), nokogiriStr(args[1]))
		return args[1]
	})
	d("attribute", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if val, ok := self(v).Get(nokogiriStr(args[0])); ok {
			return object.Wrap(object.NewString(val))
		}
		return object.NilVal()
	})
	// #attributes returns a Hash of attribute name => value.
	d("attributes", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		for _, a := range self(v).Attrs {
			h.Set(object.Wrap(object.NewString(a.Name)), object.Wrap(object.NewString(a.Value)))
		}
		return object.Wrap(h)
	})

	// Predicates.
	d("element?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).IsElement())))
	})
	d("text?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).IsText())))
	})
	d("comment?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).IsComment())))
	})
	d("cdata?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).IsCDATA())))
	})

	// Tree navigation.
	d("children", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&NokogiriNodeSet{set: self(v).Children()})
	})
	d("parent", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nokogiriNodeOrNil(self(v).Parent())
	})
	d("next", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nokogiriNodeOrNil(self(v).Next())
	})
	d("previous", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nokogiriNodeOrNil(self(v).Previous())
	})
	d("next_element", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nokogiriNodeOrNil(self(v).NextElement())
	})
	d("previous_element", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nokogiriNodeOrNil(self(v).PreviousElement())
	})
}

// registerNokogiriNodeSet installs Nokogiri::XML::NodeSet and its collection
// methods.
func (vm *VM) registerNokogiriNodeSet(mod *RClass) {
	xml := object.Kind[*RClass](mod.consts["XML"])
	cls := newClass("Nokogiri::XML::NodeSet", vm.cObject)
	xml.consts["NodeSet"] = object.Wrap(cls)
	vm.consts["Nokogiri::XML::NodeSet"] = object.Wrap(cls)

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *nokogiri.NodeSet { return object.Kind[*NokogiriNodeSet](v).set }

	// #length / #size / #count.
	length := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Length()))
	}
	d("length", length)
	d("size", length)
	d("count", length)
	d("empty?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).Empty())))
	})

	// #[](i) returns the i-th node (negative indexes count from the end), or nil.
	d("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		i := int(sqlite3IntArg(args[0]))
		if i < 0 {
			i += self(v).Length()
		}
		if i < 0 || i >= self(v).Length() {
			return object.NilVal()
		}
		return object.Wrap(&NokogiriNode{n: self(v).At(i)})
	})
	d("first", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nokogiriNodeOrNil(self(v).First())
	})
	d("last", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return nokogiriNodeOrNil(self(v).Last())
	})
	// #text concatenates the text of every node in the set.
	d("text", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Text()))
	})
	// #to_a returns the nodes as a Ruby Array.
	d("to_a", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(nokogiriNodeArray(self(v)))
	})
	// #each yields each node; without a block it returns the set (chaining).
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return v
		}
		for _, n := range self(v).Nodes() {
			vm.callBlock(blk, []object.Value{object.Wrap(&NokogiriNode{n: n})})
		}
		return v
	})
	// #map collects the block's result for each node into an Array.
	d("map", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		nodes := self(v).Nodes()
		out := object.NewArrayFromSlice(make([]object.Value, 0, len(nodes)))
		if blk == nil {
			return object.Wrap(out)
		}
		for _, n := range nodes {
			out.Elems = append(out.Elems, vm.callBlock(blk, []object.Value{object.Wrap(&NokogiriNode{n: n})}))
		}
		return object.Wrap(out)
	})
}
