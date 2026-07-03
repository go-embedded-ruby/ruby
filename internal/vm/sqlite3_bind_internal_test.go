// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math/big"
	"testing"

	sqlite3 "github.com/go-ruby-sqlite3/sqlite3"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestSQLite3BindBridge covers the Go-only arms of the Ruby-value <-> driver
// mapping the Ruby tests do not reach directly.
func TestSQLite3BindBridge(t *testing.T) {
	// sqlite3Bind: every value shape.
	if v := sqlite3Bind(nil); v != nil {
		t.Errorf("go-nil -> %v", v)
	}
	if v := sqlite3Bind(object.NilV); v != nil {
		t.Errorf("NilV -> %v", v)
	}
	if v := sqlite3Bind(object.Bool(true)); v != int64(1) {
		t.Errorf("true -> %v", v)
	}
	if v := sqlite3Bind(object.Bool(false)); v != int64(0) {
		t.Errorf("false -> %v", v)
	}
	if v := sqlite3Bind(object.Integer(7)); v != int64(7) {
		t.Errorf("int -> %v", v)
	}
	bn := &object.Bignum{I: big.NewInt(9)}
	if v := sqlite3Bind(bn); v != int64(9) {
		t.Errorf("bignum -> %v", v)
	}
	if v := sqlite3Bind(object.Float(2.5)); v != 2.5 {
		t.Errorf("float -> %v", v)
	}
	if v := sqlite3Bind(object.NewString("hi")); v != "hi" {
		t.Errorf("string -> %v", v)
	}
	if v := sqlite3Bind(object.NewStringBytesEnc([]byte{0xff}, "ASCII-8BIT")); string(v.([]byte)) != "\xff" {
		t.Errorf("binary -> %v", v)
	}
	if v := sqlite3Bind(object.Symbol("s")); v != "s" {
		t.Errorf("symbol -> %v", v)
	}
	// Default arm: a value with a to_s (an Array).
	if v := sqlite3Bind(&object.Array{}); v != "[]" {
		t.Errorf("default -> %v", v)
	}
}

// TestSQLite3ValueBridge covers the driver-value -> Ruby mapping arms.
func TestSQLite3ValueBridge(t *testing.T) {
	vm := New(nil)
	if v := sqlite3Value(vm, nil); v != object.NilV {
		t.Errorf("nil -> %v", v)
	}
	if v := sqlite3Value(vm, int64(3)); v != object.Integer(3) {
		t.Errorf("int64 -> %v", v)
	}
	if v := sqlite3Value(vm, float64(1.5)); v != object.Float(1.5) {
		t.Errorf("float64 -> %v", v)
	}
	if s, ok := object.KindOK[*object.String](sqlite3Value(vm, "x")); !ok || s.Str() != "x" {
		t.Errorf("string -> %v", sqlite3Value(vm, "x"))
	}
	if s, ok := object.KindOK[*object.String](sqlite3Value(vm, []byte{0x00})); !ok || !s.IsBinary() {
		t.Errorf("[]byte -> %v", sqlite3Value(vm, []byte{0x00}))
	}
	if v := sqlite3Value(vm, true); v != object.Bool(true) {
		t.Errorf("bool -> %v", v)
	}
	// Default (never produced by the driver) arm.
	if v := sqlite3Value(vm, struct{}{}); v != object.NilV {
		t.Errorf("default -> %v", v)
	}
}

// TestSQLite3BindKey covers sqlite3BindKey's Integer / Symbol / String / default
// arms.
func TestSQLite3BindKey(t *testing.T) {
	if k := sqlite3BindKey(object.Integer(2)); k != 2 {
		t.Errorf("int key -> %v", k)
	}
	if k := sqlite3BindKey(object.Symbol("v")); k != "v" {
		t.Errorf("sym key -> %v", k)
	}
	if k := sqlite3BindKey(object.NewString("n")); k != "n" {
		t.Errorf("str key -> %v", k)
	}
	if k := sqlite3BindKey(&object.Array{}); k != "[]" {
		t.Errorf("default key -> %v", k)
	}
}

