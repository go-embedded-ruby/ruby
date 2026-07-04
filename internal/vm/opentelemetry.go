// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	sdktrace "github.com/go-ruby-opentelemetry/opentelemetry/sdk/trace"
	rbtrace "github.com/go-ruby-opentelemetry/opentelemetry/trace"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerOpenTelemetry installs the OpenTelemetry module (require
// "opentelemetry"): the global tracer-provider accessors (OpenTelemetry
// .tracer_provider / .tracer_provider= / .tracer), the API tracing surface
// (OpenTelemetry::Trace's Tracer, Span, SpanContext and Status) and the SDK
// (OpenTelemetry::SDK::Trace's configurable TracerProvider, the simple/batch span
// processors and the in-memory span exporter reached through
// OpenTelemetry::SDK::Trace::Export). The tracing model itself — span recording,
// sampling, W3C context and the exporter seam — lives in the
// github.com/go-ruby-opentelemetry/opentelemetry library, a Ruby-faithful facade
// over the OpenTelemetry Go SDK; this file is the thin shell mapping rbgo's
// value model onto that library and back (see opentelemetry_bind.go for the value
// conversions and the in_span block driver). It runs eagerly at boot.
func (vm *VM) registerOpenTelemetry() {
	mod := newClass("OpenTelemetry", nil)
	mod.isModule = true
	vm.consts["OpenTelemetry"] = mod

	traceMod := newClass("OpenTelemetry::Trace", nil)
	traceMod.isModule = true
	mod.consts["Trace"] = traceMod
	vm.consts["OpenTelemetry::Trace"] = traceMod

	apiProvider := vm.otelDefineClass(traceMod, "OpenTelemetry::Trace", "TracerProvider", vm.cObject)
	tracerCls := vm.otelDefineClass(traceMod, "OpenTelemetry::Trace", "Tracer", vm.cObject)
	spanCls := vm.otelDefineClass(traceMod, "OpenTelemetry::Trace", "Span", vm.cObject)
	spanCtxCls := vm.otelDefineClass(traceMod, "OpenTelemetry::Trace", "SpanContext", vm.cObject)
	statusCls := vm.otelDefineClass(traceMod, "OpenTelemetry::Trace", "Status", vm.cObject)

	// The process-wide provider defaults to the API's non-recording provider,
	// exactly as OpenTelemetry.tracer_provider does before the SDK is installed.
	vm.otelProvider = &OTelTracerProvider{tp: rbtrace.NoopTracerProvider(), cls: apiProvider}

	vm.registerOpenTelemetryModule(mod)
	vm.registerOpenTelemetryProvider(apiProvider)
	vm.registerOpenTelemetryTracer(tracerCls)
	vm.registerOpenTelemetrySpan(spanCls)
	vm.registerOpenTelemetrySpanContext(spanCtxCls)
	vm.registerOpenTelemetryStatus(statusCls)
	vm.registerOpenTelemetrySDK(mod, apiProvider)
}

// otelDefineClass defines a class under a module, registering it both scoped and
// flat in vm.consts so raise/const lookup find it by qualified name.
func (vm *VM) otelDefineClass(parent *RClass, prefix, name string, super *RClass) *RClass {
	full := prefix + "::" + name
	cls := newClass(full, super)
	parent.consts[name] = cls
	vm.consts[full] = cls
	return cls
}

// otelSMethod installs a class ("singleton") method on a class.
func otelSMethod(cls *RClass, name string, fn NativeFn) {
	cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
}

// registerOpenTelemetryModule installs the OpenTelemetry.* global accessors: the
// tracer provider (get/set) and the tracer convenience that fetches a tracer from
// the current global provider.
func (vm *VM) registerOpenTelemetryModule(mod *RClass) {
	otelSMethod(mod, "tracer_provider", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.otelProvider
	})
	otelSMethod(mod, "tracer_provider=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p, ok := args[0].(*OTelTracerProvider)
		if !ok {
			raise("TypeError", "tracer_provider= expects an OpenTelemetry tracer provider, got %s", args[0].Inspect())
		}
		vm.otelProvider = p
		return args[0]
	})
	otelSMethod(mod, "tracer", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return otelTracerFor(vm.otelProvider, args)
	})
}

// otelTracerFor fetches a named, optionally-versioned tracer from a provider,
// shared by OpenTelemetry.tracer and TracerProvider#tracer.
func otelTracerFor(p *OTelTracerProvider, args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	name := otelStringArg(args[0], "tracer name")
	version := ""
	if len(args) >= 2 {
		version = otelStringArg(args[1], "tracer version")
	}
	return &OTelTracer{tr: p.tp.Tracer(name, version)}
}

