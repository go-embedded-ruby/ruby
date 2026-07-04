// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	libbleve "github.com/go-ruby-bleve/bleve"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-bleve/bleve — a pure-Go (CGO=0) full-text
// search library wrapping blevesearch/bleve/v2 — into rbgo as the native `Bleve`
// module (require "bleve"). The library owns the index format, the query DSL and
// the search machinery; this file is only the thin shell that maps Ruby values
// onto the library's Hash-shaped document/result model (see bleve_bind.go) and
// exposes the class/method surface a Ruby developer would expect:
//
//	Bleve.new_mem_index(mapping = nil)  — an in-memory index
//	Bleve.new(path, mapping = nil)      — a fresh on-disk index
//	Bleve.open(path)                    — an existing on-disk index
//	Bleve::Index    — #index / #delete / #document / #count / #search / #batch / #close
//	Bleve::Mapping  — declare typed fields (text / keyword / numeric / datetime / boolean)
//	Bleve::Query    — .match / .term / .prefix / .fuzzy / .bool / .query_string / …
//	Bleve::SearchResult / Bleve::Hit    — #total / #max_score / #hits / #facets, #id / #score
//	Bleve::Facet    — term / numeric-range / date-range aggregations
//	Bleve::Error (< StandardError) with ClosedError / NotFoundError — the exception tree
//
// The default in-memory (gtreap) and on-disk (scorch) indexes build with
// CGO_ENABLED=0 on every supported 64-bit target — amd64, arm64, riscv64,
// loong64, ppc64le and s390x (big-endian) — so the binding stays fully pure-Go.

// BleveIndex is the Ruby wrapper around a go-ruby-bleve Index.
type BleveIndex struct {
	cls *RClass
	ix  *libbleve.Index
}

func (i *BleveIndex) ToS() string     { return "#<Bleve::Index>" }
func (i *BleveIndex) Inspect() string { return i.ToS() }
func (i *BleveIndex) Truthy() bool    { return true }

// BleveMapping is the Ruby wrapper around a go-ruby-bleve Mapping.
type BleveMapping struct {
	cls *RClass
	m   *libbleve.Mapping
}

func (m *BleveMapping) ToS() string     { return "#<Bleve::Mapping>" }
func (m *BleveMapping) Inspect() string { return m.ToS() }
func (m *BleveMapping) Truthy() bool    { return true }

// BleveQuery is the Ruby wrapper around a go-ruby-bleve Query.
type BleveQuery struct {
	cls *RClass
	q   libbleve.Query
}

func (q *BleveQuery) ToS() string     { return "#<Bleve::Query>" }
func (q *BleveQuery) Inspect() string { return q.ToS() }
func (q *BleveQuery) Truthy() bool    { return true }

// BleveSearchResult is the Ruby wrapper around a go-ruby-bleve SearchResult.
type BleveSearchResult struct {
	cls *RClass
	r   *libbleve.SearchResult
}

func (r *BleveSearchResult) ToS() string     { return "#<Bleve::SearchResult>" }
func (r *BleveSearchResult) Inspect() string { return r.ToS() }
func (r *BleveSearchResult) Truthy() bool    { return true }

// BleveHit is the Ruby wrapper around a single go-ruby-bleve Hit.
type BleveHit struct {
	cls *RClass
	h   libbleve.Hit
}

func (h *BleveHit) ToS() string     { return "#<Bleve::Hit " + h.h.ID + ">" }
func (h *BleveHit) Inspect() string { return h.ToS() }
func (h *BleveHit) Truthy() bool    { return true }

// BleveBatch is the Ruby wrapper around a go-ruby-bleve Batch, yielded to the
// block of Bleve::Index#batch.
type BleveBatch struct {
	cls *RClass
	b   *libbleve.Batch
}

func (b *BleveBatch) ToS() string     { return "#<Bleve::Batch>" }
func (b *BleveBatch) Inspect() string { return b.ToS() }
func (b *BleveBatch) Truthy() bool    { return true }

