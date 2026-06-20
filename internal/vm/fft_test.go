package vm_test

import (
	"strings"
	"testing"
)

// TestFFT covers the FFT module binding of github.com/go-fft/fft: the forward and
// inverse complex and real transforms (with round-trips), the frequency-bin
// helpers, real and Complex inputs, and the optional sample-spacing argument.
// Values follow the numpy.fft conventions.
func TestFFT(t *testing.T) {
	cases := []struct{ src, want string }{
		// Forward FFT of a real impulse → a flat spectrum.
		{`p FFT.fft([1, 0, 0, 0])`, "[(1.0+0.0i), (1.0+0.0i), (1.0+0.0i), (1.0+0.0i)]\n"},
		// Complex inputs are accepted: a 2-point FFT is [c0+c1, c0-c1].
		{`p FFT.fft([Complex(1, 1), Complex(2, -1)])`, "[(3.0+0.0i), (-1.0+2.0i)]\n"},
		// ifft(fft(x)) ≈ x.
		{`p FFT.ifft(FFT.fft([1, 2, 3, 4])).map { |c| c.real.round }`, "[1, 2, 3, 4]\n"},
		// Real FFT keeps N/2+1 bins; check the real parts (Nyquist carries a tiny
		// imaginary residual).
		{`p FFT.rfft([1.0, 2.0, 3.0, 4.0]).map { |c| c.real.round }`, "[10, -2, -2]\n"},
		// irfft(rfft(x), n) ≈ x.
		{`p FFT.irfft(FFT.rfft([1.0, 2.0, 3.0, 4.0]), 4).map { |x| x.round }`, "[1, 2, 3, 4]\n"},
		// Frequency bins (numpy.fft.fftfreq / rfftfreq), default and explicit d.
		{`p FFT.fftfreq(4)`, "[0.0, 0.25, -0.5, -0.25]\n"},
		{`p FFT.fftfreq(4, 2.0)`, "[0.0, 0.125, -0.25, -0.125]\n"},
		{`p FFT.rfftfreq(4)`, "[0.0, 0.25, 0.5]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFFTMultiDimAndWindows covers the N-dimensional / 2-D transforms (via
// round-trips and the fft2≡fftn identity) and the window functions (against
// known numpy/scipy values).
func TestFFTMultiDimAndWindows(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p FFT.ifft2(FFT.fft2([1, 2, 3, 4, 5, 6], 2, 3), 2, 3).map { |c| c.real.round(6) }`, "[1.0, 2.0, 3.0, 4.0, 5.0, 6.0]\n"},
		{`p FFT.ifftn(FFT.fftn([1, 2, 3, 4], [2, 2]), [2, 2]).map { |c| c.real.round(6) }`, "[1.0, 2.0, 3.0, 4.0]\n"},
		{`p FFT.fft2([1, 2, 3, 4, 5, 6], 2, 3) == FFT.fftn([1, 2, 3, 4, 5, 6], [2, 3])`, "true\n"},
		{`p FFT.hann(4).map { |x| x.round(6) }`, "[0.0, 0.75, 0.75, 0.0]\n"},
		{`p FFT.bartlett(5).map { |x| x.round(6) }`, "[0.0, 0.5, 1.0, 0.5, 0.0]\n"},
		{`p FFT.hamming(1)`, "[1.0]\n"},
		{`p FFT.blackman(1)`, "[1.0]\n"},
		{`p FFT.blackman_harris(1)`, "[1.0]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFFTErrors covers the marshalling guards: a non-Array argument, a
// non-numeric element (complex and real paths), a non-numeric spacing, and the
// shape validation (empty, non-positive dimension, product mismatch).
func TestFFTErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`FFT.fft(5)`, "TypeError"},
		{`FFT.fft([1, "x"])`, "TypeError"},
		{`FFT.rfft([1.0, "x"])`, "TypeError"},
		{`FFT.fftfreq(4, "x")`, "TypeError"},
		{`FFT.fftn([1], [])`, "ArgumentError"},        // empty shape
		{`FFT.fftn([1], [0])`, "ArgumentError"},       // non-positive dimension
		{`FFT.fft2([1, 2, 3], 2, 2)`, "ArgumentError"}, // product != data length
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
