// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerFaker installs the Faker module (require "faker"): the Faker::Config
// seeding contract (Faker::Config.random = Random.new(seed)) plus the generator
// namespaces Faker::Name / Internet / Address / Lorem / Number / Company / Date /
// Time / Boolean / Color / Commerce / PhoneNumber. The seeded generators and
// their MRI-faithful data tables live in the github.com/go-ruby-faker/faker
// library; this module is the thin wiring that maps rbgo's Random ⇄ the library's
// Random for the deterministic-seed contract and each generator call to a library
// method (see faker_bind.go).
func (vm *VM) registerFaker() {
	mod := newClass("Faker", nil)
	mod.isModule = true
	vm.consts["Faker"] = object.Wrap(mod)

	// Faker::Config.random = Random.new(seed) sets the generator; reading it back
	// returns the configured Random (or nil before it is set). This is the gem's
	// deterministic-seed contract.
	cfg := newClass("Faker::Config", nil)
	cfg.isModule = true
	mod.consts["Config"] = object.Wrap(cfg)
	vm.consts["Faker::Config"] = object.Wrap(cfg)
	cfg.smethods["random="] = &Method{name: "random=", owner: cfg,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			vm.fakerSetRandom(args[0])
			return args[0]
		}}
	cfg.smethods["random"] = &Method{name: "random", owner: cfg,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return vm.fakerConfiguredRandom()
		}}

	// RetryLimitExceeded < StandardError mirrors the gem's unique-generator
	// exhaustion error (raised by Faker::…unique when the retry budget is spent).
	std := object.Kind[*RClass](vm.consts["StandardError"])
	uniqErr := newClass("Faker::UniqueGenerator::RetryLimitExceeded", std)
	mod.consts["UniqueGenerator"] = object.Wrap(func() *RClass {
		ug := newClass("Faker::UniqueGenerator", nil)
		ug.isModule = true
		ug.consts["RetryLimitExceeded"] = object.Wrap(uniqErr)
		return ug
	}())
	vm.consts["Faker::UniqueGenerator::RetryLimitExceeded"] = object.Wrap(uniqErr)

	vm.registerFakerGenerators(mod)
}

