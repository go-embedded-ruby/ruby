// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestActiveLdapConstants covers the loadable module, the alias require name, and
// the exception tree.
func TestActiveLdapConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "active_ldap"`, "true\n"},
		{`require "active_ldap"; p require "active_ldap"`, "false\n"},
		{`p require "activeldap"`, "true\n"},
		{`require "active_ldap"; p ActiveLdap.is_a?(Module)`, "true\n"},
		{`require "active_ldap"; p ActiveLdap::Error < StandardError`, "true\n"},
		{`require "active_ldap"; p ActiveLdap::EntryNotFound < ActiveLdap::Error`, "true\n"},
		{`require "active_ldap"; p ActiveLdap::RecordInvalid < ActiveLdap::Error`, "true\n"},
		{`require "active_ldap"; p ActiveLdap::ConnectionError < ActiveLdap::Error`, "true\n"},
		{`require "active_ldap"; p ActiveLdap.mock_directory.class`, "ActiveLdap::Directory\n"},
		{`require "active_ldap"; p ActiveLdap.escape_filter("a*b(c)")`, "\"a\\\\2ab\\\\28c\\\\29\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveLdapCoverage exercises the remaining branches: the wrapper
// to_s/inspect/truthy, the argument guards, every alFindOptions key, an absent
// attribute (nil), symbol-keyed assignment, and Model.new returning nil.
func TestActiveLdapCoverage(t *testing.T) {
	cases := []struct{ src, want string }{
		// to_s / inspect / truthy of each wrapper.
		{alSetup + `
alice = Person.create("uid" => "al", "cn" => ["A"], "sn" => ["A"])
p Person.inspect.start_with?("#<ActiveLdap::Model")
p alice.inspect.start_with?("#<ActiveLdap::Record")
p "#{alice}".start_with?("#<ActiveLdap::Record")
p ActiveLdap.mock_directory.inspect
p conn.inspect
p(alice ? :t : :f)
p(dir ? :t : :f)
p(conn ? :t : :f)
p(Person ? :t : :f)`,
			"true\ntrue\ntrue\n\"#<ActiveLdap::Directory>\"\n\"#<ActiveLdap::Connection base=dc=example,dc=com>\"\n:t\n:t\n:t\n:t\n"},
		// absent attribute -> nil; Model.new -> nil.
		{alSetup + `
Person.create("uid" => "al", "cn" => ["A"], "sn" => ["A"])
p Person.find("al")["nonexistent"]
p Person.new`, "nil\nnil\n"},
		// symbol-keyed build assignment (rubyToStr on a Symbol) + nil []= clears.
		{alSetup + `
r = Person.build(uid: "sym", cn: ["S"])
p r.id
r["cn"] = nil
p r["cn"]`, "\"sym\"\nnil\n"},
		// alFindOptions base:/scope:/limit:/attributes: all read.
		{alSetup + `
Person.create("uid" => "a", "cn" => ["A"], "sn" => ["A"])
Person.create("uid" => "b", "cn" => ["B"], "sn" => ["B"])
p Person.search(base: "ou=Users,dc=example,dc=com", scope: "one", limit: 1, attributes: ["uid"]).length`, "1\n"},
		// search with a non-Hash argument falls back to defaults.
		{alSetup + `
Person.create("uid" => "a", "cn" => ["A"], "sn" => ["A"])
p Person.search("ignored").length`, "1\n"},
		// connection with no args.
		{`require "active_ldap"
begin; ActiveLdap.connection; rescue ArgumentError; p :need_dir; end`, ":need_dir\n"},
		// model with only a name.
		{`require "active_ldap"
begin; ActiveLdap.model("X"); rescue ArgumentError; p :need_conn; end`, ":need_conn\n"},
		// reload re-reads and discards unsaved edits.
		{alSetup + `
Person.create("uid" => "al", "cn" => ["A"], "sn" => ["A"])
r = Person.find("al")
r["cn"] = ["Changed"]
r.reload
p r["cn"]`, "[\"A\"]\n"},
		// update returning false on an invalidating change (drops a required class).
		{alSetup + `
Person.create("uid" => "al", "cn" => ["A"], "sn" => ["A"])
r = Person.find("al")
p r.update("objectClass" => ["top"])`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestActiveLdapSeamEdges covers the seam's non-Array search result (a Net::LDAP
// miss) and an entry whose attribute_names is not an Array.
func TestActiveLdapSeamEdges(t *testing.T) {
	cases := []struct{ src, want string }{
		// search returns nil -> no entries.
		{`require "active_ldap"
class NilSearch
  def search(**) = nil
end
m = ActiveLdap.model("M", ActiveLdap.connection(ActiveLdap.directory(NilSearch.new)), dn_attribute: "cn", classes: ["top"])
p m.search.length`, "0\n"},
		// an entry whose attribute_names is not an Array yields just the dn.
		{`require "active_ldap"
