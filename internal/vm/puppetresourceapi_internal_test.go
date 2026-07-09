// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// praRun runs a Ruby program with `require "puppet/resource_api"` prepended.
func praRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"puppet/resource_api\"\n"+body)
}

// widgetType is a register_type call declaring a namevar, a defaulted ensure and
// an integer property, with a feature.
const widgetType = `
Puppet::ResourceApi.register_type(
  name: "widget",
  desc: "a widget",
  attributes: {
    name:   { type: "String", desc: "the name", behaviour: :namevar },
    ensure: { type: "Enum['present','absent']", desc: "state", default: "present" },
    size:   { type: "Integer", desc: "the size" },
  },
  features: ["canonicalize"])
`

// TestPuppetResourceApiRegister covers register_type and the TypeDefinition
// introspection surface, plus type lookup and definitions. It also registers a
// type exercising every attribute behaviour and a string-keyed definition (the
// praGet string fallback and a non-Array features value).
func TestPuppetResourceApiRegister(t *testing.T) {
	got := praRun(t, widgetType+`
td = Puppet::ResourceApi.type("widget")
puts td.class
puts td.name
puts td.attributes.sort.inspect
puts td.namevars.inspect
puts td.feature?("canonicalize")
puts td.has_feature?("nope")
puts Puppet::ResourceApi.definitions.include?("widget")
puts Puppet::ResourceApi.type("nope").inspect

Puppet::ResourceApi.register_type(
  name: "allbeh",
  attributes: {
    name:  { type: "String", behaviour: :namevar },
    prop:  { type: "String" },
    param: { type: "String", behaviour: :parameter },
    ro:    { type: "String", behaviour: :read_only },
    io:    { type: "String", behaviour: :init_only },
  })
puts Puppet::ResourceApi.type("allbeh").attributes.sort.inspect

Puppet::ResourceApi.register_type(
  "name" => "strkeyed",
  "attributes" => { "id" => { "type" => "String", "behaviour" => :namevar } },
  "features" => "notarray")
puts Puppet::ResourceApi.type("strkeyed").namevars.inspect
`)
	want := "Puppet::ResourceApi::TypeDefinition\nwidget\n" +
		`["ensure", "name", "size"]` + "\n" + `["name"]` + "\ntrue\nfalse\ntrue\nnil\n" +
		`["io", "name", "param", "prop", "ro"]` + "\n" + `["id"]`
	if got != want {
		t.Fatalf("register:\n got=%q\nwant=%q", got, want)
	}
}

// TestPuppetResourceApiValidate covers validate (defaults applied, completed
// resource returned) and title.
func TestPuppetResourceApiValidate(t *testing.T) {
	got := praRun(t, widgetType+`
td = Puppet::ResourceApi.type("widget")
v = td.validate({name: "w1", size: 5})
puts v.class
puts v["ensure"]
puts v["size"]
puts td.title({name: "w1", size: 5})
`)
	want := "Hash\npresent\n5\nw1"
	if got != want {
		t.Fatalf("validate:\n got=%q\nwant=%q", got, want)
	}
}

// TestPuppetResourceApiApply covers the provider protocol: apply drives the get
// and set Ruby blocks inline, computes a create change, and returns a summary.
// It also checks the change set handed to set (is nil for an absent resource,
// should the desired Hash).
func TestPuppetResourceApiApply(t *testing.T) {
	got := praRun(t, `
td = Puppet::ResourceApi.register_type(
  name: "gadget",
  attributes: {
    name:   { type: "String", behaviour: :namevar },
    ensure: { type: "Enum['present','absent']", default: "present" },
  })
seen = nil
summary = td.apply([{name: "g1", ensure: "present"}],
  get: -> { [] },
  set: ->(changes) { seen = changes })
puts summary[:created].inspect
puts summary[:updated].inspect
puts seen.keys.inspect
puts seen["g1"][:is].inspect
puts seen["g1"][:should].class
`)
	want := `["g1"]` + "\n[]\n" + `["g1"]` + "\nnil\nHash"
	if got != want {
		t.Fatalf("apply:\n got=%q\nwant=%q", got, want)
	}
}

