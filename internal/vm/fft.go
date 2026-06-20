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
	return &object.Array{Elems: out}
}

// floatArray marshals []float64 back into a Ruby Array of Float.
func floatArray(fs []float64) object.Value {
	out := make([]object.Value, len(fs))
	for i, f := range fs {
		out[i] = object.Float(f)
	}
	return &object.Array{Elems: out}
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

// registerFFT installs the FFT module and its transform functions.
func (vm *VM) registerFFT() {
	mod := newClass("FFT", nil)
	mod.isModule = true
	vm.consts["FFT"] = mod

	def := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	def("fft", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return complexArray(gofft.FFT(complexSlice(args[0])))
	})
	def("ifft", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return complexArray(gofft.IFFT(complexSlice(args[0])))
	})
	def("rfft", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return complexArray(gofft.RFFT(floatSlice(args[0])))
	})
	def("irfft", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		n := int(intArg(args[1]))
		return floatArray(gofft.IRFFT(complexSlice(args[0]), n))
	})
	def("fftfreq", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return floatArray(gofft.FFTFreq(int(intArg(args[0])), sampleSpacing(args, 1)))
	})
	def("rfftfreq", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return floatArray(gofft.RFFTFreq(int(intArg(args[0])), sampleSpacing(args, 1)))
	})
}
