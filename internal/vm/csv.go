// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"regexp"

	libcsv "github.com/go-ruby-csv/csv"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and github.com/go-ruby-csv/csv — an MRI-4.0.5-byte-exact reimplementation of
// Ruby's "csv" standard library (require "csv"), a sibling of go-ruby-uri /
// go-ruby-date / go-ruby-json. The whole CSV dialect lives in that library: the
// RFC-style quoting with embedded quotes / separators / newlines inside quoted
// fields, the col_sep / row_sep / quote_char options, the headers model
// (CSV::Row / CSV::Table), the built-in field and header converters, and
// CSV::MalformedCSVError with MRI-exact messages and line numbers.
//
// rbgo only wraps the library's *csv.Table / *csv.Row in its Ruby CSV::Table /
// CSV::Row objects, maps a Ruby option Hash to csv.Options, lifts a parsed field
// to the matching Ruby value (nil / String / Integer / Float / Date / DateTime /
// Time / Symbol) and re-raises the library's MalformedCSVError as MRI's
// CSV::MalformedCSVError. There is no former pure-Ruby prelude CSV module: this
// is an additive binding (a new require "csv" feature), like net/http or openssl.

// CSVRow is the Ruby wrapper around a *csv.Row — CSV::Row. It offers access by
// header name or by index (Field / At) plus to_a / to_h / [] / headers, all
// delegating to the library. classOf reports CSV::Row for it.
type CSVRow struct{ r *libcsv.Row }

func (r *CSVRow) ToS() string     { return csvRowString(r.r) }
func (r *CSVRow) Inspect() string { return "#<CSV::Row " + csvRowString(r.r) + ">" }
func (r *CSVRow) Truthy() bool    { return true }

// csvRowString renders a row's CSV record text — the form CSV::Row#to_s / to_csv
// yields. It re-generates the single record through the library so the quoting
// matches MRI. GenerateLine is infallible for the scalar field types the binding
// produces, so its error is discarded (it is never non-nil here).
func csvRowString(r *libcsv.Row) string {
	s, _ := libcsv.GenerateLine(r.Fields, libcsv.Options{})
	return s
}

// CSVTable is the Ruby wrapper around a *csv.Table — CSV::Table: a header row
// plus its data Rows, with Row(i) / ToArray and by-name/index access.
type CSVTable struct{ t *libcsv.Table }

func (t *CSVTable) ToS() string     { return csvTableString(t.t) }
func (t *CSVTable) Inspect() string { return "#<CSV::Table " + csvTableString(t.t) + ">" }
func (t *CSVTable) Truthy() bool    { return true }

// csvTableString renders a table's CSV document — the header row followed by each
// data row, the form CSV::Table#to_s / to_csv yields. Generate is infallible for
// the scalar field types the binding produces, so its error is discarded.
func csvTableString(t *libcsv.Table) string {
	rows := make([][]any, 0, len(t.Rows)+1)
	rows = append(rows, t.Headers)
	for _, r := range t.Rows {
		rows = append(rows, r.Fields)
	}
	s, _ := libcsv.Generate(rows, libcsv.Options{})
	return s
}

// registerCSV installs the CSV class (require "csv") backed by the go-ruby-csv
// library: the class methods (parse / parse_line / generate / generate_line /
// foreach / read), the CSV::Row and CSV::Table value classes with their by-name
// and by-index access, and the CSV::MalformedCSVError exception. It runs after
// the exception hierarchy is in place (CSV::MalformedCSVError < StandardError)
// and after Date / DateTime / Time exist (the converters lift to those classes).
func (vm *VM) registerCSV() {
	cls := newClass("CSV", vm.cObject)
	vm.cCSV = cls
	vm.consts["CSV"] = cls

	vm.registerCSVError(cls)
	vm.registerCSVRowClass(cls)
	vm.registerCSVTableClass(cls)
	vm.registerCSVClassMethods(cls)

	// CSV#<< / #push: the writer CSV.generate yields to its block appends a row
	// and returns itself (so `csv << a << b` chains). Defined on the CSV class so
	// the csvSink receiver (which reports CSV) dispatches here.
	pushFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if s, ok := self.(*csvSink); ok {
			*s.rows = append(*s.rows, vm.csvRowFor(args[0]))
		}
		return self
	}
	cls.define("<<", pushFn)
	cls.define("push", pushFn)
}

