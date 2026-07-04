// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	date "github.com/go-ruby-date/date"
	mysql "github.com/go-ruby-mysql/mysql"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and github.com/go-ruby-mysql/mysql — a pure-Go (CGO=0) reimplementation of the
// Ruby mysql2 gem over github.com/go-sql-driver/mysql and database/sql. The
// connection, the SQL execution, and the MySQL<->Ruby type coercions live in
// that library (it hands back a small explicit value model: int64 / uint64 /
// float64 / Decimal / Date / time.Time / string / []byte / nil / bool); rbgo
// wraps each library object as a Ruby object reporting the matching Mysql2::*
// class (see mysql.go for the class + method registration) and turns the
// library's values into live Ruby objects here.
//
// The TCP connection is a real host seam: mysql.NewClient dials the server over
// go-sql-driver. The deterministic test suite drives the whole
// query -> result -> cast pipeline against an in-process, pure-Go
// MySQL-compatible server (github.com/dolthub/go-mysql-server) bound to an
// ephemeral 127.0.0.1 port, so it exercises real query execution with no
// external database and no leaked server (the server is test-only and never
// enters the runtime dependency graph).

// MySQLClient is the Ruby wrapper around a *mysql.Client (Mysql2::Client). qdef
// carries the client's default query options (mysql2's default_query_options),
// merged with any per-call options on #query.
type MySQLClient struct {
	cls  *RClass
	c    *mysql.Client
	qdef mysql.QueryOptions
}

// MySQLResult is the Ruby wrapper around a buffered *mysql.Result
// (Mysql2::Result). The result carries the shape (:hash / :array) and the
// symbolize_keys flag the query ran with, so #each renders each row accordingly.
type MySQLResult struct {
	cls *RClass
	r   *mysql.Result
}

// MySQLStatement is the Ruby wrapper around a prepared *mysql.Statement
// (Mysql2::Statement). It keeps its owning client so #execute's affected-row /
// last-id side effects are read back through Mysql2::Client.
type MySQLStatement struct {
	cls    *RClass
	s      *mysql.Statement
	client *MySQLClient
}

func (c *MySQLClient) ToS() string        { return "#<Mysql2::Client>" }
func (c *MySQLClient) Inspect() string    { return "#<Mysql2::Client>" }
func (c *MySQLClient) Truthy() bool       { return true }
func (r *MySQLResult) ToS() string        { return "#<Mysql2::Result>" }
func (r *MySQLResult) Inspect() string    { return "#<Mysql2::Result>" }
func (r *MySQLResult) Truthy() bool       { return true }
func (s *MySQLStatement) ToS() string     { return "#<Mysql2::Statement>" }
func (s *MySQLStatement) Inspect() string { return "#<Mysql2::Statement>" }
func (s *MySQLStatement) Truthy() bool    { return true }

// mysqlValue maps a cast column value (mysql.Value) into the Ruby object graph,
// following mysql2's MySQL->Ruby cast table: nil -> nil, bool -> true|false,
// int64 -> Integer, an over-int64 uint64 -> a (big) Integer, float64 -> Float,
// Decimal -> BigDecimal, Date -> Date, time.Time -> Time, string -> UTF-8
// String, []byte -> an ASCII-8BIT (binary) String. The library only ever
// produces this set; any other type stringifies (covered white-box).
func (vm *VM) mysqlValue(v mysql.Value) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(n)
	case int64:
		return object.IntValue(n)
	case uint64:
		return object.NormInt(new(big.Int).SetUint64(n))
	case float64:
		return object.Float(n)
	case mysql.Decimal:
		// A DECIMAL column's exact text always parses, so newDecimalString's
		// error path (covered by the bigdecimal binding) is not reached here.
		return newDecimalString(string(n))
	case mysql.Date:
		return mysqlDate(n)
	case stdtime.Time:
		return &Time{t: gotime.FromUnix(n.Unix())}
	case string:
		return object.NewString(n)
	case []byte:
		return object.NewStringBytesEnc(append([]byte(nil), n...), "ASCII-8BIT")
	}
	return object.NewString(mysqlSprint(v))
}

