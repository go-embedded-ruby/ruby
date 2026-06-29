// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCSVBinding exercises the CSV class backed by the go-ruby-csv library
// (internal/vm/csv.go): the class methods (parse / parse_line / generate /
// generate_line / foreach / read), every option mapped to csv.Options, the
// CSV::Row / CSV::Table value classes and their field-value lifting, and the
// CSV::MalformedCSVError re-raise. Each expectation is pinned against MRI 4.0.5.
func TestCSVBinding(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// --- require returns true first call, false after -------------------
		{"require_true", `p require "csv"`, "true\n"},
		{"require_false", `require "csv"; p require "csv"`, "false\n"},

		// --- parse_line -----------------------------------------------------
		{"parse_line_basic", `require "csv"; p CSV.parse_line('a,"b,c",d')`, "[\"a\", \"b,c\", \"d\"]\n"},
		{"parse_line_empty", `require "csv"; p CSV.parse_line("")`, "nil\n"},
		{"parse_line_nil_field", `require "csv"; p CSV.parse_line("a,,b")`, "[\"a\", nil, \"b\"]\n"},
		{"parse_line_col_sep", `require "csv"; p CSV.parse_line("a;b", col_sep: ";")`, "[\"a\", \"b\"]\n"},
		{"parse_line_row_sep", `require "csv"; p CSV.parse_line("a|b\rc|d", row_sep: "\r")`, "[\"a|b\"]\n"},
		{"parse_line_noquote", `require "csv"; p CSV.parse_line('a"b', quote_char: nil)`, "[\"a\\\"b\"]\n"},
		{"parse_line_quote_char", `require "csv"; p CSV.parse_line("'a,b',c", quote_char: "'")`, "[\"a,b\", \"c\"]\n"},
		{"parse_line_liberal", `require "csv"; p CSV.parse_line('a,"b"c', liberal_parsing: true)`, "[\"a\", \"\\\"b\\\"c\"]\n"},
		{"parse_line_strip", `require "csv"; p CSV.parse_line("  a , b ", strip: true)`, "[\"a\", \"b\"]\n"},
		{"parse_line_strip_chars", `require "csv"; p CSV.parse_line("xxaxx,xxbxx", strip: "x")`, "[\"a\", \"b\"]\n"},
		{"parse_line_strip_false", `require "csv"; p CSV.parse_line(" a , b ", strip: false)`, "[\" a \", \" b \"]\n"},
		{"parse_line_headers_names", `require "csv"; p CSV.parse_line("1,2", headers: ["x","y"])["x"]`, "\"1\"\n"},

		// --- parse (no headers) ---------------------------------------------
		{"parse_rows", `require "csv"; p CSV.parse("a,b\nc,d\n")`, "[[\"a\", \"b\"], [\"c\", \"d\"]]\n"},
		{"parse_skip_blanks", `require "csv"; p CSV.parse("a\n\nb\n", skip_blanks: true)`, "[[\"a\"], [\"b\"]]\n"},
		{"parse_skip_lines_re", `require "csv"; p CSV.parse("#x\na,b\n", skip_lines: /^#/)`, "[[\"a\", \"b\"]]\n"},
		{"parse_skip_lines_str", `require "csv"; p CSV.parse("#x\na,b\n", skip_lines: "\\A#")`, "[[\"a\", \"b\"]]\n"},
		{"parse_block", `require "csv"; CSV.parse("a,b\nc,d\n") { |r| p r }`, "[\"a\", \"b\"]\n[\"c\", \"d\"]\n"},

		// --- nil_value / empty_value ----------------------------------------
		{"parse_nil_value", `require "csv"; p CSV.parse_line(",", nil_value: 0)`, "[0, 0]\n"},
		{"parse_empty_value", `require "csv"; p CSV.parse_line('"",x', empty_value: "Z")`, "[\"Z\", \"x\"]\n"},

		// --- converters ------------------------------------------------------
		{"conv_integer", `require "csv"; p CSV.parse_line("1,x", converters: [:integer])`, "[1, \"x\"]\n"},
		{"conv_float", `require "csv"; p CSV.parse_line("1.5,x", converters: [:float])`, "[1.5, \"x\"]\n"},
		{"conv_numeric", `require "csv"; p CSV.parse_line("1,2.5,x", converters: :numeric)`, "[1, 2.5, \"x\"]\n"},
		{"conv_string_name", `require "csv"; p CSV.parse_line("1,x", converters: "integer")`, "[1, \"x\"]\n"},
		{"conv_date_class", `require "csv"; require "date"; p CSV.parse_line("2020-01-02", converters: [:date]).first.class`, "Date\n"},
		{"conv_date_value", `require "csv"; require "date"; p CSV.parse_line("2020-01-02", converters: [:date]).first.to_s`, "\"2020-01-02\"\n"},

		// --- header_converters ----------------------------------------------
		{"hconv_symbol", `require "csv"; p CSV.parse("A,B\n1,2\n", headers: true, header_converters: :symbol).headers`, "[:a, :b]\n"},
		{"hconv_downcase", `require "csv"; p CSV.parse("A,B\n1,2\n", headers: true, header_converters: [:downcase]).headers`, "[\"a\", \"b\"]\n"},

		// --- generate_line / generate ---------------------------------------
		{"gen_line", `require "csv"; print CSV.generate_line(["a","b,c"])`, "a,\"b,c\"\n"},
		{"gen_line_nil", `require "csv"; print CSV.generate_line(["a",nil,"b"])`, "a,,b\n"},
		{"gen_line_int", `require "csv"; print CSV.generate_line([1,2.5])`, "1,2.5\n"},
		{"gen_line_sym", `require "csv"; print CSV.generate_line([:a,:b])`, "a,b\n"},
		{"gen_line_force", `require "csv"; print CSV.generate_line(["a","b"], force_quotes: true)`, "\"a\",\"b\"\n"},
		{"gen_line_row", `require "csv"; r = CSV.parse("a,b\n1,2\n", headers: true).first; print CSV.generate_line(r)`, "1,2\n"},
		{"gen_block", `require "csv"; print CSV.generate { |csv| csv << ["x","y"]; csv << [1,2] }`, "x,y\n1,2\n"},
		{"gen_block_push", `require "csv"; print CSV.generate { |csv| csv.push(["x"]) }`, "x\n"},
		{"gen_block_chain", `require "csv"; print CSV.generate { |csv| (csv << ["a"]) << ["b"] }`, "a\nb\n"},
		{"gen_rows", `require "csv"; print CSV.generate([["a","b"],[1,2]])`, "a,b\n1,2\n"},
		{"gen_quote_empty", `require "csv"; print CSV.generate_line(["",nil], quote_empty: false)`, ",\n"},
		{"gen_row_sep", `require "csv"; print CSV.generate_line(["a","b"], row_sep: "|")`, "a,b|"},

		// --- headers / CSV::Table / CSV::Row --------------------------------
		{"headers_first", `require "csv"; p CSV.parse("h1,h2\n1,2\n", headers: true).first["h1"]`, "\"1\"\n"},
		{"table_class", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).class`, "CSV::Table\n"},
		{"row_class", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first.class`, "CSV::Row\n"},
		{"table_headers", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).headers`, "[\"a\", \"b\"]\n"},
		{"table_to_a", `require "csv"; p CSV.parse("a,b\n1,2\n3,4\n", headers: true).to_a`, "[[\"a\", \"b\"], [\"1\", \"2\"], [\"3\", \"4\"]]\n"},
		{"table_index", `require "csv"; p CSV.parse("a,b\n1,2\n3,4\n", headers: true)[1]["b"]`, "\"4\"\n"},
		{"table_index_nil", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true)[9]`, "nil\n"},
		{"table_first", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first["a"]`, "\"1\"\n"},
		{"table_first_nil", `require "csv"; p CSV.parse("a,b\n", headers: true).first`, "nil\n"},
		{"table_size", `require "csv"; p CSV.parse("a,b\n1,2\n3,4\n", headers: true).size`, "2\n"},
		{"table_length", `require "csv"; p CSV.parse("a,b\n1,2\n3,4\n", headers: true).length`, "2\n"},
		{"table_each", `require "csv"; CSV.parse("a,b\n1,2\n3,4\n", headers: true).each { |r| p r["a"] }`, "\"1\"\n\"3\"\n"},
		{"table_truthy", `require "csv"; p(CSV.parse("a,b\n1,2\n", headers: true) ? 1 : 0)`, "1\n"},

		{"row_field", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first.field("b")`, "\"2\"\n"},
		{"row_index_int", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first[1]`, "\"2\"\n"},
		{"row_index_neg", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first[-1]`, "\"2\"\n"},
		{"row_index_oob", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first[9]`, "nil\n"},
		{"row_missing_header", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first["z"]`, "nil\n"},
		{"row_fields", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first.fields`, "[\"1\", \"2\"]\n"},
		{"row_headers", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first.headers`, "[\"a\", \"b\"]\n"},
		{"row_to_a", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first.to_a`, "[[\"a\", \"1\"], [\"b\", \"2\"]]\n"},
		{"row_to_h", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first.to_h`, "{\"a\" => \"1\", \"b\" => \"2\"}\n"},
		{"row_to_hash", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first.to_hash`, "{\"a\" => \"1\", \"b\" => \"2\"}\n"},
		{"row_to_csv", `require "csv"; print CSV.parse("a,b\n1,2\n", headers: true).first.to_csv`, "1,2\n"},
		{"row_to_s", `require "csv"; print CSV.parse("a,b\n1,2\n", headers: true).first.to_s`, "1,2\n"},
		{"row_each", `require "csv"; CSV.parse("a,b\n1,2\n", headers: true).first.each { |k,v| p [k,v] }`, "[\"a\", \"1\"]\n[\"b\", \"2\"]\n"},
		{"row_header_row", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first.header_row?`, "false\n"},
		{"row_truthy", `require "csv"; p(CSV.parse("a,b\n1,2\n", headers: true).first ? 1 : 0)`, "1\n"},
		{"row_sym_lookup", `require "csv"; p CSV.parse("A,B\n1,2\n", headers: true, header_converters: :symbol).first[:a]`, "\"1\"\n"},

		// --- return_headers / write_headers ---------------------------------
		{"return_headers", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true, return_headers: true).first.header_row?`, "true\n"},
		{"write_headers", `require "csv"; print CSV.generate([["1","2"]], headers: ["a","b"], write_headers: true)`, "a,b\n1,2\n"},

		// --- foreach (over a string) ----------------------------------------
		{"foreach", `require "csv"; CSV.foreach("a,b\nc,d\n") { |r| p r }`, "[\"a\", \"b\"]\n[\"c\", \"d\"]\n"},
		{"foreach_headers", `require "csv"; CSV.foreach("h\n1\n2\n", headers: true) { |r| p r["h"] }`, "\"1\"\n\"2\"\n"},
		{"foreach_ret", `require "csv"; p CSV.foreach("a\n") { |r| }`, "nil\n"},

		// --- date_time / time converters ------------------------------------
		{"conv_date_time", `require "csv"; require "date"; p CSV.parse_line("2020-01-02T10:00:00+00:00", converters: [:date_time]).first.class`, "DateTime\n"},
		{"conv_time", `require "csv"; require "date"; p CSV.parse_line("2020-01-02T10:00:00+00:00", converters: [:time]).first.class`, "Time\n"},

		// --- inspect / to_s / to_csv on Table and Row -----------------------
		{"table_inspect", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).inspect.start_with?("#<CSV::Table")`, "true\n"},
		{"table_to_csv", `require "csv"; print CSV.parse("a,b\n1,2\n", headers: true).to_csv`, "a,b\n1,2\n"},
		{"table_to_s", `require "csv"; print CSV.parse("a,b\n1,2\n", headers: true).to_s`, "a,b\n1,2\n"},
		{"table_puts", `require "csv"; puts CSV.parse("a,b\n1,2\n", headers: true)`, "a,b\n1,2\n"},
		{"row_inspect", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first.inspect.start_with?("#<CSV::Row")`, "true\n"},
		{"row_puts", `require "csv"; puts CSV.parse("a,b\n1,2\n", headers: true).first`, "1,2\n"},

		// --- headers as the string "first_row" ------------------------------
		{"headers_first_row", `require "csv"; p CSV.parse("h\n1\n", headers: "first_row").first["h"]`, "\"1\"\n"},
		{"headers_false", `require "csv"; p CSV.parse("a,b\n", headers: false)`, "[[\"a\", \"b\"]]\n"},

		// --- generate row from a Symbol / object (to_s lowering) ------------
		{"gen_line_object", `require "csv"; print CSV.generate_line([Object.new.tap { |o| def o.to_s; "OBJ"; end }])`, "OBJ\n"},

		// --- parse_line with headers (returns a CSV::Row) -------------------
		// headers: a bool, a String ("first_row") and a []string each select the
		// headered path (csvHasHeaders). With []string the names are explicit, so
		// the single record is a data Row; the bool/string forms consume the first
		// record as headers, leaving no data Row for a one-record input.
		{"parse_line_headers_true", `require "csv"; p CSV.parse_line("a,b", headers: true)`, "nil\n"},
		{"parse_line_headers_str", `require "csv"; p CSV.parse_line("a,b", headers: "first_row")`, "nil\n"},
		{"parse_line_headers_array", `require "csv"; p CSV.parse_line("1,2", headers: ["x","y"]).class`, "CSV::Row\n"},
		{"parse_line_headers_array_val", `require "csv"; p CSV.parse_line("1,2", headers: ["x","y"])["y"]`, "\"2\"\n"},
		{"parse_line_headers_empty", `require "csv"; p CSV.parse_line("", headers: ["x"])`, "nil\n"},

		// --- option-value edge shapes ---------------------------------------
		{"headers_truthy_int", `require "csv"; p CSV.parse("h\n1\n", headers: 1).first["h"]`, "\"1\"\n"},
		{"headers_nil", `require "csv"; p CSV.parse("a,b\n", headers: nil)`, "[[\"a\", \"b\"]]\n"},
		{"row_key_float", `require "csv"; p CSV.parse("a,b\n1,2\n", headers: true).first[1.0]`, "nil\n"},
		{"converters_nil", `require "csv"; p CSV.parse_line("1,2", converters: nil)`, "[\"1\", \"2\"]\n"},
		{"converters_array_str", `require "csv"; p CSV.parse_line("1,x", converters: ["integer"])`, "[1, \"x\"]\n"},
		{"opts_not_hash", `require "csv"; p CSV.parse_line("a,b", nil)`, "[\"a\", \"b\"]\n"},
		{"nil_value_symbol", `require "csv"; p CSV.parse_line(",", nil_value: :z)`, "[:z, :z]\n"},

		// --- generate block writer object -----------------------------------
		{"gen_writer_truthy", `require "csv"; CSV.generate { |csv| (puts(csv ? 1 : 0)); csv << ["a"] }`, "1\n"},
		{"gen_writer_inspect", `require "csv"; CSV.generate { |csv| (puts csv.inspect.start_with?("#<CSV")); csv << ["a"] }`, "true\n"},
		{"gen_writer_to_s", `require "csv"; CSV.generate { |csv| (puts csv); csv << ["a"] }`, "#<CSV>\n"},

		// --- MalformedCSVError ----------------------------------------------
		{"malformed_class", `require "csv"; begin; CSV.parse('a,"b'); rescue CSV::MalformedCSVError => e; p e.class; end`, "CSV::MalformedCSVError\n"},
		{"malformed_is_std", `require "csv"; p CSV::MalformedCSVError.ancestors.include?(StandardError)`, "true\n"},
		{"malformed_line", `require "csv"; begin; CSV.parse_line('a,"b'); rescue => e; p e.message; end`, "\"Unclosed quoted field in line 1.\"\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := eval(t, tt.src)
			if out != tt.want {
				t.Fatalf("src=%q\n got %q\nwant %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestCSVErrors pins the error class and message of the binding's raises: the
// MalformedCSVError re-raise, the RegexpError for an invalid :skip_lines pattern,
// the TypeError for a non-Array generate row, and the ENOENT for a missing file.
func TestCSVErrors(t *testing.T) {
	tests := []struct{ name, src, class, msgPart string }{
		{"malformed", `require "csv"; CSV.parse('a,"b')`, "CSV::MalformedCSVError", "Unclosed quoted field in line 1."},
		{"bad_skip_lines", `require "csv"; CSV.parse("a\nb\n", skip_lines: "(")`, "RegexpError", "missing closing )"},
		{"gen_line_nonarray", `require "csv"; CSV.generate_line(5)`, "TypeError", "no implicit conversion of Integer into Array"},
		{"read_missing", `require "csv"; CSV.read("/no/such/csv/file_xyz.csv")`, "Errno::ENOENT", "No such file or directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, msg := evalErr(t, tt.src)
			if class != tt.class {
				t.Fatalf("src=%q: got class %q, want %q", tt.src, class, tt.class)
			}
			if !strings.Contains(msg, tt.msgPart) {
				t.Fatalf("src=%q: msg %q missing %q", tt.src, msg, tt.msgPart)
			}
		})
	}
}

// TestCSVReadFile exercises CSV.read against a real file: a temp CSV is written
// and read both plain and with headers. The path is built with t.TempDir and
// filepath.ToSlash so the Ruby source carries a forward-slash path on every OS
// (the windows-latest coverage run runs the same gate).
func TestCSVReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(path, []byte("h1,h2\n1,2\n3,4\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	slash := filepath.ToSlash(path)

	if got := eval(t, `require "csv"; p CSV.read("`+slash+`")`); got != "[[\"h1\", \"h2\"], [\"1\", \"2\"], [\"3\", \"4\"]]\n" {
		t.Fatalf("CSV.read = %q", got)
	}
	if got := eval(t, `require "csv"; p CSV.read("`+slash+`", headers: true).first["h1"]`); got != "\"1\"\n" {
		t.Fatalf("CSV.read headers = %q", got)
	}
}