class OddEntry
  def dn = "cn=x,dc=com"
  def attribute_names = nil
  def [](k) = []
end
class OddSearch
  def search(**) = [OddEntry.new]
end
m = ActiveLdap.model("M", ActiveLdap.connection(ActiveLdap.directory(OddSearch.new)), dn_attribute: "cn", classes: ["top"])
r = m.search.first
p r.dn`, "\"cn=x,dc=com\"\n"},
		// a seam search carrying attributes: exercises searchOptions' attributes branch.
		{`require "active_ldap"
class AttrSearch
  def search(base:, scope:, filter:, attributes: nil)
    @seen = attributes
    []
  end
end
s = AttrSearch.new
m = ActiveLdap.model("M", ActiveLdap.connection(ActiveLdap.directory(s)), dn_attribute: "cn", classes: ["top"])
p m.search(attributes: ["cn", "sn"]).length`, "0\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// alSetup is the shared prelude building a Person model over a mock directory.
const alSetup = `require "active_ldap"
dir  = ActiveLdap.mock_directory
conn = ActiveLdap.connection(dir, base: "dc=example,dc=com")
Person = ActiveLdap.model("Person", conn,
  dn_attribute: "uid", prefix: "ou=Users",
  classes: ["top", "person", "inetOrgPerson"], scope: "sub",
  aliases: {"commonName" => "cn"}, single_valued: ["uid", "cn"])
`

// TestActiveLdapCRUD covers create/find/save(diff)/update/destroy and the record
// accessors over the in-memory directory.
func TestActiveLdapCRUD(t *testing.T) {
	src := alSetup + `
alice = Person.create("uid" => "alice", "cn" => ["Alice"], "sn" => ["Adams"])
p alice.persisted?
p alice.id
p alice.dn
p Person.base_dn
p Person.name
p conn.base
# read back
found = Person.find("alice")
p found["cn"]
p found.one("commonName")   # alias + single-value accessor
# diff-based modify
found["mail"] = "alice@example.com"
p found.changed?
p found.save
p Person.find("alice")["mail"]
# update + attributes
found.update("title" => "Engineer")
p Person.find("alice")["title"]
p found.attributes["uid"]
# search + block
Person.create("uid" => "bob", "cn" => ["Bob"], "sn" => ["B"])
names = []
Person.search(filter: "(objectClass=person)") { |r| names << r.id }
p names.sort
p Person.find_all.length
p Person.find_first(filter: "(uid=bob)").id
p Person.find_first(filter: "(uid=ghost)")
p Person.exist?("alice")
p Person.exist?("ghost")
# destroy
found.destroy
p Person.exist?("alice")
`
	want := strings.Join([]string{
		"true", `"alice"`, `"uid=alice,ou=Users,dc=example,dc=com"`,
		`"ou=Users,dc=example,dc=com"`, `"Person"`, `"dc=example,dc=com"`,
		`["Alice"]`, `"Alice"`,
		"true", "true", `["alice@example.com"]`,
		`["Engineer"]`, `["alice"]`,
		`["alice", "bob"]`, "2", `"bob"`, "nil",
		"true", "false",
		"false",
	}, "\n") + "\n"
	if got := eval(t, src); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// TestActiveLdapValidationAndLDIF covers valid?/errors, save returning false on
// an invalid record, save!/RecordInvalid, to_ldif, build/new_record?, and the
// EntryNotFound raise.
func TestActiveLdapValidationAndLDIF(t *testing.T) {
	src := alSetup + `
bad = Person.build("cn" => ["NoUID"])   # missing dn_attribute uid
p bad.new_record?
p bad.valid?
p bad.errors.any? { |m| m.include?("uid") }
p bad.save                              # false, not raised
begin
  bad.save!
rescue ActiveLdap::RecordInvalid => e
  p :record_invalid
end
begin
  Person.find("nobody")
rescue ActiveLdap::EntryNotFound
  p :not_found
end
ok = Person.create("uid" => "carol", "cn" => ["Carol"], "sn" => ["C"])
puts ok.to_ldif
`
	got := eval(t, src)
	// Structured prefix assertions, then the LDIF body.
	wantPrefix := strings.Join([]string{"true", "false", "true", "false", ":record_invalid", ":not_found"}, "\n") + "\n"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("prefix mismatch:\n%q\nwant prefix:\n%q", got, wantPrefix)
	}
	for _, frag := range []string{
		"dn: uid=carol,ou=Users,dc=example,dc=com",
		"objectClass: top",
		"cn: Carol",
		"sn: C",
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("LDIF missing %q in:\n%s", frag, got)
		}
	}
}

// TestActiveLdapNetLdapSeam covers the rubyDirectory seam by driving the ORM
// against a duck-typed object answering the Net::LDAP surface
// (#search/#add/#modify/#delete + Net::LDAP::Entry-shaped results), proving the
// integration point go-ruby-ldap's Net::LDAP plugs into.
func TestActiveLdapNetLdapSeam(t *testing.T) {
	src := `require "active_ldap"