// mysqlDate maps a mysql.Date (a DATE column) to a Ruby Date. A value from the
// server is always a valid calendar date, so date.NewDate's error is unreachable
// in production (covered white-box); it re-raises as Mysql2::Error there.
func mysqlDate(d mysql.Date) object.Value {
	rd, err := date.NewDate(d.Year, d.Month, d.Day)
	if err != nil {
		raise("Mysql2::Error", "%s", err.Error())
	}
	return &Date{d: rd}
}

// mysqlSprint renders an unexpected cast value's default form (only reached for
// a value type outside mysql2's cast set).
func mysqlSprint(v mysql.Value) string {
	if s, ok := v.(interface{ String() string }); ok {
		return s.String()
	}
	return ""
}

// mysqlBind maps one Ruby bind value to the argument type the mysql library's
// prepared-statement path accepts (int64 / float64 / string / []byte / bool /
// time.Time / nil). A Bignum, Symbol, BigDecimal and Date pass as their exact
// text; a binary String passes as raw bytes; anything else falls back to #to_s.
func mysqlBind(v object.Value) any {
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
			return []byte(n.Bytes())
		}
		return n.Str()
	case object.Symbol:
		return string(n)
	case *Time:
		return stdtime.Unix(n.t.ToUnix(), 0).UTC()
	case *BigDecimal:
		return n.ToS()
	case *Date:
		return n.d.String()
	}
	return v.ToS()
}

// mysqlBinds maps a list of Ruby bind arguments to the library's argument model.
func mysqlBinds(args []object.Value) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = mysqlBind(a)
	}
	return out
}

// mysqlStr coerces an argument to its string: a String yields its contents, any
// other value its #to_s.
func mysqlStr(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// mysqlInt coerces an argument to an int64 (a port number, a timeout in
// seconds), raising TypeError for a non-integer — the mysql counterpart of
// pgIntArg.
func mysqlInt(v object.Value) int64 {
	if n, ok := v.(object.Integer); ok {
		return int64(n)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", v.Inspect())
	return 0
}

// mysqlDuration reads a whole-second timeout keyword (connect/read/write) as a
// time.Duration.
func mysqlDuration(v object.Value) stdtime.Duration {
	return stdtime.Duration(mysqlInt(v)) * stdtime.Second
}

// mysqlFlags reads the :flags keyword: an Array of names, or a single name.
func mysqlFlags(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = e.ToS()
		}
		return out
	}
	return []string{v.ToS()}
}

