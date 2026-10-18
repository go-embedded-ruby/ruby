// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"
)

// TestFFTAndImageCheckTheirArity closes #820.
//
// Every method of both modules indexed args[0..3] directly with no count
// check, so a missing argument surfaced as
//
//	FFT.fft()   ArgumentError: fft: runtime error: index out of range [0] with length 0
//
// a Go panic recovered and relabelled, where MRI says "wrong number of
// arguments (given 0, expected 1)". A census over rbgo found 31: all 17 of
// FFT's and 14 of Image's. NDArray was already clean because it uses ndArgc,
// which is what both now use.
//
// This is a CENSUS rather than a list of names, so a method added later is
// covered without being registered anywhere -- and it asserts the number
// WALKED as well as the empty result, so a census that reads nothing fails
// instead of passing.
//
// #save is the one method held out, because it writes a file; it is checked by
// name in TestFFTAndImageArityMessages below.
func TestFFTAndImageCheckTheirArity(t *testing.T) {
	src := `def census(recv, names, skip)
  bad = []
  n = 0
  names.sort.each do |m|
    next if skip.include?(m.to_s)
    n += 1
    begin
      recv.send(m)
    rescue => e
      bad << m if e.message.include?("runtime error")
    end
  end
  [n, bad]
end
a, abad = census(FFT, FFT.methods(false), [])
b, bbad = census(Image, Image.methods(false), [])
c, cbad = census(Image.new(2, 2), Image.instance_methods(false), ["save"])
puts [a, b, c].join(" ")
puts (abad + bbad + cbad).join(" ")`
	out := strings.SplitN(strings.TrimSpace(eval(t, src)), "\n", 2)
	if len(out) == 0 || out[0] != "17 3 34" {
		t.Fatalf("walked %q, want \"17 3 34\" (FFT module, Image module, Image instance) -- "+
			"a census that does not walk proves nothing", out[0])
	}
	if len(out) > 1 && strings.TrimSpace(out[1]) != "" {
		t.Errorf("these methods still leak a Go runtime fault when called with no arguments: %s", out[1])
	}
}

// TestFFTAndImageArityMessages pins MRI's wording for one method of each shape,
// because the count is only half of it: a reader rescuing ArgumentError and
// printing e.message has to be told what it passed wrongly, and "runtime error:
// index out of range" does not. The range forms (min..max) matter too -- they
// are what an optional argument looks like to a caller.
func TestFFTAndImageArityMessages(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		// FFT: fixed one, two and three arguments, then both range forms.
		{"FFT.fft", "wrong number of arguments (given 0, expected 1)"},
		{"FFT.irfft", "wrong number of arguments (given 0, expected 2)"},
		{"FFT.fft2", "wrong number of arguments (given 0, expected 3)"},
		{"FFT.fftfreq", "wrong number of arguments (given 0, expected 1..2)"},
		{"FFT.spectrogram", "wrong number of arguments (given 0, expected 4..5)"},
		{"FFT.hann", "wrong number of arguments (given 0, expected 1)"},
		{"FFT.fft([1.0], 2)", "wrong number of arguments (given 2, expected 1)"},
		// Image, module side.
		{"Image.new", "wrong number of arguments (given 0, expected 2)"},
		{"Image.load", "wrong number of arguments (given 0, expected 1)"},
		{"Image.decode", "wrong number of arguments (given 0, expected 1)"},
		// Image, instance side, including the no-argument and range forms.
		{"Image.new(2, 2).get", "wrong number of arguments (given 0, expected 2)"},
		{"Image.new(2, 2).set", "wrong number of arguments (given 0, expected 5..6)"},
		{"Image.new(2, 2).canny", "wrong number of arguments (given 0, expected 3)"},
		{"Image.new(2, 2).crop", "wrong number of arguments (given 0, expected 4)"},
		{"Image.new(2, 2).save", "wrong number of arguments (given 0, expected 1)"},
		{"Image.new(2, 2).width(1)", "wrong number of arguments (given 1, expected 0)"},
		{"Image.new(2, 2).grayscale(1)", "wrong number of arguments (given 1, expected 0)"},
		{"Image.new(2, 2).to_png(1)", "wrong number of arguments (given 1, expected 0)"},
		{"Image.new(2, 2).adjust_brightness", "wrong number of arguments (given 0, expected 1)"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			src := "begin\n  " + tc.expr + "\n  puts \"NO ERROR\"\nrescue ArgumentError => e\n  puts e.message\nend"
			if got := strings.TrimSpace(eval(t, src)); got != tc.want {
				t.Errorf("%s raised %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

// TestFFTAndImageStillWork is the control for wrapping every method: the checks
// must not have changed any answer. Each row is a call that was correct before
// and has to stay so, including the optional arguments the new ranges allow
// (#set's alpha, #fftfreq's spacing) and #to_s, which is ALIASED from #inspect's
// native function and so inherits whatever wrapper #inspect got.
func TestFFTAndImageStillWork(t *testing.T) {
	src := `p FFT.fft([1.0, 2.0, 3.0, 4.0]).map { |c| c.real.round(6) }
p FFT.fftfreq(4).map { |f| f.round(4) }
p FFT.fftfreq(4, 0.5).map { |f| f.round(4) }
p FFT.hann(3).map { |f| f.round(6) }
p FFT.irfft(FFT.rfft([1.0, 2.0, 3.0, 4.0]), 4).map { |f| f.round(6) }
i = Image.new(4, 4)
i.set(0, 0, 255, 0, 0)
p i.get(0, 0)
i.set(1, 1, 1, 2, 3, 128)
p i.get(1, 1)
p [i.width, i.height, i.grayscale.width, i.resize(2, 2).width, i.crop(0, 0, 2, 2).width]
p [i.adjust_brightness(1.0).width, i.gaussian_blur(0.5).width, i.canny(1.0, 0.1, 0.2).width]
p i.to_png.bytesize > 0
p i.to_s == i.inspect`
	const want = "[10.0, -2.0, -2.0, -2.0]\n" +
		"[0.0, 0.25, -0.5, -0.25]\n" +
		"[0.0, 0.5, -1.0, -0.5]\n" +
		"[0.0, 1.0, 0.0]\n" +
		"[1.0, 2.0, 3.0, 4.0]\n" +
		"[255, 0, 0, 255]\n" +
		"[1, 2, 3, 128]\n" +
		"[4, 4, 4, 2, 2]\n" +
		"[4, 4, 4]\n" +
		"true\n" +
		"true\n"
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