# A minimal in-memory Net::LDAP stand-in: an entries hash keyed by DN.
class FakeEntry
  def initialize(dn, attrs) = (@dn, @attrs = dn, attrs)
  def dn = @dn
  def attribute_names = @attrs.keys
  def [](k) = @attrs[k] || []
end
class FakeLDAP
  def initialize = (@store = {})
  def add(dn:, attributes:)
    return false if @store.key?(dn)
    @store[dn] = attributes.dup
    true
  end
  def modify(dn:, operations:)
    e = @store[dn] or return false
    operations.each do |op, attr, vals|
      case op
      when :replace then vals.empty? ? e.delete(attr) : (e[attr] = vals)
      when :add     then (e[attr] ||= []).concat(vals)
      when :delete  then e.delete(attr)
      end
    end
    true
  end
  def delete(dn:)
    @store.delete(dn) ? true : false
  end
  def search(base:, scope:, filter:, attributes: nil)
    @store.map { |dn, attrs| FakeEntry.new(dn, attrs) }
  end
end

dir  = ActiveLdap.directory(FakeLDAP.new)
conn = ActiveLdap.connection(dir, base: "dc=example,dc=com")
User = ActiveLdap.model("User", conn,
  dn_attribute: "uid", prefix: "ou=People",
  classes: ["top", "person"], scope: "sub")

u = User.create("uid" => "dave", "cn" => ["Dave"])
p u.persisted?
p User.search.map(&:id)
got = User.search.first
got["cn"] = ["David"]
p got.save
p User.search.first["cn"]
got.destroy
p User.search.map(&:id)
`
	want := strings.Join([]string{
		"true", `["dave"]`, "true", `["David"]`, "[]",
	}, "\n") + "\n"
	if got := eval(t, src); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// TestActiveLdapSeamFailures covers the seam's falsy-return → ConnectionError
// mapping (add/modify/delete) and the argument/type guards on the factories.
func TestActiveLdapSeamFailures(t *testing.T) {
	cases := []struct{ src, want string }{
		// add returns false -> ConnectionError on create's save (non-validation error is raised).
		{`require "active_ldap"
class NoAdd
  def add(**) = false
  def search(**) = []
end
dir = ActiveLdap.directory(NoAdd.new)
conn = ActiveLdap.connection(dir)
M = ActiveLdap.model("M", conn, dn_attribute: "cn", classes: ["top"])
begin; M.create("cn" => ["x"]); rescue ActiveLdap::ConnectionError; p :add_failed; end`, ":add_failed\n"},
		// directory with no arg.
		{`require "active_ldap"
begin; ActiveLdap.directory; rescue ArgumentError; p :need_conn; end`, ":need_conn\n"},
		// connection with a non-directory.
		{`require "active_ldap"
begin; ActiveLdap.connection("nope"); rescue TypeError; p :bad_dir; end`, ":bad_dir\n"},
		// model without dn_attribute.
		{`require "active_ldap"
conn = ActiveLdap.connection(ActiveLdap.mock_directory)
begin; ActiveLdap.model("X", conn, classes: ["top"]); rescue ActiveLdap::Error; p :no_dn; end`, ":no_dn\n"},
		// model with a non-connection.
		{`require "active_ldap"
begin; ActiveLdap.model("X", "nope", dn_attribute: "cn"); rescue TypeError; p :bad_conn; end`, ":bad_conn\n"},
		// modify returns false -> ConnectionError on save of a changed record.
		{`require "active_ldap"
class FailModify
  def add(**) = true
  def modify(**) = false
  def search(**) = []
end
m = ActiveLdap.model("M", ActiveLdap.connection(ActiveLdap.directory(FailModify.new)), dn_attribute: "cn", classes: ["top"])
u = m.create("cn" => ["x"])
u["sn"] = ["s"]
begin; u.save; rescue ActiveLdap::ConnectionError; p :save_failed; end`, ":save_failed\n"},
		// modify returns false -> ConnectionError on update.
		{`require "active_ldap"
class FailModify2
  def add(**) = true
  def modify(**) = false
  def search(**) = []
end
m = ActiveLdap.model("M", ActiveLdap.connection(ActiveLdap.directory(FailModify2.new)), dn_attribute: "cn", classes: ["top"])
u = m.create("cn" => ["x"])
begin; u.update("sn" => ["s"]); rescue ActiveLdap::ConnectionError; p :update_failed; end`, ":update_failed\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
