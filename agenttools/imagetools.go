package agenttools

import (
	"context"
	"fmt"
	"image"
)

// The picture tools are the only ones whose answer is an image rather than a
// projection of engine state, so their bodies live here instead of inline in
// the catalog.

// imageSizeView is the pixel size of a picture.
type imageSizeView struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// imageInfoView answers "what am I looking at, and where are its parts".
type imageInfoView struct {
	URL    string     `json:"url"`
	Format string     `json:"format"`
	Width  int        `json:"width"`
	Height int        `json:"height"`
	Bytes  int        `json:"bytes"`
	Split  *splitView `json:"suggested_split,omitempty"`
	Note   string     `json:"note,omitempty"`
}

type splitView struct {
	Axis   string    `json:"axis"`
	Method string    `json:"method"`
	Parts  []cropBox `json:"parts"`
}

type cropResultView struct {
	URL         string        `json:"url"`
	Source      imageSizeView `json:"source"`
	Crop        cropBox       `json:"crop"`
	Width       int           `json:"returned_width"`
	Height      int           `json:"returned_height"`
	ContentType string        `json:"content_type"`
	Note        string        `json:"note"`
}

type splitResultView struct {
	URL    string          `json:"url"`
	Source imageSizeView   `json:"source"`
	Axis   string          `json:"axis"`
	Method string          `json:"method"`
	Parts  []splitPartView `json:"parts"`
	Note   string          `json:"note"`
}

// splitPartView is one panel of a collage. A part is described even when its
// image is not attached, so the model can come back for the one it wants.
type splitPartView struct {
	Index    int     `json:"index"`
	Box      cropBox `json:"box"`
	Width    int     `json:"returned_width,omitempty"`
	Height   int     `json:"returned_height,omitempty"`
	Attached bool    `json:"attached"`
}

