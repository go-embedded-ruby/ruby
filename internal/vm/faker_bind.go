// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	faker "github.com/go-ruby-faker/faker"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-faker/faker generator. The seeded,
// MRI-faithful generators and their data tables live in that library; rbgo only
// maps its Random ⇄ the library's Random for the deterministic-seed contract
// (Faker::Config.random = Random.new(seed)) and each Faker::<Namespace> call to a
// library method, so the faker-gem-faithful behaviour the Faker module relies on
// is preserved by construction.

// fakerGen is the seeded generator a Faker::<Namespace> method draws from,
// wrapping a *faker.Faker so faker.go never imports the library type directly.
type fakerGen struct{ f *faker.Faker }

func (g fakerGen) Name() *faker.NameGen               { return g.f.Name() }
func (g fakerGen) Internet() *faker.InternetGen       { return g.f.Internet() }
func (g fakerGen) Address() *faker.AddressGen         { return g.f.Address() }
func (g fakerGen) Company() *faker.CompanyGen         { return g.f.Company() }
func (g fakerGen) Color() *faker.ColorGen             { return g.f.Color() }
func (g fakerGen) PhoneNumber() *faker.PhoneNumberGen { return g.f.PhoneNumber() }
func (g fakerGen) Lorem() *faker.LoremGen             { return g.f.Lorem() }
func (g fakerGen) Number() *faker.NumberGen           { return g.f.Number() }
func (g fakerGen) Boolean() *faker.BooleanGen         { return g.f.Boolean() }
func (g fakerGen) Commerce() *faker.CommerceGen       { return g.f.Commerce() }

// fakerState holds the process-wide Faker generator and the Ruby Random object
// (if any) the caller configured through Faker::Config.random=, so reading the
// config back returns that same object.
type fakerState struct {
	f      *faker.Faker
	random object.Value // the Ruby Random.new(seed) set via Faker::Config.random=
}

// fakerSetRandom implements Faker::Config.random = r: it maps the rbgo Random's
// seed onto a freshly-seeded library generator, so the draw sequence reproduces
// the gem's for that seed (the deterministic-seed contract). A non-Random value
// raises TypeError, as the gem expects a Random.
func (vm *VM) fakerSetRandom(r object.Value) {
	ro, ok := r.(*RandomObj)
	if !ok {
		raise("TypeError", "Faker::Config.random must be a Random")
	}
	vm.fakerInst = &fakerState{f: faker.New(faker.NewRandom(ro.seed)), random: r}
}

// fakerConfiguredRandom returns the Ruby Random configured via
// Faker::Config.random=, or Ruby nil when none has been set (matching the gem,
// which returns nil until the caller assigns one).
func (vm *VM) fakerConfiguredRandom() object.Value {
	if vm.fakerInst == nil || vm.fakerInst.random == nil {
		return object.NilV
	}
	return vm.fakerInst.random
}

// fakerGen returns the seeded generator the Faker namespaces draw from,
// lazily creating an entropy-seeded one when Faker::Config.random= has not been
// called (matching the gem, which seeds from Random::DEFAULT until configured).
func (vm *VM) fakerGen() fakerGen {
	if vm.fakerInst == nil {
		vm.fakerInst = &fakerState{f: faker.New(faker.NewRandomEntropy())}
	}
	return fakerGen{f: vm.fakerInst.f}
}

// fakerNamespace defines Faker::<name> as a module nested under Faker and
// registered under its qualified name, returning it so the caller can attach its
// singleton generator methods.
func (vm *VM) fakerNamespace(mod *RClass, name string) *RClass {
	ns := newClass("Faker::"+name, nil)
	ns.isModule = true
	mod.consts[name] = ns
	vm.consts["Faker::"+name] = ns
	return ns
}

// fakerStr defines a zero-argument Faker::<Namespace> singleton method returning
// a String drawn from the shared seeded generator.
func fakerStr(ns *RClass, name string, get func(fakerGen) string) {
	ns.smethods[name] = &Method{name: name, owner: ns,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewString(get(vm.fakerGen()))
		}}
}

// fakerFloat defines a zero-argument Faker::<Namespace> singleton method
// returning a Float drawn from the shared seeded generator.
func fakerFloat(ns *RClass, name string, get func(fakerGen) float64) {
	ns.smethods[name] = &Method{name: name, owner: ns,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Float(get(vm.fakerGen()))
		}}
}

// fakerNS defines a Faker::<Namespace> singleton method whose body maps its
// arguments to a library call itself (the shapes that take a count / bounds or
// return an Array).
func fakerNS(ns *RClass, name string, fn func(*VM, []object.Value) object.Value) {
	ns.smethods[name] = &Method{name: name, owner: ns,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return fn(vm, args)
		}}
}

// fakerIntArg reads the nth positional argument as an int, returning def when the
// argument is absent. A non-integer argument raises TypeError.
func fakerIntArg(args []object.Value, n, def int) int {
	if n >= len(args) {
		return def
	}
	switch v := args[n].(type) {
	case object.Integer:
		return int(v)
	case object.Nil:
		return def
	}
	raise("TypeError", "no implicit conversion into Integer")
	return def
}

// fakerFloatArg reads the nth positional argument as a float, returning def when
// the argument is absent. An Integer coerces to Float; a non-numeric argument
// raises TypeError.
func fakerFloatArg(args []object.Value, n int, def float64) float64 {
	if n >= len(args) {
		return def
	}
	switch v := args[n].(type) {
	case object.Float:
		return float64(v)
	case object.Integer:
		return float64(v)
	case object.Nil:
		return def
	}
	raise("TypeError", "no implicit conversion into Float")
	return def
}
