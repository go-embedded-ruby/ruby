package vm

import (
	"math/big"
	"os"
	"regexp"
	"strconv"
	"strings"
	stdtime "time"

	goresult "github.com/go-composites/result/src"
	gotime "github.com/go-composites/time/src"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// nowUnix is the seam for Time.now's only source of non-determinism — Go's wall
// clock — so tests can pin it. The VM's controllable clock (vm.clock, which
// Timecop drives) reads through this, and Time.now reads vm.nowInstant().
var nowUnix = func() int64 { return stdtime.Now().Unix() }

// Time is the Ruby Time wrapper around Go's time.Time. Backing Time with the
// stdlib instant gives nanosecond sub-second precision, fixed-offset zones
// (time.FixedZone) and the calendar arithmetic MRI's Time needs — a UTC Time
// carries time.UTC as its location, a zoned Time a fixed offset, and a local
// Time time.Local. Timecop still drives Time.now through vm.clock (nowInstant),
// so the clock seam is preserved.
type Time struct {
	t stdtime.Time
	// zoneObj is the Ruby timezone object a Time.new(..., zone) / Time.now(in: zone)
	// was built with, or nil for a plain offset/UTC/local Time. When set, #zone
	// returns this object rather than the fixed-zone name.
	zoneObj object.Value
	// frac carries the sub-nanosecond part of the sub-second, in seconds and in
	// the range [0, 1e-9), that Go's nanosecond-resolution instant cannot hold —
	// nil when the instant lands exactly on a nanosecond. MRI keeps a Time's
	// sub-second as an exact Rational, so #subsec / #to_r (and the arithmetic that
	// feeds them) must preserve digits past the ninth; the nanosecond-granular
	// fields (#nsec, #usec, rendering) read only the whole-nanosecond part and are
	// unaffected.
	frac *big.Rat
}

// subsecRat returns the Time's exact sub-second as a Rational number of seconds in
// [0, 1): the whole-nanosecond part plus any sub-nanosecond frac.
func (t *Time) subsecRat() *big.Rat {
	r := big.NewRat(int64(t.t.Nanosecond()), 1e9)
	if t.frac != nil {
		r.Add(r, t.frac)
	}
	return r
}

// newTimeExact builds a Time at an exact number of seconds since the epoch,
// flooring to the nanosecond for the backing instant and keeping any
// sub-nanosecond remainder in frac, in the given location with the given zone.
func newTimeExact(totalSec *big.Rat, loc *stdtime.Location, zoneObj object.Value) *Time {
	nsRat := new(big.Rat).Mul(totalSec, big.NewRat(1e9, 1))
	fl := new(big.Int).Div(nsRat.Num(), nsRat.Denom()) // floor toward -inf
	var frac *big.Rat
	if rem := new(big.Rat).Sub(nsRat, new(big.Rat).SetInt(fl)); rem.Sign() != 0 {
		frac = rem.Quo(rem, big.NewRat(1e9, 1)) // nanoseconds → seconds
	}
	sec := new(big.Int).Div(fl, big.NewInt(1e9))
	ns := new(big.Int).Sub(fl, new(big.Int).Mul(sec, big.NewInt(1e9)))
	return &Time{t: stdtime.Unix(sec.Int64(), ns.Int64()).In(loc), zoneObj: zoneObj, frac: frac}
}

// localLoc resolves the machine's current local timezone, honouring a TZ set at
// runtime — a spec's with_timezone helper assigns ENV['TZ'], which rbgo writes
// through to the process environment. An IANA name is loaded from Go's tzdata; an
// empty TZ keeps the process zone; and an unloadable name falls back to UTC,
// matching MRI's treatment of an unrecognised TZ as UTC.
func localLoc() *stdtime.Location {
	tz := os.Getenv("TZ")
	if tz == "" {
		return stdtime.Local
	}
	if loc, err := stdtime.LoadLocation(tz); err == nil {
		return loc
	}
	return stdtime.UTC
}

// unixTime builds a whole-second UTC Ruby Time from a Unix timestamp — the
// constructor the non-Time bindings (file mtimes, DB/serialiser instants, …)
// reach for when they have only a Unix second count.
func unixTime(sec int64) *Time { return &Time{t: stdtime.Unix(sec, 0).UTC()} }

// offsetString renders a Time's UTC offset as "+0000" (MRI %z), used by the
// to_s / inspect representation. A UTC-location Time reports "+0000".
func (t *Time) offsetString() string {
	_, off := t.t.Zone()
	return signedOffset(off, "")
}

// fracString renders the sub-second part as ".NNN…" with trailing zeros trimmed
// (MRI 4.0 inspect), or "" when the instant is on a whole second.
func (t *Time) fracString() string {
	ns := t.t.Nanosecond()
	if ns == 0 {
		return ""
	}
	s := strings.TrimRight(pad(int64(ns), 9), "0")
	return "." + s
}

// subsecValue renders Time#subsec: the exact fraction of a second as a Rational,
// or the Integer 0 on a whole second (MRI keeps 0 an Integer, not 0/1).
func (t *Time) subsecValue() object.Value {
	if t.t.Nanosecond() == 0 && t.frac == nil {
		return object.IntValue(0)
	}
	return &object.Rational{R: t.subsecRat()}
}

// zoneValue renders Time#zone: the Ruby timezone object when the Time carries
// one, else the fixed/named zone abbreviation, or nil when the zone is unnamed.
func (t *Time) zoneValue() object.Value {
	if z := t.zoneObj; z != nil {
		return z
	}
	name, _ := t.t.Zone()
	if name == "" {
		return object.NilV
	}
	return object.NewString(name)
}

// timeDeconstructKeys is the field set Time#deconstruct_keys(nil) returns, in
// MRI's order.
var timeDeconstructKeys = []string{
	"year", "month", "day", "yday", "wday", "hour", "min", "sec", "subsec", "dst", "zone",
}

// fieldValue resolves one Time#deconstruct_keys field name to its value, with ok
// false for a name that is not a field.
func (t *Time) fieldValue(key string) (object.Value, bool) {
	tm := t.t
	switch key {
	case "year":
		return object.IntValue(int64(tm.Year())), true
	case "month":
		return object.IntValue(int64(tm.Month())), true
	case "day":
		return object.IntValue(int64(tm.Day())), true
	case "yday":
		return object.IntValue(int64(tm.YearDay())), true
	case "wday":
		return object.IntValue(int64(tm.Weekday())), true
	case "hour":
		return object.IntValue(int64(tm.Hour())), true
	case "min":
		return object.IntValue(int64(tm.Minute())), true
	case "sec":
		return object.IntValue(int64(tm.Second())), true
	case "subsec":
		return t.subsecValue(), true
	case "dst":
		return object.Bool(tm.IsDST()), true
	case "zone":
		return t.zoneValue(), true
	}
	return nil, false
}

// repr renders MRI's "2026-06-21 12:34:56 +0000"; when withFrac the sub-second
// fraction is included (inspect, not to_s). A UTC instant reports the zone as
// "UTC" rather than the "+0000" offset, matching MRI's to_s / inspect.
func (t *Time) repr(withFrac bool) string {
	base := t.t.Format("2006-01-02 15:04:05")
	frac := ""
	if withFrac {
		frac = t.fracString()
	}
	zone := t.offsetString()
	if t.t.Location() == stdtime.UTC {
		zone = "UTC"
	}
	return base + frac + " " + zone
}

func (t *Time) ToS() string     { return t.repr(false) }
func (t *Time) Inspect() string { return t.repr(true) }
func (t *Time) Truthy() bool    { return true }

// iso8601Str renders MRI Time#iso8601 / #xmlschema (require "time"):
// "%Y-%m-%dT%H:%M:%S", an optional n-digit ".NNN" fractional-second part, then
// the zone — "Z" for a UTC instant, "+HH:MM" (%:z) otherwise.
func (t *Time) iso8601Str(fracN int) string {
	out := strftime(t, "%Y-%m-%dT%H:%M:%S")
	if fracN > 0 {
		out += "." + fracDigits(t.t.Nanosecond(), fracN)
	}
	if t.t.Location() == stdtime.UTC {
		return out + "Z"
	}
	_, off := t.t.Zone()
	return out + signedOffset(off, ":")
}

// rfc2822Str renders MRI Time#rfc2822 / #rfc822 (require "time"):
// "%a, %d %b %Y %H:%M:%S %z", where a UTC instant reports RFC 2822's
// "unknown local zone" marker "-0000" rather than "+0000".
func (t *Time) rfc2822Str() string {
	out := strftime(t, "%a, %d %b %Y %H:%M:%S ")
	if t.t.Location() == stdtime.UTC {
		return out + "-0000"
	}
	_, off := t.t.Zone()
	return out + signedOffset(off, "")
}

// httpdateStr renders MRI Time#httpdate (require "time"): the instant converted
// to GMT as "%a, %d %b %Y %H:%M:%S GMT".
func (t *Time) httpdateStr() string {
	return strftime(&Time{t: t.t.UTC()}, "%a, %d %b %Y %H:%M:%S") + " GMT"
}

// timeArg asserts an argument is a Time, raising TypeError otherwise.
func timeArg(v object.Value) *Time {
	t, ok := v.(*Time)
	if !ok {
		raise("TypeError", "value must be a Time")
	}
	return t
}

// payloadTime unwraps a go-composites Result whose payload is a parsed instant,
// re-homing it onto a Go time.Time (via RFC3339 so the numeric offset survives),
// and raising ArgumentError when the parse failed.
func payloadTime(r goresult.Interface) *Time {
	if r.HasError() {
		raise("ArgumentError", "%s", r.Error().Message())
	}
	gi := r.Payload().(gotime.Interface)
	// gi.Format(RFC3339) always renders valid RFC3339, so the re-parse never errors.
	st, _ := stdtime.Parse(stdtime.RFC3339, gi.Format(stdtime.RFC3339))
	return &Time{t: st}
}

// registerTime installs the Time class, its class constructors and instance
// methods.
func (vm *VM) registerTime() {
	vm.cTime = newClass("Time", vm.cObject)
	vm.consts["Time"] = vm.cTime

	sm := func(name string, fn NativeFn) {
		vm.cTime.smethods[name] = &Method{name: name, owner: vm.cTime, native: fn}
	}
	// Time.at(time, subsec = nil, unit = :microsecond, in: nil).
	sm("at", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.timeAt(args)
	})
	// Time.now → the VM's controllable clock (Timecop drives it), optionally in a
	// given zone via the in: keyword.
	sm("now", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.timeNow(args)
	})
	// Time.new(...) → now with no args, else year[,mon,day,hour,min,sec,zone];
	// the zone may be the 7th positional argument or the in: keyword (MRI 4.0).
	sm("new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.timeNew(args)
	})
	// Time.utc / Time.gm(year[,mon,day,hour,min,sec,usec]) → a UTC instant. gm is
	// the very same Method object as utc so Time.method(:gm) == Time.method(:utc).
	utcM := &Method{name: "utc", owner: vm.cTime, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.timeFromCalendar(args, stdtime.UTC)
	}}
	vm.cTime.smethods["utc"] = utcM
	vm.cTime.smethods["gm"] = utcM
	// Time.local / Time.mktime(...) → the same, in the local zone.
	localM := &Method{name: "local", owner: vm.cTime, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.timeFromCalendar(args, localLoc())
	}}
	vm.cTime.smethods["local"] = localM
	vm.cTime.smethods["mktime"] = localM
	// Time.parse / Time.strptime keep the go-composites lenient parsers.
	sm("parse", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return payloadTime(gotime.ParseAny(strArg(args[0])))
	})
	sm("strptime", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return payloadTime(gotime.Parse(rubyLayout(strArg(args[1])), strArg(args[0])))
	})
	// The strict "require 'time'" parsers (iso8601 / xmlschema / rfc2822 / rfc822
	// / httpdate), each accepting only its own wire format.
	vm.registerStrictTimeParsers()

	d := func(name string, fn NativeFn) { vm.cTime.define(name, fn) }
	self := func(v object.Value) *Time { return v.(*Time) }

	// Time#_dump / Time._load are the private Marshal hooks: _dump packs the 8-byte
	// form, _load rebuilds a Time from it.
	d("_dump", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewStringBytesEnc(timeDumpBytes(self(v)), "ASCII-8BIT")
	})
	vm.setInstanceVisibility(vm.cTime, "_dump", visPrivate)
	vm.cTime.smethods["_load"] = &Method{name: "_load", owner: vm.cTime, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s, ok := args[0].(*object.String)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into String", vm.classOf(args[0]).name)
		}
		return marshalLoadTime([]byte(s.Str()))
	}}
	vm.setClassMethodVisibility(vm.cTime, "_load", visPrivate)

	d("to_i", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).t.Unix())
	})
	d("to_f", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(float64(self(v).t.UnixNano()) / 1e9)
	})
	d("to_r", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		tt := self(v)
		r := new(big.Rat).SetInt64(tt.t.Unix())
		return &object.Rational{R: r.Add(r, tt.subsecRat())}
	})

	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr(false))
	})
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr(true))
	})

	d("strftime", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(strftime(self(v), strArg(args[0])))
	})
	d("asctime", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(strftime(self(v), "%a %b %e %H:%M:%S %Y"))
	})

	// require "time" formatters. iso8601 / xmlschema take an optional
	// fraction_digits count (default 0); rfc2822 / rfc822 / httpdate take none.
	iso := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).iso8601Str(int(intArgOr(args, 0))))
	}
	d("iso8601", iso)
	rfc2822 := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).rfc2822Str())
	}
	d("rfc2822", rfc2822)
	d("rfc822", rfc2822)
	d("httpdate", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).httpdateStr())
	})

	// Field accessors.
	d("year", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Year()))
	})
	monthFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Month()))
	}
	d("month", monthFn)
	dayFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Day()))
	}
	d("day", dayFn)
	d("hour", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Hour()))
	})
	d("min", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Minute()))
	})
	d("sec", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Second()))
	})
	d("usec", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Nanosecond() / 1000))
	})
	d("nsec", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Nanosecond()))
	})
	d("subsec", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).subsecValue()
	})
	d("yday", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.YearDay()))
	})
	d("wday", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Weekday()))
	})

	// Weekday predicates.
	weekdayPred := func(want stdtime.Weekday) NativeFn {
		return func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Bool(self(v).t.Weekday() == want)
		}
	}
	d("sunday?", weekdayPred(stdtime.Sunday))
	d("monday?", weekdayPred(stdtime.Monday))
	d("tuesday?", weekdayPred(stdtime.Tuesday))
	d("wednesday?", weekdayPred(stdtime.Wednesday))
	d("thursday?", weekdayPred(stdtime.Thursday))
	d("friday?", weekdayPred(stdtime.Friday))
	d("saturday?", weekdayPred(stdtime.Saturday))

	// Zone queries.
	d("zone", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).zoneValue()
	})
	offsetFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_, off := self(v).t.Zone()
		return object.IntValue(int64(off))
	}
	d("utc_offset", offsetFn)
	utcPred := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).t.Location() == stdtime.UTC)
	}
	d("utc?", utcPred)
	dstFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).t.IsDST())
	}
	d("dst?", dstFn)

	// Conversions. utc/gmtime/localtime mutate the receiver and return it (MRI);
	// getutc/getlocal return a new Time.
	toUTC := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).t = self(v).t.UTC()
		return v
	}
	d("utc", toUTC)
	d("localtime", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		local := vm.getlocalTime(self(v), args)
		self(v).t = local.t
		self(v).zoneObj = local.zoneObj
		return v
	})
	getutc := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &Time{t: self(v).t.UTC()}
	}
	d("getutc", getutc)
	d("getlocal", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.getlocalTime(self(v), args)
	})

	// round / floor / ceil to ndigits sub-second digits (default 0).
	d("round", roundFn((*big.Int).Add, true))
	d("floor", roundFn(nil, false))
	d("ceil", roundFn((*big.Int).Add, false))

	// to_a → [sec, min, hour, mday, mon, year, wday, yday, isdst, zone].
	d("to_a", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self(v).t
		name, _ := t.Zone()
		zone := object.Value(object.NilV)
		if name != "" {
			zone = object.NewString(name)
		}
		return object.NewArray(
			object.IntValue(int64(t.Second())), object.IntValue(int64(t.Minute())),
			object.IntValue(int64(t.Hour())), object.IntValue(int64(t.Day())),
			object.IntValue(int64(t.Month())), object.IntValue(int64(t.Year())),
			object.IntValue(int64(t.Weekday())), object.IntValue(int64(t.YearDay())),
			object.Bool(t.IsDST()), zone,
		)
	})

	// deconstruct_keys(keys) backs pattern matching (MRI 4.0): nil returns the
	// whole field hash; an Array returns just the requested Symbol keys that name
	// a field, ignoring (not raising on) non-Symbol or unknown keys; any other
	// argument raises TypeError.
	d("deconstruct_keys", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		t := self(v)
		h := object.NewHash()
		if object.IsNil(args[0]) {
			for _, k := range timeDeconstructKeys {
				val, _ := t.fieldValue(k)
				h.Set(object.Symbol(k), val)
			}
			return h
		}
		arr, ok := args[0].(*object.Array)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected Array or nil)", vm.classOf(args[0]).name)
		}
		for _, k := range arr.Elems {
			sym, ok := k.(object.Symbol)
			if !ok {
				continue // a non-Symbol key is ignored, not an error
			}
			if val, ok := t.fieldValue(string(sym)); ok {
				h.Set(sym, val)
			}
		}
		return h
	})

	// Arithmetic and ordering.
	d("+", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.timeArith(bytecode.OpAdd, self(v), args[0])
	})
	d("-", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.timeArith(bytecode.OpSub, self(v), args[0])
	})
	d("<=>", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if other, ok := args[0].(*Time); ok {
			return object.IntValue(timeCmp(self(v), other))
		}
		// A non-Time argument: MRI reverses the comparison — it asks the other
		// object for `other <=> self` and inverts the sign of the answer (nil
		// stays nil), dispatching the sign test through the result's own #>/#<.
		r := vm.send(args[0], "<=>", []object.Value{v}, nil)
		if object.IsNil(r) {
			return object.NilV
		}
		switch {
		case vm.send(r, ">", []object.Value{object.IntValue(0)}, nil).Truthy():
			return object.IntValue(-1)
		case vm.send(r, "<", []object.Value{object.IntValue(0)}, nil).Truthy():
			return object.IntValue(1)
		default:
			return object.IntValue(0)
		}
	})
	d("<", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).t.Before(timeArg(args[0]).t))
	})
	d(">", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).t.After(timeArg(args[0]).t))
	})
	d("<=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(!self(v).t.After(timeArg(args[0]).t))
	})
	d(">=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(!self(v).t.Before(timeArg(args[0]).t))
	})
	eqFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*Time)
		return object.Bool(ok && self(v).t.Equal(other.t))
	}
	d("==", eqFn)
	d("eql?", eqFn)
	d("hash", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).t.UnixNano())
	})

	// True aliases share one Method record so Time.instance_method(:mon) ==
	// Time.instance_method(:month), matching MRI 4.0.6 (Time#mday, #tv_sec,
	// #gmt_offset, #gmtoff, #gmt?, #isdst, #gmtime, #getgm, #ctime and
	// #xmlschema are all documented aliases).
	for _, pair := range [][2]string{
		{"mon", "month"}, {"mday", "day"},
		{"tv_sec", "to_i"}, {"tv_usec", "usec"}, {"tv_nsec", "nsec"},
		{"gmt_offset", "utc_offset"}, {"gmtoff", "utc_offset"},
		{"gmt?", "utc?"}, {"isdst", "dst?"},
		{"gmtime", "utc"}, {"getgm", "getutc"},
		{"ctime", "asctime"}, {"xmlschema", "iso8601"},
	} {
		vm.cTime.methods[pair[0]] = vm.cTime.methods[pair[1]]
	}
}

