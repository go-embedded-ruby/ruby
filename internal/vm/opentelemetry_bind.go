// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"fmt"
	"sort"

	rbcontext "github.com/go-ruby-opentelemetry/opentelemetry/context"
	sdktrace "github.com/go-ruby-opentelemetry/opentelemetry/sdk/trace"
	rbtrace "github.com/go-ruby-opentelemetry/opentelemetry/trace"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin value bridge between rbgo's Ruby object graph and
// github.com/go-ruby-opentelemetry/opentelemetry — the pure-Go (CGO=0),
// opentelemetry-ruby-faithful facade over the OpenTelemetry Go SDK. The tracing
// model (span recording, sampling, W3C context, the in-memory exporter seam)
// lives in that library; this file only maps Ruby scalars/hashes onto the
// library's attribute model and back, drives a Ruby block from the library's
// span lifecycle, and holds the Go wrapper structs the Ruby classes wrap (see
// opentelemetry.go for the class/method surface). Attribute round-tripping goes
// through the SDK's native attribute types, so the same values are recorded
// identically across all six supported 64-bit arches, big-endian included.

// OTelTracerProvider is the Ruby wrapper around an OpenTelemetry tracer provider.
// It carries the SDK provider when one was configured (so add_span_processor is
// available) and the class it reports (the API provider by default, the SDK
// provider once OpenTelemetry::SDK::Trace::TracerProvider.new built it).
type OTelTracerProvider struct {
	sdk *sdktrace.TracerProvider
	tp  rbtrace.TracerProvider
	cls *RClass
}

func (p *OTelTracerProvider) ToS() string     { return "#<" + p.cls.name + ">" }
func (p *OTelTracerProvider) Inspect() string { return p.ToS() }
func (p *OTelTracerProvider) Truthy() bool    { return true }

// OTelTracer is the Ruby wrapper around an OpenTelemetry::Trace::Tracer.
type OTelTracer struct{ tr *rbtrace.Tracer }

func (t *OTelTracer) ToS() string     { return "#<OpenTelemetry::Trace::Tracer>" }
func (t *OTelTracer) Inspect() string { return t.ToS() }
func (t *OTelTracer) Truthy() bool    { return true }

// OTelSpan is the Ruby wrapper around an OpenTelemetry::Trace::Span.
type OTelSpan struct{ span rbtrace.Span }

func (s *OTelSpan) ToS() string     { return "#<OpenTelemetry::Trace::Span>" }
func (s *OTelSpan) Inspect() string { return s.ToS() }
func (s *OTelSpan) Truthy() bool    { return true }

// OTelSpanContext is the Ruby wrapper around an OpenTelemetry::Trace::SpanContext.
type OTelSpanContext struct{ sc rbtrace.SpanContext }

func (c *OTelSpanContext) ToS() string     { return "#<OpenTelemetry::Trace::SpanContext>" }
func (c *OTelSpanContext) Inspect() string { return c.ToS() }
func (c *OTelSpanContext) Truthy() bool    { return true }

// OTelStatus is the Ruby wrapper around an OpenTelemetry::Trace::Status.
type OTelStatus struct{ st rbtrace.Status }

func (s *OTelStatus) ToS() string     { return "#<OpenTelemetry::Trace::Status>" }
func (s *OTelStatus) Inspect() string { return s.ToS() }
func (s *OTelStatus) Truthy() bool    { return true }

// OTelExporter is the Ruby wrapper around the in-memory span exporter.
type OTelExporter struct {
	exp *sdktrace.InMemorySpanExporter
}

func (e *OTelExporter) ToS() string {
	return "#<OpenTelemetry::SDK::Trace::Export::InMemorySpanExporter>"
}
func (e *OTelExporter) Inspect() string { return e.ToS() }
func (e *OTelExporter) Truthy() bool    { return true }

// OTelProcessor is the Ruby wrapper around a span processor (simple or batch).
// It carries the class it reports so a Simple and a Batch processor answer their
// own class.
type OTelProcessor struct {
	sp  sdktrace.SpanProcessor
	cls *RClass
}

func (p *OTelProcessor) ToS() string     { return "#<" + p.cls.name + ">" }
func (p *OTelProcessor) Inspect() string { return p.ToS() }
func (p *OTelProcessor) Truthy() bool    { return true }

// OTelFinishedSpan is the Ruby wrapper around a finished span's read-only
// snapshot (OpenTelemetry::SDK::Trace::SpanData).
type OTelFinishedSpan struct{ fs sdktrace.FinishedSpan }

