// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	stdtime "time"

	rss "github.com/go-ruby-rss/rss"

	gotime "github.com/go-composites/time/src"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-rss/rss parser. The library owns
// the XML tokenizing, dialect auto-detection (RSS 2.0 / RSS 1.0 RDF / Atom) and
// the MRI-mirroring feed model; rbgo only wraps each parsed struct as a Ruby
// object reporting the matching RSS::* class and reads its fields on demand (see
// rss.go for the class + accessor registration). Absent scalar elements come
// back from the library as "" and absent nested elements / dates as nil; the
// wrappers surface those as an empty String and Ruby nil respectively, since the
// library — like a single parse — cannot distinguish "absent" from "empty".

// rssParse parses an RSS/Atom source string and wraps the detected feed as its
// Ruby root object. A parse error becomes an RSS::NotWellFormedError, matching
// RSS::Parser.parse raising on a malformed or unrecognised document.
func rssParse(src string) object.Value {
	feed, err := rss.Parse(src)
	if err != nil {
		return raise("RSS::NotWellFormedError", "%s", err.Error())
	}
	return rssWrap(feed)
}

// rssWrap wraps a parsed rss.Feed as its Ruby root object. Parse only ever
// returns *Rss / *RDF / *AtomFeed; the default arm (a nil/unknown feed) is not
// reachable through Parse but is guarded rather than left to panic.
func rssWrap(feed rss.Feed) object.Value {
	switch f := feed.(type) {
	case *rss.Rss:
		return &RSSRss{f}
	case *rss.RDF:
		return &RSSRDF{f}
	case *rss.AtomFeed:
		return &RSSAtomFeed{f}
	}
	return object.NilV
}

// The wrapper types. Each holds a pointer to the library struct and reports the
// matching RSS::* class (see classOf); the accessor methods registered in rss.go
// read the held struct's fields.

type RSSRss struct{ r *rss.Rss }
type RSSChannel struct{ c *rss.Channel }
type RSSItem struct{ i *rss.Item }
type RSSImage struct{ im *rss.Image }
type RSSGuid struct{ g *rss.Guid }
type RSSRDF struct{ r *rss.RDF }
type RSSRDFChannel struct{ c *rss.RDFChannel }
type RSSRDFItem struct{ i *rss.RDFItem }
type RSSRDFImage struct{ im *rss.RDFImage }
type RSSRDFTextinput struct{ t *rss.RDFTextinput }
type RSSAtomFeed struct{ f *rss.AtomFeed }
type RSSAtomEntry struct{ e *rss.AtomEntry }
type RSSAtomLink struct{ l *rss.AtomLink }
type RSSAtomPerson struct{ p *rss.AtomPerson }
type RSSAtomCategory struct{ c *rss.AtomCategory }

func (v *RSSRss) ToS() string            { return "#<RSS::Rss>" }
func (v *RSSRss) Inspect() string        { return "#<RSS::Rss>" }
func (v *RSSRss) Truthy() bool           { return true }
func (v *RSSChannel) ToS() string        { return "#<RSS::Rss::Channel>" }
func (v *RSSChannel) Inspect() string    { return "#<RSS::Rss::Channel>" }
func (v *RSSChannel) Truthy() bool       { return true }
func (v *RSSItem) ToS() string           { return "#<RSS::Rss::Channel::Item>" }
func (v *RSSItem) Inspect() string       { return "#<RSS::Rss::Channel::Item>" }
func (v *RSSItem) Truthy() bool          { return true }
func (v *RSSImage) ToS() string          { return "#<RSS::Rss::Channel::Image>" }
func (v *RSSImage) Inspect() string      { return "#<RSS::Rss::Channel::Image>" }
func (v *RSSImage) Truthy() bool         { return true }
func (v *RSSGuid) ToS() string           { return "#<RSS::Rss::Channel::Item::Guid>" }
func (v *RSSGuid) Inspect() string       { return "#<RSS::Rss::Channel::Item::Guid>" }
func (v *RSSGuid) Truthy() bool          { return true }
func (v *RSSRDF) ToS() string            { return "#<RSS::RDF>" }
func (v *RSSRDF) Inspect() string        { return "#<RSS::RDF>" }
func (v *RSSRDF) Truthy() bool           { return true }
func (v *RSSRDFChannel) ToS() string     { return "#<RSS::RDF::Channel>" }
func (v *RSSRDFChannel) Inspect() string { return "#<RSS::RDF::Channel>" }
func (v *RSSRDFChannel) Truthy() bool    { return true }
func (v *RSSRDFItem) ToS() string        { return "#<RSS::RDF::Item>" }
func (v *RSSRDFItem) Inspect() string    { return "#<RSS::RDF::Item>" }
func (v *RSSRDFItem) Truthy() bool       { return true }
func (v *RSSRDFImage) ToS() string       { return "#<RSS::RDF::Image>" }
func (v *RSSRDFImage) Inspect() string   { return "#<RSS::RDF::Image>" }
func (v *RSSRDFImage) Truthy() bool      { return true }
func (v *RSSRDFTextinput) ToS() string   { return "#<RSS::RDF::Textinput>" }
func (v *RSSRDFTextinput) Inspect() string {
	return "#<RSS::RDF::Textinput>"
}
func (v *RSSRDFTextinput) Truthy() bool     { return true }
func (v *RSSAtomFeed) ToS() string          { return "#<RSS::Atom::Feed>" }
func (v *RSSAtomFeed) Inspect() string      { return "#<RSS::Atom::Feed>" }
func (v *RSSAtomFeed) Truthy() bool         { return true }
func (v *RSSAtomEntry) ToS() string         { return "#<RSS::Atom::Feed::Entry>" }
func (v *RSSAtomEntry) Inspect() string     { return "#<RSS::Atom::Feed::Entry>" }
func (v *RSSAtomEntry) Truthy() bool        { return true }
func (v *RSSAtomLink) ToS() string          { return "#<RSS::Atom::Link>" }
func (v *RSSAtomLink) Inspect() string      { return "#<RSS::Atom::Link>" }
func (v *RSSAtomLink) Truthy() bool         { return true }
func (v *RSSAtomPerson) ToS() string        { return "#<RSS::Atom::Person>" }
func (v *RSSAtomPerson) Inspect() string    { return "#<RSS::Atom::Person>" }
func (v *RSSAtomPerson) Truthy() bool       { return true }
func (v *RSSAtomCategory) ToS() string      { return "#<RSS::Atom::Category>" }
func (v *RSSAtomCategory) Inspect() string  { return "#<RSS::Atom::Category>" }
func (v *RSSAtomCategory) Truthy() bool     { return true }

