// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	age "github.com/go-ruby-age/age"
	"github.com/go-ruby-age/age/scrypt"
	"github.com/go-ruby-age/age/x25519"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestAgeX25519RoundTrip drives the X25519 surface through rbgo end to end: a
// generated identity's public recipient encrypts a message that the identity
// decrypts, in both binary and armored form, and a recipient/identity parsed
// back from its String form round-trips too.
func TestAgeX25519RoundTrip(t *testing.T) {
	src := `
require "age"
id  = Age::X25519::Identity.generate
rcp = id.to_public
r = []
r << (id.to_s.start_with?("AGE-SECRET-KEY-1") ? "sec" : id.to_s)
r << (rcp.to_s.start_with?("age1") ? "pub" : rcp.to_s)
r << id.inspect.start_with?("\"AGE-SECRET-KEY-1")
r << rcp.inspect.start_with?("\"age1")
ct = Age.encrypt("hello world", recipients: [rcp])
r << Age.decrypt(ct, identities: [id])
act = Age.encrypt("armored!", recipients: [rcp], armor: true)
r << act.start_with?("-----BEGIN AGE ENCRYPTED FILE-----")
r << Age.decrypt(act, identities: [id])
rcp2 = Age::X25519::Recipient.from_string(rcp.to_s)
id2  = Age::X25519::Identity.from_string(id.to_s)
ct2  = Age.encrypt("via strings", recipients: [rcp2.to_s])
r << Age.decrypt(ct2, identities: [id2.to_s])
puts r.join("|")
`
	want := `sec|pub|true|true|hello world|true|armored!|via strings`
	if got := runSrc(t, src); got != want {
		t.Fatalf("age X25519 round-trip = %q want %q", got, want)
	}
}

// TestAgeScryptRoundTrip drives the passphrase (scrypt) surface: a recipient
// with a tuned work factor encrypts a message that a matching identity (with a
// tuned max work factor) decrypts, and the wrong passphrase is rejected with
// Age::IncorrectPassphraseError.
func TestAgeScryptRoundTrip(t *testing.T) {
	src := `
require "age"
r = []
rcp = Age::Scrypt::Recipient.new("correct horse battery staple", work_factor: 10)
ct  = Age.encrypt("secret msg", recipients: [rcp])
sid = Age::Scrypt::Identity.new("correct horse battery staple", max_work_factor: 20)
r << Age.decrypt(ct, identities: [sid])
# defaults (no options hash)
rcp2 = Age::Scrypt::Recipient.new("pw")
ct2  = Age.encrypt("dflt", recipients: [rcp2])
r << Age.decrypt(ct2, identities: [Age::Scrypt::Identity.new("pw")])
begin
  Age.decrypt(ct, identities: [Age::Scrypt::Identity.new("wrong")])
rescue Age::IncorrectPassphraseError => e
  r << "bad-pass"
end
puts r.join("|")
`
	want := `secret msg|dflt|bad-pass`
	if got := runSrc(t, src); got != want {
		t.Fatalf("age scrypt round-trip = %q want %q", got, want)
	}
}

// TestAgeErrorTree exercises the raised exceptions from Ruby: a wrong identity
// raises Age::NoIdentityMatchError (an Age::Error, a StandardError), a malformed
// recipient/identity string raises Age::ParseError, an empty passphrase raises
// Age::ParseError, a tampered ciphertext raises Age::FormatError, and combining
// an scrypt recipient with another raises Age::EncryptError.
func TestAgeErrorTree(t *testing.T) {
	src := `
require "age"
id  = Age::X25519::Identity.generate
rcp = id.to_public
ct  = Age.encrypt("m", recipients: [rcp])
r = []
begin
  Age.decrypt(ct, identities: [Age::X25519::Identity.generate])
rescue Age::NoIdentityMatchError => e
  r << (e.is_a?(Age::Error) && e.is_a?(StandardError) ? "no-id" : "bad")
end
begin
  Age::X25519::Recipient.from_string("age1nonsense")
rescue Age::ParseError
  r << "parse-rcp"
end
begin
  Age::X25519::Identity.from_string("nope")
rescue Age::ParseError
  r << "parse-id"
end
begin
  Age::Scrypt::Recipient.new("")
rescue Age::ParseError
  r << "empty-pass"
end
begin
  Age::Scrypt::Identity.new("")
rescue Age::ParseError
  r << "empty-pass-id"
end
begin
  Age.encrypt("m", recipients: ["age1nonsense"])
rescue Age::ParseError
  r << "str-rcp"
end
begin
  Age.decrypt(ct, identities: ["AGE-SECRET-KEY-1BOGUS"])
rescue Age::ParseError
  r << "str-id"
end
begin
  Age.decrypt("-----BEGIN AGE ENCRYPTED FILE-----\ngarbage\n", identities: [id])
rescue Age::FormatError
  r << "format"
end
begin
  Age.encrypt("m", recipients: [rcp, Age::Scrypt::Recipient.new("pw")])
rescue Age::EncryptError
  r << "encrypt"
end
puts r.join("|")
`
	want := `no-id|parse-rcp|parse-id|empty-pass|empty-pass-id|str-rcp|str-id|format|encrypt`
	if got := runSrc(t, src); got != want {
		t.Fatalf("age error tree = %q want %q", got, want)
	}
}

