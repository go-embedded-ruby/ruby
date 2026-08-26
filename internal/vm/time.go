package vm

import (
	"math/big"
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

// repr renders MRI's "2026-06-21 12:34:56 +0000"; when withFrac the sub-second
// fraction is included (inspect, not to_s).
func (t *Time) repr(withFrac bool) string {
	base := t.t.Format("2006-01-02 15:04:05")
	frac := ""
	if withFrac {
		frac = t.fracString()
	}
	return base + frac + " " + t.offsetString()
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
		zone, _ := timeZoneKw(args)
		n := vm.nowInstant()
		if zone != nil {
			n = n.In(vm.newTimeOffset(zone))
		}
		return &Time{t: n}
	})
	// Time.new(...) → now with no args, else year[,mon,day,hour,min,sec,zone];
	// the zone may be the 7th positional argument or the in: keyword (MRI 4.0).
	sm("new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.timeNew(args)
	})
	// Time.utc / Time.gm(year[,mon,day,hour,min,sec,usec]) → a UTC instant.
	utc := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.timeFromCalendar(args, stdtime.UTC)
	}
	sm("utc", utc)
	sm("gm", utc)
	// Time.local / Time.mktime(...) → the same, in the local zone.
	local := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.timeFromCalendar(args, stdtime.Local)
	}
	sm("local", local)
	sm("mktime", local)
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

	d("to_i", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).t.Unix())
	})
	d("to_f", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(float64(self(v).t.UnixNano()) / 1e9)
	})
	d("to_r", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Rational{R: big.NewRat(self(v).t.UnixNano(), 1e9)}
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
	d("ctime", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(strftime(self(v), "%a %b %e %H:%M:%S %Y"))
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
	d("xmlschema", iso)
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
	d("mon", monthFn)
	dayFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Day()))
	}
	d("day", dayFn)
	d("mday", dayFn)
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
		ns := self(v).t.Nanosecond()
		if ns == 0 {
			return object.IntValue(0)
		}
		return &object.Rational{R: big.NewRat(int64(ns), 1e9)}
	})
	d("yday", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.YearDay()))
	})
	d("wday", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Weekday()))
	})

	// POSIX time-value accessors.
	d("tv_sec", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).t.Unix())
	})
	d("tv_usec", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Nanosecond() / 1000))
	})
	d("tv_nsec", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).t.Nanosecond()))
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
		name, _ := self(v).t.Zone()
		if name == "" {
			return object.NilV
		}
		return object.NewString(name)
	})
	offsetFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_, off := self(v).t.Zone()
		return object.IntValue(int64(off))
	}
	d("utc_offset", offsetFn)
	d("gmt_offset", offsetFn)
	d("gmtoff", offsetFn)
	utcPred := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).t.Location() == stdtime.UTC)
	}
	d("utc?", utcPred)
	d("gmt?", utcPred)
	dstFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).t.IsDST())
	}
	d("dst?", dstFn)
	d("isdst", dstFn)

	// Conversions. utc/gmtime/localtime mutate the receiver and return it (MRI);
	// getutc/getlocal return a new Time.
	toUTC := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).t = self(v).t.UTC()
		return v
	}
	d("utc", toUTC)
	d("gmtime", toUTC)
	d("localtime", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).t = self(v).t.In(vm.localtimeLoc(args))
		return v
	})
	d("getutc", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &Time{t: self(v).t.UTC()}
	})
	d("getlocal", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &Time{t: self(v).t.In(vm.localtimeLoc(args))}
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

	// Arithmetic and ordering.
	d("+", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return timeOp(bytecode.OpAdd, self(v), args[0])
	})
	d("-", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return timeOp(bytecode.OpSub, self(v), args[0])
	})
	d("<=>", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*Time)
		if !ok {
			return object.NilV
		}
		return object.IntValue(timeCmp(self(v), other))
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

// localtimeLoc resolves the optional offset argument of localtime / getlocal:
// none → the local zone, else the same utc_offset forms Time.new accepts
// (String or Integer seconds).
func (vm *VM) localtimeLoc(args []object.Value) *stdtime.Location {
	if len(args) == 0 {
		return stdtime.Local
	}
	return vm.newTimeOffset(args[0])
}

// timeAt implements Time.at(time, subsec = nil, unit = :microsecond, in:).
func (vm *VM) timeAt(args []object.Value) *Time {
	zone, pos := timeZoneKw(args)
	if len(pos) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
	}
	var loc *stdtime.Location
	if zone != nil {
		loc = vm.newTimeOffset(zone)
	}
	var t stdtime.Time
	switch a := pos[0].(type) {
	case *Time:
		t = a.t
	default:
		sec, ns := splitSeconds(numFloat(pos[0]))
		if len(pos) >= 2 {
			ns += int64(numFloat(pos[1]) * float64(unitNanos(pos, 2)))
		}
		t = stdtime.Unix(sec, ns)
	}
	if loc == nil {
		loc = stdtime.UTC
	} else {
		t = t.In(loc)
		return &Time{t: t}
	}
	// A Time source keeps its own zone; a numeric source defaults to UTC.
	if _, isTime := pos[0].(*Time); isTime {
		return &Time{t: t}
	}
	return &Time{t: t.In(loc)}
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

