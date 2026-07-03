package vm

import (
	"strconv"
	"strings"
	stdtime "time"

	goresult "github.com/go-composites/result/src"
	gotime "github.com/go-composites/time/src"
	goduration "github.com/go-composites/time/src/duration"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// nowUnix is the seam for Time.now's only source of non-determinism — Go's wall
// clock — so tests can pin it. go-composites/time deliberately omits Now().
var nowUnix = func() int64 { return stdtime.Now().Unix() }

// Time binds github.com/go-composites/time — extending the go-composites
// consumer story begun by Set — into Ruby. A go-composites Time wraps an
// instant (Go's time.Time under the hood) and is deterministic by construction:
// it has no Now(), so the only non-deterministic constructor, Time.now, is built
// here from Go's stdlib time.Now().Unix() and fed to the composite's FromUnix —
// the non-determinism is the caller's, kept out of go-composites/time. Durations
// for the +/- arithmetic come from the composite's Duration sub-package.
//
// Time holds the go-composites Time.Interface directly (mirroring how Set holds
// its go-composites Set), so every Ruby instant is just a thin shell over the
// composite value and all behaviour flows through that one interface.

// Time is the Ruby wrapper around a go-composites Time.
type Time struct {
	t gotime.Interface
}

// repr renders MRI-ish "2026-06-21 12:00:00 +0000" from the composite instant.
func (t *Time) repr() string { return t.t.Format("2006-01-02 15:04:05 -0700") }

func (t *Time) ToS() string     { return t.repr() }
func (t *Time) Inspect() string { return t.repr() }
func (t *Time) Truthy() bool    { return true }

// timeArg asserts an argument is a Time, raising TypeError otherwise.
func timeArg(v object.Value) *Time {
	t, ok := object.KindOK[*Time](v)
	if !ok {
		raise("TypeError", "value must be a Time")
	}
	return t
}

// timeSeconds marshals a Ruby Integer or Float to a whole number of seconds,
// raising TypeError for anything else (mirroring the Integer/Float ↔ int64
// marshalling the task calls for).
func timeSeconds(v object.Value) int64 {
	{
		__sw175 := v
		switch {
		case object.IsInt(__sw175):
			n := object.AsInteger(__sw175)
			_ = n
			return int64(n)
		case object.IsFloat(__sw175):
			n := object.AsFloatV(__sw175)
			_ = n
			return int64(n)
		}
	}
	raise("TypeError", "no implicit conversion of %s into seconds", v.Inspect())
	return 0
}

// payloadTime unwraps a go-composites Result whose payload is a Time, raising a
// Ruby ArgumentError carrying the composite's Error message when the parse
// failed (so a malformed input is a Ruby exception, not a Go panic).
func payloadTime(r goresult.Interface) *Time {
	if r.HasError() {
		raise("ArgumentError", "%s", r.Error().Message())
	}
	return &Time{t: r.Payload().(gotime.Interface)}
}

// registerTime installs the Time class, its class constructors and instance
// methods, all delegating to the go-composites Time.Interface.
func (vm *VM) registerTime() {
	vm.cTime = newClass("Time", vm.cObject)
	vm.consts["Time"] = object.Wrap(vm.cTime)

	// Time.at(seconds) → FromUnix. Accepts an Integer or Float (truncated).
	vm.cTime.smethods["at"] = &Method{name: "at", owner: vm.cTime,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.Wrap(&Time{t: gotime.FromUnix(timeSeconds(args[0]))})
		}}
	// Time.now → built here from Go's clock (go-composites/time has no Now() by
	// design) and handed to FromUnix; whole-second resolution.
	vm.cTime.smethods["now"] = &Method{name: "now", owner: vm.cTime,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Wrap(&Time{t: gotime.FromUnix(nowUnix())})
		}}
	// Time.parse(str) → Parse(RFC3339, str); raises on the error Result.
	vm.cTime.smethods["parse"] = &Method{name: "parse", owner: vm.cTime,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.Wrap(payloadTime(gotime.Parse(stdtime.RFC3339, strArg(args[0]))))
		}}
	// Time.strptime(str, fmt) → Parse(rubyLayout(fmt), str); raises on failure.
	vm.cTime.smethods["strptime"] = &Method{name: "strptime", owner: vm.cTime,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.Wrap(payloadTime(gotime.Parse(rubyLayout(strArg(args[1])), strArg(args[0]))))
		}}

	d := func(name string, fn NativeFn) { vm.cTime.define(name, fn) }
	self := func(v object.Value) *Time { return object.Kind[*Time](v) }

	d("to_i", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).t.ToUnix())
	})
	d("to_f", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.FloatValue(float64(object.Float(float64(self(v).t.ToUnix()))))
	})

	toSFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).repr()))
	}
	d("to_s", toSFn)
	d("inspect", toSFn)

	// strftime(fmt): translate the Ruby/strftime directives to a Go layout, then
	// delegate to the composite's Format.
	d("strftime", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).t.Format(rubyLayout(strArg(args[0])))))
	})

	// Field accessors, derived from the underlying instant via Format directives.
	field := func(layout string) NativeFn {
		return func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			n, _ := strconv.ParseInt(self(v).t.Format(layout), 10, 64)
			return object.IntValue(n)
		}
	}
	d("year", field("2006"))
	d("month", field("1"))
	d("mon", field("1"))
	d("day", field("2"))
	d("mday", field("2"))
	d("hour", field("15"))
	d("min", field("4"))
	d("sec", field("5"))

	// wday → day of week 0..6 (Sunday=0), derived from the instant by formatting
	// the weekday name ("Mon", … via the Mon directive) and mapping it to MRI's
	// numbering. Following the year/month/… accessors, this stays Format-driven.
	d("wday", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(weekday(self(v)))
	})
	// Weekday predicates: sunday? … saturday?, booleans off wday.
	weekdayPred := func(want int64) NativeFn {
		return func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.BoolValue(bool(object.Bool(weekday(self(v)) == want)))
		}
	}
	d("sunday?", weekdayPred(0))
	d("monday?", weekdayPred(1))
	d("tuesday?", weekdayPred(2))
	d("wednesday?", weekdayPred(3))
	d("thursday?", weekdayPred(4))
	d("friday?", weekdayPred(5))
	d("saturday?", weekdayPred(6))

	// utc / getutc → UTC (same instant, UTC location).
	utcFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&Time{t: self(v).t.UTC()})
	}
	d("utc", utcFn)
	d("getutc", utcFn)
	// gmtime converts the receiver to UTC. MRI mutates the receiver in place and
	// returns it; with our whole-second instants converting to a UTC instant is
	// equivalent, and serialization paths (report/storage YAML) only read it back.
	d("gmtime", utcFn)

	// POSIX time-value accessors. tv_sec is the whole-second Unix time (== to_i);
	// our instants carry whole-second resolution, so the sub-second parts are all
	// zero. These let Puppet's report summary (Time.now.tv_sec) and Time#to_yaml
	// emit a value without raising.
	d("tv_sec", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).t.ToUnix())
	})
	zeroFn := func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(0)
	}
	d("tv_usec", zeroFn)
	d("usec", zeroFn)
	d("tv_nsec", zeroFn)
	d("nsec", zeroFn)
	d("subsec", zeroFn)

	// zone → abbreviated zone name ("UTC", "CET", …).
	d("zone", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).t.Zone()))
	})

	// + seconds → Add(Duration). - seconds → Add(-Duration); - Time → seconds.
	// These mirror the operator fast path (timeOp) so `send(:+, n)` agrees with
	// the `t + n` syntax.
	d("+", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return timeOp(bytecode.OpAdd, self(v), args[0])
	})
	d("-", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return timeOp(bytecode.OpSub, self(v), args[0])
	})

	// Comparison: <=> via Before/After/Equal (nil for a non-Time, as in MRI),
	// and the boolean operators / == built on it.
	d("<=>", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := object.KindOK[*Time](args[0])
		if !ok {
			return object.NilVal()
		}
		return object.IntValue(timeCmp(self(v), other))
	})
	d("<", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).t.Before(timeArg(args[0]).t))))
	})
	d(">", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).t.After(timeArg(args[0]).t))))
	})
	d("<=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(!self(v).t.After(timeArg(args[0]).t))))
	})
	d(">=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(!self(v).t.Before(timeArg(args[0]).t))))
	})
	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := object.KindOK[*Time](args[0])
		if !ok {
			return object.BoolValue(bool(object.False))
		}
		return object.BoolValue(bool(object.Bool(self(v).t.Equal(other.t))))
	})
}