// mixinTimeComparable adds Comparable to Time's ancestry so Time.include?
// (Comparable) is true (MRI mixes it in); Time's own #<=> already drives the
// comparison operators, and Comparable adds #between?/#clamp. Run after the
// prelude, which defines the Comparable module.
func (vm *VM) mixinTimeComparable() {
	if cmp, ok := vm.consts["Comparable"].(*RClass); ok {
		vm.cTime.includes = append(vm.cTime.includes, cmp)
	}
}

// timeZoneKw pops a trailing keyword hash carrying in: <zone> off args, returning
// the resolved location (nil when absent) and the remaining positional args.
func timeZoneKw(args []object.Value) (object.Value, []object.Value) {
	if n := len(args); n > 0 {
		if h, ok := args[n-1].(*object.Hash); ok {
			if z, ok := h.Get(object.Symbol("in")); ok {
				return z, args[:n-1]
			}
		}
	}
	return nil, args
}

// getlocalTime computes the local representation of recv used by Time#getlocal
// (and, mutating the receiver, Time#localtime): with no argument the machine's
// local zone, with a timezone object (one answering #utc_to_local) that zone —
// rendered through the object exactly as Time.at(in: zone) — and otherwise the
// same utc_offset forms Time.new accepts.
func (vm *VM) getlocalTime(recv *Time, args []object.Value) *Time {
	if len(args) > 0 && vm.respondsToDynamic(args[0], "utc_to_local") {
		return vm.timeInZoneObject(recv.t.Unix(), int64(recv.t.Nanosecond()), args[0])
	}
	return &Time{t: recv.t.In(vm.localtimeLoc(args))}
}

