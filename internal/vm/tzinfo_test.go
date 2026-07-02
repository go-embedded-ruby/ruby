// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestTZInfoConstants covers the TZInfo module, its value classes and its error
// tree (require "tzinfo").
func TestTZInfoConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "tzinfo"; p TZInfo.is_a?(Module)`, "true\n"},
		{`p require "tzinfo"`, "true\n"},
		{`require "tzinfo"; p require "tzinfo"`, "false\n"},
		{`require "tzinfo"; p TZInfo::Timezone.is_a?(Class)`, "true\n"},
		{`require "tzinfo"; p TZInfo::Country.is_a?(Class)`, "true\n"},
		{`require "tzinfo"; p TZInfo::InvalidTimezoneIdentifier < StandardError`, "true\n"},
		{`require "tzinfo"; p TZInfo::InvalidCountryCode < StandardError`, "true\n"},
		{`require "tzinfo"; p TZInfo::AmbiguousTime < StandardError`, "true\n"},
		{`require "tzinfo"; p TZInfo::PeriodNotFound < StandardError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTZInfoTimezone covers TZInfo::Timezone.get and the instance surface.
func TestTZInfoTimezone(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").identifier`, "\"America/New_York\"\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").name`, "\"America/New_York\"\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").canonical_identifier`, "\"America/New_York\"\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("UTC").to_s.class`, "String\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").class.name`, "\"TZInfo::Timezone\"\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").now.class`, "Time\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").utc_offset.class`, "Integer\n"},
		// An offset-DST timestamp (1979-05-27 07:32 UTC) is EDT in New York.
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").period_for_utc(Time.at(296638320)).abbreviation`, "\"EDT\"\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").abbreviation(Time.at(296638320))`, "\"EDT\"\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").dst?(Time.at(296638320))`, "true\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").utc_to_local(Time.at(296638320)).class`, "Time\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").local_to_utc(Time.at(296638320)).class`, "Time\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").current_period.class.name`, "\"TZInfo::TimezonePeriod\"\n"},
		{`require "tzinfo"; p (TZInfo::Timezone.all_identifiers.length > 100)`, "true\n"},
		{`require "tzinfo"; p TZInfo::Timezone.all.first.class.name`, "\"TZInfo::Timezone\"\n"},
		{`require "tzinfo"; p TZInfo::Timezone.get("America/New_York").inspect.start_with?("#<TZInfo::Timezone")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTZInfoPeriodAndOffset covers the TimezonePeriod and TimezoneOffset readers.
func TestTZInfoPeriodAndOffset(t *testing.T) {
	pre := `require "tzinfo"; per = TZInfo::Timezone.get("America/New_York").period_for_utc(Time.at(296638320)); `
	cases := []struct{ src, want string }{
		{pre + `p per.abbreviation`, "\"EDT\"\n"},
		{pre + `p per.dst?`, "true\n"},
		{pre + `p per.base_utc_offset`, "-18000\n"},
		{pre + `p per.std_offset`, "3600\n"},
		{pre + `p per.utc_total_offset`, "-14400\n"},
		{pre + `p per.inspect.start_with?("#<TZInfo::TimezonePeriod")`, "true\n"},
		{pre + `o = per.offset; p o.class.name`, "\"TZInfo::TimezoneOffset\"\n"},
		{pre + `o = per.offset; p o.base_utc_offset`, "-18000\n"},
		{pre + `o = per.offset; p o.std_offset`, "3600\n"},
		{pre + `o = per.offset; p o.utc_total_offset`, "-14400\n"},
		{pre + `o = per.offset; p o.abbreviation`, "\"EDT\"\n"},
		{pre + `o = per.offset; p o.dst?`, "true\n"},
		{pre + `o = per.offset; p o.inspect.start_with?("#<TZInfo::TimezoneOffset")`, "true\n"},
		{pre + `p per.to_s.start_with?("#<TZInfo::TimezonePeriod")`, "true\n"},
		{pre + `o = per.offset; p o.to_s.start_with?("#<TZInfo::TimezoneOffset")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTZInfoCountry covers TZInfo::Country.get / .all_codes and the readers.
func TestTZInfoCountry(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "tzinfo"; p TZInfo::Country.get("US").code`, "\"US\"\n"},
		{`require "tzinfo"; p TZInfo::Country.get("US").name`, "\"United States\"\n"},
		{`require "tzinfo"; p TZInfo::Country.get("US").zone_identifiers.include?("America/New_York")`, "true\n"},
		{`require "tzinfo"; p (TZInfo::Country.all_codes.length > 100)`, "true\n"},
		{`require "tzinfo"; p TZInfo::Country.get("US").class.name`, "\"TZInfo::Country\"\n"},
		{`require "tzinfo"; p TZInfo::Country.get("US").inspect.start_with?("#<TZInfo::Country")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTZInfoErrors covers the error and arity paths.
func TestTZInfoErrors(t *testing.T) {
	// An unknown zone raises TZInfo::InvalidTimezoneIdentifier.
	got := eval(t, `require "tzinfo"
begin
  TZInfo::Timezone.get("No/Such/Zone")
rescue TZInfo::InvalidTimezoneIdentifier
  puts "badzone"
end`)
	if !strings.Contains(got, "badzone") {
		t.Errorf("bad zone: got %q", got)
	}
	// An unknown country raises TZInfo::InvalidCountryCode.
	got = eval(t, `require "tzinfo"
begin
  TZInfo::Country.get("ZZ")
rescue TZInfo::InvalidCountryCode
  puts "badcountry"
end`)
	if !strings.Contains(got, "badcountry") {
		t.Errorf("bad country: got %q", got)
	}
	// No-argument calls raise ArgumentError.
	for _, call := range []string{`TZInfo::Timezone.get`, `TZInfo::Country.get`} {
		src := `require "tzinfo"
begin
  ` + call + `
rescue ArgumentError
  puts "arity"
end`
		if got := eval(t, src); !strings.Contains(got, "arity") {
			t.Errorf("%s no-arg: got %q", call, got)
		}
	}
	// A conversion method called with no Time argument raises ArgumentError.
	got = eval(t, `require "tzinfo"
begin
  TZInfo::Timezone.get("UTC").period_for_utc
rescue ArgumentError
  puts "notime"
end`)
	if !strings.Contains(got, "notime") {
		t.Errorf("no-time arg: got %q", got)
	}
	// A non-Time argument raises TypeError.
	got = eval(t, `require "tzinfo"
begin
  TZInfo::Timezone.get("UTC").period_for_utc("nope")
rescue TypeError
  puts "typeerr"
end`)
	if !strings.Contains(got, "typeerr") {
		t.Errorf("non-time arg: got %q", got)
	}
	// A local time falling in a spring-forward gap (2021-03-14 02:30 in New York,
	// which does not exist) raises TZInfo::PeriodNotFound from local_to_utc.
	got = eval(t, `require "tzinfo"
begin
  TZInfo::Timezone.get("America/New_York").local_to_utc(Time.at(1615689000))
rescue TZInfo::PeriodNotFound
  puts "gap"
end`)
	if !strings.Contains(got, "gap") {
		t.Errorf("dst gap: got %q", got)
	}
}
