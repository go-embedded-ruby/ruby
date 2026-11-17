// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	"fmt"
	"testing"

	images "github.com/go-ruby-images/images"
	"github.com/go-ruby-images/images/canvas"
)

// A minimal 2x2 lossless WebP blob. WebP is decode-only in pure Go, so a
// WebP-format Images::Image exercises the encode-error branch of #to_blob and
// #write. The bytes are a real VP8L stream (verified to decode by
// golang.org/x/image/webp) built once with a lossless encoder.
const imagesWebPHex = "524946461e000000574542505650384c110000002f0140000007d0fffef7bfff8188e87f0000"

// TestImagesShells covers the ToS/Inspect/Truthy of the two value wrappers.
func TestImagesShells(t *testing.T) {
	im := &ImagesImage{im: images.New(2, 2, canvas.Black)}
	if got := im.ToS(); got != "#<Images::Image 2x2 png>" {
		t.Errorf("image ToS = %q", got)
	}
	if im.Inspect() != im.ToS() || !im.Truthy() {
		t.Errorf("image inspect/truthy: %q / %v", im.Inspect(), im.Truthy())
	}
	c := &ImagesCanvas{c: canvas.New(1, 1, canvas.White)}
	if got := c.ToS(); got != "#<Images::Canvas>" {
		t.Errorf("canvas ToS = %q", got)
	}
	if c.Inspect() != "#<Images::Canvas>" || !c.Truthy() {
		t.Errorf("canvas inspect/truthy: %q / %v", c.Inspect(), c.Truthy())
	}
}

// TestImagesModule covers Images.supported / Images.encodable.
func TestImagesModule(t *testing.T) {
	src := `
require "images"
puts Images.supported.join(",")
puts Images.encodable.join(",")
`
	want := "png,jpeg,gif,bmp,tiff,webp\npng,jpeg,gif,bmp,tiff"
	if got := runSrc(t, src); got != want {
		t.Errorf("module funcs = %q, want %q", got, want)
	}
}

// TestImagesImageQuery covers Image.new / .read plus the query surface
// (dimensions/width/height/format/metadata/at) and to_blob round-tripping.
func TestImagesImageQuery(t *testing.T) {
	src := `
require "images"
c = Images::Canvas.new(4, 3, Images::Color::WHITE)
c[1, 2] = Images::Color::RED
img = Images::Image.read(c.to_blob)
r = []
w, h = img.dimensions
r << "dims=#{w}x#{h}"
r << "wh=#{img.width},#{img.height}"
r << "fmt=#{img.format}"
m = img.metadata
r << "meta=#{m[:width]}x#{m[:height]}/#{m[:format]}/#{m[:color_model]}"
r << "px=#{img.at(1, 2)}"
blob = img.to_blob
r << "png=#{blob[0, 4] == "\x89PNG"}"
n = Images::Image.new(2, 2)
r << "new=#{n.width}x#{n.height}/#{n.at(0, 0)}"
d = Images::Image.decode(c.to_blob)
r << "decode=#{d.width}x#{d.height}"
puts r.join("|")
`
	want := "dims=4x3|wh=4,3|fmt=png|meta=4x3/png/rgba|px=4278190335|png=true|new=2x2/0|decode=4x3"
	if got := runSrc(t, src); got != want {
		t.Errorf("image query = %q, want %q", got, want)
	}
}

// TestImagesTransforms exercises the geometric transforms + format conversion,
// covering the happy path of every error-returning transform (funnelled through
// imagesWrap) and the total transforms.
func TestImagesTransforms(t *testing.T) {
	src := `
require "images"
c = Images::Canvas.new(8, 6, Images::Color::WHITE)
img = Images::Image.read(c.to_blob)
r = []
r << "resize=#{img.resize(4, 4).dimensions.join("x")}"
r << "nearest=#{img.resize_nearest(3, 3).dimensions.join("x")}"
r << "thumb=#{img.thumbnail(4, 4).dimensions.join("x")}"
r << "crop=#{img.crop(1, 1, 3, 2).dimensions.join("x")}"
r << "rot=#{img.rotate(90).dimensions.join("x")}"
r << "flip=#{img.flip.dimensions.join("x")}"
r << "flop=#{img.flop.dimensions.join("x")}"
r << "gray=#{img.grayscale.format}"
r << "grey=#{img.greyscale.format}"
r << "inv=#{img.invert.format}"
r << "clone=#{img.clone.dimensions.join("x")}"
r << "cvt_s=#{img.convert("jpeg").format}"
r << "cvt_sym=#{img.convert(:gif).format}"
puts r.join("|")
`
	want := "resize=4x4|nearest=3x3|thumb=4x3|crop=3x2|rot=6x8|flip=8x6|flop=8x6|gray=png|grey=png|inv=png|clone=8x6|cvt_s=jpeg|cvt_sym=gif"
	if got := runSrc(t, src); got != want {
		t.Errorf("transforms = %q, want %q", got, want)
	}
}

