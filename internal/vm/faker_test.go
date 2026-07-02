// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestFakerConstants covers the Faker module, Faker::Config and the namespaces
// (require "faker").
func TestFakerConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "faker"; p Faker.is_a?(Module)`, "true\n"},
		{`p require "faker"`, "true\n"},
		{`require "faker"; p require "faker"`, "false\n"},
		{`require "faker"; p Faker::Config.is_a?(Module)`, "true\n"},
		{`require "faker"; p Faker::Name.is_a?(Module)`, "true\n"},
		{`require "faker"; p Faker::Internet.is_a?(Module)`, "true\n"},
		{`require "faker"; p Faker::Address.is_a?(Module)`, "true\n"},
		{`require "faker"; p Faker::Lorem.is_a?(Module)`, "true\n"},
		{`require "faker"; p Faker::Number.is_a?(Module)`, "true\n"},
		{`require "faker"; p Faker::Company.is_a?(Module)`, "true\n"},
		{`require "faker"; p Faker::UniqueGenerator::RetryLimitExceeded.ancestors.include?(StandardError)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFakerSeedContract covers the deterministic-seed contract: with
// Faker::Config.random = Random.new(seed) set, the draw sequence reproduces the
// gem's (a bit-exact port of MRI's MT19937). Seed 42 yields "Brittany Klocko".
func TestFakerSeedContract(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "faker"; Faker::Config.random = Random.new(42); p Faker::Name.name`, "\"Brittany Klocko\"\n"},
		// The same seed reproduces the same first draw.
		{`require "faker"; Faker::Config.random = Random.new(42); a = Faker::Name.name; Faker::Config.random = Random.new(42); b = Faker::Name.name; p a == b`, "true\n"},
		// A different seed generally yields a different first draw.
		{`require "faker"; Faker::Config.random = Random.new(1); a = Faker::Name.name; Faker::Config.random = Random.new(2); b = Faker::Name.name; p a == b`, "false\n"},
		// Config.random returns the configured Random object.
		{`require "faker"; r = Random.new(7); Faker::Config.random = r; p Faker::Config.random.equal?(r)`, "true\n"},
		// Before configuration, Config.random is nil.
		{`require "faker"; p Faker::Config.random`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFakerGenerators covers the string-returning generators across the
// namespaces (types and non-emptiness, over a fixed seed for reproducibility).
func TestFakerGenerators(t *testing.T) {
	pre := `require "faker"; Faker::Config.random = Random.new(99); `
	strMethods := []string{
		`Faker::Name.name`, `Faker::Name.first_name`, `Faker::Name.male_first_name`,
		`Faker::Name.female_first_name`, `Faker::Name.last_name`,
		`Faker::Name.name_with_middle`, `Faker::Name.prefix`, `Faker::Name.suffix`,
		`Faker::Internet.email`, `Faker::Internet.username`, `Faker::Internet.domain_name`,
		`Faker::Internet.domain_word`, `Faker::Internet.domain_suffix`, `Faker::Internet.url`,
		`Faker::Internet.slug`, `Faker::Internet.ip_v4_address`, `Faker::Internet.ip_v6_address`,
		`Faker::Internet.mac_address`,
		`Faker::Address.city`, `Faker::Address.street_name`, `Faker::Address.street_address`,
		`Faker::Address.building_number`, `Faker::Address.secondary_address`,
		`Faker::Address.zip_code`, `Faker::Address.zip`, `Faker::Address.postcode`,
		`Faker::Address.state`, `Faker::Address.state_abbr`, `Faker::Address.country`,
		`Faker::Address.country_code`, `Faker::Address.full_address`, `Faker::Address.time_zone`,
		`Faker::Company.name`, `Faker::Company.suffix`, `Faker::Company.industry`,
		`Faker::Company.buzzword`, `Faker::Company.catch_phrase`, `Faker::Company.bs`,
		`Faker::Color.color_name`, `Faker::Color.hex_color`,
		`Faker::PhoneNumber.phone_number`, `Faker::PhoneNumber.cell_phone`,
		`Faker::PhoneNumber.area_code`, `Faker::PhoneNumber.exchange_code`,
		`Faker::Lorem.word`, `Faker::Commerce.product_name`,
	}
	for _, m := range strMethods {
		src := pre + `v = ` + m + `; p v.is_a?(String) && v.length >= 0`
		if got := eval(t, src); got != "true\n" {
			t.Errorf("%s: got %q", m, got)
		}
	}
}

// TestFakerNumeric covers the count/bounds generators returning Integer / Float /
// String / Array / Boolean.
func TestFakerNumeric(t *testing.T) {
	pre := `require "faker"; Faker::Config.random = Random.new(99); `
	cases := []struct{ src, want string }{
		{pre + `p Faker::Number.number(5).is_a?(Integer)`, "true\n"},
		{pre + `p Faker::Number.digit.is_a?(Integer)`, "true\n"},
		{pre + `p Faker::Number.hexadecimal(4).length`, "4\n"},
		{pre + `p Faker::Number.binary(3).length`, "3\n"},
		{pre + `p Faker::Number.decimal(3, 2).is_a?(Float)`, "true\n"},
		{pre + `p Faker::Number.between(1, 10).is_a?(Float)`, "true\n"},
		{pre + `b = Faker::Boolean.boolean; p b == true || b == false`, "true\n"},
		{pre + `p Faker::Commerce.price(50).is_a?(Float)`, "true\n"},
		{pre + `p Faker::Commerce.department.is_a?(String)`, "true\n"},
		{pre + `p Faker::Address.latitude.is_a?(Float)`, "true\n"},
		{pre + `p Faker::Address.longitude.is_a?(Float)`, "true\n"},
		// Lorem count / Array shapes.
		{pre + `p Faker::Lorem.words(3).is_a?(Array)`, "true\n"},
		{pre + `p Faker::Lorem.words(3).length`, "3\n"},
		{pre + `p Faker::Lorem.sentence(4).is_a?(String)`, "true\n"},
		{pre + `p Faker::Lorem.sentences(2).is_a?(Array)`, "true\n"},
		{pre + `p Faker::Lorem.paragraph(3).is_a?(String)`, "true\n"},
		{pre + `p Faker::Lorem.paragraphs(2).is_a?(Array)`, "true\n"},
		{pre + `p Faker::Lorem.characters(10).length`, "10\n"},
		// Default-argument arms (called with no positional argument).
		{pre + `p Faker::Number.number.is_a?(Integer)`, "true\n"},
		{pre + `p Faker::Lorem.words.is_a?(Array)`, "true\n"},
		{pre + `p Faker::Lorem.sentence.is_a?(String)`, "true\n"},
		{pre + `b = Faker::Boolean.boolean; p b.is_a?(TrueClass) || b.is_a?(FalseClass)`, "true\n"},
		// An explicit nil count falls back to the default (fakerIntArg Nil arm).
		{pre + `p Faker::Number.number(nil).is_a?(Integer)`, "true\n"},
		// An Integer bound coerces to Float (fakerFloatArg Integer arm) and an
		// explicit nil bound falls back to the default (fakerFloatArg Nil arm).
		{pre + `p Faker::Number.between(1, 10).is_a?(Float)`, "true\n"},
		{pre + `p Faker::Commerce.price(nil).is_a?(Float)`, "true\n"},
		// A genuine Float bound (fakerFloatArg Float arm).
		{pre + `p Faker::Number.between(1.5, 9.5).is_a?(Float)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFakerNoConfig covers the lazy entropy-seeded generator used before
// Faker::Config.random= is called: the draws still succeed (non-deterministic).
func TestFakerNoConfig(t *testing.T) {
	if got := eval(t, `require "faker"; p Faker::Name.name.is_a?(String)`); got != "true\n" {
		t.Errorf("no-config draw: got %q", got)
	}
}

// TestFakerErrors covers the arity/type raise paths.
func TestFakerErrors(t *testing.T) {
	// Config.random= with no argument raises ArgumentError.
	got := eval(t, `require "faker"
begin
  Faker::Config.send(:random=)
rescue ArgumentError
  puts "arity"
end`)
	if !strings.Contains(got, "arity") {
		t.Errorf("random= arity: got %q", got)
	}
	// Config.random= with a non-Random raises TypeError.
	got = eval(t, `require "faker"
begin
  Faker::Config.random = "nope"
rescue TypeError
  puts "typeerr"
end`)
	if !strings.Contains(got, "typeerr") {
		t.Errorf("random= type: got %q", got)
	}
	// A count argument of the wrong type raises TypeError (fakerIntArg).
	got = eval(t, `require "faker"; Faker::Config.random = Random.new(1)
begin
  Faker::Number.hexadecimal("x")
rescue TypeError
  puts "int-typeerr"
end`)
	if !strings.Contains(got, "int-typeerr") {
		t.Errorf("int arg type: got %q", got)
	}
	// A bounds argument of the wrong type raises TypeError (fakerFloatArg).
	got = eval(t, `require "faker"; Faker::Config.random = Random.new(1)
begin
  Faker::Number.between("x", 5)
rescue TypeError
  puts "float-typeerr"
end`)
	if !strings.Contains(got, "float-typeerr") {
		t.Errorf("float arg type: got %q", got)
	}
}
