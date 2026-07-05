// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	racc "github.com/go-ruby-racc/racc"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-racc/racc LALR(1) runtime. The
// library owns the whole table-driven automaton (shift/reduce/accept/error and
// error recovery); rbgo only decodes a generated parser's `Racc_arg` constant
// into a racc.Tables and shuttles token symbols and reduce values across the
// Ruby⇄Go boundary. The class + method registration and the three host seams
// (next_token / _reduce_N / on_error) live in racc.go.

// raccNilCell is the sentinel a decoded parse table stores for a Ruby `nil`
// cell. It must equal the library's unexported ptrNil / nilCell constants
// (both -1<<30, an impossible real table value) so an empty action/goto slot or
// a nil per-state pointer round-trips into the engine unchanged.
const raccNilCell = -1 << 30

// raccInt reads a Ruby Integer as a Go int, treating any other value (a nil
// cell in a table row) as 0. Table cells that may legitimately be nil go through
// raccIntSlice, which maps nil to raccNilCell instead.
func raccInt(v object.Value) int {
	if n, ok := v.(object.Integer); ok {
		return int(n)
	}
	return 0
}

// raccIntSlice decodes a Ruby Array of Integer|nil into a []int, mapping every
// non-Integer element (a Ruby nil) to raccNilCell so the engine's nil-cell
// checks fire exactly as they do on MRI's tables. A non-Array (an absent table)
// decodes to nil.
func raccIntSlice(v object.Value) []int {
	arr, ok := v.(*object.Array)
	if !ok {
		return nil
	}
	out := make([]int, len(arr.Elems))
	for i, e := range arr.Elems {
		if n, ok := e.(object.Integer); ok {
			out[i] = int(n)
		} else {
			out[i] = raccNilCell
		}
	}
	return out
}

// raccTokenKey converts a Ruby token symbol to the key type the library's
// TokenTable is keyed by: a Ruby Symbol (:NUMBER) becomes a racc.Symbol, a Ruby
// String ("+") a Go string — so :foo and "foo" stay distinct keys, matching MRI
// — and the EOF markers (false / nil) become a nil key. next_token and the
// token-table decode share this function so the returned symbol looks up the
// same key the table was built with.
func raccTokenKey(v object.Value) any {
	switch s := v.(type) {
	case object.Symbol:
		return racc.Symbol(string(s))
	case *object.String:
		return s.Str()
	case object.Bool:
		return nil // false ⇒ EOF
	case object.Nil:
		return nil
	}
	return nil
}

// raccSymName reads a Ruby Symbol or String as a Go method name, returning ""
// for anything else. It names both the reduce-action methods carried in the
// reduce table (:_reduce_N / :racc_error) and yyparse's method-id argument.
func raccSymName(v object.Value) string {
	switch s := v.(type) {
	case object.Symbol:
		return string(s)
	case *object.String:
		return s.Str()
	}
	return ""
}

// raccToVal lifts a value that crossed into the engine as an `any` (a token
// value or a reduce result) back to a Ruby Value; a Go nil (MRI's `nil` result
// for an empty rule, or a failed/recovered parse) becomes Ruby nil.
func raccToVal(v any) object.Value {
	if ov, ok := v.(object.Value); ok && ov != nil {
		return ov
	}
	return object.NilV
}

// raccValArray wraps a slice of engine values (popped reduce values, or the
// value-stack snapshot handed to on_error) as a Ruby Array so the seam can pass
// it to the parser's Ruby method.
func raccValArray(vs []any) *object.Array {
	elems := make([]object.Value, len(vs))
	for i, v := range vs {
		elems[i] = raccToVal(v)
	}
	return object.NewArrayFromSlice(elems)
}

