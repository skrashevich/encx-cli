package agenttools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

// The pictures below stand in for the shape these tools exist for: a task
// picture that is really several unrelated pictures pasted into one file. Each
// panel is filled with a pattern that varies along both axes, because a panel
// of flat colour would itself look like a separator.

const (
	panelWidth  = 300
	panelHeight = 200
	seamWidth   = 10
)

// panelPattern is a slow diagonal gradient, offset per panel so neighbours do
// not blend into one another. It changes along both axes — a panel of flat
// colour would look like a separator — but gently, the way a drawing or a
// photograph does: a high-frequency pattern would smear into the separator
// under JPEG compression and make the fixture harder than any real picture.
func panelPattern(x, y, offset int) color.RGBA {
	return color.RGBA{
		R: uint8(40 + (x+offset)/3%180),
		G: uint8(40 + (y+offset)/4%180),
		B: uint8(40 + (x+y+offset)/5%180),
		A: 255,
	}
}

// horizontalCollage pastes panels side by side with a white separator between
// them, optionally leaving a white margin at both ends.
func horizontalCollage(panels, margin int) *image.RGBA {
	width := panels*panelWidth + (panels-1)*seamWidth + 2*margin
	canvas := image.NewRGBA(image.Rect(0, 0, width, panelHeight))
	fillWhite(canvas)
	for panel := range panels {
		left := margin + panel*(panelWidth+seamWidth)
		for y := range panelHeight {
			for x := range panelWidth {
				canvas.SetRGBA(left+x, y, panelPattern(x, y, panel*40))
			}
		}
	}
	return canvas
}

// verticalCollage stacks panels with a white separator between them.
func verticalCollage(panels int) *image.RGBA {
	height := panels*panelHeight + (panels-1)*seamWidth
	canvas := image.NewRGBA(image.Rect(0, 0, panelWidth, height))
	fillWhite(canvas)
	for panel := range panels {
		top := panel * (panelHeight + seamWidth)
		for y := range panelHeight {
			for x := range panelWidth {
				canvas.SetRGBA(x, top+y, panelPattern(x, y, panel*40))
			}
		}
	}
	return canvas
}

func fillWhite(canvas *image.RGBA) {
	bounds := canvas.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			canvas.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
}

func encodePNGBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode the fixture as PNG: %v", err)
	}
	return buf.Bytes()
}

func encodeJPEGBytes(t *testing.T, img image.Image, quality int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("encode the fixture as JPEG: %v", err)
	}
	return buf.Bytes()
}

func TestDetectSegmentsFindsThePanelsOfACollage(t *testing.T) {
	segments := detectSegments(horizontalCollage(3, 0), axisHorizontal)
	if len(segments) != 3 {
		t.Fatalf("a three-panel collage should split into 3 parts, got %v", segments)
	}
	// The cut goes through the middle of the separator, so neither neighbour
	// loses an edge.
	wantCuts := []int{panelWidth + seamWidth/2, 2*panelWidth + seamWidth + seamWidth/2}
	if segments[0].to != wantCuts[0] || segments[1].to != wantCuts[1] {
		t.Fatalf("cuts should fall inside the separators at %v, got %v", wantCuts, segments)
	}
	if segments[0].from != 0 || segments[2].to != panelWidth*3+seamWidth*2 {
		t.Fatalf("the parts should cover the whole picture, got %v", segments)
	}
}

func TestDetectSegmentsIgnoresTheMarginsAtTheEdges(t *testing.T) {
	const margin = 25
	segments := detectSegments(horizontalCollage(2, margin), axisHorizontal)
	if len(segments) != 2 {
		t.Fatalf("two panels inside a margin should split into 2 parts, got %v", segments)
	}
	// A run of background at the end says where the content starts, not where to
	// cut: cutting there would return an empty picture as a "part".
	if segments[0].from != margin {
		t.Fatalf("the first part should start at the content, x=%d, got %d", margin, segments[0].from)
	}
	if want := margin + 2*panelWidth + seamWidth; segments[1].to != want {
		t.Fatalf("the last part should end at the content, x=%d, got %d", want, segments[1].to)
	}
}