// BleveFacet is the Ruby wrapper around a go-ruby-bleve Facet request.
type BleveFacet struct {
	cls *RClass
	f   *libbleve.Facet
}

func (f *BleveFacet) ToS() string     { return "#<Bleve::Facet>" }
func (f *BleveFacet) Inspect() string { return f.ToS() }
func (f *BleveFacet) Truthy() bool    { return true }

// registerBleve installs the Bleve module and its classes (require "bleve"). It
// runs eagerly at boot; the error tree needs StandardError in place.
func (vm *VM) registerBleve() {
	mod := newClass("Bleve", nil)
	mod.isModule = true
	vm.consts["Bleve"] = mod

	vm.registerBleveErrors(mod)

	mk := func(name string, super *RClass) *RClass {
		full := "Bleve::" + name
		cls := newClass(full, super)
		mod.consts[name] = cls
		vm.consts[full] = cls
		return cls
	}

	idxCls := mk("Index", vm.cObject)
	mapCls := mk("Mapping", vm.cObject)
	qryCls := mk("Query", vm.cObject)
	resCls := mk("SearchResult", vm.cObject)
	hitCls := mk("Hit", vm.cObject)
	batCls := mk("Batch", vm.cObject)
	facCls := mk("Facet", vm.cObject)

	vm.registerBleveModule(mod, idxCls)
	vm.registerBleveIndex(idxCls, resCls, batCls)
	vm.registerBleveBatch(batCls)
	vm.registerBleveMapping(mapCls)
	vm.registerBleveQuery(qryCls)
	vm.registerBleveResult(resCls, hitCls)
	vm.registerBleveHit(hitCls)
	vm.registerBleveFacet(facCls)
}

// registerBleveErrors installs the Bleve::Error exception tree: Error <
// StandardError, and ClosedError / NotFoundError < Error. Each class is
// registered both as a nested constant of Bleve and under its qualified name in
// the top-level table so a re-raised library sentinel finds the matching class.
func (vm *VM) registerBleveErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Error", "Bleve::Error", std)
	reg("ClosedError", "Bleve::ClosedError", base)
	reg("NotFoundError", "Bleve::NotFoundError", base)
}

// bleveSMethod installs a class ("singleton") method on a class.
func bleveSMethod(cls *RClass, name string, fn NativeFn) {
	cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
}

// registerBleveModule installs the Bleve.* constructors: new_mem_index (an
// in-memory index), new (a fresh on-disk index at a path) and open (an existing
// on-disk index). Each accepts an optional Bleve::Mapping (default dynamic).
func (vm *VM) registerBleveModule(mod, idxCls *RClass) {
	bleveSMethod(mod, "new_mem_index", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ix, err := libbleve.NewMemIndex(bleveMappingArg(args, 0))
		raiseBleveErr(err)
		return &BleveIndex{cls: idxCls, ix: ix}
	})
	bleveSMethod(mod, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "new")
		ix, err := libbleve.New(strArg(args[0]), bleveMappingArg(args, 1))
		raiseBleveErr(err)
		return &BleveIndex{cls: idxCls, ix: ix}
	})
	bleveSMethod(mod, "open", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "open")
		ix, err := libbleve.Open(strArg(args[0]))
		raiseBleveErr(err)
		return &BleveIndex{cls: idxCls, ix: ix}
	})
}

