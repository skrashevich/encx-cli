package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

// Route maps are built from public OpenStreetMap services. Their usage
// policies require an identifying User-Agent, modest request volume, and
// visible "© OpenStreetMap contributors" attribution on the rendered map.
var (
	osmTileURL = func(z, x, y int) string {
		return fmt.Sprintf("https://tile.openstreetmap.org/%d/%d/%d.png", z, x, y)
	}
	nominatimBaseURL = "https://nominatim.openstreetmap.org"
	// FOSSGIS OSRM, the router behind openstreetmap.org directions; unlike the
	// OSRM demo server it serves a walking profile.
	osrmBaseURL = func(profile string) string {
		return "https://routing.openstreetmap.de/routed-" + profile
	}
	osmHTTPClient = &http.Client{Timeout: 20 * time.Second}

	// Nominatim allows at most one request per second.
	nominatimInterval = time.Second
	nominatimMu       sync.Mutex
	nominatimLast     time.Time
)

const (
	osmTileSize        = 256
	osmMaxZoom         = 19
	osmDefaultZoom     = 17
	osmMaxAutoZoom     = 18
	osmMaxLat          = 85.0511287798
	osmMapPadding      = 48
	osmTileConcurrency = 2
	osmAttribution     = "© OpenStreetMap contributors"
	osmPinHeight       = 40
	minRouteMapSide    = 256
	maxRouteMapSide    = 1280
	osmFrameLong       = 900
	osmFrameShort      = 700
)

type osmPoint struct {
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
	Label string  `json:"label,omitempty"`
}

type osmRoute struct {
	DistanceM float64
	DurationS float64
	Path      []osmPoint
}

var latLonPattern = regexp.MustCompile(`^\s*(-?\d{1,3}(?:\.\d+)?)\s*[,;\s]\s*(-?\d{1,3}(?:\.\d+)?)\s*$`)

// parseLatLon accepts "lat,lon" (the order Google and Yandex maps copy).
// ok is false when s does not look like a coordinate pair at all.
func parseLatLon(s string) (p osmPoint, ok bool, err error) {
	m := latLonPattern.FindStringSubmatch(s)
	if m == nil {
		return osmPoint{}, false, nil
	}
	lat, _ := strconv.ParseFloat(m[1], 64)
	lon, _ := strconv.ParseFloat(m[2], 64)
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return osmPoint{}, true, fmt.Errorf("coordinates out of range: %q (expected \"lat,lon\")", s)
	}
	return osmPoint{Lat: lat, Lon: lon}, true, nil
}

func osmGet(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", agentUserAgent)
	req.Header.Set("Accept-Language", "ru,en")
	resp, err := osmHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, summarizeDebugText(string(body), 200))
	}
	return body, nil
}

// resolvePlace turns "lat,lon" or a free-form address into a point; only
// addresses reach Nominatim.
func resolvePlace(ctx context.Context, place string) (osmPoint, error) {
	place = strings.TrimSpace(place)
	if place == "" {
		return osmPoint{}, errors.New("place is empty")
	}
	if p, ok, err := parseLatLon(place); ok {
		return p, err
	}
	if err := waitNominatimSlot(ctx); err != nil {
		return osmPoint{}, err
	}
	params := url.Values{}
	params.Set("q", place)
	params.Set("format", "jsonv2")
	params.Set("limit", "1")
	body, err := osmGet(ctx, nominatimBaseURL+"/search?"+params.Encode(), 1<<20)
	if err != nil {
		return osmPoint{}, fmt.Errorf("geocoding %q: %w", place, err)
	}
	var results []struct {
		Lat         string `json:"lat"`
		Lon         string `json:"lon"`
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		return osmPoint{}, fmt.Errorf("geocoding %q: bad response: %w", place, err)
	}
	if len(results) == 0 {
		return osmPoint{}, fmt.Errorf("address not found in OpenStreetMap: %q; pass coordinates as \"lat,lon\"", place)
	}
	lat, errLat := strconv.ParseFloat(results[0].Lat, 64)
	lon, errLon := strconv.ParseFloat(results[0].Lon, 64)
	if errLat != nil || errLon != nil {
		return osmPoint{}, fmt.Errorf("geocoding %q: bad coordinates in response", place)
	}
	return osmPoint{Lat: lat, Lon: lon, Label: results[0].DisplayName}, nil
}

