// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// dvPre is the shared Devise test preamble: a minimal ActiveRecord-shaped model
// (answering #[] / #[]= / #save over an in-memory attribute Hash), an in-memory
// user table wired to the finder seam, a frozen clock, and a low bcrypt cost so
// hashing is fast. mk registers a user; res binds one to the shared config.
const dvPre = `require "devise"
class User
  def initialize(a = {}); @a = a; end
  def [](k); @a[k]; end
  def []=(k, v); @a[k] = v; end
  def save; true; end
end
USERS = []
CFG = Devise.config
CFG.stretches = 4
CFG.finder = ->(attrs) { USERS.find { |u| attrs.all? { |k, v| u[k] == v } } }
CFG.now = ->{ Time.at(1000000000) }
def mk(a = {}); u = User.new(a); USERS << u; u; end
def res(u); CFG.build(u); end
`

// TestDeviseWrapperInspect covers ToS / Inspect / Truthy of the value wrappers.
func TestDeviseWrapperInspect(t *testing.T) {
	checks := []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{
		&DeviseConfig{},
		&DeviseResource{},
		&DeviseTokenGenerator{},
	}
	want := []string{"#<Devise::Config>", "#<Devise::Resource>", "#<Devise::TokenGenerator>"}
	for i, c := range checks {
		if c.ToS() != want[i] || c.Inspect() != want[i] || !c.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c, c.ToS(), c.Inspect(), c.Truthy())
		}
	}
}

// TestDeviseConstants covers the module/class surface and the require feature.
func TestDeviseConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "devise"`, "true\n"},
		{`require "devise"; p require "devise"`, "false\n"},
		{`require "devise"; p Devise.is_a?(Module)`, "true\n"},
		{`require "devise"; p Devise::Error < StandardError`, "true\n"},
		{`require "devise"; p Devise::Config.class`, "Class\n"},
		{`require "devise"; p Devise::Encryptor.is_a?(Module)`, "true\n"},
		{`require "devise"; p Devise::Strategies::DatabaseAuthenticatable < Warden::Strategies::Base`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDeviseModuleHelpers covers friendly_token, secure_compare and setup.
func TestDeviseModuleHelpers(t *testing.T) {
	src := `require "devise"
puts Devise.friendly_token.length
puts Devise.friendly_token(30).length
puts Devise.secure_compare("abc", "abc")
puts Devise.secure_compare("abc", "abd")
Devise.setup { |c| c.pepper = "seasoned" }
puts Devise.config.pepper
`
	if got := eval(t, src); got != "20\n30\ntrue\nfalse\nseasoned\n" {
		t.Errorf("got=%q", got)
	}
}

// TestDeviseEncryptor covers digest/compare with and without a pepper and an
// explicit cost, plus the out-of-range-cost error.
func TestDeviseEncryptor(t *testing.T) {
	src := `require "devise"
h = Devise::Encryptor.digest("secret", "", 4)
puts h.start_with?("$2")
puts Devise::Encryptor.compare(h, "secret")
puts Devise::Encryptor.compare(h, "wrong")
hp = Devise::Encryptor.digest("secret", "pep", 4)
puts Devise::Encryptor.compare(hp, "secret", "pep")
puts Devise::Encryptor.compare(hp, "secret")
`
	if got := eval(t, src); got != "true\ntrue\nfalse\ntrue\nfalse\n" {
		t.Errorf("got=%q", got)
	}
	if cls, _ := evalErr(t, `require "devise"; Devise::Encryptor.digest("x", "", 99)`); cls != "Devise::Error" {
		t.Errorf("digest bad cost: class=%q", cls)
	}
}

// TestDeviseTokenGenerator covers generate/digest and the empty-value digest.
func TestDeviseTokenGenerator(t *testing.T) {
	src := `require "devise"
