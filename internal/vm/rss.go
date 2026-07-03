// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rss "github.com/go-ruby-rss/rss"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRSS installs the RSS module (require "rss"): RSS::Parser.parse(source)
// and the read surface over the parsed feed — RSS 2.0 (RSS::Rss), RSS 1.0
// (RSS::RDF) and Atom (RSS::Atom::Feed), each with its channel/item/entry object
// tree. The parser and the MRI-mirroring feed model live in the
// github.com/go-ruby-rss/rss library; this file is the class + accessor wiring
// (see rss_bind.go for the wrappers and value conversions). The error tree
// mirrors the gem (RSS::Error < StandardError, RSS::NotWellFormedError <
// RSS::Error).
func (vm *VM) registerRSS() {
	mod := newClass("RSS", nil)
	mod.isModule = true
	vm.consts["RSS"] = mod

	vm.registerRSSErrors(mod)

	// RSS::Parser.parse(source, *ignored) — the gem accepts (rss, do_validate=true,
	// ignore_unknown_element=true, parser_class=…); the pure-Go library always
	// parses and auto-detects the dialect, so the trailing options are accepted and
	// ignored.
	parser := newClass("RSS::Parser", vm.cObject)
	mod.consts["Parser"] = parser
	vm.consts["RSS::Parser"] = parser
	parser.smethods["parse"] = &Method{name: "parse", owner: parser,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
			}
			return rssParse(strArg(args[0]))
		}}

	vm.registerRSSClasses(mod)
}

// registerRSSErrors installs RSS::Error < StandardError and RSS::NotWellFormedError
// < RSS::Error, the exception RSS::Parser.parse raises on a malformed document.
func (vm *VM) registerRSSErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("RSS::Error", std)
	mod.consts["Error"] = base
	vm.consts["RSS::Error"] = base

	nwf := newClass("RSS::NotWellFormedError", base)
	mod.consts["NotWellFormedError"] = nwf
	vm.consts["RSS::NotWellFormedError"] = nwf
}

// registerRSSClasses creates every feed-model class and installs its accessor
// methods. The classes are stored in vm.consts under their qualified names so
// classOf can resolve each wrapper, and nested under their parents so the Ruby
// constant paths (RSS::Rss::Channel, RSS::Atom::Feed::Entry, …) resolve too.
func (vm *VM) registerRSSClasses(mod *RClass) {
	// mkClass creates a class named qualified, records it flat (for classOf) and,
	// when parent is non-nil, nests it under parent's simple name.
	mkClass := func(parent *RClass, simple, qualified string) *RClass {
		c := newClass(qualified, vm.cObject)
		vm.consts[qualified] = c
		if parent != nil {
			parent.consts[simple] = c
		}
		return c
	}
	// atom is the RSS::Atom namespace module the Atom classes nest under.
	atom := newClass("RSS::Atom", nil)
	atom.isModule = true
	mod.consts["Atom"] = atom
	vm.consts["RSS::Atom"] = atom

	cRss := mkClass(mod, "Rss", "RSS::Rss")
	cChannel := mkClass(cRss, "Channel", "RSS::Rss::Channel")
	cItem := mkClass(cChannel, "Item", "RSS::Rss::Channel::Item")
	cImage := mkClass(cChannel, "Image", "RSS::Rss::Channel::Image")
	cGuid := mkClass(cItem, "Guid", "RSS::Rss::Channel::Item::Guid")

	cRDF := mkClass(mod, "RDF", "RSS::RDF")
	cRDFChannel := mkClass(cRDF, "Channel", "RSS::RDF::Channel")
	cRDFItem := mkClass(cRDF, "Item", "RSS::RDF::Item")
	cRDFImage := mkClass(cRDF, "Image", "RSS::RDF::Image")
	cRDFText := mkClass(cRDF, "Textinput", "RSS::RDF::Textinput")

	cAtomFeed := mkClass(atom, "Feed", "RSS::Atom::Feed")
	cAtomEntry := mkClass(cAtomFeed, "Entry", "RSS::Atom::Feed::Entry")
	cAtomLink := mkClass(atom, "Link", "RSS::Atom::Link")
	cAtomPerson := mkClass(atom, "Person", "RSS::Atom::Person")
	cAtomCategory := mkClass(atom, "Category", "RSS::Atom::Category")

	vm.registerRSSRss(cRss, cChannel, cItem, cImage, cGuid)
	vm.registerRSSRDF(cRDF, cRDFChannel, cRDFItem, cRDFImage, cRDFText)
	vm.registerRSSAtom(cAtomFeed, cAtomEntry, cAtomLink, cAtomPerson, cAtomCategory)
}

