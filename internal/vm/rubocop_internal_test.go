// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	rubocop "github.com/go-ruby-rubocop/rubocop"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestRuboCopSeverityName covers every arm of the severity->symbol mapper,
// including the levels no core cop in the smoke fixtures emits.
func TestRuboCopSeverityName(t *testing.T) {
	cases := []struct {
		in   rubocop.Severity
		want string
	}{
		{rubocop.Convention, "convention"},
		{rubocop.Warning, "warning"},
		{rubocop.Error, "error"},
		{rubocop.Fatal, "fatal"},
		{rubocop.Info, "info"},
		{rubocop.Refactor, "refactor"},
		{rubocop.Severity(99), "convention"}, // default arm
	}
	for _, c := range cases {
		if got := rubocopSeverityName(c.in); got != c.want {
			t.Errorf("severity %v -> %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRuboCopStr covers the String / Symbol / default (to_s) arms of rubocopStr.
func TestRuboCopStr(t *testing.T) {
	if s := rubocopStr(object.Wrap(object.NewString("x"))); s != "x" {
		t.Errorf("string -> %q", s)
	}
	if s := rubocopStr(object.SymVal(string(object.Symbol("sym")))); s != "sym" {
		t.Errorf("symbol -> %q", s)
	}
	if s := rubocopStr(object.IntValue(int64(object.Integer(7)))); s != "7" {
		t.Errorf("default to_s -> %q", s)
	}
}

// TestRuboCopWrapperInspect covers the ToS / Inspect / Truthy of every RuboCop
// value wrapper.
func TestRuboCopWrapperInspect(t *testing.T) {
	r := &RuboCopRunner{r: rubocop.NewRunner(rubocop.DefaultRegistry(), rubocop.NewConfig())}
	if r.ToS() != "#<RuboCop::Runner>" || r.Inspect() != "#<RuboCop::Runner>" || !r.Truthy() {
		t.Error("runner wrapper surface")
	}
	c := &RuboCopConfig{c: rubocop.NewConfig()}
	if c.ToS() != "#<RuboCop::Config>" || c.Inspect() != "#<RuboCop::Config>" || !c.Truthy() {
		t.Error("config wrapper surface")
	}
	offs := rubocop.NewRunner(rubocop.DefaultRegistry(), rubocop.NewConfig()).Inspect("t.rb", "x = 1 \n")
	if len(offs) == 0 {
		t.Fatal("expected offenses in fixture")
	}
	o := &RuboCopOffense{o: offs[0]}
	if o.ToS() == "" || o.Inspect() == "" || !o.Truthy() {
		t.Error("offense wrapper surface")
	}
	l := &RuboCopLocation{l: offs[0].Location}
	if l.ToS() != "#<RuboCop::Cop::Offense::Location>" || l.Inspect() != l.ToS() || !l.Truthy() {
		t.Error("location wrapper surface")
	}
}