func TestDetectSegmentsSurvivesJPEGNoise(t *testing.T) {
	// A real collage arrives as a JPEG, and JPEG rings around the hard edge
	// between a panel and its separator. A detector that demanded exactly equal
	// pixels would find no separators at all in practice.
	encoded := encodeJPEGBytes(t, horizontalCollage(3, 0), 75)
	decoded, _, err := image.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode the JPEG fixture: %v", err)
	}

	segments := detectSegments(decoded, axisHorizontal)
	if len(segments) != 3 {
		t.Fatalf("a JPEG collage should still split into 3 parts, got %v", segments)
	}
	for index, want := range [][2]int{
		{panelWidth, panelWidth + seamWidth},
		{2*panelWidth + seamWidth, 2*panelWidth + 2*seamWidth},
	} {
		if cut := segments[index].to; cut < want[0] || cut > want[1] {
			t.Fatalf("cut %d should fall inside the separator [%d,%d), got %d",
				index+1, want[0], want[1], cut)
		}
	}
}

func TestPlanSplitPicksTheAxisFromThePicture(t *testing.T) {
	plan, err := planSplit(verticalCollage(2), axisAuto, 0)
	if err != nil {
		t.Fatalf("a stack of two panels should be split: %v", err)
	}
	if plan.Axis != axisVertical {
		t.Fatalf("stacked panels are cut horizontally, so the axis is vertical, got %q", plan.Axis)
	}
	if plan.Method != "detected" || len(plan.Segments) != 2 {
		t.Fatalf("the separator in the picture should be used, got %q with %v", plan.Method, plan.Segments)
	}
}

func TestPlanSplitRefusesToGuessWithoutSeparators(t *testing.T) {
	single := image.NewRGBA(image.Rect(0, 0, panelWidth, panelHeight))
	for y := range panelHeight {
		for x := range panelWidth {
			single.SetRGBA(x, y, panelPattern(x, y, 0))
		}
	}

	// Inventing a part count would hand the model fragments that cut through the
	// picture and read like separate clues. Saying so is better.
	if _, err := planSplit(single, axisAuto, 0); err == nil {
		t.Fatal("a picture without separators should not be split blindly")
	}

	plan, err := planSplit(single, axisHorizontal, 3)
	if err != nil {
		t.Fatalf("an explicit part count should still divide the picture: %v", err)
	}
	if plan.Method != "equal" {
		t.Fatalf("a division the picture does not confirm has to be reported as equal, got %q", plan.Method)
	}
	if len(plan.Segments) != 3 {
		t.Fatalf("3 parts were asked for, got %v", plan.Segments)
	}
	if plan.Segments[0].from != 0 || plan.Segments[2].to != panelWidth {
		t.Fatalf("an equal division should cover the whole side, got %v", plan.Segments)
	}
}

func TestPlanSplitRejectsAnImpossiblePartCount(t *testing.T) {
	for _, parts := range []int{1, maxSplitParts + 1} {
		if _, err := planSplit(horizontalCollage(3, 0), axisAuto, parts); err == nil {
			t.Fatalf("parts=%d should be refused", parts)
		}
	}

	// Dividing a picture past one pixel per part would attach empty images.
	sliver := image.NewRGBA(image.Rect(0, 0, 4, 40))
	if _, err := planSplit(sliver, axisHorizontal, 8); err == nil {
		t.Fatal("a picture narrower than the requested part count should be refused")
	}
}

func TestMergeThinSegmentsKeepsFullCoverage(t *testing.T) {
	// Noise in the separator detection can carve off a sliver. Folding it into a
	// neighbour is the only fix that leaves no gap between the parts.
	merged := mergeThinSegments([]segment{{0, 3}, {3, 100}, {100, 104}, {104, 200}}, 10)
	if len(merged) != 2 {
		t.Fatalf("the two slivers should be folded away, got %v", merged)
	}
	if merged[0] != (segment{0, 104}) || merged[1] != (segment{104, 200}) {
		t.Fatalf("the parts should still cover 0..200 without gaps, got %v", merged)
	}
}