// registerOpenTelemetryProvider installs the OpenTelemetry::Trace::TracerProvider
// instance surface shared by the API provider and the SDK provider: #tracer and
// #add_span_processor (which only the SDK provider accepts).
func (vm *VM) registerOpenTelemetryProvider(cls *RClass) {
	cls.define("tracer", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return otelTracerFor(v.(*OTelTracerProvider), args)
	})
	cls.define("add_span_processor", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		p := v.(*OTelTracerProvider)
		if p.sdk == nil {
			raise("NoMethodError", "undefined method `add_span_processor' for a non-SDK tracer provider")
		}
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		sp, ok := args[0].(*OTelProcessor)
		if !ok {
			raise("TypeError", "add_span_processor expects a span processor, got %s", args[0].Inspect())
		}
		p.sdk.AddSpanProcessor(sp.sp)
		return object.NilV
	})
}

// registerOpenTelemetryTracer installs OpenTelemetry::Trace::Tracer#in_span (the
// block form that finishes the span even on a raise) and #start_span (the manual
// form the caller finishes).
func (vm *VM) registerOpenTelemetryTracer(cls *RClass) {
	cls.define("in_span", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		name := otelStringArg(args[0], "span name")
		return vm.otelInSpan(v.(*OTelTracer).tr, name, otelSpanOpts(args), blk)
	})
	cls.define("start_span", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		name := otelStringArg(args[0], "span name")
		_, span := v.(*OTelTracer).tr.StartSpan(otelCurrent(), name, otelSpanOpts(args)...)
		return &OTelSpan{span: span}
	})
}

// registerOpenTelemetrySpan installs the OpenTelemetry::Trace::Span enrich/finish
// surface: set_attribute, add_attributes, add_event, record_exception, status=,
// name=, finish, context and recording?.
func (vm *VM) registerOpenTelemetrySpan(cls *RClass) {
	self := func(v object.Value) rbtrace.Span { return v.(*OTelSpan).span }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("set_attribute", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).SetAttribute(otelStringArg(args[0], "attribute name"), otelAttrValue(args[1]))
		return v
	})
	d("add_attributes", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		h, ok := args[0].(*object.Hash)
		if !ok {
			raise("TypeError", "add_attributes expects a Hash, got %s", args[0].Inspect())
		}
		self(v).SetAttributes(otelAttrsFromHash(h))
		return v
	})
	d("add_event", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		name := otelStringArg(args[0], "event name")
		var attrs map[string]any
		if h, ok := trailingHash(args[1:]); ok {
			if av, ok := h.Get(object.Symbol("attributes")); ok {
				ah, isHash := av.(*object.Hash)
				if !isHash {
					raise("TypeError", "attributes: must be a Hash, got %s", av.Inspect())
				}
				attrs = otelAttrsFromHash(ah)
			}
		}
		self(v).AddEvent(name, attrs)
		return v
	})
	d("record_exception", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		self(v).RecordException(vm.otelExceptionError(args[0]), nil)
		return v
	})
	d("status=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetStatus(otelStatusArg(args[0]))
		return args[0]
	})
	d("name=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetName(otelStringArg(args[0], "span name"))
		return args[0]
	})
	finish := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).Finish()
		return v
	}
	d("finish", finish)
	d("end", finish)
	d("context", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &OTelSpanContext{sc: self(v).Context()}
	})
	d("recording?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Recording())
	})
}

// registerOpenTelemetrySpanContext installs OpenTelemetry::Trace::SpanContext's
// read-only identity surface: hex_trace_id, hex_span_id and valid?.
func (vm *VM) registerOpenTelemetrySpanContext(cls *RClass) {
	self := func(v object.Value) rbtrace.SpanContext { return v.(*OTelSpanContext).sc }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("hex_trace_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).HexTraceID())
	})
	d("hex_span_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).HexSpanID())
	})
	d("valid?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Valid())
	})
	d("remote?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Remote())
	})
}

// registerOpenTelemetryStatus installs OpenTelemetry::Trace::Status: the
// Status.ok/.error/.unset constructors and the #code/#description accessors.
func (vm *VM) registerOpenTelemetryStatus(cls *RClass) {
	otelSMethod(cls, "ok", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &OTelStatus{st: rbtrace.StatusOK()}
	})
	otelSMethod(cls, "unset", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &OTelStatus{st: rbtrace.StatusUnset()}
	})
	otelSMethod(cls, "error", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		desc := ""
		if len(args) > 0 {
			desc = otelStringArg(args[0], "status description")
		}
		return &OTelStatus{st: rbtrace.StatusError(desc)}
	})

	self := func(v object.Value) rbtrace.Status { return v.(*OTelStatus).st }
	cls.define("code", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return otelStatusSym(self(v))
	})
	cls.define("description", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Description)
	})
}

