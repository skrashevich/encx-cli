package main

import (
	"context"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
)

// cmdRouteMap is the command-line twin of the osm_route_map tool. Unlike the
// agent it needs no request to be explicit: it always renders the scheme and
// writes it to a file (replacing an existing out file), and never edits a
// game.
func cmdRouteMap(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	req := routeMapRequest{}
	var out string
	for _, arg := range args {
		key, val, ok := strings.Cut(arg, "=")
		if !ok {
			fatal("Arguments must be in key=value format. Got: %s", arg)
		}
		num := func() int {
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				fatal("%s must be a non-negative number, got %q", key, val)
			}
			return n
		}
		switch strings.ToLower(key) {
		case "to":
			req.to = val
		case "from":
			req.from = val
		case "profile":
			req.profile = val
		case "level":
			if req.levelNumber = num(); req.levelNumber == 0 {
				fatal("level must be a positive number")
			}
		case "zoom":
			req.zoom = num()
		case "width":
			req.width = num()
		case "height":
			req.height = num()
		case "out":
			out = val
		default:
			fatal("Unknown key %q. Supported: level, to, from, profile, zoom, width, height, out", key)
		}
	}
	if routeMapNeedsScenario(req) {
		requireGameId(cfg)
		requireAdminAuth(ctx, cfg, client)
	}

	img, result := buildRouteMap(ctx, cfg, client, req)
	if out == "" {
		out = routeMapFileName("", result["profile"].(string))
	}
	f, err := os.Create(out)
	if err != nil {
		fatal("Cannot save map: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		os.Remove(out)
		fatal("Cannot save map: %v", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(out)
		fatal("Cannot save map: %v", err)
	}
	if abs, err := filepath.Abs(out); err == nil {
		out = abs
	}
	result["path"] = out

	if cfg.jsonOutput {
		outputJSON(result)
		return
	}
	fmt.Printf("Scheme saved: %s (%dx%d, zoom %d)\n", out, result["width"], result["height"], result["zoom"])
	if route, ok := result["route"].(map[string]any); ok {
		fmt.Printf("Route (%s): %.0f m, about %.0f min\n", result["profile"], route["distance_m"], math.Ceil(route["duration_s"].(float64)/60))
	}
	for _, key := range []string{"start_skipped", "route_error"} {
		if note, ok := result[key]; ok {
			fmt.Printf("Note: %v\n", note)
		}
	}
	if n, ok := result["tile_errors"]; ok {
		fmt.Printf("Warning: %v map tiles failed to load; run again for a complete map\n", n)
	}
	fmt.Printf("Finish: %s\n", result["finish_coords"])
	fmt.Printf("OpenStreetMap: %s\n", result["osm_url"])
	fmt.Printf("Keep the credit \"%s\" under the image.\n", osmAttribution)
}