// registerFakerGenerators installs each Faker::<Namespace> as a module whose
// singleton methods draw from the shared, seeded generator. A method with a
// string result (the common case) is wired with fakerStr; the remaining shapes
// (Array, Integer, Float, Bool) have their own small adapters.
func (vm *VM) registerFakerGenerators(mod *RClass) {
	// Name.
	name := vm.fakerNamespace(mod, "Name")
	fakerStr(name, "name", func(f fakerGen) string { return f.Name().Name() })
	fakerStr(name, "first_name", func(f fakerGen) string { return f.Name().FirstName() })
	fakerStr(name, "male_first_name", func(f fakerGen) string { return f.Name().MaleFirstName() })
	fakerStr(name, "female_first_name", func(f fakerGen) string { return f.Name().FemaleFirstName() })
	fakerStr(name, "last_name", func(f fakerGen) string { return f.Name().LastName() })
	fakerStr(name, "name_with_middle", func(f fakerGen) string { return f.Name().NameWithMiddle() })
	fakerStr(name, "prefix", func(f fakerGen) string { return f.Name().Prefix() })
	fakerStr(name, "suffix", func(f fakerGen) string { return f.Name().Suffix() })

	// Internet.
	inet := vm.fakerNamespace(mod, "Internet")
	fakerStr(inet, "email", func(f fakerGen) string { return f.Internet().Email() })
	fakerStr(inet, "username", func(f fakerGen) string { return f.Internet().Username() })
	fakerStr(inet, "domain_name", func(f fakerGen) string { return f.Internet().DomainName() })
	fakerStr(inet, "domain_word", func(f fakerGen) string { return f.Internet().DomainWord() })
	fakerStr(inet, "domain_suffix", func(f fakerGen) string { return f.Internet().DomainSuffix() })
	fakerStr(inet, "url", func(f fakerGen) string { return f.Internet().URL() })
	fakerStr(inet, "slug", func(f fakerGen) string { return f.Internet().Slug() })
	fakerStr(inet, "ip_v4_address", func(f fakerGen) string { return f.Internet().IPv4Address() })
	fakerStr(inet, "ip_v6_address", func(f fakerGen) string { return f.Internet().IPv6Address() })
	fakerStr(inet, "mac_address", func(f fakerGen) string { return f.Internet().MacAddress("") })

	// Address.
	addr := vm.fakerNamespace(mod, "Address")
	fakerStr(addr, "city", func(f fakerGen) string { return f.Address().City() })
	fakerStr(addr, "street_name", func(f fakerGen) string { return f.Address().StreetName() })
	fakerStr(addr, "street_address", func(f fakerGen) string { return f.Address().StreetAddress() })
	fakerStr(addr, "building_number", func(f fakerGen) string { return f.Address().BuildingNumber() })
	fakerStr(addr, "secondary_address", func(f fakerGen) string { return f.Address().SecondaryAddress() })
	fakerStr(addr, "zip_code", func(f fakerGen) string { return f.Address().ZipCode() })
	fakerStr(addr, "zip", func(f fakerGen) string { return f.Address().Zip() })
	fakerStr(addr, "postcode", func(f fakerGen) string { return f.Address().Postcode() })
	fakerStr(addr, "state", func(f fakerGen) string { return f.Address().State() })
	fakerStr(addr, "state_abbr", func(f fakerGen) string { return f.Address().StateAbbr() })
	fakerStr(addr, "country", func(f fakerGen) string { return f.Address().Country() })
	fakerStr(addr, "country_code", func(f fakerGen) string { return f.Address().CountryCode() })
	fakerStr(addr, "full_address", func(f fakerGen) string { return f.Address().FullAddress() })
	fakerStr(addr, "time_zone", func(f fakerGen) string { return f.Address().TimeZone() })
	fakerFloat(addr, "latitude", func(f fakerGen) float64 { return f.Address().Latitude() })
	fakerFloat(addr, "longitude", func(f fakerGen) float64 { return f.Address().Longitude() })

	// Company.
	comp := vm.fakerNamespace(mod, "Company")
	fakerStr(comp, "name", func(f fakerGen) string { return f.Company().Name() })
	fakerStr(comp, "suffix", func(f fakerGen) string { return f.Company().Suffix() })
	fakerStr(comp, "industry", func(f fakerGen) string { return f.Company().Industry() })
	fakerStr(comp, "buzzword", func(f fakerGen) string { return f.Company().Buzzword() })
	fakerStr(comp, "catch_phrase", func(f fakerGen) string { return f.Company().CatchPhrase() })
	fakerStr(comp, "bs", func(f fakerGen) string { return f.Company().BS() })

	// Color.
	color := vm.fakerNamespace(mod, "Color")
	fakerStr(color, "color_name", func(f fakerGen) string { return f.Color().ColorName() })
	fakerStr(color, "hex_color", func(f fakerGen) string { return f.Color().HexColor() })

	// PhoneNumber.
	phone := vm.fakerNamespace(mod, "PhoneNumber")
	fakerStr(phone, "phone_number", func(f fakerGen) string { return f.PhoneNumber().PhoneNumber() })
	fakerStr(phone, "cell_phone", func(f fakerGen) string { return f.PhoneNumber().CellPhone() })
	fakerStr(phone, "area_code", func(f fakerGen) string { return f.PhoneNumber().AreaCode() })
	fakerStr(phone, "exchange_code", func(f fakerGen) string { return f.PhoneNumber().ExchangeCode() })

	vm.registerFakerLorem(mod)
	vm.registerFakerNumber(mod)
	vm.registerFakerBooleanCommerce(mod)
}

