// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io"
	"testing"
	stdtime "time"

	papertrail "github.com/go-ruby-paper-trail/paper-trail"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// errPT is the sentinel the fault-injection seams below return; it drives the
// PaperTrail::Error arms the happy path (a never-erroring MemoryStore + JSON
// serializer) cannot provoke.
var errPT = errors.New("papertrail: injected failure")

// ptToggleStore wraps a real store but can be flipped to fail SaveVersion or
// VersionsFor, so a version can first be recorded (healthy) and then the query /
// save error arms exercised (failing) with the same versions in place.
type ptToggleStore struct {
	inner             papertrail.Store
	failSave, failFor bool
}

func (s *ptToggleStore) SaveVersion(v papertrail.Version) (papertrail.Version, error) {
	if s.failSave {
		return papertrail.Version{}, errPT
	}
	return s.inner.SaveVersion(v)
}

func (s *ptToggleStore) VersionsFor(itemType, itemID string) ([]papertrail.Version, error) {
	if s.failFor {
		return nil, errPT
	}
	return s.inner.VersionsFor(itemType, itemID)
}

// ptToggleSerializer delegates to JSON but can be flipped to fail either the dump
// (Save-time) or load (read-time) direction, reaching the serializer error arms.
type ptToggleSerializer struct {
	inner              papertrail.Serializer
	failDump, failLoad bool
}

func (s *ptToggleSerializer) DumpObject(m map[string]any) (string, error) {
	if s.failDump {
		return "", errPT
	}
	return s.inner.DumpObject(m)
}

func (s *ptToggleSerializer) LoadObject(str string) (map[string]any, error) {
	if s.failLoad {
		return nil, errPT
	}
	return s.inner.LoadObject(str)
}

func (s *ptToggleSerializer) DumpChanges(m map[string]papertrail.Change) (string, error) {
	if s.failDump {
		return "", errPT
	}
	return s.inner.DumpChanges(m)
}

func (s *ptToggleSerializer) LoadChanges(str string) (map[string]papertrail.Change, error) {
	if s.failLoad {
		return nil, errPT
	}
	return s.inner.LoadChanges(str)
}

// ptEval runs src on v, returning the program's value; it fails the test on a
// parse / compile / runtime error.
func ptEval(t *testing.T, v *VM, src string) object.Value {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	val, rerr := v.Run(iseq)
	if rerr != nil {
		t.Fatalf("run %q: %v", src, rerr)
	}
	return val
}

// ptEvalErr runs src expecting a RubyError and returns its class name.
func ptEvalErr(t *testing.T, v *VM, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	_, rerr := v.Run(iseq)
	re, ok := rerr.(RubyError)
	if !ok {
		t.Fatalf("src=%q: expected a RubyError, got %#v", src, rerr)
	}
	return re.Class
}

func ptStr(t *testing.T, v object.Value) string {
	t.Helper()
	s, ok := v.(*object.String)
	if !ok {
		t.Fatalf("expected String, got %#v", v)
	}
	return s.Str()
}

// modelSrc defines a versioned Widget model with a string id and a name, so its
// attribute snapshot is deterministic and JSON-clean.
const modelSrc = `
class Widget
  attr_accessor :id, :name, :note
  has_paper_trail
  def initialize(id, name); @id = id; @name = name; end
end
`

// TestPaperTrailRequire covers require "paper_trail" returning true once.
func TestPaperTrailRequire(t *testing.T) {
	v := New(io.Discard)
	if got := ptEval(t, v, `require "paper_trail"`); got != object.True {
		t.Errorf("first require: got %#v want true", got)
	}
	if got := ptEval(t, v, `require "paper_trail"`); got != object.False {
		t.Errorf("second require: got %#v want false", got)
	}
}

