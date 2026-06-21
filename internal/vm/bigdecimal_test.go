package vm_test

import (
	"strings"
	"testing"
)

// TestBigDecimal covers Ruby BigDecimal (backed by
// github.com/go-composites/bigfloat — the third go-composites consumer after Set
// and Time): the Kernel#BigDecimal constructor over each accepted operand type,
// the arbitrary-precision arithmetic (+/-/*/ via Add/Sub/Mul/Div), the unary
// abs / -@, the conversions (to_f / to_s / inspect), and the ordering / equality
// operators — including the exact-decimal property (0.1 + 0.2 == 0.3) that a
// binary Float cannot represent.
func TestBigDecimal(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction from a String, parsed at full precision.
		{`p BigDecimal("1.5").class`, "BigDecimal\n"},
		{`puts BigDecimal("1.5")`, "1.5\n"},
		{`p BigDecimal("1.5").to_s`, "\"1.5\"\n"},
		{`p BigDecimal("1.5").inspect`, "\"1.5\"\n"},
		{`p [BigDecimal("1.5")]`, "[1.5]\n"}, // Go-level Inspect, via Array#inspect
		// Construction from an Integer / Bignum / Float / BigDecimal.
		{`puts BigDecimal(5)`, "5\n"},
		{`puts BigDecimal(10 ** 30)`, "1e+30\n"}, // Bignum, exact through its decimal string
		{`puts BigDecimal(2.5)`, "2.5\n"},
		{`puts BigDecimal(BigDecimal("3.5"))`, "3.5\n"}, // pass-through
		// The exact-decimal headline: 0.1 + 0.2 is exactly 0.3 (Float is not).
		{`puts (BigDecimal("0.1") + BigDecimal("0.2")).to_s`, "0.3\n"},
		{`p(BigDecimal("0.1") + BigDecimal("0.2") == BigDecimal("0.3"))`, "true\n"},
		{`p(0.1 + 0.2 == 0.3)`, "false\n"}, // contrast: binary Float
		// Arithmetic.
		{`puts (BigDecimal("1.5") - BigDecimal("0.25")).to_s`, "1.25\n"},
		{`puts (BigDecimal("2") * BigDecimal("3.5")).to_s`, "7\n"},
		{`puts (BigDecimal("10") / BigDecimal("4")).to_s`, "2.5\n"},
		// Mixed operands: an Integer / Float coerces to BigDecimal (BigDecimal wins).
		{`puts (BigDecimal("1.5") + 2).to_s`, "3.5\n"},
		{`puts (BigDecimal("1.5") + 0.5).to_s`, "2\n"},
		{`puts (BigDecimal("1.5") + BigDecimal("1000000000000000000000000")).to_s`, "1.0000000000000000000000015e+24\n"},
		// abs / unary -@.
		{`puts BigDecimal("-3.5").abs.to_s`, "3.5\n"},
		{`puts BigDecimal("3.5").abs.to_s`, "3.5\n"},
		{`puts (-BigDecimal("2.5")).to_s`, "-2.5\n"},
		{`puts BigDecimal("2.5").send(:-@).to_s`, "-2.5\n"}, // explicit -@ method
		// to_f.
		{`p BigDecimal("2.5").to_f`, "2.5\n"},
		// Comparison: <=> and the boolean operators (numeric operand coerces).
		{`p(BigDecimal("1") <=> BigDecimal("2"))`, "-1\n"},
		{`p(BigDecimal("2") <=> BigDecimal("1"))`, "1\n"},
		{`p(BigDecimal("1") <=> BigDecimal("1"))`, "0\n"},
		{`p(BigDecimal("1.5") <=> 2)`, "-1\n"},             // Integer operand coerces
		{`p(BigDecimal("1") <=> 10 ** 30)`, "-1\n"},        // Bignum operand coerces
		{`p(BigDecimal("0.5") <=> Rational(1, 2))`, "0\n"}, // Rational operand coerces
		{`p(BigDecimal("1.5") <=> 1.5)`, "0\n"},            // Float operand coerces
		{`p(BigDecimal("1") <=> "x")`, "nil\n"},            // non-numeric operand
		{`p(BigDecimal("1") < BigDecimal("2"))`, "true\n"},
		{`p(BigDecimal("2") < BigDecimal("1"))`, "false\n"},
		{`p(BigDecimal("2") > BigDecimal("1"))`, "true\n"},
		{`p(BigDecimal("1") > BigDecimal("2"))`, "false\n"},
		{`p(BigDecimal("1") <= BigDecimal("1"))`, "true\n"},
		{`p(BigDecimal("2") <= BigDecimal("1"))`, "false\n"},
		{`p(BigDecimal("1") >= BigDecimal("1"))`, "true\n"},
		{`p(BigDecimal("1") >= BigDecimal("2"))`, "false\n"},
		// Equality (operator routes through valueEqual; the explicit == method is
		// exercised too, with the numeric-coercion and non-numeric short-circuits).
		{`p(BigDecimal("1.5") == BigDecimal("1.5"))`, "true\n"},
		{`p(BigDecimal("1.5") == BigDecimal("2"))`, "false\n"},
		{`p(BigDecimal("2") == 2)`, "true\n"},               // numeric operand coerces
		{`p(BigDecimal("2") == 10 ** 30)`, "false\n"},       // Bignum operand coerces (unequal)
		{`p(BigDecimal("1.5") == "x")`, "false\n"},          // non-numeric operand
		{`p(2 == BigDecimal("2"))`, "true\n"},               // valueEqual, BigDecimal on the right
		{`p(BigDecimal("2") == BigDecimal("2"))`, "true\n"}, // valueEqual, BigDecimal both sides
		{`p BigDecimal("1.5").send(:==, BigDecimal("1.5"))`, "true\n"},
		{`p BigDecimal("1.5").send(:==, 42)`, "false\n"},
		// send agrees with the operator syntax (operator fast path via dispatch).
		{`puts BigDecimal("1.5").send(:+, BigDecimal("0.5")).to_s`, "2\n"},
		{`puts BigDecimal("1.5").send(:-, BigDecimal("0.5")).to_s`, "1\n"},
		{`puts BigDecimal("1.5").send(:*, BigDecimal("2")).to_s`, "3\n"},
		{`puts BigDecimal("3").send(:/, BigDecimal("2")).to_s`, "1.5\n"},
		// truthiness + class.
		{`p(BigDecimal("0") ? "y" : "n")`, "\"y\"\n"}, // every BigDecimal is truthy
		{`p BigDecimal("1").class`, "BigDecimal\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBigDecimalErrors covers the raising paths: a malformed string literal
// (whose composite error Result surfaces as a Ruby ArgumentError), a division by
// zero (surfaced as a Ruby ZeroDivisionError), a non-numeric operand to the
// arithmetic / ordering operators (TypeError via the coercion), an unsupported
// operator (NoMethodError), and a non-convertible constructor argument
// (TypeError).
func TestBigDecimalErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`BigDecimal("xyz")`, "ArgumentError"}, // bad string literal
		{`BigDecimal("1") / BigDecimal("0")`, "ZeroDivisionError"},
		{`BigDecimal("1") / 0`, "ZeroDivisionError"},           // coerced zero divisor
		{`BigDecimal("1") + "x"`, "TypeError"},                 // + non-numeric
		{`BigDecimal("1") - "x"`, "TypeError"},                 // - non-numeric
		{`BigDecimal("1") * "x"`, "TypeError"},                 // * non-numeric
		{`BigDecimal("1") / "x"`, "TypeError"},                 // / non-numeric
		{`BigDecimal("1") < "x"`, "TypeError"},                 // < non-numeric
		{`BigDecimal("1") > "x"`, "TypeError"},                 // > non-numeric
		{`BigDecimal("1") <= "x"`, "TypeError"},                // <= non-numeric
		{`BigDecimal("1") >= "x"`, "TypeError"},                // >= non-numeric
		{`BigDecimal("1") % BigDecimal("1")`, "NoMethodError"}, // unsupported operator
		{`BigDecimal([])`, "TypeError"},                        // non-convertible constructor arg
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