// registerRSSRss installs the RSS 2.0 (RSS::Rss) accessor surface.
func (vm *VM) registerRSSRss(cRss, cChannel, cItem, cImage, cGuid *RClass) {
	d := func(c *RClass, name string, fn NativeFn) { c.define(name, fn) }

	d(cRss, "version", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rssStr(self.(*RSSRss).r.Version)
	})
	d(cRss, "feed_type", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rssStr(self.(*RSSRss).r.FeedType())
	})
	d(cRss, "channel", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if ch := self.(*RSSRss).r.Channel; ch != nil {
			return &RSSChannel{ch}
		}
		return object.NilV
	})
	d(cRss, "to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rssStr(self.(*RSSRss).r.String())
	})
	// RSS::Rss#items is the gem's shortcut for channel.items.
	d(cRss, "items", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if ch := self.(*RSSRss).r.Channel; ch != nil {
			return rssItems(ch.Items)
		}
		return object.NewArrayFromSlice(nil)
	})

	ch := func(self object.Value) *rss.Channel { return self.(*RSSChannel).c }
	d(cChannel, "title", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(ch(self).Title) })
	d(cChannel, "link", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(ch(self).Link) })
	d(cChannel, "description", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(ch(self).Description) })
	d(cChannel, "language", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(ch(self).Language) })
	d(cChannel, "copyright", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(ch(self).Copyright) })
	d(cChannel, "managingEditor", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(ch(self).ManagingEditor) })
	d(cChannel, "webMaster", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(ch(self).WebMaster) })
	d(cChannel, "generator", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(ch(self).Generator) })
	d(cChannel, "docs", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(ch(self).Docs) })
	d(cChannel, "ttl", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(ch(self).TTL) })
	d(cChannel, "pubDate", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssTime(ch(self).PubDate) })
	d(cChannel, "lastBuildDate", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssTime(ch(self).LastBuildDate) })
	d(cChannel, "date", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssTime(ch(self).DCDate) })
	d(cChannel, "image", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssImage(ch(self).Image) })
	d(cChannel, "categories", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStrArray(ch(self).Categories) })
	d(cChannel, "items", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssItems(ch(self).Items) })

	it := func(self object.Value) *rss.Item { return self.(*RSSItem).i }
	d(cItem, "title", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(it(self).Title) })
	d(cItem, "link", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(it(self).Link) })
	d(cItem, "description", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(it(self).Description) })
	d(cItem, "author", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(it(self).Author) })
	d(cItem, "comments", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(it(self).Comments) })
	d(cItem, "pubDate", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssTime(it(self).PubDate) })
	d(cItem, "date", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssTime(it(self).DCDate) })
	d(cItem, "dc_creator", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(it(self).DCCreator) })
	d(cItem, "dc_subject", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(it(self).DCSubject) })
	d(cItem, "content_encoded", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(it(self).ContentEncoded) })
	d(cItem, "categories", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStrArray(it(self).Categories) })
	d(cItem, "guid", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssGuid(it(self).Guid) })

	d(cImage, "url", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSImage).im.URL) })
	d(cImage, "title", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSImage).im.Title) })
	d(cImage, "link", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSImage).im.Link) })
	d(cImage, "width", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSImage).im.Width) })
	d(cImage, "height", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSImage).im.Height) })

	d(cGuid, "content", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSGuid).g.Content) })
	d(cGuid, "isPermaLink", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*RSSGuid).g.IsPermaLink)
	})
}

