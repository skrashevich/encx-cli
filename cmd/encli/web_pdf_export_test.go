package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

func TestRenderScenarioPDFContainsAllLevelContentAndImage(t *testing.T) {
	var imageBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&imageBytes, img); err != nil {
		t.Fatal(err)
	}
	doc := &scenario.Document{GameID: 82913, GameTitle: "Тест 13", Levels: []scenario.Level{{
		Number: 1, Name: "Символичный", Tasks: []string{`<p>Задание: маска</p><img src="https://example.org/mask.png">`, `<button>Карта</button><script>drawHorse()</script>`},
		Hints:        []scenario.Hint{{Text: "Подсказка: хоккей", DelaySeconds: 120}},
		PenaltyHints: []scenario.PenaltyHint{{Text: "Фото артефакта", PenaltySeconds: 180}},
		Sectors:      []scenario.Sector{{Name: "Логика", Answers: []string{"КОД13"}}},
		Bonuses:      []scenario.Bonus{{Number: 1, Name: "Скорость", Answers: []string{"БОНУС13"}}},
		Comment:      "Комментарий автора",
	}}}
	var fetched int
	data, err := renderScenarioPDF(t.Context(), doc, func(_ context.Context, src string) ([]byte, error) {
		if src != "https://example.org/mask.png" {
			t.Fatalf("wrong image URL: %s", src)
		}
		fetched++
		return imageBytes.Bytes(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fetched != 1 || !bytes.HasPrefix(data, []byte("%PDF-")) || !bytes.Contains(data, []byte("/Subtype /Image")) {
		t.Fatalf("PDF or image missing: fetches=%d bytes=%d", fetched, len(data))
	}
	path := filepath.Join(t.TempDir(), "scenario.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f, reader, err := pdf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if reader.NumPage() < 3 {
		t.Fatalf("expected cover, level and interactive appendix pages, got %d", reader.NumPage())
	}
	if !bytes.Contains(data, []byte("/ToUnicode")) {
		t.Fatal("PDF has no Unicode text mapping")
	}
	plainReader, err := reader.GetPlainText()
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(plainReader)
	if err != nil || !bytes.Contains(plain, []byte("drawHorse()")) {
		t.Fatalf("interactive source missing from PDF: err=%v", err)
	}
}

func TestPDFFragmentPreservesInteractiveSource(t *testing.T) {
	parts, interactive := pdfFragmentParts(`<p>Лошадь</p><canvas></canvas><script>drawHorse()</script>`)
	if !interactive || len(parts) == 0 || !strings.Contains(parts[0].text, "Лошадь") {
		t.Fatalf("interactive content lost: %+v, %t", parts, interactive)
	}
}

func TestPDFUnsupportedSymbolKeepsCodePoint(t *testing.T) {
	got := pdfSafeText("Орёл 🦅")
	if !strings.Contains(got, "Орёл") || !strings.Contains(got, "[U+1F985]") {
		t.Fatalf("PDF text did not preserve unsupported symbol identity: %q", got)
	}
}

func TestMarkdownPDFRendersFormattingLinksTablesAndImage(t *testing.T) {
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	content := "# Заголовок\n\nТекст с **жирным**, *курсивом* и [ссылкой](https://example.org).\n\n" +
		"- Первый пункт\n- Второй пункт\n\n> Цитата\n\n```go\nprint(42)\n```\n\n" +
		"| Имя | Значение |\n| --- | --- |\n| Код | 82913 |\n\n![Фото](https://example.org/photo.png)"
	fetched := 0
	data, err := renderChatPDF(t.Context(), ChatSnapshot{Title: "Документ", Messages: []UIMessage{{Role: UIMessageRoleAssistant, Content: content}}}, func(_ context.Context, src string) ([]byte, error) {
		if src != "https://example.org/photo.png" {
			t.Fatalf("unexpected image URL: %s", src)
		}
		fetched++
		return imageBytes.Bytes(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fetched != 1 || !bytes.Contains(data, []byte("/Subtype /Image")) || !bytes.Contains(data, []byte("https://example.org")) {
		t.Fatalf("image or link missing: fetched=%d bytes=%d", fetched, len(data))
	}
	path := filepath.Join(t.TempDir(), "markdown.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f, reader, err := pdf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	plainReader, err := reader.GetPlainText()
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(plainReader)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"print(42)", "82913"} {
		if !bytes.Contains(plain, []byte(want)) {
			t.Errorf("PDF text missing %q", want)
		}
	}
	if bytes.Contains(plain, []byte("**")) || bytes.Contains(plain, []byte("| --- |")) {
		t.Fatalf("Markdown syntax leaked into PDF text: %s", plain)
	}
}

func TestWebChatPDFExport(t *testing.T) {
	store := NewChatStore()
	chat := store.Create("", 0, SecurityModeReadonly)
	if _, ok := store.AppendUserMessage(chat.ID, "Покажи сценарий"); !ok {
		t.Fatal("cannot append message")
	}
	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: store, sse: newSSEHub()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/chats/"+chat.ID+"/export?format=pdf", nil)
	w := httptest.NewRecorder()
	hub.newMux().ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("export: status=%d type=%q", w.Code, w.Header().Get("Content-Type"))
	}
}