// raccDecodeToken reads the [sym, val] pair a Ruby next_token returned into the
// (symbol-key, value) the NextToken seam yields. A nil result, a non-Array, or a
// short array is treated as EOF (MRI's `[false, false]` / nil), i.e. a nil key.
func raccDecodeToken(r object.Value) (any, object.Value) {
	arr, ok := r.(*object.Array)
	if !ok || len(arr.Elems) < 2 {
		return nil, object.NilV
	}
	return raccTokenKey(arr.Elems[0]), arr.Elems[1]
}

// raccDecodeYield reads the token an lexer object yielded to yyparse's block —
// either a single [tok, val] Array (as MRI's `yield [tok, val]`) or the two
// values passed positionally — into the (symbol-key, value) the Yyparse iterator
// yields. Anything else is EOF.
func raccDecodeYield(a []object.Value) (any, object.Value) {
	if len(a) == 1 {
		if arr, ok := a[0].(*object.Array); ok && len(arr.Elems) >= 2 {
			return raccTokenKey(arr.Elems[0]), arr.Elems[1]
		}
	}
	if len(a) >= 2 {
		return raccTokenKey(a[0]), a[1]
	}
	return nil, object.NilV
}

// raccBuildTables decodes a generated parser's `Racc_arg` constant into a
// racc.Tables plus the parallel slice of reduce-action method names. The field
// order mirrors MRI's Racc_arg array element-for-element (see racc.Tables); the
// reduce table's third-of-three column is a method symbol (:_reduce_N /
// :racc_error) on MRI, so it is lifted out into methods[rule] and replaced by
// the rule index the library's Reduce seam is handed as its method id. ok is
// false for a malformed constant (not a 14+ element Array, or a non-Array reduce
// table / non-Hash token table), which the caller turns into an ArgumentError.
func raccBuildTables(v object.Value) (*racc.Tables, []string, bool) {
	arr, ok := v.(*object.Array)
	if !ok || len(arr.Elems) < 14 {
		return nil, nil, false
	}
	e := arr.Elems

	reduceRaw, ok := e[9].(*object.Array)
	if !ok {
		return nil, nil, false
	}
	reduceTable := make([]int, 0, len(reduceRaw.Elems))
	methods := make([]string, 0, len(reduceRaw.Elems)/3)
	for i := 0; i+2 < len(reduceRaw.Elems); i += 3 {
		reduceTable = append(reduceTable,
			raccInt(reduceRaw.Elems[i]),   // len
			raccInt(reduceRaw.Elems[i+1]), // reduce_to
			i/3)                           // method id ⇒ rule index into methods
		methods = append(methods, raccSymName(reduceRaw.Elems[i+2]))
	}

	tokenTable, ok := raccTokenTable(e[10])
	if !ok {
		return nil, nil, false
	}

	t := &racc.Tables{
		ActionTable:   raccIntSlice(e[0]),
		ActionCheck:   raccIntSlice(e[1]),
		ActionDefault: raccIntSlice(e[2]),
		ActionPointer: raccIntSlice(e[3]),
		GotoTable:     raccIntSlice(e[4]),
		GotoCheck:     raccIntSlice(e[5]),
		GotoDefault:   raccIntSlice(e[6]),
		GotoPointer:   raccIntSlice(e[7]),
		NtBase:        raccInt(e[8]),
		ReduceTable:   reduceTable,
		TokenTable:    tokenTable,
		ShiftN:        raccInt(e[11]),
		ReduceN:       raccInt(e[12]),
		UseResult:     !object.IsNil(e[13]) && e[13].Truthy(),
	}
	return t, methods, true
}

// raccTokenTable decodes Racc_arg[10] — the Ruby Hash mapping an external token
// symbol to its internal id — into the library's map[any]int, keying each entry
// by raccTokenKey so a symbol returned by next_token matches. ok is false when
// the element is not a Hash.
func raccTokenTable(v object.Value) (map[any]int, bool) {
	h, ok := v.(*object.Hash)
	if !ok {
		return nil, false
	}
	m := make(map[any]int, h.Len())
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m[raccTokenKey(k)] = raccInt(val)
	}
	return m, true
}
