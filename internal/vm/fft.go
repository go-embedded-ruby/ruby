package vm

import (
	gofft "github.com/go-fft/fft"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The FFT module is a thin, cgo-free binding of github.com/go-fft/fft, giving
// Ruby a numpy.fft-style transform with no native FFTW dependency. Complex
// spectra are returned as Ruby Complex values (see complex.go).

// toComplex128 converts a Ruby number (Integer/Float/Bignum/Complex) to a Go
// complex128; ok is false for a non-numeric value.
func toComplex128(v object.Value) (complex128, bool) {
	if c, ok := v.(*object.Complex); ok {
		return complex(complexFloat(c.Re), complexFloat(c.Im)), true
	}
	if f, ok := toFloat(v); ok {
		return complex(f, 0), true
	}
	return 0, false
}

// arrayArg asserts that an argument is an Array, raising TypeError otherwise.
func arrayArg(v object.Value) *object.Array {
	a, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Array", v.Inspect())
	}
	return a
}

// complexSlice marshals a Ruby Array of numbers into []complex128.
func complexSlice(v object.Value) []complex128 {
	a := arrayArg(v)
	out := make([]complex128, len(a.Elems))
	for i, e := range a.Elems {
		c, ok := toComplex128(e)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Complex", e.Inspect())
		}
		out[i] = c
	}
	return out
}

// floatSlice marshals a Ruby Array of real numbers into []float64.
func floatSlice(v object.Value) []float64 {
	a := arrayArg(v)
	out := make([]float64, len(a.Elems))
	for i, e := range a.Elems {
		f, ok := toFloat(e)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Float", e.Inspect())
		}
		out[i] = f
	}
	return out
}

// complexArray marshals []complex128 back into a Ruby Array of Complex.
func complexArray(cs []complex128) object.Value {
	out := make([]object.Value, len(cs))
	for i, c := range cs {
		out[i] = &object.Complex{Re: object.Float(real(c)), Im: object.Float(imag(c))}
	}
	return object.NewArrayFromSlice(out)
}

// floatArray marshals []float64 back into a Ruby Array of Float.
func floatArray(fs []float64) object.Value {
	out := make([]object.Value, len(fs))
	for i, f := range fs {
		out[i] = object.Float(f)
	}
	return object.NewArrayFromSlice(out)
}

// float2DArray marshals [][]float64 into a Ruby Array of Arrays of Float.
func float2DArray(rows [][]float64) object.Value {
	out := make([]object.Value, len(rows))
	for i, r := range rows {
		out[i] = floatArray(r)
	}
	return object.NewArrayFromSlice(out)
}

// sampleSpacing reads an optional trailing spacing argument (numpy's `d`),
// defaulting to 1.0.
func sampleSpacing(args []object.Value, idx int) float64 {
	if len(args) <= idx {
		return 1.0
	}
	d, ok := toFloat(args[idx])
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Float", args[idx].Inspect())
	}
	return d
}

// intSlice marshals a Ruby Array of integers into []int.
func intSlice(v object.Value) []int {
	a := arrayArg(v)
	out := make([]int, len(a.Elems))
	for i, e := range a.Elems {
		out[i] = int(intArg(e))
	}
	return out
}

// checkShape validates that shape is non-empty with positive dimensions whose
// product is n, raising ArgumentError otherwise — so go-fft's panic-on-bad-shape
// surfaces as a Ruby error rather than crashing the VM.
func checkShape(n int, shape []int) {
	if len(shape) == 0 {
		raise("ArgumentError", "empty shape")
	}
	prod := 1
	for _, d := range shape {
		if d <= 0 {
			raise("ArgumentError", "invalid shape dimension %d", d)
		}
		prod *= d
	}
	if prod != n {
		raise("ArgumentError", "shape product %d does not match data length %d", prod, n)
	}
}