// registerCSVError installs CSV::MalformedCSVError < StandardError — MRI's class
// for a malformed document. It is registered both as a nested constant of CSV
// (so Ruby `CSV::MalformedCSVError` resolves it) and under its qualified
// top-level name (so a re-raised library error's exceptionObject lookup finds
// the same class), exactly as the URI:: / Date:: error classes are.
func (vm *VM) registerCSVError(cls *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	mErr := newClass("CSV::MalformedCSVError", std)
	cls.consts["MalformedCSVError"] = mErr
	vm.consts["CSV::MalformedCSVError"] = mErr
}

// raiseCSVErr re-raises a library error as the matching Ruby exception: a
// *MalformedCSVError becomes CSV::MalformedCSVError (its message reproduced
// verbatim — it already matches MRI, line number and all); anything else becomes
// a plain RuntimeError. It never returns when err is non-nil.
func raiseCSVErr(err error) {
	if err == nil {
		return
	}
	if m, ok := err.(*libcsv.MalformedCSVError); ok {
		raise("CSV::MalformedCSVError", "%s", m.Error())
	}
	raise("RuntimeError", "%s", err.Error())
}

// registerCSVClassMethods installs the CSV class methods backed by the library:
// CSV.parse / parse_line / generate / generate_line / foreach / read. Each maps
// the trailing option Hash to csv.Options and re-raises a library error.
func (vm *VM) registerCSVClassMethods(cls *RClass) {
	sm := func(name string, fn NativeFn) { cls.smethods[name] = &Method{name: name, owner: cls, native: fn} }

	// CSV.parse_line(str, **opts) -> the row's fields (Array), or nil for an empty
	// line (Ruby returns nil for ""). With headers: set MRI returns a CSV::Row
	// (the first record), so the headered case routes through Parse and lifts its
	// first data row — matching MRI's parse_line-with-headers result.
	sm("parse_line", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		opts := vm.csvOptions(args, 1)
		if csvHasHeaders(opts) {
			return vm.csvParseLineRow(strArg(args[0]), opts)
		}
		fields, err := libcsv.ParseLine(strArg(args[0]), opts)
		raiseCSVErr(err)
		if fields == nil {
			return object.NilV
		}
		return vm.csvFieldsToArray(fields)
	})

	// CSV.parse(str, **opts) -> [[..],..] without headers, or a CSV::Table with
	// headers: set. With a block, each row (Array or CSV::Row) is yielded and nil
	// is returned, mirroring CSV.parse(str) { |row| ... }.
	sm("parse", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.csvParse(strArg(args[0]), vm.csvOptions(args, 1), blk)
	})

	// CSV.generate_line(row, **opts) -> a single CSV record String (with the row
	// separator), mirroring CSV.generate_line.
	sm("generate_line", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := libcsv.GenerateLine(vm.csvRowFor(args[0]), vm.csvOptions(args, 1))
		raiseCSVErr(err)
		return object.NewString(s)
	})

	// CSV.generate(**opts) { |csv| csv << row ... } and CSV.generate(rows, **opts)
	// -> a CSV document String. The block form accumulates rows pushed with <<.
	sm("generate", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.csvGenerate(args, blk)
	})

	// CSV.foreach over a *string* (not a path): yields each parsed row. The String
	// receiver keeps the binding filesystem-free; CSV.read covers the path case.
	sm("foreach", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		vm.csvParse(strArg(args[0]), vm.csvOptions(args, 1), blk)
		return object.NilV
	})

	// CSV.read(path, **opts) reads the file at path and parses it, returning the
	// same result CSV.parse would (an Array of Arrays, or a CSV::Table).
	sm("read", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		data, err := os.ReadFile(pathArg(vm, args[0]))
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", strArg(args[0]))
		}
		return vm.csvParse(string(data), vm.csvOptions(args, 1), nil)
	})
}

// csvParse parses data, yielding each row to blk when given (returning nil), or
// returning the whole result otherwise: an Array of row Arrays without headers,
// or a CSV::Table with headers set. It is shared by CSV.parse / foreach / read.
func (vm *VM) csvParse(data string, opts libcsv.Options, blk *Proc) object.Value {
	result, err := libcsv.Parse(data, opts)
	raiseCSVErr(err)
	if tbl, ok := result.(*libcsv.Table); ok {
		if blk != nil {
			for _, r := range tbl.Rows {
				vm.callBlock(blk, []object.Value{&CSVRow{r: r}})
			}
			return object.NilV
		}
		return &CSVTable{t: tbl}
	}
	rows, _ := result.([][]any)
	if blk != nil {
		for _, row := range rows {
			vm.callBlock(blk, []object.Value{vm.csvFieldsToArray(row)})
		}
		return object.NilV
	}
	out := make([]object.Value, len(rows))
	for i, row := range rows {
		out[i] = vm.csvFieldsToArray(row)
	}
	return object.NewArrayFromSlice(out)
}