// localtimeLoc resolves the optional offset argument of localtime / getlocal:
// none → the local zone, else the same utc_offset forms Time.new accepts
// (String or Integer seconds).
func (vm *VM) localtimeLoc(args []object.Value) *stdtime.Location {
	if len(args) == 0 {
		return localLoc()
	}
	return vm.newTimeOffset(args[0])
}

// timeAt implements Time.at(time, subsec = nil, unit = :microsecond, in:).
func (vm *VM) timeAt(args []object.Value) *Time {
	zone, pos := timeZoneKw(args)
	if len(pos) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
	}
	hasZone := zone != nil && !object.IsNil(zone)
	// A Time source is duplicated; a subsecond argument is then a TypeError (#8173).
	if src, ok := pos[0].(*Time); ok {
		if len(pos) >= 2 {
			raise("TypeError", "can't convert Time into an exact number")
		}
		if hasZone {
			return vm.applyInstantZone(src.t.Unix(), int64(src.t.Nanosecond()), zone)
		}
		// A Time source keeps its own zone.
		return &Time{t: src.t}
	}
	sec, ns := vm.timeAtSeconds(pos[0])
	if len(pos) >= 2 {
		ns += int64(vm.numExactFloat(pos[1]) * float64(unitNanos(pos, 2)))
	}
	if hasZone {
		return vm.applyInstantZone(sec, ns, zone)
	}
	// A numeric source with no zone keeps rbgo's deterministic UTC instant. A lone
	// Float/Rational argument is placed exactly so any sub-nanosecond part of the
	// value survives (MRI keeps the sub-second as an exact Rational).
	if r, ok := atExactSeconds(pos); ok {
		return newTimeExact(r, stdtime.UTC, nil)
	}
	return &Time{t: stdtime.Unix(sec, ns).UTC()}
}

// atExactSeconds returns the exact seconds of Time.at's argument as a Rational
// (ok=true) for the lone-Float/Rational form that can carry sub-nanosecond
// precision; every other form (integer, coerced, or with an explicit subsecond
// argument) is nanosecond-granular and handled by the (sec, ns) path. A
// non-finite Float has no instant, so it raises FloatDomainError as MRI does.
func atExactSeconds(pos []object.Value) (*big.Rat, bool) {
	if len(pos) != 1 {
		return nil, false
	}
	switch n := pos[0].(type) {
	case object.Float:
		r := new(big.Rat).SetFloat64(float64(n))
		if r == nil {
			raise("FloatDomainError", "%s", n.Inspect())
		}
		return r, true
	case *object.Rational:
		return new(big.Rat).Set(n.R), true
	}
	return nil, false
}

// timeAtSeconds resolves Time.at's numeric first argument to whole seconds plus a
// nanosecond remainder, keeping a Rational exact (so microsecond/nanosecond
// round-trips are lossless) and coercing via #to_int or #to_r as MRI does.
func (vm *VM) timeAtSeconds(v object.Value) (int64, int64) {
	switch n := v.(type) {
	case object.Integer:
		return int64(n), 0
	case object.Float:
		return splitSeconds(float64(n))
	case *object.Rational:
		return ratSecNs(n.R)
	}
	if vm.respondsToDynamic(v, "to_int") {
		if i, ok := vm.send(v, "to_int", nil, nil).(object.Integer); ok {
			return int64(i), 0
		}
	}
	if vm.isNumeric(v) && vm.respondsToDynamic(v, "to_r") {
		if r, ok := vm.send(v, "to_r", nil, nil).(*object.Rational); ok {
			return ratSecNs(r.R)
		}
	}
	raise("TypeError", "can't convert %s into an exact number", vm.classOf(v).name)
	return 0, 0
}

// isNumeric reports whether v is a Numeric (an Integer/Float/Rational or any
// user class descending from Numeric). Time.at/utc only fall back to #to_r for a
// Numeric — a String or nil answers #to_r yet must still be rejected.
func (vm *VM) isNumeric(v object.Value) bool {
	nc, ok := vm.consts["Numeric"].(*RClass)
	return ok && classIsA(vm.classOf(v), nc)
}