func TestClampCropTrimsToThePicture(t *testing.T) {
	bounds := image.Rect(0, 0, 100, 80)

	// An imprecise guess that hangs over the edge is honoured as far as it
	// reaches: the model wanted the bottom right corner and gets it.
	clamped, err := clampCrop(bounds, cropBox{X: 60, Y: 60, Width: 100, Height: 100})
	if err != nil {
		t.Fatalf("a box hanging over the edge should be trimmed, not refused: %v", err)
	}
	if clamped != image.Rect(60, 60, 100, 80) {
		t.Fatalf("the box should be trimmed to the picture, got %v", clamped)
	}

	if _, err := clampCrop(bounds, cropBox{X: 200, Y: 0, Width: 10, Height: 10}); err == nil {
		t.Fatal("a box outside the picture is a mistake the model has to see")
	}
	if _, err := clampCrop(bounds, cropBox{X: 0, Y: 0, Width: 0, Height: 10}); err == nil {
		t.Fatal("an empty box should be refused")
	}
}

func TestOptionalPixelsReadsBothPixelsAndPercentages(t *testing.T) {
	args := arguments{
		"pixels":   float64(120),
		"text":     "120",
		"share":    "25%",
		"spaced":   " 50 % ",
		"nonsense": "half",
	}
	for _, testCase := range []struct {
		key  string
		want int
	}{
		{"pixels", 120},
		{"text", 120},
		{"share", 100},
		{"spaced", 200},
	} {
		got, ok, err := args.optionalPixels(testCase.key, 400)
		if err != nil || !ok {
			t.Fatalf("%s should resolve, got %d ok=%v err=%v", testCase.key, got, ok, err)
		}
		if got != testCase.want {
			t.Fatalf("%s = %d, want %d", testCase.key, got, testCase.want)
		}
	}

	if _, _, err := args.optionalPixels("nonsense", 400); err == nil {
		t.Fatal("a value that is neither pixels nor a percentage should be refused")
	}
	if _, ok, err := args.optionalPixels("missing", 400); ok || err != nil {
		t.Fatalf("an absent argument is not an error, got ok=%v err=%v", ok, err)
	}
}