// waitNominatimSlot spaces geocoding requests by nominatimInterval.
func waitNominatimSlot(ctx context.Context) error {
	// Reserve the next slot under the lock and wait outside it, so a caller
	// whose context is cancelled leaves at once instead of queueing.
	nominatimMu.Lock()
	slot := time.Now()
	if next := nominatimLast.Add(nominatimInterval); next.After(slot) {
		slot = next
	}
	nominatimLast = slot
	nominatimMu.Unlock()
	wait := time.Until(slot)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// normalizeRouteProfile maps user wording to the FOSSGIS profile names. A
// walking scheme (схема дохода) is the default; a driving one is a схема
// доезда or парковки.
func normalizeRouteProfile(profile string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", "foot", "walking", "walk", "pedestrian":
		return "foot", nil
	case "car", "driving", "auto":
		return "car", nil
	}
	return "", fmt.Errorf("unknown route profile %q: use car or foot", profile)
}

func fetchRoute(ctx context.Context, profile string, from, to osmPoint) (*osmRoute, error) {
	reqURL := fmt.Sprintf("%s/route/v1/driving/%s,%s;%s,%s?overview=full&geometries=geojson",
		osrmBaseURL(profile),
		strconv.FormatFloat(from.Lon, 'f', 6, 64), strconv.FormatFloat(from.Lat, 'f', 6, 64),
		strconv.FormatFloat(to.Lon, 'f', 6, 64), strconv.FormatFloat(to.Lat, 'f', 6, 64))
	body, err := osmGet(ctx, reqURL, 8<<20)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Routes  []struct {
			Distance float64 `json:"distance"`
			Duration float64 `json:"duration"`
			Geometry struct {
				Coordinates [][]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("bad routing response: %w", err)
	}
	if payload.Code != "Ok" || len(payload.Routes) == 0 {
		return nil, fmt.Errorf("no route found (%s %s)", payload.Code, payload.Message)
	}
	r := payload.Routes[0]
	route := &osmRoute{DistanceM: r.Distance, DurationS: r.Duration}
	for _, c := range r.Geometry.Coordinates {
		if len(c) >= 2 {
			route.Path = append(route.Path, osmPoint{Lat: c[1], Lon: c[0]})
		}
	}
	return route, nil
}

// worldPixel projects a point to Web Mercator pixels at zoom z.
func worldPixel(p osmPoint, z int) (float64, float64) {
	lat := math.Max(-osmMaxLat, math.Min(osmMaxLat, p.Lat))
	scale := float64(osmTileSize) * math.Exp2(float64(z))
	x := (p.Lon + 180) / 360 * scale
	rad := lat * math.Pi / 180
	y := (1 - math.Log(math.Tan(rad)+1/math.Cos(rad))/math.Pi) / 2 * scale
	return x, y
}

func pixelBounds(points []osmPoint, z int) (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, p := range points {
		x, y := worldPixel(p, z)
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	return
}

// fitZoom picks the closest zoom at which every point fits inside the frame
// with a margin for markers.
func fitZoom(points []osmPoint, width, height int) int {
	if len(points) < 2 {
		return osmDefaultZoom
	}
	for z := osmMaxAutoZoom; z > 1; z-- {
		minX, minY, maxX, maxY := pixelBounds(points, z)
		if maxX-minX <= float64(width-2*osmMapPadding) && maxY-minY <= float64(height-2*osmMapPadding) {
			return z
		}
	}
	return 1
}

func clampSide(v, def int) int {
	if v <= 0 {
		return def
	}
	return max(minRouteMapSide, min(maxRouteMapSide, v))
}

// frameSize follows the shape of the route, as hand-made schemes do: a
// north-south walk gets a portrait frame. An explicit side wins; a missing
// one comes from that orientation.
func frameSize(points []osmPoint, width, height int) (int, int) {
	defW, defH := osmFrameLong, osmFrameShort
	minX, minY, maxX, maxY := pixelBounds(points, osmMaxAutoZoom)
	if maxY-minY > maxX-minX {
		defW, defH = osmFrameShort, osmFrameLong
	}
	return clampSide(width, defW), clampSide(height, defH)
}

// Colours of Encounter walking schemes: the start is red, the finish black,
// a walk is a purple dotted line and a drive a solid one.
var (
	startColor = color.RGBA{235, 70, 45, 255}
	endColor   = color.RGBA{40, 40, 40, 255}
	walkColor  = color.RGBA{140, 80, 245, 255}
	driveColor = color.RGBA{0, 102, 255, 255}
)

const walkDotSpacing = 14

// renderRouteMap draws the route and start/finish pins over OSM tiles and
// reports how many tiles failed to load. A nil start shows only the finish.
func renderRouteMap(ctx context.Context, to osmPoint, from *osmPoint, route []osmPoint, profile string, width, height, zoom int) (*image.RGBA, int, int, error) {
	// The router snaps both ends to the nearest path; joining them to the
	// real points shows the last steps to each pin.
	var line []osmPoint
	if from != nil && len(route) > 0 {
		line = append(append(append(line, *from), route...), to)
	}
	points := append([]osmPoint{to}, line...)
	if from != nil {
		points = append(points, *from)
	}
	if zoom <= 0 {
		zoom = fitZoom(points, width, height)
	}
	zoom = max(1, min(osmMaxZoom, zoom))

	minX, minY, maxX, maxY := pixelBounds(points, zoom)
	left := math.Floor((minX+maxX)/2 - float64(width)/2)
	// Pins rise above their point, so the frame is shifted down a little.
	top := math.Floor((minY+maxY)/2 - float64(height)/2 - osmPinHeight/2)

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{color.RGBA{230, 230, 230, 255}}, image.Point{}, draw.Src)
	tileErrors, err := drawTiles(ctx, canvas, zoom, left, top)
	if err != nil {
		return nil, 0, 0, err
	}

	pts := make([][2]float64, len(line))
	for i, p := range line {
		x, y := worldPixel(p, zoom)
		pts[i] = [2]float64{x - left, y - top}
	}
	if len(pts) > 1 {
		drawPolyline(canvas, pts, 5, color.White)
		if profile == "foot" {
			drawDots(canvas, pts, walkDotSpacing, 3.5, walkColor)
		} else {
			drawPolyline(canvas, pts, 3, driveColor)
		}
	}
	if from != nil {
		x, y := worldPixel(*from, zoom)
		drawPin(canvas, x-left, y-top, startColor)
	}
	x, y := worldPixel(to, zoom)
	drawPin(canvas, x-left, y-top, endColor)
	drawAttribution(canvas)
	return canvas, zoom, tileErrors, nil
}

// drawTiles fetches each distinct tile once, even when the world wraps
// several times across a low-zoom frame. Tiles that fail stay background;
// only a map with no tiles at all is an error.
func drawTiles(ctx context.Context, canvas *image.RGBA, zoom int, left, top float64) (int, error) {
	n := 1 << zoom
	b := canvas.Bounds()
	x0, y0 := int(math.Floor(left/osmTileSize)), int(math.Floor(top/osmTileSize))
	x1 := int(math.Floor((left + float64(b.Dx()) - 1) / osmTileSize))
	y1 := int(math.Floor((top + float64(b.Dy()) - 1) / osmTileSize))

	positions := map[image.Point][]image.Point{}
	for ty := y0; ty <= y1; ty++ {
		if ty < 0 || ty >= n {
			continue
		}
		for tx := x0; tx <= x1; tx++ {
			key := image.Pt(((tx%n)+n)%n, ty)
			positions[key] = append(positions[key], image.Pt(tx*osmTileSize-int(left), ty*osmTileSize-int(top)))
		}
	}

	var (
		mu       sync.Mutex
		failed   int
		firstErr error
		wg       sync.WaitGroup
	)
	sem := make(chan struct{}, osmTileConcurrency)
	for key, at := range positions {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			body, err := osmGet(ctx, osmTileURL(zoom, key.X, key.Y), 2<<20)
			var tile image.Image
			if err == nil {
				tile, _, err = image.Decode(bytes.NewReader(body))
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = fmt.Errorf("map tile %d/%d/%d: %w", zoom, key.X, key.Y, err)
				}
				return
			}
			for _, p := range at {
				draw.Draw(canvas, image.Rect(p.X, p.Y, p.X+osmTileSize, p.Y+osmTileSize), tile, tile.Bounds().Min, draw.Src)
			}
		})
	}
	wg.Wait()
	if len(positions) > 0 && failed == len(positions) {
		return failed, firstErr
	}
	return failed, nil
}

