package agenttools

import (
	"errors"
	"fmt"
	"image"
	"math"
	"strings"
)

// A collage separates its panels with a plain strip of background: white
// between two drawings, a black bar between two photographs, page background
// around a screenshot. Those strips are the only structure the file carries,
// and they are enough to find, because a line of pixels that holds one colour
// along its whole length is not content.

const (
	// seamTolerance is the per-channel spread a line may still show and count as
	// plain background. JPEG rings around hard edges, so a pure white separator
	// arrives with a few units of noise on it.
	seamTolerance = 16
	// seamSamples bounds the work on a large picture. A separator is a separator
	// along its whole length, so sampling across it is enough to recognise one.
	seamSamples = 256
	// minPartFraction and minPartPixels keep texture from becoming a "part":
	// anything this thin is noise in the separator detection, not a panel.
	minPartFraction = 50 // a fiftieth of the side, i.e. 2%
	minPartPixels   = 8
	// maxSplitParts caps what one call may return. Eight pictures is already more
	// than a model can hold in one turn, and a file that looks like it has more
	// parts than that is textured rather than assembled.
	maxSplitParts = 8
)

// splitAxis is how the panels of a collage are laid out.
type splitAxis string

const (
	// axisHorizontal means the panels sit side by side, so the cuts are vertical.
	axisHorizontal splitAxis = "horizontal"
	// axisVertical means the panels are stacked, so the cuts are horizontal.
	axisVertical splitAxis = "vertical"
	// axisAuto lets the picture decide.
	axisAuto splitAxis = "auto"
)

func parseSplitAxis(raw string) (splitAxis, error) {
	switch splitAxis(strings.ToLower(strings.TrimSpace(raw))) {
	case "", axisAuto:
		return axisAuto, nil
	case axisHorizontal:
		return axisHorizontal, nil
	case axisVertical:
		return axisVertical, nil
	default:
		return "", fmt.Errorf(`axis has to be "horizontal", "vertical" or "auto", got %q`, raw)
	}
}

// segment is a half-open range of lines along the split axis.
type segment struct {
	from int
	to   int
}

func (s segment) size() int { return s.to - s.from }

// splitPlan is the decision about how a picture gets cut. It is kept apart from
// the cutting itself so enc_image_info can report the plan without encoding a
// single pixel.
type splitPlan struct {
	Axis splitAxis
	// Method is "detected" when the picture's own separators were used and
	// "equal" when the span was simply divided. The model has to know which:
	// dividing a collage whose panels differ in width cuts through content.
	Method   string
	Segments []segment
}

// planSplit decides how to cut a picture.
//
// parts is a request, not a promise: when the picture's own separators produce
// exactly that many panels they are used, and otherwise the span is divided
// evenly and the plan says so. Without parts, only a detected split is
// acceptable — guessing a count for the model would be worse than telling it to
// look at the whole picture first.
func planSplit(img image.Image, requested splitAxis, parts int) (*splitPlan, error) {
	if parts != 0 && (parts < 2 || parts > maxSplitParts) {
		return nil, fmt.Errorf("parts has to be between 2 and %d, got %d", maxSplitParts, parts)
	}

	along, detected, apparent := detectAlong(img, requested)
	if parts == 0 {
		if len(detected) == 0 {
			return nil, noUsableSeparators(apparent)
		}
		return &splitPlan{Axis: along, Method: "detected", Segments: detected}, nil
	}
	if len(detected) == parts {
		return &splitPlan{Axis: along, Method: "detected", Segments: detected}, nil
	}
	span := img.Bounds().Dx()
	if along == axisVertical {
		span = img.Bounds().Dy()
	}
	if span < parts {
		// Dividing further than one pixel per part would attach empty images.
		return nil, fmt.Errorf(
			"the picture measures %d pixels across the %s axis, which cannot be divided into %d parts",
			span, along, parts)
	}
	return &splitPlan{Axis: along, Method: "equal", Segments: equalSegments(span, parts)}, nil
}

// rects turns a plan into picture coordinates.
//
// The cross axis is never trimmed: a panel of a collage spans the full width or
// height of the file, and shaving its margins off would risk cutting a caption
// that sits in the background.
func (p *splitPlan) rects(bounds image.Rectangle) []image.Rectangle {
	rects := make([]image.Rectangle, 0, len(p.Segments))
	for _, part := range p.Segments {
		if p.Axis == axisVertical {
			rects = append(rects, image.Rect(
				bounds.Min.X, bounds.Min.Y+part.from, bounds.Max.X, bounds.Min.Y+part.to))
			continue
		}
		rects = append(rects, image.Rect(
			bounds.Min.X+part.from, bounds.Min.Y, bounds.Min.X+part.to, bounds.Max.Y))
	}
	return rects
}

// noUsableSeparators explains why a picture cannot be cut on its own
// structure. A file with more apparent panels than this tool returns is a
// different situation from one with none, and calling it "no separators" would
// send the model off to read a collage whole.
func noUsableSeparators(apparent int) error {
	if apparent > maxSplitParts {
		return fmt.Errorf(
			"this picture appears to fall into %d parts, more than the %d one call returns; "+
				"it is more likely textured than assembled, so crop the region you need instead",
			apparent, maxSplitParts)
	}
	return errors.New(
		"no separators were found, so the parts of this picture cannot be guessed; " +
			"pass parts to divide it evenly, or crop an explicit box")
}