func TestFitWithinAveragesInsteadOfDropping(t *testing.T) {
	// Two pixels per block, black and white. Averaging returns grey; picking one
	// pixel out of the block would return black or white and erase the stroke.
	source := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		for x := range 4 {
			shade := uint8(0)
			if x%2 == 1 {
				shade = 255
			}
			source.SetRGBA(x, y, color.RGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}

	shrunk := fitWithin(source, 2)
	if got := shrunk.Bounds(); got.Dx() != 2 || got.Dy() != 1 {
		t.Fatalf("the longer side should be trimmed to 2 with the aspect kept, got %v", got)
	}
	r, _, _, _ := shrunk.At(0, 0).RGBA()
	if grey := r >> 8; grey < 120 || grey > 135 {
		t.Fatalf("a black and a white pixel should average to grey, got %d", grey)
	}

	unchanged := fitWithin(source, 100)
	if unchanged != image.Image(source) {
		t.Fatal("a fragment already inside the limit should be left alone")
	}
}

func TestEncodeFragmentKeepsLineArtLossless(t *testing.T) {
	art := image.NewRGBA(image.Rect(0, 0, 8, 8))
	fillWhite(art)

	inline, contentType, err := encodeFragment(art, "png")
	if err != nil {
		t.Fatalf("encode a PNG fragment: %v", err)
	}
	if contentType != "image/png" || !strings.HasPrefix(inline, "data:image/png;base64,") {
		t.Fatalf("a PNG fragment should stay PNG, got %q / %.40q", contentType, inline)
	}

	// A fragment of a photograph is already lossy; PNG would only multiply its
	// size.
	inline, contentType, err = encodeFragment(art, "jpeg")
	if err != nil {
		t.Fatalf("encode a JPEG fragment: %v", err)
	}
	if contentType != "image/jpeg" || !strings.HasPrefix(inline, "data:image/jpeg;base64,") {
		t.Fatalf("a JPEG fragment should stay JPEG, got %q / %.40q", contentType, inline)
	}
}

func TestDecodeResourceRefusesAPictureTooLargeToDecode(t *testing.T) {
	// A URL from game content is untrusted input, and the byte cap on the fetch
	// says nothing about pixels: this header is 33 bytes and claims a picture
	// that would need eight gigabytes of memory to decode.
	huge := &encx.Resource{
		URL:         "/upload/bomb.png",
		ContentType: "image/png",
		Data:        pngHeader(46000, 46000),
	}
	_, err := decodeResource(huge)
	if err == nil {
		t.Fatal("a picture that cannot be decoded within memory should be refused")
	}
	if !strings.Contains(err.Error(), "megapixels") {
		t.Fatalf("the refusal should say why, got %v", err)
	}

	// The guard has to leave a real picture alone.
	if _, err := decodeResource(&encx.Resource{
		URL:         "/upload/collage.png",
		ContentType: "image/png",
		Data:        encodePNGBytes(t, horizontalCollage(2, 0)),
	}); err != nil {
		t.Fatalf("an ordinary picture should still decode: %v", err)
	}
}

// pngHeader builds the signature and IHDR chunk of a truecolour PNG, which is
// all a decoder needs to read to report the picture's size.
func pngHeader(width, height uint32) []byte {
	ihdr := make([]byte, 0, 17)
	ihdr = append(ihdr, []byte("IHDR")...)
	ihdr = binary.BigEndian.AppendUint32(ihdr, width)
	ihdr = binary.BigEndian.AppendUint32(ihdr, height)
	ihdr = append(ihdr, 8, 2, 0, 0, 0) // 8 bits per channel, truecolour, no interlacing

	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, uint32(len(ihdr)-4))
	out = append(out, ihdr...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(ihdr))
}

// collageEngine serves a three-panel collage as the level's task picture.
func collageEngine(t *testing.T, data []byte, contentType string) *stubEngine {
	t.Helper()
	engine := playableEngine()
	engine.resource = &encx.Resource{
		URL:         "/upload/collage.png",
		ContentType: contentType,
		Data:        data,
	}
	return engine
}

func TestImageInfoReportsThePartsWithoutSendingThePicture(t *testing.T) {
	engine := collageEngine(t, encodePNGBytes(t, horizontalCollage(3, 0)), "image/png")
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly, ReadCacheTTL: time.Minute})
	tool, ok := catalog.Lookup(toolImageInfo)
	if !ok {
		t.Fatal("the catalog should expose an image measurer")
	}

	result := tool.Execute(context.Background(), map[string]any{"url": "/upload/collage.png"})
	if result.IsError {
		t.Fatalf("measuring a picture should succeed, got %q", result.ForLLM)
	}
	// Surveying a heavy picture has to stay cheap, or a model cannot afford to
	// look before it crops.
	if len(result.Media) != 0 {
		t.Fatalf("measuring should not attach the picture, got %d media entries", len(result.Media))
	}

	var view imageInfoView
	if err := json.Unmarshal([]byte(result.ForLLM), &view); err != nil {
		t.Fatalf("the measurement should be JSON: %v", err)
	}
	if view.Width != 3*panelWidth+2*seamWidth || view.Height != panelHeight {
		t.Fatalf("the picture is %dx%d, got %dx%d",
			3*panelWidth+2*seamWidth, panelHeight, view.Width, view.Height)
	}
	if view.Split == nil || len(view.Split.Parts) != 3 {
		t.Fatalf("the three panels should be suggested as parts, got %+v", view.Split)
	}
	if view.Split.Method != "detected" || view.Split.Axis != string(axisHorizontal) {
		t.Fatalf("the split should be detected along the horizontal axis, got %+v", view.Split)
	}
	if !strings.Contains(view.Note, toolSplitImage) {
		t.Fatalf("the note should point at the splitter, got %q", view.Note)
	}

	// The measurement is small, so unlike the picture itself it is worth keeping
	// for the rest of the turn.
	tool.Execute(context.Background(), map[string]any{"url": "/upload/collage.png"})
	if got := countCalls(engine, "FetchResource"); got != 1 {
		t.Fatalf("a repeated measurement should come from the cache, got %d fetches", got)
	}
}

