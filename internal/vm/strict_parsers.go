// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strconv"
	stdtime "time"

	date "github.com/go-ruby-date/date"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the *strict* class-method parsers of Time, Date and
// DateTime — the "require 'time'" / "require 'date'" companions of the lenient
// Time.parse / Date.parse. Unlike the heuristic parsers, each of these accepts
// ONLY its own wire format and raises on anything else: Time.iso8601 the
// ISO-8601 / XML-Schema instant, Time.rfc2822 the RFC 2822 mail date, and
// Time.httpdate the three date forms RFC 2616 permits (RFC 1123, the obsolete
// RFC 850 and asctime). The Date / DateTime siblings reuse the very same
// scanners, then rebuild the calendar value through the go-ruby-date
// constructors so the reform, JDN core and formatters stay authoritative.
//
// Each scanner is a fixed set of Go reference layouts tried in order; Go's
// time.Parse is itself strict for a given layout, so the whole grammar is
// expressed declaratively. A whole-second value is kept exact; a fractional
// second survives on Time (which carries nanoseconds) and is dropped on
// DateTime (whose constructor, like MRI's own DateTime, takes whole seconds).
//
// NOTE (deferred): Date.jisx0301 / DateTime.jisx0301 need the Japanese-era
// table (M/T/S/H/R base years), which go-ruby-date keeps unexported; they are
// left for a follow-up that exposes or mirrors that table. A zone-less
// Time.iso8601 / Time.rfc3339 value is resolved to UTC here (deterministic),
// where MRI resolves it to the machine's local zone — an intentional divergence
// on the one inherently non-deterministic case.

// Reference layouts. isoTimeLayouts require the "T"-separated wall clock (with
// an optional fractional part and either "Z" or a "±HH:MM" offset, or none);
// isoDateLayouts are the bare calendar day, dashed or compact. rfc2822Layouts
// is the RFC 2822 mail date; httpdateLayouts are the three RFC 2616 forms.
var (
	isoTimeLayouts  = []string{"2006-01-02T15:04:05.999999999Z07:00", "2006-01-02T15:04:05"}
	isoDateLayouts  = []string{"2006-01-02", "20060102"}
	rfc2822Layouts  = []string{"Mon, 02 Jan 2006 15:04:05 -0700"}
	httpdateLayouts = []string{
		"Mon, 02 Jan 2006 15:04:05 GMT",  // RFC 1123
		"Monday, 02-Jan-06 15:04:05 GMT", // obsolete RFC 850
		"Mon Jan _2 15:04:05 2006",       // obsolete asctime
	}
)

// parseStrictInstant tries each reference layout in turn, returning the first
// that parses cleanly. It is the single scanner behind every strict parser.
func parseStrictInstant(s string, layouts []string) (stdtime.Time, bool) {
	for _, layout := range layouts {
		if t, err := stdtime.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return stdtime.Time{}, false
}

// isoParseLayouts is the full ISO-8601 acceptance set for Date / DateTime, which
// (unlike Time) also admit a bare calendar day. A fresh slice is returned so the
// package-level layout slices are never mutated by append.
func isoParseLayouts() []string {
	out := make([]string, 0, len(isoTimeLayouts)+len(isoDateLayouts))
	out = append(out, isoTimeLayouts...)
	out = append(out, isoDateLayouts...)
	return out
}

// registerStrictTimeParsers installs Time.iso8601 / .xmlschema / .rfc2822 /
// .rfc822 / .httpdate — the strict class-method parsers returning a Time.
func (vm *VM) registerStrictTimeParsers() {
	sm := func(name string, fn NativeFn) {
		vm.cTime.smethods[name] = &Method{name: name, owner: vm.cTime, native: fn}
	}
	strictTime := func(layouts []string, msg string) NativeFn {
		return func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			s := strArg(args[0])
			t, ok := parseStrictInstant(s, layouts)
			if !ok {
				raise("ArgumentError", msg, strconv.Quote(s))
			}
			return &Time{t: t}
		}
	}
	iso := strictTime(isoTimeLayouts, "invalid xmlschema format: %s")
	sm("iso8601", iso)
	sm("xmlschema", iso)
	rfc := strictTime(rfc2822Layouts, "not RFC 2822 compliant date: %s")
	sm("rfc2822", rfc)
	sm("rfc822", rfc)
	// httpdate always denotes a GMT instant, so the value is re-homed onto UTC.
	sm("httpdate", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s := strArg(args[0])
		t, ok := parseStrictInstant(s, httpdateLayouts)
		if !ok {
			raise("ArgumentError", "not RFC 2616 compliant date: %s", strconv.Quote(s))
		}
		return &Time{t: t.UTC()}
	})
}

// registerStrictDateParsers installs the Date and DateTime strict parsers. A
// Date value keeps only the calendar day; a DateTime keeps the wall clock and
// offset. A malformed string raises Date::Error "invalid date", exactly as MRI.
func (vm *VM) registerStrictDateParsers() {
	sm := func(cls *RClass, name string, fn NativeFn) {
		cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
	}
	// dateOnly rebuilds a plain Date from the scanned calendar day.
	dateOnly := func(layouts []string) NativeFn {
		return func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			t, ok := parseStrictInstant(strArg(args[0]), layouts)
			if !ok {
				raise("Date::Error", "invalid date")
			}
			return payloadDate(date.NewDate(t.Year(), int(t.Month()), t.Day()))
		}
	}
	// dateTime rebuilds a DateTime from the scanned day, wall clock and offset.
	dateTime := func(layouts []string) NativeFn {
		return func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			t, ok := parseStrictInstant(strArg(args[0]), layouts)
			if !ok {
				raise("Date::Error", "invalid date")
			}
			_, off := t.Zone()
			return payloadDate(date.NewDateTime(t.Year(), int(t.Month()), t.Day(),
				t.Hour(), t.Minute(), t.Second(), off))
		}
	}
	// Format → layout set. rfc3339 requires the full wall clock (no bare day).
	specs := []struct {
		name    string
		layouts []string
	}{
		{"iso8601", isoParseLayouts()},
		{"xmlschema", isoParseLayouts()},
		{"rfc3339", isoTimeLayouts},
		{"rfc2822", rfc2822Layouts},
		{"rfc822", rfc2822Layouts},
		{"httpdate", httpdateLayouts},
	}
	for _, s := range specs {
		sm(vm.cDate, s.name, dateOnly(s.layouts))
		sm(vm.cDateTime, s.name, dateTime(s.layouts))
	}
}
