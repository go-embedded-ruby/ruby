// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"

	kaminari "github.com/go-ruby-kaminari/kaminari"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires kaminari's pagination scope algebra over the pure-Go
// github.com/go-ruby-kaminari/kaminari library (require "kaminari"). Two
// paginators are exposed: Kaminari::PaginatableArray paginates an in-memory Ruby
// Array (Array#page / Kaminari.paginate_array), and Kaminari::PaginatableRelation
// paginates a lazily-evaluated collection through the library's Relation seam —
// any Ruby object answering #count and #offset(n).limit(m). That seam is what
// wires Model.page / relation.page to an ActiveRecord relation (Count -> #count,
// Slice -> #offset(offset).limit(limit)); it equally accepts an injected stub
// relation. All offset/limit arithmetic and the page-metadata methods
// (current_page / total_pages / first_page? / next_page / … / page_entries_info)
// are the deterministic library.

// KaminariArray is the Ruby wrapper around a *kaminari.Array — kaminari's
// PaginatableArray, an in-memory collection that paginates itself. Page / Per /
// Padding return fresh snapshots over the same backing slice; #records
// materialises the current window back into a Ruby Array.
type KaminariArray struct {
	a  *kaminari.Array
	vm *VM
}

func (a *KaminariArray) ToS() string     { return "#<Kaminari::PaginatableArray>" }
func (a *KaminariArray) Inspect() string { return a.ToS() }
func (a *KaminariArray) Truthy() bool    { return true }

// KaminariRelation is the Ruby wrapper around a *kaminari.RelationPaginator — the
// paginator over a Relation seam. Page / Per / Padding return fresh snapshots
// over the same seam; #records calls back through the seam (#offset.#limit) and
// returns whatever it yields (the ActiveRecord relation, or the stub's slice).
type KaminariRelation struct {
	p  *kaminari.RelationPaginator
	vm *VM
}

func (r *KaminariRelation) ToS() string     { return "#<Kaminari::PaginatableRelation>" }
func (r *KaminariRelation) Inspect() string { return r.ToS() }
func (r *KaminariRelation) Truthy() bool    { return true }

// kaminariScope is the shared metadata surface both paginators expose (kaminari's
// Array and RelationPaginator implement the identical getters). installKaminariMeta
// binds the metadata methods once against this interface.
type kaminariScope interface {
	CurrentPage() int
	TotalPages() int
	TotalCount() int
	LimitValue() int
	OffsetValue() int
	FirstPage() bool
	LastPage() bool
	OutOfRange() bool
	PrevPage() *int
	NextPage() *int
	CurrentPerPage() int
	EntriesInfo() kaminari.EntriesInfo
}

// kaminariRubyRelation adapts a Ruby relation object (anything answering #count
// and #offset(n).limit(m)) to the library's Relation seam. Count reads the total
// once; Slice materialises the [offset, offset+limit) window by chaining
// #offset(offset).limit(limit) — a negative limit ("no limit", from .per(nil))
// skips the #limit call and returns the offset relation whole.
type kaminariRubyRelation struct {
	vm  *VM
	rel object.Value
}

func (r *kaminariRubyRelation) Count() int {
	return kaminariToInt(r.vm.send(r.rel, "count", nil, nil))
}

func (r *kaminariRubyRelation) Slice(offset, limit int) any {
	rel := r.vm.send(r.rel, "offset", []object.Value{object.IntValue(int64(offset))}, nil)
	if limit < 0 {
		return rel
	}
	return r.vm.send(rel, "limit", []object.Value{object.IntValue(int64(limit))}, nil)
}

// kaminariToInt reads a Ruby #count result as an int (a non-integer degrades to
// 0, so a misbehaving stub cannot crash the paginator).
func kaminariToInt(v object.Value) int {
	if n, ok := v.(object.Integer); ok {
		return int(n)
	}
	return 0
}

// kaminariPageNum reads the #page argument (absent / non-integer default to 1;
// the library clamps a value below 1 up to 1 anyway).
func kaminariPageNum(args []object.Value) int {
	if len(args) == 0 {
		return 1
	}
	if n, ok := args[0].(object.Integer); ok {
		return int(n)
	}
	return 1
}

// kaminariPerPtr reads the #per argument into the *int the library uses: an
// Integer yields that count, while an absent, nil or non-integer argument yields
// nil — kaminari's "no limit" (a single page holding everything).
func kaminariPerPtr(args []object.Value) *int {
	if len(args) == 0 || object.IsNil(args[0]) {
		return nil
	}
	if n, ok := args[0].(object.Integer); ok {
		return kaminari.Intp(int(n))
	}
	return nil
}