// TestPuppetResourceApiDevErrors covers the Puppet::DevError schema-rejection
// paths: a definition with no name / no namevar attribute, a non-Hash
// attributes value, and a non-Hash attribute spec.
func TestPuppetResourceApiDevErrors(t *testing.T) {
	cases := []string{
		`Puppet::ResourceApi.register_type(desc: "no name")`,
		`Puppet::ResourceApi.register_type(name: "d1", attributes: "notahash")`,
		`Puppet::ResourceApi.register_type(name: "d2", attributes: { name: "notahash" })`,
	}
	for _, expr := range cases {
		got := praRun(t, "begin; "+expr+"; rescue => e; puts e.class; end")
		if got != "Puppet::DevError" {
			t.Fatalf("%s expected Puppet::DevError, got %q", expr, got)
		}
	}
}

// TestPuppetResourceApiResourceErrors covers the Puppet::ResourceError paths: an
// unknown attribute and a type mismatch on validate, and a desired resource that
// fails validation during apply.
func TestPuppetResourceApiResourceErrors(t *testing.T) {
	cases := []string{
		`Puppet::ResourceApi.register_type(name: "r1", attributes: {name: {type: "String", behaviour: :namevar}}).validate({name: "x", bogus: 1})`,
		`Puppet::ResourceApi.register_type(name: "r2", attributes: {name: {type: "Integer", behaviour: :namevar}}).validate({name: "notint"})`,
		`Puppet::ResourceApi.register_type(name: "r3", attributes: {name: {type: "String", behaviour: :namevar}}).apply([42], get: -> { [] }, set: ->(c) {})`,
		`Puppet::ResourceApi.register_type(name: "r4", attributes: {name: {type: "String", behaviour: :namevar}}).title({})`,
	}
	for _, expr := range cases {
		got := praRun(t, "begin; "+expr+"; rescue => e; puts e.class; end")
		if got != "Puppet::ResourceError" {
			t.Fatalf("%s expected Puppet::ResourceError, got %q", expr, got)
		}
	}
}

// TestPuppetResourceApiArgErrors covers the ArgumentError paths: register_type
// with a missing / non-Hash argument, type / validate / title / feature? / apply
// with missing arguments, apply with a non-Array desired, and apply with a
// missing / non-Proc provider block.
func TestPuppetResourceApiArgErrors(t *testing.T) {
	setup := `td = Puppet::ResourceApi.register_type(name: "a1", attributes: {name: {type: "String", behaviour: :namevar}})` + "\n"
	cases := []struct{ pre, expr string }{
		{"", "Puppet::ResourceApi.register_type"},
		{"", "Puppet::ResourceApi.register_type(42)"},
		{"", "Puppet::ResourceApi.type"},
		{setup, "td.validate"},
		{setup, "td.title"},
		{setup, "td.feature?"},
		{setup, "td.apply"},
		{setup, `td.apply("notarray", get: -> {[]}, set: ->(c){})`},
		{setup, `td.apply([{name: "x"}])`},
		{setup, `td.apply([{name: "x"}], set: ->(c){})`},
		{setup, `td.apply([{name: "x"}], get: 42, set: ->(c){})`},
	}
	for _, c := range cases {
		got := praRun(t, c.pre+"begin; "+c.expr+"; rescue => e; puts e.class; end")
		if got != "ArgumentError" {
			t.Fatalf("%s expected ArgumentError, got %q", c.expr, got)
		}
	}
}

// TestPuppetResourceApiStringers covers the object.Value marker methods on the
// wrapper.
func TestPuppetResourceApiStringers(t *testing.T) {
	got := praRun(t, widgetType+`
td = Puppet::ResourceApi.type("widget")
puts "yes" if td
puts td.to_s
puts td.inspect
`)
	want := "yes\n#<Puppet::ResourceApi::TypeDefinition widget>\n#<Puppet::ResourceApi::TypeDefinition widget>"
	if got != want {
		t.Fatalf("stringers:\n got=%q\nwant=%q", got, want)
	}
}
