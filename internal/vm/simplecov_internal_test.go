// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// scRun runs a Ruby program with `require "simplecov"` already prepended,
// returning its trimmed stdout. Every SimpleCov test drives the whole binding
// through rbgo, exactly as a user would.
func scRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"simplecov\"\n"+body)
}

// TestSimpleCovStartResultFlow drives the headline flow: SimpleCov.start with a
// DSL config block (a filter, a group, a threshold), feeding raw coverage through
// the deferred VM line-coverage seam (SimpleCov.add_coverage), then reading the
// SimpleCov::Result model — covered_percent, files, groups, the strength and line
// counters — and the SimpleFormatter text output.
func TestSimpleCovStartResultFlow(t *testing.T) {
	got := scRun(t, `
SimpleCov.start do
  add_filter "/test/"
  add_group "Models", "app/models"
  minimum_coverage 90
end
SimpleCov.add_coverage "/proj/app/models/user.rb", [nil, 1, 1, 0]
SimpleCov.add_coverage "/proj/test/user_test.rb", [1, 1]
r = SimpleCov.result
puts r.covered_percent.round(2)
puts r.covered_lines
puts r.missed_lines
puts r.total_lines
puts r.covered_strength.round(4)
puts r.files.map(&:filename).inspect
puts r.source_files.length
puts r.groups.keys.inspect
puts r.groups["Models"].map(&:filename).inspect
puts r.command_name
puts r.created_at.class
puts r.least_covered_file.filename
p r
puts r.to_s
puts !!r
puts r.files.first.to_s
`)
	want := strings.Join([]string{
		"66.67",                        // 2 covered of 3 relevant
		"2",                            // covered_lines
		"1",                            // missed_lines
		"3",                            // total_lines (relevant)
		"0.7",                          // covered_strength (2 hits / 3 lines, engine rounds to 1 decimal)
		`["/proj/app/models/user.rb"]`, // test file filtered out
		"1",                            // source_files count
		`["Models"]`,                   // group names
		`["/proj/app/models/user.rb"]`, // group members
		"rbgo",                         // command_name
		"Time",                         // created_at class
		"/proj/app/models/user.rb",     // least covered
		"#<SimpleCov::Result>",         // inspect
		"#<SimpleCov::Result>",         // to_s
		"true",                         // truthy
		"#<SimpleCov::SourceFile>",     // source-file to_s
	}, "\n")
	if got != want {
		t.Fatalf("start/result flow:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestSimpleCovSourceFileMetrics drives every SimpleCov::SourceFile accessor.
func TestSimpleCovSourceFileMetrics(t *testing.T) {
	got := scRun(t, `
SimpleCov.root "/proj"
SimpleCov.add_coverage "/proj/lib/a.rb", [nil, 1, 0, 2, nil]
sf = SimpleCov.result.files.first
puts sf.filename
puts sf.project_filename
puts sf.covered_percent.round(2)
puts sf.covered_strength.round(4)
puts sf.covered_lines
puts sf.missed_lines
puts sf.never_lines
puts sf.lines_of_code
puts sf.relevant_lines
puts sf.lines.inspect
p sf
puts !!sf
`)
	want := strings.Join([]string{
		"/proj/lib/a.rb",
		"/lib/a.rb",
		"66.67", // 2 covered of 3 relevant
		"1.0",   // (1+0+2)/3
		"2",
		"1",
		"2",
		"3",
		"3",
		"[nil, 1, 0, 2, nil]",
		"#<SimpleCov::SourceFile>",
		"true",
	}, "\n")
	if got != want {
		t.Fatalf("source-file metrics:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestSimpleCovResultConstructors drives SimpleCov::Result.new (bare-Array and
// {"lines"=>…} coverage shapes, with and without a command_name:) and the
// .from_hash / #to_hash resultset round-trip.
func TestSimpleCovResultConstructors(t *testing.T) {
	got := scRun(t, `
r1 = SimpleCov::Result.new({"/a.rb" => [nil, 1, 0]})
puts r1.covered_percent.round(2)
puts r1.command_name
r2 = SimpleCov::Result.new({"/a.rb" => {"lines" => [1, 1]}}, command_name: "Cukes")
puts r2.covered_percent.round(2)
puts r2.command_name
h = r1.to_hash
puts h.keys.inspect
puts h["rbgo"]["coverage"]["/a.rb"]["lines"].inspect
puts (h["rbgo"]["timestamp"].is_a?(Integer))
r3 = SimpleCov::Result.from_hash(r1.to_resultset)
puts r3.covered_percent.round(2)
`)
	want := strings.Join([]string{
		"50.0", "rbgo",
		"100.0", "Cukes",
		`["rbgo"]`,
		"[nil, 1, 0]",
		"true",
		"50.0",
	}, "\n")
	if got != want {
		t.Fatalf("result constructors:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestSimpleCovFiltersAndGroups drives every add_filter / add_group form: a
// Regexp filter (with i and m flags), an Array filter, a block filter, and a
// Regexp and block group.
func TestSimpleCovFiltersAndGroups(t *testing.T) {
	got := scRun(t, `
SimpleCov.start do
  add_filter %r{/VENDOR/}i
  add_filter %r{generated}m
  add_filter ["/spec/"]
  add_filter { |sf| sf.filename.end_with?("_pb.rb") }
  add_group "Regexpish", %r{/lib/}
  add_group("Blockish") { |sf| sf.filename.include?("app") }
end
SimpleCov.add_coverage "/proj/lib/keep.rb", [1, 1]
SimpleCov.add_coverage "/proj/app/keep2.rb", [1, 0]
SimpleCov.add_coverage "/proj/vendor/skip.rb", [1, 1]
SimpleCov.add_coverage "/proj/x/generated/skip.rb", [1, 1]
SimpleCov.add_coverage "/proj/spec/skip.rb", [1, 1]
SimpleCov.add_coverage "/proj/lib/thing_pb.rb", [1, 1]
r = SimpleCov.result
puts r.files.map(&:filename).sort.inspect
puts r.groups["Regexpish"].map(&:filename).inspect
puts r.groups["Blockish"].map(&:filename).inspect
`)
	want := strings.Join([]string{
		`["/proj/app/keep2.rb", "/proj/lib/keep.rb"]`,
		`["/proj/lib/keep.rb"]`,
		`["/proj/app/keep2.rb"]`,
	}, "\n")
	if got != want {
		t.Fatalf("filters/groups:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestSimpleCovThresholdChecks drives the threshold surface: minimum_coverage
// (numeric and Hash forms), minimum_coverage_by_file, maximum_coverage_drop and
// refuse_coverage_drop, and the run_checks exit codes (success, below minimum,
// excessive drop).
func TestSimpleCovThresholdChecks(t *testing.T) {
	got := scRun(t, `
SimpleCov.start
SimpleCov.add_coverage "/a.rb", [1, 1, 0, 1] # 75%
r = SimpleCov.result
SimpleCov.minimum_coverage 50
puts SimpleCov.run_checks(r)                 # 0 success
SimpleCov.start
SimpleCov.add_coverage "/a.rb", [1, 1, 0, 1]
r = SimpleCov.result
SimpleCov.minimum_coverage(line: 90)
puts SimpleCov.run_checks(r)                 # 2 below minimum
SimpleCov.start
SimpleCov.add_coverage "/a.rb", [1, 1, 0, 1] # 75%
low = SimpleCov.result
SimpleCov.start
SimpleCov.add_coverage "/a.rb", [1, 1, 1, 1] # 100%
high = SimpleCov.result
SimpleCov.maximum_coverage_drop 5
puts SimpleCov.run_checks(low, high)         # 3 dropped too far
SimpleCov.minimum_coverage_by_file 10
SimpleCov.maximum_coverage_drop 2.5           # Float arm of simpleCovFloat
SimpleCov.minimum_coverage("branch" => 50)    # String criterion arm + branch criterion
SimpleCov.refuse_coverage_drop :line
SimpleCov.refuse_coverage_drop
puts SimpleCov.result?
`)
	want := strings.Join([]string{"0", "2", "3", "true"}, "\n")
	if got != want {
		t.Fatalf("threshold checks:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestSimpleCovConfigAccessors drives command_name / root getters and setters,
// the coverage getter and bulk coverage= setter, configure, and formatter
// get/set.
func TestSimpleCovConfigAccessors(t *testing.T) {
	got := scRun(t, `
SimpleCov.start
puts SimpleCov.command_name
puts SimpleCov.command_name("RSpec")
puts SimpleCov.root
puts SimpleCov.root("/here")
SimpleCov.configure { minimum_coverage 1 }
SimpleCov.coverage = {"/a.rb" => [1, 0]}
puts SimpleCov.coverage.inspect
puts SimpleCov.result?
puts SimpleCov.formatter.class
f = SimpleCov::Formatter::SimpleFormatter.new
SimpleCov.formatter = f
puts (SimpleCov.formatter.equal?(f))
`)
	want := strings.Join([]string{
		"rbgo",
		"RSpec",
		"",
		"/here",
		`{"/a.rb" => [1, 0]}`,
		"true",
		"SimpleCov::Formatter::SimpleFormatter",
		"true",
	}, "\n")
	if got != want {
		t.Fatalf("config accessors:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestSimpleCovAtExit drives at_exit in both forms: registering a hook block, then
// the no-block invocation that runs the hook, formats through the active
// formatter and returns the exit code from the threshold checks.
func TestSimpleCovAtExit(t *testing.T) {
	got := scRun(t, `
$ran = []
SimpleCov.start
SimpleCov.add_coverage "/a.rb", [1, 0]  # 50%
SimpleCov.minimum_coverage 90
SimpleCov.at_exit { $ran << :hook }
code = SimpleCov.at_exit
puts $ran.inspect
puts code
`)
	want := strings.Join([]string{"[:hook]", "2"}, "\n")
	if got != want {
		t.Fatalf("at_exit:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestSimpleCovFormatter drives the SimpleFormatter directly: a grouped result
// renders its sections, and an ungrouped one renders nothing (as in SimpleCov).
func TestSimpleCovFormatter(t *testing.T) {
	got := scRun(t, `
SimpleCov.start { add_group "Lib", "/lib/" }
SimpleCov.add_coverage "/lib/a.rb", [1, 0]
out = SimpleCov::Formatter::SimpleFormatter.new.format(SimpleCov.result)
puts out.include?("Group: Lib")
puts out.include?("50.0")
SimpleCov.start
SimpleCov.add_coverage "/lib/a.rb", [1, 0]
puts SimpleCov::Formatter::SimpleFormatter.new.format(SimpleCov.result).empty?
`)
	want := strings.Join([]string{"true", "true", "true"}, "\n")
	if got != want {
		t.Fatalf("formatter:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestSimpleCovResultsetRoundTrip drives store_resultset / load_resultset through
// the engine's FS seam: a resultset written to a temp file loads back byte-equal.
func TestSimpleCovResultsetRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".resultset.json")
	src := fmt.Sprintf(`PATH = %q
rs = {"RSpec" => {"coverage" => {"/a.rb" => {"lines" => [nil, 1, 0]}}, "timestamp" => 1700000000}}
SimpleCov.store_resultset(PATH, rs)
back = SimpleCov.load_resultset(PATH)
puts back.keys.inspect
puts back["RSpec"]["coverage"]["/a.rb"]["lines"].inspect
puts back["RSpec"]["timestamp"]
`, path)
	got := scRun(t, src)
	want := strings.Join([]string{`["RSpec"]`, "[nil, 1, 0]", "1700000000"}, "\n")
	if got != want {
		t.Fatalf("resultset round-trip:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestSimpleCovEmptyResult drives the empty path: no coverage supplied yields an
// empty result (100% by SimpleCov convention) with no least-covered file and no
// files — proving no coverage source is fabricated.
func TestSimpleCovEmptyResult(t *testing.T) {
	got := scRun(t, `
SimpleCov.start
r = SimpleCov.result
puts r.files.inspect
puts r.least_covered_file.inspect
puts r.covered_percent
puts SimpleCov.result?
`)
	want := strings.Join([]string{"[]", "nil", "100.0", "false"}, "\n")
	if got != want {
		t.Fatalf("empty result:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// scErr runs a snippet expected to raise, returning "Class: message".
func scErr(t *testing.T, body string) string {
	t.Helper()
	return scRun(t, "begin\n"+body+"\nrescue Exception => e\n  puts \"#{e.class}: #{e.message}\"\nend")
}

// TestSimpleCovErrors drives every raising branch across both files: bad argument
// counts, wrong types, unknown criteria, malformed coverage/resultset shapes, an
// unparseable Regexp filter, an empty from_hash and FS faults.
func TestSimpleCovErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "missing", "nope.json") // parent dir absent -> write fault
	corrupt := filepath.Join(dir, "corrupt.json")     // exists but unparseable -> load fault
	cases := []struct{ body, want string }{
		{`SimpleCov.add_filter`, "ArgumentError: wrong number of arguments (given 0, expected 1)"},
		{`SimpleCov.add_filter 5`, "TypeError: filter must be a String, Regexp or Array, got 5"},
		{`SimpleCov.add_group`, "ArgumentError: wrong number of arguments (given 0, expected 1..2)"},
		{`SimpleCov.add_group "X"`, "ArgumentError: add_group requires a filter argument or a block"},
		{`SimpleCov.add_group "X", 5`, "TypeError: group filter must be a String or Regexp, got 5"},
		{`SimpleCov.minimum_coverage`, "ArgumentError: wrong number of arguments (given 0, expected 1)"},
		{`SimpleCov.minimum_coverage "x"`, "TypeError: coverage threshold must be numeric, got \"x\""},
		{`SimpleCov.minimum_coverage(bogus: 1)`, "ArgumentError: unknown coverage criterion \"bogus\""},
		{`SimpleCov.minimum_coverage(5 => 1)`, "TypeError: coverage criterion must be a Symbol or String, got 5"},
		{`SimpleCov.add_coverage "a"`, "ArgumentError: wrong number of arguments (given 1, expected 2)"},
		{`SimpleCov.add_coverage "a", 5`, "TypeError: coverage lines must be an Array, got 5"},
		{`SimpleCov.add_coverage "a", [1, "x"]`, "TypeError: coverage line must be an Integer or nil, got \"x\""},
		{`SimpleCov.coverage = 5`, "TypeError: coverage must be a Hash, got 5"},
		{`SimpleCov.coverage = {5 => [1]}`, "TypeError: coverage filename must be a String, got 5"},
		{`SimpleCov.coverage = {"a" => 5}`, "TypeError: coverage entry must be an Array or Hash, got 5"},
		{`SimpleCov.coverage = {"a" => {}}`, "TypeError: coverage entry hash must carry a \"lines\" Array, got {}"},
		{`SimpleCov.coverage = {"a" => {"lines" => 5}}`, "TypeError: coverage entry hash must carry a \"lines\" Array, got {\"lines\" => 5}"},
		{`SimpleCov.run_checks`, "ArgumentError: wrong number of arguments (given 0, expected 1..2)"},
		{`SimpleCov.run_checks 5`, "TypeError: expected a SimpleCov::Result, got 5"},
		{`SimpleCov::Result.new`, "ArgumentError: wrong number of arguments (given 0, expected 1)"},
		{`SimpleCov::Result.new 5`, "TypeError: coverage must be a Hash, got 5"},
		{`SimpleCov::Result.from_hash`, "ArgumentError: wrong number of arguments (given 0, expected 1)"},
		{`SimpleCov::Result.from_hash 5`, "TypeError: resultset must be a Hash, got 5"},
		{`SimpleCov::Result.from_hash({})`, "ArgumentError: resultset is empty"},
		{`SimpleCov::Result.from_hash({5 => {}})`, "TypeError: resultset command must be a String, got 5"},
		{`SimpleCov::Result.from_hash({"c" => 5})`, "TypeError: resultset entry must be a Hash, got 5"},
		{`SimpleCov::Result.from_hash({"c" => {"coverage" => 5}})`, "TypeError: resultset coverage must be a Hash, got 5"},
		{`SimpleCov.store_resultset "only-one-arg"`, "ArgumentError: wrong number of arguments (given 1, expected 2)"},
		{`SimpleCov.store_resultset 5, 6`, "TypeError: resultset must be a Hash, got 6"},
		{`SimpleCov::Formatter::SimpleFormatter.new.format`, "ArgumentError: wrong number of arguments (given 0, expected 1)"},
		{`SimpleCov.start { add_filter(/(?=x)/) }`, ""}, // RegexpError message is engine-specific; matched below
		{fmt.Sprintf("File.write(%q, \"{bad json\")\nSimpleCov.load_resultset %q", corrupt, corrupt), ""},
		{fmt.Sprintf(`SimpleCov.store_resultset(%q, {"c" => {"coverage" => {}, "timestamp" => 1}})`, bad), ""},
	}
	for _, c := range cases {
		got := scErr(t, c.body)
		switch {
		case strings.Contains(c.body, "(?=x)"):
			if !strings.HasPrefix(got, "RegexpError:") {
				t.Errorf("regexp filter: got %q, want RegexpError", got)
			}
		case strings.Contains(c.body, "load_resultset"):
			if !strings.HasPrefix(got, "RuntimeError:") {
				t.Errorf("load fault: got %q, want RuntimeError", got)
			}
		case strings.Contains(c.body, "store_resultset(") && strings.Contains(c.body, "coverage"):
			if !strings.HasPrefix(got, "RuntimeError:") {
				t.Errorf("store fault: got %q, want RuntimeError", got)
			}
		default:
			if got != c.want {
				t.Errorf("body %q:\n got:  %q\n want: %q", c.body, got, c.want)
			}
		}
	}
}
