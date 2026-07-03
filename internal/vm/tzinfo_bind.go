// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	tzinfo "github.com/go-ruby-tzinfo/tzinfo"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's object graph (object.Value and
// the VM's Time shell) and the github.com/go-ruby-tzinfo/tzinfo library. The
// zone resolution and conversion live in that library; rbgo only turns Ruby
// arguments into Go time.Time and library results back into Ruby values.

// tzGet resolves a zone by identifier, raising TZInfo::InvalidTimezoneIdentifier
// when the id is unknown (matching TZInfo::Timezone.get).
func tzGet(id string) object.Value {
	tz, err := tzinfo.Get(id)
	if err != nil {
		raise("TZInfo::InvalidTimezoneIdentifier", "%s", err.Error())
	}
	return object.Wrap(&Timezone{tz: tz})
}

// tzCheck raises a Ruby RuntimeError carrying err's message when the embedded
// database read fails. The IANA data is embedded, so this never fires in
// practice; it exists so a corrupt build surfaces as a Ruby exception rather than
// a nil dereference.
func tzCheck(err error) {
	if err != nil {
		raise("RuntimeError", "%s", err.Error())
	}
}

// tzIdentifiers returns TZInfo::Timezone.all_identifiers as a Ruby Array of
// Strings.
func tzIdentifiers() object.Value {
	ids, err := tzinfo.AllIdentifiers()
	tzCheck(err)
	return strSliceToArray(ids)
}

// tzAll returns TZInfo::Timezone.all as a Ruby Array of Timezone objects.
func tzAll() object.Value {
	all, err := tzinfo.All()
	tzCheck(err)
	arr := object.NewArrayFromSlice(make([]object.Value, len(all)))
	for i, tz := range all {
		arr.Elems[i] = object.Wrap(&Timezone{tz: tz})
	}
	return object.Wrap(arr)
}

// tzGetCountry resolves a country by ISO code, raising TZInfo::InvalidCountryCode
// when the code is unknown (matching TZInfo::Country.get).
func tzGetCountry(code string) object.Value {
	c, err := tzinfo.GetCountry(code)
	if err != nil {
		raise("TZInfo::InvalidCountryCode", "%s", err.Error())
	}
	return object.Wrap(&Country{c: c})
}

// tzCountryCodes returns TZInfo::Country.all_codes as a Ruby Array of Strings.
func tzCountryCodes() object.Value { return strSliceToArray(tzinfo.AllCountryCodes()) }

// rubyTimeArg reads a required Ruby Time argument and returns it as a Go
// time.Time (UTC), raising ArgumentError when it is missing and TypeError when
// it is not a Time.
func rubyTimeArg(args []object.Value) stdtime.Time {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return stdtime.Unix(timeArg(args[0]).t.ToUnix(), 0).UTC()
}

// goTimeToRuby wraps a Go time.Time as an rbgo Time (whole-second resolution,
// matching the rest of the Time surface).
//
// The instant is carried in the Go time.Time's OWN location, not forced to UTC:
// MRI renders strftime / to_s in the Time's local zone (the wall clock of that
// zone), so a Chronic parse of "2016-05-27 12:00:00" or a TZInfo local
// conversion must keep its wall clock and offset. We round-trip through
// RFC3339 (which encodes the numeric offset) so gotime's Parse rebuilds a
// composite whose Format renders that same wall clock; getutc / UTC re-shifts
// to UTC as MRI does. FromUnix would discard the offset and render UTC-shifted.
func goTimeToRuby(t stdtime.Time) object.Value {
	// Truncate to whole seconds (the rest of the Time surface is second
	// resolution) while preserving the location, then encode the offset via
	// RFC3339 for zonedFromRFC3339 to reconstruct.
	t = t.Truncate(stdtime.Second)
	return object.Wrap(&Time{t: zonedFromRFC3339(t.Format(stdtime.RFC3339), t.Unix())})
}

// zonedFromRFC3339 rebuilds a gotime composite from an RFC3339 string, keeping
// the numeric offset it encodes so Format renders the wall clock of that zone.
// The RFC3339 rendering of any valid time.Time parses cleanly; the error path
// falls back to the UTC instant so a malformed input can never panic.
func zonedFromRFC3339(rfc3339 string, unix int64) gotime.Interface {
	r := gotime.Parse(stdtime.RFC3339, rfc3339)
	if r.HasError() {
		return gotime.FromUnix(unix)
	}
	return r.Payload().(gotime.Interface)
}
