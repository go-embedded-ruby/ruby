package vm

import (
	godate "github.com/go-composites/date/src"
	goresult "github.com/go-composites/result/src"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Date binds github.com/go-composites/date — the fourth real consumer of the
// go-composites family (after Set, Time and BigDecimal) — into Ruby. A
// go-composites Date is a calendar date (year/month/day, no time-of-day, pinned
// to midnight UTC under the hood), so it complements the VM's existing Time
// (an instant) with a pure calendar value, mirroring Ruby's own Date vs Time
// split. It is deterministic by construction: go-composites/date has no Today(),
// so every Date the VM holds is built from explicit values (FromYMD / Parse) or
// derived from one by day arithmetic.
//
// Date holds the go-composites date.Interface directly (mirroring how Time holds
// its go-composites Time and Set its go-composites Set), so every Ruby Date is a
// thin shell over the composite value and all behaviour flows through that one
// interface — construction (FromYMD / Parse, each a fallible Result), the field
// accessors (Year/Month/Day, Weekday), the ISO string (ToGoString), the day
// arithmetic (AddDays / DaysBetween) and the comparisons (Before/After/Equal).

// Date is the Ruby wrapper around a go-composites Date.
type Date struct {
	d godate.Interface
}

// repr renders the ISO "YYYY-MM-DD" calendar date via the composite's
// ToGoString — the same form MRI's Date#to_s / Date#inspect (sans the
// "#<Date: ...>" wrapper) presents.
func (d *Date) repr() string { return d.d.ToGoString() }

func (d *Date) ToS() string     { return d.repr() }
func (d *Date) Inspect() string { return d.repr() }
func (d *Date) Truthy() bool    { return true }

// dateArg asserts an argument is a Date, raising TypeError otherwise (the Date
// counterpart of timeArg / setArg).
func dateArg(v object.Value) *Date {
	d, ok := v.(*Date)
	if !ok {
		raise("TypeError", "value must be a Date")
	}
	return d
}

// dateDays marshals a Ruby Integer to a whole number of days, raising TypeError
// for anything else (the day-offset counterpart of timeSeconds; only an Integer
// is a meaningful day count, so a Float is rejected).
func dateDays(v object.Value) int {
	if n, ok := v.(object.Integer); ok {
		return int(n)
	}
	raise("TypeError", "no implicit conversion of %s into days", v.Inspect())
	return 0
}

// payloadDate unwraps a go-composites Result whose payload is a Date, raising a
// Ruby ArgumentError carrying the composite's Error message when the
// construction failed (so an invalid date / malformed parse is a Ruby
// exception, not a Go panic) — mirroring time.go's payloadTime.
func payloadDate(r goresult.Interface) *Date {
	if r.HasError() {
		raise("ArgumentError", "%s", r.Error().Message())
	}
	return &Date{d: r.Payload().(godate.Interface)}
}

// weekdayToWday maps the composite's English weekday name to Ruby's wday number
// (0 = Sunday … 6 = Saturday).
var weekdayToWday = map[string]int64{
	"Sunday":    0,
	"Monday":    1,
	"Tuesday":   2,
	"Wednesday": 3,
	"Thursday":  4,
	"Friday":    5,
	"Saturday":  6,
}

// dateOp implements the Date operator fast path reached from binary(): d + n
// shifts forward by n days, d - n shifts back, and d - other yields the whole
// number of days between the two dates (an Integer, as Ruby's Date#- gives a
// Rational we render as a plain day count — go-composites/date's DaysBetween
// resolution). A non-Date, non-Integer right operand raises TypeError via
// dateDays.
func dateOp(op bytecode.Op, a *Date, b object.Value) object.Value {
	switch op {
	case bytecode.OpAdd:
		return dateShift(a, dateDays(b))
	case bytecode.OpSub:
		if other, ok := b.(*Date); ok {
			// d - other: days from other to d (positive when d is later).
			return object.Integer(other.d.DaysBetween(a.d))
		}
		return dateShift(a, -dateDays(b))
	}
	return raise("NoMethodError", "undefined method '%s' for a Date", op)
}

// dateShift shifts a Date by n days via the composite's AddDays. The non-null
// AddDays Result always carries a payload.
func dateShift(d *Date, n int) object.Value {
	return &Date{d: d.d.AddDays(n).Payload().(godate.Interface)}
}

// dateCmp returns -1/0/1 ordering two Dates through Before/After (Equal
// otherwise).
func dateCmp(a, b *Date) int64 {
	switch {
	case a.d.Before(b.d):
		return -1
	case a.d.After(b.d):
		return 1
	default:
		return 0
	}
}

// dateEqual reports Date equality for valueEqual / the == operator fast path.
func dateEqual(a *Date, other object.Value) bool {
	b, ok := other.(*Date)
	return ok && a.d.Equal(b.d)
}

// registerDate installs the Date class, its class constructors and instance
// methods, all delegating to the go-composites date.Interface.
func (vm *VM) registerDate() {
	vm.cDate = newClass("Date", vm.cObject)
	vm.consts["Date"] = vm.cDate

	// Date.new(year, month, day) → FromYMD; an invalid calendar date (Feb 30,
	// month 13, …) surfaces the composite's error Result as a Ruby ArgumentError.
	vm.cDate.smethods["new"] = &Method{name: "new", owner: vm.cDate,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			y := int(intArg(args[0]))
			m := int(intArg(args[1]))
			day := int(intArg(args[2]))
			return payloadDate(godate.FromYMD(y, m, day))
		}}
	// Date.parse(str) → Parse(ISO "YYYY-MM-DD"); a non-ISO input surfaces the
	// composite's error Result as a Ruby ArgumentError. (MRI's Date.parse is
	// lenient; this binding is the documented ISO-only subset go-composites/date
	// supports.)
	vm.cDate.smethods["parse"] = &Method{name: "parse", owner: vm.cDate,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return payloadDate(godate.Parse(strArg(args[0])))
		}}

	d := func(name string, fn NativeFn) { vm.cDate.define(name, fn) }
	self := func(v object.Value) *Date { return v.(*Date) }

	// Field accessors. year, month/mon, day/mday come straight off the composite.
	d("year", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).d.Year())
	})
	monthFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).d.Month())
	}
	d("month", monthFn)
	d("mon", monthFn)
	dayFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).d.Day())
	}
	d("day", dayFn)
	d("mday", dayFn)

	// wday: Ruby's day-of-week index, 0 = Sunday … 6 = Saturday, derived from the
	// composite's English Weekday name. cwday is the ISO variant, 1 = Monday … 7
	// = Sunday.
	d("wday", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(weekdayToWday[self(v).d.Weekday()])
	})
	d("cwday", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		w := weekdayToWday[self(v).d.Weekday()]
		if w == 0 {
			return object.Integer(7) // Sunday is 7 in the ISO (cwday) numbering
		}
		return object.Integer(w)
	})
	// strftime is not implemented: go-composites/date exposes no format layouts
	// (only ToGoString / Weekday), so the full MRI strftime is out of reach
	// without reaching past the composite — left out rather than faked. The
	// English day name is available via #wday/#cwday (numeric) and the ISO string
	// via #to_s.

	toSFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr())
	}
	d("to_s", toSFn)
	d("inspect", toSFn)

	// + days → AddDays(n). - days → AddDays(-n); - Date → day count between.
	// These mirror the operator fast path (dateOp) so `send(:+, n)` agrees with
	// the `d + n` syntax.
	d("+", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return dateOp(bytecode.OpAdd, self(v), args[0])
	})
	d("-", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return dateOp(bytecode.OpSub, self(v), args[0])
	})

	// next_day(n=1) / prev_day(n=1): shift forward / back by n days (→ AddDays).
	dayOffset := func(sign int) NativeFn {
		return func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			n := 1
			if len(args) > 0 {
				n = dateDays(args[0])
			}
			return dateShift(self(v), sign*n)
		}
	}
	d("next_day", dayOffset(1))
	d("prev_day", dayOffset(-1))

	// leap? → IsLeapYear (a plain Boolean); yday → DayOfYear, the 1-based day of
	// the year (1–366), matching MRI's Date#leap? / Date#yday.
	d("leap?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).d.IsLeapYear())
	})
	d("yday", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).d.DayOfYear())
	})

	// next_month(n=1) / prev_month(n=1): shift forward / back by n calendar months
	// (→ AddMonths), with MRI's day-of-month normalisation (31 Jan >> 1 → 28 Feb).
	// AddMonths on a concrete Date never fails, so its Result always carries the
	// payload.
	monthShift := func(d *Date, n int) object.Value {
		return &Date{d: d.d.AddMonths(n).Payload().(godate.Interface)}
	}
	monthOffset := func(sign int) NativeFn {
		return func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			n := 1
			if len(args) > 0 {
				n = dateDays(args[0])
			}
			return monthShift(self(v), sign*n)
		}
	}
	d("next_month", monthOffset(1))
	d("prev_month", monthOffset(-1))

	// >> n / << n: MRI's month-shift operators. The VM has no shift opcode, so
	// these dispatch as ordinary method sends (like Integer#<< / Rational#**); the
	// right operand is an Integer month count (dateDays raises TypeError
	// otherwise). >> shifts forward, << shifts back.
	d(">>", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return monthShift(self(v), dateDays(args[0]))
	})
	d("<<", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return monthShift(self(v), -dateDays(args[0]))
	})

	// Comparison: <=> via Before/After/Equal (nil for a non-Date, as in MRI), and
	// the boolean operators / == built on it.
	d("<=>", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*Date)
		if !ok {
			return object.NilV
		}
		return object.Integer(dateCmp(self(v), other))
	})
	d("<", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).d.Before(dateArg(args[0]).d))
	})
	d(">", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).d.After(dateArg(args[0]).d))
	})
	d("<=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(!self(v).d.After(dateArg(args[0]).d))
	})
	d(">=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(!self(v).d.Before(dateArg(args[0]).d))
	})
	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*Date)
		if !ok {
			return object.False
		}
		return object.Bool(self(v).d.Equal(other.d))
	})
}