// registerRSSRDF installs the RSS 1.0 (RSS::RDF) accessor surface.
func (vm *VM) registerRSSRDF(cRDF, cChannel, cItem, cImage, cText *RClass) {
	d := func(c *RClass, name string, fn NativeFn) { c.define(name, fn) }

	d(cRDF, "feed_type", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rssStr(self.(*RSSRDF).r.FeedType())
	})
	d(cRDF, "to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rssStr(self.(*RSSRDF).r.String())
	})
	d(cRDF, "channel", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if c := self.(*RSSRDF).r.Channel; c != nil {
			return &RSSRDFChannel{c}
		}
		return object.NilV
	})
	d(cRDF, "image", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if im := self.(*RSSRDF).r.Image; im != nil {
			return &RSSRDFImage{im}
		}
		return object.NilV
	})
	d(cRDF, "textinput", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if t := self.(*RSSRDF).r.Textinput; t != nil {
			return &RSSRDFTextinput{t}
		}
		return object.NilV
	})
	d(cRDF, "items", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return rssRDFItems(self.(*RSSRDF).r.Items)
	})

	c := func(self object.Value) *rss.RDFChannel { return self.(*RSSRDFChannel).c }
	d(cChannel, "about", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(c(self).About) })
	d(cChannel, "title", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(c(self).Title) })
	d(cChannel, "link", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(c(self).Link) })
	d(cChannel, "description", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(c(self).Description) })
	d(cChannel, "date", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssTime(c(self).DCDate) })
	d(cChannel, "dc_creator", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(c(self).DCCreator) })

	i := func(self object.Value) *rss.RDFItem { return self.(*RSSRDFItem).i }
	d(cItem, "about", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(i(self).About) })
	d(cItem, "title", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(i(self).Title) })
	d(cItem, "link", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(i(self).Link) })
	d(cItem, "description", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(i(self).Description) })
	d(cItem, "date", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssTime(i(self).DCDate) })
	d(cItem, "dc_creator", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(i(self).DCCreator) })
	d(cItem, "dc_subject", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(i(self).DCSubject) })
	d(cItem, "content_encoded", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(i(self).ContentEncoded) })

	d(cImage, "about", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSRDFImage).im.About) })
	d(cImage, "title", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSRDFImage).im.Title) })
	d(cImage, "url", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSRDFImage).im.URL) })
	d(cImage, "link", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSRDFImage).im.Link) })

	d(cText, "about", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSRDFTextinput).t.About) })
	d(cText, "title", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSRDFTextinput).t.Title) })
	d(cText, "description", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSRDFTextinput).t.Description) })
	d(cText, "name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSRDFTextinput).t.Name) })
	d(cText, "link", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSRDFTextinput).t.Link) })
}

// registerRSSAtom installs the Atom (RSS::Atom::Feed) accessor surface.
func (vm *VM) registerRSSAtom(cFeed, cEntry, cLink, cPerson, cCategory *RClass) {
	d := func(c *RClass, name string, fn NativeFn) { c.define(name, fn) }

	f := func(self object.Value) *rss.AtomFeed { return self.(*RSSAtomFeed).f }
	d(cFeed, "feed_type", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(f(self).FeedType()) })
	d(cFeed, "to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(f(self).String()) })
	d(cFeed, "id", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(f(self).ID) })
	d(cFeed, "title", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(f(self).Title) })
	d(cFeed, "subtitle", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(f(self).Subtitle) })
	d(cFeed, "rights", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(f(self).Rights) })
	d(cFeed, "generator", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(f(self).Generator) })
	d(cFeed, "updated", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssTime(f(self).Updated) })
	d(cFeed, "authors", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssAtomPersons(f(self).Authors) })
	d(cFeed, "links", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssAtomLinks(f(self).Links) })
	d(cFeed, "categories", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssAtomCategories(f(self).Categories) })
	d(cFeed, "entries", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssAtomEntries(f(self).Entries) })
	// RSS::Atom::Feed#items aliases #entries, the common cross-dialect accessor.
	d(cFeed, "items", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssAtomEntries(f(self).Entries) })

	e := func(self object.Value) *rss.AtomEntry { return self.(*RSSAtomEntry).e }
	d(cEntry, "id", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(e(self).ID) })
	d(cEntry, "title", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(e(self).Title) })
	d(cEntry, "summary", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(e(self).Summary) })
	d(cEntry, "content", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(e(self).Content) })
	d(cEntry, "rights", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(e(self).Rights) })
	d(cEntry, "updated", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssTime(e(self).Updated) })
	d(cEntry, "published", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssTime(e(self).Published) })
	d(cEntry, "authors", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssAtomPersons(e(self).Authors) })
	d(cEntry, "links", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssAtomLinks(e(self).Links) })
	d(cEntry, "categories", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssAtomCategories(e(self).Categories) })

	d(cLink, "href", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSAtomLink).l.Href) })
	d(cLink, "rel", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSAtomLink).l.Rel) })
	d(cLink, "type", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSAtomLink).l.Type) })
	d(cLink, "title", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSAtomLink).l.Title) })

	d(cPerson, "name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSAtomPerson).p.Name) })
	d(cPerson, "uri", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSAtomPerson).p.URI) })
	d(cPerson, "email", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSAtomPerson).p.Email) })

	d(cCategory, "term", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSAtomCategory).c.Term) })
	d(cCategory, "scheme", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSAtomCategory).c.Scheme) })
	d(cCategory, "label", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return rssStr(self.(*RSSAtomCategory).c.Label) })
}