// ratSecNs floor-divides a Rational number of seconds into whole seconds and a
// nanosecond remainder in [0, 1e9), preserving exactness.
func ratSecNs(r *big.Rat) (int64, int64) {
	num, den := r.Num(), r.Denom()
	q := new(big.Int).Div(num, den) // Euclidean: floors toward -inf
	rem := new(big.Int).Sub(num, new(big.Int).Mul(q, den))
	ns := new(big.Int).Mul(rem, big.NewInt(1e9))
	ns.Div(ns, den)
	return q.Int64(), ns.Int64()
}

// applyInstantZone renders a known instant (sec + ns) through Time.at/now's in:
// zone: a timezone object (via #utc_to_local) or a plain offset.
func (vm *VM) applyInstantZone(sec, ns int64, zone object.Value) *Time {
	if vm.respondsToDynamic(zone, "utc_to_local") {
		return vm.timeInZoneObject(sec, ns, zone)
	}
	return &Time{t: stdtime.Unix(sec, ns).In(vm.newTimeOffset(zone))}
}

// unitNanos reads the optional unit symbol (index i) of Time.at, returning the
// nanoseconds one such unit is worth (:millisecond/:microsecond/:nanosecond;
// default microsecond).
func unitNanos(pos []object.Value, i int) int64 {
	if len(pos) <= i {
		return 1000
	}
	switch pos[i].(object.Symbol) {
	case "millisecond":
		return 1_000_000
	case "microsecond", "usec":
		return 1000
	case "nanosecond", "nsec":
		return 1
	}
	raise("ArgumentError", "unexpected unit: %s", pos[i].Inspect())
	return 0
}

// splitSeconds decomposes a floating second count into whole seconds and the
// remaining nanoseconds (rounded).
func splitSeconds(f float64) (int64, int64) {
	whole := int64(f)
	ns := int64((f - float64(whole)) * 1e9)
	if ns < 0 { // negative fraction: borrow a second so ns stays in [0,1e9)
		whole--
		ns += 1e9
	}
	return whole, ns
}

// numFloat marshals a Ruby Integer / Float / Rational to a float64, raising
// TypeError otherwise.
func numFloat(v object.Value) float64 {
	switch n := v.(type) {
	case object.Integer:
		return float64(n)
	case object.Float:
		return float64(n)
	case *object.Rational:
		f, _ := n.R.Float64()
		return f
	}
	raise("TypeError", "no implicit conversion of %s into Time", v.Inspect())
	return 0
}

// timeInt coerces a calendar field to an int the way MRI does: an Integer or
// Float directly, otherwise via #to_int (whose result must itself be an
// Integer), else a class-named TypeError.
func (vm *VM) timeInt(v object.Value) int {
	switch n := v.(type) {
	case object.Integer:
		return int(n)
	case object.Float:
		return int(n)
	}
	// A Bignum, or any object with #to_int, is coerced through the protocol; a
	// Bignum answers #to_int with itself, so it lands in the result switch below.
	if vm.respondsTo(v, "to_int") {
		r := vm.send(v, "to_int", nil, nil)
		switch n := r.(type) {
		case object.Integer:
			return int(n)
		case *object.Bignum:
			// Every rbgo Bignum is outside int64, so no calendar field can hold one.
			raise("RangeError", "bignum too big to convert into 'long'")
		}
		raise("TypeError", "can't convert %s to Integer (%s#to_int gives %s)",
			vm.classOf(v).name, vm.classOf(v).name, vm.classOf(r).name)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", vm.classOf(v).name)
	return 0
}

// timeNow implements Time.now(in: zone): the VM clock, optionally rendered in a
// given offset or timezone object (via #utc_to_local).
func (vm *VM) timeNow(args []object.Value) *Time {
	zone, _, _, _ := popTimeKwargs(args)
	n := vm.nowInstant()
	if zone == nil || object.IsNil(zone) {
		return &Time{t: n}
	}
	return vm.applyInstantZone(n.Unix(), int64(n.Nanosecond()), zone)
}

// timeNew implements Time.new: no positional args → now (optionally zoned); a lone
// String argument → the ISO-8601-like string form; else
// year[,mon,day,hour,min,sec,zone] with an optional in: keyword overriding the
// positional zone.
func (vm *VM) timeNew(args []object.Value) *Time {
	kwZone, precision, hasPrec, pos := popTimeKwargs(args)
	if len(pos) == 0 {
		n := vm.nowInstant()
		if kwZone == nil || object.IsNil(kwZone) {
			return &Time{t: n}
		}
		return vm.applyInstantZone(n.Unix(), int64(n.Nanosecond()), kwZone)
	}
	if s, ok := pos[0].(*object.String); ok && len(pos) == 1 {
		return vm.timeNewFromString(s, kwZone, precision, hasPrec)
	}
	if len(pos) > 7 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 0..7)", len(pos))
	}
	// Resolve the zone: the 7th positional slot or the in: keyword, never both.
	var zoneArg object.Value
	if len(pos) >= 7 && !object.IsNil(pos[6]) {
		zoneArg = pos[6]
	}
	if kwZone != nil && !object.IsNil(kwZone) {
		if zoneArg != nil {
			raise("ArgumentError", "timezone argument given as positional and keyword arguments")
		}
		zoneArg = kwZone
	}
	cal := pos
	if len(pos) >= 7 {
		cal = pos[:6]
	}
	if zoneArg == nil {
		return vm.buildTime(cal, 0, false, localLoc())
	}
	if vm.respondsToDynamic(zoneArg, "local_to_utc") {
		return vm.buildTimeZoneObjectNew(cal, zoneArg)
	}
	return vm.buildTime(cal, 0, false, vm.newTimeOffset(zoneArg))
}

// popTimeKwargs strips a trailing keyword hash carrying in: and/or precision:,
// returning the zone value (nil when absent), the precision value, whether a
// precision: key was present, and the remaining positional args.
func popTimeKwargs(args []object.Value) (zone, precision object.Value, hasPrec bool, pos []object.Value) {
	if n := len(args); n > 0 {
		if h, ok := args[n-1].(*object.Hash); ok {
			z, hasIn := h.Get(object.Symbol("in"))
			p, hasP := h.Get(object.Symbol("precision"))
			if hasIn || hasP {
				if hasIn {
					zone = z
				}
				return zone, p, hasP, args[:n-1]
			}
		}
	}
	return nil, nil, false, args
}

// newTimeOffset resolves Time.new's utc_offset / in: argument to a location: a
// String in "(+|-)HH[:MM[:SS]]", "UTC"/"Z" or single military-letter form; an
// Integer/Float/Rational number of seconds in (-86400, 86400); or an object that
// answers #to_str, #to_int or #to_r. Anything else is a TypeError, as MRI reports
// for an object it cannot read as an exact number.
func (vm *VM) newTimeOffset(v object.Value) *stdtime.Location {
	switch x := v.(type) {
	case *object.String:
		return parseUtcOffset(x.Str())
	case object.Integer:
		return fixedOffsetLoc(int(x))
	case object.Float:
		return fixedOffsetLoc(int(x))
	case *object.Rational:
		f, _ := x.R.Float64()
		return fixedOffsetLoc(int(f))
	case *object.Bignum:
		raise("ArgumentError", "utc_offset out of range")
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return parseUtcOffset(s.Str())
		}
	}
	if vm.respondsToDynamic(v, "to_int") {
		if n, ok := vm.send(v, "to_int", nil, nil).(object.Integer); ok {
			return fixedOffsetLoc(int(n))
		}
	}
	if vm.isNumeric(v) && vm.respondsToDynamic(v, "to_r") {
		if r, ok := vm.send(v, "to_r", nil, nil).(*object.Rational); ok {
			f, _ := r.R.Float64()
			return fixedOffsetLoc(int(f))
		}
	}
	raise("TypeError", "can't convert %s into an exact number", vm.classOf(v).name)
	return nil
}

// buildTimeZoneObjectNew implements Time.new(y,mo,d,h,mi,s, zone) for a timezone
// object: it reads the given fields as a local wall clock, hands that Time-like
// value to zone#local_to_utc to obtain the UTC instant, and derives the fixed
// offset from the difference. The zone object is retained so #zone returns it.
func (vm *VM) buildTimeZoneObjectNew(cal []object.Value, zone object.Value) *Time {
	wall := vm.buildTime(cal, 0, false, stdtime.UTC)
	localEpoch := wall.t.Unix()
	ns := int64(wall.t.Nanosecond())
	result := vm.send(zone, "local_to_utc", []object.Value{wall}, nil)
	utcEpoch := vm.zoneResultToIEpoch(result)
	return vm.assembleZoneObjectTime(zone, wall, utcEpoch, localEpoch-utcEpoch, ns)
}