func TestSplitImageAttachesEveryPartSeparately(t *testing.T) {
	engine := collageEngine(t, encodePNGBytes(t, horizontalCollage(3, 0)), "image/png")
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, ok := catalog.Lookup(toolSplitImage)
	if !ok {
		t.Fatal("the catalog should expose an image splitter")
	}

	result := tool.Execute(context.Background(), map[string]any{"url": "/upload/collage.png"})
	if result.IsError {
		t.Fatalf("splitting a collage should succeed, got %q", result.ForLLM)
	}
	// One image per panel is the whole point: three clues the model can read
	// apart, at their own resolution.
	if len(result.Media) != 3 {
		t.Fatalf("each panel should arrive as its own image, got %d", len(result.Media))
	}
	for index, inline := range result.Media {
		if !strings.HasPrefix(inline, "data:image/") {
			t.Fatalf("part %d should be an inline image, got %.40q", index+1, inline)
		}
		if !partDecodes(t, inline, panelHeight) {
			t.Fatalf("part %d should decode at the panel's own height", index+1)
		}
	}

	var view splitResultView
	if err := json.Unmarshal([]byte(result.ForLLM), &view); err != nil {
		t.Fatalf("the split should be JSON: %v", err)
	}
	if len(view.Parts) != 3 {
		t.Fatalf("every part should be described, got %+v", view.Parts)
	}
	for _, part := range view.Parts {
		if !part.Attached {
			t.Fatalf("part %d should be attached, got %+v", part.Index, part)
		}
	}
	if view.Parts[0].Box.X != 0 || view.Parts[1].Box.X != panelWidth+seamWidth/2 {
		t.Fatalf("the boxes should follow the separators, got %+v", view.Parts)
	}
	if view.Method != "detected" {
		t.Fatalf("the picture's own separators should have been used, got %q", view.Method)
	}
}

func TestSplitImageCanReturnOnePart(t *testing.T) {
	engine := collageEngine(t, encodePNGBytes(t, horizontalCollage(3, 0)), "image/png")
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, _ := catalog.Lookup(toolSplitImage)

	result := tool.Execute(context.Background(), map[string]any{"url": "/upload/collage.png", "part": 2})
	if result.IsError {
		t.Fatalf("asking for one part should succeed, got %q", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("only the requested part should be attached, got %d", len(result.Media))
	}

	var view splitResultView
	if err := json.Unmarshal([]byte(result.ForLLM), &view); err != nil {
		t.Fatalf("the split should be JSON: %v", err)
	}
	// The other parts are still described, so the model can come back for them
	// without splitting the picture again.
	if len(view.Parts) != 3 {
		t.Fatalf("every part should still be described, got %+v", view.Parts)
	}
	if view.Parts[1].Index != 2 || !view.Parts[1].Attached {
		t.Fatalf("part 2 should be the attached one, got %+v", view.Parts)
	}
	if view.Parts[0].Attached || view.Parts[2].Attached {
		t.Fatalf("the parts that were not asked for should not claim to be attached, got %+v", view.Parts)
	}

	if result = tool.Execute(context.Background(), map[string]any{
		"url": "/upload/collage.png", "part": 9,
	}); !result.IsError {
		t.Fatal("a part that does not exist should be refused")
	}
}

func TestSplitImageBoundsWhatItReturns(t *testing.T) {
	engine := collageEngine(t, encodePNGBytes(t, horizontalCollage(3, 0)), "image/png")
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, _ := catalog.Lookup(toolSplitImage)

	result := tool.Execute(context.Background(), map[string]any{
		"url": "/upload/collage.png", "max_dimension": 50,
	})
	if result.IsError {
		t.Fatalf("a smaller limit should succeed, got %q", result.ForLLM)
	}
	var view splitResultView
	if err := json.Unmarshal([]byte(result.ForLLM), &view); err != nil {
		t.Fatalf("the split should be JSON: %v", err)
	}
	for _, part := range view.Parts {
		if part.Width > 50 || part.Height > 50 {
			t.Fatalf("part %d should fit inside 50 pixels, got %dx%d", part.Index, part.Width, part.Height)
		}
	}

	if result = tool.Execute(context.Background(), map[string]any{
		"url": "/upload/collage.png", "max_dimension": maxAllowedDimension + 1,
	}); !result.IsError {
		t.Fatal("a limit past what a provider accepts should be refused")
	}
}

func TestSplitImageIsNotCached(t *testing.T) {
	engine := collageEngine(t, encodePNGBytes(t, horizontalCollage(3, 0)), "image/png")
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly, ReadCacheTTL: time.Minute})
	tool, _ := catalog.Lookup(toolSplitImage)

	tool.Execute(context.Background(), map[string]any{"url": "/upload/collage.png"})
	tool.Execute(context.Background(), map[string]any{"url": "/upload/collage.png"})

	// Three fragments of base64 are worse to keep than to fetch again.
	if got := countCalls(engine, "FetchResource"); got != 2 {
		t.Fatalf("split results should not be memoized, got %d fetches", got)
	}
}