// TestPaperTrailCreateUpdateDestroy covers the create/update/destroy callback
// points, the version readers, object / object_changes, reify, live? and the
// version / proxy neighbour queries.
func TestPaperTrailCreateUpdateDestroy(t *testing.T) {
	v := New(io.Discard)
	ptEval(t, v, modelSrc)
	ptEval(t, v, `$w = Widget.new("w1", "first")`)

	// Create.
	ptEval(t, v, `$w.save`)
	if n := ptEval(t, v, `$w.versions.length`); n != object.Integer(1) {
		t.Fatalf("after create: versions=%v want 1", n)
	}
	if ev := ptEval(t, v, `$w.versions.first.event`); ptStr(t, ev) != "create" {
		t.Errorf("create event = %q", ptStr(t, ev))
	}
	// A create's object snapshot is absent (nil), its changeset present.
	if o := ptEval(t, v, `$w.versions.first.object`); !object.IsNil(o) {
		t.Errorf("create object = %#v want nil", o)
	}
	if ch := ptEval(t, v, `$w.versions.first.object_changes["name"].last`); ptStr(t, ch) != "first" {
		t.Errorf("create change name.last = %q", ptStr(t, ch))
	}
	if id := ptEval(t, v, `$w.versions.first.item_id`); ptStr(t, id) != "w1" {
		t.Errorf("item_id = %q want w1", ptStr(t, id))
	}
	if it := ptEval(t, v, `$w.versions.first.item_type`); ptStr(t, it) != "Widget" {
		t.Errorf("item_type = %q want Widget", ptStr(t, it))
	}

	// Update.
	ptEval(t, v, `$w.name = "second"; $w.save`)
	if n := ptEval(t, v, `$w.versions.length`); n != object.Integer(2) {
		t.Fatalf("after update: versions=%v want 2", n)
	}
	if ev := ptEval(t, v, `$w.versions.last.event`); ptStr(t, ev) != "update" {
		t.Errorf("update event = %q", ptStr(t, ev))
	}
	// The update's object holds the pre-change name.
	if o := ptEval(t, v, `$w.versions.last.object["name"]`); ptStr(t, o) != "first" {
		t.Errorf("update object name = %q want first", ptStr(t, o))
	}
	if ch := ptEval(t, v, `$w.versions.last.object_changes["name"].first`); ptStr(t, ch) != "first" {
		t.Errorf("update change name.first = %q", ptStr(t, ch))
	}
	// reify rebuilds the model as it was before the update.
	if nm := ptEval(t, v, `$w.versions.last.reify.name`); ptStr(t, nm) != "first" {
		t.Errorf("reify name = %q want first", ptStr(t, nm))
	}

	// Version neighbours.
	if ev := ptEval(t, v, `$w.versions.last.previous_version.event`); ptStr(t, ev) != "create" {
		t.Errorf("previous_version event = %q want create", ptStr(t, ev))
	}
	if p := ptEval(t, v, `$w.versions.first.previous_version`); !object.IsNil(p) {
		t.Errorf("first.previous_version = %#v want nil", p)
	}
	if ev := ptEval(t, v, `$w.versions.first.next_version.event`); ptStr(t, ev) != "update" {
		t.Errorf("next_version event = %q want update", ptStr(t, ev))
	}
	if n := ptEval(t, v, `$w.versions.last.next_version`); !object.IsNil(n) {
		t.Errorf("last.next_version = %#v want nil", n)
	}

	// Proxy queries on the live record.
	if n := ptEval(t, v, `$w.paper_trail.versions.length`); n != object.Integer(2) {
		t.Errorf("proxy versions=%v want 2", n)
	}
	if live := ptEval(t, v, `$w.paper_trail.live?`); live != object.True {
		t.Errorf("live? = %#v want true", live)
	}
	if nm := ptEval(t, v, `$w.paper_trail.previous_version.name`); ptStr(t, nm) != "first" {
		t.Errorf("proxy previous_version name = %q want first", ptStr(t, nm))
	}
	if n := ptEval(t, v, `$w.paper_trail.next_version`); !object.IsNil(n) {
		t.Errorf("proxy next_version = %#v want nil", n)
	}

	// Destroy.
	ptEval(t, v, `$w.destroy`)
	if ev := ptEval(t, v, `$w.versions.last.event`); ptStr(t, ev) != "destroy" {
		t.Errorf("destroy event = %q", ptStr(t, ev))
	}
	// A destroy records no changeset.
	if ch := ptEval(t, v, `$w.versions.last.object_changes`); !object.IsNil(ch) {
		t.Errorf("destroy object_changes = %#v want nil", ch)
	}
	if live := ptEval(t, v, `$w.paper_trail.live?`); live != object.False {
		t.Errorf("after destroy live? = %#v want false", live)
	}
}

