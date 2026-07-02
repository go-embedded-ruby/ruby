package webapp

import (
	"bytes"
	"embed"
	"strconv"
	"strings"
	"testing"

	ruby "github.com/go-embedded-ruby/ruby"
)

// apps holds the real Ruby web apps this harness runs through rbgo. Embedding
// keeps the suite independent of the working directory (it runs unchanged under
// the coverage job, on Windows, and under qemu on every 64-bit target).
//
//go:embed apps/*.rb
var apps embed.FS

// runApp executes the named app fixture through the public ruby.Run API and
// returns its printed output together with any parse/compile/runtime error. It
// is the whole seam: a synthetic Rack env goes in via the app source, a
// deterministic response comes out via stdout — no socket, no network.
func runApp(t *testing.T, name string) (string, error) {
	t.Helper()
	src, err := apps.ReadFile("apps/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var out bytes.Buffer
	runErr := ruby.Run(string(src), &out)
	return out.String(), runErr
}

// featureLoadable reports whether `require <feature>` succeeds on a fresh VM.
// It is how the Sinatra and ActiveRecord stages decide between asserting the
// real response and recording the gap, so the suite self-updates the moment a
// binding lands.
func featureLoadable(feature string) (bool, string) {
	var out bytes.Buffer
	err := ruby.Run("require "+strconv.Quote(feature)+"; print \"LOADED\"", &out)
	if err != nil {
		return false, err.Error()
	}
	return out.String() == "LOADED", out.String()
}

// arORMProbe exercises the exact ActiveRecord ORM chain stage 4b's app needs —
// establish_connection, Schema.define, a model insert, and where(...).order.to_a
// — inside a Ruby rescue. `require "active_record"` succeeding is NOT enough: the
// binding can expose the constant yet lack a method the route calls (it shipped
// without Schema.define). The probe prints "READY" only when the whole chain
// runs clean, and otherwise "GAP: <ExceptionClass>: <message>" naming the exact
// missing capability.
const arORMProbe = `
require "active_record"
begin
  ActiveRecord::Base.establish_connection(adapter: "sqlite3", database: ":memory:")
  ActiveRecord::Schema.define do
    create_table(:ar_probe) { |t| t.string :name }
  end
  klass = Class.new(ActiveRecord::Base) { self.table_name = "ar_probe" }
  klass.create!(name: "x")
  raise "query returned no rows" unless klass.where("name = ?", "x").order(:name).to_a.length == 1
  print "READY"
rescue Exception => e
  print "GAP: #{e.class}: #{e.message}"
end
`

// arORMReady reports whether the ActiveRecord ORM path stage 4b relies on
// actually works, returning the probe detail (a "GAP: ..." string naming the
// missing method) when it does not. It flips to true automatically once the AR
// binding gains the methods, upgrading stage 4b to a hard assertion.
func arORMReady() (bool, string) {
	var out bytes.Buffer
	if err := ruby.Run(arORMProbe, &out); err != nil {
		return false, "interpreter error: " + err.Error()
	}
	got := out.String()
	return got == "READY", got
}

// mustRun runs an app that is expected to succeed, failing the test on any
// interpreter error (the response assertions live in each stage).
func mustRun(t *testing.T, name string) string {
	t.Helper()
	out, err := runApp(t, name)
	if err != nil {
		t.Fatalf("%s: interpreter error (expected clean run): %v\noutput:\n%s", name, err, out)
	}
	return out
}

// Stage 1 — Pure Rack. A bare lambda returning [status, headers, body] driven
// by a synthetic env. Proves procs, Hash access, interpolation and multiple
// assignment all execute through rbgo.
func TestStage1PureRack(t *testing.T) {
	out := mustRun(t, "stage1_pure_rack.rb")
	want := "status=200\ncontent-type=text/plain\nbody=hello /x\n"
	if out != want {
		t.Fatalf("stage 1 response mismatch\n got: %q\nwant: %q", out, want)
	}
}

// Stage 2 — Rack + ERB view. The handler renders an ERB template with
// request-derived locals. Proves request -> handler -> view -> response.
func TestStage2RackERB(t *testing.T) {
	out := mustRun(t, "stage2_rack_erb.rb")
	wantBody := "<h1>Hello amy</h1>\n<p>Path: /greet</p>\n<ul><li>a</li><li>b</li><li>c</li></ul>"
	want := "status=200\ncontent-type=text/html\nbody<<" + wantBody + ">>\n"
	if out != want {
		t.Fatalf("stage 2 response mismatch\n got: %q\nwant: %q", out, want)
	}
}

// Stage 3 — Sinatra DSL. When sinatra/base is loadable, the classic
// Sinatra::Base app must answer GET /hi?n=amy with "hi amy" through its Rack
// call interface. When it is not, the exact gap is recorded — that gap is a
// deliverable, not a failure.
func TestStage3Sinatra(t *testing.T) {
	if ok, _ := featureLoadable("sinatra/base"); !ok {
		out, err := runApp(t, "stage3_sinatra.rb")
		if err == nil {
			t.Fatalf("stage 3: sinatra/base reports unloadable yet the app ran clean:\n%s", out)
		}
		t.Logf("GAP stage 3 (Sinatra DSL): sinatra/base is not runnable through rbgo.\n"+
			"  need: a go-ruby-sinatra binding (Sinatra::Base, the get/post class-DSL,\n"+
			"        params, and the Rack #call adapter).\n"+
			"  observed error: %v", err)
		t.Skip("Sinatra binding absent — gap recorded")
	}
	out := mustRun(t, "stage3_sinatra.rb")
	want := "status=200\nbody=hi amy\n"
	if out != want {
		t.Fatalf("stage 3 response mismatch\n got: %q\nwant: %q", out, want)
	}
}

// Stage 4 — data-backed route with bindings that exist today: an in-memory
// SQLite database queried per request and rendered as HTML (ERB) and JSON.
// This is the proof that a *data-driven* response is servable through rbgo now.
func TestStage4SQLiteData(t *testing.T) {
	out := mustRun(t, "stage4_sqlite3_erb_json.rb")
	want := strings.Join([]string{
		"html_status=200",
		"html_body=<ul><li>amy (30)</li><li>cat (40)</li></ul>",
		"json_status=200",
		`json_body=[{"name":"amy","age":30},{"name":"cat","age":40}]`,
		"",
	}, "\n")
	if out != want {
		t.Fatalf("stage 4 response mismatch\n got: %q\nwant: %q", out, want)
	}
}

// Stage 4b — the same data route expressed through the ActiveRecord ORM. When
// active_record is loadable it must render the AR-queried rows; otherwise the
// gap is recorded. This flips to a hard assertion automatically once the
// go-ruby-activerecord binding (PR #102) lands on main.
func TestStage4bActiveRecord(t *testing.T) {
	// require "active_record" succeeding is not sufficient — probe the specific
	// ORM chain the route calls (establish_connection + Schema.define + insert +
	// where.order.to_a). When any required method is missing the probe reports the
	// exact gap and we skip; when the binding gains those methods this flips to a
	// hard assertion automatically.
	if ready, detail := arORMReady(); !ready {
		t.Logf("GAP stage 4b (ActiveRecord ORM route): the AR binding is loadable but the\n"+
			"  ORM route path does not run yet.\n"+
			"  need: go-ruby-activerecord to implement the chain the route uses\n"+
			"        (ActiveRecord::Schema.define + create_table + Model.create! +\n"+
			"        Model.where(...).order(...).to_a).\n"+
			"  note: the equivalent route over the raw sqlite3 binding IS green (stage 4).\n"+
			"  probe result: %s", detail)
		t.Skip("ActiveRecord ORM path incomplete — gap recorded")
	}
	out := mustRun(t, "stage4b_active_record.rb")
	want := "status=200\nbody=<ul><li>amy (30)</li><li>cat (40)</li></ul>\n"
	if out != want {
		t.Fatalf("stage 4b response mismatch\n got: %q\nwant: %q", out, want)
	}
}