func TestCropImageReturnsTheRequestedRectangle(t *testing.T) {
	engine := collageEngine(t, encodeJPEGBytes(t, horizontalCollage(3, 0), 90), "image/jpeg")
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, ok := catalog.Lookup(toolCropImage)
	if !ok {
		t.Fatal("the catalog should expose an image cropper")
	}

	result := tool.Execute(context.Background(), map[string]any{
		"url": "/upload/collage.png", "x": "0", "y": 0, "width": panelWidth, "height": panelHeight,
	})
	if result.IsError {
		t.Fatalf("cropping should succeed, got %q", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("the crop should be attached as media, got %v", len(result.Media))
	}

	var view cropResultView
	if err := json.Unmarshal([]byte(result.ForLLM), &view); err != nil {
		t.Fatalf("the crop should be JSON: %v", err)
	}
	if view.Crop != (cropBox{X: 0, Y: 0, Width: panelWidth, Height: panelHeight}) {
		t.Fatalf("the crop should be the first panel, got %+v", view.Crop)
	}
	// The model has to be told the size it is looking at to ask for the next
	// crop, and the size it asked about to notice a clamp.
	if view.Source.Width != 3*panelWidth+2*seamWidth {
		t.Fatalf("the source size should be reported, got %+v", view.Source)
	}
}

func TestCropImageAcceptsPercentagesAndAnOpenEnd(t *testing.T) {
	engine := collageEngine(t, encodePNGBytes(t, horizontalCollage(2, 0)), "image/png")
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, _ := catalog.Lookup(toolCropImage)

	// The right half of a picture whose size the model has not measured.
	result := tool.Execute(context.Background(), map[string]any{
		"url": "/upload/collage.png", "x": "50%",
	})
	if result.IsError {
		t.Fatalf("a percentage should be accepted, got %q", result.ForLLM)
	}

	var view cropResultView
	if err := json.Unmarshal([]byte(result.ForLLM), &view); err != nil {
		t.Fatalf("the crop should be JSON: %v", err)
	}
	width := 2*panelWidth + seamWidth
	if view.Crop.X != width/2 {
		t.Fatalf("50%% of %d is %d, got %d", width, width/2, view.Crop.X)
	}
	// An omitted width means "to the far edge", which is what "the right half"
	// means without a measurement.
	if view.Crop.Width != width-width/2 || view.Crop.Height != panelHeight {
		t.Fatalf("the crop should reach both far edges, got %+v", view.Crop)
	}
}

func TestCropImageRefusesWhatItCannotDecode(t *testing.T) {
	engine := collageEngine(t, []byte("<html>not a picture</html>"), "text/html")
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})

	for _, name := range []string{toolImageInfo, toolCropImage, toolSplitImage} {
		tool, _ := catalog.Lookup(name)
		result := tool.Execute(context.Background(), map[string]any{"url": "/upload/task.html"})
		if !result.IsError {
			t.Fatalf("%s should refuse a file that is not a picture, got %q", name, result.ForLLM)
		}
		if len(result.Media) != 0 {
			t.Fatalf("%s must not attach media for a refused fetch", name)
		}
	}
}

