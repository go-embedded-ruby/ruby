// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestParquet covers the Ruby Parquet module (backed by
// github.com/go-ruby-parquet/parquet, the pure-Go red-parquet-faithful port of
// Apache Parquet over apache/arrow-go): the Parquet.write / Parquet.read /
// Parquet.load convenience round-trip, the ArrowFileReader / ArrowFileWriter
// classes, per-file compression and row-group options, and interoperation with
// the Arrow module's Arrow::Table / Arrow::Schema.
func TestParquet(t *testing.T) {
	const req = `require "parquet"; require "arrow"; `
	for _, c := range []struct{ src, want string }{
		// Module round-trip through in-memory bytes (default Snappy).
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); p(Parquet.read(Parquet.write(t)).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3],"n"=>["a","b","c"],"ok"=>[true,false,nil]}); p(Parquet.read(Parquet.write(t)).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); p Parquet.write(t).class`, "String\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); p Parquet.write(t, nil).class`, "String\n"},

		// Compression / row-group / dictionary options all round-trip.
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); p(Parquet.read(Parquet.write(t, compression: :uncompressed)).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); p(Parquet.read(Parquet.write(t, compression: :gzip)).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); p(Parquet.read(Parquet.write(t, compression: "zstd")).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); p(Parquet.read(Parquet.write(t, row_group_size: 2, dictionary: false)).to_h == t.to_h)`, "true\n"},

		// Reader over in-memory bytes: metadata + read_table + read_row_group.
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); r=Parquet::ArrowFileReader.new(Parquet.write(t)); p r.n_rows`, "3\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); r=Parquet::ArrowFileReader.new(Parquet.write(t)); p r.num_rows`, "3\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); r=Parquet::ArrowFileReader.new(Parquet.write(t)); p r.n_row_groups`, "1\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); r=Parquet::ArrowFileReader.new(Parquet.write(t)); p r.num_row_groups`, "1\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); r=Parquet::ArrowFileReader.new(Parquet.write(t)); p r.read_table["id"].to_a`, "[1, 2, 3]\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); r=Parquet::ArrowFileReader.new(Parquet.write(t)); p r.read_row_group(0)["id"].to_a`, "[1, 2, 3]\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); r=Parquet::ArrowFileReader.new(Parquet.write(t)); p r.schema.fields.map{|f| f.name}`, "[\"id\"]\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); r=Parquet::ArrowFileReader.new(Parquet.write(t)); p r.close`, "nil\n"},
		{`t=Arrow::Table.new({"id"=>[1]}); p(Parquet::ArrowFileReader.new(Parquet.write(t)).class)`, "Parquet::ArrowFileReader\n"},

		// Writer to in-memory bytes: write / write_table / << then close → bytes.
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); w=Parquet::ArrowFileWriter.new(t.schema); w.write(t); p(Parquet.read(w.close).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); w=Parquet::ArrowFileWriter.new(t.schema); w.write_table(t); p(Parquet.read(w.close).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); w=Parquet::ArrowFileWriter.new(t.schema); w << t; p(Parquet.read(w.close).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1,2,3]}); w=Parquet::ArrowFileWriter.new(t.schema, compression: :zstd); w.write(t); p(Parquet.read(w.close).to_h == t.to_h)`, "true\n"},
		{`t=Arrow::Table.new({"id"=>[1]}); w=Parquet::ArrowFileWriter.new(t.schema); w.write(t); w.close; p(w.close.class)`, "String\n"}, // idempotent close still returns bytes
		{`t=Arrow::Table.new({"id"=>[1]}); p(Parquet::ArrowFileWriter.new(t.schema).class)`, "Parquet::ArrowFileWriter\n"},

		// to_s / inspect rendering of the reader and writer wrappers.
		{`t=Arrow::Table.new({"id"=>[1]}); p Parquet::ArrowFileReader.new(Parquet.write(t)).to_s`, "\"#<Parquet::ArrowFileReader>\"\n"},
		{`t=Arrow::Table.new({"id"=>[1]}); p Parquet::ArrowFileWriter.new(t.schema).inspect`, "\"#<Parquet::ArrowFileWriter>\"\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestParquetFileRoundTrip covers the filesystem path forms: Parquet.write to a
// path, Parquet.load of that path, ArrowFileReader.new over a path, and the
// ArrowFileWriter path form whose #close flushes the buffer to disk and returns
// nil.
func TestParquetFileRoundTrip(t *testing.T) {
	dir := strings.ReplaceAll(t.TempDir(), "\\", "/")
	src := `require "parquet"; require "arrow"