// TestImagesFilters exercises the scikit-image filter/edge/threshold/morphology
// surface plus the scalar colour ops and Convolve.
func TestImagesFilters(t *testing.T) {
	src := `
require "images"
c = Images::Canvas.new(8, 8, Images::Color::WHITE)
c.rect(2, 2, 5, 5, Images::Color::BLACK, Images::Color::BLACK)
img = Images::Image.read(c.to_blob)
r = []
r << "blur=#{img.blur(1).format}"
r << "gauss=#{img.gaussian_blur(1.0).format}"
r << "unsharp=#{img.unsharp_mask(1.0, 0.5).format}"
r << "median=#{img.median(1).format}"
r << "sharpen=#{img.sharpen.format}"
r << "sobel=#{img.sobel.format}"
r << "prewitt=#{img.prewitt.format}"
r << "scharr=#{img.scharr.format}"
r << "laplacian=#{img.laplacian.format}"
r << "canny=#{img.canny(1.0, 30.0, 90.0).format}"
r << "convolve=#{img.convolve(3, 3, [0, 0, 0, 0, 1, 0, 0, 0, 0]).dimensions.join("x")}"
r << "erode=#{img.erode(1).format}"
r << "dilate=#{img.dilate(1).format}"
r << "open=#{img.open(1).format}"
r << "close=#{img.close(1).format}"
r << "otsu=#{img.otsu.format}"
r << "otsu_t=#{img.otsu_threshold.class}"
r << "thr=#{img.threshold(128).format}"
r << "bright=#{img.brightness(10.0).format}"
r << "contrast=#{img.contrast(1.2).format}"
puts r.join("|")
`
	want := "blur=png|gauss=png|unsharp=png|median=png|sharpen=png|sobel=png|prewitt=png|scharr=png|laplacian=png|canny=png|convolve=8x8|erode=png|dilate=png|open=png|close=png|otsu=png|otsu_t=Integer|thr=png|bright=png|contrast=png"
	if got := runSrc(t, src); got != want {
		t.Errorf("filters = %q, want %q", got, want)
	}
}

// TestImagesWrite covers Image#write: a successful write, an unencodable target
// format inferred from the extension (.webp), and an unwritable path — the last
// two raising Images::Error.
func TestImagesWrite(t *testing.T) {
	dir := t.TempDir()
	src := fmt.Sprintf(`
require "images"
c = Images::Canvas.new(2, 2, Images::Color::WHITE)
img = Images::Image.read(c.to_blob)
r = []
r << (img.write(%q).equal?(img) ? "ok" : "bad")
begin
  img.write(%q)
rescue Images::Error
  r << "webp-err"
end
begin
  img.write(%q)
rescue Images::Error
  r << "path-err"
end
puts r.join("|")
`, dir+"/out.png", dir+"/out.webp", dir+"/no_such_dir/out.png")
	want := "ok|webp-err|path-err"
	if got := runSrc(t, src); got != want {
		t.Errorf("write = %q, want %q", got, want)
	}
}

