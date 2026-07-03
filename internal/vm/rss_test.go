// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// rssFeed20 is a full RSS 2.0 document exercising every channel and item
// accessor (Dublin Core dc:date/dc:creator/dc:subject and content:encoded
// included).
const rssFeed20 = `<?xml version="1.0"?>
<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel>
<title>Ex</title><link>http://e.com</link><description>D</description>
<language>en-us</language><copyright>(c) 2026</copyright>
<managingEditor>ed@e.com</managingEditor><webMaster>wm@e.com</webMaster>
<generator>rbgo</generator><docs>http://docs</docs><ttl>60</ttl>
<pubDate>Mon, 06 Jul 2026 00:01:00 +0000</pubDate>
<lastBuildDate>Mon, 06 Jul 2026 00:02:00 +0000</lastBuildDate>
<dc:date>2026-07-06T00:03:00Z</dc:date>
<category>News</category>
<image><url>http://img</url><title>I</title><link>http://e.com</link><width>88</width><height>31</height></image>
<item>
<title>It1</title><link>http://e.com/1</link><description>B</description>
<author>a@e.com</author><comments>http://e.com/c</comments>
<pubDate>Mon, 06 Jul 2026 01:00:00 +0000</pubDate>
<dc:date>2026-07-06T01:05:00Z</dc:date>
<dc:creator>Alice</dc:creator><dc:subject>Tech</dc:subject>
<content:encoded>rich</content:encoded><category>Tech</category>
<guid isPermaLink="true">http://e.com/1</guid>
</item>
</channel>
</rss>`

// heredoc wraps src as a single-quoted heredoc bound to name so a multi-line XML
// literal reaches the parser verbatim.
func heredoc(name, src string) string {
	return name + " = <<~'RSSXML'\n" + src + "\nRSSXML\n"
}

// TestRSSFeature covers the require probe and the module/error tree shape.
func TestRSSFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "rss"`, "true\n"},
		{`require "rss"; p require "rss"`, "false\n"},
		{`require "rss"; p RSS.is_a?(Module)`, "true\n"},
		{`require "rss"; p RSS::Error < StandardError`, "true\n"},
		{`require "rss"; p RSS::NotWellFormedError < RSS::Error`, "true\n"},
		{`require "rss"; p RSS::Parser.is_a?(Class)`, "true\n"},
		{`require "rss"; p RSS::Rss::Channel::Item::Guid.is_a?(Class)`, "true\n"},
		{`require "rss"; p RSS::Atom::Feed::Entry.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRSS20 covers the full RSS 2.0 read surface.