// timeNew implements Time.new: no positional args → now (optionally zoned), else
// year[,mon,day,hour,min,sec,zone] with an optional in: keyword overriding the
// positional zone.
func (vm *VM) timeNew(args []object.Value) *Time {
	kwZone, pos := timeZoneKw(args)
	if len(pos) == 0 {
		n := vm.nowInstant()
		if kwZone != nil {
			n = n.In(vm.newTimeOffset(kwZone))
		}
		return &Time{t: n}
	}
	loc := stdtime.Local
	if len(pos) >= 7 {
		loc = vm.newTimeOffset(pos[6])
	}
	if kwZone != nil {
		loc = vm.newTimeOffset(kwZone)
	}
	return vm.buildTime(pos, 0, loc)
}

// newTimeOffset resolves Time.new's utc_offset / in: argument to a location: a
// String in "(+|-)HH[:MM[:SS]]", "UTC"/"Z" or single military-letter form; or an
// Integer number of seconds in (-86400, 86400). Anything else is a TypeError, as
// MRI reports for an object it cannot read as an exact number.
func (vm *VM) newTimeOffset(v object.Value) *stdtime.Location {
	switch x := v.(type) {
	case *object.String:
		return parseUtcOffset(x.Str())
	case object.Integer:
		return fixedOffsetLoc(int(x))
	case *object.Bignum:
		raise("ArgumentError", "utc_offset out of range")
	}
	raise("TypeError", "can't convert %s into an exact number", vm.classOf(v).name)
	return nil
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
// ArgumentError for any form it does not recognise.
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
	if hh > 23 || mm > 59 || ss > 59 {
		raiseBadOffset(s)
	}
	return fixedOffsetLoc(sign * (hh*3600 + mm*60 + ss))
}

func raiseBadOffset(s string) {
	raise("ArgumentError",
		"\"+HH:MM\", \"-HH:MM\", \"UTC\" or \"A\"..\"I\",\"K\"..\"Z\" expected for utc_offset: %s", s)
}

// timeFromCalendar implements Time.utc / Time.local: year[,mon,day,hour,min,sec,
// usec] in the given location.
func (vm *VM) timeFromCalendar(args []object.Value, loc *stdtime.Location) *Time {
	usec := 0.0
	if len(args) >= 7 {
		usec = numFloat(args[6])
	}
	return vm.buildTime(args, usec, loc)
}

// buildTime assembles a Time from calendar parts (with usec added in), validating
// each field's range the way MRI does before handing normalised parts to Go's
// time.Date.
func (vm *VM) buildTime(pos []object.Value, usec float64, loc *stdtime.Location) *Time {
	year := vm.timeInt(pos[0])
	month := vm.partOr(pos, 1, 1)
	day := vm.partOr(pos, 2, 1)
	hour := vm.partOr(pos, 3, 0)
	min := vm.partOr(pos, 4, 0)
	secF := 0.0
	if len(pos) >= 6 {
		secF = numFloat(pos[5])
	}
	sec := int(secF)
	ns := int64((secF-float64(sec))*1e9) + int64(usec*1000)

	// MRI range-checks each field, then normalises overflow (Feb 30 → Mar 2,
	// hour 24 → next day, sec 60 → next minute) exactly as Go's time.Date does.
	checkRange("mon", month, 1, 12)
	checkRange("mday", day, 1, 31)
	checkRange("hour", hour, 0, 24)
	checkRange("min", min, 0, 59)
	checkRange("sec", sec, 0, 60)
	return &Time{t: stdtime.Date(year, stdtime.Month(month), day, hour, min, sec, int(ns), loc)}
}

// partOr returns the integer calendar part at index i, or def when it is absent
// or nil (MRI treats a nil calendar field as "use the default").
func (vm *VM) partOr(pos []object.Value, i, def int) int {
	if len(pos) <= i || object.IsNil(pos[i]) {
		return def
	}
	return vm.timeInt(pos[i])
}

// checkRange raises MRI's "<field> out of range" ArgumentError when v is outside
// [lo, hi].
func checkRange(field string, v, lo, hi int) {
	if v < lo || v > hi {
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

// timeOp implements the Time operator fast path reached from binary(): t + secs
// shifts forward, t - secs shifts back, and t - other yields the Float seconds
// between the two instants. A non-Time, non-numeric right operand raises via
// numFloat / timeSeconds.
func timeOp(op bytecode.Op, a *Time, b object.Value) object.Value {
	switch op {
	case bytecode.OpAdd:
		return timeShift(a, numFloat(b))
	case bytecode.OpSub:
		if other, ok := b.(*Time); ok {
			d := a.t.Sub(other.t)
			return object.Float(d.Seconds())
		}
		return timeShift(a, -numFloat(b))
	}
	return raise("NoMethodError", "undefined method '%s' for a Time", op)
}

// timeShift shifts a Time forward by sec seconds (which may be fractional),
// preserving its location.
func timeShift(t *Time, sec float64) object.Value {
	whole, ns := splitSeconds(sec)
	return &Time{t: t.t.Add(stdtime.Duration(whole)*stdtime.Second + stdtime.Duration(ns)*stdtime.Nanosecond)}
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
			b.WriteString(strftime(t, exp))
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

// zoneName renders %Z: the zone's name, or its numeric offset when it is a bare
// fixed-offset zone.
func zoneName(tm stdtime.Time) string {
	name, off := tm.Zone()
	if name == "" {
		return signedOffset(off, ":")
	}
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
