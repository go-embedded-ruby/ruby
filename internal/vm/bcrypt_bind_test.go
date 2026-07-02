// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"testing"

	bcrypt "github.com/go-ruby-bcrypt/bcrypt"
)

// TestBCryptCreateVerify drives BCrypt::Password.create + verification through
// rbgo end to end: a hash created at a low cost verifies for the right secret and
// rejects a wrong one, and its parsed fields (cost/version/salt/checksum) round-
// trip through Password.new.
func TestBCryptCreateVerify(t *testing.T) {
	src := `
require "bcrypt"
pw = BCrypt::Password.create("s3cret", cost: 4)
r = []
r << pw.cost
r << pw.version
r << (pw == "s3cret")
r << (pw == "nope")
r << pw.is_password?("s3cret")
stored = BCrypt::Password.new(pw.to_s)
r << (stored.checksum == pw.checksum)
r << (stored.salt == pw.salt)
r << (stored.to_str == pw.to_s)
r << pw.inspect.start_with?("\"$2")
puts r.join(",")
`
	if got, want := runSrc(t, src), "4,2a,true,false,true,true,true,true,true"; got != want {
		t.Fatalf("bcrypt create/verify = %q want %q", got, want)
	}
}

// TestBCryptEngine exercises the low-level BCrypt::Engine surface and its cost
// constants through rbgo.
func TestBCryptEngine(t *testing.T) {
	src := `
require "bcrypt"
r = []
salt = BCrypt::Engine.generate_salt(4)
r << salt.start_with?("$2a$04$")
h = BCrypt::Engine.hash_secret("pw", salt)
r << (BCrypt::Password.new(h) == "pw")
r << BCrypt::Engine.valid_secret?("x")
r << BCrypt::Engine.valid_salt?(salt)
r << BCrypt::Engine.valid_salt?("nope")
r << BCrypt::Engine.autodetect_cost(salt)
r << BCrypt::Engine::DEFAULT_COST
r << BCrypt::Engine::MIN_COST
r << BCrypt::Engine::MAX_COST
puts r.join(",")
`
	if got, want := runSrc(t, src), "true,true,true,true,false,4,12,4,31"; got != want {
		t.Fatalf("bcrypt engine = %q want %q", got, want)
	}
}

// TestBCryptErrors proves the BCrypt::Errors exception tree is wired: a malformed
// hash raises BCrypt::Errors::InvalidHash (a BCryptError, a StandardError), an
// over-max cost raises ArgumentError, and a bad salt raises InvalidSalt.
func TestBCryptErrors(t *testing.T) {
	src := `
require "bcrypt"
r = []
begin
  BCrypt::Password.new("not-a-hash")
rescue BCrypt::Errors::InvalidHash => e
  r << "invalid-hash"
end
begin
  BCrypt::Password.new("not-a-hash")
rescue StandardError
  r << "std"
end
begin
  BCrypt::Password.create("x", cost: 99)
rescue ArgumentError
  r << "arg"
end
begin
  BCrypt::Engine.hash_secret("x", "bad-salt")
rescue BCrypt::Errors::InvalidSalt
  r << "invalid-salt"
end
begin
  BCrypt::Engine.generate_salt(0)
rescue BCrypt::Errors::InvalidCost
  r << "invalid-cost"
end
puts r.join(",")
`
	if got, want := runSrc(t, src), "invalid-hash,std,arg,invalid-salt,invalid-cost"; got != want {
		t.Fatalf("bcrypt errors = %q want %q", got, want)
	}
}

// TestBCryptRaiseError covers raiseBCryptError's sentinel-to-Ruby-class mapping
// directly, including the InvalidSecret arm and the default fall-through that the
// Ruby-level tests do not reach (a generic sentinel maps to BCryptError).
func TestBCryptRaiseError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{bcrypt.ErrCostTooHigh, "ArgumentError"},
		{bcrypt.ErrInvalidSecret, "BCrypt::Errors::InvalidSecret"},
		{bcrypt.ErrInvalidCost, "BCrypt::Errors::InvalidCost"},
		{bcrypt.ErrInvalidSalt, "BCrypt::Errors::InvalidSalt"},
		{bcrypt.ErrInvalidHash, "BCrypt::Errors::InvalidHash"},
		{errors.New("boom"), "BCrypt::Errors::BCryptError"},
	}
	for _, c := range cases {
		func() {
			defer func() {
				r := recover()
				re, ok := r.(RubyError)
				if !ok {
					t.Fatalf("raiseBCryptError(%v): got %v want RubyError", c.err, r)
				}
				if re.Class != c.want {
					t.Errorf("raiseBCryptError(%v): class %q want %q", c.err, re.Class, c.want)
				}
			}()
			raiseBCryptError(c.err)
		}()
	}
}

// TestBCryptPasswordValue covers the BCryptPassword value methods reached in Go
// (Truthy, and Inspect via a directly constructed value) so the wrapper's own
// object.Value surface is exercised.
func TestBCryptPasswordValue(t *testing.T) {
	p, err := bcrypt.CreateString("v", bcrypt.WithCost(4))
	if err != nil {
		t.Fatal(err)
	}
	bp := &BCryptPassword{p: p}
	if !bp.Truthy() {
		t.Error("BCryptPassword should be truthy")
	}
	if got := bp.ToS(); got != p.String() {
		t.Errorf("ToS = %q want %q", got, p.String())
	}
	if bp.Inspect() == bp.ToS() {
		t.Error("Inspect should quote the hash")
	}
}
