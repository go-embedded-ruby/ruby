// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestBleve covers the Ruby Bleve module (backed by github.com/go-ruby-bleve/bleve,
// the pure-Go full-text search library over blevesearch/bleve/v2): building an
// in-memory index, indexing Hash documents, the whole query DSL, the search
// options (size / from / fields / sort / highlight), the result and hit surface,
// batches, mappings and the class identities.
func TestBleve(t *testing.T) {
	const req = `require "bleve"; `
	// two-doc text index used by many cases
	const two = `ix=Bleve.new_mem_index; ix.index("a",{"t"=>"hello world"}); ix.index("b",{"t"=>"goodbye moon"}); `
	// three-doc "apple" text + numeric index used by the paging/sort cases
	const three = `ix=Bleve.new_mem_index; ix.index("a",{"t"=>"apple","n"=>3}); ix.index("b",{"t"=>"apple","n"=>1}); ix.index("c",{"t"=>"apple","n"=>2}); `
	for _, c := range []struct{ src, want string }{
		// Construction + count + path.
		{`p Bleve.new_mem_index.count`, "0\n"},
		{`p Bleve.new_mem_index(nil).count`, "0\n"},
		{`p Bleve.new_mem_index.path`, "\"\"\n"},
		{two + `p ix.count`, "2\n"},

		// Match + query-string search, hit id/score.
		{two + `p ix.search(Bleve::Query.match("hello")).total`, "1\n"},
		{two + `p ix.search(Bleve::Query.match("hello")).hits[0].id`, "\"a\"\n"},
		{two + `p(ix.search(Bleve::Query.match("hello")).hits[0].score > 0)`, "true\n"},
		{two + `p ix.search("goodbye").hits[0].id`, "\"b\"\n"},
		{two + `p ix.search(Bleve::Query.match_all).total`, "2\n"},
		{two + `p ix.search(Bleve::Query.match_none).total`, "0\n"},

		// Field-scoped query variants.
		{two + `p ix.search(Bleve::Query.term("hello").field("t")).total`, "1\n"},
		{two + `p ix.search(Bleve::Query.prefix("hel").field("t")).total`, "1\n"},
		{two + `p ix.search(Bleve::Query.wildcard("he*o").field("t")).total`, "1\n"},
		{two + `p ix.search(Bleve::Query.regexp("hel.o").field("t")).total`, "1\n"},
		{two + `p ix.search(Bleve::Query.fuzzy("helo").field("t").fuzziness(1)).total`, "1\n"},
		{two + `p ix.search(Bleve::Query.match_phrase("hello world").field("t")).total`, "1\n"},
		{two + `p ix.search(Bleve::Query.match("hello").field("t").boost(2.0)).total`, "1\n"},

		// Numeric range (both open-ended sides).
		{`ix=Bleve.new_mem_index; ix.index("a",{"n"=>5}); ix.index("b",{"n"=>50}); p ix.search(Bleve::Query.numeric_range(10, nil).field("n")).total`, "1\n"},
		{`ix=Bleve.new_mem_index; ix.index("a",{"n"=>5}); ix.index("b",{"n"=>50}); p ix.search(Bleve::Query.numeric_range(nil, 10).field("n")).total`, "1\n"},

		// Boolean query: must / should / must_not, must-only, and no-arg.
		{`ix=Bleve.new_mem_index; ix.index("a",{"t"=>"red apple"}); ix.index("b",{"t"=>"green apple"}); ix.index("c",{"t"=>"red car"}); ` +
			`p ix.search(Bleve::Query.bool(must:[Bleve::Query.match("apple").field("t")], should:[Bleve::Query.match("red").field("t")], must_not:[Bleve::Query.match("green").field("t")])).total`, "1\n"},
		{`ix=Bleve.new_mem_index; ix.index("a",{"t"=>"red apple"}); ix.index("b",{"t"=>"green apple"}); ` +
			`p ix.search(Bleve::Query.bool(must:[Bleve::Query.match("apple").field("t")])).total`, "2\n"},
		{two + `p(ix.search(Bleve::Query.bool).total >= 0)`, "true\n"},

		// Fields on a hit (as Array and as a single String) + document.
		{`ix=Bleve.new_mem_index; ix.index("a",{"t"=>"hello","n"=>7}); p ix.search(Bleve::Query.match("hello").field("t"), fields:["*"]).hits[0].fields["t"]`, "\"hello\"\n"},
		{`ix=Bleve.new_mem_index; ix.index("a",{"t"=>"hello"}); p ix.search(Bleve::Query.match("hello").field("t"), fields:"t").hits[0].fields["t"]`, "\"hello\"\n"},
		{`ix=Bleve.new_mem_index; ix.index("a",{"t"=>"hi","n"=>5}); p ix.document("a")["t"]`, "\"hi\"\n"},
		{`ix=Bleve.new_mem_index; ix.index("a",{"t"=>"hi","n"=>5}); p ix.document("a")["n"]`, "5.0\n"},

		// Delete.
		{`ix=Bleve.new_mem_index; ix.index("a",{"t"=>"x"}); ix.delete("a"); p ix.count`, "0\n"},

		// Paging + sort by the built-in _id (Array and single-String forms).
		{three + `p ix.search(Bleve::Query.match_all, size:1).hits.length`, "1\n"},
		{three + `p ix.search(Bleve::Query.match_all, size:10, sort:["_id"]).hits.map{|h| h.id}`, "[\"a\", \"b\", \"c\"]\n"},
		{three + `p ix.search(Bleve::Query.match_all, size:10, from:1, sort:"_id").hits.map{|h| h.id}`, "[\"b\", \"c\"]\n"},

		// Result aggregates.
		{three + `p(ix.search(Bleve::Query.match("apple").field("t")).max_score > 0)`, "true\n"},
		{three + `p(ix.search(Bleve::Query.match("apple").field("t")).took >= 0)`, "true\n"},

		// Highlighting populates per-field fragments.
		{`ix=Bleve.new_mem_index; ix.index("a",{"t"=>"the quick brown fox"}); ` +
			`p(ix.search(Bleve::Query.match("quick").field("t"), highlight:true, fields:["t"]).hits[0].fragments["t"].length > 0)`, "true\n"},
		{`ix=Bleve.new_mem_index; ix.index("a",{"t"=>"the quick brown fox"}); ` +
			`p(ix.search(Bleve::Query.match("quick").field("t"), highlight_style:"html", fields:["t"]).hits[0].fragments.class)`, "Hash\n"},

		// Hit#to_h.
		{`ix=Bleve.new_mem_index; ix.index("a",{"t"=>"hello"}); h=ix.search(Bleve::Query.match("hello").field("t"), fields:["*"]).hits[0].to_h; p h["id"]`, "\"a\"\n"},
		{`ix=Bleve.new_mem_index; ix.index("a",{"t"=>"hello"}); h=ix.search(Bleve::Query.match("hello").field("t"), fields:["*"]).hits[0].to_h; p h["fields"].class`, "Hash\n"},

		// Batch.
		{`ix=Bleve.new_mem_index; ix.batch{|b| b.index("a",{"t"=>"x"}); b.index("b",{"t"=>"y"}); b.delete("b")}; p ix.count`, "1\n"},
		{`ix=Bleve.new_mem_index; c=nil; ix.batch{|b| c=b.class}; p c`, "Bleve::Batch\n"},

		// Close returns nil; the index is then unusable.
		{`p Bleve.new_mem_index.close`, "nil\n"},

		// Mapping: every typed-field setter + default analyzer, then index & count.
		{`m=Bleve::Mapping.new; m.set_default_analyzer("standard"); m.add_text_field("t"); m.add_text_field_with_analyzer("body","standard"); ` +
			`m.add_keyword_field("tag"); m.add_numeric_field("n"); m.add_datetime_field("d"); m.add_boolean_field("ok"); ` +
			`ix=Bleve.new_mem_index(m); ix.index("a",{"t"=>"hello","tag"=>"x","n"=>1,"ok"=>true}); p ix.count`, "1\n"},

		// Symbol document keys resolve to the same fields as String keys.
		{`ix=Bleve.new_mem_index; ix.index("a",{t: "hello"}); p ix.search(Bleve::Query.match("hello").field("t")).total`, "1\n"},

		// A document exercises every scalar mapping: nil, Float, Symbol value,
		// nested Array and nested Hash.
		{`ix=Bleve.new_mem_index; ix.index("a",{"a"=>nil,"f"=>1.5,"s"=>:sym,"arr"=>[1,2],"nested"=>{"x"=>1}}); p ix.count`, "1\n"},

		// A non-Hash trailing search argument is ignored (no keyword options).
		{two + `p ix.search(Bleve::Query.match_all, 5).total`, "2\n"},

		// Class identities.
		{`p Bleve.new_mem_index.class`, "Bleve::Index\n"},
		{`p Bleve::Query.match("x").class`, "Bleve::Query\n"},
		{`p Bleve::Mapping.new.class`, "Bleve::Mapping\n"},
		{`p Bleve::Facet.term("t",3).class`, "Bleve::Facet\n"},
		{two + `p ix.search(Bleve::Query.match_all).class`, "Bleve::SearchResult\n"},
		{two + `p ix.search(Bleve::Query.match("hello")).hits[0].class`, "Bleve::Hit\n"},

		// to_s renders a stable label.
		{`p Bleve.new_mem_index.to_s`, "\"#<Bleve::Index>\"\n"},
		{`p Bleve::Query.match("x").to_s`, "\"#<Bleve::Query>\"\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBleveFacets covers the aggregation surface: a term facet over a keyword
// field and the numeric- and date-range bucket builders, all read back as plain
// Ruby Hashes/Arrays.
func TestBleveFacets(t *testing.T) {
	const req = `require "bleve"; require "time"; `
	// keyword tag + numeric n + datetime d, three docs.
	const setup = `m=Bleve::Mapping.new; m.add_keyword_field("tag"); m.add_numeric_field("n"); m.add_datetime_field("d"); ` +
		`ix=Bleve.new_mem_index(m); ` +
		`ix.index("a",{"tag"=>"x","n"=>5,"d"=>Time.at(1000)}); ` +
		`ix.index("b",{"tag"=>"x","n"=>50,"d"=>Time.at(1000000)}); ` +
		`ix.index("c",{"tag"=>"y","n"=>500,"d"=>Time.at(1000)}); `
	for _, c := range []struct{ src, want string }{
		{setup + `f=Bleve::Facet.term("tag",10); r=ix.search(Bleve::Query.match_all, facets:{"tags"=>f}); ` +
			`p r.facets["tags"]["terms"].map{|t| [t["term"], t["count"]]}.sort`, "[[\"x\", 2], [\"y\", 1]]\n"},
		{setup + `f=Bleve::Facet.term("n",10).add_numeric_range("lo",nil,100).add_numeric_range("hi",100,nil); ` +
			`r=ix.search(Bleve::Query.match_all, facets:{"nn"=>f}); ` +
			`p r.facets["nn"]["numeric_ranges"].map{|x| [x["name"], x["count"]]}.sort`, "[[\"hi\", 1], [\"lo\", 2]]\n"},
		{setup + `f=Bleve::Facet.term("d",10).add_date_range("old",Time.at(0),Time.at(500000)).add_date_range("new",Time.at(500000),Time.at(2000000)); ` +
			`r=ix.search(Bleve::Query.match_all, facets:{"dd"=>f}); ` +
			`p r.facets["dd"]["date_ranges"].map{|x| [x["name"], x["count"]]}.sort`, "[[\"new\", 1], [\"old\", 2]]\n"},
		{setup + `f=Bleve::Facet.term("tag",10); r=ix.search(Bleve::Query.match_all, facets:{"tags"=>f}); ` +
			`p r.facets["tags"]["field"]`, "\"tag\"\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBleveErrors covers the exception surface and every argument-validation
// branch: the ClosedError / NotFoundError sentinels, the TypeErrors of the value
// bridge and option parsing, and the native-method arity checks.
func TestBleveErrors(t *testing.T) {
	const req = `require "bleve"; `
	rescueClass := func(body string) string {
		return `begin; ` + body + `; rescue => e; p e.class; end`
	}
	for _, c := range []struct{ src, want string }{
		// Sentinels.
		{rescueClass(`ix=Bleve.new_mem_index; ix.close; ix.count`), "Bleve::ClosedError\n"},
		{rescueClass(`Bleve.new_mem_index.document("nope")`), "Bleve::NotFoundError\n"},

		// TypeErrors from the value bridge and option parsing.
		{rescueClass(`Bleve.new_mem_index.index("a", 5)`), "TypeError\n"},
		{rescueClass(`Bleve.new_mem_index.index("a", {"x"=>(1..2)})`), "TypeError\n"},
		{rescueClass(`Bleve.new_mem_index(5)`), "TypeError\n"},
		{rescueClass(`Bleve.new_mem_index.search(5)`), "TypeError\n"},
		{rescueClass(`Bleve.new_mem_index.search(Bleve::Query.match_all, fields:5)`), "TypeError\n"},
		{rescueClass(`Bleve.new_mem_index.search(Bleve::Query.match_all, facets:5)`), "TypeError\n"},
		{rescueClass(`Bleve.new_mem_index.search(Bleve::Query.match_all, facets:{"x"=>5})`), "TypeError\n"},
		{rescueClass(`Bleve::Query.bool(must:5)`), "TypeError\n"},
		{rescueClass(`Bleve::Query.bool(must:[5])`), "TypeError\n"},
		{rescueClass(`Bleve::Query.date_range("x","y")`), "TypeError\n"},
		{rescueClass(`Bleve::Query.match("x").boost("hi")`), "TypeError\n"},

		// Arity (ArgumentError).
		{rescueClass(`Bleve.new`), "ArgumentError\n"},
		{rescueClass(`Bleve.open`), "ArgumentError\n"},
		{rescueClass(`Bleve::Query.match`), "ArgumentError\n"},
		{rescueClass(`Bleve.new_mem_index.index("a")`), "ArgumentError\n"},

		// LocalJumpError: batch without a block.
		{rescueClass(`Bleve.new_mem_index.batch`), "LocalJumpError\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBleveOnDisk covers the on-disk constructors: Bleve.new writes a fresh
// index at a path, Bleve.open reopens it (documents persist), and the failure
// paths (re-creating an existing path, opening a missing one) surface as
// Bleve::Error.
func TestBleveOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx")
	missing := filepath.Join(dir, "nope")

	// Create, index, close; reopen and read the count back.
	src := fmt.Sprintf(`require "bleve"
ix = Bleve.new(%q)
ix.index("a", {"t" => "hello"})
p ix.count
ix.close
re = Bleve.open(%q)
p re.count
p re.path == %q
re.close`, path, path, path)
	if got := eval(t, src); got != "1\n1\ntrue\n" {
		t.Errorf("on-disk round-trip got %q", got)
	}

	// Re-creating an existing path fails; opening a missing one fails.
	errSrc := fmt.Sprintf(`require "bleve"
begin; Bleve.new(%q); rescue => e; p e.class; end
begin; Bleve.open(%q); rescue => e; p e.class; end`, path, missing)
	if got := eval(t, errSrc); got != "Bleve::Error\nBleve::Error\n" {
		t.Errorf("on-disk error paths got %q", got)
	}
}
