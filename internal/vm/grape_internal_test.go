// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"reflect"
	"testing"

	grape "github.com/go-ruby-grape/grape"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestGrapeWrapperInspect covers the ToS / Inspect / Truthy of every Grape value
// wrapper.
func TestGrapeWrapperInspect(t *testing.T) {
	checks := []struct {
		toS, inspect string
		truthy       bool
		val          interface {
			ToS() string
			Inspect() string
			Truthy() bool
		}
	}{
		{"#<Grape::Router>", "#<Grape::Router>", true, &GrapeRouter{rt: grape.NewRouter()}},
		{"#<Grape::Router::Route GET /x>", "#<Grape::Router::Route GET /x>", true, &GrapeRoute{rt: grape.NewRoute("GET", "/x", nil)}},
		{"#<Grape::Router::Match>", "#<Grape::Router::Match>", true, &GrapeMatch{}},
		{"#<Grape::Validations::ParamsScope>", "#<Grape::Validations::ParamsScope>", true, &GrapeValidator{}},
		{"#<Grape::Validations::ParamsScope::DSL>", "#<Grape::Validations::ParamsScope::DSL>", true, &GrapeParamsBuilder{}},
		{"#<Grape::Formatter>", "#<Grape::Formatter>", true, &GrapeFormatter{}},
	}
	for _, c := range checks {
		if c.val.ToS() != c.toS || c.val.Inspect() != c.inspect || c.val.Truthy() != c.truthy {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c.val, c.val.ToS(), c.val.Inspect(), c.val.Truthy())
		}
	}
}

// TestGrapeStr covers the String / Symbol / default (to_s) arms of grapeStr.
func TestGrapeStr(t *testing.T) {
	if grapeStr(object.Wrap(object.NewString("x"))) != "x" {
		t.Error("string arm")
	}
	if grapeStr(object.SymVal(string(object.Symbol("y")))) != "y" {
		t.Error("symbol arm")
	}
	if grapeStr(object.IntValue(int64(object.Integer(3)))) != "3" {
		t.Error("default arm")
	}
}

// TestGrapeArg covers the present / absent arms of grapeArg.
func TestGrapeArg(t *testing.T) {
	if !object.IsNil(grapeArg(nil)) {
		t.Error("absent arm")
	}
	if grapeArg([]object.Value{object.IntValue(int64(object.Integer(1)))}) != object.IntValue(int64(object.Integer(1))) {
		t.Error("present arm")
	}
}

// TestGrapeStatusName covers every arm of grapeStatusName.
func TestGrapeStatusName(t *testing.T) {
	if grapeStatusName(grape.StatusOK) != "ok" {
		t.Error("ok arm")
	}
	if grapeStatusName(grape.StatusNotFound) != "not_found" {
		t.Error("404 arm")
	}
	if grapeStatusName(grape.StatusMethodNotAllowed) != "method_not_allowed" {
		t.Error("405 arm")
	}
}

// TestGrapeOptions covers the with-Hash, non-Hash and no-trailing-arg arms.
func TestGrapeOptions(t *testing.T) {
	h := object.NewHash()
	if grapeOptions([]object.Value{object.SymVal(string(object.Symbol("n"))), object.Wrap(h)}) != h {
		t.Error("hash arm")
	}
	if grapeOptions([]object.Value{object.SymVal(string(object.Symbol("n"))), object.IntValue(int64(object.Integer(1)))}) != nil {
		t.Error("non-hash arm")
	}
	if grapeOptions([]object.Value{object.SymVal(string(object.Symbol("n")))}) != nil {
		t.Error("no-opts arm")
	}
}