g = Devise::TokenGenerator.new
raw, enc = g.generate("reset_password_token")
puts raw.length > 0
puts enc == g.digest("reset_password_token", raw)
puts g.digest("reset_password_token", "") == ""
`
	if got := eval(t, src); got != "true\ntrue\ntrue\n" {
		t.Errorf("got=%q", got)
	}
}

// TestDeviseConfigSettings drives every plain setting accessor and the
// authentication_keys TypeError branch and the finder-not-a-Proc branch.
func TestDeviseConfigSettings(t *testing.T) {
	src := `require "devise"
c = Devise::Config.new
c.stretches = 4; puts c.stretches
c.pepper = "salt"; puts c.pepper
c.maximum_attempts = 5; puts c.maximum_attempts
c.authentication_keys = [:email, :username]; puts c.authentication_keys.inspect
c.unlock_strategy = :time
c.lock_strategy = :none
c.password_length_min = 8
c.password_length_max = 100
c.reset_password_within = 3600
c.remember_for = 7200
c.unlock_in = 900
c.expire_all_remember_me_on_sign_out = false
c.allow_unconfirmed_access_for = 3600
c.allow_unconfirmed_access_for = nil
c.confirm_within = 3600
c.confirm_within = nil
c.timeout_in = 1800
c.timeout_in = nil
puts "ok"
`
	if got := eval(t, src); got != "4\nsalt\n5\n[:email, :username]\nok\n" {
		t.Errorf("got=%q", got)
	}
	if cls, _ := evalErr(t, `require "devise"; Devise::Config.new.authentication_keys = 5`); cls != "TypeError" {
		t.Errorf("authentication_keys: class=%q", cls)
	}
	if cls, _ := evalErr(t, `require "devise"; Devise::Config.new.finder = 5`); cls != "ArgumentError" {
		t.Errorf("finder=5: class=%q", cls)
	}
}

// TestDeviseDatabaseAuthenticatable covers password=, valid_password?,
// authenticatable_salt, model, and the bad-cost password= error.
func TestDeviseDatabaseAuthenticatable(t *testing.T) {
	src := dvPre + `
u = mk("email" => "a@b.com")
r = res(u)
puts r.authenticatable_salt
r.password = "password1"
puts u["encrypted_password"].start_with?("$2")
puts r.valid_password?("password1")
puts r.valid_password?("nope")
puts r.authenticatable_salt.length
puts r.model.equal?(u)
`
	if got := eval(t, src); got != "\ntrue\ntrue\nfalse\n29\ntrue\n" {
		t.Errorf("got=%q", got)
	}
	bad := dvPre + `
bc = Devise::Config.new
bc.stretches = 99
bc.build(mk("email" => "z@z.com")).password = "x"
`
	if cls, _ := evalErr(t, bad); cls != "Devise::Error" {
		t.Errorf("password= bad cost: class=%q", cls)
	}
}

// TestDeviseValidatable covers presence/format/uniqueness/length/confirmation,
// the no-argument path (blank email), and the finder-less config path.
func TestDeviseValidatable(t *testing.T) {
	src := dvPre + `
u = mk("email" => "a@b.com")
puts res(u).validatable_errors(password: "password1", password_confirmation: "password1", password_provided: true, email_changed: true).inspect
puts res(mk("email" => "")).validatable_errors.inspect
puts res(mk("email" => "nope")).validatable_errors(email_changed: true).inspect
mk("email" => "dup@x.com")
u2 = mk("email" => "dup@x.com")
puts res(u2).validatable_errors(email_changed: true).inspect
puts res(mk("email" => "c@d.com")).validatable_errors(password: "ab", password_confirmation: "zz", password_provided: true, email_changed: false).inspect
long = "x" * 200
puts res(mk("email" => "e@f.com")).validatable_errors(password: long, password_confirmation: long, password_provided: true).inspect
puts res(mk("email" => "g@h.com")).validatable_errors(password: "", password_confirmation: "", password_provided: true).inspect
puts Devise::Config.new.build(User.new("email" => "solo@x.com")).validatable_errors(email_changed: true).inspect
`
	want := "[]\n" +
		`[["email", :blank]]` + "\n" +
		`[["email", :invalid]]` + "\n" +
		`[["email", :taken]]` + "\n" +
		`[["password", :confirmation], ["password", :too_short]]` + "\n" +
		`[["password", :too_long]]` + "\n" +
		`[["password", :blank]]` + "\n" +
		"[]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestDeviseRecoverable covers token issuance, period validity, reset, the mailer
// callback, and the class-level reset_password_by_token flow (success + errors).
func TestDeviseRecoverable(t *testing.T) {
	src := dvPre + `
