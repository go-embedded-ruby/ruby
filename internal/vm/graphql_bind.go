// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"context"
	"fmt"

	graphql "github.com/go-ruby-graphql/graphql"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent GraphQL core of github.com/go-ruby-graphql/graphql — a
// pure-Go (cgo-free) Ruby-flavoured façade over github.com/graphql-go/graphql, a
// mature implementation of the GraphQL specification. It carries the three
// instance value types the GraphQL module wraps — a type reference, a compiled
// schema and the object-type definition builder yielded to
// GraphQL::ObjectType.define — plus the value conversions that turn a Ruby value
// returned by a field resolver into the Go value graphql-go completes, the ones
// that turn a graphql-go result back into a Ruby Hash, and the resolver bridge
// that re-enters the VM to run a Ruby block for every field. All execution is
// delegated to go-ruby-graphql; a query rbgo runs against a schema produces the
// exact data / errors Hash shape graphql-ruby produces. See graphql.go for the
// module wiring.

// GraphQLType is a member of the GraphQL type system exposed to Ruby: a scalar,
// an object type, or a List / NonNull wrapper. It carries the wrapped
// go-ruby-graphql type (usable as a field or argument type) and, when it is an
// object type, the concrete *graphql.ObjectType usable as a schema root. The cls
// field is the Ruby class it reports (GraphQL::Type or GraphQL::ObjectType).
type GraphQLType struct {
	cls  *RClass
	name string
	t    graphql.Type
	obj  *graphql.ObjectType // non-nil iff this is an object type
}

func (t *GraphQLType) ToS() string {
	if t.name != "" {
		return fmt.Sprintf("#<%s %s>", t.cls.name, t.name)
	}
	return "#<" + t.cls.name + ">"
}
func (t *GraphQLType) Inspect() string { return t.ToS() }
func (t *GraphQLType) Truthy() bool    { return true }

// GraphQLSchema is an instance of GraphQL::Schema: a validated type system rooted
// at a query type, against which query documents are executed with #execute.
type GraphQLSchema struct {
	cls *RClass
	sch *graphql.Schema
}

func (s *GraphQLSchema) ToS() string     { return "#<GraphQL::Schema>" }
func (s *GraphQLSchema) Inspect() string { return s.ToS() }
func (s *GraphQLSchema) Truthy() bool    { return true }

// GraphQLObjectDSL is the definition builder yielded to the block of
// GraphQL::ObjectType.define: each #field call appends a field (with its Ruby
// resolver block) to the map from which the object type is built when the block
// returns.
type GraphQLObjectDSL struct {
	cls    *RClass
	vm     *VM
	fields graphql.FieldMap
}

func (d *GraphQLObjectDSL) ToS() string     { return "#<GraphQL::ObjectType::DSL>" }
func (d *GraphQLObjectDSL) Inspect() string { return d.ToS() }
func (d *GraphQLObjectDSL) Truthy() bool    { return true }

// graphqlCtxKey is the private key under which #execute stashes the Ruby context:
// value into the Go context.Context, so a field resolver can hand it back to the
// Ruby block as the third resolver argument.
type graphqlCtxKey struct{}

// graphqlResolve runs a field's Ruby resolver block and reshapes the result for
// go-ruby-graphql. The block is called with (source, args, context) — the parent
// value, the coerced field arguments as a Ruby Hash, and the request context —
// and its return value is converted to a Go value graphql-go completes. A block
// that returns a GraphQL::ExecutionError (or raises a Ruby exception) collects a
// field error at the current path, exactly as a graphql-ruby resolver does.
func (vm *VM) graphqlResolve(proc *Proc, p graphql.ResolveParams) (result interface{}, rerr error) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				rerr = graphql.NewExecutionError(re.Message)
				return
			}
			panic(r)
		}
	}()
	source := rubyFromGo(p.Source)
	args := rubyFromGo(p.Args)
	ret := vm.callBlock(proc, []object.Value{source, args, graphqlContextValue(p.Context)})
	if ee, ok := vm.graphqlExecErr(ret); ok {
		return nil, ee
	}
	return goFromRuby(ret), nil
}

// graphqlContextValue extracts the Ruby context value #execute stashed in the Go
// context, or nil when the request carried no context.
func graphqlContextValue(ctx context.Context) object.Value {
	if ctx != nil {
		if v, ok := ctx.Value(graphqlCtxKey{}).(object.Value); ok {
			return v
		}
	}
	return object.NilV
}

// graphqlExecErr reports whether a resolver's return value is a
// GraphQL::ExecutionError instance and, if so, converts it to the go-ruby-graphql
// error that collects a field error carrying its message.
func (vm *VM) graphqlExecErr(v object.Value) (*graphql.ExecutionError, bool) {
	o, ok := v.(*RObject)
	if !ok {
		return nil, false
	}
	target, ok := vm.consts["GraphQL::ExecutionError"].(*RClass)
	if !ok {
		return nil, false
	}
	for c := o.class; c != nil; c = c.super {
		if c == target {
			return graphql.NewExecutionError(vm.exceptionMessageText(o)), true
		}
	}
	return nil, false
}

