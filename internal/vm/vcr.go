// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	vcr "github.com/go-ruby-vcr/vcr"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the VCR module (require "vcr"): the Ruby surface
//
//	VCR.configure { |c| c.cassette_library_dir = "..."; c.default_record_mode = :once }
//	VCR.use_cassette("name", record: :new_episodes) { ... Net::HTTP.get ... }
//
// on top of the pure-Go github.com/go-ruby-vcr/vcr cassette store. The library is
// the record/replay state machine and cassette (de)serializer; it deliberately
// performs no HTTP and reads no clock — those are seams the binding supplies:
//
//   - the filesystem seam (VCR.FS) → the real filesystem (OSFS), so cassettes
//     live on disk; a test injects an in-memory FS to stay hermetic;
//   - the clock seam (VCR.Now) → time.Now, stamping recorded_at;
//   - the HTTP doer seam → the bound Net::HTTP transport (vcr_bind.go): while a
//     use_cassette block is active, every outgoing request is routed through the
//     cassette (Cassette.Interact) which replays a recorded interaction or records
//     a new one by calling the real transport.
//
// The record/replay routing itself (the Net::HTTP interception + the value
// conversions between the Ruby Net::HTTP world and the library's Request/Response)
// lives in vcr_bind.go.

// registerVCR installs the VCR module and its VCR::Errors::UnhandledHTTPRequestError
// error class, and creates the per-VM configuration + cassette store (vcrConfig)
// with the os-backed filesystem seam and time.Now clock. It runs after
// registerNetHTTPTransport (whose real transport the record doer drives).
func (vm *VM) registerVCR() {
	vm.vcrConfig = &vcr.VCR{
		RecordMode: vcr.RecordOnce,
		FS:         vcr.OSFS(),
		Now:        func() time.Time { return time.Now() },
	}

	mod := newClass("VCR", nil)
	mod.isModule = true
	vm.consts["VCR"] = mod

	vm.registerVCRErrors(mod)
	vm.registerVCRConfiguration(mod)

	// VCR.configure { |config| ... } yields the configuration object whose setters
	// mutate vcrConfig (cassette_library_dir, default_record_mode, ...). With no
	// block it is a no-op returning the configuration, mirroring the gem.
	mod.smethods["configure"] = &Method{name: "configure", owner: mod,
		native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
			cfg := vm.vcrConfigObject(mod)
			if blk != nil {
				vm.callBlock(blk, []object.Value{cfg})
			}
			return cfg
		}}

	// VCR.use_cassette("name"[, options]) { ... } runs the block with the named
	// cassette active, so bound Net::HTTP requests inside it record/replay through
	// it. Returns the block's value.
	mod.smethods["use_cassette"] = &Method{name: "use_cassette", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.vcrUseCassette(args, blk)
		}}
}

// registerVCRErrors installs the VCR error tree used by the :none record mode: an
// unhandled request raises VCR::Errors::UnhandledHTTPRequestError, which a
// `rescue VCR::Errors::UnhandledHTTPRequestError` (or the parent StandardError)
// catches. The Errors module and the fully-qualified class name are both published
// so raise() resolves the class by its string key.
func (vm *VM) registerVCRErrors(mod *RClass) {
	errs := newClass("VCR::Errors", nil)
	errs.isModule = true
	mod.consts["Errors"] = errs
	vm.consts["VCR::Errors"] = errs

	base := newClass("VCR::Errors::Error", vm.consts["StandardError"].(*RClass))
	errs.consts["Error"] = base
	vm.consts["VCR::Errors::Error"] = base

	unhandled := newClass("VCR::Errors::UnhandledHTTPRequestError", base)
	errs.consts["UnhandledHTTPRequestError"] = unhandled
	vm.consts["VCR::Errors::UnhandledHTTPRequestError"] = unhandled
}

