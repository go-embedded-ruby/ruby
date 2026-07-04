// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"context"

	graphql "github.com/go-ruby-graphql/graphql"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerGraphQL installs the GraphQL module (require "graphql"): the built-in
// scalar types (GraphQL::Types::Int/Float/String/Boolean/ID), the programmatic
// schema builder (GraphQL::ObjectType.define with its field DSL, and the
// GraphQL.list_of / GraphQL.non_null wrappers), GraphQL::Schema.define plus
// #execute, and the GraphQL::Error / GraphQL::ExecutionError tree. The GraphQL
// class-macro DSL of graphql-ruby is replaced by an equivalent programmatic
// builder, but the runtime contract graphql-ruby guarantees — the type system,
// query execution semantics and the exact data / errors Hash shape — is
// preserved by delegating every schema and query to
// github.com/go-ruby-graphql/graphql, a pure-Go (cgo-free) Ruby-flavoured façade
// over github.com/graphql-go/graphql. The instance value types, the resolver
// bridge and the Go<->Ruby value conversions live in graphql_bind.go.
func (vm *VM) registerGraphQL() {
	mod := newClass("GraphQL", nil)
	mod.isModule = true
	vm.consts["GraphQL"] = mod

	vm.registerGraphQLErrors(mod)
	tc, oc := vm.registerGraphQLTypes(mod)
	vm.registerGraphQLObjectType(mod, tc, oc)
	vm.registerGraphQLSchema(mod)
}

// registerGraphQLErrors installs the GraphQL error tree: GraphQL::Error <
// StandardError and GraphQL::ExecutionError < GraphQL::Error. A resolver returns
// a GraphQL::ExecutionError to collect a client-facing field error; GraphQL::Error
// is raised for a schema that fails to validate. Each class is registered both as
// a nested constant of GraphQL and under its qualified name, exactly as the other
// bindings' error trees are.
func (vm *VM) registerGraphQLErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Error", "GraphQL::Error", std)
	ee := reg("ExecutionError", "GraphQL::ExecutionError", base)

	// GraphQL::ExecutionError.new(message) — the value a resolver returns to signal
	// a handled failure. #initialize stores @message (StandardError's protocol),
	// so #message and the field-error bridge both read it.
	ee.smethods["new"] = &Method{name: "new", owner: ee,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			o := &RObject{class: ee, ivars: map[string]object.Value{}}
			if len(args) > 0 {
				o.ivars["@message"] = object.NewString(args[0].ToS())
			}
			return o
		}}
}

// registerGraphQLTypes installs the type-system surface: the GraphQL::Type base
// class and the GraphQL::ObjectType class, the GraphQL::Types module holding the
// five built-in scalars, and the GraphQL.list_of / GraphQL.non_null wrapper
// helpers. It returns the Type and ObjectType classes so the object-type builder
// can stamp them onto the values it produces.
func (vm *VM) registerGraphQLTypes(mod *RClass) (typeCls, objCls *RClass) {
	typeCls = newClass("GraphQL::Type", vm.cObject)
	mod.consts["Type"] = typeCls
	vm.consts["GraphQL::Type"] = typeCls

	objCls = newClass("GraphQL::ObjectType", typeCls)
	mod.consts["ObjectType"] = objCls
	vm.consts["GraphQL::ObjectType"] = objCls

	types := newClass("GraphQL::Types", nil)
	types.isModule = true
	mod.consts["Types"] = types
	vm.consts["GraphQL::Types"] = types

	for name, t := range map[string]graphql.Type{
		"Int":     graphql.Int,
		"Float":   graphql.Float,
		"String":  graphql.String,
		"Boolean": graphql.Boolean,
		"ID":      graphql.ID,
	} {
		types.consts[name] = &GraphQLType{cls: typeCls, name: name, t: t}
	}

	// GraphQL.list_of(type) / GraphQL.non_null(type) — the [T] and T! wrappers,
	// mirroring graphql-ruby's list and non-null type modifiers.
	mod.smethods["list_of"] = &Method{name: "list_of", owner: mod,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			of := graphqlTypeArg(args[0])
			return &GraphQLType{cls: typeCls, name: "[" + of.name + "]", t: graphql.ListType(of.t)}
		}}
	mod.smethods["non_null"] = &Method{name: "non_null", owner: mod,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			of := graphqlTypeArg(args[0])
			return &GraphQLType{cls: typeCls, name: of.name + "!", t: graphql.NonNullType(of.t)}
		}}
	return typeCls, objCls
}

