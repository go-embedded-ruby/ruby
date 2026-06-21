package vm_test

import (
	"strings"
	"testing"
)

// TestImage covers the go-images binding: construction, pixel get/set, the
// filter/transform/colour operations, and the Value-interface methods.
func TestImage(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction, dimensions, pixel access.
		{`i = Image.new(4, 3); p [i.width, i.height]`, "[4, 3]\n"},
		{`i = Image.new(2, 2); i.set(0, 0, 10, 20, 30); p i.get(0, 0)`, "[10, 20, 30, 255]\n"},          // default alpha
		{`i = Image.new(2, 2); i.set(0, 0, 10, 20, 30, 128); p i.get(0, 0)`, "[10, 20, 30, 128]\n"},     // explicit alpha
		{`i = Image.new(2, 2); i.set(0, 0, 10, 20, 30); p i.invert.get(0, 0)`, "[245, 235, 225, 255]\n"}, // 255 - v
		// Unary operations (no error) — share one closure; varied for confidence.
		{`p Image.new(4, 4).grayscale.width`, "4\n"},
		{`p Image.new(4, 4).sobel.height`, "4\n"},
		{`p Image.new(4, 3).rotate90.width`, "3\n"}, // 90° swaps the axes
		{`p Image.new(4, 3).rotate90.height`, "4\n"},
		{`p Image.new(4, 4).flip_horizontal.width`, "4\n"},
		{`p Image.new(4, 4).rgb_to_hsv.width`, "4\n"},
		{`p Image.new(4, 4).otsu.width`, "4\n"},
		{`p Image.new(4, 4).sharpen.width`, "4\n"},
		// Scalar operations.
		{`p Image.new(2, 2).adjust_brightness(10.0).width`, "2\n"},
		{`p Image.new(2, 2).adjust_contrast(1.2).width`, "2\n"},
		// Operations that can fail (here they succeed) — each its own closure.
		{`p Image.new(8, 8).gaussian_blur(1.0).width`, "8\n"},
		{`p Image.new(8, 8).box_blur(1).width`, "8\n"},
		{`p Image.new(8, 8).median(1).width`, "8\n"},
		{`p Image.new(8, 8).erode(1).width`, "8\n"},
		{`p Image.new(8, 8).dilate(1).width`, "8\n"},
		{`p Image.new(8, 8).canny(1.0, 0.1, 0.2).width`, "8\n"},
		{`p Image.new(8, 8).resize(16, 4).width`, "16\n"},
		{`p Image.new(8, 8).resize(16, 4).height`, "4\n"},
		{`p Image.new(8, 8).crop(2, 2, 3, 3).width`, "3\n"},
		// Value-interface: p → Inspect, puts → ToS, ?: → Truthy, .inspect method.
		{`p Image.new(2, 2)`, "#<Image 2x2>\n"},
		{`puts Image.new(2, 2)`, "#<Image 2x2>\n"},
		{`p Image.new(2, 2).inspect`, "\"#<Image 2x2>\"\n"},
		{`p Image.new(2, 2).to_s`, "\"#<Image 2x2>\"\n"},
		{`p(Image.new(2, 2) ? "y" : "n")`, "\"y\"\n"},
		// Save → Load PNG round-trip preserves dimensions and pixels.
		{`i = Image.new(3, 2)
i.set(1, 1, 99, 88, 77)
i.save("/tmp/rbgo_imgtest.png")
j = Image.load("/tmp/rbgo_imgtest.png")
p [j.width, j.height, j.get(1, 1)]`, "[3, 2, [99, 88, 77, 255]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestImageErrors covers the raising paths: bad dimensions, a missing file, an
// out-of-bounds crop, and an unwritable save path.
func TestImageErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Image.new(0, 2)`, "ArgumentError"},
		{`Image.load("/nonexistent-rbgo.png")`, "ArgumentError"},
		{`Image.new(4, 4).crop(0, 0, 99, 99)`, "ArgumentError"},
		{`Image.new(2, 2).save("/no/such/dir/x.png")`, "ArgumentError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