// registerVCRConfiguration installs the VCR::Configuration class whose accessors
// read and write the per-VM vcrConfig. VCR.configure yields one instance.
func (vm *VM) registerVCRConfiguration(mod *RClass) {
	cfg := newClass("VCR::Configuration", vm.cObject)
	mod.consts["Configuration"] = cfg
	vm.consts["VCR::Configuration"] = cfg

	cfg.define("cassette_library_dir=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.vcrConfig.CassetteDir = strArg(args[0])
		return args[0]
	})
	cfg.define("cassette_library_dir", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.vcrConfig.CassetteDir)
	})
	cfg.define("default_record_mode=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.vcrConfig.RecordMode = vcrParseMode(args[0])
		return args[0]
	})
	cfg.define("default_record_mode", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(vm.vcrConfig.RecordMode.String())
	})
	cfg.define("allow_playback_repeats=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.vcrConfig.AllowPlaybackRepeats = args[0].Truthy()
		return args[0]
	})
	cfg.define("allow_playback_repeats", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.vcrConfig.AllowPlaybackRepeats)
	})
}

// vcrConfigObject builds a fresh VCR::Configuration instance (a thin facade whose
// accessors mutate vcrConfig); one is yielded to each VCR.configure block.
func (vm *VM) vcrConfigObject(mod *RClass) object.Value {
	cls := mod.consts["Configuration"].(*RClass)
	return &RObject{class: cls, ivars: map[string]object.Value{}}
}

// vcrUseCassette runs the block with the named cassette active. It resolves the
// per-cassette options (record: mode, allow_playback_repeats:), opens the cassette
// through the library (which loads any on-disk recording and, on block exit,
// persists newly-recorded interactions), sets vcrCassette for the block's dynamic
// extent so bound Net::HTTP routes through it, and returns the block's value.
func (vm *VM) vcrUseCassette(args []object.Value, blk *Proc) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	if blk == nil {
		raise("ArgumentError", "no block given (yield)")
	}
	name := strArg(args[0])
	opts := vm.vcrCassetteOptions(args)

	var result object.Value
	// Nesting: restore whatever cassette (if any) was active around this block.
	prev := vm.vcrCassette
	err := vm.vcrConfig.UseCassette(name, opts, func(c *vcr.Cassette) error {
		vm.vcrCassette = c
		defer func() { vm.vcrCassette = prev }()
		result = vm.callBlock(blk, nil)
		return nil
	})
	if err != nil {
		// A cassette that fails to load (corrupt YAML) or persist surfaces here; an
		// unmatched request under :none raised inside the block instead (vcrRoute).
		raise("VCR::Errors::Error", "%s", err.Error())
	}
	return result
}

// vcrCassetteOptions builds the per-cassette overrides from an optional trailing
// options Hash: record: (a :once/:none/:new_episodes/:all Symbol or its String)
// and allow_playback_repeats: (a boolean). Absent keys inherit the VCR defaults.
func (vm *VM) vcrCassetteOptions(args []object.Value) vcr.CassetteOptions {
	var opts vcr.CassetteOptions
	if len(args) < 2 {
		return opts
	}
	h, ok := args[1].(*object.Hash)
	if !ok {
		return opts
	}
	if v, ok := h.Get(object.Symbol("record")); ok {
		mode := vcrParseMode(v)
		opts.RecordMode = &mode
	}
	if v, ok := h.Get(object.Symbol("allow_playback_repeats")); ok {
		b := v.Truthy()
		opts.AllowPlaybackRepeats = &b
	}
	return opts
}

// vcrParseMode maps a Ruby record-mode value (a :once/:none/:new_episodes/:all
// Symbol, or the equivalent String) to a vcr.RecordMode, raising ArgumentError on
// an unknown mode (mirroring the library's ParseRecordMode).
func vcrParseMode(v object.Value) vcr.RecordMode {
	var s string
	switch m := v.(type) {
	case object.Symbol:
		s = string(m)
	case *object.String:
		s = m.Str()
	default:
		raise("ArgumentError", "invalid record mode: %s", v.Inspect())
	}
	mode, err := vcr.ParseRecordMode(s)
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	return mode
}
