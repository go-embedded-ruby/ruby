// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"math/big"
	"testing"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	date "github.com/go-ruby-date/date"
	mysql "github.com/go-ruby-mysql/mysql"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// stringerValue is a mysql.Value with a String method, exercising mysqlSprint's
// Stringer branch through mysqlValue's default arm.
type stringerValue struct{}

func (stringerValue) String() string { return "custom" }

// TestMySQLValueDefault covers mysqlValue's default arm (a value outside mysql2's
// cast set) and both mysqlSprint branches: a Stringer renders via String(), a
// non-Stringer renders as the empty string.
func TestMySQLValueDefault(t *testing.T) {
	vm := New(io.Discard)
	if got := vm.mysqlValue(stringerValue{}).ToS(); got != "custom" {
		t.Errorf("stringer default = %q, want %q", got, "custom")
	}
	// int32 is not in the cast set and has no String(): empty string.
	if got := vm.mysqlValue(int32(7)).ToS(); got != "" {
		t.Errorf("non-stringer default = %q, want empty", got)
	}
}

// TestMySQLDateError covers mysqlDate's unreachable-in-production error branch:
// an invalid calendar date raises Mysql2::Error.
func TestMySQLDateError(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic for an invalid date")
		}
		re, ok := r.(RubyError)
		if !ok || re.Class != "Mysql2::Error" {
			t.Fatalf("expected Mysql2::Error, got %#v", r)
		}
	}()
	mysqlDate(mysql.Date{Year: 2026, Month: 13, Day: 1})
}

// TestMySQLBind covers every mysqlBind arm: Ruby values map to the Go argument
// types the mysql library's prepared-statement path accepts.
func TestMySQLBind(t *testing.T) {
	bin := object.NewStringBytesEnc([]byte{0x41, 0x42}, "ASCII-8BIT")
	tm := &Time{t: gotime.FromUnix(1000)}
	dec := newDecimalString("3.14")
	rd, _ := date.NewDate(2026, 1, 2)
	dt := &Date{d: rd}

	cases := []struct {
		name string
		in   object.Value
		want any
	}{
		{"nil", object.NilV, nil},
		{"bool", object.Bool(true), true},
		{"int", object.IntValue(5), int64(5)},
		{"bignum", &object.Bignum{I: big.NewInt(42)}, "42"},
		{"float", object.Float(1.5), 1.5},
		{"string", object.NewString("x"), "x"},
		{"symbol", object.Symbol("s"), "s"},
		{"bigdecimal", dec, "0.314e1"},
		{"date", dt, "2026-01-02"},
	}
	for _, c := range cases {
		if got := mysqlBind(c.in); got != c.want {
			t.Errorf("%s: mysqlBind = %#v, want %#v", c.name, got, c.want)
		}
	}

	// Binary String -> []byte (compared element-wise).
	if got, ok := mysqlBind(bin).([]byte); !ok || string(got) != "AB" {
		t.Errorf("binary: mysqlBind = %#v, want []byte(\"AB\")", mysqlBind(bin))
	}
	// Time -> time.Time.
	if got, ok := mysqlBind(tm).(stdtime.Time); !ok || got.Unix() != 1000 {
		t.Errorf("time: mysqlBind = %#v, want time.Time@1000", mysqlBind(tm))
	}
	// Default arm: an unmapped value (Array) falls back to #to_s.
	if got := mysqlBind(object.NewArrayFromSlice([]object.Value{object.IntValue(1)})); got != "[1]" {
		t.Errorf("default: mysqlBind = %#v, want %q", got, "[1]")
	}
}