// fetchImage downloads and decodes the picture named by the url argument.
func fetchImage(ctx context.Context, engine Engine, args arguments) (*decodedImage, error) {
	rawURL, err := args.requireString("url")
	if err != nil {
		return nil, err
	}
	resource, err := engine.FetchResource(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return decodeResource(resource)
}

// describeImage measures a picture and reports the parts it splits into. It
// attaches nothing, so a model can survey a heavy picture cheaply before
// deciding what to actually look at.
func describeImage(ctx context.Context, engine Engine, args arguments) (any, error) {
	source, err := fetchImage(ctx, engine, args)
	if err != nil {
		return nil, err
	}
	view := &imageInfoView{
		URL:    source.url,
		Format: source.format,
		Width:  source.width(),
		Height: source.height(),
		Bytes:  source.bytes,
	}

	// Without a requested part count the only thing planSplit can refuse over is
	// a picture that has no separators, which is an answer rather than a failure.
	plan, err := planSplit(source.img, axisAuto, 0)
	if err != nil {
		view.Note = "This picture carries no separators, so it is one picture rather than a collage. " +
			"Use " + toolViewImage + " to see it whole, or " + toolCropImage + " for a detail."
		return view, nil
	}

	bounds := source.img.Bounds()
	parts := make([]cropBox, 0, len(plan.Segments))
	for _, rect := range plan.rects(bounds) {
		parts = append(parts, newCropBox(bounds, rect))
	}
	view.Split = &splitView{Axis: string(plan.Axis), Method: plan.Method, Parts: parts}
	view.Note = fmt.Sprintf(
		"This looks like %d pictures pasted into one file. Call %s to see each part on its own.",
		len(parts), toolSplitImage)
	return view, nil
}

// cropImageForModel returns one rectangle of a picture at its own resolution.
func cropImageForModel(ctx context.Context, engine Engine, args arguments) (any, error) {
	source, err := fetchImage(ctx, engine, args)
	if err != nil {
		return nil, err
	}
	maxDimension, err := args.maxDimensionArgument()
	if err != nil {
		return nil, err
	}
	rect, err := cropFromArguments(args, source)
	if err != nil {
		return nil, err
	}
	piece, err := renderFragment(source, rect, maxDimension)
	if err != nil {
		return nil, err
	}

	return toolOutput{
		value: &cropResultView{
			URL:         source.url,
			Source:      imageSizeView{Width: source.width(), Height: source.height()},
			Crop:        piece.Box,
			Width:       piece.Width,
			Height:      piece.Height,
			ContentType: piece.ContentType,
			Note:        "The crop is attached to this result.",
		},
		media: []string{piece.dataURL},
	}, nil
}

// cropFromArguments resolves the requested rectangle. An omitted width or
// height means "to the far edge", which is how a model asks for the right half
// of a picture without measuring it first.
func cropFromArguments(args arguments, source *decodedImage) (image.Rectangle, error) {
	width, height := source.width(), source.height()

	left, _, err := args.optionalPixels("x", width)
	if err != nil {
		return image.Rectangle{}, err
	}
	top, _, err := args.optionalPixels("y", height)
	if err != nil {
		return image.Rectangle{}, err
	}
	cropWidth, given, err := args.optionalPixels("width", width)
	if err != nil {
		return image.Rectangle{}, err
	}
	if !given {
		cropWidth = width - left
	}
	cropHeight, given, err := args.optionalPixels("height", height)
	if err != nil {
		return image.Rectangle{}, err
	}
	if !given {
		cropHeight = height - top
	}

	return clampCrop(source.img.Bounds(), cropBox{X: left, Y: top, Width: cropWidth, Height: cropHeight})
}

// splitImageForModel cuts a collage apart and attaches every part as its own
// image, so each panel is read on its own terms instead of being one detail in
// a picture the provider has already shrunk.
func splitImageForModel(ctx context.Context, engine Engine, args arguments) (any, error) {
	source, err := fetchImage(ctx, engine, args)
	if err != nil {
		return nil, err
	}
	maxDimension, err := args.maxDimensionArgument()
	if err != nil {
		return nil, err
	}
	requestedAxis, _ := args.optionalString("axis")
	along, err := parseSplitAxis(requestedAxis)
	if err != nil {
		return nil, err
	}
	requestedParts, _ := args.optionalInt("parts")
	plan, err := planSplit(source.img, along, requestedParts)
	if err != nil {
		return nil, err
	}

	bounds := source.img.Bounds()
	rects := plan.rects(bounds)
	only, single, err := selectedPart(args, len(rects))
	if err != nil {
		return nil, err
	}

	view := &splitResultView{
		URL:    source.url,
		Source: imageSizeView{Width: source.width(), Height: source.height()},
		Axis:   string(plan.Axis),
		Method: plan.Method,
	}
	media := make([]string, 0, len(rects))
	for index, rect := range rects {
		number := index + 1
		box := newCropBox(bounds, rect)
		if single && number != only {
			view.Parts = append(view.Parts, splitPartView{Index: number, Box: box})
			continue
		}
		piece, err := renderFragment(source, rect, maxDimension)
		if err != nil {
			return nil, err
		}
		view.Parts = append(view.Parts, splitPartView{
			Index:    number,
			Box:      box,
			Width:    piece.Width,
			Height:   piece.Height,
			Attached: true,
		})
		media = append(media, piece.dataURL)
	}
	view.Note = splitNote(plan, len(media))

	return toolOutput{value: view, media: media}, nil
}

func splitNote(plan *splitPlan, attached int) string {
	note := fmt.Sprintf(
		"%d of %d parts are attached to this result, in reading order. Read them as separate clues: "+
			"the parts of a task picture are usually unrelated to each other.",
		attached, len(plan.Segments))
	if plan.Method == "equal" {
		note += " No separators matching that many parts were found, so the picture was divided evenly " +
			"and a cut may run through content. Call " + toolImageInfo + " to see where its real parts are."
	}
	return note
}

func selectedPart(args arguments, total int) (int, bool, error) {
	number, ok := args.optionalInt("part")
	if !ok {
		return 0, false, nil
	}
	if number < 1 || number > total {
		return 0, false, fmt.Errorf(
			"part %d does not exist; this picture splits into %d parts", number, total)
	}
	return number, true, nil
}
