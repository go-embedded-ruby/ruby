// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestArrow covers the Ruby Arrow module (backed by github.com/go-ruby-arrow/arrow,
// the pure-Go red-arrow-faithful port of Apache Arrow): typed arrays and builders,
// the DataType/Field/Schema surface, RecordBatch/Table construction from column
// Hashes and (schema, arrays) pairs, element access and enumeration, and the
// wire-compatible IPC round-trip through Table#save / Table.load.
func TestArrow(t *testing.T) {
	const req = `require "arrow"; `
	for _, c := range []struct{ src, want string }{
		// Array construction (inferred type) + element access.
		{`p Arrow::Array.new([1,2,nil,4]).to_a`, "[1, 2, nil, 4]\n"},
		{`p Arrow::Array.new([1,2,3]).value_data_type.name`, "\"int64\"\n"},
		{`p Arrow::Array.new(["x",nil,"z"]).to_a`, "[\"x\", nil, \"z\"]\n"},
		{`p Arrow::Array.new(["x"]).value_data_type.name`, "\"utf8\"\n"},
		{`p Arrow::Array.new([true,false,nil]).to_a`, "[true, false, nil]\n"},
		{`p Arrow::Array.new([1.5,2.5]).to_a`, "[1.5, 2.5]\n"},
		{`p Arrow::Array.new([1,2,3])[1]`, "2\n"},
		{`p Arrow::Array.new([:a,:b]).to_a`, "[\"a\", \"b\"]\n"},
		{`p Arrow::Array.new([1,2,3], "int32").value_data_type.name`, "\"int32\"\n"},
		{`p Arrow::Array.new([1,2,3])[-1]`, "3\n"},
		{`p Arrow::Array.new([1,nil,3]).n_nulls`, "1\n"},
		{`p Arrow::Array.new([1,nil,3]).null?(1)`, "true\n"},
		{`p Arrow::Array.new([1,nil,3]).valid?(0)`, "true\n"},
		{`p Arrow::Array.new([1,2,3]).length`, "3\n"},
		{`p Arrow::Array.new([1,2,3]).size`, "3\n"},
		{`p Arrow::Array.new([1,2,3]).n_rows`, "3\n"},
		{`r=[]; Arrow::Array.new([1,2,3]).each{|e| r<<e}; p r`, "[1, 2, 3]\n"},
		{`p(Arrow::Array.new([1,2]).class)`, "Arrow::Array\n"},

		// Explicit typed Array (narrow width) + typed builder.
		{`p Arrow::Array.new([1,2,3], :int32).value_data_type.name`, "\"int32\"\n"},
		{`b=Arrow::ArrayBuilder.new(:int32); b.append(1); b<<2; b.append_null; p b.finish.to_a`, "[1, 2, nil]\n"},
		{`b=Arrow::ArrayBuilder.new(:int64); b.append_values([1,2,3]); p b.length`, "3\n"},
		{`b=Arrow::ArrayBuilder.new(:string); b.append("hi"); p b.size`, "1\n"},
		{`b=Arrow::ArrayBuilder.new(:float64); p b.value_data_type.name`, "\"float64\"\n"},
		{`b=Arrow::ArrayBuilder.new(:double); p b.value_data_type.name`, "\"float64\"\n"},

		// DataType surface.
		{`p Arrow::DataType.int64.name`, "\"int64\"\n"},
		{`p Arrow::DataType.string.to_s`, "\"utf8\"\n"},
		{`p Arrow::DataType.resolve(:boolean).name`, "\"bool\"\n"},
		{`p(Arrow::DataType.int64 == Arrow::DataType.int64)`, "true\n"},
		{`p(Arrow::DataType.int64 == Arrow::DataType.int32)`, "false\n"},
		{`p(Arrow::DataType.int64 == 7)`, "false\n"},
		{`p Arrow::DataType.list(:int64).name`, "\"list\"\n"},

		// List + Struct element round-trip.
		{`p Arrow::Array.new([[1,2],[3]], Arrow::DataType.list(:int64)).to_a`, "[[1, 2], [3]]\n"},
		{`st=Arrow::DataType.struct(Arrow::Field.new("x",:int64), Arrow::Field.new("y",:string)); p Arrow::Array.new([{"x"=>1,"y"=>"a"}], st).to_a`, "[{\"x\" => 1, \"y\" => \"a\"}]\n"},

		// Decimal128 (rendered as a decimal String on read).
		{`p Arrow::Array.new(["1.50", 2.5], Arrow::DataType.decimal128(10,2)).to_a`, "[\"1.50\", \"2.50\"]\n"},

		// Field / Schema.
		{`p Arrow::Field.new("a", :int64).name`, "\"a\"\n"},
		{`p Arrow::Field.new("a", :int64).nullable?`, "true\n"},
		{`p Arrow::Field.new("a", :int64, false).nullable?`, "false\n"},
		{`p Arrow::Field.new("a", :int64).data_type.name`, "\"int64\"\n"},
		{`p Arrow::Schema.new({"a"=>:int64, "b"=>:string}).n_fields`, "2\n"},
		{`p Arrow::Schema.new({"a"=>:int64}).num_fields`, "1\n"},
		{`p Arrow::Schema.new({"a"=>:int64,"b"=>:string}).fields.map{|f| f.name}`, "[\"a\", \"b\"]\n"},
		{`p Arrow::Schema.new({"a"=>:int64,"b"=>:string})[0].name`, "\"a\"\n"},
		{`p Arrow::Schema.new({"a"=>:int64})["a"].name`, "\"a\"\n"},
		{`p Arrow::Schema.new({"a"=>:int64})["z"]`, "nil\n"},
		{`p Arrow::Schema.new([Arrow::Field.new("a",:int64)]).n_fields`, "1\n"},

		// Table from column Hashes.
		{`p Arrow::Table.new({"id"=>[1,2,3]}).num_rows`, "3\n"},
		{`p Arrow::Table.new({"id"=>[1,2,3],"n"=>["a","b","c"]}).n_columns`, "2\n"},
		{`p Arrow::Table.new({"id"=>[1,2,3]})["id"].to_a`, "[1, 2, 3]\n"},
		{`p Arrow::Table.new({"id"=>[1,2,3]})[0].to_a`, "[1, 2, 3]\n"},
		{`p Arrow::Table.new({"id"=>[1,2,3]}).to_h`, "{\"id\" => [1, 2, 3]}\n"},
		{`p Arrow::Table.new({"id"=>[1,2,3]}).schema.n_fields`, "1\n"},
		{`p Arrow::Table.new({"id"=>[1,2,3,4]}).slice(1,2)["id"].to_a`, "[2, 3]\n"},
		{`r=[]; Arrow::Table.new({"id"=>[1,2]}).each_record{|x| r<<x}; p r`, "[{\"id\" => 1}, {\"id\" => 2}]\n"},
		{`p Arrow::Table.new({"id"=>[1,2]}).record_batch.num_rows`, "2\n"},
		{`p(Arrow::Table.new({"id"=>[1]}).class)`, "Arrow::Table\n"},

		// Table from (schema, arrays).
		{`s=Arrow::Schema.new({"a"=>:int64}); c=Arrow::Array.new([9,8]); p Arrow::Table.new(s,[c]).to_h`, "{\"a\" => [9, 8]}\n"},

		// RecordBatch.
		{`p Arrow::RecordBatch.new({"id"=>[1,2,3]}).num_rows`, "3\n"},
		{`p Arrow::RecordBatch.new({"id"=>[1,2,3]}).n_columns`, "1\n"},
		{`p Arrow::RecordBatch.new({"id"=>[1,2,3]}).column(0).to_a`, "[1, 2, 3]\n"},
		{`p Arrow::RecordBatch.new({"id"=>[1,2,3]})["id"].to_a`, "[1, 2, 3]\n"},
		{`p Arrow::RecordBatch.new({"a"=>[1,2],"b"=>[3,4]}).to_h`, "{\"a\" => [1, 2], \"b\" => [3, 4]}\n"},
		{`p Arrow::RecordBatch.new({"id"=>[1,2,3,4]}).slice(0,2)["id"].to_a`, "[1, 2]\n"},
		{`p Arrow::RecordBatch.new({"id"=>[1,2,3]}).schema.n_fields`, "1\n"},
		{`r=[]; Arrow::RecordBatch.new({"id"=>[7]}).each_record{|x| r<<x}; p r`, "[{\"id\" => 7}]\n"},

		// IPC round-trip through bytes (streaming + file formats) preserves values.
		{`t=Arrow::Table.new({"id"=>[1,2,3],"n"=>["a","b","c"],"ok"=>[true,false,nil]}); bytes=t.save; p(Arrow::Table.load(bytes).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); p(Arrow::Table.load(t.save(:arrow_streaming)).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); p(Arrow::Table.load(t.save(:arrow)).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); p(Arrow::Table.load(t.save(format: :arrow)).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); p(Arrow::Table.load(t.save(nil)).to_h == t.to_h)`, "true\n"},

		// to_s / inspect rendering across the wrapper classes.
		{`p Arrow::Field.new("a", :int64).to_s.include?("int64")`, "true\n"},
		{`p Arrow::Schema.new({"a"=>:int64}).to_s.include?("int64")`, "true\n"},
		{`p Arrow::Array.new([1,2]).to_s.class`, "String\n"},
		{`p Arrow::Table.new({"id"=>[1]}).to_s.include?("RecordBatch")`, "true\n"},
		{`p Arrow::RecordBatch.new({"id"=>[1]}).to_s.include?("RecordBatch")`, "true\n"},

		// Symbol column key resolves to the same column as its String name.
		{`p Arrow::Table.new({"id"=>[7,8]})[:id].to_a`, "[7, 8]\n"},
		{`p Arrow::RecordBatch.new({"id"=>[7,8]})[:id].to_a`, "[7, 8]\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArrowTime covers the Time <-> Timestamp / Date32 mapping (second precision,
// UTC) across the typed builders.
func TestArrowTime(t *testing.T) {
	const req = `require "arrow"; require "time"; `
	for _, c := range []struct{ src, want string }{
		{`p Arrow::Array.new([Time.at(1_000_000_000)], :timestamp)[0].to_i`, "1000000000\n"},
		{`p Arrow::Array.new([Time.at(1_000_000_000)], :timestamp).value_data_type.name`, "\"timestamp\"\n"},
		{`p Arrow::Array.new([Time.at(0)], :date)[0].to_i`, "0\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArrowFileSave covers Table#save to a filesystem path and Table.load of that
// path (the file branch of Table.load, distinct from the in-memory bytes branch).
func TestArrowFileSave(t *testing.T) {
	path := strings.ReplaceAll(t.TempDir()+"/t.arrow", "\\", "/")
	src := `require "arrow"
t = Arrow::Table.new({"id"=>[1,2,3]})
t.save(` + `"` + path + `"` + `, :arrow)
p(Arrow::Table.load(` + `"` + path + `"` + `).to_h == t.to_h)`
	if got := eval(t, src); got != "true\n" {
		t.Errorf("file round-trip got=%q want=%q", got, "true\n")
	}
}

// TestArrowErrors covers the binding's argument-guard and library-error raises:
// bad type specs, out-of-range Integers, wrong argument shapes, missing blocks,
// and the Arrow::Error IO tree.
func TestArrowErrors(t *testing.T) {
	const req = `require "arrow"; `
	for _, c := range []struct{ src, want string }{
		{`Arrow::Array.new`, "ArgumentError"},                                    // missing values
		{`Arrow::Array.new(7)`, "TypeError"},                                     // values not an Array
		{`Arrow::Array.new([Object.new])`, "TypeError"},                          // unmappable element
		{`Arrow::Array.new([])`, "ArgumentError"},                                // cannot infer from empty
		{`Arrow::Array.new([1,2,3])[9]`, "IndexError"},                           // index out of range
		{`Arrow::Array.new([1,2,3])[nil]`, "TypeError"},                          // index not an Integer
		{`Arrow::Array.new([1,2,3]).each`, "LocalJumpError"},                     // each without a block
		{`Arrow::Array.new([1], :nope)`, "ArgumentError"},                        // unknown type name
		{`Arrow::Array.new([1], 7)`, "TypeError"},                                // type spec not a DataType/Symbol
		{`Arrow::Array.new([2**64])`, "TypeError"},                               // Integer out of range
		{`Arrow::ArrayBuilder.new`, "ArgumentError"},                             // missing type
		{`Arrow::ArrayBuilder.new(:int8).append(999)`, "ArgumentError"},          // out of Int8 range
		{`Arrow::ArrayBuilder.new(:int64).append("x")`, "TypeError"},             // wrong element type
		{`Arrow::Field.new("a")`, "ArgumentError"},                               // missing type
		{`Arrow::Field.new(7, :int64)`, "TypeError"},                             // name not String/Symbol
		{`Arrow::Schema.new(7)`, "TypeError"},                                    // bad schema arg
		{`Arrow::Schema.new([7])`, "TypeError"},                                  // not a Field
		{`Arrow::Schema.new({"a"=>:int64})[9]`, "IndexError"},                    // field index out of range
		{`Arrow::Schema.new({"a"=>:int64})[nil]`, "TypeError"},                   // bad field key
		{`Arrow::Table.new`, "ArgumentError"},                                    // missing args
		{`Arrow::Table.new(7)`, "TypeError"},                                     // not a Hash / schema
		{`Arrow::Table.new(Arrow::Schema.new({"a"=>:int64}))`, "ArgumentError"},  // schema without columns
		{`Arrow::Table.new(Arrow::Schema.new({"a"=>:int64}), 7)`, "TypeError"},   // columns not an Array
		{`Arrow::Table.new(Arrow::Schema.new({"a"=>:int64}), [7])`, "TypeError"}, // column not an Arrow::Array
		{`Arrow::Table.new({1=>[1]})`, "TypeError"},                              // column name not String/Symbol
		{`Arrow::Table.new({"a"=>[1],"b"=>[1,2]})`, "ArgumentError"},             // ragged columns
		{`Arrow::Table.new({"id"=>[1]})["z"]`, "IndexError"},                     // no such column
		{`Arrow::Table.new({"id"=>[1]})[nil]`, "TypeError"},                      // bad column key
		{`Arrow::Table.new({"id"=>[1,2,3]}).slice(0,9)`, "IndexError"},           // slice out of range
		{`Arrow::Table.new({"id"=>[1]}).each_record`, "LocalJumpError"},          // each_record no block
		{`Arrow::Table.new({"id"=>[1]}).save(:nope)`, "ArgumentError"},           // unknown format
		{`Arrow::Table.new({"id"=>[1]}).save(Object.new)`, "ArgumentError"},      // bad save argument
		{`Arrow::Table.load(7)`, "TypeError"},                                    // load not a String
		{`Arrow::Table.load("not arrow bytes at all")`, "Arrow::Error::Io"},      // undecodable IPC bytes
		{`Arrow::RecordBatch.new({"id"=>[1]}).each_record`, "LocalJumpError"},    // each_record no block
		{`Arrow::RecordBatch.new({"a"=>[1],"b"=>[1,2]})`, "ArgumentError"},       // ragged columns
		{`Arrow::RecordBatch.new({"id"=>[1,2,3]}).slice(0,9)`, "IndexError"},     // slice out of range
	} {
		if err := runErr(t, req+c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestArrowErrorTree covers the Arrow::Error exception hierarchy: Arrow::Error <
// StandardError and Arrow::Error::Io < Arrow::Error, and rescuing a re-raised
// library IO error as the faithful class.
func TestArrowErrorTree(t *testing.T) {
	const req = `require "arrow"; `
	for _, c := range []struct{ src, want string }{
		{`p(Arrow::Error < StandardError)`, "true\n"},
		{`p(Arrow::Error::Io < Arrow::Error)`, "true\n"},
		{`begin; Arrow::Table.load("garbage"); rescue Arrow::Error => e; puts "caught #{e.class}"; end`, "caught Arrow::Error::Io\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