func fillCircle(img *image.RGBA, cx, cy, r float64, c color.Color) {
	b := img.Bounds()
	for y := max(b.Min.Y, int(cy-r)-1); y <= min(b.Max.Y-1, int(cy+r)+1); y++ {
		for x := max(b.Min.X, int(cx-r)-1); x <= min(b.Max.X-1, int(cx+r)+1); x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, c)
			}
		}
	}
}

// segmentVisible reports whether a segment thickened by r can touch img, so
// off-frame parts of a long route cost nothing.
func segmentVisible(img *image.RGBA, a, b [2]float64, r float64) bool {
	bounds := img.Bounds()
	return math.Max(a[0], b[0])+r >= float64(bounds.Min.X) && math.Min(a[0], b[0])-r <= float64(bounds.Max.X) &&
		math.Max(a[1], b[1])+r >= float64(bounds.Min.Y) && math.Min(a[1], b[1])-r <= float64(bounds.Max.Y)
}

func drawPolyline(img *image.RGBA, pts [][2]float64, r float64, c color.Color) {
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if !segmentVisible(img, a, b, r) {
			continue
		}
		steps := max(1, int(math.Hypot(b[0]-a[0], b[1]-a[1])))
		for s := 0; s <= steps; s++ {
			t := float64(s) / float64(steps)
			fillCircle(img, a[0]+(b[0]-a[0])*t, a[1]+(b[1]-a[1])*t, r, c)
		}
	}
}