// registerBleveIndex installs the Bleve::Index instance surface: index / delete /
// document / count / search / batch / close / path.
func (vm *VM) registerBleveIndex(cls, resCls, batCls *RClass) {
	self := func(v object.Value) *BleveIndex { return v.(*BleveIndex) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("index", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 2, "index")
		raiseBleveErr(self(v).ix.Index(strArg(args[0]), bleveDoc(args[1])))
		return v
	})
	d("delete", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "delete")
		raiseBleveErr(self(v).ix.Delete(strArg(args[0])))
		return v
	})
	d("count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		n, err := self(v).ix.Count()
		raiseBleveErr(err)
		return object.IntValue(int64(n))
	})
	d("document", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "document")
		fields, err := self(v).ix.Document(strArg(args[0]))
		raiseBleveErr(err)
		return bleveFieldsToHash(fields)
	})
	d("search", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "search")
		q := bleveQueryArg(args[0])
		opts := bleveSearchOpts(args[1:])
		res, err := self(v).ix.Search(q, opts...)
		raiseBleveErr(err)
		return &BleveSearchResult{cls: resCls, r: res}
	})
	d("batch", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (batch)")
		}
		err := self(v).ix.Batch(func(b *libbleve.Batch) error {
			vm.callBlock(blk, []object.Value{&BleveBatch{cls: batCls, b: b}})
			return nil
		})
		raiseBleveErr(err)
		return v
	})
	d("close", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		raiseBleveErr(self(v).ix.Close())
		return object.NilV
	})
	d("path", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ix.Path())
	})
}

// registerBleveBatch installs the Bleve::Batch surface yielded to Index#batch:
// index and delete queue operations applied atomically when the block returns.
func (vm *VM) registerBleveBatch(cls *RClass) {
	self := func(v object.Value) *BleveBatch { return v.(*BleveBatch) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("index", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 2, "index")
		raiseBleveErr(self(v).b.Index(strArg(args[0]), bleveDoc(args[1])))
		return v
	})
	d("delete", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "delete")
		self(v).b.Delete(strArg(args[0]))
		return v
	})
}

// registerBleveMapping installs Bleve::Mapping.new and the fluent field-declaring
// setters that mirror the library's typed-field builders.
func (vm *VM) registerBleveMapping(cls *RClass) {
	bleveSMethod(cls, "new", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &BleveMapping{cls: cls, m: libbleve.NewMapping()}
	})

	self := func(v object.Value) *BleveMapping { return v.(*BleveMapping) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("set_default_analyzer", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "set_default_analyzer")
		self(v).m.SetDefaultAnalyzer(strArg(args[0]))
		return v
	})
	d("add_text_field", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "add_text_field")
		self(v).m.AddTextField(strArg(args[0]))
		return v
	})
	d("add_text_field_with_analyzer", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 2, "add_text_field_with_analyzer")
		self(v).m.AddTextFieldWithAnalyzer(strArg(args[0]), strArg(args[1]))
		return v
	})
	d("add_keyword_field", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "add_keyword_field")
		self(v).m.AddKeywordField(strArg(args[0]))
		return v
	})
	d("add_numeric_field", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "add_numeric_field")
		self(v).m.AddNumericField(strArg(args[0]))
		return v
	})
	d("add_datetime_field", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "add_datetime_field")
		self(v).m.AddDateTimeField(strArg(args[0]))
		return v
	})
	d("add_boolean_field", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "add_boolean_field")
		self(v).m.AddBooleanField(strArg(args[0]))
		return v
	})
}