// detectAlong resolves axisAuto by trying both layouts: the one that finds more
// panels wins, and a tie goes to the longer side, which is the way a collage is
// usually assembled. The third return is how many panels the winning axis
// appeared to have before the count cap, which is what makes a refusal
// explainable.
func detectAlong(img image.Image, requested splitAxis) (splitAxis, []segment, int) {
	if requested != axisAuto {
		segments, apparent := detectSegments(img, requested)
		return requested, segments, apparent
	}
	horizontal, apparentX := detectSegments(img, axisHorizontal)
	vertical, apparentY := detectSegments(img, axisVertical)
	switch {
	case len(horizontal) > len(vertical):
		return axisHorizontal, horizontal, apparentX
	case len(vertical) > len(horizontal):
		return axisVertical, vertical, apparentY
	case img.Bounds().Dy() > img.Bounds().Dx():
		return axisVertical, vertical, apparentY
	default:
		return axisHorizontal, horizontal, apparentX
	}
}

// detectSegments finds the panels of a collage along one axis, and reports how
// many it appeared to find. The panels are withheld when the picture has no
// separators, or so many that its "panels" are really texture.
func detectSegments(img image.Image, along splitAxis) ([]segment, int) {
	segments := segmentsBetweenSeams(uniformLines(img, along))
	if len(segments) < 2 || len(segments) > maxSplitParts {
		return nil, len(segments)
	}
	return segments, len(segments)
}

// segmentsBetweenSeams turns the runs of plain background into panels.
//
// A run at either end of the picture is a margin: it says where the content
// starts, not where to cut. An interior run is a separator, and the cut goes
// through its middle so neither neighbour loses an edge.
func segmentsBetweenSeams(uniform []bool) []segment {
	first, last := contentRange(uniform)
	if first < 0 {
		return nil
	}
	cuts := interiorSeamCentres(uniform, first, last)
	return mergeThinSegments(segmentsAt(cuts, first, last+1), minPartSize(len(uniform)))
}

// contentRange is the first and last line that holds something other than plain
// background, inclusive. It returns -1 when the whole picture is one colour.
func contentRange(uniform []bool) (int, int) {
	first, last := -1, -1
	for line, plain := range uniform {
		if plain {
			continue
		}
		if first < 0 {
			first = line
		}
		last = line
	}
	return first, last
}

// interiorSeamCentres is the middle of every run of plain background that lies
// between content on both sides.
func interiorSeamCentres(uniform []bool, first, last int) []int {
	var centres []int
	run := -1
	for line := first; line <= last; line++ {
		if uniform[line] {
			if run < 0 {
				run = line
			}
			continue
		}
		if run >= 0 {
			centres = append(centres, run+(line-run)/2)
			run = -1
		}
	}
	return centres
}

func segmentsAt(cuts []int, from, to int) []segment {
	segments := make([]segment, 0, len(cuts)+1)
	start := from
	for _, cut := range cuts {
		segments = append(segments, segment{from: start, to: cut})
		start = cut
	}
	return append(segments, segment{from: start, to: to})
}

// mergeThinSegments folds every piece that is too small to be a panel into its
// neighbour, so the parts still cover the content without gaps.
func mergeThinSegments(segments []segment, min int) []segment {
	merged := make([]segment, 0, len(segments))
	for _, current := range segments {
		if len(merged) > 0 && current.size() < min {
			merged[len(merged)-1].to = current.to
			continue
		}
		merged = append(merged, current)
	}
	// The first piece has no left neighbour, so it folds into the second.
	if len(merged) > 1 && merged[0].size() < min {
		merged[1].from = merged[0].from
		merged = merged[1:]
	}
	return merged
}

func minPartSize(span int) int {
	return max(span/minPartFraction, minPartPixels)
}

// equalSegments divides a span into pieces of as near the same size as the span
// allows, covering it completely.
func equalSegments(span, parts int) []segment {
	segments := make([]segment, 0, parts)
	for part := range parts {
		segments = append(segments, segment{
			from: part * span / parts,
			to:   (part + 1) * span / parts,
		})
	}
	return segments
}

// uniformLines marks every line across the picture that holds a single colour.
// For axisHorizontal a line is a column, because panels standing side by side
// are separated by columns.
func uniformLines(img image.Image, along splitAxis) []bool {
	bounds := img.Bounds()
	lines, across := bounds.Dx(), bounds.Dy()
	if along == axisVertical {
		lines, across = across, lines
	}
	offsets := sampleOffsets(across, seamSamples)
	uniform := make([]bool, lines)
	for line := range lines {
		uniform[line] = isUniformLine(img, along, line, offsets)
	}
	return uniform
}

func isUniformLine(img image.Image, along splitAxis, line int, offsets []int) bool {
	bounds := img.Bounds()
	lo := [3]uint32{math.MaxUint32, math.MaxUint32, math.MaxUint32}
	var hi [3]uint32
	for _, offset := range offsets {
		x, y := bounds.Min.X+line, bounds.Min.Y+offset
		if along == axisVertical {
			x, y = bounds.Min.X+offset, bounds.Min.Y+line
		}
		r, g, b, _ := img.At(x, y).RGBA()
		for channel, value := range [3]uint32{r >> 8, g >> 8, b >> 8} {
			if value < lo[channel] {
				lo[channel] = value
			}
			if value > hi[channel] {
				hi[channel] = value
			}
			if hi[channel]-lo[channel] > seamTolerance {
				return false
			}
		}
	}
	return true
}

// sampleOffsets picks at most count evenly spread positions along span.
func sampleOffsets(span, count int) []int {
	if span <= count {
		offsets := make([]int, span)
		for i := range offsets {
			offsets[i] = i
		}
		return offsets
	}
	offsets := make([]int, count)
	for i := range offsets {
		offsets[i] = i * span / count
	}
	return offsets
}