// rssStr wraps a library scalar as a Ruby String.
func rssStr(s string) object.Value { return object.NewString(s) }

// rssTime wraps a library *time.Time as a rbgo Time (whole-second instant), or
// Ruby nil when the element was absent. The instant is preserved via its Unix
// seconds, so #to_i is stable regardless of the host time zone.
func rssTime(tp *stdtime.Time) object.Value {
	if tp == nil {
		return object.NilV
	}
	return &Time{t: gotime.FromUnix(tp.Unix())}
}

// rssStrArray wraps a library []string (e.g. an item's categories) as a Ruby
// Array of Strings.
func rssStrArray(ss []string) object.Value {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(elems)
}

// rssItems wraps a []*rss.Item as a Ruby Array of RSS::Rss::Channel::Item.
func rssItems(items []*rss.Item) object.Value {
	elems := make([]object.Value, len(items))
	for i, it := range items {
		elems[i] = &RSSItem{it}
	}
	return object.NewArrayFromSlice(elems)
}

// rssImage wraps a *rss.Image as an RSS::Rss::Channel::Image, or nil when absent.
func rssImage(im *rss.Image) object.Value {
	if im == nil {
		return object.NilV
	}
	return &RSSImage{im}
}

// rssGuid wraps a *rss.Guid as an RSS::Rss::Channel::Item::Guid, or nil.
func rssGuid(g *rss.Guid) object.Value {
	if g == nil {
		return object.NilV
	}
	return &RSSGuid{g}
}

// rssRDFItems wraps a []*rss.RDFItem as a Ruby Array of RSS::RDF::Item.
func rssRDFItems(items []*rss.RDFItem) object.Value {
	elems := make([]object.Value, len(items))
	for i, it := range items {
		elems[i] = &RSSRDFItem{it}
	}
	return object.NewArrayFromSlice(elems)
}

// rssAtomEntries wraps a []*rss.AtomEntry as a Ruby Array of
// RSS::Atom::Feed::Entry.
func rssAtomEntries(entries []*rss.AtomEntry) object.Value {
	elems := make([]object.Value, len(entries))
	for i, e := range entries {
		elems[i] = &RSSAtomEntry{e}
	}
	return object.NewArrayFromSlice(elems)
}

// rssAtomLinks wraps a []*rss.AtomLink as a Ruby Array of RSS::Atom::Link.
func rssAtomLinks(links []*rss.AtomLink) object.Value {
	elems := make([]object.Value, len(links))
	for i, l := range links {
		elems[i] = &RSSAtomLink{l}
	}
	return object.NewArrayFromSlice(elems)
}

// rssAtomPersons wraps a []*rss.AtomPerson (authors/contributors) as a Ruby
// Array of RSS::Atom::Person.
func rssAtomPersons(people []*rss.AtomPerson) object.Value {
	elems := make([]object.Value, len(people))
	for i, p := range people {
		elems[i] = &RSSAtomPerson{p}
	}
	return object.NewArrayFromSlice(elems)
}

// rssAtomCategories wraps a []*rss.AtomCategory as a Ruby Array of
// RSS::Atom::Category.
func rssAtomCategories(cats []*rss.AtomCategory) object.Value {
	elems := make([]object.Value, len(cats))
	for i, c := range cats {
		elems[i] = &RSSAtomCategory{c}
	}
	return object.NewArrayFromSlice(elems)
}
