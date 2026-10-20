// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"
)

// TestFFTChecksItsArity is half of #820; the Image half is in
// wave71_arity_image_test.go, which is tagged !rbgo_core because Image is the
// first of the gems the core build leaves out. FFT is in core, so this file is
// untagged and runs under both.
//
// Every FFT method indexed args[0..3] with no count check, so a missing
// argument surfaced as
//
//	FFT.fft()   ArgumentError: fft: runtime error: index out of range [0] with length 0
//
// a Go panic recovered and relabelled, where MRI says "wrong number of
// arguments (given 0, expected 1)".
//
// This is a CENSUS rather than a list of names, so a method added later is
// covered without being registered anywhere -- and it asserts the number
// WALKED as well as the empty result, so a census that reads nothing fails
// instead of passing.
func TestFFTChecksItsArity(t *testing.T) {
	src := `bad = []
n = 0
FFT.methods(false).sort.each do |m|
  n += 1
  begin
    FFT.send(m)
  rescue => e
    bad << m if e.message.include?("runtime error")
  end
end
puts n
puts bad.join(" ")`
	out := strings.SplitN(strings.TrimSpace(eval(t, src)), "\n", 2)
	if out[0] != "17" {
		t.Fatalf("walked %q FFT methods, want 17 -- a census that does not walk proves nothing", out[0])
	}
	if len(out) > 1 && strings.TrimSpace(out[1]) != "" {
		t.Errorf("these FFT methods still leak a Go runtime fault when called with no arguments: %s", out[1])
	}
}

// TestFFTArityMessages pins MRI's wording for each shape FFT has, because the
// count is only half of it: a reader rescuing ArgumentError and printing
// e.message has to be told what they passed wrongly, and "runtime error: index
// out of range" does not. The range forms are what an optional argument looks
// like to a caller -- numpy's sample spacing d, and spectrogram's.
func TestFFTArityMessages(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"FFT.fft", "wrong number of arguments (given 0, expected 1)"},
		{"FFT.irfft", "wrong number of arguments (given 0, expected 2)"},
		{"FFT.fft2", "wrong number of arguments (given 0, expected 3)"},
		{"FFT.fftfreq", "wrong number of arguments (given 0, expected 1..2)"},
		{"FFT.rfftfreq", "wrong number of arguments (given 0, expected 1..2)"},
		{"FFT.psd", "wrong number of arguments (given 0, expected 1..2)"},
		{"FFT.spectrogram", "wrong number of arguments (given 0, expected 4..5)"},
		{"FFT.hann", "wrong number of arguments (given 0, expected 1)"},
		{"FFT.fftn", "wrong number of arguments (given 0, expected 2)"},
		{"FFT.fft([1.0], 2)", "wrong number of arguments (given 2, expected 1)"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			src := "begin\n  " + tc.expr + "\n  puts \"NO ERROR\"\nrescue ArgumentError => e\n  puts e.message\nend"
			if got := strings.TrimSpace(eval(t, src)); got != tc.want {
				t.Errorf("%s raised %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

// TestFFTStillWorks is the control for wrapping every method: the checks must
// not have changed any answer. Each row was right before and has to stay so,
// including the optional spacing the new ranges allow.
func TestFFTStillWorks(t *testing.T) {
	src := `p FFT.fft([1.0, 2.0, 3.0, 4.0]).map { |c| c.real.round(6) }
p FFT.fftfreq(4).map { |f| f.round(4) }
p FFT.fftfreq(4, 0.5).map { |f| f.round(4) }
p FFT.hann(3).map { |f| f.round(6) }
p FFT.irfft(FFT.rfft([1.0, 2.0, 3.0, 4.0]), 4).map { |f| f.round(6) }
p FFT.psd([1.0, 2.0, 3.0, 4.0]).size
p FFT.psd([1.0, 2.0, 3.0, 4.0], 0.5).size`
	const want = "[10.0, -2.0, -2.0, -2.0]\n" +
		"[0.0, 0.25, -0.5, -0.25]\n" +
		"[0.0, 0.5, -1.0, -0.5]\n" +
		"[0.0, 1.0, 0.0]\n" +
		"[1.0, 2.0, 3.0, 4.0]\n" +
		"3\n3\n"
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