// TestSQLite3IntArgBignum covers the Bignum arm of sqlite3IntArg (the Integer
// arm is reached through busy_timeout= in the Ruby tests).
func TestSQLite3IntArgBignum(t *testing.T) {
	bn := &object.Bignum{I: big.NewInt(100)}
	if n := sqlite3IntArg(bn); n != 100 {
		t.Errorf("bignum arg -> %d", n)
	}
}

// TestSQLite3IntArgTypeError covers sqlite3IntArg's non-integer raise.
func TestSQLite3IntArgTypeError(t *testing.T) {
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok || re.Class != "TypeError" {
			t.Errorf("want TypeError, got %v", r)
		}
	}()
	sqlite3IntArg(object.NewString("x"))
}

// TestSQLite3ModeDefault covers sqlite3Mode's no-argument default arm.
func TestSQLite3ModeDefault(t *testing.T) {
	if m := sqlite3Mode(nil); m != sqlite3.Deferred {
		t.Errorf("default mode -> %q", m)
	}
	if m := sqlite3Mode([]object.Value{object.NewString("exclusive")}); m != sqlite3.Exclusive {
		t.Errorf("exclusive mode -> %q", m)
	}
	if m := sqlite3Mode([]object.Value{object.NewString("weird")}); m != sqlite3.Deferred {
		t.Errorf("unknown mode -> %q", m)
	}
}

// TestSQLite3StringArgToS covers sqlite3StringArg's non-String to_s arm.
func TestSQLite3StringArgToS(t *testing.T) {
	if s := sqlite3StringArg(object.Integer(5)); s != "5" {
		t.Errorf("to_s arg -> %q", s)
	}
}

// TestSQLite3RaiseErrorGeneric covers raiseSQLite3Error's non-*Error fallback,
// which raises the base SQLite3::Exception.
func TestSQLite3RaiseErrorGeneric(t *testing.T) {
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok || re.Class != string(sqlite3.ExcException) {
			t.Errorf("want %s, got %v", sqlite3.ExcException, r)
		}
	}()
	raiseSQLite3Error(errors.New("plain"))
}

// TestSQLite3HashColsNoOwner covers sqlite3HashCols returning nil for a
// statement built without an owning database wrapper.
func TestSQLite3HashColsNoOwner(t *testing.T) {
	if cols := sqlite3HashCols(&SQLite3Statement{}); cols != nil {
		t.Errorf("no-owner cols -> %v", cols)
	}
}

// TestSQLite3SpreadNoArray covers sqlite3Spread's non-Array (pass-through) arm.
func TestSQLite3SpreadNoArray(t *testing.T) {
	in := []object.Value{object.Integer(1), object.Integer(2)}
	out := sqlite3Spread(in)
	if len(out) != 2 || out[0] != object.Integer(1) {
		t.Errorf("no-array spread -> %v", out)
	}
	// A single non-Array argument also passes through.
	if got := sqlite3Spread([]object.Value{object.Integer(9)}); len(got) != 1 {
		t.Errorf("single scalar spread -> %v", got)
	}
}

// TestSQLite3ExecuteHashPrepareError covers sqlite3ExecuteHash's prepare-error
// arm by running against a closed database.
func TestSQLite3ExecuteHashPrepareError(t *testing.T) {
	db := sqlite3Open(":memory:")
	_ = db.db.Close()
	if _, _, err := sqlite3ExecuteHash(db.db, "SELECT 1", nil); err == nil {
		t.Error("want error preparing on a closed database")
	}
}

// TestSQLite3ValueMethods covers the ToS / Inspect / Truthy display methods on
// the Database and Statement wrappers.
func TestSQLite3ValueMethods(t *testing.T) {
	db := &SQLite3Database{}
	if db.ToS() != "#<SQLite3::Database>" || db.Inspect() != "#<SQLite3::Database>" || !db.Truthy() {
		t.Error("Database display methods")
	}
	st := &SQLite3Statement{}
	if st.ToS() != "#<SQLite3::Statement>" || st.Inspect() != "#<SQLite3::Statement>" || !st.Truthy() {
		t.Error("Statement display methods")
	}
}
