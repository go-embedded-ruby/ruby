// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	rss "github.com/go-ruby-rss/rss"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestRSSWrapperMethods covers the ToS / Inspect / Truthy value-protocol arms of
// every RSS wrapper (the Ruby surface reads fields, never routing through these).
func TestRSSWrapperMethods(t *testing.T) {
	wrappers := []object.Value{
		&RSSRss{&rss.Rss{}}, &RSSChannel{&rss.Channel{}}, &RSSItem{&rss.Item{}},
		&RSSImage{&rss.Image{}}, &RSSGuid{&rss.Guid{}}, &RSSRDF{&rss.RDF{}},
		&RSSRDFChannel{&rss.RDFChannel{}}, &RSSRDFItem{&rss.RDFItem{}},
		&RSSRDFImage{&rss.RDFImage{}}, &RSSRDFTextinput{&rss.RDFTextinput{}},
		&RSSAtomFeed{&rss.AtomFeed{}}, &RSSAtomEntry{&rss.AtomEntry{}},
		&RSSAtomLink{&rss.AtomLink{}}, &RSSAtomPerson{&rss.AtomPerson{}},
		&RSSAtomCategory{&rss.AtomCategory{}},
	}
	for _, w := range wrappers {
		if w.ToS() == "" || w.Inspect() == "" || !w.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", w, w.ToS(), w.Inspect(), w.Truthy())
		}
	}
}

// TestRSSNilChannelGuards covers the defensive nil-channel arms of RSS::Rss#channel
// and #items. A parsed <rss> always carries a channel (the library errors
// otherwise), so these arms are unreachable from the Ruby surface and are driven
// directly here with a channel-less struct.
func TestRSSNilChannelGuards(t *testing.T) {
	vm := New(nil)
	vm.registerRSS()
	f := &RSSRss{&rss.Rss{Channel: nil}}
	if ch := vm.send(f, "channel", nil, nil); ch != object.NilV {
		t.Errorf("nil channel -> %#v", ch)
	}
	items := vm.send(f, "items", nil, nil)
	arr, ok := items.(*object.Array)
	if !ok || len(arr.Elems) != 0 {
		t.Errorf("nil channel items -> %#v", items)
	}
	// RSS::RDF#channel has the same defensive nil arm (a parsed RDF always carries
	// a channel), driven here with a channel-less RDF struct.
	if ch := vm.send(&RSSRDF{&rss.RDF{Channel: nil}}, "channel", nil, nil); ch != object.NilV {
		t.Errorf("nil RDF channel -> %#v", ch)
	}
}

// TestRSSParseUnknownFeed covers rssParse's guard for a Feed whose concrete type
// is none of the three roots (unreachable through rss.Parse, which only ever
// returns *Rss / *RDF / *AtomFeed, so exercised with a stub Feed).
func TestRSSParseUnknownFeed(t *testing.T) {
	// rssParse type-switches on the concrete value; a value that is none of the
	// three returns Ruby nil. We cannot make rss.Parse yield such a value, so the
	// guard is asserted structurally: an all-fields-zero *Rss is a known type, so
	// build the unreachable path by calling the switch's fallthrough directly is
	// not possible without an unknown type — instead confirm the three known types
	// map to their wrappers, pinning the switch.
	if _, ok := rssWrap(&rss.Rss{Channel: &rss.Channel{}}).(*RSSRss); !ok {
		t.Error("*Rss did not map to *RSSRss")
	}
	if _, ok := rssWrap(&rss.RDF{}).(*RSSRDF); !ok {
		t.Error("*RDF did not map to *RSSRDF")
	}
	if _, ok := rssWrap(&rss.AtomFeed{}).(*RSSAtomFeed); !ok {
		t.Error("*AtomFeed did not map to *RSSAtomFeed")
	}
	if rssWrap(stubFeed{}) != object.NilV {
		t.Error("unknown feed did not map to nil")
	}
}

// stubFeed is a Feed whose concrete type is none of the three roots, used to
// drive rssWrap's default arm.
type stubFeed struct{}

func (stubFeed) FeedType() string { return "stub" }
func (stubFeed) String() string   { return "" }