CFG.send_reset_password_instructions = ->(rec, tok) { puts "mailer:#{tok.length > 0}" }
u = mk("email" => "a@b.com")
r = res(u)
tok = r.set_reset_password_token
puts tok.length > 0
puts u["reset_password_token"] != nil
puts r.reset_password_period_valid?
u["reset_password_sent_at"] = Time.at(0)
puts r.reset_password_period_valid?
r.send_reset_password_instructions
r.reset_password("newpassword")
p u["reset_password_token"]
puts r.valid_password?("newpassword")
u2 = mk("email" => "z@z.com")
r2 = res(u2)
raw = r2.set_reset_password_token
back = CFG.reset_password_by_token(raw, "brandnew1", "brandnew1")
puts back.model.equal?(u2)
puts back.valid_password?("brandnew1")
`
	want := "true\ntrue\ntrue\nfalse\nmailer:true\nnil\ntrue\ntrue\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}

	// not found
	if cls, _ := evalErr(t, dvPre+"\nCFG.reset_password_by_token(\"bogus\", \"a\", \"b\")"); cls != "Devise::Error" {
		t.Errorf("reset not found: class=%q", cls)
	}
	// expired
	exp := dvPre + `
u = mk("email" => "e@e.com")
raw = res(u).set_reset_password_token
u["reset_password_sent_at"] = Time.at(0)
CFG.reset_password_by_token(raw, "brandnew1", "brandnew1")
`
	if cls, _ := evalErr(t, exp); cls != "Devise::Error" {
		t.Errorf("reset expired: class=%q", cls)
	}
	// confirmation mismatch
	mis := dvPre + `
u = mk("email" => "m@m.com")
raw = res(u).set_reset_password_token
CFG.reset_password_by_token(raw, "brandnew1", "different1")
`
	if cls, _ := evalErr(t, mis); cls != "Devise::Error" {
		t.Errorf("reset mismatch: class=%q", cls)
	}
}

// TestDeviseRememberable covers the remember/forget lifecycle, the value fallback
// to the salt, expiry, and the serialize_from_cookie flow (success + invalid).
func TestDeviseRememberable(t *testing.T) {
	src := dvPre + `
u = mk("email" => "a@b.com", "id" => "7")
r = res(u)
puts r.remember_expired?
r.remember_me!
puts u["remember_token"] != nil
puts r.remember_expired?
puts r.rememberable_value == u["remember_token"]
val = r.rememberable_value
back = CFG.serialize_from_cookie("7", val)
puts back.model.equal?(u)
r.forget_me!
p u["remember_token"]
r.password = "password1"
puts r.rememberable_value == r.authenticatable_salt
`
	want := "true\ntrue\nfalse\ntrue\ntrue\nnil\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
	if cls, _ := evalErr(t, dvPre+"\nCFG.serialize_from_cookie(\"999\", \"x\")"); cls != "Devise::Error" {
		t.Errorf("cookie invalid: class=%q", cls)
	}
}

// TestDeviseConfirmable covers token issuance, confirm, the period windows, the
// inactive message, and confirm_by_token (success + not found + already/expired).
func TestDeviseConfirmable(t *testing.T) {
	src := dvPre + `