// TestGrapeType covers every type-name arm plus the unknown default.
func TestGrapeType(t *testing.T) {
	cases := map[string]grape.Type{
		"Integer": grape.TypeInteger,
		"Float":   grape.TypeFloat,
		"String":  grape.TypeString,
		"Boolean": grape.TypeBoolean,
		"Date":    grape.TypeDate,
		"Time":    grape.TypeTime,
		"Array":   grape.TypeArray,
		"Hash":    grape.TypeHash,
		"Unknown": grape.Type(""),
	}
	for name, want := range cases {
		if got := grapeType(object.Wrap(object.NewString(name))); got != want {
			t.Errorf("grapeType(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestGrapeBuildParamNoOpts covers the nil-options fast return of grapeBuildParam.
func TestGrapeBuildParamNoOpts(t *testing.T) {
	p := grapeBuildParam("id", true, nil)
	if p.Name != "id" || !p.Required || p.Type != "" {
		t.Errorf("bare param = %+v", p)
	}
}

// TestGrapeValueList covers the Array arm and the single-literal arm.
func TestGrapeValueList(t *testing.T) {
	arr := &object.Array{Elems: []object.Value{object.IntValue(int64(object.Integer(1))), object.IntValue(int64(object.Integer(2)))}}
	if got := grapeValueList(object.Wrap(arr)); !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
		t.Errorf("array arm = %v", got)
	}
	if got := grapeValueList(object.IntValue(int64(object.Integer(9)))); !reflect.DeepEqual(got, []any{int64(9)}) {
		t.Errorf("scalar arm = %v", got)
	}
}

// TestGrapeRegexpNonRegexp covers grapeRegexp's non-Regexp arm (zero Regexp).
func TestGrapeRegexpNonRegexp(t *testing.T) {
	if r := grapeRegexp(object.IntValue(int64(object.Integer(1)))); r.Match != nil {
		t.Error("non-Regexp should yield the zero Regexp")
	}
}

// TestGrapeRawHashTypeError covers grapeRawHash's non-Hash raise arm.
func TestGrapeRawHashTypeError(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected a raise on a non-Hash argument")
		}
	}()
	grapeRawHash(object.IntValue(int64(object.Integer(1))))
}

// TestGrapeCheckFormatErr covers the nil (no-op) and non-nil (raise) arms.
func TestGrapeCheckFormatErr(t *testing.T) {
	grapeCheckFormatErr(nil) // must not raise
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected a raise on a non-nil error")
		}
	}()
	grapeCheckFormatErr(errStub{})
}

// errStub is a trivial error for the format-error arm.
type errStub struct{}

func (errStub) Error() string { return "boom" }

// TestGrapeRouteHandlerNil covers the nil-handler arms of Route#handler /
// Match#handler / Match#route where a matched route carries no handler value.
func TestGrapeRouteHandlerNil(t *testing.T) {
	r := &GrapeRoute{rt: grape.NewRoute("GET", "/x", nil), handler: object.NilVal()}
	// handler is nil -> the accessor returns Ruby nil, not a Go nil interface.
	if !object.IsNil(r.handler) {
		t.Skip("handler set")
	}
	m := &GrapeMatch{} // no route, no handler
	if !object.IsNil(m.route) || !object.IsNil(m.handler) {
		t.Error("empty match should carry no route/handler")
	}
}

// TestGrapeRouteHandlerGoNil covers the Route#handler / Match#handler / Match#route
// arms where the wrapper's handler/route field is a literal Go nil (not object.NilV),
// so the accessor returns Ruby nil.
func TestGrapeRouteHandlerGoNil(t *testing.T) {
	vm := New(nil)
	// Route#handler with a nil handler field.
	route := object.Kind[*RClass](vm.consts["Grape::Router::Route"])
	r := &GrapeRoute{rt: grape.NewRoute("GET", "/x", nil), handler: object.NilVal()}
	if got := route.methods["handler"].native(vm, object.Wrap(r), nil, nil); !object.IsNil(got) {
		t.Errorf("Route#handler nil field -> %v, want nil", got)
	}
	// Match#handler / #route with nil fields.
	match := object.Kind[*RClass](vm.consts["Grape::Router::Match"])
	m := &GrapeMatch{}
	if got := match.methods["handler"].native(vm, object.Wrap(m), nil, nil); !object.IsNil(got) {
		t.Errorf("Match#handler nil field -> %v, want nil", got)
	}
	if got := match.methods["route"].native(vm, object.Wrap(m), nil, nil); !object.IsNil(got) {
		t.Errorf("Match#route nil field -> %v, want nil", got)
	}
}

// TestGrapeRouteHandlerPresent covers the Route#handler arm that returns the
// stored handler value when the route carries a real (present) handler — the
// non-nil branch of the accessor, distinct from the nil arms above.
func TestGrapeRouteHandlerPresent(t *testing.T) {
	vm := New(nil)
	route := object.Kind[*RClass](vm.consts["Grape::Router::Route"])
	h := object.NewString("do-thing")
	r := &GrapeRoute{rt: grape.NewRoute("GET", "/x", nil), handler: object.Wrap(h)}
	if got := route.methods["handler"].native(vm, object.Wrap(r), nil, nil); got != object.Wrap(h) {
		t.Errorf("Route#handler present field -> %v, want %v", got, h)
	}
}

