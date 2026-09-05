package agenttools

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // an animated task picture decodes to its first frame, which is the one carrying the task
	"image/jpeg"
	"image/png"
	"math"
	"strconv"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/skrashevich/encx-cli/encx"
)

// A provider downscales a picture as a whole before the model sees it, so the
// small print inside it is gone by then. Cropping is what hands a region back
// at its own resolution — which is why these helpers sit next to
// enc_view_image rather than replacing it.

const (
	// defaultMaxDimension is the longer side a returned fragment is trimmed to.
	// Vision models work at roughly this resolution and providers shrink
	// anything larger themselves, so sending more only costs bandwidth.
	defaultMaxDimension = 1568
	// maxAllowedDimension bounds what a caller may ask for.
	maxAllowedDimension = 4096
	// jpegQuality re-encodes a fragment of a photograph: high enough that small
	// print survives, low enough that several fragments fit in one request.
	jpegQuality = 90
	// pngByteBudget is where a PNG stops being worth its exactness. Above it the
	// fragment is a photograph rather than line art or a screenshot, and a
	// handful of those would be megabytes of base64.
	pngByteBudget = 1 << 20
	// maxCoordinate bounds a crop coordinate. It sits far past the side of any
	// picture that passes maxDecodedPixels, so it only catches nonsense — and it
	// has to catch it, because rectangle arithmetic on an unbounded coordinate
	// overflows into "the whole picture" rather than into an error.
	maxCoordinate = 1 << 24
	// maxDecodedPixels bounds what may be decoded at all. The fetch is capped in
	// bytes, but bytes say nothing about pixels: a few kilobytes of PNG can
	// declare a picture of forty thousand pixels a side, and decoding it would
	// ask for gigabytes on a phone. A task picture is a photograph or a
	// screenshot, so this is far above anything real.
	maxDecodedPixels = 25 << 20
)

// decodedImage is a fetched picture ready to be cut up.
type decodedImage struct {
	img    image.Image
	format string // as image.Decode reports it: "jpeg", "png", "gif", "webp", …
	url    string
	bytes  int
}

func (d *decodedImage) width() int  { return d.img.Bounds().Dx() }
func (d *decodedImage) height() int { return d.img.Bounds().Dy() }

// decodeResource turns a fetched file into an image. The decoder, not the
// server's Content-Type, has the last word: authors host task pictures on
// whatever service they like, and a JPEG served as application/octet-stream is
// common enough that trusting the header would refuse pictures that decode
// perfectly well.
func decodeResource(resource *encx.Resource) (*decodedImage, error) {
	if resource == nil || len(resource.Data) == 0 {
		return nil, errors.New("the resource is empty")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(resource.Data))
	if err != nil {
		return nil, fmt.Errorf(
			"%s (%s) could not be read as an image; JPEG, PNG, GIF, WebP, BMP and TIFF are supported: %w",
			resource.URL, resource.ContentType, err)
	}
	if err := checkDecodedSize(resource.URL, config.Width, config.Height); err != nil {
		return nil, err
	}

	img, format, err := image.Decode(bytes.NewReader(resource.Data))
	if err != nil {
		return nil, fmt.Errorf("%s could not be decoded as an image: %w", resource.URL, err)
	}
	return &decodedImage{img: img, format: format, url: resource.URL, bytes: len(resource.Data)}, nil
}

// checkDecodedSize refuses a picture before it is decoded: a URL taken from
// game content is untrusted input, and the byte cap on the fetch does not bound
// the allocation a decoder will ask for.
func checkDecodedSize(url string, width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("%s reports a size of %dx%d, which is not a picture", url, width, height)
	}
	if pixels := int64(width) * int64(height); pixels > maxDecodedPixels {
		return fmt.Errorf(
			"%s is %dx%d, %d megapixels; anything over %d is refused because decoding it would ask for "+
				"gigabytes of memory", url, width, height, pixels>>20, maxDecodedPixels>>20)
	}
	return nil
}