// TestPaperTrailNoOpUpdate covers the no-op update (a save with no attribute
// change records no new version) and save!.
func TestPaperTrailNoOpUpdate(t *testing.T) {
	v := New(io.Discard)
	ptEval(t, v, modelSrc)
	ptEval(t, v, `$w = Widget.new("w1", "n"); $w.save!`)
	ptEval(t, v, `$w.save!`) // no change -> no version
	if n := ptEval(t, v, `$w.versions.length`); n != object.Integer(1) {
		t.Errorf("no-op update: versions=%v want 1", n)
	}
}

// TestPaperTrailUnsavedInstance covers the never-recorded instance: versions is
// empty, and live? on it (no state yet) is true.
func TestPaperTrailUnsavedInstance(t *testing.T) {
	v := New(io.Discard)
	ptEval(t, v, modelSrc)
	if n := ptEval(t, v, `Widget.new("x", "y").versions.length`); n != object.Integer(0) {
		t.Errorf("unsaved versions=%v want 0", n)
	}
	if live := ptEval(t, v, `Widget.new("x", "y").paper_trail.live?`); live != object.True {
		t.Errorf("unsaved live? = %#v want true", live)
	}
	if p := ptEval(t, v, `Widget.new("x", "y").paper_trail.previous_version`); !object.IsNil(p) {
		t.Errorf("unsaved proxy previous_version = %#v want nil", p)
	}
}

// TestPaperTrailItemIDForms covers the item-id seam: an integer id, and a
// synthetic id when the model has no id attribute.
func TestPaperTrailItemIDForms(t *testing.T) {
	v := New(io.Discard)
	// Integer id.
	ptEval(t, v, `
class Gadget
  attr_accessor :id, :name
  has_paper_trail
end`)
	ptEval(t, v, `$g = Gadget.new; $g.id = 7; $g.name = "a"; $g.save`)
	if id := ptEval(t, v, `$g.versions.first.item_id`); ptStr(t, id) != "7" {
		t.Errorf("integer item_id = %q want 7", ptStr(t, id))
	}
	// No id attribute -> synthetic id, still queryable.
	ptEval(t, v, `
class Blob2
  attr_accessor :name
  has_paper_trail
end`)
	ptEval(t, v, `$b = Blob2.new; $b.name = "z"; $b.save`)
	if n := ptEval(t, v, `$b.versions.length`); n != object.Integer(1) {
		t.Errorf("synthetic-id versions=%v want 1", n)
	}
	if id := ptEval(t, v, `$b.versions.first.item_id`); ptStr(t, id) != "pt-1" {
		t.Errorf("synthetic item_id = %q want pt-1", ptStr(t, id))
	}
}

// TestPaperTrailConfigFilters covers has_paper_trail only:/ignore:/on:/skip: and
// the ptNameList option shapes (array, single value, nil, absent).
func TestPaperTrailConfigFilters(t *testing.T) {
	v := New(io.Discard)
	// only: [array] with on: [:create] and skip: single value.
	ptEval(t, v, `
class Post
  attr_accessor :id, :title, :body, :secret
  has_paper_trail only: [:title], on: [:create], skip: :secret, ignore: nil
end`)
	ptEval(t, v, `$p = Post.new; $p.id = "p1"; $p.title = "t"; $p.body = "b"; $p.secret = "s"; $p.save`)
	// on: [:create] means an update records nothing.
	ptEval(t, v, `$p.title = "t2"; $p.save`)
	if n := ptEval(t, v, `$p.versions.length`); n != object.Integer(1) {
		t.Errorf("on:create only: versions=%v want 1", n)
	}
	// skip: :secret keeps it out of the create changeset.
	if got := ptEval(t, v, `$p.versions.first.object_changes.key?("secret")`); got != object.False {
		t.Errorf("skip secret present in changeset: %#v", got)
	}

	// ignore: single value; a change only to an ignored attribute is a no-op.
	ptEval(t, v, `
class Doc
  attr_accessor :id, :body, :views
  has_paper_trail ignore: :views
end`)
	ptEval(t, v, `$d = Doc.new; $d.id = "d1"; $d.body = "x"; $d.save`)
	ptEval(t, v, `$d.views = 5; $d.save`) // ignored-only change -> no version
	if n := ptEval(t, v, `$d.versions.length`); n != object.Integer(1) {
		t.Errorf("ignore-only update: versions=%v want 1", n)
	}
}