// TestImagesImageOpenTakesNoBlock pins a NON-defect, which is the only reason it
// exists.
//
// #950 asked whether Images::Image.open discarding its block was a gap, the way
// the socket doors' discarded blocks were (#931/#932: 27 hung spec files) and
// the way Augeas.open's was (#952). Reading the reference settles it the other
// way. go-ruby-images' README calls itself "a MiniMagick / ruby-vips
// replacement", and MiniMagick's is:
//
//	def self.open(path_or_url, ext = nil, **options)
//
// with no block parameter — no `yield`, no `block_given?`, no `ensure`. The only
// blocks in its body are internal, handed to `uri.open` / `pathname.open`.
// ruby-vips has no `Image.open` at all; it spells this `Vips::Image
// .new_from_file`. A Ruby method that takes no block and is handed one ignores
// it, so rbgo discarding it IS the contract.
//
// ⛔ WITHOUT THIS TEST THE CORRECT BEHAVIOUR IS INDISTINGUISHABLE FROM THE
// DEFECT IT RESEMBLES. The three other `open` doors that discarded a block were
// all real bugs — #931/#932 for the sockets, #952 for Augeas — so the next
// reader to notice this one has every reason to assume the same and to "fix" it
// into socketOpenWithBlock.
//
// ⛔⛔ AND THAT FIX WOULD BE WORSE THAN POINTLESS, which is what the mutation
// showed rather than reasoned: socketOpenWithBlock closes through the CLASS'S
// OWN `close`, and on Images::Image `close` is the MORPHOLOGICAL CLOSING
// operator — dilate then erode, taking a radius — just as `#open` is
// morphological opening. Wiring the door makes the program die with
//
//	ArgumentError: wrong number of arguments (given 0, expected 1)
//
// from that operator, called with no radius on the way out of the block. The
// helper's "close whatever the class calls close" contract is silent about
// classes where that name means something else, and this is one.
func TestImagesImageOpenTakesNoBlock(t *testing.T) {
	dir := t.TempDir()
	src := fmt.Sprintf(`
require "images"
Images::Image.read(Images::Canvas.new(2, 2, Images::Color::WHITE).to_blob).write(%q)

ran = false
got = Images::Image.open(%q) { |i| ran = true; "the block's value" }

# The block is not called, and open answers the IMAGE -- not the block's value,
# which is the half a yield-only "fix" would still get wrong.
puts ran.inspect
puts got.class
puts got.dimensions.inspect
`, dir+"/in.png", dir+"/in.png")
	want := "false\nImages::Image\n[2, 2]"
	if got := runSrc(t, src); got != want {
		t.Errorf("Image.open with a block:\n got=%q\nwant=%q\n"+
			"a block that RAN, or a return value that is not the image, means this door was wired to "+
			"socketOpenWithBlock -- see testdata/open-doors.txt: MiniMagick's open takes no block", got, want)
	}
}

// TestImagesImageErrors covers every Images::Image error branch: a missing file
// and a corrupt blob (imagesWrap -> Images::Error), a non-positive resize and an
// unencodable convert (also imagesWrap), a bad rotation, an out-of-bounds pixel
// (Images::OutOfBoundsError), a WebP #to_blob (encode error), a non-String blob
// (TypeError), a non-numeric filter argument (TypeError), a non-Array kernel
// (TypeError), the ToS format arm, and an arity error.
func TestImagesImageErrors(t *testing.T) {
	src := `
require "images"
c = Images::Canvas.new(3, 3, Images::Color::WHITE)
img = Images::Image.read(c.to_blob)
r = []
begin; Images::Image.open("/no/such/file.png"); rescue Images::Error; r << "open"; end
begin; Images::Image.read("not an image"); rescue Images::Error; r << "read"; end
begin; img.resize(0, 0); rescue Images::Error; r << "resize"; end
begin; img.rotate(45); rescue Images::Error; r << "rotate"; end
begin; img.convert("xcf"); rescue Images::Error; r << "convert-str"; end
begin; img.convert(:webp); rescue Images::Error; r << "convert-sym"; end
begin; img.convert(123); rescue Images::Error; r << "convert-tos"; end
begin
  img.at(99, 99)
rescue Images::OutOfBoundsError => e
  r << (e.is_a?(Images::Error) && e.is_a?(StandardError) ? "oob" : "bad")
end
webp = ["` + imagesWebPHex + `"].pack("H*")
begin; Images::Image.read(webp).to_blob; rescue Images::Error; r << "blob"; end
begin; Images::Image.read(123); rescue TypeError; r << "type"; end
begin; img.gaussian_blur("x"); rescue TypeError; r << "float"; end
begin; img.convolve(3, 3, "notarray"); rescue TypeError; r << "slice"; end
begin; Images::Image.open; rescue ArgumentError; r << "arity"; end
puts r.join("|")
`
	want := "open|read|resize|rotate|convert-str|convert-sym|convert-tos|oob|blob|type|float|slice|arity"
	if got := runSrc(t, src); got != want {
		t.Errorf("image errors = %q, want %q", got, want)
	}
}

