// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	"strings"
	"testing"
)

// The Image half of #820. Tagged !rbgo_core, as image.go and image_test.go
// are: Image is the first of the gems the core build leaves out, so under
// -tags rbgo_core the constant does not exist and a test naming it would fail
// for the wrong reason. The FFT half is untagged and runs under both.

// TestImageChecksItsArity is the census over both of Image's faces -- the
// module methods and the instance ones. It asserts the number WALKED (3 and
// 34) as well as the empty result, so a census that reads nothing fails
// instead of passing. #save is held out, because it writes a file; it is
// checked by name in TestImageArityMessages.
func TestImageChecksItsArity(t *testing.T) {
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
a, abad = census(Image, Image.methods(false), [])
b, bbad = census(Image.new(2, 2), Image.instance_methods(false), ["save"])
puts [a, b].join(" ")
puts (abad + bbad).join(" ")`
	out := strings.SplitN(strings.TrimSpace(eval(t, src)), "\n", 2)
	if out[0] != "3 34" {
		t.Fatalf("walked %q, want \"3 34\" (Image module, Image instance) -- "+
			"a census that does not walk proves nothing", out[0])
	}
	if len(out) > 1 && strings.TrimSpace(out[1]) != "" {
		t.Errorf("these Image methods still leak a Go runtime fault when called with no arguments: %s", out[1])
	}
}

// TestImageArityMessages pins MRI's wording for each shape Image has: fixed
// counts, the 5..6 range of #set's optional alpha, and the zero-argument form
// a caller trips by passing something.
func TestImageArityMessages(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"Image.new", "wrong number of arguments (given 0, expected 2)"},
		{"Image.load", "wrong number of arguments (given 0, expected 1)"},
		{"Image.decode", "wrong number of arguments (given 0, expected 1)"},
		{"Image.new(2, 2).get", "wrong number of arguments (given 0, expected 2)"},
		{"Image.new(2, 2).set", "wrong number of arguments (given 0, expected 5..6)"},
		{"Image.new(2, 2).canny", "wrong number of arguments (given 0, expected 3)"},
		{"Image.new(2, 2).crop", "wrong number of arguments (given 0, expected 4)"},
		{"Image.new(2, 2).resize", "wrong number of arguments (given 0, expected 2)"},
		{"Image.new(2, 2).save", "wrong number of arguments (given 0, expected 1)"},
		{"Image.new(2, 2).adjust_brightness", "wrong number of arguments (given 0, expected 1)"},
		{"Image.new(2, 2).gaussian_blur", "wrong number of arguments (given 0, expected 1)"},
		{"Image.new(2, 2).width(1)", "wrong number of arguments (given 1, expected 0)"},
		{"Image.new(2, 2).grayscale(1)", "wrong number of arguments (given 1, expected 0)"},
		{"Image.new(2, 2).to_png(1)", "wrong number of arguments (given 1, expected 0)"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			src := "begin\n  " + tc.expr + "\n  puts \"NO ERROR\"\nrescue ArgumentError => e\n  puts e.message\nend"
			if got := strings.TrimSpace(eval(t, src)); got != tc.want {
				t.Errorf("%s raised %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

// TestImageStillWorks is the control for wrapping every method. #to_s earns its
// own row: it is registered from #inspect's native function, so it silently
// inherits whatever wrapper #inspect got, and only comparing the two says the
// alias still works.
func TestImageStillWorks(t *testing.T) {
	src := `i = Image.new(4, 4)
i.set(0, 0, 255, 0, 0)
p i.get(0, 0)
i.set(1, 1, 1, 2, 3, 128)
p i.get(1, 1)
p [i.width, i.height, i.grayscale.width, i.resize(2, 2).width, i.crop(0, 0, 2, 2).width]
p [i.adjust_brightness(1.0).width, i.gaussian_blur(0.5).width, i.canny(1.0, 0.1, 0.2).width]
p i.to_png.bytesize > 0
p i.to_s == i.inspect`
	const want = "[255, 0, 0, 255]\n" +
		"[1, 2, 3, 128]\n" +
		"[4, 4, 4, 2, 2]\n" +
		"[4, 4, 4]\n" +
		"true\n" +
		"true\n"
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
