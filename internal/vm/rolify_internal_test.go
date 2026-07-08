// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"
)

// rolifyPreamble defines a role holder (User, via rolify) and a resource
// (Forum, via resourcify), each with an id, shared by the TestRolify cases.
const rolifyPreamble = `require "rolify"
class User
  rolify
  attr_reader :id
  def initialize(id); @id = id; end
end
class Forum
  resourcify
  attr_reader :id
  def initialize(id); @id = id; end
end
`

// TestRolify covers the require "rolify" surface backed by
// github.com/go-ruby-rolify/rolify: the rolify / resourcify class macros, the
// global/class/instance scope semantics with inheritance and the :any wildcard,
// add_role / has_role? / has_cached_role? / remove_role / roles, the
// has_all/any predicates (both bare-name and Hash forms, plus the gem aliases),
// the resource-side helpers and the Rolify::Role view object.
func TestRolify(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"global add/has/remove", `
u = User.new(1)
p u.add_role(:admin).name
p u.has_role?(:admin)
p u.has_role?(:missing)
p u.remove_role(:admin)
p u.has_role?(:admin)
p u.remove_role(:admin)`,
			"\"admin\"\ntrue\nfalse\ntrue\nfalse\nfalse\n"},

		{"class + instance scope inheritance", `
u = User.new(1); f = Forum.new(7)
u.add_role(:moderator, Forum)
p u.has_role?(:moderator, Forum)
p u.has_role?(:moderator, f)
u.add_role(:owner, f)
p u.has_role?(:owner, f)
p u.has_role?(:owner, :any)
p u.has_role?(:owner, Forum)`,
			"true\ntrue\ntrue\ntrue\nfalse\n"},

		{"roles listing + Rolify::Role view", `
u = User.new(2); f = Forum.new(9)
u.add_role(:admin)
u.add_role(:mod, Forum)
u.add_role(:owner, f)
rs = u.roles
p rs.map(&:name).sort
admin = rs.find { |r| r.name == "admin" }
p admin.resource_type
p admin.resource_id
p admin.id.is_a?(Integer)
mod = rs.find { |r| r.name == "mod" }
p mod.resource_type
p mod.resource_id
own = rs.find { |r| r.name == "owner" }
p own.resource_id
puts admin.to_s
p admin
p(admin ? "truthy" : "falsey")`,
			"[\"admin\", \"mod\", \"owner\"]\nnil\nnil\ntrue\n\"Forum\"\nnil\n\"9\"\n#<Rolify::Role name=admin>\n#<Rolify::Role name=admin>\n\"truthy\"\n"},

		{"empty roles", `p User.new(50).roles`, "[]\n"},

		{"has_all/any predicates", `
u = User.new(3)
u.add_role(:admin)
u.add_role(:mod, Forum)
p u.has_all_of_roles?({name: :admin}, {name: :mod, resource: Forum})
p u.has_all_of_roles?({name: :admin}, {name: :nope})
p u.has_any_of_roles?(:nope, :admin)
p u.has_any_of_roles?(:nope)
p u.has_all_roles?(:admin)
p u.has_any_role?(:admin)
p u.has_all_of_roles?`,
			"true\nfalse\ntrue\nfalse\ntrue\ntrue\nfalse\n"},

		{"has_cached_role? + invalidation", `
u = User.new(4)
u.add_role(:admin)
p u.has_cached_role?(:admin)
u.add_role(:mod, Forum)
p u.has_cached_role?(:mod, Forum)
p u.has_cached_role?(:nope)`,
			"true\ntrue\nfalse\n"},

		{"resource-side helpers", `
u = User.new(5); f = Forum.new(11)
u.add_role(:admin)
u.add_role(:mod, Forum)
u.add_role(:owner, f)
p f.roles.map(&:name).sort
p f.applied_roles.map(&:name).sort
p f.roles_to_administrate.map(&:name).sort`,
			"[\"owner\"]\n[\"admin\", \"mod\", \"owner\"]\n[\"mod\", \"owner\"]\n"},

		{"require returns true then false", `p require("rolify"); p require("rolify")`, "true\nfalse\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The standalone require probe must not be preceded by the preamble's
			// own require "rolify" (which would make the first probe report false).
			src := rolifyPreamble + tc.body
			if strings.HasPrefix(strings.TrimSpace(tc.body), "p require") {
				src = tc.body
			}
			if got := eval(t, src); got != tc.want {
				t.Errorf("%s:\n got: %q\nwant: %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestRolifyErrors covers the ArgumentError raised when a role name is missing.
func TestRolifyErrors(t *testing.T) {
	class, _ := evalErr(t, rolifyPreamble+`User.new(1).add_role`)
	if class != "ArgumentError" {
		t.Fatalf("add_role with no name: got %q, want ArgumentError", class)
	}
}