// drawDots places evenly spaced dots along the whole polyline, carrying the
// spacing across vertices so bends keep the rhythm.
func drawDots(img *image.RGBA, pts [][2]float64, spacing, r float64, c color.Color) {
	next := 0.0
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		length := math.Hypot(b[0]-a[0], b[1]-a[1])
		if length == 0 {
			continue
		}
		if !segmentVisible(img, a, b, r) {
			next = math.Mod(next-length, spacing)
			if next < 0 {
				next += spacing
			}
			continue
		}
		for ; next <= length; next += spacing {
			t := next / length
			fillCircle(img, a[0]+(b[0]-a[0])*t, a[1]+(b[1]-a[1])*t, r, c)
		}
		next -= length
	}
}

// drawPin draws a teardrop map pin whose tip marks the exact point.
func drawPin(img *image.RGBA, x, y float64, c color.Color) {
	const headR = 14.0
	headY := y - osmPinHeight + headR
	shape := func(grow float64, c color.Color) {
		fillCircle(img, x, headY, headR+grow, c)
		// The tail narrows from most of the head's width to the tip.
		for py := headY; py <= y-4; py++ {
			fillCircle(img, x, py, 0.8*headR*(y-4-py)/(y-4-headY)+grow, c)
		}
	}
	shape(2, color.White)
	shape(0, c)
	fillCircle(img, x, headY, 5, color.White)
	fillCircle(img, x, y, 4.5, color.White)
	fillCircle(img, x, y, 3, c)
}