t = Arrow::Table.new({"id"=>[1,2,3],"n"=>["a","b","c"]})
Parquet.write(t, "` + dir + `/a.parquet")
p(Parquet.load("` + dir + `/a.parquet").to_h == t.to_h)
p(Parquet::ArrowFileReader.new("` + dir + `/a.parquet").read_table.to_h == t.to_h)
w = Parquet::ArrowFileWriter.new(t.schema, "` + dir + `/b.parquet")
w.write(t)
p w.close
p(Parquet.load("` + dir + `/b.parquet").to_h == t.to_h)
w.close`
	want := "true\ntrue\nnil\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("file round-trip got=%q want=%q", got, want)
	}
}

// TestParquetErrors covers the binding's argument-guard and library-error raises:
// wrong argument types and shapes, out-of-range row groups, unknown options,
// writing to a closed writer, and undecodable Parquet bytes.
func TestParquetErrors(t *testing.T) {
	const req = `require "parquet"; require "arrow"; `
	for _, c := range []struct{ src, want string }{
		{`Parquet.write`, "ArgumentError"},                                                                                                                    // missing table
		{`Parquet.write(7)`, "TypeError"},                                                                                                                     // not an Arrow::Table
		{`Parquet.write(Arrow::Table.new({"id"=>[1]}), 7)`, "ArgumentError"},                                                                                  // bad write argument
		{`Parquet.write(Arrow::Table.new({"id"=>[1]}), compression: :nope)`, "ArgumentError"},                                                                 // unknown codec
		{`Parquet.write(Arrow::Table.new({"id"=>[1]}), compression: 7)`, "TypeError"},                                                                         // codec not a String/Symbol
		{`Parquet.write(Arrow::Table.new({"id"=>[1]}), row_group_size: "big")`, "TypeError"},                                                                  // size not an Integer
		{`Parquet.load`, "ArgumentError"},                                                                                                                     // missing path
		{`Parquet.load(7)`, "TypeError"},                                                                                                                      // path not a String
		{`Parquet.load("/no/such/file.parquet")`, "Parquet::Error::Io"},                                                                                       // missing file
		{`Parquet.read`, "ArgumentError"},                                                                                                                     // missing bytes
		{`Parquet.read(7)`, "TypeError"},                                                                                                                      // bytes not a String
		{`Parquet.read("not parquet bytes at all")`, "Parquet::Error::Io"},                                                                                    // undecodable
		{`Parquet::ArrowFileReader.new`, "ArgumentError"},                                                                                                     // missing source
		{`Parquet::ArrowFileReader.new(7)`, "TypeError"},                                                                                                      // source not a String
		{`Parquet::ArrowFileReader.new("garbage bytes here")`, "Parquet::Error::Io"},                                                                          // undecodable bytes
		{`r=Parquet::ArrowFileReader.new(Parquet.write(Arrow::Table.new({"id"=>[1]}))); r.read_row_group(9)`, "IndexError"},                                   // out of range
		{`r=Parquet::ArrowFileReader.new(Parquet.write(Arrow::Table.new({"id"=>[1]}))); r.read_row_group(nil)`, "TypeError"},                                  // index not an Integer
		{`Parquet::ArrowFileWriter.new`, "ArgumentError"},                                                                                                     // missing schema
		{`Parquet::ArrowFileWriter.new(7)`, "TypeError"},                                                                                                      // schema not an Arrow::Schema
		{`t=Arrow::Table.new({"id"=>[1]}); w=Parquet::ArrowFileWriter.new(t.schema); w.write(7)`, "TypeError"},                                                // write not a table
		{`t=Arrow::Table.new({"id"=>[1]}); w=Parquet::ArrowFileWriter.new(Arrow::Schema.new({"other"=>:string})); w.write(t)`, "TypeError"},                   // schema mismatch
		{`t=Arrow::Table.new({"id"=>[1]}); w=Parquet::ArrowFileWriter.new(t.schema); w.close; w.write(t)`, "Parquet::Error"},                                  // write after close
		{`t=Arrow::Table.new({"id"=>[1]}); Parquet::ArrowFileWriter.new(t.schema, "/no/such/dir/x.parquet").tap{|w| w.write(t)}.close`, "Parquet::Error::Io"}, // unwritable path
	} {
		if err := runErr(t, req+c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestParquetErrorTree covers the Parquet::Error exception hierarchy:
// Parquet::Error < StandardError and Parquet::Error::Io < Parquet::Error, and
// rescuing a re-raised library IO error as the faithful class.
func TestParquetErrorTree(t *testing.T) {
	const req = `require "parquet"; `
	for _, c := range []struct{ src, want string }{
		{`p(Parquet::Error < StandardError)`, "true\n"},
		{`p(Parquet::Error::Io < Parquet::Error)`, "true\n"},
		{`begin; Parquet.read("garbage"); rescue Parquet::Error => e; puts "caught #{e.class}"; end`, "caught Parquet::Error::Io\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
