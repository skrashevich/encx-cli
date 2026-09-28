package encx

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"
)

// TestLiveAdminUploadGameImage creates and deletes its own game. Run it with
// ENCX_LIVE_DOMAIN, ENCX_LIVE_LOGIN, ENCX_LIVE_PASSWORD and ENCX_LIVE_ENGINE
// (legacy or new) to check the real upload endpoint and the returned media URL.
func TestLiveAdminUploadGameImage(t *testing.T) {
	mode := EngineMode(os.Getenv("ENCX_LIVE_ENGINE"))
	if mode != EngineLegacy && mode != EngineNew {
		t.Skip("set ENCX_LIVE_ENGINE=legacy or new")
	}
	var c *Client
	if mode == EngineLegacy {
		c = liveLegacyClient(t)
	} else {
		c = liveClient(t)
	}
	params := scratchGameParams()
	params.Title = fmt.Sprintf("encx image upload test %d", time.Now().UnixNano())
	params.Description = "temporary game for verifying image upload"
	if mode == EngineLegacy {
		params.ZoneID = 1
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	gameID, err := c.AdminCreateGame(ctx, params)
	if err != nil {
		t.Fatalf("create scratch game: %v", err)
	}
	t.Logf("created temporary game %d on %s", gameID, os.Getenv(liveDomainEnv))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		if err := c.AdminDeleteGame(cleanupCtx, gameID); err != nil {
			t.Errorf("delete temporary game %d: %v", gameID, err)
			return
		}
		t.Logf("deleted temporary game %d", gameID)
	})

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 1, color.RGBA{G: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("upload-test-%d.png", time.Now().UnixNano())
	file, err := c.AdminUploadGameImage(ctx, gameID, name, encoded.Bytes())
	if err != nil {
		t.Fatalf("upload %q to game %d: %v", name, gameID, err)
	}
	if file.Name != name || file.URL == "" {
		t.Fatalf("upload returned %+v", file)
	}
	t.Logf("uploaded image: name=%s url=%s", file.Name, file.URL)
	resource, err := c.FetchResource(ctx, file.URL, ResourceOptions{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("download uploaded image: %v", err)
	}
	if !bytes.Equal(resource.Data, encoded.Bytes()) {
		t.Fatalf("downloaded image differs: got %d bytes, want %d", len(resource.Data), encoded.Len())
	}
	if _, err := c.AdminUploadGameImage(ctx, gameID, name, encoded.Bytes()); err == nil {
		t.Fatal("duplicate image upload was accepted")
	}
	t.Logf("image readback verified (%d bytes); duplicate name rejected", encoded.Len())
}