// csvHasHeaders reports whether the options request headers — true / "first_row"
// / a []string of names. A false or absent Headers reads as no headers.
func csvHasHeaders(o libcsv.Options) bool {
	switch h := o.Headers.(type) {
	case bool:
		return h
	case string:
		return h != ""
	case []string:
		return true
	default:
		return false
	}
}

// csvParseLineRow implements CSV.parse_line with headers: the first record is
// parsed as a one-row table and its first data Row returned (a CSV::Row), or nil
// when the line yields no record — matching MRI's headered parse_line.
func (vm *VM) csvParseLineRow(line string, opts libcsv.Options) object.Value {
	result, err := libcsv.Parse(line, opts)
	raiseCSVErr(err)
	tbl, ok := result.(*libcsv.Table)
	if !ok {
		return object.NilV
	}
	if r, ok := tbl.Row(0); ok {
		return &CSVRow{r: r}
	}
	return object.NilV
}

// csvGenerate implements CSV.generate: with a block, an accumulator object is
// yielded whose << pushes a row; the rendered document is returned. With a
// leading Array argument the rows are taken from it directly (CSV.generate(rows)).
func (vm *VM) csvGenerate(args []object.Value, blk *Proc) object.Value {
	optsAt := 0
	var rows [][]any
	if len(args) > 0 {
		if arr, ok := args[0].(*object.Array); ok {
			optsAt = 1
			for _, e := range arr.Elems {
				rows = append(rows, vm.csvRowFor(e))
			}
		}
	}
	opts := vm.csvOptions(args, optsAt)
	if blk != nil {
		sink := &csvSink{rows: &rows}
		vm.callBlock(blk, []object.Value{sink})
	}
	s, err := libcsv.Generate(rows, opts)
	raiseCSVErr(err)
	return object.NewString(s)
}

// csvSink is the accumulator object CSV.generate yields to its block: its <<
// appends a row to the pending rows and returns the sink (so `csv << a << b`
// chains), mirroring the CSV writer the block receives in MRI. classOf reports
// CSV for it, so the << / push methods registered on the CSV class dispatch.
type csvSink struct {
	rows *[][]any
}

func (s *csvSink) ToS() string     { return "#<CSV>" }
func (s *csvSink) Inspect() string { return "#<CSV>" }
func (s *csvSink) Truthy() bool    { return true }

// csvFieldsToArray lifts a parsed library row ([]any) to a Ruby Array of Ruby
// field values.
func (vm *VM) csvFieldsToArray(fields []any) *object.Array {
	out := make([]object.Value, len(fields))
	for i, f := range fields {
		out[i] = vm.csvFieldToRuby(f)
	}
	return object.NewArrayFromSlice(out)
}

// csvFieldToRuby maps a single parsed field to its Ruby value: nil -> nil, a
// string -> String, an int -> Integer, a float64 -> Float, a *csv.Date /
// *csv.DateTime re-parsed through rbgo's Date / DateTime (or Time, for the :time
// converter), a csv.Symbol -> Symbol. A header-converted value reuses the same
// mapping (a converted header is a string or Symbol).
func (vm *VM) csvFieldToRuby(f any) object.Value {
	switch v := f.(type) {
	case nil:
		return object.NilV
	case string:
		return object.NewString(v)
	case int:
		return object.IntValue(int64(v))
	case float64:
		return object.Float(v)
	case libcsv.Symbol:
		return object.Symbol(string(v))
	case libcsv.Date:
		return vm.csvDate(v.Raw)
	case libcsv.DateTime:
		return vm.csvDateTime(v.Raw, v.Time)
	case object.Value:
		// A :nil_value / :empty_value substitution is stored as the original Ruby
		// value (an object.Value) so it round-trips byte-for-byte — an Integer stays
		// an Integer, a Symbol a Symbol — rather than being re-derived from a Go form.
		return v
	default:
		// The library surfaces only the field types enumerated above; anything else
		// is rendered through one-field generation so the mapping stays total.
		s, _ := libcsv.GenerateLine([]any{v}, libcsv.Options{})
		return object.NewString(s)
	}
}