CFG.send_confirmation_instructions = ->(rec, tok) { puts "cmail:#{tok.length > 0}" }
u = mk("email" => "a@b.com")
r = res(u)
puts r.confirmed?
tok = r.generate_confirmation_token
puts tok.length > 0
puts r.confirmation_period_valid?
puts r.active_for_authentication?
puts r.inactive_message
r.send_confirmation_instructions
r.confirm
puts r.confirmed?
u2 = mk("email" => "c@c.com")
r2 = res(u2)
raw = r2.generate_confirmation_token
back = CFG.confirm_by_token(raw)
puts back.confirmed?
`
	want := "false\ntrue\ntrue\ntrue\ninactive\ncmail:true\ntrue\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}

	// grace/expiry windows + unconfirmed message on a config with a zero grace.
	win := `require "devise"
class User
  def initialize(a = {}); @a = a; end
  def [](k); @a[k]; end
  def []=(k, v); @a[k] = v; end
  def save; true; end
end
c = Devise::Config.new
c.now = ->{ Time.at(1000000000) }
c.finder = ->(a) { nil }
c.allow_unconfirmed_access_for = 0
c.confirm_within = 10
u = User.new("email" => "x@y.com")
r = c.build(u)
puts r.confirmation_period_valid?
puts r.active_for_authentication?
puts r.inactive_message
u["confirmation_sent_at"] = Time.at(0)
puts r.confirmation_period_expired?
`
	if got := eval(t, win); got != "false\nfalse\nunconfirmed\ntrue\n" {
		t.Errorf("confirm windows got=%q", got)
	}

	// already confirmed
	already := dvPre + `
u = mk("email" => "a@a.com")
r = res(u)
r.generate_confirmation_token
r.confirm
r.confirm
`
	if cls, _ := evalErr(t, already); cls != "Devise::Error" {
		t.Errorf("already confirmed: class=%q", cls)
	}
	// confirm_by_token not found
	if cls, _ := evalErr(t, dvPre+"\nCFG.confirm_by_token(\"bogus\")"); cls != "Devise::Error" {
		t.Errorf("confirm not found: class=%q", cls)
	}
	// confirm period expired
	pexp := `require "devise"
class User
  def initialize(a = {}); @a = a; end
  def [](k); @a[k]; end
  def []=(k, v); @a[k] = v; end
  def save; true; end
end
c = Devise::Config.new
c.now = ->{ Time.at(1000000000) }
c.confirm_within = 10
u = User.new("email" => "x@y.com")
u["confirmation_sent_at"] = Time.at(0)
c.build(u).confirm
`
	if cls, _ := evalErr(t, pexp); cls != "Devise::Error" {
		t.Errorf("confirm expired: class=%q", cls)
	}
}

// TestDeviseLockable covers locking, unlocking, the failed-attempt gate, the
// unlock mailer, and unlock_access_by_token (success + not found + no block).
func TestDeviseLockable(t *testing.T) {
	src := dvPre + `
CAP = []
CFG.send_unlock_instructions = ->(rec, tok) { CAP << tok }
u = mk("email" => "a@b.com")
r = res(u)
puts r.access_locked?
puts r.failed_attempts
r.lock_access!
puts r.access_locked?
r.unlock_access!
puts r.access_locked?
puts r.valid_for_authentication? { true }
puts r.valid_for_authentication? { false }
puts r.failed_attempts
u2 = mk("email" => "lock@x.com")
r2 = res(u2)
r2.lock_access!(true)
raw = CAP.last
back = CFG.unlock_access_by_token(raw)
puts back.model.equal?(u2)
puts back.access_locked?
`
	want := "false\n0\ntrue\nfalse\ntrue\nfalse\n1\ntrue\nfalse\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
	if cls, _ := evalErr(t, dvPre+"\nCFG.unlock_access_by_token(\"bogus\")"); cls != "Devise::Error" {
		t.Errorf("unlock not found: class=%q", cls)
	}
	if cls, _ := evalErr(t, dvPre+"\nres(mk(\"email\" => \"n@n.com\")).valid_for_authentication?"); cls != "ArgumentError" {
		t.Errorf("valid_for_auth no block: class=%q", cls)
	}
}

// TestDeviseTrackable covers the sign-in tracking fields.
func TestDeviseTrackable(t *testing.T) {
	src := dvPre + `