func TestPictureToolsAreAvailableUnderEveryPolicy(t *testing.T) {
	// Looking at a picture changes nothing in the game, so a read-only session
	// has to keep the tools that make a picture readable at all.
	catalog := newTestCatalog(t, playableEngine(), Options{Policy: PolicyReadonly})
	names := toolNames(catalog.Tools())
	for _, want := range []string{toolImageInfo, toolCropImage, toolSplitImage} {
		if !contains(names, want) {
			t.Fatalf("%s should be exposed under a read-only policy, got %v", want, names)
		}
		tool, _ := catalog.Lookup(want)
		if tool.Mutating() {
			t.Fatalf("%s is a read tool and must not ask for confirmation", want)
		}
	}
}

func TestSystemPromptAddendumTellsTheModelToSplitCollages(t *testing.T) {
	catalog := newTestCatalog(t, playableEngine(), Options{Policy: PolicyReadonly})
	addendum := catalog.SystemPromptAddendum()
	for _, want := range []string{toolSplitImage, toolCropImage, toolImageInfo, "collage"} {
		if !strings.Contains(addendum, want) {
			t.Fatalf("the addendum should mention %q so the model knows to cut a collage up:\n%s",
				want, addendum)
		}
	}
}

// TestLiveCollageSplit checks the detector against a real task picture, which
// no synthetic fixture can stand in for: authors assemble collages in an image
// editor and save them as JPEG, and the separator has to survive that.
//
//	ENCX_LIVE_IMAGE=https://d1.endata.cx/data/games/78368/Spoiler_7_2_fgsetass.jpg \
//	  go test ./agenttools/ -run TestLiveCollageSplit -v
func TestLiveCollageSplit(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("ENCX_LIVE_IMAGE"))
	if rawURL == "" {
		t.Skip("set ENCX_LIVE_IMAGE to a task picture to check the detector against real content")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Skipf("%s is unreachable: %v", rawURL, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, encx.DefaultResourceMaxBytes))
	if err != nil {
		t.Fatalf("read %s: %v", rawURL, err)
	}

	source, err := decodeResource(&encx.Resource{
		URL:         rawURL,
		ContentType: response.Header.Get("Content-Type"),
		Data:        body,
	})
	if err != nil {
		t.Fatalf("decode %s: %v", rawURL, err)
	}
	t.Logf("%s is %s %dx%d, %d bytes", rawURL, source.format, source.width(), source.height(), source.bytes)

	plan, err := planSplit(source.img, axisAuto, 0)
	if err != nil {
		t.Fatalf("no parts were detected in %s: %v", rawURL, err)
	}
	for index, rect := range plan.rects(source.img.Bounds()) {
		box := newCropBox(source.img.Bounds(), rect)
		t.Logf("part %d along %s: x=%d y=%d %dx%d",
			index+1, plan.Axis, box.X, box.Y, box.Width, box.Height)
	}
	if plan.Method != "detected" || len(plan.Segments) < 2 {
		t.Fatalf("a collage should be split on its own separators, got %q with %d parts",
			plan.Method, len(plan.Segments))
	}
}

// partDecodes checks that an attached part is a real image of the expected
// height — a part that does not decode would reach the provider as a broken
// image part.
func partDecodes(t *testing.T, inline string, wantHeight int) bool {
	t.Helper()
	_, encoded, found := strings.Cut(inline, ";base64,")
	if !found {
		return false
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return false
	}
	return img.Bounds().Dy() == wantHeight
}