// csvDate re-parses a :date converter's raw text through rbgo's now-bound Date,
// so the Ruby value is a genuine Date object (the library hands us only the raw
// shape, leaving the calendar to the binding's Date). A parse failure raises
// Date::Error, as Date.parse would.
func (vm *VM) csvDate(raw string) object.Value {
	return vm.send(vm.cDate, "parse", []object.Value{object.NewString(raw)}, nil)
}

// csvDateTime re-parses a :date_time / :time converter's raw text: when isTime
// the result is a Ruby Time (the :time converter), otherwise a DateTime. Each is
// produced through the matching rbgo class method so the value is a real
// Time / DateTime object.
func (vm *VM) csvDateTime(raw string, isTime bool) object.Value {
	cls := vm.cDateTime
	if isTime {
		cls = vm.cTime
	}
	return vm.send(cls, "parse", []object.Value{object.NewString(raw)}, nil)
}

// csvRowFor coerces a Ruby value to a library generate-row ([]any): an Array's
// elements are each lowered to their Go form; a CSV::Row contributes its raw
// fields. Anything else raises TypeError, matching MRI's generate_line which
// expects an Array(-like) row.
func (vm *VM) csvRowFor(v object.Value) []any {
	switch x := v.(type) {
	case *object.Array:
		row := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			row[i] = vm.csvFieldFromRuby(e)
		}
		return row
	case *CSVRow:
		return x.r.Fields
	default:
		raise("TypeError", "no implicit conversion of %s into Array", classNameOf(v))
		return nil
	}
}

// csvFieldFromRuby lowers a Ruby field value to the Go scalar the library
// generator stringifies: nil -> nil (an empty unquoted field), String -> string,
// Integer -> int64, Float -> float64, Symbol -> its name. Anything else is sent
// #to_s so an arbitrary object renders as MRI's generate would.
func (vm *VM) csvFieldFromRuby(v object.Value) any {
	switch x := v.(type) {
	case object.Nil:
		return nil
	case *object.String:
		return x.Str()
	case object.Integer:
		return int64(x)
	case object.Float:
		return float64(x)
	case object.Symbol:
		return string(x)
	default:
		return strArg(vm.send(v, "to_s", nil, nil))
	}
}

// csvOptions maps the trailing option Hash (at index i, if any) to csv.Options.
// Every Ruby option key (col_sep:/row_sep:/quote_char:/headers:/converters:/
// header_converters:/skip_blanks:/skip_lines:/liberal_parsing:/force_quotes:/
// strip:/nil_value:/empty_value:/write_headers:/return_headers:/quote_empty:) is
// translated to the library's field; an absent key keeps the library default.
func (vm *VM) csvOptions(args []object.Value, i int) libcsv.Options {
	var o libcsv.Options
	if i >= len(args) {
		return o
	}
	h, ok := args[i].(*object.Hash)
	if !ok {
		return o
	}
	get := func(key string) (object.Value, bool) { return h.Get(object.Symbol(key)) }

	if v, ok := get("col_sep"); ok {
		o.ColSep = strArg(v)
	}
	if v, ok := get("row_sep"); ok {
		o.RowSep = strArg(v)
	}
	if v, ok := get("quote_char"); ok {
		if _, isNil := v.(object.Nil); isNil {
			o.NoQuote = true
		} else {
			o.QuoteChar = strArg(v)
		}
	}
	if v, ok := get("headers"); ok {
		o.Headers = csvHeadersOpt(v)
	}
	if v, ok := get("return_headers"); ok {
		o.ReturnHeaders = v.Truthy()
	}
	if v, ok := get("write_headers"); ok {
		o.WriteHeaders = v.Truthy()
	}
	if v, ok := get("converters"); ok {
		o.Converters = csvConverterNames(v)
	}
	if v, ok := get("header_converters"); ok {
		o.HeaderConverters = csvConverterNames(v)
	}
	if v, ok := get("skip_blanks"); ok {
		o.SkipBlanks = v.Truthy()
	}
	if v, ok := get("skip_lines"); ok {
		o.SkipLines = csvSkipLines(v)
	}
	if v, ok := get("liberal_parsing"); ok {
		o.LiberalParsing = v.Truthy()
	}
	if v, ok := get("force_quotes"); ok {
		o.ForceQuotes = v.Truthy()
	}
	if v, ok := get("strip"); ok {
		csvStripOpt(&o, v)
	}
	if v, ok := get("quote_empty"); ok {
		o.QuoteEmpty = v.Truthy()
		o.QuoteEmptySet = true
	}
	// nil_value / empty_value carry the original Ruby value through the library
	// (Options.*Value is `any`), so csvFieldToRuby returns it unchanged — a
	// substituted Integer stays an Integer, a Symbol a Symbol.
	if v, ok := get("nil_value"); ok {
		o.NilValue = v
		o.NilValueSet = true
	}
	if v, ok := get("empty_value"); ok {
		o.EmptyValue = v
		o.EmptyValueSet = true
	}
	return o
}