// timeOp implements the Time operator fast path reached from binary(): t + secs
// shifts forward by a Duration, t - secs shifts back, and t - other yields the
// whole seconds between the two instants (an Integer, as MRI gives a Float we
// truncate to seconds — the composite's Sub resolution). A non-Time, non-numeric
// right operand raises TypeError via timeSeconds.
func timeOp(op bytecode.Op, a *Time, b object.Value) object.Value {
	switch op {
	case bytecode.OpAdd:
		return timeShift(a, timeSeconds(b))
	case bytecode.OpSub:
		if other, ok := object.KindOK[*Time](b); ok {
			return object.IntValue(a.t.Sub(other.t).ToSeconds())
		}
		return timeShift(a, -timeSeconds(b))
	}
	return raise("NoMethodError", "undefined method '%s' for a Time", op)
}

// timeShift shifts a Time by sec seconds via the composite's Duration
// arithmetic. The non-null Add Result always carries a payload.
func timeShift(t *Time, sec int64) object.Value {
	r := t.t.Add(goduration.FromSeconds(sec))
	return object.Wrap(&Time{t: r.Payload().(gotime.Interface)})
}

// weekdayNum maps the abbreviated weekday name produced by the "Mon" Format
// directive to MRI's wday numbering (Sunday=0 … Saturday=6).
var weekdayNum = map[string]int64{
	"Sun": 0, "Mon": 1, "Tue": 2, "Wed": 3, "Thu": 4, "Fri": 5, "Sat": 6,
}

// weekday derives a Time's day-of-week (0..6, Sunday=0) from the underlying
// instant via the "Mon" Format directive, mirroring the year/month/… accessors.
func weekday(t *Time) int64 { return weekdayNum[t.t.Format("Mon")] }

// timeCmp returns -1/0/1 ordering two Times through Before/After/Equal.
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
	b, ok := object.KindOK[*Time](other)
	return ok && a.t.Equal(b.t)
}

// strftimeToGo maps Ruby/C strftime directives to the corresponding Go
// reference-time tokens, so strftime / strptime can drive the composite's
// Format / Parse (which speak Go layouts).
var strftimeToGo = map[byte]string{
	'Y': "2006",
	'y': "06",
	'm': "01",
	'd': "02",
	'e': "_2",
	'H': "15",
	'I': "03",
	'M': "04",
	'S': "05",
	'p': "PM",
	'P': "pm",
	'A': "Monday",
	'a': "Mon",
	'B': "January",
	'b': "Jan",
	'Z': "MST",
	'z': "-0700",
	'j': "002",
	'%': "%",
}

// rubyLayout converts a strftime format string ("%Y-%m-%d") into a Go layout
// ("2006-01-02"); unknown directives and literal text pass through verbatim.
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