func TestRSS20(t *testing.T) {
	prog := heredoc("SRC", rssFeed20) + `require "rss"
f = RSS::Parser.parse(SRC)
c = f.channel
i = f.items.first
p f.class.name
p f.feed_type
p f.version
p [c.title, c.link, c.description, c.language, c.copyright]
p [c.managingEditor, c.webMaster, c.generator, c.docs, c.ttl]
p c.categories
p [c.image.url, c.image.title, c.image.link, c.image.width, c.image.height]
p [c.pubDate.to_i == Time.parse("2026-07-06T00:01:00Z").to_i,
   c.lastBuildDate.to_i == Time.parse("2026-07-06T00:02:00Z").to_i,
   c.date.to_i == Time.parse("2026-07-06T00:03:00Z").to_i]
p f.items.length == c.items.length
p [i.title, i.link, i.description, i.author, i.comments]
p [i.dc_creator, i.dc_subject, i.content_encoded]
p i.categories
p [i.guid.content, i.guid.isPermaLink]
p [i.pubDate.to_i == Time.parse("2026-07-06T01:00:00Z").to_i,
   i.date.to_i == Time.parse("2026-07-06T01:05:00Z").to_i]
p f.to_s.include?("<rss")
`
	want := strings.Join([]string{
		`"RSS::Rss"`,
		`"rss"`,
		`"2.0"`,
		`["Ex", "http://e.com", "D", "en-us", "(c) 2026"]`,
		`["ed@e.com", "wm@e.com", "rbgo", "http://docs", "60"]`,
		`["News"]`,
		`["http://img", "I", "http://e.com", "88", "31"]`,
		`[true, true, true]`,
		`true`,
		`["It1", "http://e.com/1", "B", "a@e.com", "http://e.com/c"]`,
		`["Alice", "Tech", "rich"]`,
		`["Tech"]`,
		`["http://e.com/1", true]`,
		`[true, true]`,
		`true`,
		"",
	}, "\n")
	if got := eval(t, prog); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// rssRDF is a full RSS 1.0 (RDF) document.
const rssRDF = `<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel rdf:about="http://e.com/rss"><title>RC</title><link>http://e.com</link><description>RD</description>
<dc:date>2026-07-06T00:03:00Z</dc:date><dc:creator>Bob</dc:creator></channel>
<image rdf:about="http://img"><title>IM</title><url>http://img/u</url><link>http://e.com</link></image>
<textinput rdf:about="http://e.com/ti"><title>TI</title><description>TD</description><name>q</name><link>http://e.com/s</link></textinput>
<item rdf:about="http://e.com/1"><title>RI</title><link>http://e.com/1</link><description>RB</description>
<dc:date>2026-07-06T01:05:00Z</dc:date><dc:creator>Carol</dc:creator><dc:subject>Sci</dc:subject><content:encoded>rc</content:encoded></item>
</rdf:RDF>`

// TestRSSRDF covers the full RSS 1.0 read surface.
func TestRSSRDF(t *testing.T) {
	prog := heredoc("SRC", rssRDF) + `require "rss"
f = RSS::Parser.parse(SRC)
c = f.channel
im = f.image
ti = f.textinput
i = f.items.first
p f.class.name
p f.feed_type
p [c.about, c.title, c.link, c.description, c.dc_creator]
p c.date.to_i == Time.parse("2026-07-06T00:03:00Z").to_i
p [im.about, im.title, im.url, im.link]
p [ti.about, ti.title, ti.description, ti.name, ti.link]
p [i.about, i.title, i.link, i.description, i.dc_creator, i.dc_subject, i.content_encoded]
p i.date.to_i == Time.parse("2026-07-06T01:05:00Z").to_i
p f.to_s.include?("rdf:RDF")
`
	want := strings.Join([]string{
		`"RSS::RDF"`,
		`"rss"`,
		`["http://e.com/rss", "RC", "http://e.com", "RD", "Bob"]`,
		`true`,
		`["http://img", "IM", "http://img/u", "http://e.com"]`,
		`["http://e.com/ti", "TI", "TD", "q", "http://e.com/s"]`,
		`["http://e.com/1", "RI", "http://e.com/1", "RB", "Carol", "Sci", "rc"]`,
		`true`,
		`true`,
		"",
	}, "\n")
	if got := eval(t, prog); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// rssAtom is a full Atom document.
const rssAtom = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"><id>urn:feed</id><title>AT</title><subtitle>AS</subtitle>
<updated>2026-07-06T00:03:00Z</updated><rights>(c)</rights><generator>gen</generator>
<author><name>Dana</name><uri>http://d</uri><email>d@e.com</email></author>
<link href="http://e.com" rel="self" type="application/atom+xml" title="L"/>
<category term="tech" scheme="http://s" label="Tech"/>
<entry><id>urn:e1</id><title>AE</title><summary>ASum</summary><content>AC</content><rights>er</rights>
<updated>2026-07-06T01:00:00Z</updated><published>2026-07-06T00:30:00Z</published>
<author><name>Erin</name></author><link href="http://e.com/1" rel="alternate"/><category term="x"/></entry></feed>`

// TestRSSAtom covers the full Atom read surface.
func TestRSSAtom(t *testing.T) {
	prog := heredoc("SRC", rssAtom) + `require "rss"
g = RSS::Parser.parse(SRC)
a = g.authors.first
l = g.links.first
cat = g.categories.first
e = g.items.first
p g.class.name
p g.feed_type
p [g.id, g.title, g.subtitle, g.rights, g.generator]
p g.updated.to_i == Time.parse("2026-07-06T00:03:00Z").to_i
p [a.name, a.uri, a.email]
p [l.href, l.rel, l.type, l.title]
p [cat.term, cat.scheme, cat.label]
p g.entries.length == g.items.length
p [e.id, e.title, e.summary, e.content, e.rights]
p [e.updated.to_i == Time.parse("2026-07-06T01:00:00Z").to_i,
   e.published.to_i == Time.parse("2026-07-06T00:30:00Z").to_i]
p [e.authors.first.name, e.links.first.href, e.categories.first.term]
p g.to_s.include?("<feed")
`
	want := strings.Join([]string{
		`"RSS::Atom::Feed"`,
		`"atom"`,
		`["urn:feed", "AT", "AS", "(c)", "gen"]`,
		`true`,
		`["Dana", "http://d", "d@e.com"]`,
		`["http://e.com", "self", "application/atom+xml", "L"]`,
		`["tech", "http://s", "Tech"]`,
		`true`,
		`["urn:e1", "AE", "ASum", "AC", "er"]`,
		`[true, true]`,
		`["Erin", "http://e.com/1", "x"]`,
		`true`,
		"",
	}, "\n")
	if got := eval(t, prog); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// TestRSSAbsentAndErrors covers the nil arms (absent image/guid/textinput, a
// channel-less feed) and the parser error/argument checks.
func TestRSSAbsentAndErrors(t *testing.T) {
	// A minimal RSS 2.0 with no image and an item with no guid.
	minimal := `<rss version="2.0"><channel><title>M</title><link>l</link><description>d</description>` +
		`<item><title>i</title></item></channel></rss>`
	prog := heredoc("SRC", minimal) + `require "rss"
f = RSS::Parser.parse(SRC)
p f.channel.image
p f.items.first.guid
p f.channel.categories
p f.channel.pubDate
`
	want := "nil\nnil\n[]\nnil\n"
	if got := eval(t, prog); got != want {
		t.Errorf("minimal: got %q want %q", got, want)
	}

	// An RDF with no image/textinput yields nil for those.
	rdfMin := `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/">` +
		`<channel rdf:about="a"><title>c</title><link>l</link><description>d</description></channel></rdf:RDF>`
	prog2 := heredoc("SRC", rdfMin) + `require "rss"
f = RSS::Parser.parse(SRC)
p [f.image, f.textinput, f.items]
`
	if got := eval(t, prog2); got != "[nil, nil, []]\n" {
		t.Errorf("rdf minimal: got %q", got)
	}

	// A malformed document raises RSS::NotWellFormedError.
	if cls, _ := evalErr(t, `require "rss"; RSS::Parser.parse("<rss><channel>")`); cls != "RSS::NotWellFormedError" {
		t.Errorf("malformed: got %s", cls)
	}
	// parse with no argument is an ArgumentError.
	if cls, _ := evalErr(t, `require "rss"; RSS::Parser.parse`); cls != "ArgumentError" {
		t.Errorf("parse/0: got %s", cls)
	}
	// Trailing gem options (do_validate, …) are accepted and ignored.
	if got := eval(t, heredoc("SRC", rssFeed20)+`require "rss"; p RSS::Parser.parse(SRC, false, true).feed_type`); got != "\"rss\"\n" {
		t.Errorf("ignored options: got %q", got)
	}
}
