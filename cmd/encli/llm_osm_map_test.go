package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

type fakeOSM struct {
	mu          sync.Mutex
	userAgents  []string
	routePaths  []string
	geocoded    []string
	routeStatus int
}

// withFakeOSM points every OSM endpoint at one test server that returns plain
// grey tiles, a fixed geocoding hit, and a two-segment route.
func withFakeOSM(t *testing.T) *fakeOSM {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	fake := &fakeOSM{routeStatus: http.StatusOK}
	prevInterval := nominatimInterval
	nominatimInterval = 0
	t.Cleanup(func() { nominatimInterval = prevInterval })
	var tile bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, osmTileSize, osmTileSize))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	if err := png.Encode(&tile, img); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.userAgents = append(fake.userAgents, r.UserAgent())
		fake.mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/tiles/"):
			w.Write(tile.Bytes())
		case r.URL.Path == "/search":
			fake.mu.Lock()
			fake.geocoded = append(fake.geocoded, r.URL.Query().Get("q"))
			fake.mu.Unlock()
			if r.URL.Query().Get("q") == "nowhere" {
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprint(w, `[{"lat":"55.7539","lon":"37.6208","display_name":"Красная площадь, Москва"}]`)
		case strings.Contains(r.URL.Path, "/route/v1/"):
			fake.mu.Lock()
			fake.routePaths = append(fake.routePaths, r.URL.Path)
			fake.mu.Unlock()
			fake.mu.Lock()
			status := fake.routeStatus
			fake.mu.Unlock()
			if status != http.StatusOK {
				http.Error(w, "down", status)
				return
			}
			fmt.Fprint(w, `{"code":"Ok","routes":[{"distance":1234.4,"duration":900.6,"geometry":{"coordinates":[[37.6100,55.7500],[37.6150,55.7520],[37.6208,55.7539]]}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	prevTile, prevNominatim, prevOSRM := osmTileURL, nominatimBaseURL, osrmBaseURL
	osmTileURL = func(z, x, y int) string { return fmt.Sprintf("%s/tiles/%d/%d/%d.png", srv.URL, z, x, y) }
	nominatimBaseURL = srv.URL
	osrmBaseURL = func(profile string) string { return srv.URL + "/routed-" + profile }
	t.Cleanup(func() { osmTileURL, nominatimBaseURL, osrmBaseURL = prevTile, prevNominatim, prevOSRM })
	return fake
}

func runRouteMapTool(t *testing.T, args string) map[string]any {
	t.Helper()
	raw := executeToolCallSafe(t.Context(), &config{}, nil, &llmSession{securityMode: SecurityModeFull}, "osm_route_map", args)
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("osm_route_map returned undecodable JSON %q: %v", raw, err)
	}
	return got
}

func decodeMapFile(t *testing.T, path string) image.Image {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("map is not a PNG: %v", err)
	}
	return img
}

func sameColor(a color.Color, b color.RGBA) bool {
	r, g, bl, _ := a.RGBA()
	return uint8(r>>8) == b.R && uint8(g>>8) == b.G && uint8(bl>>8) == b.B
}

func TestParseLatLon(t *testing.T) {
	for _, s := range []string{"55.7539, 37.6208", "55.7539 37.6208", "55.7539;37.6208", "-33.9,151"} {
		if _, ok, err := parseLatLon(s); !ok || err != nil {
			t.Errorf("parseLatLon(%q) = ok %v, err %v", s, ok, err)
		}
	}
	if _, ok, err := parseLatLon("95.1, 37.6"); !ok || err == nil {
		t.Error("latitude above 90 was accepted")
	}
	if _, ok, _ := parseLatLon("Москва, Красная площадь"); ok {
		t.Error("an address was parsed as coordinates")
	}
}

func TestOSMRouteMapDestinationOnly(t *testing.T) {
	fake := withFakeOSM(t)
	got := runRouteMapTool(t, `{"to":"55.7539, 37.6208"}`)
	if got["error"] != nil {
		t.Fatalf("tool failed: %v", got)
	}
	if len(fake.geocoded) != 0 || len(fake.routePaths) != 0 {
		t.Fatalf("coordinates-only call hit geocoder %v or router %v", fake.geocoded, fake.routePaths)
	}
	path := got["path"].(string)
	img := decodeMapFile(t, path)
	if img.Bounds().Dx() != osmFrameLong || img.Bounds().Dy() != osmFrameShort {
		t.Fatalf("map size = %v, want landscape %dx%d", img.Bounds(), osmFrameLong, osmFrameShort)
	}
	// A lone finish is centred, with the frame lowered by half a pin so the
	// pin's tip sits just below the middle.
	tipX, tipY := osmFrameLong/2, osmFrameShort/2+osmPinHeight/2
	if !sameColor(img.At(tipX, tipY), endColor) {
		t.Fatalf("pin tip pixel = %v, want black finish", img.At(tipX, tipY))
	}
	html := got["task_html"].(string)
	if got["finish_coords"] != "55.7539, 37.6208" || !strings.Contains(html, `<span class="coords">55.7539, 37.6208</span>`) ||
		!strings.HasSuffix(html, `<img src="IMAGE_URL">`) || strings.Contains(html, "OpenStreetMap") {
		t.Fatalf("finish_coords = %v, task_html = %s", got["finish_coords"], html)
	}
	if got["zoom"].(float64) != osmDefaultZoom {
		t.Fatalf("zoom = %v", got["zoom"])
	}
	if !strings.Contains(got["osm_url"].(string), "mlat=55.753900") {
		t.Fatalf("osm_url = %v", got["osm_url"])
	}
	for _, ua := range fake.userAgents {
		if ua != agentUserAgent {
			t.Fatalf("request sent User-Agent %q", ua)
		}
	}
	if name, _, err := readAgentImage(path); err != nil || !strings.HasPrefix(name, "dohod-") {
		t.Fatalf("admin_upload_image cannot read the map: %q, %v", name, err)
	}
}

func TestOSMRouteMapWalkingRouteFromAddress(t *testing.T) {
	fake := withFakeOSM(t)
	got := runRouteMapTool(t, `{"to":"Красная площадь","from":"55.7500,37.6100","profile":"foot","width":400,"height":300,"name":"shema"}`)
	if got["error"] != nil {
		t.Fatalf("tool failed: %v", got)
	}
	if len(fake.geocoded) != 1 || fake.geocoded[0] != "Красная площадь" {
		t.Fatalf("geocoded = %v", fake.geocoded)
	}
	if len(fake.routePaths) != 1 || !strings.HasPrefix(fake.routePaths[0], "/routed-foot/route/v1/driving/37.610000,55.750000;37.620800,55.753900") {
		t.Fatalf("route request = %v", fake.routePaths)
	}
	route := got["route"].(map[string]any)
	if route["distance_m"].(float64) != 1234 || route["duration_s"].(float64) != 901 {
		t.Fatalf("route = %v", route)
	}
	if !strings.Contains(got["osm_url"].(string), "engine=fossgis_osrm_foot") {
		t.Fatalf("osm_url = %v", got["osm_url"])
	}
	path := got["path"].(string)
	if !strings.HasSuffix(path, "/maps/shema.png") {
		t.Fatalf("path = %q", path)
	}
	img := decodeMapFile(t, path)
	if img.Bounds().Dx() != 400 || img.Bounds().Dy() != 300 {
		t.Fatalf("map size = %v", img.Bounds())
	}
	if !strings.Contains(got["task_html"].(string), "Финальная точка дохода") {
		t.Fatalf("walking task_html = %v", got["task_html"])
	}
	var start, end, line bool
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.At(x, y)
			start = start || sameColor(c, startColor)
			end = end || sameColor(c, endColor)
			line = line || sameColor(c, walkColor)
		}
	}
	if !start || !end || !line {
		t.Fatalf("map is missing start %v, end %v, or route %v", start, end, line)
	}

	again := runRouteMapTool(t, `{"to":"55.7539,37.6208","name":"shema.png"}`)
	if again["error"] == nil || !strings.Contains(fmt.Sprint(again["error"]), "already exists") {
		t.Fatalf("existing map was overwritten: %v", again)
	}
}

func TestOSMRouteMapKeepsPointsWhenRoutingFails(t *testing.T) {
	fake := withFakeOSM(t)
	fake.mu.Lock()
	fake.routeStatus = http.StatusServiceUnavailable
	fake.mu.Unlock()
	got := runRouteMapTool(t, `{"to":"55.7539,37.6208","from":"55.7500,37.6100","profile":"car"}`)
	if got["error"] != nil || got["route_error"] == nil || got["route"] != nil {
		t.Fatalf("result = %v", got)
	}
	if !strings.HasPrefix(fake.routePaths[0], "/routed-car/") {
		t.Fatalf("car route = %v", fake.routePaths)
	}
	decodeMapFile(t, got["path"].(string))
}

func TestOSMRouteMapRejectsUnknownPlaceAndProfile(t *testing.T) {
	withFakeOSM(t)
	if got := runRouteMapTool(t, `{"to":"nowhere"}`); !strings.Contains(fmt.Sprint(got["error"]), "not found") {
		t.Fatalf("unknown address result = %v", got)
	}
	if got := runRouteMapTool(t, `{"to":"55.75,37.62","profile":"boat"}`); !strings.Contains(fmt.Sprint(got["error"]), "car or foot") {
		t.Fatalf("unknown profile result = %v", got)
	}
}

func TestFitZoomKeepsPointsInFrame(t *testing.T) {
	points := []osmPoint{{Lat: 55.75, Lon: 37.61}, {Lat: 55.80, Lon: 37.70}}
	z := fitZoom(points, 800, 600)
	minX, minY, maxX, maxY := pixelBounds(points, z)
	if maxX-minX > 800-2*osmMapPadding || maxY-minY > 600-2*osmMapPadding {
		t.Fatalf("zoom %d does not fit the points", z)
	}
	minX, minY, maxX, maxY = pixelBounds(points, z+1)
	if maxX-minX <= 800-2*osmMapPadding && maxY-minY <= 600-2*osmMapPadding {
		t.Fatalf("zoom %d is not the closest fit", z)
	}
}

func TestOSMRouteMapToolIsReadOnly(t *testing.T) {
	if isMutationTool("osm_route_map") || !shouldExposeTool("osm_route_map", SecurityModeReadonly) {
		t.Fatal("osm_route_map must stay available in read-only mode")
	}
	for _, tool := range getTools() {
		if tool.Function.Name == "osm_route_map" {
			return
		}
	}
	t.Fatal("osm_route_map is missing from agent tools")
}

func TestAttributionBoxIsLight(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 100))
	drawAttribution(img)
	r, g, b, _ := img.At(299, 99).RGBA()
	if r>>8 < 200 || g>>8 < 200 || b>>8 < 200 {
		t.Fatalf("attribution background = %v, want light", img.At(299, 99))
	}
}

func TestOSMRouteMapFrameFollowsRouteShape(t *testing.T) {
	withFakeOSM(t)
	// The fake route runs mostly west-east, so the frame is landscape; a
	// north-south pair without a route gets a portrait one.
	wide := runRouteMapTool(t, `{"to":"55.7539,37.6208","from":"55.7500,37.6100","profile":"car"}`)
	if wide["width"].(float64) <= wide["height"].(float64) {
		t.Fatalf("west-east route got %vx%v", wide["width"], wide["height"])
	}
	if !strings.Contains(wide["task_html"].(string), "Доедьте") {
		t.Fatalf("car task_html = %v", wide["task_html"])
	}
	pts := []osmPoint{{Lat: 55.76, Lon: 37.62}, {Lat: 55.75, Lon: 37.621}}
	if w, h := frameSize(pts, 0, 0); w >= h {
		t.Fatalf("north-south frame = %dx%d, want portrait", w, h)
	}
	if w, h := frameSize(pts, 400, 0); w != 400 || h != osmFrameLong {
		t.Fatalf("north-south frame with width 400 = %dx%d", w, h)
	}
}

func TestOSMRouteMapToleratesSomeTileFailures(t *testing.T) {
	withFakeOSM(t)
	good := osmTileURL
	var calls int
	var mu sync.Mutex
	osmTileURL = func(z, x, y int) string {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return strings.Replace(good(z, x, y), "/tiles/", "/missing/", 1)
		}
		return good(z, x, y)
	}
	got := runRouteMapTool(t, `{"to":"55.7539,37.6208"}`)
	if got["error"] != nil || got["tile_errors"] != float64(1) {
		t.Fatalf("result = %v", got)
	}
	osmTileURL = func(z, x, y int) string { return strings.Replace(good(z, x, y), "/tiles/", "/missing/", 1) }
	if got := runRouteMapTool(t, `{"to":"55.7539,37.6208","name":"none"}`); got["error"] == nil {
		t.Fatalf("a map without any tiles was accepted: %v", got)
	}
}

func TestDrawTilesFetchesWrappedTileOnce(t *testing.T) {
	fake := withFakeOSM(t)
	canvas := image.NewRGBA(image.Rect(0, 0, 1280, 512))
	if failed, err := drawTiles(t.Context(), canvas, 1, -384, 0); err != nil || failed != 0 {
		t.Fatalf("drawTiles = %d, %v", failed, err)
	}
	// Zoom 1 has 2x2 tiles; a 1280 px frame repeats them but fetches each once.
	if len(fake.userAgents) != 4 {
		t.Fatalf("fetched %d tiles, want 4", len(fake.userAgents))
	}
}

func TestRouteMapFileNameStripsReservedCharacters(t *testing.T) {
	cases := map[string]string{
		"ab:c.png":     "abc.png",
		"../x\\y?.jpg": "xy.png",
		"line\nbreak":  "linebreak.png",
		"шема 1":       "шема 1.png",
	}
	for in, want := range cases {
		if got := routeMapFileName(in, "foot"); got != want {
			t.Errorf("routeMapFileName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := routeMapFileName(strings.Repeat("я", 300), "foot"); len([]rune(got)) != 104 {
		t.Errorf("long name kept %d runes", len([]rune(got)))
	}
	if got := routeMapFileName(" :? ", "foot"); !strings.HasPrefix(got, "dohod-") {
		t.Errorf("empty walking name = %q", got)
	}
	if got := routeMapFileName("", "car"); !strings.HasPrefix(got, "doezd-") {
		t.Errorf("empty driving name = %q", got)
	}
}

func TestWaitNominatimSlotSpacesRequests(t *testing.T) {
	prevInterval, prevLast := nominatimInterval, nominatimLast
	t.Cleanup(func() { nominatimInterval, nominatimLast = prevInterval, prevLast })
	nominatimInterval = 50 * time.Millisecond
	nominatimLast = time.Time{}
	start := time.Now()
	for range 3 {
		if err := waitNominatimSlot(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("three requests took %v, want at least 100ms", elapsed)
	}
}

// Levels shaped like demo.en.cx game 32055: a briefing with the parking, a
// walking scheme, a task at the reached spot, and a scheme inside it.
var walkingGameLevels = []scenario.Level{
	{Number: 1, Tasks: []string{`Парковка: <span class="coords">57.618854, 39.872352</span>, запасная <span class="coords">57.619326, 39.870779</span>.`}},
	{Number: 2, Tasks: []string{`Пройдите от красной точки до чёрной.<br/><span class="coords">57.6153, 39.8669</span><br/><img src="map01.png">`}},
	{Number: 3, Tasks: []string{`Задание на месте <span class="coords">57.6153, 39.8669</span>`}},
	{Number: 4, Tasks: []string{`Без координат`}},
	{Number: 5, Tasks: []string{`Схема внутри локации <span class="coords">57.61535, 39.86695</span><img src="map02.png">`}},
	{Number: 6, Tasks: []string{`Иди на 57.61495, 39.8669 и ищи код 12.5`}},
}

func TestLevelSchemePointsFollowTheTeam(t *testing.T) {
	// Out of order on purpose: the scenario order is the level number.
	levels := slices.Clone(walkingGameLevels)
	slices.Reverse(levels)
	cases := []struct {
		level         int
		finish, start *osmPoint
	}{
		// The start of the first scheme is the last parking in the briefing.
		{2, &osmPoint{Lat: 57.6153, Lon: 39.8669}, &osmPoint{Lat: 57.619326, Lon: 39.870779}},
		{5, &osmPoint{Lat: 57.61535, Lon: 39.86695}, &osmPoint{Lat: 57.6153, Lon: 39.8669}},
		// Bare pairs count when a task has no coords spans.
		{6, &osmPoint{Lat: 57.61495, Lon: 39.8669}, &osmPoint{Lat: 57.61535, Lon: 39.86695}},
		{1, &osmPoint{Lat: 57.618854, Lon: 39.872352}, nil},
		// No coordinates of its own, but the team still starts somewhere.
		{4, nil, &osmPoint{Lat: 57.6153, Lon: 39.8669}},
	}
	for _, c := range cases {
		finish, start, err := levelSchemePoints(levels, c.level)
		if err != nil || !reflect.DeepEqual(finish, c.finish) || !reflect.DeepEqual(start, c.start) {
			t.Errorf("level %d: finish %v start %v err %v", c.level, finish, start, err)
		}
	}
	if _, _, err := levelSchemePoints(levels, 9); err == nil {
		t.Error("a missing level produced a scheme")
	}
	embedded := `<div id="map"></div><script>ymaps.ready(function() { new ymaps.Map('map', {center: [57.621571, 39.889096]}) })</script>`
	for _, text := range []string{embedded, `код 12.5 и 3, 4`, `номер 123.45678, 12.34567`} {
		if pts := taskPoints(text); len(pts) != 0 {
			t.Errorf("taskPoints(%q) = %v, want none", text, pts)
		}
	}
	if pts := taskPoints(`<span class='coords big'>57.6153, 39.8669</span>`); len(pts) != 1 {
		t.Errorf("a coords span with other markup was missed: %v", pts)
	}
}

func withScenario(t *testing.T, levels []scenario.Level) {
	t.Helper()
	prev := routeMapScenario
	routeMapScenario = func(context.Context, *encx.Client, int) ([]scenario.Level, error) { return levels, nil }
	t.Cleanup(func() { routeMapScenario = prev })
}

// Level mode is exercised through the builder the tool and the command
// share; the tool itself adds only the admin sign-in.
func TestRouteMapLevelMode(t *testing.T) {
	fake := withFakeOSM(t)
	withScenario(t, walkingGameLevels)
	cfg := &config{gameId: 32055}
	build := func(req routeMapRequest) map[string]any {
		_, result := buildRouteMap(t.Context(), cfg, nil, req)
		return result
	}

	walk := build(routeMapRequest{levelNumber: 2, profile: "foot"})
	if walk["from"] == nil || walk["route"] == nil || walk["finish_coords"] != "57.6153, 39.8669" || walk["level_number"] != 2 {
		t.Fatalf("level 2 = %v", walk)
	}
	inside := build(routeMapRequest{levelNumber: 5, profile: "foot"})
	if inside["start_skipped"] == nil || inside["from"] != nil {
		t.Fatalf("level 5 = %v", inside)
	}
	// A level without coordinates takes the finish the user gives.
	named := build(routeMapRequest{levelNumber: 4, to: "57.61495, 39.8669", profile: "foot"})
	if named["from"] == nil || named["finish_coords"] != "57.61495, 39.8669" {
		t.Fatalf("level 4 with to = %v", named)
	}
	// Points the user names are drawn as given, however close.
	near := build(routeMapRequest{to: "57.61535, 39.86695", from: "57.6153, 39.8669", profile: "foot"})
	if near["from"] == nil || near["start_skipped"] != nil {
		t.Fatalf("explicit close points = %v", near)
	}
	if len(fake.routePaths) != 3 {
		t.Fatalf("routes = %v", fake.routePaths)
	}
}

func TestOSMRouteMapWarnsWhenIncomplete(t *testing.T) {
	fake := withFakeOSM(t)
	fake.mu.Lock()
	fake.routeStatus = http.StatusServiceUnavailable
	fake.mu.Unlock()
	got := runRouteMapTool(t, `{"to":"55.7539,37.6208","from":"55.7500,37.6100","profile":"foot"}`)
	if got["warning"] == nil {
		t.Fatalf("a scheme without its route carried no warning: %v", got)
	}
	if got := runRouteMapTool(t, `{"profile":"foot"}`); !strings.Contains(fmt.Sprint(got["error"]), "level number") {
		t.Fatalf("missing finish result = %v", got)
	}
}

func TestRouteMapCommandAlwaysWritesTheFile(t *testing.T) {
	fake := withFakeOSM(t)
	out := filepath.Join(t.TempDir(), "scheme.png")
	cmdRouteMap(t.Context(), &config{jsonOutput: true}, nil, []string{
		"to=55.7539,37.6208", "from=55.7500,37.6100", "profile=foot", "out=" + out,
	})
	img := decodeMapFile(t, out)
	if img.Bounds().Dx() < minRouteMapSide || len(fake.routePaths) != 1 || !strings.Contains(fake.routePaths[0], "/routed-foot/") {
		t.Fatalf("size %v, routes %v", img.Bounds(), fake.routePaths)
	}
}

func TestAgentAddsSchemesOnlyWhenAsked(t *testing.T) {
	for _, tool := range getTools() {
		if tool.Function.Name == "osm_route_map" && !strings.Contains(tool.Function.Description, "Only when the user explicitly asks") {
			t.Fatal("osm_route_map description does not restrict it to explicit requests")
		}
	}
	src, err := os.ReadFile("agent_runner.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "ONLY when the user explicitly asks for a scheme") {
		t.Fatal("the system prompt does not restrict route schemes to explicit requests")
	}
}