// registerGraphQLObjectType installs GraphQL::ObjectType.define and the field DSL
// yielded to its block. GraphQL::ObjectType.define(name:, description:) { |t| … }
// builds an object type from the fields declared with t.field(name, type, args:,
// description:) { |obj, args, ctx| … }; each field's block is the resolver,
// re-entered by the executor for every field of every query.
func (vm *VM) registerGraphQLObjectType(mod *RClass, typeCls, objCls *RClass) {
	dslCls := newClass("GraphQL::ObjectType::DSL", vm.cObject)
	objCls.consts["DSL"] = dslCls
	vm.consts["GraphQL::ObjectType::DSL"] = dslCls

	// t.field(name, type, args: {}, description: "") { |obj, args, ctx| … } — a
	// named, typed, resolvable field. The block is the resolver and is required;
	// args: maps argument names to their GraphQL types.
	dslCls.define("field", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		dsl := self.(*GraphQLObjectDSL)
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2+)", len(args))
		}
		if blk == nil {
			raise("ArgumentError", "no resolver block given for field %s", args[0].Inspect())
		}
		name := graphqlNameArg(args[0])
		ftype := graphqlTypeArg(args[1])
		kw := graphqlKwargs(args[2:])
		field := &graphql.Field{Type: ftype.t}
		if v, ok := graphqlKwGet(kw, "description"); ok {
			field.Description = v.ToS()
		}
		if v, ok := graphqlKwGet(kw, "args"); ok {
			field.Args = graphqlBuildArgs(v)
		}
		proc := blk
		field.Resolve = func(p graphql.ResolveParams) (interface{}, error) {
			return vm.graphqlResolve(proc, p)
		}
		dsl.fields[name] = field
		return object.NilV
	})

	// GraphQL::ObjectType.define(name:, description:) { |t| … } — builds an object
	// type; the block declares its fields through the yielded DSL. name: is
	// required; the block is required.
	objCls.smethods["define"] = &Method{name: "define", owner: objCls,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			kw := graphqlKwargs(args)
			nameV, ok := graphqlKwGet(kw, "name")
			if !ok {
				raise("ArgumentError", "missing keyword: :name")
			}
			if blk == nil {
				raise("ArgumentError", "no definition block given")
			}
			dsl := &GraphQLObjectDSL{cls: dslCls, vm: vm, fields: graphql.FieldMap{}}
			vm.callBlock(blk, []object.Value{dsl})
			cfg := graphql.ObjectTypeConfig{Name: graphqlNameArg(nameV), Fields: dsl.fields}
			if v, ok := graphqlKwGet(kw, "description"); ok {
				cfg.Description = v.ToS()
			}
			obj := graphql.NewObjectType(cfg)
			return &GraphQLType{cls: objCls, name: cfg.Name, t: obj, obj: obj}
		}}
}

// graphqlBuildArgs converts a Ruby args: option — a Hash mapping argument names
// to their GraphQL types — into the graphql.ArgumentMap a field is built with.
// A non-Hash args: option raises TypeError.
func graphqlBuildArgs(v object.Value) graphql.ArgumentMap {
	h, ok := v.(*object.Hash)
	if !ok {
		raise("TypeError", "args: must be a Hash, got %s", v.Inspect())
	}
	out := graphql.ArgumentMap{}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		out[graphqlKeyString(k)] = &graphql.Argument{Type: graphqlTypeArg(val).t}
	}
	return out
}

// registerGraphQLSchema installs GraphQL::Schema.define(query:, mutation:) and the
// #execute instance method. define validates the type system and raises
// GraphQL::Error for an invalid or query-less schema; #execute runs a query
// document and returns the graphql-ruby-shaped data / errors Hash.
func (vm *VM) registerGraphQLSchema(mod *RClass) {
	schemaCls := newClass("GraphQL::Schema", vm.cObject)
	mod.consts["Schema"] = schemaCls
	vm.consts["GraphQL::Schema"] = schemaCls

	schemaCls.smethods["define"] = &Method{name: "define", owner: schemaCls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			kw := graphqlKwargs(args)
			qv, ok := graphqlKwGet(kw, "query")
			if !ok {
				raise("GraphQL::Error", "missing keyword: :query")
			}
			cfg := graphql.SchemaConfig{Query: graphqlRootType(qv, "query")}
			if mv, ok := graphqlKwGet(kw, "mutation"); ok {
				cfg.Mutation = graphqlRootType(mv, "mutation")
			}
			sch, err := graphql.NewSchema(cfg)
			if err != nil {
				raise("GraphQL::Error", "%s", err.Error())
			}
			return &GraphQLSchema{cls: schemaCls, sch: sch}
		}}

	// schema.execute(query, variables:, operation_name:, context:, root_value:) →
	// the result Hash ({"data" => …, "errors" => …}).
	schemaCls.define("execute", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		query := strArg(args[0])
		kw := graphqlKwargs(args[1:])
		params := graphql.ExecuteParams{}
		if v, ok := graphqlKwGet(kw, "variables"); ok {
			if m, isMap := goFromRuby(v).(map[string]interface{}); isMap {
				params.Variables = m
			}
		}
		if v, ok := graphqlKwGet(kw, "operation_name"); ok {
			params.OperationName = v.ToS()
		}
		if v, ok := graphqlKwGet(kw, "root_value"); ok {
			if m, isMap := goFromRuby(v).(map[string]interface{}); isMap {
				params.RootValue = m
			}
		}
		ctxVal, _ := graphqlKwGet(kw, "context")
		params.Context = context.WithValue(context.Background(), graphqlCtxKey{}, ctxVal)
		res := self.(*GraphQLSchema).sch.Execute(query, params)
		return graphqlResultToRuby(res)
	})
}

// graphqlRootType coerces a schema root option (query:/mutation:) to the concrete
// *graphql.ObjectType it must be, raising GraphQL::Error when the value is not a
// GraphQL object type.
func graphqlRootType(v object.Value, role string) *graphql.ObjectType {
	t, ok := v.(*GraphQLType)
	if !ok || t.obj == nil {
		raise("GraphQL::Error", "%s root must be a GraphQL object type, got %s", role, v.Inspect())
	}
	return t.obj
}