// registerOpenTelemetrySDK installs OpenTelemetry::SDK::Trace: the configurable
// TracerProvider, the simple/batch span processors and the in-memory span
// exporter under OpenTelemetry::SDK::Trace::Export.
func (vm *VM) registerOpenTelemetrySDK(otelMod, apiProvider *RClass) {
	sdkMod := newClass("OpenTelemetry::SDK", nil)
	sdkMod.isModule = true
	otelMod.consts["SDK"] = sdkMod
	vm.consts["OpenTelemetry::SDK"] = sdkMod

	sdkTraceMod := newClass("OpenTelemetry::SDK::Trace", nil)
	sdkTraceMod.isModule = true
	sdkMod.consts["Trace"] = sdkTraceMod
	vm.consts["OpenTelemetry::SDK::Trace"] = sdkTraceMod

	// The SDK provider subclasses the API provider so it inherits #tracer and
	// #add_span_processor, mirroring
	// OpenTelemetry::SDK::Trace::TracerProvider < OpenTelemetry::Trace::TracerProvider.
	sdkProvider := vm.otelDefineClass(sdkTraceMod, "OpenTelemetry::SDK::Trace", "TracerProvider", apiProvider)
	spanData := vm.otelDefineClass(sdkTraceMod, "OpenTelemetry::SDK::Trace", "SpanData", vm.cObject)

	exportMod := newClass("OpenTelemetry::SDK::Trace::Export", nil)
	exportMod.isModule = true
	sdkTraceMod.consts["Export"] = exportMod
	vm.consts["OpenTelemetry::SDK::Trace::Export"] = exportMod

	inMemory := vm.otelDefineClass(exportMod, "OpenTelemetry::SDK::Trace::Export", "InMemorySpanExporter", vm.cObject)
	simpleProc := vm.otelDefineClass(exportMod, "OpenTelemetry::SDK::Trace::Export", "SimpleSpanProcessor", vm.cObject)
	batchProc := vm.otelDefineClass(exportMod, "OpenTelemetry::SDK::Trace::Export", "BatchSpanProcessor", vm.cObject)

	// SDK::Trace::TracerProvider.new builds a recording provider (AlwaysOn); span
	// processors are attached with #add_span_processor.
	otelSMethod(sdkProvider, "new", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysOn()))
		return &OTelTracerProvider{sdk: tp, tp: tp, cls: sdkProvider}
	})

	otelSMethod(inMemory, "new", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &OTelExporter{exp: sdktrace.NewInMemorySpanExporter()}
	})
	inMemory.define("finished_spans", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		spans := v.(*OTelExporter).exp.FinishedSpans()
		out := make([]object.Value, len(spans))
		for i, s := range spans {
			out[i] = &OTelFinishedSpan{fs: s}
		}
		return object.NewArrayFromSlice(out)
	})
	inMemory.define("reset", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		v.(*OTelExporter).exp.Reset()
		return object.NilV
	})

	newProcessor := func(cls *RClass, make func(sdktrace.SpanExporter) sdktrace.SpanProcessor) {
		otelSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			exp, ok := args[0].(*OTelExporter)
			if !ok {
				raise("TypeError", "a span processor expects a span exporter, got %s", args[0].Inspect())
			}
			return &OTelProcessor{sp: make(exp.exp), cls: cls}
		})
	}
	newProcessor(simpleProc, sdktrace.NewSimpleSpanProcessor)
	newProcessor(batchProc, sdktrace.NewBatchSpanProcessor)

	vm.registerOpenTelemetrySpanData(spanData)
}

// registerOpenTelemetrySpanData installs OpenTelemetry::SDK::Trace::SpanData — the
// read-only finished-span snapshot an exporter surfaces: name, kind, attributes,
// events, status, hex_trace_id and hex_span_id.
func (vm *VM) registerOpenTelemetrySpanData(cls *RClass) {
	self := func(v object.Value) sdktrace.FinishedSpan { return v.(*OTelFinishedSpan).fs }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name())
	})
	d("kind", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return otelKindToSym(self(v).Kind())
	})
	d("attributes", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return otelAttrsToHash(self(v).Attributes())
	})
	d("events", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		evs := self(v).Events()
		out := make([]object.Value, len(evs))
		for i, ev := range evs {
			h := object.NewHash()
			h.Set(object.NewString("name"), object.NewString(ev.Name))
			h.Set(object.NewString("attributes"), otelAttrsToHash(ev.Attributes))
			out[i] = h
		}
		return object.NewArrayFromSlice(out)
	})
	d("status", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &OTelStatus{st: self(v).Status()}
	})
	d("hex_trace_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).HexTraceID())
	})
	d("hex_span_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).SpanContext().HexSpanID())
	})
}

// otelExceptionError builds a Go error carrying the message of a Ruby exception
// object (or the stringification of any other value), so Span#record_exception
// records it as an exception event through the backing SDK.
func (vm *VM) otelExceptionError(v object.Value) error {
	return &otelRubyError{msg: vm.exceptionMessageText(v)}
}

// otelRubyError adapts a Ruby exception's message to the Go error the library's
// RecordException consumes.
type otelRubyError struct{ msg string }

func (e *otelRubyError) Error() string { return e.msg }