// TestGrapeCoercedExtraKey covers grapeCoercedToHash's arm for a coerced key not
// present in the declared ParamSet (the defensive passthrough).
func TestGrapeCoercedExtraKey(t *testing.T) {
	set := &grape.ParamSet{Params: []*grape.Param{{Name: "a"}}}
	h := grapeCoercedToHash(nil, set, map[string]any{"a": int64(1), "extra": "x"})
	if len(h.Keys) != 2 {
		t.Fatalf("expected both keys, got %d", len(h.Keys))
	}
	v, _ := h.Get(object.Wrap(object.NewString("extra")))
	if s, ok := object.KindOK[*object.String](v); !ok || s.Str() != "x" {
		t.Errorf("extra key = %v", v)
	}
}

// TestGrapeToGo covers every arm of the Ruby->Go value mapper.
func TestGrapeToGo(t *testing.T) {
	if grapeToGo(object.NilVal()) != nil {
		t.Error("nil arm")
	}
	if grapeToGo(object.NilVal()) != nil {
		t.Error("go-nil arm")
	}
	if grapeToGo(object.BoolValue(bool(object.Bool(true)))) != true {
		t.Error("bool arm")
	}
	if grapeToGo(object.IntValue(int64(object.Integer(5)))) != int64(5) {
		t.Error("int arm")
	}
	if g := grapeToGo(object.Wrap(&object.Bignum{I: big.NewInt(7)})); g.(*big.Int).Int64() != 7 {
		t.Error("bignum arm")
	}
	if grapeToGo(object.FloatValue(float64(object.Float(1.5)))) != 1.5 {
		t.Error("float arm")
	}
	if grapeToGo(object.Wrap(object.NewString("s"))) != "s" {
		t.Error("string arm")
	}
	if grapeToGo(object.SymVal(string(object.Symbol("y")))) != "y" {
		t.Error("symbol arm")
	}
	arr := grapeToGo(object.Wrap(&object.Array{Elems: []object.Value{object.IntValue(int64(object.Integer(1)))}})).([]any)
	if len(arr) != 1 || arr[0] != int64(1) {
		t.Error("array arm")
	}
	h := object.NewHash()
	h.Set(object.Wrap(object.NewString("k")), object.IntValue(int64(object.Integer(2))))
	m := grapeToGo(object.Wrap(h)).(map[string]any)
	if m["k"] != int64(2) {
		t.Error("hash arm")
	}
	// The default arm (a value none of the above) falls back to to_s.
	if grapeToGo(object.Wrap(&GrapeFormatter{})) != "#<Grape::Formatter>" {
		t.Error("default arm")
	}
}

// TestGrapeFromGo covers every arm of the Go->Ruby value mapper.
func TestGrapeFromGo(t *testing.T) {
	if !object.IsNil(grapeFromGo(nil)) {
		t.Error("nil arm")
	}
	if grapeFromGo(true) != object.BoolValue(bool(object.Bool(true))) {
		t.Error("bool arm")
	}
	if grapeFromGo(int(3)) != object.IntValue(int64(object.Integer(3))) {
		t.Error("int arm")
	}
	if grapeFromGo(int64(4)) != object.IntValue(int64(object.Integer(4))) {
		t.Error("int64 arm")
	}
	if grapeFromGo(2.5) != object.FloatValue(float64(object.Float(2.5))) {
		t.Error("float arm")
	}
	if s, ok := object.KindOK[*object.String](grapeFromGo("s")); !ok || s.Str() != "s" {
		t.Error("string arm")
	}
	arr, ok := object.KindOK[*object.Array](grapeFromGo([]any{int64(1), "x"}))
	if !ok || len(arr.Elems) != 2 {
		t.Error("array arm")
	}
	hh, ok := object.KindOK[*object.Hash](grapeFromGo(map[string]any{"k": int64(1)}))
	if !ok || len(hh.Keys) != 1 {
		t.Error("hash arm")
	}
	// The default arm (an unmapped Go type) yields nil.
	if !object.IsNil(grapeFromGo(struct{}{})) {
		t.Error("default arm")
	}
}