u = mk("email" => "a@b.com")
r = res(u)
r.update_tracked_fields("1.2.3.4")
puts r.sign_in_count
puts u["current_sign_in_ip"]
r.update_tracked_fields!("5.6.7.8")
puts r.sign_in_count
puts u["last_sign_in_ip"]
`
	if got := eval(t, src); got != "1\n1.2.3.4\n2\n1.2.3.4\n" {
		t.Errorf("got=%q", got)
	}
}

// TestDeviseTimeoutable covers timedout? and timeout_in with and without a window.
func TestDeviseTimeoutable(t *testing.T) {
	src := dvPre + `
u = mk("email" => "a@b.com")
r = res(u)
puts r.timedout?(Time.at(0))
p r.timeout_in
c = Devise::Config.new
c.now = ->{ Time.at(1000000000) }
c.timeout_in = 100
tr = c.build(User.new)
puts tr.timedout?(Time.at(0))
puts tr.timedout?(Time.at(1000000000))
puts tr.timeout_in
`
	if got := eval(t, src); got != "false\nnil\ntrue\nfalse\n100\n" {
		t.Errorf("got=%q", got)
	}
}

// TestDeviseClockFallback covers the wireNow fallback when the clock Proc yields
// a non-Time value, and the mailer callbacks' no-callback branch.
func TestDeviseClockFallback(t *testing.T) {
	src := `require "devise"
class User
  def initialize(a = {}); @a = a; end
  def [](k); @a[k]; end
  def []=(k, v); @a[k] = v; end
  def save; true; end
end
c = Devise::Config.new
c.now = ->{ 42 }
u = User.new("reset_password_sent_at" => Time.at(0))
puts c.build(u).reset_password_period_valid?
r = c.build(User.new("email" => "n@n.com"))
r.send_reset_password_instructions
r.send_confirmation_instructions
r.lock_access!(true)
puts "done"
`
	if got := eval(t, src); got != "false\ndone\n" {
		t.Errorf("got=%q", got)
	}
}

// TestDeviseWardenStrategy is the headline integration: the
// database_authenticatable strategy plugged into the bound Warden Manager
// authenticates a request (success), rejects a bad password and an incomplete
// request (failure app), and survives a malformed query string.
func TestDeviseWardenStrategy(t *testing.T) {
	src := dvPre + `
u = mk("email" => "amy@x.com")
res(u).password = "secret12"
APP = ->(env) {
  usr = env["warden"].authenticate!(:database_authenticatable)
  [200, { "Content-Type" => "text/plain" }, ["hi #{usr["email"]}"]]
}
FAIL = ->(env) { [401, {}, ["denied"]] }
def env_for(q); { "REQUEST_METHOD" => "GET", "PATH_INFO" => "/", "QUERY_STRING" => q, "rack.session" => {} }; end
ok = Warden::Manager.new(APP) { |m| m.default_strategies :database_authenticatable }
s, h, b = ok.call(env_for("email=amy@x.com&password=secret12"))
puts s
puts b.join
bad = Warden::Manager.new(APP) { |m| m.default_strategies(:database_authenticatable); m.failure_app = FAIL }
s2, _, _ = bad.call(env_for("email=amy@x.com&password=wrong"))
puts s2
s3, _, _ = bad.call(env_for("email=amy@x.com"))
puts s3
s4, _, _ = bad.call(env_for("%zz"))
puts s4
`
	if got := eval(t, src); got != "200\nhi amy@x.com\n401\n401\n401\n" {
		t.Errorf("got=%q", got)
	}
}