// drawAttribution writes the credit OSM requires. basicfont has no "©"
// glyph, so the sign is drawn as a ring around a "c".
func drawAttribution(img *image.RGBA) {
	face := basicfont.Face7x13
	text := strings.TrimPrefix(osmAttribution, "©")
	textW := font.MeasureString(face, text).Ceil()
	const signW, pad = 13, 4
	b := img.Bounds()
	box := image.Rect(b.Max.X-textW-signW-2*pad, b.Max.Y-13-2*pad, b.Max.X, b.Max.Y)
	draw.Draw(img, box, &image.Uniform{color.NRGBA{255, 255, 255, 210}}, image.Point{}, draw.Over)

	ink := color.RGBA{40, 40, 40, 255}
	cx, cy := float64(box.Min.X+pad+6), float64(box.Min.Y+pad+6)+0.5
	for a := 0.0; a < 2*math.Pi; a += 0.05 {
		img.Set(int(cx+6*math.Cos(a)), int(cy+6*math.Sin(a)), ink)
	}
	d := font.Drawer{Dst: img, Src: image.NewUniform(ink), Face: face}
	d.Dot = fixed.P(int(cx)-3, box.Min.Y+pad+10)
	d.DrawString("c")
	d.Dot = fixed.P(box.Min.X+pad+signW, box.Min.Y+pad+11)
	d.DrawString(text)
}

var windowsReservedName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])$`)

// routeMapFileName drops characters that are control codes or reserved on
// Windows (where ":" would open an NTFS stream) and bounds the length.
func routeMapFileName(name, profile string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`:*?"<>|\`, r) {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	name = sanitizeUploadFilename(name)
	if name == "file" || strings.Trim(name, ". ") == "" {
		prefix := "dohod-"
		if profile == "car" {
			prefix = "doezd-"
		}
		return prefix + uploadPrefix() + ".png"
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if r := []rune(base); len(r) > 100 {
		base = string(r[:100])
	}
	if windowsReservedName.MatchString(base) {
		base = "_" + base
	}
	return base + ".png"
}

// saveRouteMap stores the PNG under the chat uploads root so
// admin_upload_image accepts it; an existing file is never overwritten.
func saveRouteMap(img image.Image, name, profile string) (string, error) {
	dir := filepath.Join(chatUploadsRoot(), "maps")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, routeMapFileName(name, profile))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("file %s already exists; choose another name", filepath.Base(path))
		}
		return "", err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func osmCoord(v float64) string { return strconv.FormatFloat(v, 'f', 6, 64) }

// finishCoords formats a point the way task texts show it: "57.6153, 39.8669".
func finishCoords(p osmPoint) string {
	short := func(v float64) string { return strconv.FormatFloat(math.Round(v*1e6)/1e6, 'f', -1, 64) }
	return short(p.Lat) + ", " + short(p.Lon)
}

const (
	redBadge   = `<span style="background-color:red; color: white; padding:5px; border-radius:5px;">красной точки</span>`
	blackBadge = `<span style="background-color:black; color: white; padding:5px; border-radius:5px;">чёрной</span>`
)

// routeTaskHTML renders the level text Encounter walking games use with a
// scheme: which pin to walk between, the finish duplicated as coordinates,
// the image, and the OSM credit. imageURL is the uploaded map's URL.
func routeTaskHTML(profile string, hasStart bool, finish osmPoint, imageURL string) string {
	var lead string
	switch {
	case !hasStart:
		lead = `Доберитесь до <span style="background-color:black; color: white; padding:5px; border-radius:5px;">чёрной точки</span> на карте. Финальная точка продублирована координатами.`
	case profile == "foot":
		lead = "Пройдите от " + redBadge + " на карте до " + blackBadge + ". Финальная точка дохода продублирована координатами."
	default:
		lead = "Доедьте от " + redBadge + " на карте до " + blackBadge + ". Финальная точка доезда продублирована координатами."
	}
	return lead + `<br/><span class="coords">` + finishCoords(finish) + `</span><br/><img src="` + html.EscapeString(imageURL) +
		`"><br/><small>` + osmAttribution + `</small>`
}

const routeMapImagePlaceholder = "IMAGE_URL"

// sameSpotMeters is how close a start may be to the finish before the scheme
// shows the finish alone: the scheme then guides within one location.
const sameSpotMeters = 15

