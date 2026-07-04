// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math"
	"math/big"
	"testing"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	libarrow "github.com/go-ruby-arrow/arrow"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestArrowScalarToRuby covers arrowScalarToRuby across every Go element type the
// library's readers can produce — all integer widths (signed and unsigned,
// including the >int64 UInt64/UInt promotion to a Bignum), both float widths,
// String, Time, List ([]any) and Struct (map[string]any) — plus the defensive
// tail for an element the library never actually yields.
func TestArrowScalarToRuby(t *testing.T) {
	big64 := new(big.Int).SetUint64(uint64(math.MaxInt64) + 1)
	for _, c := range []struct {
		in   any
		want string // Ruby #inspect of the produced value
	}{
		{nil, "nil"},
		{true, "true"},
		{int8(-8), "-8"},
		{int16(-16), "-16"},
		{int32(-32), "-32"},
		{int64(-64), "-64"},
		{int(-1), "-1"},
		{uint8(8), "8"},
		{uint16(16), "16"},
		{uint32(32), "32"},
		{uint64(64), "64"},
		{uint64(math.MaxInt64) + 1, big64.String()},
		{uint(65), "65"},
		{uint(math.MaxInt64) + 1, big64.String()},
		{float32(1.5), "1.5"},
		{float64(2.5), "2.5"},
		{"hi", "\"hi\""},
		{[]any{int64(1), nil}, "[1, nil]"},
		{map[string]any{"b": int64(2), "a": int64(1)}, "{\"a\" => 1, \"b\" => 2}"},
		{struct{}{}, "nil"}, // defensive tail: an unmodelled element reads back nil
	} {
		if got := arrowScalarToRuby(c.in).Inspect(); got != c.want {
			t.Errorf("arrowScalarToRuby(%#v) = %q; want %q", c.in, got, c.want)
		}
	}
}

// TestArrowValueToScalar covers the Go-only branches of arrowValueToScalar that a
// Ruby program cannot reach directly: a Go-nil interface, a Bignum whose value
// still fits int64 (a Ruby literal in that range is an Integer, never a Bignum),
// a Bignum that fits uint64, a Symbol, a Time, and the TypeError for an
// unmappable value.
func TestArrowValueToScalar(t *testing.T) {
	if got := arrowValueToScalar(nil); got != nil {
		t.Errorf("arrowValueToScalar(nil) = %#v; want nil", got)
	}
	if got := arrowValueToScalar(&object.Bignum{I: big.NewInt(7)}); got != int64(7) {
		t.Errorf("Bignum(7) -> %#v; want int64(7)", got)
	}
	u := new(big.Int).SetUint64(uint64(math.MaxInt64) + 1)
	if got := arrowValueToScalar(&object.Bignum{I: u}); got != u.Uint64() {
		t.Errorf("Bignum(2^63) -> %#v; want uint64", got)
	}
	if got := arrowValueToScalar(object.Symbol("sym")); got != "sym" {
		t.Errorf("Symbol -> %#v; want \"sym\"", got)
	}
	tm := &Time{t: gotime.FromUnix(1000)}
	if got, ok := arrowValueToScalar(tm).(stdtime.Time); !ok || got.Unix() != 1000 {
		t.Errorf("Time -> %#v; want time.Time@1000", got)
	}
	// A value with no Arrow representation raises TypeError (an Arrow::DataType
	// wrapper is not itself a storable element).
	assertRaises(t, "TypeError", func() { arrowValueToScalar(&ArrowDataType{dt: libarrow.Int64()}) })
}

// TestArrowRaiseErr covers raiseArrowErr: nil is a no-op, a library *Error maps to
// its faithful Ruby class, and a foreign error falls back to RuntimeError (the
// defensive branch the library's own errors never reach).
func TestArrowRaiseErr(t *testing.T) {
	raiseArrowErr(nil) // no panic

	_, ipcErr := libarrow.DecodeTable([]byte("not arrow"))
	assertRaises(t, "Arrow::Error::Io", func() { raiseArrowErr(ipcErr) })
	assertRaises(t, "RuntimeError", func() { raiseArrowErr(errors.New("boom")) })
}

// TestArrowWrapperRendering covers the object.Value surface (ToS / Inspect /
// Truthy) of every Arrow wrapper — reached by the VM's printing and
// boolean-coercion paths and pinned here directly.
func TestArrowWrapperRendering(t *testing.T) {
	dt := libarrow.Int64()
	field := libarrow.NewField("a", dt)
	schema := libarrow.NewSchema(field)
	arr, _ := libarrow.NewArray([]any{int64(1), int64(2)})
	builder := libarrow.NewArrayBuilder(dt)
	table, _ := libarrow.NewTable(schema, []*libarrow.Array{arr})

	wrappers := []object.Value{
		&ArrowDataType{dt: dt},
		&ArrowField{f: field},
		&ArrowSchema{s: schema},
		&ArrowArray{a: arr},
		&ArrowArrayBuilder{b: builder},
		&ArrowRecordBatch{r: table.RecordBatch()},
		&ArrowTable{t: table},
	}
	for _, w := range wrappers {
		if !w.Truthy() {
			t.Errorf("%T must be truthy", w)
		}
		if w.ToS() == "" || w.Inspect() == "" {
			t.Errorf("%T rendered empty ToS/Inspect", w)
		}
	}
}
