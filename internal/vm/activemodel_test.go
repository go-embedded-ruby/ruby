// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// amCase runs one Ruby source and compares captured stdout.
func amCase(t *testing.T, src, want string) {
	t.Helper()
	if got := eval(t, src); got != want {
		t.Errorf("src=%q\n got=%q\nwant=%q", src, got, want)
	}
}

// TestActiveModelRequire covers the module, its constants and the require keys.
func TestActiveModelRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "active_model"`, "true\n"},
		{`require "active_model"; p require "active_model"`, "false\n"},
		{`p require "activemodel"`, "true\n"},
		{`require "active_model"; p ActiveModel.is_a?(Module)`, "true\n"},
		{`require "active_model"; p ActiveModel::Validations.is_a?(Module)`, "true\n"},
		{`require "active_model"; p ActiveModel::Naming.is_a?(Module)`, "true\n"},
		{`require "active_model"; p ActiveModel::Errors.is_a?(Class)`, "true\n"},
		{`require "active_model"; p ActiveModel::Error.is_a?(Class)`, "true\n"},
		{`require "active_model"; p ActiveModel::Name.is_a?(Class)`, "true\n"},
		{`require "active_model"; p ActiveModel::EachValidator < ActiveModel::Validator`, "true\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelName covers ActiveModel::Name and its readers, including the
// namespaced form and the name override.
func TestActiveModelName(t *testing.T) {
	base := `require "active_model"; n = ActiveModel::Name.new("Person"); `
	cases := []struct{ src, want string }{
		{base + `p n.name`, "\"Person\"\n"},
		{base + `p n.to_s`, "\"Person\"\n"},
		{base + `puts "#{n}"`, "Person\n"},
		{base + `p n.singular`, "\"person\"\n"},
		{base + `p n.plural`, "\"people\"\n"},
		{base + `p n.element`, "\"person\"\n"},
		{base + `p n.human`, "\"Person\"\n"},
		{base + `p n.collection`, "\"people\"\n"},
		{base + `p n.param_key`, "\"person\"\n"},
		{base + `p n.route_key`, "\"people\"\n"},
		{base + `p n.singular_route_key`, "\"person\"\n"},
		{base + `p n.i18n_key`, ":person\n"},
		{base + `p n.class`, "ActiveModel::Name\n"},
		// Namespaced: Name.new(name, namespace).
		{`require "active_model"; n = ActiveModel::Name.new("Admin::User", "Admin"); p n.singular`, "\"admin_user\"\n"},
		{`require "active_model"; n = ActiveModel::Name.new("Admin::User", "Admin"); p n.param_key`, "\"user\"\n"},
		// name override (third arg).
		{`require "active_model"; n = ActiveModel::Name.new("Whatever", nil, "Person"); p n.singular`, "\"person\"\n"},
		// A Class argument uses its name.
		{`require "active_model"; class Person; end; n = ActiveModel::Name.new(Person); p n.plural`, "\"people\"\n"},
		// to_str allows implicit string coercion.
		{base + `p n.to_str`, "\"Person\"\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelNaming covers the ActiveModel::Naming module functions and the
// model_name class method installed by `extend ActiveModel::Naming`.
func TestActiveModelNaming(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "active_model"; class Person; end; p ActiveModel::Naming.singular(Person)`, "\"person\"\n"},
		{`require "active_model"; class Person; end; p ActiveModel::Naming.plural(Person)`, "\"people\"\n"},
		{`require "active_model"; class Person; end; p ActiveModel::Naming.param_key(Person)`, "\"person\"\n"},
		{`require "active_model"; class Person; end; p ActiveModel::Naming.route_key(Person)`, "\"people\"\n"},
		{`require "active_model"; class Person; end; p ActiveModel::Naming.singular_route_key(Person)`, "\"person\"\n"},
		{`require "active_model"; class Sheep; end; p ActiveModel::Naming.uncountable?(Sheep)`, "true\n"},
		{`require "active_model"; class Person; end; p ActiveModel::Naming.uncountable?(Person)`, "false\n"},
		// extend Naming installs model_name; Naming.singular then reads it.
		{`require "active_model"
class Person
  extend ActiveModel::Naming
end
p Person.model_name.plural`, "\"people\"\n"},
		{`require "active_model"
class Person
  extend ActiveModel::Naming
end
p ActiveModel::Naming.singular(Person)`, "\"person\"\n"},
		// model_name on an instance whose class responds to it.
		{`require "active_model"
class Person
  extend ActiveModel::Naming
end
p ActiveModel::Naming.plural(Person.new)`, "\"people\"\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// amModel is a class including ActiveModel::Validations with attr accessors, used
// by the validation tests.
const amModel = `require "active_model"
class Widget
  include ActiveModel::Validations
  attr_accessor :name, :size, :code, :email, :status, :role, :terms, :password, :password_confirmation, :quantity
end
`

// TestActiveModelPresenceAbsence covers presence / absence and the basic
// valid?/invalid?/errors surface.
func TestActiveModelPresenceAbsence(t *testing.T) {
	cases := []struct{ src, want string }{
		{amModel + `Widget.validates :name, presence: true
w = Widget.new
p w.valid?`, "false\n"},
		{amModel + `Widget.validates :name, presence: true
w = Widget.new
w.valid?
p w.errors.full_messages`, "[\"Name can't be blank\"]\n"},
		{amModel + `Widget.validates :name, presence: true
w = Widget.new; w.name = "ok"
p w.valid?`, "true\n"},
		{amModel + `Widget.validates :name, presence: true
w = Widget.new
p w.invalid?`, "true\n"},
		{amModel + `Widget.validates :name, absence: true
w = Widget.new; w.name = "x"
w.valid?
p w.errors.full_messages`, "[\"Name must be blank\"]\n"},
		// errors is empty before validation.
		{amModel + `Widget.validates :name, presence: true
w = Widget.new
p w.errors.empty?`, "true\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelLength covers the length validator and its per-key messages.
func TestActiveModelLength(t *testing.T) {
	cases := []struct{ src, want string }{
		{amModel + `Widget.validates :name, length: { minimum: 3 }
w = Widget.new; w.name = "ab"; w.valid?
p w.errors.full_messages`, "[\"Name is too short (minimum is 3 characters)\"]\n"},
		{amModel + `Widget.validates :name, length: { maximum: 2 }
w = Widget.new; w.name = "abc"; w.valid?
p w.errors.full_messages`, "[\"Name is too long (maximum is 2 characters)\"]\n"},
		{amModel + `Widget.validates :name, length: { is: 4 }
w = Widget.new; w.name = "abc"; w.valid?
p w.errors.full_messages`, "[\"Name is the wrong length (should be 4 characters)\"]\n"},
		{amModel + `Widget.validates :name, length: { in: 2..4 }
w = Widget.new; w.name = "a"; w.valid?
p w.errors.full_messages`, "[\"Name is too short (minimum is 2 characters)\"]\n"},
		{amModel + `Widget.validates :name, length: { within: 2..4 }
w = Widget.new; w.name = "abcde"; w.valid?
p w.errors.full_messages`, "[\"Name is too long (maximum is 4 characters)\"]\n"},
		// custom per-key messages.
		{amModel + `Widget.validates :name, length: { minimum: 3, too_short: "too wee" }
w = Widget.new; w.name = "a"; w.valid?
p w.errors.full_messages`, "[\"Name too wee\"]\n"},
		{amModel + `Widget.validates :name, length: { maximum: 1, too_long: "too big" }
w = Widget.new; w.name = "ab"; w.valid?
p w.errors.full_messages`, "[\"Name too big\"]\n"},
		{amModel + `Widget.validates :name, length: { is: 2, wrong_length: "nope" }
w = Widget.new; w.name = "a"; w.valid?
p w.errors.full_messages`, "[\"Name nope\"]\n"},
		// length on an array attribute (valueLength slice arm).
		{amModel + `Widget.validates :name, length: { minimum: 2 }
w = Widget.new; w.name = [1]; w.valid?
p w.errors.full_messages`, "[\"Name is too short (minimum is 2 characters)\"]\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelFormat covers the format validator (with/without/shorthand).
func TestActiveModelFormat(t *testing.T) {
	cases := []struct{ src, want string }{
		{amModel + `Widget.validates :code, format: { with: /\A[a-z]+\z/ }
w = Widget.new; w.code = "AB1"; w.valid?
p w.errors.full_messages`, "[\"Code is invalid\"]\n"},
		{amModel + `Widget.validates :code, format: { without: /\d/ }
w = Widget.new; w.code = "a1"; w.valid?
p w.errors.full_messages`, "[\"Code is invalid\"]\n"},
		// shorthand: format: /re/ means with:.
		{amModel + `Widget.validates :code, format: /\A[a-z]+\z/
w = Widget.new; w.code = "ok"; w.valid?
p w.valid?`, "true\n"},
		// custom message + case-insensitive flag translation.
		{amModel + `Widget.validates :code, format: { with: /\Aabc\z/i, message: "bad" }
w = Widget.new; w.code = "ABC"; w.valid?
p w.valid?`, "true\n"},
		{amModel + `Widget.validates :code, format: { with: /x/, message: "bad" }
w = Widget.new; w.code = "y"; w.valid?
p w.errors.full_messages`, "[\"Code bad\"]\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelInclusionExclusion covers inclusion/exclusion, array and range.
func TestActiveModelInclusionExclusion(t *testing.T) {
	cases := []struct{ src, want string }{
		{amModel + `Widget.validates :status, inclusion: { in: ["a", "b"] }
w = Widget.new; w.status = "z"; w.valid?
p w.errors.full_messages`, "[\"Status is not included in the list\"]\n"},
		{amModel + `Widget.validates :status, inclusion: { in: ["a", "b"] }
w = Widget.new; w.status = "a"
p w.valid?`, "true\n"},
		{amModel + `Widget.validates :role, exclusion: { in: ["admin"] }
w = Widget.new; w.role = "admin"; w.valid?
p w.errors.full_messages`, "[\"Role is reserved\"]\n"},
		// numeric range membership.
		{amModel + `Widget.validates :size, inclusion: { in: 1..5 }
w = Widget.new; w.size = 9; w.valid?
p w.errors.full_messages`, "[\"Size is not included in the list\"]\n"},
		{amModel + `Widget.validates :size, inclusion: { within: 1..5 }
w = Widget.new; w.size = 3
p w.valid?`, "true\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelNumericality covers the numericality validator's comparisons,
// parity, only_integer, symbol/proc bounds and not-a-number.
func TestActiveModelNumericality(t *testing.T) {
	cases := []struct{ src, want string }{
		{amModel + `Widget.validates :size, numericality: true
w = Widget.new; w.size = "abc"; w.valid?
p w.errors.full_messages`, "[\"Size is not a number\"]\n"},
		{amModel + `Widget.validates :size, numericality: { only_integer: true }
w = Widget.new; w.size = 1.5; w.valid?
p w.errors.full_messages`, "[\"Size must be an integer\"]\n"},
		{amModel + `Widget.validates :size, numericality: { greater_than: 10 }
w = Widget.new; w.size = 5; w.valid?
p w.errors.full_messages`, "[\"Size must be greater than 10\"]\n"},
		{amModel + `Widget.validates :size, numericality: { greater_than_or_equal_to: 10 }
w = Widget.new; w.size = 5; w.valid?
p w.errors.full_messages`, "[\"Size must be greater than or equal to 10\"]\n"},
		{amModel + `Widget.validates :size, numericality: { equal_to: 10 }
w = Widget.new; w.size = 5; w.valid?
p w.errors.full_messages`, "[\"Size must be equal to 10\"]\n"},
		{amModel + `Widget.validates :size, numericality: { less_than: 3 }
w = Widget.new; w.size = 5; w.valid?
p w.errors.full_messages`, "[\"Size must be less than 3\"]\n"},
		{amModel + `Widget.validates :size, numericality: { less_than_or_equal_to: 3 }
w = Widget.new; w.size = 5; w.valid?
p w.errors.full_messages`, "[\"Size must be less than or equal to 3\"]\n"},
		{amModel + `Widget.validates :size, numericality: { other_than: 5 }
w = Widget.new; w.size = 5; w.valid?
p w.errors.full_messages`, "[\"Size must be other than 5\"]\n"},
		{amModel + `Widget.validates :size, numericality: { odd: true }
w = Widget.new; w.size = 4; w.valid?
p w.errors.full_messages`, "[\"Size must be odd\"]\n"},
		{amModel + `Widget.validates :size, numericality: { even: true }
w = Widget.new; w.size = 3; w.valid?
p w.errors.full_messages`, "[\"Size must be even\"]\n"},
		// float bound, passing.
		{amModel + `Widget.validates :size, numericality: { greater_than: 1.5 }
w = Widget.new; w.size = 2
p w.valid?`, "true\n"},
		// symbol bound resolves a method on the model.
		{amModel + `Widget.validates :size, numericality: { greater_than: :floor }
w = Widget.new
def w.floor; 10; end
w.size = 5; w.valid?
p w.errors.full_messages`, "[\"Size must be greater than 10\"]\n"},
		// proc bound.
		{amModel + `Widget.validates :size, numericality: { less_than: ->(r) { 3 } }
w = Widget.new; w.size = 5; w.valid?
p w.errors.full_messages`, "[\"Size must be less than 3\"]\n"},
		// custom message.
		{amModel + `Widget.validates :size, numericality: { greater_than: 10, message: "too small" }
w = Widget.new; w.size = 5; w.valid?
p w.errors.full_messages`, "[\"Size too small\"]\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelConfirmationAcceptance covers confirmation and acceptance.
func TestActiveModelConfirmationAcceptance(t *testing.T) {
	cases := []struct{ src, want string }{
		{amModel + `Widget.validates :password, confirmation: true
w = Widget.new; w.password = "a"; w.password_confirmation = "b"; w.valid?
p w.errors.full_messages`, "[\"Password confirmation doesn't match Password\"]\n"},
		{amModel + `Widget.validates :password, confirmation: true
w = Widget.new; w.password = "a"; w.password_confirmation = "a"
p w.valid?`, "true\n"},
		// case-insensitive confirmation.
		{amModel + `Widget.validates :password, confirmation: { case_sensitive: false }
w = Widget.new; w.password = "AbC"; w.password_confirmation = "abc"
p w.valid?`, "true\n"},
		{amModel + `Widget.validates :password, confirmation: { message: "nope" }
w = Widget.new; w.password = "a"; w.password_confirmation = "b"; w.valid?
p w.errors.full_messages`, "[\"Password confirmation nope\"]\n"},
		{amModel + `Widget.validates :terms, acceptance: true
w = Widget.new; w.terms = false; w.valid?
p w.errors.full_messages`, "[\"Terms must be accepted\"]\n"},
		{amModel + `Widget.validates :terms, acceptance: true
w = Widget.new; w.terms = true
p w.valid?`, "true\n"},
		{amModel + `Widget.validates :terms, acceptance: { accept: "yes" }
w = Widget.new; w.terms = "no"; w.valid?
p w.errors.full_messages`, "[\"Terms must be accepted\"]\n"},
		{amModel + `Widget.validates :terms, acceptance: { accept: ["ok"], message: "agree please" }
w = Widget.new; w.terms = "no"; w.valid?
p w.errors.full_messages`, "[\"Terms agree please\"]\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelConditions covers if/unless/on/allow_nil/allow_blank and a
// literal :message.
func TestActiveModelConditions(t *testing.T) {
	cases := []struct{ src, want string }{
		// if: symbol
		{amModel + `Widget.validates :name, presence: true, if: :check?
w = Widget.new
def w.check?; true; end
w.valid?
p w.errors.empty?`, "false\n"},
		{amModel + `Widget.validates :name, presence: true, if: :check?
w = Widget.new
def w.check?; false; end
p w.valid?`, "true\n"},
		// unless: proc
		{amModel + `Widget.validates :name, presence: true, unless: ->(r) { true }
w = Widget.new
p w.valid?`, "true\n"},
		// if: array of conditions
		{amModel + `Widget.validates :name, presence: true, if: [:a?, :b?]
w = Widget.new
def w.a?; true; end
def w.b?; true; end
w.valid?
p w.errors.empty?`, "false\n"},
		// on: symbol context
		{amModel + `Widget.validates :name, presence: true, on: :create
w = Widget.new
p w.valid?(:update)`, "true\n"},
		{amModel + `Widget.validates :name, presence: true, on: :create
w = Widget.new
p w.valid?(:create)`, "false\n"},
		// on: array
		{amModel + `Widget.validates :name, presence: true, on: [:create, :update]
w = Widget.new
p w.valid?(:update)`, "false\n"},
		// allow_nil skips nil, catches blank string
		{amModel + `Widget.validates :name, length: { minimum: 3 }, allow_nil: true
w = Widget.new
p w.valid?`, "true\n"},
		// allow_blank skips blank
		{amModel + `Widget.validates :name, length: { minimum: 3 }, allow_blank: true
w = Widget.new; w.name = ""
p w.valid?`, "true\n"},
		// literal message string
		{amModel + `Widget.validates :name, presence: true, message: "required"
w = Widget.new; w.valid?
p w.errors.full_messages`, "[\"Name required\"]\n"},
		// symbol message key
		{amModel + `Widget.validates :name, presence: true, message: :blank
w = Widget.new; w.valid?
p w.errors.full_messages`, "[\"Name can't be blank\"]\n"},
		// proc message
		{amModel + `Widget.validates :name, presence: true, message: ->(r) { "nope" }
w = Widget.new; w.valid?
p w.errors.full_messages`, "[\"Name nope\"]\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelCustom covers validate blocks / methods, validates_each and
// validates_with.
func TestActiveModelCustom(t *testing.T) {
	cases := []struct{ src, want string }{
		// validate with a block
		{amModel + `Widget.validate do |rec|
  errors.add(:base, "bad") if name.nil?
end
w = Widget.new; w.valid?
p w.errors.full_messages`, "[\"bad\"]\n"},
		// validate with a method name
		{amModel + `Widget.validate :check
w = Widget.new
def w.check
  errors.add(:name, "is off")
end
w.valid?
p w.errors.full_messages`, "[\"Name is off\"]\n"},
		// validate block with a condition
		{amModel + `Widget.validate(if: ->(r) { false }) do |rec|
  errors.add(:base, "bad")
end
w = Widget.new
p w.valid?`, "true\n"},
		// validates_each
		{amModel + `Widget.validates_each :name, :code do |rec, attr, val|
  rec.errors.add(attr, "missing") if val.nil?
end
w = Widget.new; w.valid?
p w.errors.full_messages`, "[\"Name missing\", \"Code missing\"]\n"},
		// validates_with a Validator subclass
		{amModel + `class EvenValidator < ActiveModel::Validator
  def validate(record)
    record.errors.add(:base, "not even") unless record.size.to_i.even?
  end
end
Widget.validates_with EvenValidator
w = Widget.new; w.size = 3; w.valid?
p w.errors.full_messages`, "[\"not even\"]\n"},
		// validates_with passing options to the validator
		{amModel + `class OptValidator < ActiveModel::Validator
  def validate(record)
    record.errors.add(:base, options[:msg])
  end
end
Widget.validates_with OptValidator, msg: "boom"
w = Widget.new; w.valid?
p w.errors.full_messages`, "[\"boom\"]\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelErrorsAPI covers the ActiveModel::Errors collection surface.
func TestActiveModelErrorsAPI(t *testing.T) {
	setup := amModel + `Widget.validates :name, presence: true
w = Widget.new; w.valid?
`
	cases := []struct{ src, want string }{
		{setup + `p w.errors.size`, "1\n"},
		{setup + `p w.errors.count`, "1\n"},
		{setup + `p w.errors.any?`, "true\n"},
		{setup + `p w.errors.empty?`, "false\n"},
		{setup + `p w.errors.blank?`, "false\n"},
		{setup + `p w.errors.include?(:name)`, "true\n"},
		{setup + `p w.errors.has_key?(:name)`, "true\n"},
		{setup + `p w.errors.key?(:size)`, "false\n"},
		{setup + `p w.errors.attribute_names`, "[:name]\n"},
		{setup + `p w.errors[:name]`, "[\"can't be blank\"]\n"},
		{setup + `p w.errors.messages_for(:name)`, "[\"can't be blank\"]\n"},
		{setup + `p w.errors.full_messages_for(:name)`, "[\"Name can't be blank\"]\n"},
		{setup + `p w.errors.messages`, "{name: [\"can't be blank\"]}\n"},
		{setup + `p w.errors.details`, "{name: [{error: :blank}]}\n"},
		{setup + `p w.errors.added?(:name, :blank)`, "true\n"},
		{setup + `p w.errors.of_kind?(:name, :blank)`, "true\n"},
		{setup + `w.errors.each { |e| puts e.full_message }`, "Name can't be blank\n"},
		{setup + `p w.errors.where(:name).map(&:message)`, "[\"can't be blank\"]\n"},
		{setup + `w.errors.clear; p w.errors.empty?`, "true\n"},
		// add returns an Error; string type is a literal message.
		{amModel + `w = Widget.new
e = w.errors.add(:base, "boom")
p e.message`, "\"boom\"\n"},
		{amModel + `w = Widget.new
w.errors.add(:name, :too_short, count: 3)
p w.errors.full_messages`, "[\"Name is too short (minimum is 3 characters)\"]\n"},
		// added? with a literal string message.
		{amModel + `w = Widget.new
w.errors.add(:base, "boom")
p w.errors.added?(:base, "boom")`, "true\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelErrorAPI covers a single ActiveModel::Error.
func TestActiveModelErrorAPI(t *testing.T) {
	setup := amModel + `Widget.validates :name, presence: true
w = Widget.new; w.valid?
e = w.errors.where(:name).first
`
	cases := []struct{ src, want string }{
		{setup + `p e.class`, "ActiveModel::Error\n"},
		{setup + `p e.attribute`, ":name\n"},
		{setup + `p e.type`, ":blank\n"},
		{setup + `p e.message`, "\"can't be blank\"\n"},
		{setup + `p e.full_message`, "\"Name can't be blank\"\n"},
		{setup + `p e.details`, "{error: :blank}\n"},
		// options round-trip: a literal message option surfaces as a String.
		{amModel + `w = Widget.new
e = w.errors.add(:base, :invalid, message: "boom")
p e.options`, "{message: \"boom\"}\n"},
		// a count option surfaces as an Integer.
		{amModel + `w = Widget.new
e = w.errors.add(:name, :too_short, count: 3)
p e.options`, "{count: 3}\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelInheritanceAndRead covers subclass inheritance of validators and
// read_attribute_for_validation.
func TestActiveModelInheritanceAndRead(t *testing.T) {
	cases := []struct{ src, want string }{
		{amModel + `Widget.validates :name, presence: true
class Gadget < Widget
  validates :code, presence: true
end
g = Gadget.new; g.valid?
p g.errors.full_messages`, "[\"Name can't be blank\", \"Code can't be blank\"]\n"},
		{amModel + `w = Widget.new; w.name = "hi"
p w.read_attribute_for_validation(:name)`, "\"hi\"\n"},
		// read falls back to the instance variable when no reader exists.
		{`require "active_model"
class Thing
  include ActiveModel::Validations
  def set; @secret = 42; end
end
t = Thing.new; t.set
p t.read_attribute_for_validation(:secret)`, "42\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelErrorsRaise covers the ArgumentError / RegexpError branches of the
// DSL and Errors surface.
func TestActiveModelErrorsRaise(t *testing.T) {
	raises := []struct{ src, class string }{
		{amModel + `Widget.validates`, "ArgumentError"},
		{amModel + `Widget.validates :name, if: 5
Widget.new.valid?`, "ArgumentError"},
		{amModel + `Widget.validates :name, length: { in: 5 }`, "ArgumentError"},
		{amModel + `Widget.validates :name, length: 5`, "ArgumentError"},
		{amModel + `Widget.validates :name, inclusion: 5`, "ArgumentError"},
		{amModel + `Widget.validates :name, format: { with: "notaregexp" }`, "ArgumentError"},
		{amModel + `Widget.validates :name, format: { with: /(?=x)/ }`, "RegexpError"},
		{amModel + `Widget.validate`, "ArgumentError"},
		{amModel + `Widget.validates_each :name`, "ArgumentError"},
		{amModel + `Widget.validates_with`, "ArgumentError"},
		{amModel + `Widget.validates_with 5`, "ArgumentError"},
		{amModel + `w = Widget.new; w.errors.add`, "ArgumentError"},
		{amModel + `w = Widget.new; w.errors[]`, "ArgumentError"},
		{amModel + `w = Widget.new; w.errors.include?`, "ArgumentError"},
		{amModel + `w = Widget.new; w.errors.has_key?`, "ArgumentError"},
		{amModel + `w = Widget.new; w.errors.key?`, "ArgumentError"},
		{amModel + `w = Widget.new; w.errors.messages_for`, "ArgumentError"},
		{amModel + `w = Widget.new; w.errors.full_messages_for`, "ArgumentError"},
		{amModel + `w = Widget.new; w.errors.added?`, "ArgumentError"},
		{amModel + `w = Widget.new; w.errors.of_kind?`, "ArgumentError"},
		{amModel + `w = Widget.new; w.errors.where`, "ArgumentError"},
		{amModel + `w = Widget.new; w.errors.each`, "ArgumentError"},
		{amModel + `w = Widget.new; w.read_attribute_for_validation`, "ArgumentError"},
		{`require "active_model"; ActiveModel::Name.new`, "ArgumentError"},
		{`require "active_model"; ActiveModel::Naming.singular`, "ArgumentError"},
		{`require "active_model"; ActiveModel::Naming.uncountable?`, "ArgumentError"},
		// Validator / EachValidator base bodies raise NotImplementedError.
		{`require "active_model"
class V < ActiveModel::Validator; end
V.new.validate(nil)`, "NotImplementedError"},
		{`require "active_model"
class E < ActiveModel::EachValidator; end
E.new.validate_each(nil, nil, nil)`, "NotImplementedError"},
	}
	for _, c := range raises {
		cls, _ := evalErr(t, c.src)
		if cls != c.class {
			t.Errorf("src=%q raised %q, want %q", c.src, cls, c.class)
		}
	}
}

// TestActiveModelExtra covers the remaining option/conversion arms reachable from
// Ruby: bare validates (no options), non-integer length bound, exclusive ranges,
// the x/m regexp flags, symbol/hash/range attribute values, the ivar-fallback
// attribute read, string conditions, and the proc / literal error types.
func TestActiveModelExtra(t *testing.T) {
	cases := []struct{ src, want string }{
		// validates with no options hash is a no-op (amBuildOptions nil path).
		{amModel + `Widget.validates :name
w = Widget.new
p w.valid?`, "true\n"},
		// the instance #validate alias of #valid?.
		{amModel + `Widget.validates :name, presence: true
w = Widget.new
p w.validate`, "false\n"},
		// a non-integer length bound is ignored (amIntPtr nil path).
		{amModel + `Widget.validates :name, length: { minimum: 1.5 }
w = Widget.new; w.name = "a"
p w.valid?`, "true\n"},
		// exclusive length range (amIntRange Exclusive path): 2...4 caps at 3.
		{amModel + `Widget.validates :name, length: { in: 2...4 }
w = Widget.new; w.name = "abcd"; w.valid?
p w.errors.full_messages`, "[\"Name is too long (maximum is 3 characters)\"]\n"},
		// x (extended) regexp flag.
		{amModel + `Widget.validates :code, format: { with: /a b c/x }
w = Widget.new; w.code = "abc"
p w.valid?`, "true\n"},
		// m (dot-all) regexp flag.
		{amModel + `Widget.validates :code, format: { with: /a.c/m }
w = Widget.new; w.code = "a\nc"
p w.valid?`, "true\n"},
		// symbol attribute value against a symbol inclusion set.
		{amModel + `Widget.validates :status, inclusion: { in: [:a, :b] }
w = Widget.new; w.status = :a
p w.valid?`, "true\n"},
		// hash attribute value: an empty hash is blank.
		{amModel + `Widget.validates :name, presence: true
w = Widget.new; w.name = {}; w.valid?
p w.errors.empty?`, "false\n"},
		{amModel + `Widget.validates :name, presence: true
w = Widget.new; w.name = { a: 1 }
p w.valid?`, "true\n"},
		// a range attribute value (goOfRuby default arm) is present.
		{amModel + `Widget.validates :name, presence: true
w = Widget.new; w.name = (1..3)
p w.valid?`, "true\n"},
		// attribute read falls back to the instance variable during validation.
		{`require "active_model"
class Thing
  include ActiveModel::Validations
  validates :secret, presence: true
end
t = Thing.new; t.valid?
p t.errors.full_messages`, "[\"Secret can't be blank\"]\n"},
		// string if: condition names a method.
		{amModel + `Widget.validates :name, presence: true, if: "run?"
w = Widget.new
def w.run?; true; end
w.valid?
p w.errors.empty?`, "false\n"},
		// a non-string/symbol/proc :message falls back to the default message.
		{amModel + `Widget.validates :name, presence: true, message: 5
w = Widget.new; w.valid?
p w.errors.full_messages`, "[\"Name can't be blank\"]\n"},
		// a numericality bound of some other type (amBound default arm).
		{amModel + `Widget.validates :size, numericality: { greater_than: "0" }
w = Widget.new; w.size = 5
p w.valid?`, "true\n"},
		// a proc error type is resolved when added, keeping its symbol type.
		{amModel + `w = Widget.new
e = w.errors.add(:base, ->(rec) { :taken })
p e.type`, ":taken\n"},
		// a non-symbol/string error type renders through to_s.
		{amModel + `w = Widget.new
e = w.errors.add(:base, 5)
p e.message`, "\"5\"\n"},
		// a trailing non-hash argument is ignored (amOptsMap !ok arm).
		{amModel + `w = Widget.new
w.errors.add(:name, :blank, 5)
p w.errors.size`, "1\n"},
		// add with only an attribute defaults the type to :invalid (amErrorType nil arm).
		{amModel + `w = Widget.new
w.errors.add(:name)
p w.errors.full_messages`, "[\"Name is invalid\"]\n"},
		// handle inspects: Errors and Error have Go-side inspect/to_s.
		{amModel + `w = Widget.new
p w.errors`, "#<ActiveModel::Errors>\n"},
		{amModel + `Widget.validates :name, presence: true
w = Widget.new; w.valid?
p w.errors.where(:name).first`, "#<ActiveModel::Error attribute=name>\n"},
		{amModel + `Widget.validates :name, presence: true
w = Widget.new; w.valid?
puts w.errors.where(:name).first`, "Name can't be blank\n"},
		{`require "active_model"; n = ActiveModel::Name.new("Person"); p n`, "#<ActiveModel::Name Person>\n"},
		// extend on a non-class object is a no-op (extended !ok arm).
		{`require "active_model"
o = Object.new
o.extend(ActiveModel::Naming)
p o.respond_to?(:model_name)`, "false\n"},
	}
	for _, c := range cases {
		amCase(t, c.src, c.want)
	}
}

// TestActiveModelValidatorOptions covers ActiveModel::Validator#options default.
func TestActiveModelValidatorOptions(t *testing.T) {
	amCase(t, `require "active_model"
class V < ActiveModel::Validator; end
p V.new.options`, "{}\n")
	amCase(t, `require "active_model"
class V < ActiveModel::Validator; end
p V.new(foo: 1).options`, "{foo: 1}\n")
}