// timeInZoneObject renders a known UTC instant through a timezone object's
// #utc_to_local method (Time.now/Time.at with in: a timezone object): the local
// wall clock comes from the result's broken-down fields, and the fixed offset is
// the difference between that wall clock and the instant.
func (vm *VM) timeInZoneObject(utcEpoch, ns int64, zone object.Value) *Time {
	wall := &Time{t: stdtime.Unix(utcEpoch, ns).UTC()}
	result := vm.send(zone, "utc_to_local", []object.Value{wall}, nil)
	localEpoch := vm.zoneResultFieldsEpoch(result)
	return vm.assembleZoneObjectTime(zone, wall, utcEpoch, localEpoch-utcEpoch, ns)
}

// assembleZoneObjectTime builds the final zone-object Time from the instant, the
// derived offset (range-checked as MRI does) and the abbreviation reported by the
// zone's optional #abbr method.
func (vm *VM) assembleZoneObjectTime(zone object.Value, wall *Time, utcEpoch, offset, ns int64) *Time {
	if offset <= -86400 || offset >= 86400 {
		raise("ArgumentError", "utc_offset out of range")
	}
	name := ""
	if vm.respondsToDynamic(zone, "abbr") {
		if s, ok := vm.send(zone, "abbr", []object.Value{wall}, nil).(*object.String); ok {
			name = s.Str()
		}
	}
	t := stdtime.Unix(utcEpoch, ns).In(stdtime.FixedZone(name, int(offset)))
	return &Time{t: t, zoneObj: zone}
}

// zoneResultToIEpoch reads the UTC epoch of a #local_to_utc result via #to_i (the
// result may be a Time, Integer or any object answering #to_i).
func (vm *VM) zoneResultToIEpoch(result object.Value) int64 {
	switch n := result.(type) {
	case *Time:
		return n.t.Unix()
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		return n.I.Int64()
	}
	switch r := vm.send(result, "to_i", nil, nil).(type) {
	case object.Integer:
		return int64(r)
	case *object.Bignum:
		return r.I.Int64()
	}
	raise("TypeError", "can't convert %s into an exact number", vm.classOf(result).name)
	return 0
}

// zoneResultFieldsEpoch reads the local wall clock of a #utc_to_local result as a
// UTC epoch: a Time result contributes its own broken-down wall clock (read as
// UTC), while any other result — an Integer or an object answering #to_i — is
// taken as the epoch directly (MRI ignores a non-Time result's field readers).
func (vm *VM) zoneResultFieldsEpoch(result object.Value) int64 {
	if tt, ok := result.(*Time); ok {
		t := tt.t
		return stdtime.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, stdtime.UTC).Unix()
	}
	return vm.zoneResultToIEpoch(result)
}

// fixedOffsetLoc turns a numeric utc_offset into a fixed zone, rejecting a
// magnitude of a whole day or more the way MRI does.
func fixedOffsetLoc(sec int) *stdtime.Location {
	if sec <= -86400 || sec >= 86400 {
		raise("ArgumentError", "utc_offset out of range")
	}
	return stdtime.FixedZone("", sec)
}

// parseUtcOffset reads Time.new's String utc_offset, raising MRI's exact
// ArgumentError for any form it does not recognise. An hour field ≥ 24 is not a
// format error but an out-of-range offset (fixedOffsetLoc reports it), while a
// minute/second field ≥ 60 is a format error.
func parseUtcOffset(s string) *stdtime.Location {
	if s == "UTC" {
		return stdtime.UTC
	}
	// A single military-zone letter: A..I = +1..+9h, K..M = +10..+12h,
	// N..Y = -1..-12h, Z = UTC ('J', the local zone, is intentionally excluded).
	if len(s) == 1 {
		c := s[0]
		switch {
		case c >= 'A' && c <= 'I':
			return fixedOffsetLoc(int(c-'A'+1) * 3600)
		case c >= 'K' && c <= 'M':
			return fixedOffsetLoc(int(c-'K'+10) * 3600)
		case c >= 'N' && c <= 'Y':
			return fixedOffsetLoc(-int(c-'N'+1) * 3600)
		case c == 'Z':
			return stdtime.UTC
		}
		raiseBadOffset(s)
	}
	sign := 1
	switch {
	case strings.HasPrefix(s, "+"):
	case strings.HasPrefix(s, "-"):
		sign = -1
	default:
		raiseBadOffset(s)
	}
	body := strings.ReplaceAll(s[1:], ":", "")
	if len(body) != 2 && len(body) != 4 && len(body) != 6 {
		raiseBadOffset(s)
	}
	hh, mm, ss := 0, 0, 0
	for i := 0; i < len(body); i += 2 {
		n, err := strconv.Atoi(body[i : i+2])
		if err != nil {
			raiseBadOffset(s)
		}
		switch i {
		case 0:
			hh = n
		case 2:
			mm = n
		case 4:
			ss = n
		}
	}
	if mm > 59 || ss > 59 {
		raiseBadOffset(s)
	}
	total := sign * (hh*3600 + mm*60 + ss)
	if total == 0 && sign < 0 {
		// "-00:00" is RFC 3339's unknown-offset UTC: MRI reports its zone as "UTC".
		return stdtime.UTC
	}
	return fixedOffsetLoc(total)
}

func raiseBadOffset(s string) {
	raise("ArgumentError",
		"\"+HH:MM\", \"-HH:MM\", \"UTC\" or \"A\"..\"I\",\"K\"..\"Z\" expected for utc_offset: %s", s)
}

// timeStrFull matches Time.new's ISO-8601-like String form; timeStrYearOnly the
// bare-year shorthand. Field widths are validated after the match so a malformed
// component yields MRI's "can't parse" rather than a silent mis-parse.
var (
	timeStrYearOnly = regexp.MustCompile(`^\d{4,}$`)
	timeStrFull     = regexp.MustCompile(`^(\d+)-(\d+)-(\d+)[ T](\d+):(\d+):(\d+)(\.\d*)?(.*)$`)
)

// timeNewFromString implements the Ruby 3.2+ Time.new(String) form, parsing an
// ISO-8601-like instant (or a bare year), applying the precision: truncation and
// resolving the zone from the String's own offset, else the in: keyword, else
// local.
func (vm *VM) timeNewFromString(s *object.String, kwZone, precision object.Value, hasPrec bool) *Time {
	if !asciiCompatEnc(s.Enc) {
		raise("ArgumentError", "time string should have ASCII compatible encoding")
	}
	str := s.Str()
	// prec is the number of sub-second digits kept: 9 by default, an explicit
	// non-negative count (which may exceed 9, landing in the sub-nanosecond frac),
	// or "keep everything" for precision: nil or a negative count.
	prec := 9
	if hasPrec {
		if object.IsNil(precision) {
			prec = -1
		} else if prec = vm.timeInt(precision); prec < 0 {
			prec = -1
		}
	}

	year, mon, day, hour, min, sec := 0, 1, 1, 0, 0, 0
	fracDigits := ""
	var strLoc *stdtime.Location

	switch {
	case timeStrYearOnly.MatchString(str):
		year = parseRubyInt(str)
	case timeStrFull.MatchString(str):
		m := timeStrFull.FindStringSubmatch(str)
		if len(m[1]) < 4 || len(m[2]) != 2 || len(m[3]) != 2 ||
			len(m[4]) != 2 || len(m[5]) != 2 || len(m[6]) != 2 {
			timeCantParse(str)
		}
		year, mon, day = atoi(m[1]), atoi(m[2]), atoi(m[3])
		hour, min, sec = atoi(m[4]), atoi(m[5]), atoi(m[6])
		if m[7] == "." { // a dot with no digits after it
			timeCantParse(str)
		}
		if len(m[7]) > 1 {
			fracDigits = m[7][1:]
		}
		if rest := m[8]; rest != "" {
			off := rest
			if strings.HasPrefix(off, " ") {
				off = off[1:]
			}
			if off == "" || strings.ContainsAny(off, " \t\n\v\f\r") {
				timeCantParse(str)
			}
			strLoc = parseUtcOffset(off)
		}
	default:
		timeCantParse(str)
	}

	checkRange("mon", mon, 1, 12)
	checkRange("mday", day, 1, 31)
	checkRange("hour", hour, 0, 24)
	checkRange("min", min, 0, 59)
	checkRange("sec", sec, 0, 60)

	if prec >= 0 && len(fracDigits) > prec {
		fracDigits = fracDigits[:prec]
	}
	ns, frac := splitFracDigits(fracDigits)

	loc := localLoc()
	switch {
	case strLoc != nil:
		loc = strLoc
	case kwZone != nil && !object.IsNil(kwZone):
		loc = vm.newTimeOffset(kwZone)
	}
	return &Time{t: stdtime.Date(year, stdtime.Month(mon), day, hour, min, sec, ns, loc), frac: frac}
}

