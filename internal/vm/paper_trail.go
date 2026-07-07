// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strconv"
	stdtime "time"

	papertrail "github.com/go-ruby-paper-trail/paper-trail"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the PaperTrail model-versioning surface (require "paper_trail")
// over the github.com/go-ruby-paper-trail/paper-trail library. Everything
// deterministic — event detection, the object snapshot, the object_changes
// changeset, the only/ignore/skip/on filters, reify and the version queries —
// lives in the library; this binding provides its host seams:
//
//   - the attribute SNAPSHOT (before/after) is the tracked instance's Ruby
//     attribute map, read from its @ivars at the save/destroy callback points
//     (see paper_trail_bind.go);
//   - the version STORE is the reference in-memory Store (papertrail.MemoryStore),
//     shared by every model's Tracker so versions persist for the process;
//   - the CLOCK stamped onto Version#created_at is time.Now (injectable for
//     deterministic tests via vm.paperTrail.clock);
//   - the WHODUNNIT and the enabled toggle come from the shared RequestContext
//     exposed as PaperTrail.request (PaperTrail.request.whodunnit = user).

// paperTrailState is the per-VM PaperTrail state: the shared seams (store, request,
// clock), the per-model-class Tracker cache (each carries that model's
// has_paper_trail Config), and the per-instance record-keeping (its item id and
// last recorded attribute snapshot, so a save infers create vs update and a
// destroy has a before-image).
type paperTrailState struct {
	store      papertrail.Store
	request    *papertrail.RequestContext
	clock      papertrail.Clock
	serializer papertrail.Serializer
	trackers   map[*RClass]*papertrail.Tracker
	configs    map[*RClass]papertrail.Config
	states     map[*RObject]*ptInstanceState
	nextID     int64
}

// ptInstanceState tracks one versioned model instance across its lifecycle: the
// stable item id assigned on first record, and the last attribute snapshot
// recorded (nil until the first create, and reset to nil after a destroy so a
// re-save is a fresh create).
type ptInstanceState struct {
	itemID string
	last   map[string]any
}

// PTVersion is the Ruby wrapper around a papertrail.Version bound to the model
// class it belongs to (so #reify can rebuild an instance and #previous_version /
// #next_version can resolve neighbours through that class's Tracker).
type PTVersion struct {
	v   papertrail.Version
	cls *RClass
}

func (v *PTVersion) ToS() string     { return "#<PaperTrail::Version " + v.v.Event + ">" }
func (v *PTVersion) Inspect() string { return v.ToS() }
func (v *PTVersion) Truthy() bool    { return true }

// PTProxy is the Ruby wrapper behind model#paper_trail — the per-instance query
// facade (#versions / #previous_version / #next_version / #live?).
type PTProxy struct {
	inst *RObject
	cls  *RClass
}

func (p *PTProxy) ToS() string     { return "#<PaperTrail::RecordTrail>" }
func (p *PTProxy) Inspect() string { return p.ToS() }
func (p *PTProxy) Truthy() bool    { return true }

// PTRequest is the Ruby wrapper behind PaperTrail.request — the shared
// RequestContext carrying the whodunnit and the enabled toggle.
type PTRequest struct{ rc *papertrail.RequestContext }

func (r *PTRequest) ToS() string     { return "#<PaperTrail::Request>" }
func (r *PTRequest) Inspect() string { return r.ToS() }
func (r *PTRequest) Truthy() bool    { return true }

// registerPaperTrail installs the PaperTrail module and its Version / RecordTrail
// / Request surface (require "paper_trail"), and the has_paper_trail class macro
// (paper_trail_bind.go). The versioning engine is the
// github.com/go-ruby-paper-trail/paper-trail library; this binding wires its
// snapshot / store / clock / whodunnit seams onto rbgo's object model.
func (vm *VM) registerPaperTrail() {
	vm.paperTrail = &paperTrailState{
		store:      papertrail.NewMemoryStore(),
		request:    papertrail.NewRequest(),
		clock:      papertrail.ClockFunc(func() stdtime.Time { return vm.ptNow() }),
		serializer: papertrail.JSONSerializer{},
		trackers:   map[*RClass]*papertrail.Tracker{},
		states:     map[*RObject]*ptInstanceState{},
	}

	mod := newClass("PaperTrail", nil)
	mod.isModule = true
	vm.consts["PaperTrail"] = mod

	// PaperTrail::Error < StandardError — the umbrella for a serializer / store
	// failure surfaced to Ruby.
	errCls := newClass("PaperTrail::Error", vm.consts["StandardError"].(*RClass))
	mod.consts["Error"] = errCls
	vm.consts["PaperTrail::Error"] = errCls

	vm.registerPaperTrailModule(mod)
	vm.registerPaperTrailVersion(mod)
	vm.registerPaperTrailProxy(mod)
	vm.registerPaperTrailRequest(mod)
	vm.installHasPaperTrail()
}

// ptNow reads the clock seam's wall time. It is a package-level indirection so a
// test can freeze time; production returns time.Now.
var ptNowFunc = func() stdtime.Time { return stdtime.Now() }

