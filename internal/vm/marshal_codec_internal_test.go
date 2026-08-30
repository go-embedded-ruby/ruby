package vm

import (
	"io"
	"math/big"
	"strings"
	"testing"
	stdtime "time"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-marshal/marshal"
)

// expectMarshalRaise runs fn and asserts it panics with a RubyError of the given class
// whose message contains want.
func expectMarshalRaise(t *testing.T, class, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok {
			t.Fatalf("expected RubyError, got %v", r)
		}
		if re.Class != class || !strings.Contains(re.Message, want) {
			t.Fatalf("got %s:%q, want %s containing %q", re.Class, re.Message, class, want)
		}
	}()
	fn()
}

// TestToMarshalValueBranches exercises the legacy converters kept for
// pstore / sinatra_session across every value kind, including the shared-pointer
// links, the hash default, and the two error branches.
func TestToMarshalValueBranches(t *testing.T) {
	seen := map[object.Value]marshal.Value{}
	s := object.NewString("x")
	a := object.NewArray(object.Integer(1))
	h := object.NewHash()
	h.Set(object.Symbol("k"), object.NilV)
	h.Default = object.Integer(0)
	big70 := object.NormInt(new(big.Int).Lsh(big.NewInt(1), 70))
	root := object.NewArray(
		object.NilV, object.Bool(true), object.Integer(3), big70,
		object.Float(1.5), object.Symbol("s"), s, s, a, a, h, h,
	)
	mv := toMarshalValue(root, seen)
	// Round-trip through the engine and back, covering fromMarshalValue too.
	back := fromMarshalValue(mv, map[marshal.Value]object.Value{})
	ra, ok := back.(*object.Array)
	if !ok || len(ra.Elems) != 12 {
		t.Fatalf("round-trip shape wrong: %#v", back)
	}
	if !ra.Elems[6].(*object.String).Frozen && ra.Elems[6] != ra.Elems[7] {
		t.Fatalf("shared string identity not preserved")
	}
	rh := ra.Elems[10].(*object.Hash)
	if rh.Default == nil {
		t.Fatalf("hash default lost")
	}

	// A hash carrying a default proc cannot be converted.
	hp := object.NewHash()
	hp.DefaultProc = &Proc{}
	expectMarshalRaise(t, "TypeError", "default proc", func() {
		toMarshalValue(hp, map[object.Value]marshal.Value{})
	})
	// An unsupported value kind panics with the MRI _dump_data message.
	expectMarshalRaise(t, "TypeError", "_dump_data", func() {
		toMarshalValue(&Proc{}, map[object.Value]marshal.Value{})
	})
}

// TestMarshalClassErrors covers marshalClass's not-a-class / undefined branches.
func TestMarshalClassErrors(t *testing.T) {
	vm := New(io.Discard)
	if got := vm.marshalClass("String"); got != vm.consts["String"] {
		t.Fatalf("String lookup failed")
	}
	// First segment resolves to a non-class constant.
	vm.cObject.consts["MConst"] = object.Integer(1)
	expectMarshalRaise(t, "ArgumentError", "undefined class/module MConst::Foo", func() {
		vm.marshalClass("MConst::Foo")
	})
	// Middle segment undefined under a real class.
	expectMarshalRaise(t, "ArgumentError", "undefined class/module String::Nope", func() {
		vm.marshalClass("String::Nope")
	})
	// A constant that exists but is not a class/module.
	vm.cObject.consts["MNum"] = object.Integer(7)
	expectMarshalRaise(t, "ArgumentError", "undefined class/module MNum", func() {
		vm.marshalClass("MNum")
	})
}

// TestMarshalBigIntTypeError covers marshalBigInt's non-integer branch.
func TestMarshalBigIntTypeError(t *testing.T) {
	if marshalBigInt(object.Integer(5)).Int64() != 5 {
		t.Fatalf("Integer conversion wrong")
	}
	if marshalBigInt(&object.Bignum{I: big.NewInt(9)}).Int64() != 9 {
		t.Fatalf("Bignum conversion wrong")
	}
	expectMarshalRaise(t, "TypeError", "conversion to Integer", func() {
		marshalBigInt(object.NilV)
	})
}

// TestMarshalIsKwargs covers the empty-hash and non-symbol-key branches.
func TestMarshalIsKwargs(t *testing.T) {
	if marshalIsKwargs(object.NewHash()) {
		t.Fatalf("empty hash must not be kwargs")
	}
	h := object.NewHash()
	h.Set(object.NewString("k"), object.NilV)
	if marshalIsKwargs(h) {
		t.Fatalf("string-keyed hash must not be kwargs")
	}
	kw := object.NewHash()
	kw.Set(object.Symbol("freeze"), object.Bool(true))
	if !marshalIsKwargs(kw) {
		t.Fatalf("symbol-keyed hash must be kwargs")
	}
}

// TestMarshalTimeNonUTC covers writeTime's fixed-offset branch (a non-UTC Time
// dumps with an :offset ivar) and the packed-form round-trip.
func TestMarshalTimeNonUTC(t *testing.T) {
	vm := New(io.Discard)
	loc := stdtime.FixedZone("X", 2*3600)
	tm := &Time{t: stdtime.Date(1970, 1, 1, 2, 0, 0, 0, loc)}
	b := vm.marshalDump(tm, -1)
	// The UTC bit (0x40) must be clear: byte index 15 is the high byte of the
	// packed "p" word (2 header + I u :Time <len> + 3 low payload bytes).
	if b[15]&0x40 != 0 {
		t.Fatalf("non-UTC time set the UTC bit: % x", b)
	}
	back := vm.marshalLoad(b, nil, false)
	if _, ok := back.(*Time); !ok {
		t.Fatalf("non-UTC time did not round-trip to a Time: %#v", back)
	}
}

// TestMarshalEncodingIvarNonString covers applyIvar's defensive branch when an
// I-wrapped string's :encoding ivar is not itself a String.
func TestMarshalEncodingIvarNonString(t *testing.T) {
	vm := New(io.Discard)
	r := &mReader{vm: vm}
	s := object.NewString("x")
	r.applyIvar(s, "encoding", object.Integer(1)) // ignored, no panic
	if s.Enc != "" {
		t.Fatalf("encoding must be unchanged, got %q", s.Enc)
	}
}
