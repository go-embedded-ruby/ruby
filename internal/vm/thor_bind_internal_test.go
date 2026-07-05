// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	thor "github.com/go-ruby-thor/thor"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestThorValueProtocol covers the ToS / Inspect / Truthy arms of every Thor
// wrapper, exercised directly for completeness.
func TestThorValueProtocol(t *testing.T) {
	vals := []struct {
		v    object.Value
		want string
	}{
		{&ThorOption{}, "#<Thor::Option>"},
		{&ThorOptions{}, "#<Thor::Options>"},
		{&ThorCommand{}, "#<Thor::Command>"},
		{&ThorBase{}, "#<Thor::Base>"},
	}
	for _, c := range vals {
		if c.v.ToS() != c.want || c.v.Inspect() != c.want || !c.v.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c.v, c.v.ToS(), c.v.Inspect(), c.v.Truthy())
		}
	}
}

// TestThorValueToGo covers every arm of the Ruby→Thor value mapper, including the
// nil arm and the terminal #to_s fallback.
func TestThorValueToGo(t *testing.T) {
	if got := thorValueToGo(object.Integer(7)); got != int64(7) {
		t.Errorf("int arm got=%v", got)
	}
	if got := thorValueToGo(object.Float(2.5)); got != 2.5 {
		t.Errorf("float arm got=%v", got)
	}
	if got := thorValueToGo(object.Bool(true)); got != true {
		t.Errorf("bool arm got=%v", got)
	}
	if got := thorValueToGo(object.NewString("s")); got != "s" {
		t.Errorf("string arm got=%v", got)
	}
	if got := thorValueToGo(object.Symbol("sym")); got != "sym" {
		t.Errorf("symbol arm got=%v", got)
	}
	arr := thorValueToGo(object.NewArrayFromSlice([]object.Value{object.NewString("a"), object.Integer(2)}))
	if s, ok := arr.([]string); !ok || len(s) != 2 || s[0] != "a" || s[1] != "2" {
		t.Errorf("array arm got=%v", arr)
	}
	h := object.NewHash()
	h.Set(object.NewString("k"), object.NewString("v"))
	m := thorValueToGo(h)
	if om, ok := m.(*thor.OrderedMap); !ok {
		t.Errorf("hash arm got=%T", m)
	} else if v, _ := om.Get("k"); v != "v" {
		t.Errorf("hash arm value got=%q", v)
	}
	if got := thorValueToGo(object.NilV); got != nil {
		t.Errorf("nil arm got=%v", got)
	}
	// The terminal fallback: an unmapped Ruby value degrades to its #to_s.
	if got := thorValueToGo(&ThorOption{}); got != "#<Thor::Option>" {
		t.Errorf("fallback arm got=%v", got)
	}
}

// TestThorValueToRuby covers every arm of the Thor→Ruby value mapper, including
// the terminal arm for a Go type the model never yields.
func TestThorValueToRuby(t *testing.T) {
	if got := thorValueToRuby(nil); got != object.NilV {
		t.Errorf("nil arm got=%v", got)
	}
	if got := thorValueToRuby(true); got != object.Bool(true) {
		t.Errorf("bool arm got=%v", got)
	}
	if got := thorValueToRuby("s"); got.ToS() != "s" {
		t.Errorf("string arm got=%v", got)
	}
	if got := thorValueToRuby(int64(7)); got != object.Integer(7) {
		t.Errorf("int arm got=%v", got)
	}
	if got := thorValueToRuby(2.5); got != object.Float(2.5) {
		t.Errorf("float arm got=%v", got)
	}
	if got := thorValueToRuby([]string{"a", "b"}); got.Inspect() != `["a", "b"]` {
		t.Errorf("array arm got=%v", got.Inspect())
	}
	om := thor.NewOrderedMap()
	om.Set("k", "v")
	if got := thorValueToRuby(om); got.Inspect() != `{"k" => "v"}` {
		t.Errorf("orderedmap arm got=%v", got.Inspect())
	}
	// A Go type the Thor model never yields degrades to nil.
	if got := thorValueToRuby(struct{}{}); got != object.NilV {
		t.Errorf("fallback arm got=%v", got)
	}
}

// TestThorName covers thorName's Symbol and String arms.
func TestThorName(t *testing.T) {
	if got := thorName(object.Symbol("boolean")); got != "boolean" {
		t.Errorf("symbol arm got=%q", got)
	}
	if got := thorName(object.NewString("boolean")); got != "boolean" {
		t.Errorf("string arm got=%q", got)
	}
}