// csvHeadersOpt maps the :headers value to the library's Headers field: true ->
// true (first row), false / nil -> false (no headers), a String such as
// "first_row" passes through, an Array of names -> []string. Any other value is
// treated as truthy (first-row headers), matching MRI's coercion.
func csvHeadersOpt(v object.Value) any {
	switch x := v.(type) {
	case object.Bool:
		return bool(x)
	case object.Nil:
		return false
	case *object.String:
		return x.Str()
	case *object.Array:
		names := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			names[i] = strArg(e)
		}
		return names
	default:
		return v.Truthy()
	}
}

// csvConverterNames maps the :converters / :header_converters value to the
// library's []string of converter names: a single Symbol or String is one name;
// an Array yields each element's name. A nil / empty value yields no converters.
func csvConverterNames(v object.Value) []string {
	switch x := v.(type) {
	case object.Symbol:
		return []string{string(x)}
	case *object.String:
		return []string{x.Str()}
	case *object.Array:
		names := make([]string, 0, len(x.Elems))
		for _, e := range x.Elems {
			names = append(names, csvName(e))
		}
		return names
	default:
		return nil
	}
}

// csvName renders a converter-name element (a Symbol or String) as its text.
func csvName(v object.Value) string {
	if s, ok := v.(object.Symbol); ok {
		return string(s)
	}
	return strArg(v)
}

// csvSkipLines maps the :skip_lines value to the library's RE2 source: a Regexp
// contributes its source, a String its literal text (MRI accepts either). The
// source is compile-checked here so an invalid pattern raises a Ruby RegexpError
// rather than reaching the library's MustCompile (which would panic); MRI's CSV
// likewise rejects a bad :skip_lines pattern at parse time.
func csvSkipLines(v object.Value) string {
	var src string
	if r, ok := v.(*Regexp); ok {
		src = r.source
	} else {
		src = strArg(v)
	}
	if _, err := regexp.Compile(src); err != nil {
		raise("RegexpError", "%s: /%s/", err.Error(), src)
	}
	return src
}

// csvStripOpt maps the :strip value: true -> strip whitespace, a String -> strip
// those specific characters. A falsey value leaves stripping off.
func csvStripOpt(o *libcsv.Options, v object.Value) {
	if s, ok := v.(*object.String); ok {
		o.StripChars = s.Str()
		return
	}
	o.StripSpace = v.Truthy()
}

// registerCSVRowClass installs CSV::Row with its by-name / by-index access and
// the to_a / to_h / headers / field / [] methods, all delegating to the library.
func (vm *VM) registerCSVRowClass(cls *RClass) {
	row := newClass("CSV::Row", vm.cObject)
	cls.consts["Row"] = row
	vm.consts["CSV::Row"] = row
	vm.cCSVRow = row

	rowOf := func(v object.Value) *libcsv.Row { return v.(*CSVRow).r }

	d := func(name string, fn NativeFn) { row.define(name, fn) }

	// [] / field: an Integer index (At) or a header name (Field). A missing
	// header or out-of-range index reads as nil, as CSV::Row#[] does.
	idx := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		r := rowOf(v)
		if n, ok := args[0].(object.Integer); ok {
			if val, ok := r.At(int(n)); ok {
				return vm.csvFieldToRuby(val)
			}
			return object.NilV
		}
		if val, ok := r.Field(csvLookupKey(args[0])); ok {
			return vm.csvFieldToRuby(val)
		}
		return object.NilV
	}
	d("[]", idx)
	d("field", idx)

	d("headers", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.csvHeadersArray(rowOf(v).Headers)
	})
	d("fields", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.csvFieldsToArray(rowOf(v).Fields)
	})
	d("to_a", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.csvRowPairs(rowOf(v))
	})
	d("to_h", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.csvRowHash(rowOf(v))
	})
	d("to_hash", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.csvRowHash(rowOf(v))
	})
	d("header_row?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(rowOf(v).HeaderRow)
	})
	toCSV := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(csvRowString(rowOf(v)))
	}
	d("to_s", toCSV)
	d("to_csv", toCSV)
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		r := rowOf(v)
		for i, h := range r.Headers {
			var f any
			if i < len(r.Fields) {
				f = r.Fields[i]
			}
			pair := object.NewArray(vm.csvFieldToRuby(h), vm.csvFieldToRuby(f))
			vm.callBlock(blk, []object.Value{pair})
		}
		return v
	})
}