func (vm *VM) ptNow() stdtime.Time { return ptNowFunc() }

// registerPaperTrailModule installs the PaperTrail module methods: PaperTrail
// .request (the shared RequestContext facade) and the whodunnit / enabled
// convenience delegators.
func (vm *VM) registerPaperTrailModule(mod *RClass) {
	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}
	sm("request", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &PTRequest{rc: vm.paperTrail.request}
	})
	sm("whodunnit", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return ptWhodunnitValue(vm.paperTrail.request.Whodunnit)
	})
	sm("whodunnit=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.paperTrail.request.Whodunnit = ptWhodunnitString(args)
		return argFirst(args)
	})
	sm("enabled?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.paperTrail.request.Enabled)
	})
	sm("enabled=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.paperTrail.request.Enabled = argFirst(args).Truthy()
		return argFirst(args)
	})
}

// registerPaperTrailVersion installs PaperTrail::Version — the audit record's
// readers, the deserialized #object / #object_changes, #reify (rebuilds the model
// instance) and the #previous_version / #next_version neighbours.
func (vm *VM) registerPaperTrailVersion(mod *RClass) {
	cls := newClass("PaperTrail::Version", vm.cObject)
	mod.consts["Version"] = cls
	vm.consts["PaperTrail::Version"] = cls

	self := func(v object.Value) *PTVersion { return v.(*PTVersion) }

	cls.define("item_type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).v.ItemType)
	})
	cls.define("item_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).v.ItemID)
	})
	cls.define("event", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).v.Event)
	})
	cls.define("whodunnit", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ptWhodunnitValue(self(v).v.Whodunnit)
	})
	cls.define("created_at", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return goTimeToRuby(self(v).v.CreatedAt)
	})
	cls.define("object", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		attrs, err := vm.ptSerializer().LoadObject(self(v).v.Object)
		if err != nil {
			raise("PaperTrail::Error", "%s", err.Error())
		}
		return ptAttrsHash(attrs)
	})
	cls.define("object_changes", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		changes, err := vm.ptSerializer().LoadChanges(self(v).v.ObjectChanges)
		if err != nil {
			raise("PaperTrail::Error", "%s", err.Error())
		}
		return ptChangesHash(changes)
	})
	cls.define("reify", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		pv := self(v)
		attrs, err := vm.ptTrackerFor(pv.cls).Reify(pv.v)
		if err != nil {
			raise("PaperTrail::Error", "%s", err.Error())
		}
		return vm.ptReifyInstance(pv.cls, attrs)
	})
	cls.define("previous_version", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		pv := self(v)
		prev, err := vm.ptTrackerFor(pv.cls).PreviousVersion(pv.v)
		if err != nil {
			raise("PaperTrail::Error", "%s", err.Error())
		}
		return ptVersionValue(prev, pv.cls)
	})
	cls.define("next_version", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		pv := self(v)
		next, err := vm.ptTrackerFor(pv.cls).NextVersion(pv.v)
		if err != nil {
			raise("PaperTrail::Error", "%s", err.Error())
		}
		return ptVersionValue(next, pv.cls)
	})
}

// registerPaperTrailProxy installs PaperTrail::RecordTrail — the model#paper_trail
// facade: #versions, #live?, and the #previous_version / #next_version reified
// neighbours for the instance's current (live) state.
func (vm *VM) registerPaperTrailProxy(mod *RClass) {
	cls := newClass("PaperTrail::RecordTrail", vm.cObject)
	mod.consts["RecordTrail"] = cls
	vm.consts["PaperTrail::RecordTrail"] = cls

	self := func(v object.Value) *PTProxy { return v.(*PTProxy) }

	cls.define("versions", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		p := self(v)
		return vm.ptVersionsFor(p.inst, p.cls)
	})
	cls.define("live?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		p := self(v)
		st, ok := vm.paperTrail.states[p.inst]
		if !ok {
			return object.True
		}
		live, err := vm.ptTrackerFor(p.cls).Live(st.itemID)
		if err != nil {
			raise("PaperTrail::Error", "%s", err.Error())
		}
		return object.Bool(live)
	})
	cls.define("previous_version", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		p := self(v)
		vs := vm.ptVersionSlice(p.inst, p.cls)
		if len(vs) == 0 {
			return object.NilV
		}
		attrs, err := vm.ptTrackerFor(p.cls).Reify(vs[len(vs)-1])
		if err != nil {
			raise("PaperTrail::Error", "%s", err.Error())
		}
		return vm.ptReifyInstance(p.cls, attrs)
	})
	cls.define("next_version", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		// A live record has no succeeding version (it is already its own latest
		// state), so #next_version is nil — matching the gem.
		return object.NilV
	})
}

