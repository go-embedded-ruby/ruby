// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	resourceapi "github.com/go-ruby-puppet-resource-api/puppet-resource-api"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// praHashArg reads the single definition/resource Hash argument, raising
// ArgumentError when it is missing or not a Hash.
func praHashArg(args []object.Value) *object.Hash {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	h, ok := args[0].(*object.Hash)
	if !ok {
		raise("ArgumentError", "expected a Hash")
	}
	return h
}

// praGet reads a definition key by symbol or string.
func praGet(h *object.Hash, key string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}

// praStr reads an optional string key, defaulting to "".
func praStr(h *object.Hash, key string) string {
	if v, ok := praGet(h, key); ok {
		return v.ToS()
	}
	return ""
}

// praBuildDefinition turns a register_type definition Hash into a
// resourceapi.Definition: name, desc, the attributes map (type / desc / default
// / behaviour) and the feature list. Schema errors surface from Register.
func praBuildDefinition(args []object.Value) resourceapi.Definition {
	h := praHashArg(args)
	def := resourceapi.Definition{
		Name:       praStr(h, "name"),
		Desc:       praStr(h, "desc"),
		Attributes: map[string]resourceapi.Attribute{},
	}
	if av, ok := praGet(h, "attributes"); ok {
		if attrs, ok := av.(*object.Hash); ok {
			for _, k := range attrs.Keys {
				spec, _ := attrs.Get(k)
				def.Attributes[k.ToS()] = praBuildAttribute(spec)
			}
		}
	}
	if fv, ok := praGet(h, "features"); ok {
		if arr, ok := fv.(*object.Array); ok {
			def.Features = arrToStrings(arr)
		}
	}
	return def
}

// praBuildAttribute turns one attribute-spec Hash into a resourceapi.Attribute.
func praBuildAttribute(spec object.Value) resourceapi.Attribute {
	h, ok := spec.(*object.Hash)
	if !ok {
		return resourceapi.Attribute{}
	}
	attr := resourceapi.Attribute{
		Type:      praStr(h, "type"),
		Desc:      praStr(h, "desc"),
		Behaviour: praBehaviour(praStr(h, "behaviour")),
	}
	if d, ok := praGet(h, "default"); ok {
		attr.Default = rubyToGoValue(d)
		attr.HasDefault = true
	}
	return attr
}

// praBehaviour maps the gem's :behaviour symbol onto a resourceapi.Behaviour;
// an unset or unrecognised value is the default Property.
func praBehaviour(s string) resourceapi.Behaviour {
	switch s {
	case "namevar":
		return resourceapi.Namevar
	case "read_only":
		return resourceapi.ReadOnly
	case "parameter":
		return resourceapi.Parameter
	case "init_only":
		return resourceapi.InitOnly
	default:
		return resourceapi.Property
	}
}

// registerPraType installs the Puppet::ResourceApi::TypeDefinition instance
// surface.
func (vm *VM) registerPraType(cls *RClass) {
	self := func(v object.Value) *resourceapi.Type { return v.(*PraType).t }

	cls.define("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name())
	})
	cls.define("attributes", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return strSliceToRuby(self(v).AttributeNames())
	})
	cls.define("namevars", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return strSliceToRuby(self(v).Namevars())
	})
	feature := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Bool(self(v).HasFeature(args[0].ToS()))
	}
	cls.define("feature?", feature)
	cls.define("has_feature?", feature)

	// validate(resource) — validate and canonicalise a desired-state Hash,
	// returning the completed resource; Puppet::ResourceError on a violation.
	cls.define("validate", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := self(v).Validate(praResource(args))
		if err != nil {
			raise("Puppet::ResourceError", "%s", err.Error())
		}
		return facterValueToRuby(map[string]any(out))
	})

	// title(resource) — the resource title; Puppet::ResourceError on failure.
	cls.define("title", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		title, err := self(v).Title(praResource(args))
		if err != nil {
			raise("Puppet::ResourceError", "%s", err.Error())
		}
		return object.NewString(title)
	})

	// apply(desired, get:, set:) — run the provider protocol against a desired
	// list, calling the Ruby get / set blocks inline; returns a summary Hash.
	// Puppet::ResourceError when the desired list fails validation.
	cls.define("apply", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		desired := praResourceList(args[0])
		kw := fgKwHash(args[1:])
		getProc := praProcKw(kw, "get")
		setProc := praProcKw(kw, "set")

		t := self(v)
		ctx := resourceapi.NewContext(t, resourceapi.DiscardLogger{})
		prov := &praProvider{vm: vm, get: getProc, set: setProc}
		summary, err := t.Apply(ctx, prov, desired)
		if err != nil {
			raise("Puppet::ResourceError", "%s", err.Error())
		}
		return praSummaryToRuby(summary)
	})
}

