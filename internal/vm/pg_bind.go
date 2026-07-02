// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	pg "github.com/go-ruby-pg/pg"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent PostgreSQL v3 wire implementation of
// github.com/go-ruby-pg/pg. The protocol codec, the MD5/SCRAM authentication
// math, the OID type decoders and the PG::Result-shaped result layer live in
// that library; rbgo supplies the host seam — the TCP socket — and maps Ruby
// query arguments onto the library's Exec / ExecParams / ExecPrepared calls and
// the decoded column values back into the object graph (nil -> nil, bool ->
// true|false, int64 -> Integer, float64 -> Float, string -> String, []byte ->
// ASCII-8BIT String, time.Time -> a String timestamp, []any -> Array).
//
// rbgo has no live TCP socket yet, so the socket seam is the same injected
// IO-like object the redis binding uses (rubyConn bridges #read/#write to the
// library's io.ReadWriter Conn). PG.connect drives the StartupMessage + auth
// handshake over it via the library's PasswordAuthenticator when user/password
// keywords are supplied.

// PGResultObj is the Ruby wrapper around a *pg.Result (PG::Result). ntuples /
// nfields / fields / getvalue / values / [] map straight onto the library's
// methods; a nil result (a bodyless command) is still a valid zero-row Result.
type PGResultObj struct {
	cls *RClass
	res *pg.Result
}

func (r *PGResultObj) ToS() string     { return "#<PG::Result>" }
func (r *PGResultObj) Inspect() string { return "#<PG::Result>" }
func (r *PGResultObj) Truthy() bool    { return true }

// pgValue maps a decoded column value (pg.Result.Getvalue's any) into the object
// graph. The library decodes per OID to a small Go set; a NULL cell is nil.
func (vm *VM) pgValue(v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(n)
	case int64:
		return object.Integer(n)
	case int:
		return object.Integer(int64(n))
	case float64:
		return object.Float(n)
	case string:
		return object.NewString(n)
	case []byte:
		return &object.String{B: n, Enc: "ASCII-8BIT"}
	case time.Time:
		return object.NewString(n.Format("2006-01-02 15:04:05.999999999-07"))
	case []any:
		return vm.pgArray(n)
	}
	// The decoders only ever produce the cases above; anything else stringifies.
	return object.NewString(pgSprint(v))
}

// pgArray maps a decoded PostgreSQL array column ([]any) to a Ruby Array.
func (vm *VM) pgArray(vals []any) *object.Array {
	arr := &object.Array{Elems: make([]object.Value, len(vals))}
	for i, e := range vals {
		arr.Elems[i] = vm.pgValue(e)
	}
	return arr
}

// pgSprint renders an unexpected decoded value's default form (only reached for
// a decoder type outside the mapped set).
func pgSprint(v any) string {
	if s, ok := v.(interface{ String() string }); ok {
		return s.String()
	}
	return ""
}

// pgArgs maps the Ruby positional bind arguments of exec_params / exec_prepared
// to the library's `any` argument model. The library's EncodeParam stringifies
// each in text format, so the primitive Go types it understands are kept and any
// other value falls back to its to_s.
func pgArgs(vals []object.Value) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = pgArg(v)
	}
	return out
}

// pgArg maps one Ruby bind value to a library argument.
func pgArg(v object.Value) any {
	switch n := v.(type) {
	case nil, object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		return n.I.String()
	case object.Float:
		return float64(n)
	case *object.String:
		if n.IsBinary() {
			return []byte(n.B)
		}
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

// pgParamArray reads an exec_params/exec_prepared params argument: a single
// trailing Array is spread into its elements (the pg gem passes params as one
// Array), otherwise the arguments are taken as-is.
func pgParamArray(args []object.Value) []object.Value {
	if len(args) == 1 {
		if arr, ok := args[0].(*object.Array); ok {
			return arr.Elems
		}
	}
	return args
}

// raisePGError re-raises a library query error as a PG::Error. A *pg.Error
// carries the server's SQLSTATE and message; any other error (a transport fault)
// raises PG::ConnectionBad. It never returns (raise panics).
func raisePGError(err error) {
	if pe, ok := err.(*pg.Error); ok {
		raise("PG::Error", "%s", pe.Error())
	}
	raise("PG::ConnectionBad", "%s", err.Error())
}

// --- argument coercion -----------------------------------------------------

// pgArg0 returns the first argument, raising an ArgumentError when there is none.
func pgArg0(args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return args[0]
}

// pgStringArg coerces an argument to its string: a String yields its contents,
// any other value its to_s.
func pgStringArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// pgIntArg coerces an argument to an int64, raising a TypeError for a
// non-integer.
func pgIntArg(v object.Value) int64 {
	switch n := v.(type) {
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		return n.I.Int64()
	}
	raise("TypeError", "no implicit conversion to Integer")
	return 0
}

// pgStrings maps a []string to a Ruby Array of Strings.
func pgStrings(ss []string) *object.Array {
	arr := &object.Array{Elems: make([]object.Value, len(ss))}
	for i, s := range ss {
		arr.Elems[i] = object.NewString(s)
	}
	return arr
}

// pgConnectArgs reads the PG.connect argument list. A trailing keyword Hash may
// carry connection: / conn: (the IO seam) and the StartupMessage parameters
// (user:, dbname:/database:, password:, application_name:, client_encoding:, …).
// It returns the IO object, the StartupMessage params (user + non-password,
// non-connection keys), the user/password for authentication and whether a
// password was supplied.
func pgConnectArgs(args []object.Value) (io object.Value, params pg.StartupParams, user, password string, hasAuth bool) {
	params = pg.StartupParams{}
	if len(args) == 0 {
		return nil, params, "", "", false
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok {
		return nil, params, "", "", false
	}
	for i := 0; i < h.Len(); i++ {
		k := h.Keys[i]
		v, _ := h.Get(k)
		name := pgKeyName(k)
		switch name {
		case "connection", "conn":
			io = v
		case "password":
			password = pgStringArg(v)
			hasAuth = true
		case "user":
			user = pgStringArg(v)
			params["user"] = user
		case "dbname", "database":
			params["database"] = pgStringArg(v)
		case "host", "port", "hostaddr":
			// Transport keywords belong to the socket owner, not the wire params.
		default:
			params[name] = pgStringArg(v)
		}
	}
	return io, params, user, password, hasAuth
}

// pgKeyName renders a keyword Hash key (Symbol or String) to its name.
func pgKeyName(k object.Value) string {
	switch n := k.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return k.ToS()
}