// registerFFT installs the FFT module and its transform functions.
//
// Every method declares its arity AT the registration site, and def checks it
// before the body runs. That is deliberate rather than a check inside each
// closure: the bodies index args[0..3] directly, so a missing argument used to
// surface as "runtime error: index out of range" -- a recovered Go panic
// relabelled ArgumentError -- where MRI says "wrong number of arguments"
// (#820). Putting the counts in def's signature means a method added later
// cannot be registered without them.
func (vm *VM) registerFFT() {
	mod := newClass("FFT", nil)
	mod.isModule = true
	vm.consts["FFT"] = mod

	def := func(name string, min, max int, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			ndArgc(args, min, max)
			return fn(vm, self, args, blk)
		}}
	}

	def("fft", 1, 1, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return complexArray(gofft.FFT(complexSlice(args[0])))
	})
	def("ifft", 1, 1, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return complexArray(gofft.IFFT(complexSlice(args[0])))
	})
	def("rfft", 1, 1, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return complexArray(gofft.RFFT(floatSlice(args[0])))
	})
	def("irfft", 2, 2, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		n := int(intArg(args[1]))
		return floatArray(gofft.IRFFT(complexSlice(args[0]), n))
	})
	// The bin-frequency helpers take numpy's optional sample spacing d.
	def("fftfreq", 1, 2, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return floatArray(gofft.FFTFreq(int(intArg(args[0])), sampleSpacing(args, 1)))
	})
	def("rfftfreq", 1, 2, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return floatArray(gofft.RFFTFreq(int(intArg(args[0])), sampleSpacing(args, 1)))
	})

	// N-dimensional transforms over flat row-major data plus an explicit shape.
	def("fftn", 2, 2, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		data, shape := complexSlice(args[0]), intSlice(args[1])
		checkShape(len(data), shape)
		return complexArray(gofft.FFTN(data, shape))
	})
	def("ifftn", 2, 2, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		data, shape := complexSlice(args[0]), intSlice(args[1])
		checkShape(len(data), shape)
		return complexArray(gofft.IFFTN(data, shape))
	})
	// 2-D transforms over flat row-major data of shape rows×cols.
	def("fft2", 3, 3, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		data := complexSlice(args[0])
		rows, cols := int(intArg(args[1])), int(intArg(args[2]))
		checkShape(len(data), []int{rows, cols})
		return complexArray(gofft.FFT2(data, [2]int{rows, cols}))
	})
	def("ifft2", 3, 3, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		data := complexSlice(args[0])
		rows, cols := int(intArg(args[1])), int(intArg(args[2]))
		checkShape(len(data), []int{rows, cols})
		return complexArray(gofft.IFFT2(data, [2]int{rows, cols}))
	})

	// Window functions (numpy/scipy conventions): length n → []float64.
	window := func(name string, f func(int) []float64) {
		def(name, 1, 1, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return floatArray(f(int(intArg(args[0]))))
		})
	}
	window("hann", gofft.Hann)
	window("hamming", gofft.Hamming)
	window("blackman", gofft.Blackman)
	window("blackman_harris", gofft.BlackmanHarris)
	window("bartlett", gofft.Bartlett)

	// Spectral helpers: one-sided power spectral density and a windowed
	// spectrogram (an array of per-segment PSD frames).
	def("psd", 1, 2, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return floatArray(gofft.PSD(floatSlice(args[0]), sampleSpacing(args, 1)))
	})
	def("spectrogram", 4, 5, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		signal := floatSlice(args[0])
		segment, overlap := int(intArg(args[1])), int(intArg(args[2]))
		win := floatSlice(args[3])
		// Validate go-fft's constraints up front so a bad call raises a Ruby
		// error instead of panicking the VM.
		if segment <= 0 {
			raise("ArgumentError", "segment length must be positive")
		}
		if len(win) != segment {
			raise("ArgumentError", "window length must equal segment length")
		}
		if overlap < 0 || overlap >= segment {
			raise("ArgumentError", "overlap must satisfy 0 <= overlap < segment")
		}
		return float2DArray(gofft.Spectrogram(signal, segment, overlap, win, sampleSpacing(args, 4)))
	})
}