// praResource reads the single desired-resource Hash argument as a
// resourceapi.Resource.
func praResource(args []object.Value) resourceapi.Resource {
	return praResourceFromValue(praHashArg(args))
}

// praResourceFromValue converts a Ruby Hash to a resourceapi.Resource.
func praResourceFromValue(v object.Value) resourceapi.Resource {
	m, ok := rubyToGoValue(v).(map[string]any)
	if !ok {
		return resourceapi.Resource{}
	}
	return m
}

// praResourceList converts a Ruby Array of Hashes to a slice of Resources.
func praResourceList(v object.Value) []resourceapi.Resource {
	arr, ok := v.(*object.Array)
	if !ok {
		raise("ArgumentError", "expected an Array of resources")
	}
	out := make([]resourceapi.Resource, len(arr.Elems))
	for i, e := range arr.Elems {
		out[i] = praResourceFromValue(e)
	}
	return out
}

// praProcKw reads a mandatory get / set block from the kwargs Hash, raising
// ArgumentError when it is missing or not callable.
func praProcKw(h *object.Hash, key string) *Proc {
	if h == nil {
		raise("ArgumentError", "apply requires a %s: block", key)
	}
	v, ok := fgKwGet(h, key)
	if !ok {
		raise("ArgumentError", "apply requires a %s: block", key)
	}
	p, ok := v.(*Proc)
	if !ok {
		raise("ArgumentError", "%s: must be a Proc", key)
	}
	return p
}

// praProvider adapts a pair of Ruby blocks to the resourceapi.Provider protocol;
// both blocks run inline on the VM goroutine under the GVL.
type praProvider struct {
	vm       *VM
	get, set *Proc
}

// Get calls the Ruby get block and reads back an Array of current-state Hashes.
func (p *praProvider) Get(_ *resourceapi.Context) ([]resourceapi.Resource, error) {
	return praResourceList(p.vm.callBlock(p.get, nil)), nil
}

// Set calls the Ruby set block with the change set keyed by title.
func (p *praProvider) Set(_ *resourceapi.Context, changes map[string]resourceapi.Change) error {
	p.vm.callBlock(p.set, []object.Value{praChangesToRuby(changes)})
	return nil
}

// praChangesToRuby renders a change set as a Ruby Hash title => {is:, should:}.
func praChangesToRuby(changes map[string]resourceapi.Change) object.Value {
	h := object.NewHash()
	for title, ch := range changes {
		entry := object.NewHash()
		entry.Set(object.Symbol("is"), praResourceOrNil(ch.Is))
		entry.Set(object.Symbol("should"), praResourceOrNil(ch.Should))
		h.Set(object.NewString(title), entry)
	}
	return h
}

// praResourceOrNil converts a Resource to a Ruby Hash, or nil when absent.
func praResourceOrNil(r resourceapi.Resource) object.Value {
	if r == nil {
		return object.NilV
	}
	return facterValueToRuby(map[string]any(r))
}

// praSummaryToRuby renders an Apply summary as a Ruby Hash of action => titles.
func praSummaryToRuby(s resourceapi.Summary) object.Value {
	h := object.NewHash()
	h.Set(object.Symbol("created"), strSliceToRuby(s.Created))
	h.Set(object.Symbol("updated"), strSliceToRuby(s.Updated))
	h.Set(object.Symbol("deleted"), strSliceToRuby(s.Deleted))
	h.Set(object.Symbol("unchanged"), strSliceToRuby(s.Unchanged))
	return h
}
