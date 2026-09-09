package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// A 72-page scenario PDF once reached the model as 240 bytes under
// "truncated": false, and the model invented the rest of the game.
func TestPrepareToolResultKeepsDocumentContent(t *testing.T) {
	content := strings.Repeat("сценарий уровня, коды и подсказки. ", 3000)
	raw, err := json.Marshal(map[string]any{
		"path": "/tmp/scenario.pdf", "num_pages": 72,
		"read": len(content), "truncated": false, "content": content,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(raw) <= 8000 {
		t.Fatalf("fixture is %d bytes; it must exceed the generic summarizer threshold", len(raw))
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(prepareToolResultForLLM("read_pdf_file", string(raw))), &got); err != nil {
		t.Fatalf("decode prepared result: %v", err)
	}

	delivered, _ := got["content"].(string)
	if len(delivered) != maxToolContentForLLM && len(delivered) != maxToolContentForLLM-1 {
		t.Fatalf("delivered %d bytes, want the full %d-byte budget", len(delivered), maxToolContentForLLM)
	}
	if !strings.HasPrefix(content, delivered) {
		t.Fatal("delivered text is not a prefix of the document")
	}
	if !utf8.ValidString(delivered) {
		t.Fatal("delivered text was cut mid-rune")
	}
	if got["truncated"] != true {
		t.Fatalf("truncated = %v, want the payload to admit it was cut", got["truncated"])
	}
	if read := getAnyInt(got["read"]); read != len(delivered) {
		t.Fatalf("read = %d, want the delivered length %d", read, len(delivered))
	}
	if omitted := getAnyInt(got["omitted_bytes"]); omitted != len(content)-len(delivered) {
		t.Fatalf("omitted_bytes = %d, want %d", omitted, len(content)-len(delivered))
	}
	hint, _ := got["hint"].(string)
	if !strings.Contains(hint, "page=") {
		t.Fatalf("hint = %q, want it to name the paging argument", hint)
	}
}

func TestPrepareToolResultLeavesSmallDocumentsAlone(t *testing.T) {
	raw := `{"path":"/tmp/a.txt","size":11,"offset":0,"read":11,"truncated":false,"content":"привет мир"}`
	if got := prepareToolResultForLLM("read_local_file", raw); got != raw {
		t.Fatalf("result = %q, want it untouched", got)
	}
}

func TestPrepareToolResultPointsAtTheNextOffset(t *testing.T) {
	content := strings.Repeat("x", maxToolContentForLLM+500)
	raw, _ := json.Marshal(map[string]any{
		"path": "/tmp/a.txt", "size": len(content), "offset": 100,
		"read": len(content), "truncated": false, "content": content,
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(prepareToolResultForLLM("read_local_file", string(raw))), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	hint, _ := got["hint"].(string)
	if !strings.Contains(hint, "offset=") || !strings.Contains(hint, strconv.Itoa(100+maxToolContentForLLM)) {
		t.Fatalf("hint = %q, want it to name the next offset", hint)
	}
}

func TestPrepareToolResultKeepsWikipediaExtract(t *testing.T) {
	extract := strings.Repeat("текст статьи. ", 6000)
	raw, _ := json.Marshal(map[string]any{
		"lang": "ru", "title": "Тест", "pageid": 1, "url": "u", "extract": extract, "redirect": false,
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(prepareToolResultForLLM("wikipedia_article", string(raw))), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	delivered, _ := got["extract"].(string)
	if len(delivered) < maxToolContentForLLM-4 {
		t.Fatalf("extract delivered %d bytes, want the full budget", len(delivered))
	}
	if got["title"] != "Тест" {
		t.Fatalf("title = %v, want the metadata preserved", got["title"])
	}
}

func TestSummarizeDebugTextCutsOnRuneBoundary(t *testing.T) {
	// "ф" is two bytes, so an odd limit lands mid-rune.
	got := summarizeDebugText(strings.Repeat("ф", 50), 11)
	if !utf8.ValidString(got) {
		t.Fatalf("summarizeDebugText produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("summary = %q, want the truncation marker", got)
	}
}
