package object

import "sync"

// Symbols are interned: boxing a symbol name into a Value reuses a shared,
// immutable box instead of allocating a fresh one. A Symbol is `type Symbol
// string`, so boxing a non-empty one into the Value interface allocates via the
// runtime's convTstring — one allocation per box on the hot paths that box a
// symbol per operation (symbol-keyed Hash lookups such as h.Get(SymVal("k")),
// keyword-argument dispatch, method_missing names). Ruby symbols are single
// objects — :foo.equal?(:foo) — so sharing the box changes no semantics and
// additionally makes that identity hold by pointer, not only by value.
//
// The table is a sync.Map (endian-agnostic; the intern key is the name string,
// never a machine-word layout), so SymVal is safe from concurrent VM threads
// without a hand-rolled lock. It grows once per distinct symbol name seen and is
// never pruned, matching Ruby, where symbols live for the process lifetime.
var symIntern sync.Map // map[string]Value

// SymVal boxes name as a Symbol Value, interning the box so a given symbol name
// yields the identical Value on every call. The first call for a name allocates
// its box; every later call is an allocation-free map load. Use it on hot paths
// that box a symbol per operation; the result is exactly object.Symbol(name)
// (same dynamic type and value), only shared.
func SymVal(name string) Value {
	if v, ok := symIntern.Load(name); ok {
		return v.(Value)
	}
	// Miss: box once and publish it. LoadOrStore resolves a race with a
	// concurrent first-boxer by returning whichever box won, so all callers
	// still converge on one shared Value.
	v, _ := symIntern.LoadOrStore(name, Value{tag: TagSym, obj: Symbol(name)})
	return v.(Value)
}