// TestAgeArgumentErrors covers the missing-keyword paths: Age.encrypt without
// recipients: and Age.decrypt without identities: both raise ArgumentError, and
// the trailing-argument shapes ageOptsHash handles (a non-Hash trailing arg, and
// no trailing arg at all via Scrypt::Recipient.new/pw) are reached.
func TestAgeArgumentErrors(t *testing.T) {
	src := `
require "age"
r = []
begin
  Age.encrypt("m")
rescue ArgumentError => e
  r << "no-rcp"
end
begin
  Age.decrypt("x", {})
rescue ArgumentError => e
  r << "no-id"
end
# a non-Hash trailing arg: ageOptsHash returns nil, so no work_factor is applied
rcp = Age::Scrypt::Recipient.new("pw", 7)
r << (rcp.is_a?(Age::Scrypt::Recipient) ? "int-arg" : "bad")
id = Age::Scrypt::Identity.new("pw", 7)
r << (id.is_a?(Age::Scrypt::Identity) ? "int-arg2" : "bad")
puts r.join("|")
`
	want := `no-rcp|no-id|int-arg|int-arg2`
	if got := runSrc(t, src); got != want {
		t.Fatalf("age argument errors = %q want %q", got, want)
	}
}

// TestAgeRecipientIdentityTypeErrors covers the default TypeError branch of the
// recipient/identity conversions (a non-recipient / non-identity element) and
// the single-value (non-Array) normalisation of ageOptElems.
func TestAgeRecipientIdentityTypeErrors(t *testing.T) {
	single := `
require "age"
id  = Age::X25519::Identity.generate
rcp = id.to_public
ct  = Age.encrypt("solo", recipients: rcp)      # a bare recipient, not an Array
r = []
r << Age.decrypt(ct, identities: id)             # a bare identity, not an Array
begin
  Age.decrypt(ct, identities: 99)                # a non-identity scalar
rescue TypeError
  r << "id-type"
end
puts r.join("|")
`
	if got := runSrc(t, single); got != "solo|id-type" {
		t.Fatalf("age single-value/type = %q want %q", got, "solo|id-type")
	}

	rcpType := `
require "age"
begin
  Age.encrypt("m", recipients: [123])
rescue TypeError
  puts "rcp-type"
end
`
	if got := runSrc(t, rcpType); got != "rcp-type" {
		t.Fatalf("age recipient TypeError = %q want %q", got, "rcp-type")
	}
}

// TestAgeGenerateFailure drives the otherwise-unreachable randomness-failure
// branch of Age::X25519::Identity.generate by substituting a failing generator
// through the ageGenerate seam; the raised exception is Age::ParseError (an
// Age::Error), the class the parse sentinel maps to.
func TestAgeGenerateFailure(t *testing.T) {
	orig := ageGenerate
	t.Cleanup(func() { ageGenerate = orig })
	ageGenerate = func() (*x25519.Identity, error) {
		return nil, errors.New("no randomness")
	}
	src := `
require "age"
begin
  Age::X25519::Identity.generate
rescue Age::Error => e
  puts "gen-fail"
end
`
	if got := runSrc(t, src); got != "gen-fail" {
		t.Fatalf("age generate failure = %q want %q", got, "gen-fail")
	}
}