// registerBleveQuery installs Bleve::Query: a class method per query constructor
// plus the fluent field / boost / fuzziness setters shared across query types.
func (vm *VM) registerBleveQuery(cls *RClass) {
	wrap := func(q libbleve.Query) object.Value { return &BleveQuery{cls: cls, q: q} }

	text1 := func(name string, ctor func(string) libbleve.Query) {
		bleveSMethod(cls, name, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			bleveArity(args, 1, name)
			return wrap(ctor(strArg(args[0])))
		})
	}
	text1("match", libbleve.Match)
	text1("match_phrase", libbleve.MatchPhrase)
	text1("term", libbleve.Term)
	text1("prefix", libbleve.Prefix)
	text1("fuzzy", libbleve.Fuzzy)
	text1("wildcard", libbleve.Wildcard)
	text1("regexp", libbleve.Regexp)
	text1("query_string", libbleve.QueryString)

	nullary := func(name string, ctor func() libbleve.Query) {
		bleveSMethod(cls, name, func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return wrap(ctor())
		})
	}
	nullary("match_all", libbleve.MatchAll)
	nullary("match_none", libbleve.MatchNone)

	bleveSMethod(cls, "numeric_range", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 2, "numeric_range")
		return wrap(libbleve.NumericRange(bleveBound(args[0]), bleveBound(args[1])))
	})
	bleveSMethod(cls, "date_range", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 2, "date_range")
		return wrap(libbleve.DateRange(bleveTime(args[0]), bleveTime(args[1])))
	})
	bleveSMethod(cls, "bool", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		h := bleveOptsHash(args)
		return wrap(libbleve.Bool(
			bleveQuerySlice(h, "must"),
			bleveQuerySlice(h, "should"),
			bleveQuerySlice(h, "must_not"),
		))
	})

	self := func(v object.Value) *BleveQuery { return v.(*BleveQuery) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("field", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "field")
		q := self(v)
		q.q = q.q.Field(strArg(args[0]))
		return v
	})
	d("boost", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "boost")
		q := self(v)
		q.q = q.q.Boost(bleveFloat(args[0]))
		return v
	})
	d("fuzziness", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 1, "fuzziness")
		q := self(v)
		q.q = q.q.Fuzziness(int(intArg(args[0])))
		return v
	})
}

// registerBleveResult installs Bleve::SearchResult: the aggregate stats (total /
// max_score / took), the page of Bleve::Hit and the computed facets.
func (vm *VM) registerBleveResult(cls, hitCls *RClass) {
	self := func(v object.Value) *BleveSearchResult { return v.(*BleveSearchResult) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("total", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).r.Total()))
	})
	d("max_score", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).r.MaxScore())
	})
	d("took", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).r.Took().Seconds())
	})
	d("hits", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		hits := self(v).r.Hits()
		out := make([]object.Value, len(hits))
		for i, h := range hits {
			out[i] = &BleveHit{cls: hitCls, h: h}
		}
		return object.NewArrayFromSlice(out)
	})
	d("facets", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		for name, fr := range self(v).r.Facets() {
			h.Set(object.NewString(name), bleveFacetResultToHash(fr))
		}
		return h
	})
}

// registerBleveHit installs Bleve::Hit: id / score / fields / fragments and #to_h.
func (vm *VM) registerBleveHit(cls *RClass) {
	self := func(v object.Value) *BleveHit { return v.(*BleveHit) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).h.ID)
	})
	d("score", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).h.Score)
	})
	d("fields", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return bleveFieldsToHash(self(v).h.Fields)
	})
	d("fragments", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return bleveFragmentsToHash(self(v).h.Fragments)
	})
	d("to_h", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self(v).h
		out := object.NewHash()
		out.Set(object.NewString("id"), object.NewString(h.ID))
		out.Set(object.NewString("score"), object.Float(h.Score))
		out.Set(object.NewString("fields"), bleveFieldsToHash(h.Fields))
		out.Set(object.NewString("fragments"), bleveFragmentsToHash(h.Fragments))
		return out
	})
}

// registerBleveFacet installs Bleve::Facet: the term-facet constructor and the
// fluent numeric- and date-range bucket setters.
func (vm *VM) registerBleveFacet(cls *RClass) {
	bleveSMethod(cls, "term", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 2, "term")
		return &BleveFacet{cls: cls, f: libbleve.TermFacet(strArg(args[0]), int(intArg(args[1])))}
	})

	self := func(v object.Value) *BleveFacet { return v.(*BleveFacet) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("add_numeric_range", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 3, "add_numeric_range")
		self(v).f.AddNumericRange(strArg(args[0]), bleveBound(args[1]), bleveBound(args[2]))
		return v
	})
	d("add_date_range", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bleveArity(args, 3, "add_date_range")
		self(v).f.AddDateRange(strArg(args[0]), bleveTime(args[1]), bleveTime(args[2]))
		return v
	})
}