// splitFracDigits converts a decimal sub-second digit string (e.g. "123456789876"
// for 0.123456789876 s) into the whole-nanosecond count and any sub-nanosecond
// remainder in seconds — nil when the string holds nine digits or fewer, or when
// the digits past the ninth are all zero.
func splitFracDigits(digits string) (int, *big.Rat) {
	if digits == "" {
		return 0, nil
	}
	nsPart := digits
	if len(nsPart) > 9 {
		nsPart = nsPart[:9]
	}
	ns := atoi(nsPart + strings.Repeat("0", 9-len(nsPart)))
	if len(digits) <= 9 {
		return ns, nil
	}
	extra := digits[9:]
	num, _ := new(big.Int).SetString(extra, 10)
	if num.Sign() == 0 {
		return ns, nil
	}
	den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(extra))), nil)
	frac := new(big.Rat).SetFrac(num, den)        // fraction of one nanosecond
	return ns, frac.Quo(frac, big.NewRat(1e9, 1)) // → seconds
}

// timeCantParse raises MRI's ArgumentError for an unparseable Time.new String.
func timeCantParse(s string) {
	raise("ArgumentError", "can't parse: %s", strconv.Quote(s))
}

// asciiCompatEnc reports whether an encoding tag is ASCII-compatible; the wide
// UTF-16/UTF-32 forms are not and a Time string in one is rejected.
func asciiCompatEnc(enc string) bool {
	switch strings.ToUpper(enc) {
	case "UTF-16LE", "UTF-16BE", "UTF-16", "UTF-32LE", "UTF-32BE", "UTF-32":
		return false
	}
	return true
}

// atoi parses an all-ASCII-digit string already validated by the caller.
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// monthAbbrevs are the three-letter month names Time.new accepts for a String
// month argument (case-insensitively).
var monthAbbrevs = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}

// monthAbbrev maps a three-letter month name to 1..12, or 0 when unrecognised.
func monthAbbrev(s string) int {
	ls := strings.ToLower(s)
	for i, m := range monthAbbrevs {
		if ls == m {
			return i + 1
		}
	}
	return 0
}

// parseRubyInt parses a String calendar field with Kernel#Integer semantics:
// optional surrounding whitespace, an optional sign and base-10 digits only.
func parseRubyInt(s string) int {
	t := strings.TrimSpace(s)
	neg := false
	switch {
	case strings.HasPrefix(t, "+"):
		t = t[1:]
	case strings.HasPrefix(t, "-"):
		neg = true
		t = t[1:]
	}
	if t == "" {
		raiseBadInteger(s)
	}
	n := 0
	for i := 0; i < len(t); i++ {
		c := t[i]
		if c < '0' || c > '9' {
			raiseBadInteger(s)
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		n = -n
	}
	return n
}

func raiseBadInteger(s string) {
	raise("ArgumentError", "invalid value for Integer(): %s", strconv.Quote(s))
}

// timeFieldInt coerces a calendar field (year, day, hour, min) to an int: a
// String is parsed with Integer() semantics, otherwise the #to_int protocol
// (Integer/Float direct) applies.
func (vm *VM) timeFieldInt(v object.Value) int {
	if s, ok := v.(*object.String); ok {
		return parseRubyInt(s.Str())
	}
	return vm.timeInt(v)
}

// timeFieldPart returns calendar field i coerced to an int, or def when it is
// absent or nil.
func (vm *VM) timeFieldPart(pos []object.Value, i, def int) int {
	if len(pos) <= i || object.IsNil(pos[i]) {
		return def
	}
	return vm.timeFieldInt(pos[i])
}

// timeMonthPart returns the month field, honouring a nil/absent default and the
// String short-month-name / numeral forms MRI's month_arg accepts.
func (vm *VM) timeMonthPart(pos []object.Value, i, def int) int {
	if len(pos) <= i || object.IsNil(pos[i]) {
		return def
	}
	v := pos[i]
	str, isStr := "", false
	if s, ok := v.(*object.String); ok {
		str, isStr = s.Str(), true
	} else if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			str, isStr = s.Str(), true
		}
	}
	if isStr {
		if len(str) == 3 {
			if m := monthAbbrev(str); m > 0 {
				return m
			}
		}
		return parseRubyInt(str)
	}
	return vm.timeInt(v)
}

// timeSecParts coerces the seconds field to whole seconds plus a nanosecond
// remainder: an Integer/String is a whole second, a Float/Rational carries a
// fraction, and #to_int / #to_r are honoured.
func (vm *VM) timeSecParts(v object.Value) (int, int64) {
	switch n := v.(type) {
	case object.Integer:
		return int(n), 0
	case object.Float:
		return floatSecParts(float64(n))
	case *object.Rational:
		s, ns := ratSecNs(n.R)
		return int(s), ns
	case *object.String:
		return parseRubyInt(n.Str()), 0
	}
	if vm.respondsToDynamic(v, "to_int") {
		if i, ok := vm.send(v, "to_int", nil, nil).(object.Integer); ok {
			return int(i), 0
		}
	}
	if vm.isNumeric(v) && vm.respondsToDynamic(v, "to_r") {
		if r, ok := vm.send(v, "to_r", nil, nil).(*object.Rational); ok {
			s, ns := ratSecNs(r.R)
			return int(s), ns
		}
	}
	raise("TypeError", "no implicit conversion of %s into Time", v.Inspect())
	return 0, 0
}

// floatSecParts splits a floating second count into whole seconds and a
// nanosecond remainder.
func floatSecParts(f float64) (int, int64) {
	w := int(f)
	return w, int64((f - float64(w)) * 1e9)
}

// numExactFloat coerces an Integer/Float/Rational (or #to_int / #to_r) to a
// float64 for a sub-second argument, raising TypeError otherwise.
func (vm *VM) numExactFloat(v object.Value) float64 {
	switch n := v.(type) {
	case object.Integer:
		return float64(n)
	case object.Float:
		return float64(n)
	case *object.Rational:
		f, _ := n.R.Float64()
		return f
	}
	if vm.respondsToDynamic(v, "to_int") {
		if i, ok := vm.send(v, "to_int", nil, nil).(object.Integer); ok {
			return float64(i)
		}
	}
	if vm.isNumeric(v) && vm.respondsToDynamic(v, "to_r") {
		if r, ok := vm.send(v, "to_r", nil, nil).(*object.Rational); ok {
			f, _ := r.R.Float64()
			return f
		}
	}
	raise("TypeError", "no implicit conversion of %s into Time", v.Inspect())
	return 0
}

// timeFromCalendar implements Time.utc / Time.local / Time.gm / Time.mktime,
// accepting both the year[,mon,day,hour,min,sec,usec] form (1..8 args, the 8th
// ignored) and the C-style 10-argument gmtime form (sec, min, hour, mday, mon,
// year, …), in the given location.
func (vm *VM) timeFromCalendar(args []object.Value, loc *stdtime.Location) *Time {
	if len(args) == 10 {
		// C-style: sec, min, hour, mday, mon, year, wday, yday, isdst, tz.
		pos := []object.Value{args[5], args[4], args[3], args[2], args[1], args[0]}
		return vm.buildTime(pos, 0, false, loc)
	}
	if len(args) == 0 || len(args) == 9 || len(args) > 10 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 1..8)", len(args))
	}
	usecNs, usecGiven := int64(0), false
	if len(args) >= 7 && !object.IsNil(args[6]) {
		usecNs = vm.usecNanos(args[6])
		usecGiven = true
	}
	return vm.buildTime(args, usecNs, usecGiven, loc)
}

// usecNanos coerces Time.utc/local's 7th argument (microseconds, possibly
// fractional) to nanoseconds, rejecting a value outside [0, 1_000_000).
func (vm *VM) usecNanos(v object.Value) int64 {
	f := vm.numExactFloat(v)
	if f < 0 || f >= 1_000_000 {
		raise("ArgumentError", "subsecx out of range")
	}
	return int64(f * 1000)
}

