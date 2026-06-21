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
	t, ok := v.(*Time)
	if !ok {
		raise("TypeError", "value must be a Time")
	}
	return t
}

// timeSeconds marshals a Ruby Integer or Float to a whole number of seconds,
// raising TypeError for anything else (mirroring the Integer/Float ↔ int64
// marshalling the task calls for).
func timeSeconds(v object.Value) int64 {
	switch n := v.(type) {
	case object.Integer:
		return int64(n)
	case object.Float:
		return int64(n)
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
	vm.consts["Time"] = vm.cTime

	// Time.at(seconds) → FromUnix. Accepts an Integer or Float (truncated).
	vm.cTime.smethods["at"] = &Method{name: "at", owner: vm.cTime,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &Time{t: gotime.FromUnix(timeSeconds(args[0]))}
		}}
	// Time.now → built here from Go's clock (go-composites/time has no Now() by
	// design) and handed to FromUnix; whole-second resolution.
	vm.cTime.smethods["now"] = &Method{name: "now", owner: vm.cTime,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &Time{t: gotime.FromUnix(nowUnix())}
		}}
	// Time.parse(str) → Parse(RFC3339, str); raises on the error Result.
	vm.cTime.smethods["parse"] = &Method{name: "parse", owner: vm.cTime,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return payloadTime(gotime.Parse(stdtime.RFC3339, strArg(args[0])))
		}}
	// Time.strptime(str, fmt) → Parse(rubyLayout(fmt), str); raises on failure.
	vm.cTime.smethods["strptime"] = &Method{name: "strptime", owner: vm.cTime,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return payloadTime(gotime.Parse(rubyLayout(strArg(args[1])), strArg(args[0])))
		}}

	d := func(name string, fn NativeFn) { vm.cTime.define(name, fn) }
	self := func(v object.Value) *Time { return v.(*Time) }

	d("to_i", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).t.ToUnix())
	})
	d("to_f", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(float64(self(v).t.ToUnix()))
	})

	toSFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr())
	}
	d("to_s", toSFn)
	d("inspect", toSFn)

	// strftime(fmt): translate the Ruby/strftime directives to a Go layout, then
	// delegate to the composite's Format.
	d("strftime", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).t.Format(rubyLayout(strArg(args[0]))))
	})

	// Field accessors, derived from the underlying instant via Format directives.
	field := func(layout string) NativeFn {
		return func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			n, _ := strconv.ParseInt(self(v).t.Format(layout), 10, 64)
			return object.Integer(n)
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

	// utc / getutc → UTC (same instant, UTC location).
	utcFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &Time{t: self(v).t.UTC()}
	}
	d("utc", utcFn)
	d("getutc", utcFn)

	// zone → abbreviated zone name ("UTC", "CET", …).
	d("zone", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).t.Zone())
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
		other, ok := args[0].(*Time)
		if !ok {
			return object.NilV
		}
		return object.Integer(timeCmp(self(v), other))
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
	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*Time)
		if !ok {
			return object.False
		}
		return object.Bool(self(v).t.Equal(other.t))
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
		if other, ok := b.(*Time); ok {
			return object.Integer(a.t.Sub(other.t).ToSeconds())
		}
		return timeShift(a, -timeSeconds(b))
	}
	return raise("NoMethodError", "undefined method '%s' for a Time", op)
}

// timeShift shifts a Time by sec seconds via the composite's Duration
// arithmetic. The non-null Add Result always carries a payload.
func timeShift(t *Time, sec int64) object.Value {
	r := t.t.Add(goduration.FromSeconds(sec))
	return &Time{t: r.Payload().(gotime.Interface)}
}

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
	b, ok := other.(*Time)
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