// csvLookupKey coerces a CSV::Row#[] header key to the Go value the library's
// Field compares against a stored header: a Ruby Symbol becomes a csv.Symbol, a
// String its text — so :a finds a :symbol-converted header and "a" a plain one,
// matching MRI where the two are distinct.
func csvLookupKey(v object.Value) any {
	switch x := v.(type) {
	case object.Symbol:
		return libcsv.Symbol(string(x))
	case *object.String:
		return x.Str()
	default:
		// Any other key (e.g. a Float) cannot equal a string/Symbol header, so it
		// is passed through unchanged and Field misses — reading as nil, as MRI's
		// CSV::Row#[] does for a non-Integer, non-name key.
		return v
	}
}

// csvHeadersArray lifts a row/table's header slice ([]any of string or
// csv.Symbol) to a Ruby Array.
func (vm *VM) csvHeadersArray(headers []any) *object.Array {
	out := make([]object.Value, len(headers))
	for i, h := range headers {
		out[i] = vm.csvFieldToRuby(h)
	}
	return object.NewArrayFromSlice(out)
}

// csvRowPairs renders CSV::Row#to_a — an Array of [header, value] pairs in
// column order (MRI's Row#to_a / to_ary form).
func (vm *VM) csvRowPairs(r *libcsv.Row) *object.Array {
	out := make([]object.Value, len(r.Headers))
	for i, h := range r.Headers {
		var f any
		if i < len(r.Fields) {
			f = r.Fields[i]
		}
		out[i] = object.NewArray(vm.csvFieldToRuby(h), vm.csvFieldToRuby(f))
	}
	return object.NewArrayFromSlice(out)
}

// csvRowHash renders CSV::Row#to_h — an ordered header->value Hash (a repeated
// header keeps the last value, as MRI's to_h does).
func (vm *VM) csvRowHash(r *libcsv.Row) *object.Hash {
	hash := object.NewHash()
	for i, h := range r.Headers {
		var f any
		if i < len(r.Fields) {
			f = r.Fields[i]
		}
		hash.Set(vm.csvFieldToRuby(h), vm.csvFieldToRuby(f))
	}
	return hash
}

// registerCSVTableClass installs CSV::Table with its by-index row access and the
// to_a / headers / [] / each / size methods, delegating to the library.
func (vm *VM) registerCSVTableClass(cls *RClass) {
	tbl := newClass("CSV::Table", vm.cObject)
	cls.consts["Table"] = tbl
	vm.consts["CSV::Table"] = tbl
	vm.cCSVTable = tbl

	tblOf := func(v object.Value) *libcsv.Table { return v.(*CSVTable).t }
	d := func(name string, fn NativeFn) { tbl.define(name, fn) }

	d("headers", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.csvHeadersArray(tblOf(v).Headers)
	})
	d("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if r, ok := tblOf(v).Row(int(intArg(args[0]))); ok {
			return &CSVRow{r: r}
		}
		return object.NilV
	})
	d("first", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if r, ok := tblOf(v).Row(0); ok {
			return &CSVRow{r: r}
		}
		return object.NilV
	})
	d("to_a", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.csvTableArray(tblOf(v))
	})
	tblCSV := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(csvTableString(tblOf(v)))
	}
	d("to_s", tblCSV)
	d("to_csv", tblCSV)
	d("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(tblOf(v).Rows)))
	})
	d("length", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(tblOf(v).Rows)))
	})
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		for _, r := range tblOf(v).Rows {
			vm.callBlock(blk, []object.Value{&CSVRow{r: r}})
		}
		return v
	})
}

// csvTableArray renders CSV::Table#to_a — the header row followed by each data
// row as an Array of values (MRI's Table#to_a form: [[h1,h2],[v1,v2],...]).
func (vm *VM) csvTableArray(t *libcsv.Table) *object.Array {
	out := make([]object.Value, 0, len(t.Rows)+1)
	out = append(out, vm.csvHeadersArray(t.Headers))
	for _, r := range t.Rows {
		out = append(out, vm.csvFieldsToArray(r.Fields))
	}
	return object.NewArrayFromSlice(out)
}
