// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"os/user"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// etcSeams installs deterministic os/user lookups so Etc's success and
// not-found branches are exercised identically on every platform (Windows has
// no passwd/group database, so the real lookups would only ever take the error
// path there). It returns a fresh VM with the Etc module installed.
func etcSeams(t *testing.T, found bool) *VM {
	t.Helper()
	origID, origNm, origGID, origGrp, origUID, origTmp :=
		etcLookupID, etcLookup, etcLookupGID, etcLookupGrp, etcCurrentUID, etcTempDir
	t.Cleanup(func() {
		etcLookupID, etcLookup, etcLookupGID, etcLookupGrp, etcCurrentUID, etcTempDir =
			origID, origNm, origGID, origGrp, origUID, origTmp
	})
	etcCurrentUID = func() string { return "0" }
	etcTempDir = func() string { return "/seam-tmp" }
	if found {
		u := &user.User{Username: "root", Uid: "0", Gid: "0", Name: "Super User", HomeDir: "/root"}
		g := &user.Group{Name: "wheel", Gid: "0"}
		etcLookupID = func(string) (*user.User, error) { return u, nil }
		etcLookup = func(string) (*user.User, error) { return u, nil }
		etcLookupGID = func(string) (*user.Group, error) { return g, nil }
		etcLookupGrp = func(string) (*user.Group, error) { return g, nil }
	} else {
		etcLookupID = func(string) (*user.User, error) { return nil, errors.New("nope") }
		etcLookup = func(string) (*user.User, error) { return nil, errors.New("nope") }
		etcLookupGID = func(string) (*user.Group, error) { return nil, errors.New("nope") }
		etcLookupGrp = func(string) (*user.Group, error) { return nil, errors.New("nope") }
	}
	return New(nil)
}

// callEtc invokes an Etc module function by name.
func callEtc(t *testing.T, vm *VM, name string, args []object.Value) object.Value {
	t.Helper()
	mod := object.Kind[*RClass](vm.consts["Etc"])
	m := mod.smethods[name]
	if m == nil {
		t.Fatalf("Etc.%s not found", name)
	}
	return m.native(vm, mod, args, nil)
}

func TestEtcLookupsFound(t *testing.T) {
	vm := etcSeams(t, true)

	// getpwuid(0) -> Etc::Passwd struct with MRI members.
	pw := callEtc(t, vm, "getpwuid", []object.Value{object.Integer(0)})
	o, ok := object.KindOK[*RObject](pw)
	if !ok {
		t.Fatalf("getpwuid not an object: %#v", pw)
	}
	if o.ivars["@name"].ToS() != "root" || o.ivars["@uid"] != object.Integer(0) {
		t.Fatalf("passwd fields: %#v", o.ivars)
	}
	if o.class.name != "Etc::Passwd" {
		t.Fatalf("class = %q", o.class.name)
	}

	// no argument -> current user (etcCurrentUID); nil argument is the same.
	if v := callEtc(t, vm, "getpwuid", nil); object.Kind[*RObject](v).ivars["@name"].ToS() != "root" {
		t.Fatalf("getpwuid() current user wrong")
	}
	if v := callEtc(t, vm, "getpwuid", []object.Value{object.NilV}); object.Kind[*RObject](v).ivars["@name"].ToS() != "root" {
		t.Fatalf("getpwuid(nil) wrong")
	}

	// getpwnam, getgrgid, getgrnam, systmpdir all succeed.
	if object.Kind[*RObject](callEtc(t, vm, "getpwnam", []object.Value{object.NewString("root")})).ivars["@name"].ToS() != "root" {
		t.Fatalf("getpwnam wrong")
	}
	gr := object.Kind[*RObject](callEtc(t, vm, "getgrgid", []object.Value{object.Integer(0)}))
	if gr.ivars["@name"].ToS() != "wheel" || gr.class.name != "Etc::Group" {
		t.Fatalf("getgrgid: %#v", gr.ivars)
	}
	if object.Kind[*RObject](callEtc(t, vm, "getgrnam", []object.Value{object.NewString("wheel")})).ivars["@name"].ToS() != "wheel" {
		t.Fatalf("getgrnam wrong")
	}
	if callEtc(t, vm, "systmpdir", nil).ToS() != "/seam-tmp" {
		t.Fatalf("systmpdir wrong")
	}
}

func TestEtcEnsureClassesCached(t *testing.T) {
	vm := etcSeams(t, true)
	// First lookup builds the Passwd/Group struct classes; a second lookup must
	// reuse them (the cached branch of ensureClasses).
	first := object.Kind[*RObject](callEtc(t, vm, "getpwuid", []object.Value{object.Integer(0)}))
	second := object.Kind[*RObject](callEtc(t, vm, "getgrgid", []object.Value{object.Integer(0)}))
	third := object.Kind[*RObject](callEtc(t, vm, "getpwnam", []object.Value{object.NewString("root")}))
	if first.class != third.class {
		t.Fatalf("Passwd class not reused across lookups")
	}
	if second.class.name != "Etc::Group" {
		t.Fatalf("Group class wrong")
	}
}

func TestEtcLookupsNotFound(t *testing.T) {
	vm := etcSeams(t, false)
	cases := []struct {
		name string
		args []object.Value
	}{
		{"getpwuid", []object.Value{object.Integer(123)}},
		{"getpwnam", []object.Value{object.NewString("nobody")}},
		{"getgrgid", []object.Value{object.Integer(123)}},
		{"getgrnam", []object.Value{object.NewString("nogroup")}},
	}
	for _, c := range cases {
		got := catchRaise(func() { callEtc(t, vm, c.name, c.args) })
		if got != "ArgumentError" {
			t.Fatalf("%s not-found: got %q, want ArgumentError", c.name, got)
		}
	}
}

func TestEtcNotImplemented(t *testing.T) {
	vm := etcSeams(t, true)
	for _, m := range []string{
		"getpwent", "setpwent", "endpwent", "getgrent", "setgrent", "endgrent",
		"group", "passwd", "getlogin", "confstr", "sysconf", "nprocessors",
	} {
		got := catchRaise(func() { callEtc(t, vm, m, nil) })
		if got != "NotImplementedError" {
			t.Fatalf("Etc.%s: got %q, want NotImplementedError", m, got)
		}
	}
}

func TestEtcAtoiOr0(t *testing.T) {
	// Numeric id parses; a non-numeric id falls back to 0 (defensive branch for
	// platforms whose os/user could return a non-numeric id).
	if got := atoiOr0("42"); got != object.Integer(42) {
		t.Fatalf("atoiOr0(42) = %v", got)
	}
	if got := atoiOr0("notanumber"); got != object.Integer(0) {
		t.Fatalf("atoiOr0(non-numeric) = %v", got)
	}
}