var (
	coordsSpanPattern = regexp.MustCompile(`class=["']?[^"'>]*\bcoords\b[^>]*>\s*([^<]+?)\s*<`)
	// A bare pair needs four decimals so ordinary numbers in a task are not
	// mistaken for coordinates.
	bareCoordsPattern = regexp.MustCompile(`(?:^|[^\d.])(-?\d{1,2}\.\d{4,}\s*,\s*-?\d{1,3}\.\d{4,})`)
	// Code is not text a team reads: an embedded map's centre in a script
	// is not a place the team is sent to.
	scriptPattern = regexp.MustCompile(`(?is)<script\b.*?</script>`)
	stylePattern  = regexp.MustCompile(`(?is)<style\b.*?</style>`)
)

// taskPoints lists the coordinates a task gives, in order. Encounter tasks
// mark them with <span class="coords">; a task without such spans falls back
// to bare "lat, lon" pairs.
func taskPoints(task string) []osmPoint {
	task = stylePattern.ReplaceAllString(scriptPattern.ReplaceAllString(task, ""), "")
	var raw []string
	for _, m := range coordsSpanPattern.FindAllStringSubmatch(task, -1) {
		raw = append(raw, m[1])
	}
	if len(raw) == 0 {
		for _, m := range bareCoordsPattern.FindAllStringSubmatch(task, -1) {
			raw = append(raw, m[1])
		}
	}
	var points []osmPoint
	for _, r := range raw {
		if p, ok, err := parseLatLon(html.UnescapeString(r)); ok && err == nil {
			points = append(points, p)
		}
	}
	return points
}

// levelSchemePoints applies the rule of walking games: a level's scheme ends
// at the first coordinates of that level and starts where the team last was,
// the last coordinates of any earlier level.
func levelSchemePoints(levels []scenario.Level, number int) (finish, start *osmPoint, err error) {
	sorted := slices.Clone(levels)
	slices.SortStableFunc(sorted, func(a, b scenario.Level) int { return a.Number - b.Number })
	for _, level := range sorted {
		var points []osmPoint
		for _, task := range level.Tasks {
			points = append(points, taskPoints(task)...)
		}
		if level.Number == number {
			// A level without coordinates still has a start; the caller may
			// know the finish.
			if len(points) > 0 {
				finish = &points[0]
			}
			return finish, start, nil
		}
		if len(points) > 0 {
			start = &points[len(points)-1]
		}
	}
	return nil, nil, fmt.Errorf("level %d not found in the scenario", number)
}

// distanceMeters is accurate to a few percent at city scale, plenty for
// telling one spot from the next.
func distanceMeters(a, b osmPoint) float64 {
	dLat := (a.Lat - b.Lat) * 111320
	dLon := (a.Lon - b.Lon) * 111320 * math.Cos(a.Lat*math.Pi/180)
	return math.Hypot(dLat, dLon)
}

// routeMapRequest is what both the agent tool and the route-map command ask
// for; levelNumber takes the points from the game's scenario.
type routeMapRequest struct {
	to, from, profile   string
	levelNumber         int
	zoom, width, height int
}