// registerPaperTrailRequest installs PaperTrail::Request — the whodunnit / enabled
// accessors over the shared RequestContext.
func (vm *VM) registerPaperTrailRequest(mod *RClass) {
	cls := newClass("PaperTrail::Request", vm.cObject)
	mod.consts["Request"] = cls
	vm.consts["PaperTrail::Request"] = cls

	self := func(v object.Value) *papertrail.RequestContext { return v.(*PTRequest).rc }

	cls.define("whodunnit", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return ptWhodunnitValue(self(v).Whodunnit)
	})
	cls.define("whodunnit=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).Whodunnit = ptWhodunnitString(args)
		return argFirst(args)
	})
	cls.define("enabled?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Enabled)
	})
	cls.define("enabled=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).Enabled = argFirst(args).Truthy()
		return argFirst(args)
	})
}

// ptTrackerFor returns the Tracker for a model class, building it on first use
// against the shared store / request / clock seams and the class's has_paper_trail
// Config (or the default all-events Config when the macro carried no options).
func (vm *VM) ptTrackerFor(cls *RClass) *papertrail.Tracker {
	if t, ok := vm.paperTrail.trackers[cls]; ok {
		return t
	}
	t := papertrail.New(papertrail.Options{
		ItemType:   cls.name,
		Config:     vm.ptConfigFor(cls),
		Store:      vm.paperTrail.store,
		Clock:      vm.paperTrail.clock,
		Serializer: vm.paperTrail.serializer,
		Request:    vm.paperTrail.request,
	})
	vm.paperTrail.trackers[cls] = t
	return t
}

// ptSerializer returns the process serializer seam used to decode a version's
// stored object / changes (the default JSONSerializer; injectable for tests).
func (vm *VM) ptSerializer() papertrail.Serializer { return vm.paperTrail.serializer }

// ptReifyInstance rebuilds a model instance of cls from a reified attribute map,
// setting each attribute as an @ivar so the model's readers see the past state.
func (vm *VM) ptReifyInstance(cls *RClass, attrs map[string]any) object.Value {
	obj := &RObject{class: cls, ivars: map[string]object.Value{}}
	for k, val := range attrs {
		obj.ivars["@"+k] = rubyOfGo(val)
	}
	return obj
}

// ptVersionsFor returns the Ruby Array of PaperTrail::Version for an instance
// (empty when the instance was never recorded).
func (vm *VM) ptVersionsFor(inst *RObject, cls *RClass) object.Value {
	vs := vm.ptVersionSlice(inst, cls)
	arr := object.NewArrayFromSlice(make([]object.Value, len(vs)))
	for i := range vs {
		arr.Elems[i] = &PTVersion{v: vs[i], cls: cls}
	}
	return arr
}

// ptVersionSlice returns the stored versions for an instance, oldest-first, or an
// empty slice when it has no item id yet (never saved).
func (vm *VM) ptVersionSlice(inst *RObject, cls *RClass) []papertrail.Version {
	st, ok := vm.paperTrail.states[inst]
	if !ok {
		return nil
	}
	vs, err := vm.ptTrackerFor(cls).Versions(st.itemID)
	if err != nil {
		raise("PaperTrail::Error", "%s", err.Error())
	}
	return vs
}

// ptVersionValue wraps a library version pointer (or nil) as its Ruby value.
func ptVersionValue(v *papertrail.Version, cls *RClass) object.Value {
	if v == nil {
		return object.NilV
	}
	return &PTVersion{v: *v, cls: cls}
}

// ptWhodunnitValue renders a stored whodunnit as a Ruby String, or nil when unset.
func ptWhodunnitValue(s string) object.Value {
	if s == "" {
		return object.NilV
	}
	return object.NewString(s)
}

// ptWhodunnitString reads a whodunnit assignment argument into its string form
// (nil clears it); a non-string actor is stringified as the gem does.
func ptWhodunnitString(args []object.Value) string {
	v := argFirst(args)
	if !v.Truthy() {
		return ""
	}
	return arStr(v)
}

// ptAttrsHash renders a reified/loaded attribute map as a Ruby Hash keyed by the
// attribute name (a String), or nil for an absent snapshot (a create's object).
func ptAttrsHash(attrs map[string]any) object.Value {
	if attrs == nil {
		return object.NilV
	}
	h := object.NewHash()
	for k, v := range attrs {
		h.Set(object.NewString(k), rubyOfGo(v))
	}
	return h
}

// ptChangesHash renders an object_changes map as a Ruby Hash of attribute =>
// [old, new], or nil when there is no changeset (a destroy).
func ptChangesHash(changes map[string]papertrail.Change) object.Value {
	if changes == nil {
		return object.NilV
	}
	h := object.NewHash()
	for k, c := range changes {
		h.Set(object.NewString(k), object.NewArray(rubyOfGo(c.Old), rubyOfGo(c.New)))
	}
	return h
}

// argFirst returns the first argument or nil when none was given.
func argFirst(args []object.Value) object.Value {
	if len(args) == 0 {
		return object.NilV
	}
	return args[0]
}

// ptItemID formats a model id attribute as the Version item id string.
func ptItemID(id any) string {
	switch n := id.(type) {
	case string:
		return n
	case int64:
		return strconv.FormatInt(n, 10)
	}
	return ""
}
