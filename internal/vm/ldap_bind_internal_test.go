// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !wasm

package vm

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	server "github.com/glauth/ldap"
)

// ldapDir is a tiny in-memory directory backing the in-process LDAP server the
// Net::LDAP binding is driven against. It is not a full DSA: it applies the bind
// check, a real filter-engine search (with a sentinel base that forces a
// non-network operations error), and a real delete, and acknowledges the
// remaining operations so their round trips are validated end to end — exactly
// mirroring how the etcd binding is exercised against an embedded etcd.
type ldapDir struct {
	mu      sync.Mutex
	entries map[string]map[string][]string
}

const (
	ldapAdminDN = "cn=admin,dc=example,dc=com"
	ldapAdminPW = "secret"
)

func (d *ldapDir) Bind(bindDN, pw string, conn net.Conn) (server.LDAPResultCode, error) {
	if bindDN == "" || (bindDN == ldapAdminDN && pw == ldapAdminPW) {
		return server.LDAPResultSuccess, nil
	}
	return server.LDAPResultInvalidCredentials, nil
}

func (d *ldapDir) Search(boundDN string, req server.SearchRequest, conn net.Conn) (server.ServerSearchResult, error) {
	// A sentinel base forces a non-network result-code error so the binding's
	// "search failed but the connection is fine" path is exercised.
	if strings.Contains(strings.ToLower(req.BaseDN), "boom") {
		return server.ServerSearchResult{ResultCode: server.LDAPResultOperationsError}, fmt.Errorf("forced operations error")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	packet, err := server.CompileFilter(req.Filter)
	if err != nil {
		return server.ServerSearchResult{ResultCode: server.LDAPResultOperationsError}, err
	}
	base := strings.ToLower(req.BaseDN)
	var out []*server.Entry
	for dn, attrs := range d.entries {
		if !ldapInScope(strings.ToLower(dn), base, req.Scope) {
			continue
		}
		e := &server.Entry{DN: dn}
		for name, vals := range attrs {
			e.Attributes = append(e.Attributes, &server.EntryAttribute{Name: name, Values: vals})
		}
		if ok, _ := server.ServerApplyFilter(packet, e); ok {
			out = append(out, e)
		}
	}
	return server.ServerSearchResult{Entries: out, ResultCode: server.LDAPResultSuccess}, nil
}

func (d *ldapDir) Delete(boundDN, deleteDN string, conn net.Conn) (server.LDAPResultCode, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.entries[deleteDN]; !ok {
		return server.LDAPResultNoSuchObject, nil
	}
	delete(d.entries, deleteDN)
	return server.LDAPResultSuccess, nil
}

func (d *ldapDir) Add(boundDN string, req server.AddRequest, conn net.Conn) (server.LDAPResultCode, error) {
	return server.LDAPResultSuccess, nil
}
func (d *ldapDir) Modify(boundDN string, req server.ModifyRequest, conn net.Conn) (server.LDAPResultCode, error) {
	return server.LDAPResultSuccess, nil
}
func (d *ldapDir) ModifyDN(boundDN string, req server.ModifyDNRequest, conn net.Conn) (server.LDAPResultCode, error) {
	return server.LDAPResultSuccess, nil
}

// trueCmp answers every compare with CompareTrue, so the binding's #compare
// (which returns the boolean result unchanged) is exercised end to end.
type trueCmp struct{}

func (trueCmp) Compare(boundDN string, req server.CompareRequest, conn net.Conn) (server.LDAPResultCode, error) {
	return server.LDAPResultCompareTrue, nil
}

func ldapInScope(dn, base string, scope int) bool {
	switch scope {
	case server.ScopeBaseObject:
		return dn == base
	case server.ScopeSingleLevel:
		if !strings.HasSuffix(dn, ","+base) {
			return false
		}
		return !strings.Contains(strings.TrimSuffix(dn, ","+base), ",")
	default:
		return dn == base || strings.HasSuffix(dn, ","+base)
	}
}

// startLDAPServer boots the in-process LDAP server on an ephemeral loopback port
// and returns its host:port, torn down in a t.Cleanup.
func startLDAPServer(t *testing.T) string {
	t.Helper()
	d := &ldapDir{entries: map[string]map[string][]string{
		"dc=example,dc=com":                    {"objectclass": {"domain"}, "dc": {"example"}},
		"ou=people,dc=example,dc=com":          {"objectclass": {"organizationalUnit"}, "ou": {"people"}},
		"cn=alice,ou=people,dc=example,dc=com": {"objectclass": {"person"}, "cn": {"alice"}, "mail": {"alice@example.com"}, "sn": {"Adams"}},
		"cn=bob,ou=people,dc=example,dc=com":   {"objectclass": {"person"}, "cn": {"bob"}, "mail": {"bob@example.com"}, "sn": {"Baker"}},
		"cn=carol,ou=people,dc=example,dc=com": {"objectclass": {"person"}, "cn": {"carol"}, "mail": {"carol@example.com"}, "sn": {"Cole"}},
	}}
	s := server.NewServer()
	s.BindFunc("", d)
	s.SearchFunc("", d)
	s.AddFunc("", d)
	s.ModifyFunc("", d)
	s.DeleteFunc("", d)
	s.ModifyDNFunc("", d)
	s.CompareFunc("", trueCmp{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = s.Serve(ln) }()
	t.Cleanup(func() { _ = ln.Close() })
	time.Sleep(50 * time.Millisecond)
	return ln.Addr().String()
}

// startDeadLDAPServer accepts connections and immediately closes them, so any
// LDAP operation fails with a network-class error — exercising the binding's
// re-raise-on-broken-connection paths deterministically.
func startDeadLDAPServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

// ldapRun runs a Ruby program with HOST and PORT constants bound to the server's
// address, returning its trimmed stdout. Every Net::LDAP test drives the whole
// binding through rbgo, exactly as a user would.
func ldapRun(t *testing.T, addr, body string) string {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	src := fmt.Sprintf("require \"net/ldap\"\nHOST = %q\nPORT = %s\n", host, port) + body
	return runSrc(t, src)
}

// TestNetLDAP drives the entire Net::LDAP binding end to end against one shared
// in-process server.
func TestNetLDAP(t *testing.T) {
	addr := startLDAPServer(t)

	t.Run("ConnectBindAndReaders", func(t *testing.T) {
		got := ldapRun(t, addr, `
r = []
ldap = Net::LDAP.new(host: HOST, port: PORT, base: "dc=example,dc=com",
                     auth: {method: :simple, username: "cn=admin,dc=example,dc=com", password: "secret"})
r << ldap.host
r << ldap.port
r << ldap.base
r << ldap.bind                     # true (right creds)
res = ldap.get_operation_result
r << res.code                      # 0
r << res.name                      # Success
r << res.success?                  # true
r << res.message.class.name        # String
r << res.error_message.class.name  # String
r << res.matched_dn.class.name     # String
# wrong password -> false, non-zero code
bad = Net::LDAP.new(host: HOST, port: PORT, auth: {method: :simple, username: "cn=admin,dc=example,dc=com", password: "nope"})
r << bad.bind                      # false
r << (bad.get_operation_result.code != 0)   # true
# anonymous (no auth) connection: default method, bind against empty DN succeeds
anon = Net::LDAP.new(host: HOST, port: PORT)
r << anon.bind                     # true
# per-call auth override on #bind
r << ldap.bind(username: "cn=admin,dc=example,dc=com", password: "secret")  # true
ldap.close; bad.close; anon.close
puts r.join("\n")
`)
		want := "127.0.0.1\n" + portOf(addr) + "\ndc=example,dc=com\ntrue\n0\nSuccess\ntrue\nString\nString\nString\nfalse\ntrue\ntrue\ntrue"
		if got != want {
			t.Fatalf("ConnectBindAndReaders:\ngot:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("SearchFilterEntry", func(t *testing.T) {
		got := ldapRun(t, addr, `
ldap = Net::LDAP.new(host: HOST, port: PORT, base: "dc=example,dc=com")
r = []
# String filter + block form; returns the entries array
entries = ldap.search(filter: "(&(objectclass=person)(cn=al*))") { |e| r << ("cb:" + e.dn) }
r << entries.size                          # 1
e = entries.first
r << e.dn                                  # cn=alice,...
r << e[:cn].first                          # alice (Symbol key, case-insensitive)
r << e["MAIL"].first                       # alice@example.com (String key, case-insensitive)
r << e[:absent].inspect                    # [] (empty array)
r << e.first(:cn)                          # alice
r << e.first(:absent).inspect              # nil
r << e.attribute_names.include?("cn")      # true
names = []; e.each_attribute { |n, v| names << n }
r << names.include?("mail")                # true
r << e.to_h[:dn]                           # cn=alice,...
# Filter object built with the DSL, scope + attributes + size options
f = Net::LDAP::Filter.eq("objectclass", "person") & Net::LDAP::Filter.present("mail")
res = ldap.search(base: "ou=people,dc=example,dc=com", scope: Net::LDAP::SearchScope_SingleLevel,
                  filter: f, attributes: ["cn"], size: 5)
r << res.size                              # 3
# symbol scope + Filter.construct
res2 = ldap.search(scope: :sub, filter: Net::LDAP::Filter.construct("(sn=Baker)"))
r << res2.first[:cn].first                 # bob
# no options at all (nil options Hash path)
r << (ldap.search.is_a?(Array))            # true
# a search that the server fails with a non-network code -> nil
r << ldap.search(base: "cn=boom").inspect  # nil
ldap.close
puts r.join("\n")
`)
		lines := strings.Split(got, "\n")
		expect := map[int]string{0: "cb:cn=alice,ou=people,dc=example,dc=com", 1: "1", 2: "cn=alice,ou=people,dc=example,dc=com", 3: "alice", 4: "alice@example.com", 5: "[]", 6: "alice", 7: "nil", 8: "true", 9: "true"}
		for i, w := range expect {
			if lines[i] != w {
				t.Fatalf("SearchFilterEntry line %d = %q want %q\nfull:\n%s", i, lines[i], w, got)
			}
		}
		if !strings.Contains(got, "\n3\n") || !strings.Contains(got, "\nbob\n") || !strings.HasSuffix(got, "nil") {
			t.Fatalf("SearchFilterEntry tail:\n%s", got)
		}
	})

	t.Run("AddModifyDeleteRenameCompare", func(t *testing.T) {
		got := ldapRun(t, addr, `
ldap = Net::LDAP.new(host: HOST, port: PORT, base: "dc=example,dc=com")
r = []
r << ldap.add(dn: "cn=dave,ou=people,dc=example,dc=com", attributes: {cn: "dave", sn: ["Davis"], objectclass: "person"})  # true
r << ldap.add(dn: "cn=eve,ou=people,dc=example,dc=com")   # true (no attributes key)
r << ldap.modify(dn: "cn=alice,ou=people,dc=example,dc=com",
                 operations: [[:replace, :mail, "a@x"], [:add, "desc", ["d1","d2"]], [:delete, :sn, []]])  # true
r << ldap.modify(dn: "cn=alice,ou=people,dc=example,dc=com")   # true (no operations key)
r << ldap.rename(olddn: "cn=bob,ou=people,dc=example,dc=com", newrdn: "cn=bobby", delete_attributes: true)  # true
r << ldap.modify_rdn(olddn: "cn=bobby,ou=people,dc=example,dc=com", newrdn: "cn=bob", delete_attributes: false)  # true (alias, delOld=false)
r << ldap.replace_attribute("cn=alice,ou=people,dc=example,dc=com", :mail, "a2@x")  # true
r << ldap.add_attribute("cn=alice,ou=people,dc=example,dc=com", "telephoneNumber", ["1"])  # true
r << ldap.delete_attribute("cn=alice,ou=people,dc=example,dc=com", :telephoneNumber)  # true
r << ldap.delete(dn: "cn=carol,ou=people,dc=example,dc=com")  # true
r << ldap.delete(dn: "cn=carol,ou=people,dc=example,dc=com")  # false (already gone)
r << (ldap.get_operation_result.code == 32)  # true (NoSuchObject)
r << ldap.compare("cn=alice,ou=people,dc=example,dc=com", :cn, "alice")  # true
ldap.close
puts r.join("\n")
`)
		want := "true\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\nfalse\ntrue\ntrue"
		if got != want {
			t.Fatalf("AddModifyDeleteRenameCompare:\ngot:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("FiltersAndInspect", func(t *testing.T) {
		got := ldapRun(t, addr, `
r = []
F = Net::LDAP::Filter
r << F.eq("cn", "a").to_s                  # (cn=a)
r << F.present("mail").to_s                # (mail=*)
r << F.pres("mail").to_s                   # (mail=*)
r << F.ge("age", "18").to_s                # (age>=18)
r << F.le("age", "65").to_s                # (age<=65)
r << F.begins("cn", "al").to_s             # (cn=al*)
r << F.ends("cn", "ce").to_s               # (cn=*ce)
r << F.contains("cn", "li").to_s           # (cn=*li*)
r << (F.eq("a","1") & F.eq("b","2")).to_s  # (&(a=1)(b=2))
r << (F.eq("a","1") | F.eq("b","2")).to_s  # (|(a=1)(b=2))
r << F.eq("a","1").negate.to_s             # (!(a=1))
r << (~F.eq("a","1")).to_s                 # (!(a=1))
r << F.from_rfc2254("(cn=x)").to_s         # (cn=x)
# Inspect / to_s / truthy of every wrapper
ldap = Net::LDAP.new(host: HOST, port: PORT, base: "dc=example,dc=com")
e = ldap.search(filter: "(cn=alice)").first
res = ldap.get_operation_result
flt = F.eq("cn","a")
objs = [ldap, e, flt, res]
objs.each { |o| p o; print o.to_s, "\n"; raise "falsy" unless o }
r << "OK #{objs.size}"
ldap.close
puts r.join("\n")
`)
		wantHead := strings.Join([]string{
			"(cn=a)", "(mail=*)", "(mail=*)", "(age>=18)", "(age<=65)",
			"(cn=al*)", "(cn=*ce)", "(cn=*li*)", "(&(a=1)(b=2))", "(|(a=1)(b=2))",
			"(!(a=1))", "(!(a=1))", "(cn=x)",
		}, "\n")
		if !strings.Contains(got, wantHead) {
			t.Fatalf("FiltersAndInspect head:\n%s", got)
		}
		if !strings.Contains(got, "#<Net::LDAP>") || !strings.Contains(got, "#<Net::LDAP::Entry dn=cn=alice") ||
			!strings.Contains(got, "#<Net::LDAP::Filter (cn=a)>") || !strings.Contains(got, "OK 4") {
			t.Fatalf("FiltersAndInspect inspects:\n%s", got)
		}
	})

	t.Run("OpenBlock", func(t *testing.T) {
		got := ldapRun(t, addr, `
r = []
val = Net::LDAP.new(host: HOST, port: PORT, base: "dc=example,dc=com").open { |l| l.search(filter: "(cn=alice)").size }
r << val                     # 1 (block value returned; connection closed after)
l2 = Net::LDAP.new(host: HOST, port: PORT)
r << l2.open.equal?(l2)      # true (no block returns self)
l2.close
puts r.join("\n")
`)
		if got != "1\ntrue" {
			t.Fatalf("OpenBlock:\n%s", got)
		}
	})

	t.Run("CoverageCorners", func(t *testing.T) {
		// A fresh server: this subtest asserts entry counts, so it must not see the
		// mutations earlier subtests made to the shared directory.
		got := ldapRun(t, startLDAPServer(t), `
ldap = Net::LDAP.new(host: HOST, port: PORT, base: "dc=example,dc=com")
r = []
# scope symbol forms: :base and :one
r << ldap.search(base: "cn=alice,ou=people,dc=example,dc=com", scope: :base, filter: "(objectclass=*)").size  # 1
r << ldap.search(base: "ou=people,dc=example,dc=com", scope: :one, filter: "(objectclass=*)").size            # 3
# filter: nil means match everything
r << (ldap.search(base: "dc=example,dc=com", filter: nil).size >= 1)   # true
# a trailing non-Hash argument is not treated as options (ldapOptsHash)
r << ldap.search(123).is_a?(Array)      # true
# a String modify operation kind
r << ldap.modify(dn: "cn=alice,ou=people,dc=example,dc=com", operations: [["replace", "mail", "s@x"]])  # true
# rename without delete_attributes: (default true path)
r << ldap.rename(olddn: "cn=alice,ou=people,dc=example,dc=com", newrdn: "cn=alicia")  # true
# auth :method given as a String
a = Net::LDAP.new(host: HOST, port: PORT, auth: {method: "simple", username: "cn=admin,dc=example,dc=com", password: "secret"})
r << a.bind   # true
a.close
ldap.close
puts r.join("\n")
`)
		if got != "1\n3\ntrue\ntrue\ntrue\ntrue\ntrue" {
			t.Fatalf("CoverageCorners:\n%s", got)
		}
	})

	t.Run("ArgumentAndTypeErrors", func(t *testing.T) {
		got := ldapRun(t, addr, `
ldap = Net::LDAP.new(host: HOST, port: PORT, base: "dc=example,dc=com")
r = []
def try(r, label)
  yield
  r << "#{label}:noraise"
rescue => e
  r << "#{label}:#{e.class.name.split('::').last}"
end
try(r, "new_noopts")   { Net::LDAP.new }
try(r, "auth_type")    { Net::LDAP.new(host: HOST, port: PORT, auth: 5) }
try(r, "add_noopts")   { ldap.add }
try(r, "filter_type")  { ldap.search(filter: 123) }
try(r, "filter_syntax"){ ldap.search(filter: "(cn=al") }
try(r, "scope_bad")    { ldap.search(scope: :nope, filter: "(cn=a)") }
try(r, "attrs_type")   { ldap.add(dn: "x", attributes: 5) }
try(r, "attr_val_type"){ ldap.add(dn: "x", attributes: {cn: 5}) }
try(r, "ops_type")     { ldap.modify(dn: "x", operations: 5) }
try(r, "op_triple")    { ldap.modify(dn: "x", operations: [[:add]]) }
try(r, "op_kind_type") { ldap.modify(dn: "x", operations: [[5, :cn, "v"]]) }
try(r, "op_kind_unk")  { ldap.modify(dn: "x", operations: [[:frob, :cn, "v"]]) }
try(r, "entry_idx0")   { ldap.search(filter: "(cn=alice)").first[] }
try(r, "entry_first0") { ldap.search(filter: "(cn=alice)").first.first }
try(r, "each_attr_nb") { ldap.search(filter: "(cn=alice)").first.each_attribute }
try(r, "filter_eq_ar") { Net::LDAP::Filter.eq("cn") }
try(r, "filter_pres0") { Net::LDAP::Filter.present }
try(r, "filter_cons0") { Net::LDAP::Filter.construct }
try(r, "filter_and_t") { Net::LDAP::Filter.eq("a","1") & "notafilter" }
try(r, "repl_arity")   { ldap.replace_attribute("dn", "cn") }
try(r, "del_attr_ar")  { ldap.delete_attribute("dn") }
ldap.close
puts r.join("\n")
`)
		checks := map[string]string{
			"new_noopts": "ArgumentError", "auth_type": "TypeError", "add_noopts": "ArgumentError",
			"filter_type": "TypeError", "filter_syntax": "FilterSyntax", "scope_bad": "ArgumentError",
			"attrs_type": "TypeError", "attr_val_type": "TypeError", "ops_type": "TypeError",
			"op_triple": "ArgumentError", "op_kind_type": "ArgumentError", "op_kind_unk": "ArgumentError",
			"entry_idx0": "ArgumentError", "entry_first0": "ArgumentError", "each_attr_nb": "LocalJumpError",
			"filter_eq_ar": "ArgumentError", "filter_pres0": "ArgumentError", "filter_cons0": "ArgumentError", "filter_and_t": "TypeError",
			"repl_arity": "ArgumentError", "del_attr_ar": "ArgumentError",
		}
		for label, want := range checks {
			if !strings.Contains(got, label+":"+want) {
				t.Fatalf("ArgumentAndTypeErrors: missing %q\nfull:\n%s", label+":"+want, got)
			}
		}
	})

	t.Run("AuthMethodDefaultAndValueForms", func(t *testing.T) {
		// auth without :method defaults to simple; :method given as a non-symbol
		// falls back to "simple"; both connect and bind fine against the server.
		got := ldapRun(t, addr, `
r = []
a = Net::LDAP.new(host: HOST, port: PORT, auth: {username: "cn=admin,dc=example,dc=com", password: "secret"})
r << a.bind      # true (default simple)
a.close
b = Net::LDAP.new(host: HOST, port: PORT, auth: {method: 5, username: "cn=admin,dc=example,dc=com", password: "secret"})
r << b.bind      # true (method 5 -> "simple")
b.close
puts r.join("\n")
`)
		if got != "true\ntrue" {
			t.Fatalf("AuthMethodDefaultAndValueForms:\n%s", got)
		}
	})
}

// TestNetLDAPConnectFailure covers ldap.New's error path: a client pointed at an
// unreachable endpoint fails to connect, which the binding re-raises as a
// Net::LDAP error. It needs no server.
func TestNetLDAPConnectFailure(t *testing.T) {
	got := runSrc(t, `
require "net/ldap"
begin
  Net::LDAP.new(host: "127.0.0.1", port: 1)
  puts "noraise"
rescue Net::LDAP::Error => e
  puts "ldap_error"
end
`)
	if got != "ldap_error" {
		t.Fatalf("TestNetLDAPConnectFailure: got %q", got)
	}
}

// TestNetLDAPNetworkErrors covers the re-raise-on-broken-connection paths: a
// client connected to a server that immediately drops the socket fails each
// operation with a network-class error, which #bind and #search re-raise as a
// Net::LDAP exception (rather than reporting a false result).
func TestNetLDAPNetworkErrors(t *testing.T) {
	addr := startDeadLDAPServer(t)
	got := ldapRun(t, addr, `
r = []
ldap = Net::LDAP.new(host: HOST, port: PORT, base: "dc=example,dc=com",
                     auth: {method: :simple, username: "cn=admin,dc=example,dc=com", password: "secret"})
def net(r, label)
  yield
  r << "#{label}:noraise"
rescue Net::LDAP::Error
  r << "#{label}:ok"
end
net(r, "bind")    { ldap.bind }
net(r, "search")  { ldap.search(filter: "(cn=a)") }
net(r, "add")     { ldap.add(dn: "cn=x,dc=example,dc=com", attributes: {cn: "x"}) }
net(r, "compare") { ldap.compare("cn=x,dc=example,dc=com", "cn", "x") }
ldap.close
puts r.join("\n")
`)
	for _, label := range []string{"bind", "search", "add", "compare"} {
		if !strings.Contains(got, label+":ok") {
			t.Fatalf("TestNetLDAPNetworkErrors: %s did not raise\nfull:\n%s", label, got)
		}
	}
}

// TestNetLDAPRequire proves require "net/ldap", "net-ldap" and "ldap" are all
// provided features naming the same Net::LDAP class.
func TestNetLDAPRequire(t *testing.T) {
	got := runSrc(t, `
a = require "net/ldap"
b = require "net-ldap"
c = require "ldap"
puts a
puts(Net::LDAP.equal?(Net::LDAP))
puts(defined?(Net::LDAP::Filter) ? "filter" : "no")
`)
	if !strings.Contains(got, "true") || !strings.Contains(got, "filter") {
		t.Fatalf("TestNetLDAPRequire:\n%s", got)
	}
}

// portOf returns the port component of a host:port address.
func portOf(addr string) string {
	_, port, _ := net.SplitHostPort(addr)
	return port
}