// TestPaperTrailWhodunnitAndClock covers the whodunnit seam (PaperTrail.request
// and PaperTrail module accessors), the enabled toggle, and the clock seam
// stamped onto created_at.
func TestPaperTrailWhodunnitAndClock(t *testing.T) {
	v := New(io.Discard)
	old := ptNowFunc
	defer func() { ptNowFunc = old }()
	fixed := stdtime.Date(2031, 3, 4, 5, 6, 7, 0, stdtime.UTC)
	ptNowFunc = func() stdtime.Time { return fixed }

	ptEval(t, v, modelSrc)

	// No whodunnit yet -> nil on the version and the accessors.
	ptEval(t, v, `$w = Widget.new("w1", "n"); $w.save`)
	if wd := ptEval(t, v, `$w.versions.first.whodunnit`); !object.IsNil(wd) {
		t.Errorf("default whodunnit = %#v want nil", wd)
	}
	if wd := ptEval(t, v, `PaperTrail.request.whodunnit`); !object.IsNil(wd) {
		t.Errorf("request whodunnit = %#v want nil", wd)
	}
	if wd := ptEval(t, v, `PaperTrail.whodunnit`); !object.IsNil(wd) {
		t.Errorf("module whodunnit = %#v want nil", wd)
	}
	// created_at reflects the injected clock.
	if yr := ptEval(t, v, `$w.versions.first.created_at.year`); yr != object.Integer(2031) {
		t.Errorf("created_at year = %v want 2031", yr)
	}

	// Set whodunnit via the request; a new change stamps it.
	ptEval(t, v, `PaperTrail.request.whodunnit = "alice"`)
	if wd := ptEval(t, v, `PaperTrail.request.whodunnit`); ptStr(t, wd) != "alice" {
		t.Errorf("request whodunnit = %q want alice", ptStr(t, wd))
	}
	if en := ptEval(t, v, `PaperTrail.request.enabled?`); en != object.True {
		t.Errorf("request enabled? = %#v want true", en)
	}
	ptEval(t, v, `$w.name = "n2"; $w.save`)
	if wd := ptEval(t, v, `$w.versions.last.whodunnit`); ptStr(t, wd) != "alice" {
		t.Errorf("stamped whodunnit = %q want alice", ptStr(t, wd))
	}

	// Module-level whodunnit setter + reader.
	ptEval(t, v, `PaperTrail.whodunnit = "bob"`)
	if wd := ptEval(t, v, `PaperTrail.whodunnit`); ptStr(t, wd) != "bob" {
		t.Errorf("module whodunnit = %q want bob", ptStr(t, wd))
	}
	// Clearing whodunnit with nil.
	ptEval(t, v, `PaperTrail.request.whodunnit = nil`)
	if wd := ptEval(t, v, `PaperTrail.request.whodunnit`); !object.IsNil(wd) {
		t.Errorf("cleared whodunnit = %#v want nil", wd)
	}

	// Disabling versioning suppresses new versions.
	before := ptEval(t, v, `$w.versions.length`)
	ptEval(t, v, `PaperTrail.request.enabled = false`)
	if en := ptEval(t, v, `PaperTrail.enabled?`); en != object.False {
		t.Errorf("module enabled? = %#v want false", en)
	}
	ptEval(t, v, `$w.name = "n3"; $w.save`)
	if got := ptEval(t, v, `$w.versions.length`); got != before {
		t.Errorf("disabled save recorded a version: %v != %v", got, before)
	}
	// Re-enable via the module accessor.
	ptEval(t, v, `PaperTrail.enabled = true`)
	if en := ptEval(t, v, `PaperTrail.request.enabled?`); en != object.True {
		t.Errorf("re-enabled? = %#v want true", en)
	}
}