// kaminariPaddingNum reads the #padding argument as an int (absent / non-integer
// -> 0).
func kaminariPaddingNum(args []object.Value) int {
	if len(args) == 0 {
		return 0
	}
	if n, ok := args[0].(object.Integer); ok {
		return int(n)
	}
	return 0
}

// kaminariIntpValue maps a *int metadata result (prev_page / next_page) to a Ruby
// Integer, or nil when the library reports none.
func kaminariIntpValue(p *int) object.Value {
	if p == nil {
		return object.NilV
	}
	return object.IntValue(int64(*p))
}

// kaminariEntriesInfo renders kaminari's page_entries_info sentence from the pure
// EntriesInfo data: the empty, single-page and multi-page shapes the gem's helper
// produces (with the entry/entries pluralisation on a lone record).
func kaminariEntriesInfo(e kaminari.EntriesInfo) string {
	switch {
	case e.Empty:
		return "No entries found"
	case e.OnePage:
		if e.TotalCount == 1 {
			return "Displaying 1 entry"
		}
		return fmt.Sprintf("Displaying all %d entries", e.TotalCount)
	default:
		return fmt.Sprintf("Displaying entries %d - %d of %d in total", e.First, e.Last, e.TotalCount)
	}
}

// kaminariRecordsToRuby maps the []any window kaminari.Array#Records yields (each
// element is the original Ruby value, stored as any) back into a Ruby Array.
func kaminariRecordsToRuby(items []any) *object.Array {
	out := make([]object.Value, len(items))
	for i, it := range items {
		out[i] = it.(object.Value)
	}
	return object.NewArrayFromSlice(out)
}

// kaminariArrayOpts reads the trailing options Hash of Kaminari.paginate_array
// (total_count: / limit: / offset: / padding:) into the library's ArrayOptions.
func kaminariArrayOpts(args []object.Value) []kaminari.ArrayOption {
	if len(args) < 2 {
		return nil
	}
	h, ok := args[1].(*object.Hash)
	if !ok {
		return nil
	}
	var opts []kaminari.ArrayOption
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		n, isInt := val.(object.Integer)
		if !isInt {
			continue
		}
		switch arStr(k) {
		case "total_count":
			opts = append(opts, kaminari.WithTotalCount(int(n)))
		case "limit":
			opts = append(opts, kaminari.WithLimit(int(n)))
		case "offset":
			opts = append(opts, kaminari.WithOffset(int(n)))
		case "padding":
			opts = append(opts, kaminari.WithPadding(int(n)))
		}
	}
	return opts
}

// kaminariItems copies a Ruby Array's elements into the []any backing a
// PaginatableArray (each element is carried unchanged as a Ruby value).
func kaminariItems(arr *object.Array) []any {
	items := make([]any, len(arr.Elems))
	for i, e := range arr.Elems {
		items[i] = e
	}
	return items
}

// kaminariNewArray builds a bare PaginatableArray over a Ruby Array (Array#page).
func kaminariNewArray(arr *object.Array) *kaminari.Array {
	return kaminari.NewArray(kaminariItems(arr))
}

// kaminariPaginateRel wraps a Ruby relation object in the library's Relation seam,
// pages it, and returns the KaminariRelation wrapper. Shared by Model.page /
// relation.page / Kaminari.paginate — the count is read up front by Paginate.
func (vm *VM) kaminariPaginateRel(rel object.Value, page int) object.Value {
	seam := &kaminariRubyRelation{vm: vm, rel: rel}
	return &KaminariRelation{p: kaminari.Paginate(seam).Page(page), vm: vm}
}

// registerKaminari installs the Kaminari module and its two paginators
// (require "kaminari"): Kaminari.paginate_array(items) / Array#page paginate an
// in-memory Array, Kaminari.paginate(relation) paginates any relation-shaped
// object, and Model.page / relation.page wire the seam to an ActiveRecord
// relation. It runs after registerActiveRecord so it can add #page to the
// ActiveRecord::Base / Model / Relation surface.
func (vm *VM) registerKaminari() {
	mod := newClass("Kaminari", nil)
	mod.isModule = true
	vm.consts["Kaminari"] = mod

	vm.registerKaminariArrayClass(mod)
	vm.registerKaminariRelationClass(mod)

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// Kaminari.paginate_array(items, total_count: n) -> PaginatableArray.
	sm("paginate_array", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		arr, ok := args[0].(*object.Array)
		if !ok {
			raise("TypeError", "no implicit conversion into Array")
		}
		a := kaminari.PaginateArray(kaminariItems(arr), kaminariArrayOpts(args)...)
		return &KaminariArray{a: a, vm: vm}
	})

	// Kaminari.paginate(relation) -> PaginatableRelation over the Relation seam.
	sm("paginate", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return vm.kaminariPaginateRel(args[0], 1)
	})

	vm.registerKaminariArrayCoreMethod()
	vm.registerKaminariActiveRecord()
}