// TestThorStrList covers thorStrList's Array and scalar arms.
func TestThorStrList(t *testing.T) {
	if got := thorStrList(object.NewArrayFromSlice([]object.Value{object.NewString("-f"), object.NewString("--flag")})); len(got) != 2 || got[0] != "-f" {
		t.Errorf("array arm got=%v", got)
	}
	if got := thorStrList(object.NewString("-f")); len(got) != 1 || got[0] != "-f" {
		t.Errorf("scalar arm got=%v", got)
	}
}

// TestThorArgAndConfigHelpers covers the small argument/config helpers' absent /
// wrong-type arms unreachable from a well-formed Ruby call.
func TestThorArgAndConfigHelpers(t *testing.T) {
	if thorOptHash(nil, 0) != nil {
		t.Error("thorOptHash absent should be nil")
	}
	if thorIntArg(nil, 0) != 0 {
		t.Error("thorIntArg absent should be 0")
	}
	if got := thorIntArg([]object.Value{object.NewString("x")}, 0); got != 0 {
		t.Errorf("thorIntArg non-int got=%d", got)
	}
	if got := thorIntArg([]object.Value{object.Integer(4)}, 0); got != 4 {
		t.Errorf("thorIntArg present got=%d", got)
	}
	if thorArgv(nil, 0) != nil {
		t.Error("thorArgv absent should be nil")
	}
	if thorArgv([]object.Value{object.NewString("x")}, 0) != nil {
		t.Error("thorArgv non-array should be nil")
	}
	// thorConfig full path.
	h := object.NewHash()
	h.Set(object.Symbol("basename"), object.NewString("app"))
	h.Set(object.Symbol("package_name"), object.NewString("Pkg"))
	h.Set(object.Symbol("terminal_width"), object.Integer(120))
	cfg := thorConfig(h)
	if cfg.Basename != "app" || cfg.PackageName != "Pkg" || cfg.TerminalWidth != 120 {
		t.Errorf("thorConfig got=%+v", cfg)
	}
	// terminal_width given a non-Integer leaves the width unset.
	h2 := object.NewHash()
	h2.Set(object.Symbol("terminal_width"), object.NewString("wide"))
	if thorConfig(h2).TerminalWidth != 0 {
		t.Error("thorConfig non-int terminal_width should stay 0")
	}
	if thorConfig(nil).Basename != "" {
		t.Error("thorConfig nil should be zero")
	}
}

// TestThorBuildOptionAllKeywords covers every keyword arm of thorBuildOption
// (including lazy_default / banner / group / hide / repeatable / enum) and the
// nil-hash arm.
func TestThorBuildOptionAllKeywords(t *testing.T) {
	var vm VM
	h := object.NewHash()
	h.Set(object.Symbol("desc"), object.NewString("d"))
	h.Set(object.Symbol("type"), object.Symbol("array"))
	h.Set(object.Symbol("required"), object.Bool(true))
	h.Set(object.Symbol("default"), object.NewArrayFromSlice([]object.Value{object.NewString("x")}))
	h.Set(object.Symbol("lazy_default"), object.NewString("lz"))
	h.Set(object.Symbol("aliases"), object.NewArrayFromSlice([]object.Value{object.NewString("-a")}))
	h.Set(object.Symbol("banner"), object.NewString("B"))
	h.Set(object.Symbol("enum"), object.NewArrayFromSlice([]object.Value{object.NewString("x"), object.NewString("y")}))
	h.Set(object.Symbol("group"), object.NewString("G"))
	h.Set(object.Symbol("hide"), object.Bool(true))
	h.Set(object.Symbol("repeatable"), object.Bool(true))
	o, err := vm.thorBuildOption("tags", h)
	if err != nil {
		t.Fatalf("thorBuildOption err=%v", err)
	}
	if o.Desc != "d" || o.Typ != thor.Array || !o.Required || o.Banner != "B" || o.Group != "G" || !o.Hide || !o.Repeatable {
		t.Errorf("thorBuildOption fields o=%+v", o)
	}
	// The nil-hash arm applies only Thor's defaults.
	o2, err := vm.thorBuildOption("name", nil)
	if err != nil || !o2.StringType() {
		t.Errorf("thorBuildOption nil-hash o=%+v err=%v", o2, err)
	}
}
