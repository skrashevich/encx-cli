package agenttools

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
)

// Level content is author-written HTML. Images are the part an agent cannot read
// from the text at all, and on many levels they carry the whole task.
var (
	imgSrcPattern  = regexp.MustCompile(`(?i)<img[^>]+src\s*=\s*["']([^"']+)["']`)
	hrefImgPattern = regexp.MustCompile(`(?i)<a[^>]+href\s*=\s*["']([^"']+\.(?:png|jpe?g|gif|webp|bmp))["']`)
)

// levelImage is one picture referenced by level content.
type levelImage struct {
	URL   string `json:"url"`
	Where string `json:"where"`
}

// collectLevelImages pulls every image reference out of a level, recording where
// each one came from so the agent can tell a task picture from a bonus picture.
func collectLevelImages(level *encx.Level) []levelImage {
	if level == nil {
		return nil
	}

	seen := map[string]string{}
	add := func(html, where string) {
		for _, url := range extractImageURLs(html) {
			if _, ok := seen[url]; !ok {
				seen[url] = where
			}
		}
	}

	for _, task := range level.Tasks {
		add(task.TaskText, "task")
	}
	if level.Task != nil {
		add(level.Task.TaskText, "task")
	}
	for _, bonus := range level.Bonuses {
		add(bonus.Task, "bonus:"+bonus.Name)
		add(bonus.Help, "bonus:"+bonus.Name)
	}
	for _, help := range level.Helps {
		if help.HelpText != nil {
			add(*help.HelpText, "hint")
		}
	}
	for _, help := range level.PenaltyHelps {
		if help.HelpText != nil {
			add(*help.HelpText, "penalty_hint")
		}
	}
	for _, message := range level.Messages {
		add(message.MessageText, "message")
	}

	images := make([]levelImage, 0, len(seen))
	for url, where := range seen {
		images = append(images, levelImage{URL: url, Where: where})
	}
	sort.Slice(images, func(i, j int) bool { return images[i].URL < images[j].URL })
	return images
}

func extractImageURLs(html string) []string {
	if strings.TrimSpace(html) == "" {
		return nil
	}
	var urls []string
	for _, pattern := range []*regexp.Regexp{imgSrcPattern, hrefImgPattern} {
		for _, match := range pattern.FindAllStringSubmatch(html, -1) {
			if len(match) > 1 {
				if url := strings.TrimSpace(match[1]); url != "" {
					urls = append(urls, url)
				}
			}
		}
	}
	return urls
}

// dataURL renders a fetched resource as an inline image for a multimodal model.
//
// PicoClaw's providers turn a "data:image/…" entry in ToolResult.Media into an
// image_url part, so returning one here is what lets the model actually see the
// picture instead of being handed a link it cannot open.
func dataURL(resource *encx.Resource) (string, error) {
	if resource == nil || len(resource.Data) == 0 {
		return "", fmt.Errorf("the resource is empty")
	}
	contentType := strings.ToLower(strings.TrimSpace(resource.ContentType))
	if !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("%s is %s, not an image", resource.URL, resource.ContentType)
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(resource.Data), nil
}