// buildRouteMap resolves the points, routes and renders a scheme, returning
// the image with the result fields shared by the tool and the command.
func buildRouteMap(ctx context.Context, cfg *config, client *encx.Client, req routeMapRequest) (*image.RGBA, map[string]any) {
	toPlace, fromPlace, levelNumber := req.to, req.from, req.levelNumber
	profile, err := normalizeRouteProfile(req.profile)
	if err != nil {
		fatal("%v", err)
	}

	var to osmPoint
	var from *osmPoint
	var startFromScenario bool
	if routeMapNeedsScenario(req) {
		if cfg.gameId <= 0 {
			fatal("game id is required with a level number")
		}
		levels, err := routeMapScenario(ctx, client, cfg.gameId)
		if err != nil {
			fatalEncx("Read game scenario", err)
		}
		finish, start, err := levelSchemePoints(levels, levelNumber)
		if err != nil {
			fatal("%v", err)
		}
		if finish == nil && strings.TrimSpace(toPlace) == "" {
			fatal("level %d gives no coordinates; pass the finish explicitly", levelNumber)
		}
		if finish != nil {
			to = *finish
		}
		from, startFromScenario = start, start != nil
	}
	if strings.TrimSpace(toPlace) != "" {
		if to, err = resolvePlace(ctx, toPlace); err != nil {
			fatal("Cannot resolve destination: %v", err)
		}
	} else if levelNumber <= 0 {
		fatal("a finish (to) or a level number is required")
	}
	if strings.TrimSpace(fromPlace) != "" {
		p, err := resolvePlace(ctx, fromPlace)
		if err != nil {
			fatal("Cannot resolve start: %v", err)
		}
		from, startFromScenario = &p, false
	}

	// A previous location at the finish itself means the scheme guides within
	// one location, so only the finish is drawn. Points the user names are
	// drawn as given.
	var startNote string
	if startFromScenario && distanceMeters(*from, to) <= sameSpotMeters {
		startNote = fmt.Sprintf("the previous location is within %d m of the finish; the scheme shows the finish only", sameSpotMeters)
		from = nil
	}

	result := map[string]any{"profile": profile, "to": to}
	if levelNumber > 0 {
		result["level_number"] = levelNumber
	}
	if startNote != "" {
		result["start_skipped"] = startNote
	}
	osmURL := fmt.Sprintf("https://www.openstreetmap.org/?mlat=%s&mlon=%s", osmCoord(to.Lat), osmCoord(to.Lon))
	var routePath []osmPoint
	framePoints := []osmPoint{to}
	if from != nil {
		result["from"] = *from
		framePoints = append(framePoints, *from)
		osmURL = fmt.Sprintf("https://www.openstreetmap.org/directions?engine=fossgis_osrm_%s&route=%s,%s;%s,%s",
			profile, osmCoord(from.Lat), osmCoord(from.Lon), osmCoord(to.Lat), osmCoord(to.Lon))
		route, err := fetchRoute(ctx, profile, *from, to)
		if err != nil {
			result["route_error"] = err.Error()
		} else {
			routePath = route.Path
			framePoints = append(framePoints, route.Path...)
			result["route"] = map[string]any{
				"distance_m": math.Round(route.DistanceM),
				"duration_s": math.Round(route.DurationS),
			}
		}
	}

	width, height := frameSize(framePoints, req.width, req.height)
	img, zoom, tileErrors, err := renderRouteMap(ctx, to, from, routePath, profile, width, height, req.zoom)
	if err != nil {
		fatal("Cannot render map: %v", err)
	}
	result["width"] = width
	result["height"] = height
	result["zoom"] = zoom
	result["osm_url"] = osmURL
	result["attribution"] = osmAttribution
	result["finish_coords"] = finishCoords(to)
	result["task_html"] = routeTaskHTML(profile, from != nil, to, routeMapImagePlaceholder)
	if tileErrors > 0 {
		result["tile_errors"] = tileErrors
	}
	if result["route_error"] != nil || tileErrors > 0 {
		result["warning"] = "the scheme is incomplete (no route or missing map tiles); tell the user and do not put it into a level without their consent"
	}
	return img, result
}

// routeMapScenario reads a game's levels in one request: reading them level
// by level through the editor trips the engine's ensure_human check.
var routeMapScenario = func(ctx context.Context, client *encx.Client, gameID int) ([]scenario.Level, error) {
	doc, err := client.GetGameScenario(ctx, gameID)
	if err != nil {
		return nil, err
	}
	return doc.Levels, nil
}

// routeMapNeedsScenario reports whether some point must come from the game.
func routeMapNeedsScenario(req routeMapRequest) bool {
	return req.levelNumber > 0 && (strings.TrimSpace(req.to) == "" || strings.TrimSpace(req.from) == "")
}

func toolOSMRouteMap(ctx context.Context, cfg *config, client *encx.Client, req routeMapRequest, name string) {
	if routeMapNeedsScenario(req) && cfg.gameId > 0 {
		requireAdminAuth(ctx, cfg, client)
	}
	img, result := buildRouteMap(ctx, cfg, client, req)
	path, err := saveRouteMap(img, name, result["profile"].(string))
	if err != nil {
		fatal("Cannot save map: %v", err)
	}
	result["path"] = path
	outputJSON(result)
}
