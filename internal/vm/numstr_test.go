package vm_test

import (
	"strings"
	"testing"
)

// TestStringToR covers String#to_r: leading-whitespace skip, trailing-garbage
// tolerance, sign handling, decimal points, exponents, underscores, the
// numerator/denominator slash, and the Rational(0,1) fallback — every value
// asserted against MRI 4.0.5.
func TestStringToR(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p "".to_r`, "(0/1)\n"},
		{`p "2".to_r`, "(2/1)\n"},
		{`p "1765".to_r`, "(1765/1)\n"},
		{`p "2 foo".to_r`, "(2/1)\n"}, // trailing garbage ignored
		{`p " 2".to_r`, "(2/1)\n"},    // leading space ignored
		{`p "  1765, ".to_r`, "(1765/1)\n"},
		{`p "glark".to_r`, "(0/1)\n"}, // unparseable → 0/1
		{`p "a1765".to_r`, "(0/1)\n"}, // non-numeric leading char
		{`p "-20".to_r`, "(-20/1)\n"}, // leading minus
		{`p "+20".to_r`, "(20/1)\n"},  // leading plus
		{`p ".9".to_r`, "(9/10)\n"},   // leading period IS a decimal point
		{`p "-.5".to_r`, "(-1/2)\n"},
		{`p ".".to_r`, "(0/1)\n"}, // a lone period is not a number
		{`p "3.33".to_r`, "(333/100)\n"},
		{`p "-3.33".to_r`, "(-333/100)\n"},
		{`p "190_22".to_r`, "(19022/1)\n"}, // underscores between digits
		{`p "-190_22.7".to_r`, "(-190227/10)\n"},
		{`p "12__3".to_r`, "(12/1)\n"},  // doubled underscore ends the run
		{`p "1.5e2".to_r`, "(150/1)\n"}, // exponent
		{`p "1e3".to_r`, "(1000/1)\n"},
		{`p "1e-2".to_r`, "(1/100)\n"}, // negative exponent
		{`p "1e+2".to_r`, "(100/1)\n"}, // signed positive exponent
		{`p "1e".to_r`, "(1/1)\n"},     // 'e' with no exponent digits
		{`p "20/3".to_r`, "(20/3)\n"},  // slash separates numerator/denominator
		{`p " -19.10/3".to_r`, "(-191/30)\n"},
		{`p "5/2".to_r`, "(5/2)\n"},
		{`p "5/".to_r`, "(5/1)\n"}, // slash with no denominator digits
		{`p String.new.to_r`, "(0/1)\n"},
		{`p "3/4".to_r.class`, "Rational\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestStringToRZeroDenominator: "1/0" raises ZeroDivisionError even for the
// lenient String#to_r.
func TestStringToRZeroDenominator(t *testing.T) {
	if err := runErr(t, `"1/0".to_r`); err == nil || !strings.Contains(err.Error(), "ZeroDivisionError") {
		t.Errorf("got %v want ZeroDivisionError", err)
	}
}

// TestStringToC covers String#to_c across the real, imaginary, a+bi, polar and
// underscore forms, the i/I/j/J units, and the Complex(0,0) fallback.
func TestStringToC(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p "5".to_c`, "(5+0i)\n"},
		{`p "2i".to_c`, "(0+2i)\n"},
		{`p "i".to_c`, "(0+1i)\n"}, // bare unit → 1
		{`p "-i".to_c`, "(0-1i)\n"},
		{`p "35i".to_c`, "(0+35i)\n"},
		{`p "-29i".to_c`, "(0-29i)\n"},
		{`p "79+4i".to_c`, "(79+4i)\n"},
		{`p "79-4i".to_c`, "(79-4i)\n"},
		{`p "79+i".to_c`, "(79+1i)\n"}, // bare unit imaginary
		{`p "79-i".to_c`, "(79-1i)\n"},
		{`p "79+4I".to_c`, "(79+4i)\n"}, // I / j / J units
		{`p "79+4j".to_c`, "(79+4i)\n"},
		{`p "79+4J".to_c`, "(79+4i)\n"},
		{`p "2e3+4i".to_c`, "(2000.0+4i)\n"}, // exponent real
		{`p "4+2e3i".to_c`, "(4+2000.0i)\n"}, // exponent imaginary
		{`p "2.3".to_c`, "(2.3+0i)\n"},       // float → Float component
		{`p "4+2.3i".to_c`, "(4+2.3i)\n"},
		{`p "2/3".to_c`, "((2/3)+0i)\n"}, // fraction → Rational component
		{`p "-2/3".to_c`, "((-2/3)+0i)\n"},
		{`p "4+2/3i".to_c`, "(4+(2/3)*i)\n"},
		{`p "4/2".to_c`, "((2/1)+0i)\n"},    // a fraction stays a ratio even when whole
		{`p "5/i".to_c`, "(5+0i)\n"},        // slash with no denominator → integer, rest ignored
		{`p "+5".to_c`, "(5+0i)\n"},         // leading plus on the real part
		{`p "7_9+4_0i".to_c`, "(79+40i)\n"}, // underscores
		{`p "5+3_1i".to_c`, "(5+31i)\n"},
		{`p "5+3__1i".to_c`, "(5+0i)\n"}, // doubled underscore breaks the imaginary term
		{`p "12_3".to_c`, "(123+0i)\n"},
		{`p "12__3".to_c`, "(12+0i)\n"},
		{`p "ruby".to_c`, "(0+0i)\n"},     // unparseable → 0+0i
		{`p "NaN".to_c`, "(0+0i)\n"},      // 'N' is not an imaginary unit
		{`p "Infinity".to_c`, "(0+1i)\n"}, // leading 'I' is the imaginary unit
		{`p "-Infinity".to_c`, "(0-1i)\n"},
		{`p "79+4iruby".to_c`, "(79+4i)\n"}, // trailing garbage ignored
		{`p "  79+4i".to_c`, "(79+4i)\n"},   // leading whitespace ignored
		{`p "5@x".to_c`, "(5+0i)\n"},        // '@' with no valid angle falls back to the real
		{`p "5".to_c.class`, "Complex\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestStringToCPolar checks the polar "m@a" form parses to Complex.polar(m, a);
// the exact float values are libm-dependent so only the near value is asserted.
func TestStringToCPolar(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p "79@4".to_c == Complex.polar(79, 4)`, "true\n"},
		{`p "-79@4".to_c == Complex.polar(-79, 4)`, "true\n"},
		{`p "79@-4".to_c == Complex.polar(79, -4)`, "true\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestStringToCZeroDenominator: a zero denominator in a complex literal raises
// ZeroDivisionError, as MRI does.
func TestStringToCZeroDenominator(t *testing.T) {
	if err := runErr(t, `"2/0i".to_c`); err == nil || !strings.Contains(err.Error(), "ZeroDivisionError") {
		t.Errorf("got %v want ZeroDivisionError", err)
	}
}

// TestKernelRationalString covers the String argument forms of Kernel#Rational,
// which use String#to_r's grammar but strictly (no trailing garbage) and scale a
// two-string call.
func TestKernelRationalString(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Rational("1/3")`, "(1/3)\n"},
		{`p Rational("0.3")`, "(3/10)\n"},
		{`p Rational("2")`, "(2/1)\n"},
		{`p Rational(".52")`, "(13/25)\n"},
		{`p Rational("3.33")`, "(333/100)\n"},
		{`p Rational("  3/4  ")`, "(3/4)\n"}, // surrounding whitespace allowed
		{`p Rational(".52", ".6")`, "(13/15)\n"},
		{`p Rational("1/3", "2/5")`, "(5/6)\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKernelRationalCoercions covers the non-String, non-plain-numeric argument
// paths of Kernel#Rational: a Complex with a zero imaginary part, a #to_r object,
// and a #to_int-only object.
func TestKernelRationalCoercions(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Rational(Complex(4, 0))`, "(4/1)\n"},
		{`p Rational(1, Complex(2, 0))`, "(1/2)\n"},
		{`o = Object.new; def o.to_r; Rational(3, 7); end; p Rational(o)`, "(3/7)\n"},
		{`o = Object.new; def o.to_int; 5; end; p Rational(o)`, "(5/1)\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKernelRationalErrors covers the raising argument paths of Kernel#Rational.
func TestKernelRationalErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Rational("")`, "ArgumentError"},
		{`Rational("glark")`, "ArgumentError"},
		{`Rational("2 foo")`, "ArgumentError"}, // trailing garbage rejected
		{`Rational("1/0")`, "ZeroDivisionError"},
		{`Rational(Complex(1, 2))`, "RangeError"},
		{`Rational([])`, "TypeError"},
		{`Rational(nil)`, "TypeError"},
		{`Rational(:sym)`, "TypeError"},
		{`Rational(Float::INFINITY)`, "FloatDomainError"},
		{`o = Object.new; def o.to_r; [1]; end; Rational(o)`, "can't convert Object to Rational (Object#to_r gives Array)"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestKernelRationalExceptionFalse: `exception: false` yields nil instead of
// raising, including when the swallowed error comes from a #to_r / #to_int call.
func TestKernelRationalExceptionFalse(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Rational("abc", exception: false)`, "nil\n"},
		{`p Rational(:sym, exception: false)`, "nil\n"},
		{`p Rational(:sym, 1, exception: false)`, "nil\n"},
		{`o = Object.new; def o.to_r; raise; end; p Rational(o, exception: false)`, "nil\n"},
		{`o = Object.new; def o.to_int; raise; end; p Rational(o, exception: false)`, "nil\n"},
		{`p Rational(1, 2, exception: false)`, "(1/2)\n"}, // success path with the keyword
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKernelComplexString covers the String argument form of Kernel#Complex
// (strict) and its error / exception: paths.
func TestKernelComplexString(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Complex("9")`, "(9+0i)\n"},
		{`p Complex("79+4i")`, "(79+4i)\n"},
		{`p Complex("2/3")`, "((2/3)+0i)\n"},
		{`p Complex("i")`, "(0+1i)\n"},
		{`p Complex("1+2i")`, "(1+2i)\n"},
		{`p Complex("bad", exception: false)`, "nil\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Complex("ruby")`, "ArgumentError"},
		{`Complex("79+4iruby")`, "ArgumentError"}, // trailing garbage rejected
		{`Complex(nil)`, "TypeError"},
		{`Complex(1, nil)`, "TypeError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestNumericToC covers Integer/Float/Rational#to_c (real number wrapped as
// Complex(self, 0)) and Numeric#i (the pure-imaginary Complex(0, self)).
func TestNumericToC(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p 1.to_c`, "(1+0i)\n"},
		{`p 1.5.to_c`, "(1.5+0i)\n"},
		{`p Rational(1, 2).to_c`, "((1/2)+0i)\n"},
		{`p 1.to_c.class`, "Complex\n"},
		{`p 34.i.instance_of?(Complex)`, "true\n"},
		{`p 7342.i.real`, "0\n"},
		{`p 62.81.i.imag`, "62.81\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestComplexUndefined covers the methods a Complex does NOT answer: the sign
// predicates, the Comparable ordering operators (Complex keeps <=> but is not
// Comparable), and the pure-imaginary constructor #i — each a NoMethodError.
func TestComplexUndefined(t *testing.T) {
	for _, m := range []string{"positive?", "negative?", "i", "<", "<=", ">", ">=", "clamp", "between?"} {
		src := `p Complex(1, 2).respond_to?(:` + m + `)`
		if got := eval(t, src); got != "false\n" {
			t.Errorf("Complex responds to %q: got %q", m, got)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Complex(1, 2) < 3`, "NoMethodError"},
		{`Complex(2, 0) < 3`, "NoMethodError"},
		{`Complex(1, 2).clamp(0, 5)`, "NoMethodError"},
		{`Complex(1, 2).i`, "NoMethodError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
	// <=> and real? still work (Complex is not Comparable but keeps these).
	for _, c := range []struct{ src, want string }{
		{`p(Complex(2, 0) <=> 3)`, "-1\n"},
		{`p(Complex(1, 2) <=> Complex(3, 4))`, "nil\n"},
		{`p Complex(1, 2).real?`, "false\n"},
		{`p 5.real?`, "true\n"},
		{`p 1.5.real?`, "true\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestComplexPolarComplexArgs: Complex.polar accepts arguments that are Complex
// with a zero imaginary part, and rejects a Complex with a non-zero one.
func TestComplexPolarComplexArgs(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Complex.polar(2+0i, 0+0i).real.real?`, "true\n"},
		{`p Complex.polar(3+0i).imag.round(6)`, "0.0\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Complex.polar(Complex(1, 2))`, "TypeError"},
		{`Complex.polar(1, Complex(1, 2))`, "TypeError"},
		{`Complex.polar(nil)`, "TypeError"},
		{`Complex.polar(1, nil)`, "TypeError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