func (s *OTelFinishedSpan) ToS() string {
	return "#<OpenTelemetry::SDK::Trace::SpanData name=" + s.fs.Name() + ">"
}
func (s *OTelFinishedSpan) Inspect() string { return s.ToS() }
func (s *OTelFinishedSpan) Truthy() bool    { return true }

// otelStringArg coerces a Ruby String or Symbol argument to a Go string, raising
// TypeError otherwise (a span/tracer/attribute name must be a String or Symbol).
func otelStringArg(v object.Value, what string) string {
	switch s := v.(type) {
	case *object.String:
		return s.Str()
	case object.Symbol:
		return string(s)
	}
	raise("TypeError", "%s must be a String or Symbol, got %s", what, v.Inspect())
	panic("unreachable")
}

// otelAttrValue maps a Ruby attribute value onto the Go value the library's
// attribute model accepts. The OpenTelemetry attribute types are the scalar
// String/Integer/Float/Boolean and their homogeneous array forms; a Ruby Array
// is narrowed to the matching typed slice, and anything else is stringified,
// mirroring how opentelemetry-ruby coerces an attribute value.
func otelAttrValue(v object.Value) any {
	switch n := v.(type) {
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	case *object.Array:
		return otelSliceValue(n.Elems)
	}
	return v.ToS()
}

// otelSliceValue narrows a Ruby Array into the homogeneous typed slice the
// attribute model records natively ([]string/[]int64/[]float64/[]bool). A mixed
// or otherwise unsupported array falls back to a []string of stringified
// elements, matching opentelemetry-ruby's coercion of a non-homogeneous array.
func otelSliceValue(elems []object.Value) any {
	if len(elems) == 0 {
		return []string{}
	}
	switch elems[0].(type) {
	case object.Integer:
		out := make([]int64, len(elems))
		for i, e := range elems {
			n, ok := e.(object.Integer)
			if !ok {
				return otelStringSlice(elems)
			}
			out[i] = int64(n)
		}
		return out
	case object.Float:
		out := make([]float64, len(elems))
		for i, e := range elems {
			n, ok := e.(object.Float)
			if !ok {
				return otelStringSlice(elems)
			}
			out[i] = float64(n)
		}
		return out
	case object.Bool:
		out := make([]bool, len(elems))
		for i, e := range elems {
			n, ok := e.(object.Bool)
			if !ok {
				return otelStringSlice(elems)
			}
			out[i] = bool(n)
		}
		return out
	default:
		return otelStringSlice(elems)
	}
}

// otelStringSlice stringifies every element of a Ruby Array into a []string.
func otelStringSlice(elems []object.Value) []string {
	out := make([]string, len(elems))
	for i, e := range elems {
		if s, ok := e.(*object.String); ok {
			out[i] = s.Str()
			continue
		}
		out[i] = e.ToS()
	}
	return out
}

// otelAttrsFromHash converts a Ruby Hash of attributes into the library's
// name => value map, rendering each key as a String/Symbol name.
func otelAttrsFromHash(h *object.Hash) map[string]any {
	m := make(map[string]any, h.Len())
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m[otelStringArg(k, "attribute name")] = otelAttrValue(val)
	}
	return m
}

// otelValueToRuby maps a Go attribute value read back from the library into the
// rbgo object graph, the inverse of otelAttrValue. The library only ever hands
// back the scalar bool/int64/float64/string and their slice forms; a String
// (and, defensively, any other type) lands on the default and is stringified.
func otelValueToRuby(v any) object.Value {
	switch n := v.(type) {
	case bool:
		return object.Bool(n)
	case int64:
		return object.IntValue(n)
	case float64:
		return object.Float(n)
	case []bool:
		out := make([]object.Value, len(n))
		for i, e := range n {
			out[i] = object.Bool(e)
		}
		return object.NewArrayFromSlice(out)
	case []int64:
		out := make([]object.Value, len(n))
		for i, e := range n {
			out[i] = object.IntValue(e)
		}
		return object.NewArrayFromSlice(out)
	case []float64:
		out := make([]object.Value, len(n))
		for i, e := range n {
			out[i] = object.Float(e)
		}
		return object.NewArrayFromSlice(out)
	case []string:
		out := make([]object.Value, len(n))
		for i, e := range n {
			out[i] = object.NewString(e)
		}
		return object.NewArrayFromSlice(out)
	default:
		return object.NewString(fmt.Sprint(n))
	}
}

// otelAttrsToHash renders a library attribute map as a Ruby Hash keyed by
// String, in sorted key order so the Hash renders deterministically regardless
// of Go map iteration order.
func otelAttrsToHash(m map[string]any) object.Value {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := object.NewHash()
	for _, k := range keys {
		h.Set(object.NewString(k), otelValueToRuby(m[k]))
	}
	return h
}