// TestImagesCanvas covers the chunky_png-style Canvas surface: construction, PNG
// decode round-trip (decode/from_blob and load/from_file), per-pixel access,
// drawing primitives, compose and PNG output.
func TestImagesCanvas(t *testing.T) {
	dir := t.TempDir()
	src := fmt.Sprintf(`
require "images"
c = Images::Canvas.new(6, 4, Images::Color::WHITE)
r = []
r << "wh=#{c.width}x#{c.height}"
c[0, 0] = Images::Color::RED
r << "px=#{c[0, 0]}"
r << "at=#{c.at(0, 0)}"
c.set(1, 0, Images::Color::GREEN)
c.point(2, 0, Images::Color::BLUE)
r << "set=#{c[1, 0]},#{c[2, 0]}"
c.fill(Images::Color::WHITE)
c.line(0, 0, 5, 3, Images::Color::BLACK)
c.rect(1, 1, 4, 3, Images::Color::BLACK, Images::Color::TRANSPARENT)
c.circle(3, 2, 1, Images::Color::BLACK)
blob = c.to_blob
r << "png=#{blob[0, 4] == "\x89PNG"}"
dec = Images::Canvas.decode(blob)
r << "decode=#{dec.width}x#{dec.height}"
fb = Images::Canvas.from_blob(blob)
r << "from_blob=#{fb.width}x#{fb.height}"
path = %q
c.save(path)
ld = Images::Canvas.load(path)
r << "load=#{ld.width}x#{ld.height}"
ff = Images::Canvas.from_file(path)
r << "from_file=#{ff.width}x#{ff.height}"
over = Images::Canvas.new(2, 2, Images::Color::RED)
c.compose(over, 0, 0)
r << "compose=#{c[0, 0]}"
puts r.join("|")
`, dir+"/c.png")
	want := "wh=6x4|px=4278190335|at=4278190335|set=16711935,65535|png=true|decode=6x4|from_blob=6x4|load=6x4|from_file=6x4|compose=4278190335"
	if got := runSrc(t, src); got != want {
		t.Errorf("canvas = %q, want %q", got, want)
	}
}

// TestImagesCanvasErrors covers the Canvas error branches: out-of-bounds read
// and write (Images::OutOfBoundsError), a corrupt decode, a missing load file
// and an unwritable save path (Images::Error), and an arity error.
func TestImagesCanvasErrors(t *testing.T) {
	dir := t.TempDir()
	src := fmt.Sprintf(`
require "images"
c = Images::Canvas.new(2, 2, Images::Color::WHITE)
r = []
begin; c[9, 9]; rescue Images::OutOfBoundsError; r << "read"; end
begin; c[9, 9] = Images::Color::RED; rescue Images::OutOfBoundsError; r << "write"; end
begin; Images::Canvas.decode("not a png"); rescue Images::Error; r << "decode"; end
begin; Images::Canvas.load("/no/such/file.png"); rescue Images::Error; r << "load"; end
begin; c.save(%q); rescue Images::Error; r << "save"; end
begin; Images::Canvas.new(2); rescue ArgumentError; r << "arity"; end
puts r.join("|")
`, dir+"/no_such_dir/x.png")
	want := "read|write|decode|load|save|arity"
	if got := runSrc(t, src); got != want {
		t.Errorf("canvas errors = %q, want %q", got, want)
	}
}

// TestImagesColor covers the Images::Color helper module: the named constants,
// rgb/rgba construction, from_hex (and its ArgumentError), the r/g/b/a
// component accessors, hex/to_hex and compose.
func TestImagesColor(t *testing.T) {
	src := `
require "images"
r = []
r << "black=#{Images::Color::BLACK}"
r << "white=#{Images::Color::WHITE}"
r << "trans=#{Images::Color::TRANSPARENT}"
red = Images::Color.rgb(255, 0, 0)
r << "rgb=#{red}"
half = Images::Color.rgba(0, 255, 0, 128)
r << "rgba=#{half}"
h = Images::Color.from_hex("#0000ffff")
r << "hex_in=#{h == Images::Color::BLUE}"
r << "comp=#{Images::Color.r(red)},#{Images::Color.g(half)},#{Images::Color.b(Images::Color::BLUE)},#{Images::Color.a(half)}"
r << "hex=#{Images::Color.hex(red)}"
r << "to_hex=#{Images::Color.to_hex(Images::Color::GREEN)}"
r << "compose=#{Images::Color.compose(Images::Color::WHITE, Images::Color::BLACK)}"
begin; Images::Color.from_hex("nope"); rescue ArgumentError; r << "badhex"; end
puts r.join("|")
`
	want := "black=255|white=4294967295|trans=0|rgb=4278190335|rgba=16711808|hex_in=true|comp=255,255,255,128|hex=#ff0000ff|to_hex=#00ff00ff|compose=4294967295|badhex"
	if got := runSrc(t, src); got != want {
		t.Errorf("color = %q, want %q", got, want)
	}
}

// TestImagesNewFill covers Images::Image.new / Images::Canvas.new with an
// explicit fill colour (the imagesColorArg arm), complementing the default-fill
// (canvasTransparent) paths exercised elsewhere.
func TestImagesNewFill(t *testing.T) {
	src := `
require "images"
img = Images::Image.new(2, 2, Images::Color::RED)
c = Images::Canvas.new(2, 2, Images::Color::BLUE)
puts "#{img.at(0, 0)}|#{c[0, 0]}"
`
	want := "4278190335|65535"
	if got := runSrc(t, src); got != want {
		t.Errorf("new fill = %q, want %q", got, want)
	}
}