// buildTime assembles a Time from calendar parts, coercing each field the way MRI
// does (Strings, #to_int, month names, fractional seconds), range-checking, then
// letting Go's time.Date normalise overflow (Feb 30 → Mar 2, hour 24 → next day,
// sec 60 → next minute). When usecGiven, the microseconds argument supplies the
// sub-second part and any fractional seconds are discarded.
func (vm *VM) buildTime(pos []object.Value, usecNs int64, usecGiven bool, loc *stdtime.Location) *Time {
	year := vm.timeFieldInt(pos[0])
	month := vm.timeMonthPart(pos, 1, 1)
	day := vm.timeFieldPart(pos, 2, 1)
	hour := vm.timeFieldPart(pos, 3, 0)
	min := vm.timeFieldPart(pos, 4, 0)
	sec, secNs := 0, int64(0)
	if len(pos) >= 6 && !object.IsNil(pos[5]) {
		sec, secNs = vm.timeSecParts(pos[5])
	}
	ns := secNs
	if usecGiven {
		ns = usecNs
	}

	checkRange("mon", month, 1, 12)
	checkRange("mday", day, 1, 31)
	checkRange("hour", hour, 0, 24)
	checkRange("min", min, 0, 59)
	checkRange("sec", sec, 0, 60)
	return &Time{t: stdtime.Date(year, stdtime.Month(month), day, hour, min, sec, int(ns), loc)}
}

// checkRange enforces MRI's two-tier range error: a value below the low bound is
// wildly off ("argument out of range"), while one above the high bound gets the
// field-specific "<field> out of range".
func checkRange(field string, v, lo, hi int) {
	if v < lo {
		raise("ArgumentError", "argument out of range")
	}
	if v > hi {
		raise("ArgumentError", "%s out of range", field)
	}
}

// roundFn builds Time#round / #floor / #ceil. add is (*big.Int).Add for round /
// ceil (nil for floor); half selects round's nearest-neighbour bias.
func roundFn(add func(z, x, y *big.Int) *big.Int, half bool) NativeFn {
	return func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		digits := 0
		if len(args) > 0 {
			digits = vm.timeInt(args[0])
		}
		t := v.(*Time).t
		sec, ns := t.Unix(), int64(t.Nanosecond())
		total := big.NewInt(sec)
		total.Mul(total, big.NewInt(1e9))
		total.Add(total, big.NewInt(ns))

		unit := big.NewInt(1)
		if digits < 9 {
			unit.Exp(big.NewInt(10), big.NewInt(int64(9-digits)), nil)
		}
		q, r := new(big.Int), new(big.Int)
		q.DivMod(total, unit, r)
		if half { // round: bump the quotient when the remainder reaches half a unit.
			twice := new(big.Int).Mul(r, big.NewInt(2))
			if twice.Cmp(unit) >= 0 {
				q.Add(q, big.NewInt(1))
			}
		} else if add != nil && r.Sign() > 0 { // ceil: bump on any remainder.
			add(q, q, big.NewInt(1))
		}
		q.Mul(q, unit)
		newSec := new(big.Int).Div(q, big.NewInt(1e9)).Int64()
		newNs := new(big.Int).Mod(q, big.NewInt(1e9)).Int64()
		return &Time{t: stdtime.Unix(newSec, newNs).In(t.Location())}
	}
}

// timeArith implements Time#+ and Time#-. Subtracting another Time yields the
// Float number of seconds between the two instants; otherwise the operand is a
// shift amount, coerced to an exact Rational number of seconds (MRI's num_exact)
// and applied to the nanosecond, preserving the receiver's location and zone.
func (vm *VM) timeArith(op bytecode.Op, a *Time, b object.Value) object.Value {
	if bt, ok := b.(*Time); ok {
		if op == bytecode.OpSub {
			return object.Float(a.t.Sub(bt.t).Seconds())
		}
		// Adding two Times is meaningless — MRI rejects it outright.
		raise("TypeError", "time + time?")
	}
	delta := vm.timeDeltaRat(b)
	if op == bytecode.OpSub {
		delta = new(big.Rat).Neg(delta)
	}
	return a.shiftByRat(delta)
}

// timeDeltaRat coerces a Time#+/#- shift amount to an exact Rational number of
// seconds, following MRI's num_exact: an Integer/Bignum or Rational is exact, a
// Float is taken through its exact binary #to_r, and any other object is coerced
// via #to_r only when it *also* answers #to_int (else via #to_int alone), while a
// String or nil — and an object answering neither — is rejected outright.
func (vm *VM) timeDeltaRat(v object.Value) *big.Rat {
	switch n := v.(type) {
	case object.Float:
		r := new(big.Rat).SetFloat64(float64(n))
		if r == nil { // NaN or ±Infinity has no exact rational
			raise("FloatDomainError", "%s", n.Inspect())
		}
		return r
	case *object.Rational:
		return new(big.Rat).Set(n.R)
	case *object.String, object.Nil:
		// Rejected before any coercion, matching MRI's num_exact.
	default:
		if bi, ok := object.BigOf(v); ok { // Integer or Bignum
			return new(big.Rat).SetInt(bi)
		}
		if vm.respondsToDynamic(v, "to_int") {
			if vm.respondsToDynamic(v, "to_r") {
				return valueToRat(vm.send(v, "to_r", nil, nil))
			}
			return valueToRat(vm.send(v, "to_int", nil, nil))
		}
	}
	raise("TypeError", "can't convert %s into an exact number", vm.classOf(v).name)
	return nil
}

// valueToRat converts an Integer/Bignum/Rational (as returned by #to_r or
// #to_int) to a big.Rat, raising TypeError on any other result.
func valueToRat(v object.Value) *big.Rat {
	if r, ok := v.(*object.Rational); ok {
		return new(big.Rat).Set(r.R)
	}
	if bi, ok := object.BigOf(v); ok {
		return new(big.Rat).SetInt(bi)
	}
	raise("TypeError", "can't convert into an exact number")
	return nil
}

// shiftByRat returns the Time advanced by delta seconds (which may carry a
// sub-nanosecond fraction), computed exactly from the receiver's own exact
// instant so precision beyond the nanosecond is preserved, with the receiver's
// location and Ruby timezone object carried onto the result.
func (t *Time) shiftByRat(delta *big.Rat) *Time {
	cur := new(big.Rat).SetInt64(t.t.Unix())
	cur.Add(cur, t.subsecRat())
	cur.Add(cur, delta)
	return newTimeExact(cur, t.t.Location(), t.zoneObj)
}

// timeCmp returns -1/0/1 ordering two Times.
func timeCmp(a, b *Time) int64 {
	switch {
	case a.t.Before(b.t):
		return -1
	case a.t.After(b.t):
		return 1
	default:
		return 0
	}
}

// timeEqual reports Time equality for valueEqual / the == operator fast path.
func timeEqual(a *Time, other object.Value) bool {
	b, ok := other.(*Time)
	return ok && a.t.Equal(b.t)
}

// ---- strftime -------------------------------------------------------------

// pad renders n as a zero-padded decimal at least width wide.
func pad(n int64, width int) string {
	s := strconv.FormatInt(n, 10)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// signedOffset renders an offset in seconds with the given inner separator
// ("" → "+0900", ":" → "+09:00", "::" is handled by the caller adding seconds).
func signedOffset(off int, sep string) string {
	sign := "+"
	if off < 0 {
		sign, off = "-", -off
	}
	hh, mm := off/3600, (off%3600)/60
	return sign + pad(int64(hh), 2) + sep + pad(int64(mm), 2)
}

// strftimeExpand rewrites the compound directives (%c %D %F %R %r %T %X %x %h)
// into their primitive expansions.
var strftimeExpand = map[byte]string{
	'c': "%a %b %e %H:%M:%S %Y",
	'D': "%m/%d/%y",
	'x': "%m/%d/%y",
	'F': "%Y-%m-%d",
	'R': "%H:%M",
	'r': "%I:%M:%S %p",
	'T': "%H:%M:%S",
	'X': "%H:%M:%S",
	'h': "%b",
	'v': "%e-%^b-%Y", // VMS/Oracle date, e.g. " 3-FEB-2001"
}

// strftime formats a Time per Ruby's strftime directive set: flags (-_0^#),
// an optional width, then a directive. Unknown directives pass through verbatim.
func strftime(t *Time, format string) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			b.WriteByte(format[i])
			continue
		}
		j := i + 1
		flags, width, colons := "", -1, 0
		for j < len(format) && strings.IndexByte("-_0^#", format[j]) >= 0 {
			flags += string(format[j])
			j++
		}
		for j < len(format) && format[j] == ':' {
			colons++
			j++
		}
		w := 0
		for j < len(format) && format[j] >= '0' && format[j] <= '9' {
			w = w*10 + int(format[j]-'0')
			width = w
			j++
		}
		if j >= len(format) {
			b.WriteString(format[i:])
			break
		}
		dir := format[j]
		if exp, ok := strftimeExpand[dir]; ok {
			// A composite/alias directive expands to a sub-format; the parsed
			// flags and explicit width still apply to its rendered result, so
			// `%^h` upcases (%b → FEB) and `%10h` right-pads to width 10.
			b.WriteString(applyFlags(strftime(t, exp), flags, 0, ' ', width))
			i = j
			continue
		}
		out, ok := strftimeField(t, dir, colons, width)
		if !ok {
			b.WriteString(format[i : j+1])
			i = j
			continue
		}
		b.WriteString(applyFlags(out.val, flags, out.width, out.pad, width))
		i = j
	}
	return b.String()
}