// TestAgeRaiseError maps each go-ruby-age sentinel onto its Ruby exception class
// (and an unrecognised error onto the Age::Error base), covering every arm of
// raiseAgeError directly.
func TestAgeRaiseError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{age.ErrNoIdentityMatch, "Age::NoIdentityMatchError"},
		{age.ErrIncorrectPassphrase, "Age::IncorrectPassphraseError"},
		{age.ErrParse, "Age::ParseError"},
		{age.ErrFormat, "Age::FormatError"},
		{age.ErrEncrypt, "Age::EncryptError"},
		{errors.New("boom"), "Age::Error"},
	}
	for _, c := range cases {
		func() {
			defer func() {
				re, ok := recover().(RubyError)
				if !ok {
					t.Fatalf("raiseAgeError(%v): did not raise a RubyError", c.err)
				}
				if re.Class != c.want {
					t.Errorf("raiseAgeError(%v): class %q want %q", c.err, re.Class, c.want)
				}
			}()
			raiseAgeError(c.err)
		}()
	}
}

// TestAgeValueMethods covers the Go-level object.Value surface of the four
// wrapper types (Truthy, ToS, Inspect) reached directly rather than through
// dispatch, so the scrypt wrappers' fixed ToS/Inspect and the X25519 wrappers'
// key rendering are exercised.
func TestAgeValueMethods(t *testing.T) {
	id, err := x25519.Generate()
	if err != nil {
		t.Fatal(err)
	}
	xi := &AgeX25519Identity{id: id}
	xr := &AgeX25519Recipient{rcp: id.ToPublic()}
	if !xi.Truthy() || !xr.Truthy() {
		t.Error("X25519 wrappers should be truthy")
	}
	if !strings.HasPrefix(xi.ToS(), "AGE-SECRET-KEY-1") {
		t.Errorf("identity ToS = %q", xi.ToS())
	}
	if !strings.HasPrefix(xr.ToS(), "age1") {
		t.Errorf("recipient ToS = %q", xr.ToS())
	}
	if !strings.HasPrefix(xi.Inspect(), `"AGE-SECRET-KEY-1`) {
		t.Errorf("identity Inspect = %q", xi.Inspect())
	}
	if !strings.HasPrefix(xr.Inspect(), `"age1`) {
		t.Errorf("recipient Inspect = %q", xr.Inspect())
	}

	sr, err := scrypt.NewRecipient("pw")
	if err != nil {
		t.Fatal(err)
	}
	si, err := scrypt.NewIdentity("pw")
	if err != nil {
		t.Fatal(err)
	}
	sri := &AgeScryptRecipient{rcp: sr}
	sii := &AgeScryptIdentity{id: si}
	if !sri.Truthy() || !sii.Truthy() {
		t.Error("scrypt wrappers should be truthy")
	}
	if sri.ToS() != "#<Age::Scrypt::Recipient>" || sri.Inspect() != sri.ToS() {
		t.Errorf("scrypt recipient render = %q / %q", sri.ToS(), sri.Inspect())
	}
	if sii.ToS() != "#<Age::Scrypt::Identity>" || sii.Inspect() != sii.ToS() {
		t.Errorf("scrypt identity render = %q / %q", sii.ToS(), sii.Inspect())
	}
}

// TestAgeClassOf proves the four wrapper types report their bound class through
// the interpreter's classOf dispatch basis.
func TestAgeClassOf(t *testing.T) {
	vm := New(&bytes.Buffer{})
	mod := vm.consts["Age"].(*RClass)
	x := mod.consts["X25519"].(*RClass)
	s := mod.consts["Scrypt"].(*RClass)
	xiCls := x.consts["Identity"].(*RClass)
	xrCls := x.consts["Recipient"].(*RClass)
	siCls := s.consts["Identity"].(*RClass)
	srCls := s.consts["Recipient"].(*RClass)

	id, _ := x25519.Generate()
	sr, _ := scrypt.NewRecipient("pw")
	si, _ := scrypt.NewIdentity("pw")
	pairs := []struct {
		v    object.Value
		want *RClass
	}{
		{&AgeX25519Identity{cls: xiCls, id: id}, xiCls},
		{&AgeX25519Recipient{cls: xrCls, rcp: id.ToPublic()}, xrCls},
		{&AgeScryptIdentity{cls: siCls, id: si}, siCls},
		{&AgeScryptRecipient{cls: srCls, rcp: sr}, srCls},
	}
	for _, p := range pairs {
		if got := vm.classOf(p.v); got != p.want {
			t.Errorf("classOf(%T) = %v want %v", p.v, got, p.want)
		}
	}
}