// TestPaperTrailStoreErrors covers the store-error arms: a save whose
// SaveVersion fails, and the query arms (versions / live? / neighbours) when
// VersionsFor fails, using a version first recorded healthy.
func TestPaperTrailStoreErrors(t *testing.T) {
	v := New(io.Discard)
	st := &ptToggleStore{inner: papertrail.NewMemoryStore()}
	v.paperTrail.store = st
	ptEval(t, v, modelSrc)
	ptEval(t, v, `$w = Widget.new("w1", "n"); $w.save`)
	// Grab a healthy version handle before flipping the store to fail.
	ptEval(t, v, `$ver = $w.versions.first`)

	st.failFor = true
	if c := ptEvalErr(t, v, `$w.versions`); c != "PaperTrail::Error" {
		t.Errorf("versions store error class = %q", c)
	}
	if c := ptEvalErr(t, v, `$w.paper_trail.live?`); c != "PaperTrail::Error" {
		t.Errorf("live? store error class = %q", c)
	}
	if c := ptEvalErr(t, v, `$w.paper_trail.previous_version`); c != "PaperTrail::Error" {
		t.Errorf("proxy previous_version store error class = %q", c)
	}
	if c := ptEvalErr(t, v, `$ver.previous_version`); c != "PaperTrail::Error" {
		t.Errorf("version previous_version store error class = %q", c)
	}
	if c := ptEvalErr(t, v, `$ver.next_version`); c != "PaperTrail::Error" {
		t.Errorf("version next_version store error class = %q", c)
	}

	st.failFor = false
	st.failSave = true
	if c := ptEvalErr(t, v, `$w.name = "z"; $w.save`); c != "PaperTrail::Error" {
		t.Errorf("save store error class = %q", c)
	}
	if c := ptEvalErr(t, v, `$w.destroy`); c != "PaperTrail::Error" {
		t.Errorf("destroy store error class = %q", c)
	}
}

// TestPaperTrailSerializerErrors covers the serializer-error arms: object /
// object_changes / reify with a load-failing serializer, the proxy's reified
// previous_version, and a dump-failing serializer on save.
func TestPaperTrailSerializerErrors(t *testing.T) {
	v := New(io.Discard)
	ser := &ptToggleSerializer{inner: papertrail.JSONSerializer{}}
	v.paperTrail.serializer = ser
	ptEval(t, v, modelSrc)
	// Record a create then an update, so there are versions to read back.
	ptEval(t, v, `$w = Widget.new("w1", "first"); $w.save; $w.name = "second"; $w.save`)

	ser.failLoad = true
	if c := ptEvalErr(t, v, `$w.versions.last.object`); c != "PaperTrail::Error" {
		t.Errorf("object load error class = %q", c)
	}
	if c := ptEvalErr(t, v, `$w.versions.last.object_changes`); c != "PaperTrail::Error" {
		t.Errorf("object_changes load error class = %q", c)
	}
	if c := ptEvalErr(t, v, `$w.versions.last.reify`); c != "PaperTrail::Error" {
		t.Errorf("reify load error class = %q", c)
	}
	if c := ptEvalErr(t, v, `$w.paper_trail.previous_version`); c != "PaperTrail::Error" {
		t.Errorf("proxy previous_version reify error class = %q", c)
	}

	ser.failLoad = false
	ser.failDump = true
	if c := ptEvalErr(t, v, `$w.name = "third"; $w.save`); c != "PaperTrail::Error" {
		t.Errorf("save dump error class = %q", c)
	}
}

// TestPaperTrailHelpers covers the pure helpers whose edge branches the Ruby
// surface cannot reach: argFirst with no argument and ptItemID's fallthrough.
func TestPaperTrailHelpers(t *testing.T) {
	if v := argFirst(nil); !object.IsNil(v) {
		t.Errorf("argFirst(nil) = %#v want nil", v)
	}
	if got := ptItemID(3.14); got != "" {
		t.Errorf("ptItemID(float) = %q want empty", got)
	}
	if got := ptItemID("abc"); got != "abc" {
		t.Errorf("ptItemID(string) = %q want abc", got)
	}
	if got := ptItemID(int64(9)); got != "9" {
		t.Errorf("ptItemID(int64) = %q want 9", got)
	}

	// The wrapper display/truthiness methods (used by inspect / to_s / boolean
	// context) are covered directly.
	for _, w := range []object.Value{&PTVersion{}, &PTProxy{}, &PTRequest{}} {
		if w.ToS() == "" || w.Inspect() == "" || !w.Truthy() {
			t.Errorf("wrapper %#v: ToS/Inspect/Truthy mismatch", w)
		}
	}
}