// goFromRuby converts a Ruby value returned by a resolver into the plain Go value
// go-ruby-graphql completes: nil, bool, int, float64, string, a []interface{} for
// an Array and a map[string]interface{} for a Hash (whose keys become their
// String / Symbol text). Any other value is rendered through its #to_s, matching
// how graphql-ruby coerces an unexpected scalar to a String.
func goFromRuby(v object.Value) interface{} {
	switch x := v.(type) {
	case nil, object.Nil:
		return nil
	case object.Bool:
		return bool(x)
	case object.Integer:
		return int(x)
	case object.Float:
		return float64(x)
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	case *object.Array:
		out := make([]interface{}, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = goFromRuby(e)
		}
		return out
	case *object.Hash:
		out := make(map[string]interface{}, x.Len())
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			out[graphqlKeyString(k)] = goFromRuby(val)
		}
		return out
	default:
		return v.ToS()
	}
}

// graphqlKeyString renders a Ruby Hash key as the string used for the
// corresponding Go map / GraphQL field name: a Symbol and a String yield their
// text, and any other key its #to_s.
func graphqlKeyString(k object.Value) string {
	switch x := k.(type) {
	case object.Symbol:
		return string(x)
	case *object.String:
		return x.Str()
	default:
		return k.ToS()
	}
}

// rubyFromGo converts a plain Go value produced by go-ruby-graphql (a result
// payload, a coerced argument map, or an error entry) into the matching Ruby
// value: nil becomes nil, the scalars map across, a slice becomes an Array and a
// map becomes a Hash with String keys. []map[string]interface{} — the shape of a
// result's "errors" and each error's "locations" — is handled explicitly so the
// whole error tree round-trips. Any other Go value is rendered through fmt.
func rubyFromGo(v interface{}) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(x)
	case int:
		return object.Integer(int64(x))
	case int64:
		return object.Integer(x)
	case float64:
		return object.Float(x)
	case string:
		return object.NewString(x)
	case []interface{}:
		elems := make([]object.Value, len(x))
		for i, e := range x {
			elems[i] = rubyFromGo(e)
		}
		return object.NewArrayFromSlice(elems)
	case []map[string]interface{}:
		elems := make([]object.Value, len(x))
		for i, e := range x {
			elems[i] = rubyFromGo(e)
		}
		return object.NewArrayFromSlice(elems)
	case map[string]interface{}:
		h := object.NewHashCap(len(x))
		for k, val := range x {
			h.Set(object.NewString(k), rubyFromGo(val))
		}
		return h
	default:
		return object.NewString(fmt.Sprintf("%v", x))
	}
}

// graphqlResultToRuby reshapes a go-ruby-graphql Result into the Ruby Hash
// GraphQL::Schema#execute returns: a "data" key (present, possibly nil, for a
// document that reached execution) and an "errors" Array of {"message", and
// where present "locations"/"path"/"extensions"} hashes, exactly as
// graphql-ruby serialises them.
func graphqlResultToRuby(res graphql.Result) object.Value {
	h := object.NewHash()
	if d, ok := res["data"]; ok {
		h.Set(object.NewString("data"), rubyFromGo(d))
	}
	if errs := res.Errors(); errs != nil {
		h.Set(object.NewString("errors"), rubyFromGo(errs))
	}
	return h
}

// graphqlKwargs returns the trailing keyword Hash of a GraphQL entry point (the
// name:/args:/variables:/context: options), or nil when the last argument is not
// a Hash.
func graphqlKwargs(rest []object.Value) *object.Hash {
	if len(rest) == 0 {
		return nil
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}

// graphqlKwGet fetches a symbol-keyed keyword option, reporting ok=false when the
// Hash is absent or the key is missing.
func graphqlKwGet(h *object.Hash, key string) (object.Value, bool) {
	if h == nil {
		return object.NilV, false
	}
	return h.Get(object.Symbol(key))
}

// graphqlNameArg coerces a name argument (name:, a field name, an argument name)
// to a String, accepting either a String or a Symbol as graphql-ruby does.
func graphqlNameArg(v object.Value) string {
	switch x := v.(type) {
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	default:
		raise("TypeError", "no implicit conversion of %s into String", v.Inspect())
		return ""
	}
}

// graphqlTypeArg coerces a value used as a field / argument type to the wrapped
// GraphQLType, raising TypeError for anything that is not a GraphQL type.
func graphqlTypeArg(v object.Value) *GraphQLType {
	t, ok := v.(*GraphQLType)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into GraphQL type", v.Inspect())
	}
	return t
}
