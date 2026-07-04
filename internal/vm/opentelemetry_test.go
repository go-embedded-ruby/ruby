// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// otelSetup wires an SDK tracer provider with a simple processor and an in-memory
// exporter (E), installs it as the global provider (PV) and fetches a tracer (T),
// mirroring the opentelemetry-ruby setup dance. Every happy-path case runs on top
// of it.
const otelSetup = `require "opentelemetry"
E = OpenTelemetry::SDK::Trace::Export::InMemorySpanExporter.new
PV = OpenTelemetry::SDK::Trace::TracerProvider.new
PV.add_span_processor(OpenTelemetry::SDK::Trace::Export::SimpleSpanProcessor.new(E))
OpenTelemetry.tracer_provider = PV
T = OpenTelemetry.tracer_provider.tracer("app", "1.0")
`

// TestOpenTelemetry covers the Ruby OpenTelemetry module (backed by
// github.com/go-ruby-opentelemetry/opentelemetry, the pure-Go opentelemetry-ruby
// facade over the OpenTelemetry Go SDK): the tracer/span lifecycle (in_span /
// start_span), span enrichment (attributes, events, status, name, exception),
// the SpanContext identity surface and the in-memory exporter's finished-span
// snapshots.
func TestOpenTelemetry(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// in_span records the span name.
		{`T.in_span("work"){|s| }; p E.finished_spans[0].name`, "\"work\"\n"},
		// in_span attributes: keyword plus set_attribute both land on the span.
		{`T.in_span("w", attributes: {"a"=>1}){|s| s.set_attribute("b","x")}; p E.finished_spans[0].attributes`, "{\"a\" => 1, \"b\" => \"x\"}\n"},
		// Span kinds round-trip through the finished span.
		{`T.in_span("w", kind: :server){|s|}; p E.finished_spans[0].kind`, ":server\n"},
		{`T.in_span("w", kind: :client){|s|}; p E.finished_spans[0].kind`, ":client\n"},
		{`T.in_span("w", kind: :producer){|s|}; p E.finished_spans[0].kind`, ":producer\n"},
		{`T.in_span("w", kind: :consumer){|s|}; p E.finished_spans[0].kind`, ":consumer\n"},
		{`T.in_span("w", kind: :internal){|s|}; p E.finished_spans[0].kind`, ":internal\n"},
		{`T.in_span("w"){|s|}; p E.finished_spans[0].kind`, ":internal\n"},
		// add_event, with and without attributes.
		{`T.in_span("w"){|s| s.add_event("e", attributes: {"k"=>"v"})}; p E.finished_spans[0].events[0]`, "{\"name\" => \"e\", \"attributes\" => {\"k\" => \"v\"}}\n"},
		{`T.in_span("w"){|s| s.add_event("e2")}; p E.finished_spans[0].events[0]["attributes"]`, "{}\n"},
		// status= from a Status object and from a Symbol shorthand.
		{`T.in_span("w"){|s| s.status = OpenTelemetry::Trace::Status.error("bad")}; sp=E.finished_spans[0]; p [sp.status.code, sp.status.description]`, "[:error, \"bad\"]\n"},
		{`T.in_span("w"){|s| s.status = :ok}; p E.finished_spans[0].status.code`, ":ok\n"},
		{`T.in_span("w"){|s| s.status = :error}; p E.finished_spans[0].status.code`, ":error\n"},
		{`T.in_span("w"){|s| s.status = :unset}; p E.finished_spans[0].status.code`, ":unset\n"},
		// add_attributes merges a Hash of attributes.
		{`T.in_span("w"){|s| s.add_attributes("x"=>1, "y"=>true)}; p E.finished_spans[0].attributes`, "{\"x\" => 1, \"y\" => true}\n"},
		// name= renames the span.
		{`T.in_span("w"){|s| s.name = "renamed"}; p E.finished_spans[0].name`, "\"renamed\"\n"},
		// record_exception records an "exception" event.
		{`T.in_span("w"){|s| s.record_exception(RuntimeError.new("boom"))}; p E.finished_spans[0].events.map{|e| e["name"]}`, "[\"exception\"]\n"},
		// recording? is true for an SDK (AlwaysOn) span.
		{`T.in_span("w"){|s| p s.recording?}`, "true\n"},
		// SpanContext identity surface.
		{`T.in_span("w"){|s| c=s.context; p [c.hex_trace_id.length, c.hex_span_id.length, c.valid?, c.remote?]}`, "[32, 16, true, false]\n"},
		// start_span is finished by the caller (finish and its end alias).
		{`sp=T.start_span("m"); sp.set_attribute("k",1); sp.finish; p E.finished_spans[0].name`, "\"m\"\n"},
		{`sp=T.start_span("m2"); sp.end; p E.finished_spans[0].name`, "\"m2\"\n"},
		{`sp=T.start_span("m", attributes: {"a"=>1}, kind: :client); sp.finish; p E.finished_spans[0].kind`, ":client\n"},
		// OpenTelemetry.tracer is a convenience over the global provider.
		{`OpenTelemetry.tracer("x").in_span("w"){}; p E.finished_spans[0].name`, "\"w\"\n"},
		// Finished-span hex ids and reset.
		{`T.in_span("w"){}; sp=E.finished_spans[0]; p [sp.hex_trace_id.length, sp.hex_span_id.length]`, "[32, 16]\n"},
		{`T.in_span("w"){}; E.reset; p E.finished_spans.length`, "0\n"},
		// A nested in_span is a child of the current span: same trace id.
		{`T.in_span("outer"){|o| T.in_span("inner"){|i|}}; s=E.finished_spans; p(s[0].hex_trace_id == s[1].hex_trace_id)`, "true\n"},
		// Status constructors and accessors.
		{`p OpenTelemetry::Trace::Status.ok.code`, ":ok\n"},
		{`p OpenTelemetry::Trace::Status.unset.code`, ":unset\n"},
		{`p OpenTelemetry::Trace::Status.error.description`, "\"\"\n"},
		// The batch processor constructs (it buffers; export timing is the SDK's).
		{`p OpenTelemetry::SDK::Trace::Export::BatchSpanProcessor.new(E).class`, "OpenTelemetry::SDK::Trace::Export::BatchSpanProcessor\n"},
		// require reports the feature as provided.
		{`p require "opentelemetry"`, "false\n"},
	} {
		if got := eval(t, otelSetup+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOpenTelemetryAttributes covers the Ruby<->attribute value bridge across the
// scalar types, their homogeneous array forms, the non-homogeneous fallback to a
// stringified array, an empty array, a Symbol value and a nil value.
func TestOpenTelemetryAttributes(t *testing.T) {
	const src = `T.in_span("w", attributes: {"i"=>7,"f"=>1.5,"b"=>true,"s"=>"x","sym"=>:y,` +
		`"ai"=>[1,2],"af"=>[1.5,2.5],"ab"=>[true,false],"as"=>["a","b"],` +
		`"imix"=>[1,"x"],"fmix"=>[1.5,"x"],"bmix"=>[true,"x"],"smix"=>["a",1],"empty"=>[]}) do |s|
  s.set_attribute("nilv", nil)
end
p E.finished_spans[0].attributes`
	const want = `{"ab" => [true, false], "af" => [1.5, 2.5], "ai" => [1, 2], "as" => ["a", "b"], ` +
		`"b" => true, "bmix" => ["true", "x"], "empty" => [], "f" => 1.5, "fmix" => ["1.5", "x"], ` +
		`"i" => 7, "imix" => ["1", "x"], "nilv" => "", "s" => "x", "smix" => ["a", "1"], "sym" => "y"}` + "\n"
	if got := eval(t, otelSetup+src); got != want {
		t.Errorf("attributes got=%q want=%q", got, want)
	}
}

// TestOpenTelemetryInSpan covers the in_span block lifecycle guarantees: a raised
// exception is recorded on the span, its status set to error, and re-raised; a
// non-exception unwind (a block break) passes through while the span is still
// finished.
func TestOpenTelemetryInSpan(t *testing.T) {
	const raiseSrc = `begin
  T.in_span("boom") { |s| raise "kaboom" }
rescue => e
  puts "rescued: #{e.message}"
end
sp = E.finished_spans[0]
puts "#{sp.name} #{sp.status.code} #{sp.events.map{|x| x["name"]}}"`
	if got := eval(t, otelSetup+raiseSrc); got != "rescued: kaboom\nboom error [\"exception\"]\n" {
		t.Errorf("raise path got=%q", got)
	}

	const breakSrc = `def wrap; T.in_span("brk") { |s| break 99 }; end
p wrap
p E.finished_spans[0].name`
	if got := eval(t, otelSetup+breakSrc); got != "99\n\"brk\"\n" {
		t.Errorf("break path got=%q", got)
	}
}

// TestOpenTelemetryWrappers covers the Go wrapper render/truthiness surface
// (to_s, inspect, boolean context) for every OpenTelemetry object, including the
// default API tracer provider before the SDK is installed.
func TestOpenTelemetryWrappers(t *testing.T) {
	// The default provider (before the SDK is installed) is the API provider.
	if got := eval(t, `require "opentelemetry"; p OpenTelemetry.tracer_provider`); got != "#<OpenTelemetry::Trace::TracerProvider>\n" {
		t.Errorf("default provider got=%q", got)
	}
	for _, c := range []struct{ src, want string }{
		{`p PV`, "#<OpenTelemetry::SDK::Trace::TracerProvider>\n"},
		{`puts PV`, "#<OpenTelemetry::SDK::Trace::TracerProvider>\n"},
		{`p(PV ? 1 : 2)`, "1\n"},
		{`p T`, "#<OpenTelemetry::Trace::Tracer>\n"},
		{`puts T`, "#<OpenTelemetry::Trace::Tracer>\n"},
		{`p(T ? 1 : 2)`, "1\n"},
		{`T.in_span("w"){|s| p s; puts s; p(s ? 1 : 2)}`, "#<OpenTelemetry::Trace::Span>\n#<OpenTelemetry::Trace::Span>\n1\n"},
		{`T.in_span("w"){|s| c=s.context; p c; puts c; p(c ? 1 : 2)}`, "#<OpenTelemetry::Trace::SpanContext>\n#<OpenTelemetry::Trace::SpanContext>\n1\n"},
		{`st=OpenTelemetry::Trace::Status.ok; p st; puts st; p(st ? 1 : 2)`, "#<OpenTelemetry::Trace::Status>\n#<OpenTelemetry::Trace::Status>\n1\n"},
		{`p E; puts E; p(E ? 1 : 2)`, "#<OpenTelemetry::SDK::Trace::Export::InMemorySpanExporter>\n#<OpenTelemetry::SDK::Trace::Export::InMemorySpanExporter>\n1\n"},
		{`sp=OpenTelemetry::SDK::Trace::Export::SimpleSpanProcessor.new(E); p sp; puts sp; p(sp ? 1 : 2)`, "#<OpenTelemetry::SDK::Trace::Export::SimpleSpanProcessor>\n#<OpenTelemetry::SDK::Trace::Export::SimpleSpanProcessor>\n1\n"},
		{`T.in_span("fin"){}; d=E.finished_spans[0]; p d; puts d; p(d ? 1 : 2)`, "#<OpenTelemetry::SDK::Trace::SpanData name=fin>\n#<OpenTelemetry::SDK::Trace::SpanData name=fin>\n1\n"},
	} {
		if got := eval(t, otelSetup+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOpenTelemetryErrors covers the argument/type validation across the module:
// wrong arities, non-String/Symbol names, non-Hash attribute bundles, an unknown
// span kind/status, a missing in_span block, add_span_processor on the non-SDK
// provider, and the processor's exporter check.
func TestOpenTelemetryErrors(t *testing.T) {
	// add_span_processor is undefined on the API (non-SDK) provider, which is the
	// global provider before the SDK installs one.
	if err := runErr(t, `require "opentelemetry"; OpenTelemetry.tracer_provider.add_span_processor(nil)`); err == nil || !strings.Contains(err.Error(), "NoMethodError") {
		t.Errorf("add_span_processor on API provider got=%v want NoMethodError", err)
	}
	for _, c := range []struct{ src, want string }{
		{`OpenTelemetry.tracer`, "ArgumentError"},                                       // tracer, no name
		{`OpenTelemetry.tracer(7)`, "TypeError"},                                        // tracer name not String/Symbol
		{`OpenTelemetry.tracer("x", 7)`, "TypeError"},                                   // tracer version not String/Symbol
		{`OpenTelemetry.tracer_provider = 7`, "TypeError"},                              // provider not a provider
		{`PV.tracer`, "ArgumentError"},                                                  // provider#tracer, no name
		{`PV.add_span_processor`, "ArgumentError"},                                      // no processor
		{`PV.add_span_processor(7)`, "TypeError"},                                       // not a processor
		{`T.in_span`, "ArgumentError"},                                                  // in_span, no name
		{`T.in_span("x")`, "LocalJumpError"},                                            // in_span, no block
		{`T.in_span(7){}`, "TypeError"},                                                 // in_span name not String/Symbol
		{`T.in_span("w", attributes: 7){}`, "TypeError"},                                // attributes: not a Hash
		{`T.in_span("w", kind: :nope){}`, "ArgumentError"},                              // unknown kind
		{`T.start_span`, "ArgumentError"},                                               // start_span, no name
		{`T.start_span(7)`, "TypeError"},                                                // start_span name not String/Symbol
		{`T.in_span("w"){|s| s.set_attribute("k")}`, "ArgumentError"},                   // set_attribute arity
		{`T.in_span("w"){|s| s.set_attribute(7, 1)}`, "TypeError"},                      // set_attribute key not String/Symbol
		{`T.in_span("w"){|s| s.add_attributes}`, "ArgumentError"},                       // add_attributes arity
		{`T.in_span("w"){|s| s.add_attributes(7)}`, "TypeError"},                        // add_attributes not a Hash
		{`T.in_span("w"){|s| s.add_event}`, "ArgumentError"},                            // add_event arity
		{`T.in_span("w"){|s| s.add_event("e", attributes: 7)}`, "TypeError"},            // add_event attributes: not a Hash
		{`T.in_span("w"){|s| s.record_exception}`, "ArgumentError"},                     // record_exception arity
		{`T.in_span("w"){|s| s.status = :nope}`, "ArgumentError"},                       // unknown status
		{`OpenTelemetry::Trace::Status.error(7)`, "TypeError"},                          // status description not String/Symbol
		{`OpenTelemetry::SDK::Trace::Export::SimpleSpanProcessor.new`, "ArgumentError"}, // processor, no exporter
		{`OpenTelemetry::SDK::Trace::Export::SimpleSpanProcessor.new(7)`, "TypeError"},  // processor, bad exporter
	} {
		if err := runErr(t, otelSetup+c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