// otelSpanKind maps a Ruby span-kind Symbol/String onto the library's SpanKind.
// An unknown name raises ArgumentError, matching opentelemetry-ruby's rejection
// of an unrecognised kind.
func otelSpanKind(v object.Value) rbtrace.SpanKind {
	switch otelStringArg(v, "span kind") {
	case "internal":
		return rbtrace.Internal
	case "server":
		return rbtrace.Server
	case "client":
		return rbtrace.Client
	case "producer":
		return rbtrace.Producer
	case "consumer":
		return rbtrace.Consumer
	}
	raise("ArgumentError", "unknown span kind %s", v.Inspect())
	panic("unreachable")
}

// otelKindToSym renders a library SpanKind back as the Ruby Symbol
// opentelemetry-ruby uses.
func otelKindToSym(k rbtrace.SpanKind) object.Value {
	switch k {
	case rbtrace.Server:
		return object.Symbol("server")
	case rbtrace.Client:
		return object.Symbol("client")
	case rbtrace.Producer:
		return object.Symbol("producer")
	case rbtrace.Consumer:
		return object.Symbol("consumer")
	default:
		return object.Symbol("internal")
	}
}

// otelStatusSym renders a Status code as the Ruby Symbol (:unset/:ok/:error).
func otelStatusSym(st rbtrace.Status) object.Value {
	switch st.Code {
	case rbtrace.Ok:
		return object.Symbol("ok")
	case rbtrace.Error:
		return object.Symbol("error")
	default:
		return object.Symbol("unset")
	}
}

// otelStatusArg resolves a status= argument into a library Status. It accepts an
// OpenTelemetry::Trace::Status object, or a shorthand Symbol/String (:ok /
// :error / :unset), raising for anything else.
func otelStatusArg(v object.Value) rbtrace.Status {
	if s, ok := v.(*OTelStatus); ok {
		return s.st
	}
	switch otelStringArg(v, "status") {
	case "ok":
		return rbtrace.StatusOK()
	case "error":
		return rbtrace.StatusError("")
	case "unset":
		return rbtrace.StatusUnset()
	}
	raise("ArgumentError", "unknown status %s", v.Inspect())
	panic("unreachable")
}

// otelSpanOpts parses the trailing keyword Hash of in_span/start_span into the
// library's span options: attributes: (a Hash) and kind: (a Symbol/String).
func otelSpanOpts(args []object.Value) []rbtrace.SpanOption {
	var opts []rbtrace.SpanOption
	h, ok := trailingHash(args)
	if !ok {
		return opts
	}
	if av, ok := h.Get(object.Symbol("attributes")); ok {
		attrs, isHash := av.(*object.Hash)
		if !isHash {
			raise("TypeError", "attributes: must be a Hash, got %s", av.Inspect())
		}
		opts = append(opts, rbtrace.WithAttributes(otelAttrsFromHash(attrs)))
	}
	if kv, ok := h.Get(object.Symbol("kind")); ok {
		opts = append(opts, rbtrace.WithKind(otelSpanKind(kv)))
	}
	return opts
}

// otelCurrent returns the current OpenTelemetry context, so a start_span is
// parented to any span made current by an enclosing in_span.
func otelCurrent() *rbcontext.Context { return rbcontext.Current() }

// otelInSpan drives OpenTelemetry::Trace::Tracer#in_span: it starts a span, makes
// it the current context, yields it to the Ruby block, and finishes it
// afterwards — even when the block raises. A raised Ruby exception is recorded on
// the span as an exception event, the status is set to error, and the exception
// is re-raised, mirroring how the Ruby block form records and re-raises. Non-
// exception unwinds (a block break/return) pass through untouched so control flow
// stays faithful; the span is still finished in every case.
func (vm *VM) otelInSpan(tr *rbtrace.Tracer, name string, opts []rbtrace.SpanOption, blk *Proc) object.Value {
	ctx, span := tr.StartSpan(rbcontext.Current(), name, opts...)
	tok := rbcontext.Attach(ctx)
	finish := func() {
		rbcontext.Detach(tok)
		span.Finish()
	}
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				span.RecordException(errors.New(re.Message), nil)
				span.SetStatus(rbtrace.StatusError(re.Message))
			}
			finish()
			panic(r)
		}
	}()
	result := vm.callBlock(blk, []object.Value{&OTelSpan{span: span}})
	finish()
	return result
}