// registerFakerLorem installs Faker::Lorem, whose sentence/paragraph helpers take
// a count argument and whose *s variants return an Array.
func (vm *VM) registerFakerLorem(mod *RClass) {
	lorem := vm.fakerNamespace(mod, "Lorem")
	fakerStr(lorem, "word", func(f fakerGen) string { return f.Lorem().Word() })
	fakerNS(lorem, "words", func(vm *VM, args []object.Value) object.Value {
		return strSliceToArray(vm.fakerGen().Lorem().Words(fakerIntArg(args, 0, 3), false))
	})
	fakerNS(lorem, "sentence", func(vm *VM, args []object.Value) object.Value {
		return object.Wrap(object.NewString(vm.fakerGen().Lorem().Sentence(fakerIntArg(args, 0, 4), false)))
	})
	fakerNS(lorem, "sentences", func(vm *VM, args []object.Value) object.Value {
		return strSliceToArray(vm.fakerGen().Lorem().Sentences(fakerIntArg(args, 0, 3), false))
	})
	fakerNS(lorem, "paragraph", func(vm *VM, args []object.Value) object.Value {
		return object.Wrap(object.NewString(vm.fakerGen().Lorem().Paragraph(fakerIntArg(args, 0, 3), false)))
	})
	fakerNS(lorem, "paragraphs", func(vm *VM, args []object.Value) object.Value {
		return strSliceToArray(vm.fakerGen().Lorem().Paragraphs(fakerIntArg(args, 0, 3), false))
	})
	fakerNS(lorem, "characters", func(vm *VM, args []object.Value) object.Value {
		return object.Wrap(object.NewString(vm.fakerGen().Lorem().Characters(fakerIntArg(args, 0, 255))))
	})
}

// registerFakerNumber installs Faker::Number, whose helpers take integer digit
// counts or numeric bounds and return an Integer / Float / String.
func (vm *VM) registerFakerNumber(mod *RClass) {
	num := vm.fakerNamespace(mod, "Number")
	fakerNS(num, "number", func(vm *VM, args []object.Value) object.Value {
		return object.IntValue(vm.fakerGen().Number().Number(fakerIntArg(args, 0, 10)))
	})
	fakerNS(num, "digit", func(vm *VM, _ []object.Value) object.Value {
		return object.IntValue(int64(vm.fakerGen().Number().Digit()))
	})
	fakerNS(num, "hexadecimal", func(vm *VM, args []object.Value) object.Value {
		return object.Wrap(object.NewString(vm.fakerGen().Number().Hexadecimal(fakerIntArg(args, 0, 6))))
	})
	fakerNS(num, "binary", func(vm *VM, args []object.Value) object.Value {
		return object.Wrap(object.NewString(vm.fakerGen().Number().Binary(fakerIntArg(args, 0, 4))))
	})
	fakerNS(num, "decimal", func(vm *VM, args []object.Value) object.Value {
		return object.FloatValue(float64(object.Float(vm.fakerGen().Number().Decimal(fakerIntArg(args, 0, 5), fakerIntArg(args, 1, 2)))))
	})
	fakerNS(num, "between", func(vm *VM, args []object.Value) object.Value {
		return object.FloatValue(float64(object.Float(vm.fakerGen().Number().Between(fakerFloatArg(args, 0, 1), fakerFloatArg(args, 1, 5000)))))
	})
}

// registerFakerBooleanCommerce installs Faker::Boolean, Faker::Commerce and the
// Date/Time namespaces (loadable shells over the seeded date/time generators).
func (vm *VM) registerFakerBooleanCommerce(mod *RClass) {
	boolean := vm.fakerNamespace(mod, "Boolean")
	fakerNS(boolean, "boolean", func(vm *VM, args []object.Value) object.Value {
		return object.BoolValue(bool(object.Bool(vm.fakerGen().Boolean().Boolean(fakerFloatArg(args, 0, 0.5)))))
	})

	commerce := vm.fakerNamespace(mod, "Commerce")
	fakerStr(commerce, "product_name", func(f fakerGen) string { return f.Commerce().ProductName() })
	fakerNS(commerce, "price", func(vm *VM, args []object.Value) object.Value {
		return object.FloatValue(float64(object.Float(vm.fakerGen().Commerce().Price(fakerFloatArg(args, 0, 100)))))
	})
	fakerNS(commerce, "department", func(vm *VM, args []object.Value) object.Value {
		return object.Wrap(object.NewString(vm.fakerGen().Commerce().Department(fakerIntArg(args, 0, 3), false)))
	})
}