// cropBox is a fragment of a picture in the coordinates a model reasons in:
// zero-based, top-left origin, pixels.
type cropBox struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func newCropBox(bounds, rect image.Rectangle) cropBox {
	return cropBox{
		X:      rect.Min.X - bounds.Min.X,
		Y:      rect.Min.Y - bounds.Min.Y,
		Width:  rect.Dx(),
		Height: rect.Dy(),
	}
}

// clampCrop resolves a requested box against the picture. A box hanging over an
// edge is an imprecise guess and is honoured as far as it reaches; a box that
// misses the picture entirely is a mistake the model has to be told about.
func clampCrop(bounds image.Rectangle, box cropBox) (image.Rectangle, error) {
	if box.Width <= 0 || box.Height <= 0 {
		return image.Rectangle{}, fmt.Errorf(
			"the crop at x=%d y=%d is %dx%d inside a %dx%d picture; width and height have to be positive",
			box.X, box.Y, box.Width, box.Height, bounds.Dx(), bounds.Dy())
	}
	requested := image.Rect(box.X, box.Y, box.X+box.Width, box.Y+box.Height).Add(bounds.Min)
	clamped := requested.Intersect(bounds)
	if clamped.Empty() {
		return image.Rectangle{}, fmt.Errorf(
			"the crop x=%d y=%d %dx%d lies outside the %dx%d picture",
			box.X, box.Y, box.Width, box.Height, bounds.Dx(), bounds.Dy())
	}
	return clamped, nil
}

// cropImage returns the fragment inside rect, sharing pixels with the source
// when the source allows it.
func cropImage(img image.Image, rect image.Rectangle) image.Image {
	if sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return sub.SubImage(rect)
	}
	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), img, rect.Min, draw.Src)
	return out
}

// fitWithin shrinks a fragment so its longer side is at most limit, averaging
// each source block instead of picking one pixel out of it. Nearest-neighbour
// sampling would drop exactly what a crop is taken for: hairline strokes and
// small print.
func fitWithin(img image.Image, limit int) image.Image {
	bounds := img.Bounds()
	longer := max(bounds.Dx(), bounds.Dy())
	if limit <= 0 || longer <= limit {
		return img
	}

	scale := float64(limit) / float64(longer)
	width := max(int(math.Round(float64(bounds.Dx())*scale)), 1)
	height := max(int(math.Round(float64(bounds.Dy())*scale)), 1)

	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		y0 := bounds.Min.Y + y*bounds.Dy()/height
		y1 := bounds.Min.Y + (y+1)*bounds.Dy()/height
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := range width {
			x0 := bounds.Min.X + x*bounds.Dx()/width
			x1 := bounds.Min.X + (x+1)*bounds.Dx()/width
			if x1 <= x0 {
				x1 = x0 + 1
			}
			out.SetRGBA(x, y, averageBlock(img, image.Rect(x0, y0, x1, y1)))
		}
	}
	return out
}

// averageBlock is the mean colour of one source block, in the premultiplied
// form image.RGBA stores.
func averageBlock(img image.Image, block image.Rectangle) color.RGBA {
	var r, g, b, a uint64
	for y := block.Min.Y; y < block.Max.Y; y++ {
		for x := block.Min.X; x < block.Max.X; x++ {
			pr, pg, pb, pa := img.At(x, y).RGBA()
			r += uint64(pr)
			g += uint64(pg)
			b += uint64(pb)
			a += uint64(pa)
		}
	}
	count := uint64(block.Dx() * block.Dy())
	return color.RGBA{
		R: uint8((r / count) >> 8),
		G: uint8((g / count) >> 8),
		B: uint8((b / count) >> 8),
		A: uint8((a / count) >> 8),
	}
}

// fragment is one cropped piece on its way to the model. The views in
// imagetools.go are what reaches it as JSON; this only feeds them.
type fragment struct {
	Box         cropBox
	Width       int
	Height      int
	ContentType string
	dataURL     string
}