// mysqlHashArg returns args[i] as an *object.Hash, or nil when the index is out
// of range or the argument is not a Hash.
func mysqlHashArg(args []object.Value, i int) *object.Hash {
	if i < len(args) {
		if h, ok := args[i].(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// mysqlKeyName renders a keyword Hash key (Symbol or String) to its name.
func mysqlKeyName(k object.Value) string {
	switch n := k.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return k.ToS()
}

// mysqlConnectOptions reads Mysql2::Client.new's keyword Hash into a
// mysql.Options plus the default query options (mysql2's default_query_options:
// as / symbolize_keys / cast / cast_booleans). Both the connection keywords and
// their common aliases (username/user, password/pass, database/dbname) are
// honoured, matching the gem.
func mysqlConnectOptions(h *object.Hash) (mysql.Options, mysql.QueryOptions) {
	opts := mysql.Options{}
	qdef := mysql.DefaultQueryOptions()
	if h == nil {
		opts.QueryDefaults = &qdef
		return opts, qdef
	}
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		switch mysqlKeyName(k) {
		case "host":
			opts.Host = v.ToS()
		case "port":
			opts.Port = int(mysqlInt(v))
		case "username", "user":
			opts.Username = v.ToS()
		case "password", "pass":
			opts.Password = v.ToS()
		case "database", "dbname":
			opts.Database = v.ToS()
		case "socket":
			opts.Socket = v.ToS()
		case "encoding":
			opts.Encoding = v.ToS()
		case "flags":
			opts.Flags = mysqlFlags(v)
		case "connect_timeout":
			opts.ConnectTimeout = mysqlDuration(v)
		case "read_timeout":
			opts.ReadTimeout = mysqlDuration(v)
		case "write_timeout":
			opts.WriteTimeout = mysqlDuration(v)
		default:
			mysqlQueryDefault(&qdef, mysqlKeyName(k), v)
		}
	}
	opts.QueryDefaults = &qdef
	return opts, qdef
}

// mysqlQueryDefault applies one query-option keyword (as / symbolize_keys /
// cast / cast_booleans) to qo. An unrelated keyword is ignored, matching the
// gem's tolerance of options it does not recognise.
func mysqlQueryDefault(qo *mysql.QueryOptions, name string, v object.Value) {
	switch name {
	case "as":
		qo.As = mysqlAs(v)
	case "symbolize_keys":
		qo.SymbolizeKeys = v.Truthy()
	case "cast":
		qo.Cast = v.Truthy()
	case "cast_booleans":
		qo.CastBooleans = v.Truthy()
	}
}

// mysqlQueryOptions merges Mysql2::Client#query's per-call option Hash over the
// client's defaults (as / symbolize_keys / cast / cast_booleans).
func mysqlQueryOptions(base mysql.QueryOptions, h *object.Hash) mysql.QueryOptions {
	qo := base
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		mysqlQueryDefault(&qo, mysqlKeyName(k), v)
	}
	return qo
}

// mysqlAs maps an :as keyword value onto the library's row-shape string:
// "array" selects positional Array rows, anything else the default Hash rows.
func mysqlAs(v object.Value) string {
	if v.ToS() == "array" {
		return "array"
	}
	return "hash"
}

// raiseMySQL re-raises a library query/connection error as a Ruby Mysql2::Error
// (or a subclass named by clsName) carrying #message, #error_number and
// #sql_state. Every error the library returns is a *mysql.Error, so the
// assertion is total (a client-side failure carries number 0 / SQLSTATE
// "HY000").
func (vm *VM) raiseMySQL(clsName string, err error) {
	me := err.(*mysql.Error)
	cls := vm.consts[clsName].(*RClass)
	exc := &RObject{class: cls, ivars: map[string]object.Value{
		"@message":      object.NewString(me.Error()),
		"@error_number": object.IntValue(int64(me.ErrorNumber())),
		"@sql_state":    object.NewString(me.SQLState()),
	}}
	panic(vm.excError(exc))
}

// mysqlRow renders one result row as the Ruby value the result's shape selects:
// a positional Array (as: :array), or a Hash keyed by column name whose keys are
// Symbols when the query used symbolize_keys, Strings otherwise.
func (vm *VM) mysqlRow(r *mysql.Result, fields []string, row mysql.Row) object.Value {
	if r.As() == "array" {
		arr := object.NewArrayFromSlice(make([]object.Value, len(row)))
		for i, cell := range row {
			arr.Elems[i] = vm.mysqlValue(cell)
		}
		return arr
	}
	h := object.NewHash()
	for i, name := range fields {
		var key object.Value
		if r.SymbolizeKeys() {
			key = object.Symbol(name)
		} else {
			key = object.NewString(name)
		}
		h.Set(key, vm.mysqlValue(row[i]))
	}
	return h
}

// mysqlRows renders every result row (see mysqlRow).
func (vm *VM) mysqlRows(r *mysql.Result) []object.Value {
	fields := r.Fields()
	rows := r.Rows()
	out := make([]object.Value, len(rows))
	for i, row := range rows {
		out[i] = vm.mysqlRow(r, fields, row)
	}
	return out
}