// field carries a directive's raw value plus its default field width and pad.
type field struct {
	val   string
	width int
	pad   byte
}

// strftimeField computes a single directive's value (ignoring flags/width, which
// applyFlags layers on), reporting ok=false for an unknown directive.
func strftimeField(t *Time, dir byte, colons, width int) (field, bool) {
	tm := t.t
	num := func(n int64, w int) field { return field{val: strconv.FormatInt(n, 10), width: w, pad: '0'} }
	str := func(s string) field { return field{val: s, width: 0, pad: ' '} }
	_, off := tm.Zone()
	switch dir {
	case 'Y':
		return num(int64(tm.Year()), 4), true
	case 'C':
		return num(int64(tm.Year()/100), 2), true
	case 'y':
		return num(int64(mod(tm.Year(), 100)), 2), true
	case 'm':
		return num(int64(tm.Month()), 2), true
	case 'd':
		return num(int64(tm.Day()), 2), true
	case 'e':
		return field{val: strconv.Itoa(tm.Day()), width: 2, pad: ' '}, true
	case 'H':
		return num(int64(tm.Hour()), 2), true
	case 'k':
		return field{val: strconv.Itoa(tm.Hour()), width: 2, pad: ' '}, true
	case 'I':
		return num(int64(hour12(tm.Hour())), 2), true
	case 'l':
		return field{val: strconv.Itoa(hour12(tm.Hour())), width: 2, pad: ' '}, true
	case 'M':
		return num(int64(tm.Minute()), 2), true
	case 'S':
		return num(int64(tm.Second()), 2), true
	case 'L':
		return field{val: fracDigits(tm.Nanosecond(), 3), width: 0, pad: '0'}, true
	case 'N':
		w := 9
		if width > 0 {
			w = width
		}
		return field{val: fracDigits(tm.Nanosecond(), w), width: 0, pad: '0'}, true
	case 'j':
		return num(int64(tm.YearDay()), 3), true
	case 'p':
		return str(ampm(tm.Hour(), true)), true
	case 'P':
		return str(ampm(tm.Hour(), false)), true
	case 'A':
		return str(tm.Weekday().String()), true
	case 'a':
		return str(tm.Weekday().String()[:3]), true
	case 'B':
		return str(tm.Month().String()), true
	case 'b':
		return str(tm.Month().String()[:3]), true
	case 'u':
		return num(int64(isoWeekday(tm.Weekday())), 0), true
	case 'w':
		return num(int64(tm.Weekday()), 0), true
	case 's':
		return num(tm.Unix(), 0), true
	case 'z':
		return str(zoneOffset(off, colons)), true
	case 'Z':
		return str(zoneName(tm)), true
	case 'U':
		return num(int64(weekOfYear(tm, stdtime.Sunday)), 2), true
	case 'W':
		return num(int64(weekOfYear(tm, stdtime.Monday)), 2), true
	case 'G':
		y, _ := tm.ISOWeek()
		return num(int64(y), 4), true
	case 'g':
		y, _ := tm.ISOWeek()
		return num(int64(mod(y, 100)), 2), true
	case 'V':
		_, wk := tm.ISOWeek()
		return num(int64(wk), 2), true
	case 'n':
		return str("\n"), true
	case 't':
		return str("\t"), true
	case '%':
		return str("%"), true
	}
	return field{}, false
}

// applyFlags lays strftime's flags and explicit width over a field's raw value:
// '-' drops padding, '_' pads with spaces, '0' pads with zeros, '^' upcases and
// '#' swaps case; an explicit width widens beyond the field's default.
func applyFlags(val, flags string, defWidth int, defPad byte, width int) string {
	pad := defPad
	noPad := false
	for _, f := range flags {
		switch f {
		case '-':
			noPad = true
		case '_':
			pad = ' '
		case '0':
			pad = '0'
		case '^':
			val = strings.ToUpper(val)
		case '#':
			val = swapCase(val)
		}
	}
	w := defWidth
	if width > 0 {
		w = width
	}
	if noPad {
		w = 0
	}
	for len(val) < w {
		val = string(pad) + val
	}
	return val
}

// mod returns a Euclidean-positive modulus.
func mod(a, m int) int { return ((a % m) + m) % m }

// hour12 maps a 24-hour clock hour to its 12-hour equivalent (0 and 12 → 12).
func hour12(h int) int {
	h = mod(h, 12)
	if h == 0 {
		return 12
	}
	return h
}

// ampm renders AM/PM (upper) or am/pm (lower) for the given hour.
func ampm(h int, upper bool) string {
	s := "am"
	if h >= 12 {
		s = "pm"
	}
	if upper {
		return strings.ToUpper(s)
	}
	return s
}

// isoWeekday maps Go's Sunday=0 weekday to ISO's Monday=1…Sunday=7.
func isoWeekday(w stdtime.Weekday) int {
	if w == stdtime.Sunday {
		return 7
	}
	return int(w)
}

// fracDigits renders the first n digits of a nanosecond value (zero-padded to 9
// then truncated/extended), used by %L and %N.
func fracDigits(ns, n int) string {
	s := pad(int64(ns), 9)
	if n <= 9 {
		return s[:n]
	}
	return s + strings.Repeat("0", n-9)
}

// zoneOffset renders %z / %:z / %::z for an offset in seconds.
func zoneOffset(off, colons int) string {
	switch colons {
	case 1:
		return signedOffset(off, ":")
	case 2:
		sign := "+"
		a := off
		if a < 0 {
			sign, a = "-", -a
		}
		return sign + pad(int64(a/3600), 2) + ":" + pad(int64((a%3600)/60), 2) + ":" + pad(int64(a%60), 2)
	default:
		return signedOffset(off, "")
	}
}

// zoneName renders %Z: the zone's abbreviation, or the empty string for a bare
// fixed-offset zone — MRI's Time#strftime emits nothing for an offset-only zone
// (unlike #inspect, which shows the numeric offset).
func zoneName(tm stdtime.Time) string {
	name, _ := tm.Zone()
	return name
}

// weekOfYear computes %U (start=Sunday) / %W (start=Monday): the count of whole
// start-of-week days elapsed in the year.
func weekOfYear(tm stdtime.Time, start stdtime.Weekday) int {
	wday := int(tm.Weekday()-start+7) % 7
	return (tm.YearDay() - wday + 6) / 7
}

// swapCase inverts the case of every ASCII letter (strftime's '#' flag).
func swapCase(s string) string {
	b := []byte(s)
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z':
			b[i] = c - 32
		case c >= 'A' && c <= 'Z':
			b[i] = c + 32
		}
	}
	return string(b)
}

// strftimeToGo maps Ruby/C strftime directives to Go reference-time tokens so
// strptime can drive the go-composites Parse (which speaks Go layouts).
var strftimeToGo = map[byte]string{
	'Y': "2006", 'y': "06", 'm': "01", 'd': "02", 'e': "_2",
	'H': "15", 'I': "03", 'M': "04", 'S': "05", 'p': "PM", 'P': "pm",
	'A': "Monday", 'a': "Mon", 'B': "January", 'b': "Jan",
	'Z': "MST", 'z': "-0700", 'j': "002", '%': "%",
}

// rubyLayout converts a strftime format string into a Go layout; unknown
// directives and literal text pass through verbatim.
func rubyLayout(format string) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) {
			if tok, ok := strftimeToGo[format[i+1]]; ok {
				b.WriteString(tok)
				i++
				continue
			}
		}
		b.WriteByte(format[i])
	}
	return b.String()
}