// renderFragment crops, shrinks and encodes one piece of a picture.
func renderFragment(source *decodedImage, rect image.Rectangle, maxDimension int) (fragment, error) {
	piece := fitWithin(cropImage(source.img, rect), maxDimension)
	encoded, contentType, err := encodeFragment(piece, source.format)
	if err != nil {
		return fragment{}, err
	}
	return fragment{
		Box:         newCropBox(source.img.Bounds(), rect),
		Width:       piece.Bounds().Dx(),
		Height:      piece.Bounds().Dy(),
		ContentType: contentType,
		dataURL:     encoded,
	}, nil
}

// encodeFragment renders a fragment as an inline data URL, which is the form
// PicoClaw's providers turn into an image part.
//
// A fragment of a JPEG stays JPEG: the picture is already lossy and PNG would
// multiply its size for no gain. Line art and screenshots are encoded as PNG so
// flat colour and small text stay exact — unless the result is too heavy to be
// anything but a photograph, in which case JPEG wins on size.
func encodeFragment(img image.Image, sourceFormat string) (string, string, error) {
	var buf bytes.Buffer
	if sourceFormat == "png" || sourceFormat == "gif" {
		if err := png.Encode(&buf, img); err != nil {
			return "", "", fmt.Errorf("encode the fragment as PNG: %w", err)
		}
		if buf.Len() <= pngByteBudget {
			return inlineImage("image/png", buf.Bytes()), "image/png", nil
		}
		buf.Reset()
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return "", "", fmt.Errorf("encode the fragment as JPEG: %w", err)
	}
	return inlineImage("image/jpeg", buf.Bytes()), "image/jpeg", nil
}

func inlineImage(contentType string, data []byte) string {
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// optionalPixels reads one crop coordinate.
//
// The value stays a float64 until it has been range-checked, because
// converting one that does not fit an int is undefined in Go — and because an
// out-of-range coordinate has to be refused rather than converted: rectangle
// arithmetic on it overflows, and an overflowed box canonicalises into "the
// whole picture" instead of an error.
func (a arguments) optionalPixels(key string, span int) (int, bool, error) {
	raw, ok := a[key]
	if !ok || raw == nil {
		return 0, false, nil
	}
	value, err := a.pixelValue(key, span)
	if err != nil {
		return 0, false, err
	}
	if math.IsNaN(value) || math.IsInf(value, 0) ||
		value < -maxCoordinate || value > maxCoordinate {
		return 0, false, fmt.Errorf(
			"argument %q is %v, outside the %d pixels a picture can measure", key, raw, maxCoordinate)
	}
	return int(value), true, nil
}

// pixelValue resolves one coordinate to pixels. Providers are inconsistent
// about number typing, and a model that has not measured the picture yet can
// only reason in fractions, so a percentage of the relevant side — "33%" — is
// accepted next to a pixel count.
func (a arguments) pixelValue(key string, span int) (float64, error) {
	raw := a[key]
	if text, isText := raw.(string); isText {
		trimmed := strings.TrimSpace(text)
		if percent, isPercent := strings.CutSuffix(trimmed, "%"); isPercent {
			share, err := strconv.ParseFloat(strings.TrimSpace(percent), 64)
			if err != nil {
				return 0, fmt.Errorf("argument %q is not a percentage: %q", key, text)
			}
			// ParseFloat accepts "Inf" and "NaN"; both survive this arithmetic and
			// are caught by the range check.
			return math.Round(share / 100 * float64(span)), nil
		}
	}
	if number, isNumber := raw.(float64); isNumber {
		return number, nil
	}
	value, ok := a.optionalInt(key)
	if !ok {
		return 0, fmt.Errorf(
			"argument %q has to be a pixel count or a percentage of the picture like \"33%%\"", key)
	}
	return float64(value), nil
}

// maxDimensionArgument reads the cap on the longer side of a returned fragment.
func (a arguments) maxDimensionArgument() (int, error) {
	value, ok := a.optionalInt("max_dimension")
	if !ok {
		return defaultMaxDimension, nil
	}
	if value <= 0 || value > maxAllowedDimension {
		return 0, fmt.Errorf(
			"max_dimension has to be between 1 and %d pixels, got %d", maxAllowedDimension, value)
	}
	return value, nil
}
