package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strconv"
	"strings"
	"sync"

	"github.com/phpdave11/gofpdf"
	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/net/html"
)

type pdfImageLoader func(context.Context, string) ([]byte, error)

type scenarioPDF struct {
	ctx         context.Context
	pdf         *gofpdf.Fpdf
	loadImage   pdfImageLoader
	imageSeq    int
	interactive []string
}

var pdfFont = sync.OnceValue(func() *sfnt.Font {
	font, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		panic(err)
	}
	return font
})

// Go's bundled font covers Cyrillic but not every symbol used in quests.
// Spell unsupported runes by code point so no task text is silently dropped
// and a single emoji cannot abort a complete scenario export.
func pdfSafeText(value string) string {
	font := pdfFont()
	var glyphBuffer sfnt.Buffer
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '\n':
			b.WriteByte('\n')
			continue
		case '\r':
			continue
		case '\t':
			b.WriteString("    ")
			continue
		}
		glyph, err := font.GlyphIndex(&glyphBuffer, r)
		if err != nil || glyph == 0 {
			fmt.Fprintf(&b, "[U+%X]", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func newScenarioPDF(ctx context.Context, title string, load pdfImageLoader) *scenarioPDF {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(16, 18, 16)
	pdf.SetAutoPageBreak(true, 16)
	pdf.AddUTF8FontFromBytes("Go", "", goregular.TTF)
	pdf.AddUTF8FontFromBytes("Go", "B", gobold.TTF)
	pdf.AddUTF8FontFromBytes("Go", "I", goitalic.TTF)
	pdf.AddUTF8FontFromBytes("Go", "BI", gobolditalic.TTF)
	pdf.SetTitle(title, true)
	pdf.SetAuthor("encli", true)
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Go", "", 8)
		pdf.SetTextColor(110, 115, 120)
		pdf.CellFormat(0, 5, strconv.Itoa(pdf.PageNo()), "", 0, "R", false, 0, "")
	})
	return &scenarioPDF{ctx: ctx, pdf: pdf, loadImage: load}
}

func (p *scenarioPDF) heading(label string, size float64) {
	p.pdf.SetFont("Go", "B", size)
	p.pdf.SetTextColor(30, 51, 55)
	p.pdf.MultiCell(0, size*0.52, pdfSafeText(label), "", "L", false)
	p.pdf.Ln(2)
}

func (p *scenarioPDF) text(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	p.pdf.SetFont("Go", "", 10)
	p.pdf.SetTextColor(33, 37, 40)
	p.pdf.MultiCell(0, 5.5, pdfSafeText(value), "", "L", false)
	p.pdf.Ln(2)
}

func (p *scenarioPDF) section(label string) {
	p.pdf.Ln(3)
	p.heading(label, 11)
}

func renderScenarioPDF(ctx context.Context, doc *scenario.Document, load pdfImageLoader) ([]byte, error) {
	if doc == nil {
		return nil, fmt.Errorf("scenario is missing")
	}
	title := doc.GameTitle
	if title == "" {
		title = fmt.Sprintf("Игра %d", doc.GameID)
	}
	p := newScenarioPDF(ctx, title, load)
	p.pdf.AddPage()
	p.heading(title, 18)
	p.text(fmt.Sprintf("Сценарий игры %d · уровней: %d", doc.GameID, len(doc.Levels)))
	if len(doc.Levels) > 0 {
		p.text(fmt.Sprintf("Уровни: %d–%d", doc.Levels[0].Number, doc.Levels[len(doc.Levels)-1].Number))
	}
	for _, level := range doc.Levels {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p.pdf.AddPage()
		p.heading(fmt.Sprintf("Уровень %d. %s", level.Number, level.Name), 15)
		p.writeLevel(level)
	}
	p.writeInteractiveAppendix()
	var out bytes.Buffer
	if err := p.pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func renderChatPDF(ctx context.Context, snap ChatSnapshot, load pdfImageLoader) ([]byte, error) {
	p := newScenarioPDF(ctx, snap.Title, load)
	p.pdf.AddPage()
	p.heading(snap.Title, 18)
	if snap.Domain != "" {
		p.text("Домен: " + snap.Domain)
	}
	if snap.GameID != 0 {
		p.text(fmt.Sprintf("Игра: %d", snap.GameID))
	}
	for _, message := range snap.Messages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		role := string(message.Role)
		switch message.Role {
		case UIMessageRoleUser:
			role = "Пользователь"
		case UIMessageRoleAssistant:
			role = "Ассистент"
		case UIMessageRoleTool:
			role = "Инструмент"
		}
		p.section(role)
		p.markdown(message.Content)
	}
	p.writeInteractiveAppendix()
	var out bytes.Buffer
	if err := p.pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (p *scenarioPDF) writeLevel(level scenario.Level) {
	if level.AutopassSecond != 0 || level.AutopassPenaltySecond != 0 || level.RequiredSectorsCount != 0 {
		p.text(fmt.Sprintf("Автопереход: %d с · штраф: %d с · секторов для прохождения: %d",
			level.AutopassSecond, level.AutopassPenaltySecond, level.RequiredSectorsCount))
	}
	for i, task := range level.Tasks {
		p.section(fmt.Sprintf("Задание %d", i+1))
		p.fragment(task)
	}
	for i, hint := range level.Hints {
		label := fmt.Sprintf("Подсказка %d", i+1)
		if hint.Title != "" {
			label += ". " + hint.Title
		}
		label += fmt.Sprintf(" · через %d с", hint.DelaySeconds)
		p.section(label)
		p.fragment(hint.Text)
	}
	for i, hint := range level.PenaltyHints {
		label := fmt.Sprintf("Штрафная подсказка %d", i+1)
		if hint.Title != "" {
			label += ". " + hint.Title
		}
		label += fmt.Sprintf(" · через %d с · штраф %d с", hint.DelaySeconds, hint.PenaltySeconds)
		p.section(label)
		p.fragment(hint.Text)
		if hint.Comment != "" {
			p.text("Комментарий: " + hint.Comment)
		}
		if hint.RequestConfirm {
			p.text("Требует подтверждения")
		}
	}
	for i, sector := range level.Sectors {
		p.section(fmt.Sprintf("Сектор %d. %s", i+1, sector.Name))
		p.text("Ответы: " + strings.Join(sector.Answers, "; "))
	}
	for i, answers := range level.SectorAnswers {
		p.text(fmt.Sprintf("Группа ответов %d: %s", i+1, strings.Join(answers, "; ")))
	}
	for i, bonus := range level.Bonuses {
		label := fmt.Sprintf("Бонус %d. %s", bonus.Number, bonus.Name)
		if bonus.Number == 0 {
			label = fmt.Sprintf("Бонус %d. %s", i+1, bonus.Name)
		}
		p.section(label)
		if bonus.Negative {
			p.text(fmt.Sprintf("Штрафное время: %d с", bonus.AwardSeconds))
		} else {
			p.text(fmt.Sprintf("Бонусное время: %d с", bonus.AwardSeconds))
		}
		if bonus.Task != "" {
			p.fragment(bonus.Task)
		}
		if bonus.Hint != "" {
			p.text("Подсказка:")
			p.fragment(bonus.Hint)
		}
		p.text("Ответы: " + strings.Join(bonus.Answers, "; "))
	}
	if level.Comment != "" {
		p.section("Комментарий автора")
		p.fragment(level.Comment)
	}
}

type pdfPart struct {
	text  string
	image string
}

func pdfFragmentParts(raw string) ([]pdfPart, bool) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return []pdfPart{{text: raw}}, false
	}
	var parts []pdfPart
	var b strings.Builder
	interactive := false
	flush := func() {
		if s := strings.TrimSpace(b.String()); s != "" {
			parts = append(parts, pdfPart{text: s})
		}
		b.Reset()
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style":
				interactive = interactive || n.Data == "script"
				return
			case "canvas", "iframe", "video", "audio":
				interactive = true
				b.WriteString("\n[Интерактивный элемент: " + n.Data + "]\n")
				return
			case "img":
				flush()
				for _, attr := range n.Attr {
					if attr.Key == "src" {
						parts = append(parts, pdfPart{image: attr.Val})
						break
					}
				}
				return
			case "br", "p", "div", "li", "tr", "h1", "h2", "h3":
				b.WriteByte('\n')
			}
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == html.ElementNode {
			if n.Data == "a" {
				for _, attr := range n.Attr {
					if attr.Key == "href" && isPDFLink(attr.Val) {
						b.WriteString(" (" + attr.Val + ")")
					}
				}
			}
			switch n.Data {
			case "p", "div", "li", "tr", "h1", "h2", "h3":
				b.WriteByte('\n')
			}
		}
	}
	walk(doc)
	flush()
	return parts, interactive
}

func isPDFLink(raw string) bool {
	return strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://")
}

func (p *scenarioPDF) fragment(raw string) {
	if !pdfHTMLTagRE.MatchString(raw) {
		p.markdown(raw)
		return
	}
	parts, interactive := pdfFragmentParts(raw)
	for _, part := range parts {
		if part.image != "" {
			p.image(part.image)
		} else {
			p.text(part.text)
		}
	}
	if interactive {
		p.interactive = append(p.interactive, raw)
		p.text(fmt.Sprintf("Интерактивный элемент %d: исходный HTML в приложении", len(p.interactive)))
	}
}

func (p *scenarioPDF) writeInteractiveAppendix() {
	if len(p.interactive) == 0 {
		return
	}
	p.pdf.AddPage()
	p.heading("Приложение: интерактивные элементы", 16)
	for i, raw := range p.interactive {
		p.section(fmt.Sprintf("Интерактивный элемент %d", i+1))
		p.code(raw)
	}
}

func (p *scenarioPDF) image(src string) {
	imageLabel := src
	if strings.HasPrefix(src, "data:") {
		imageLabel = "встроенное изображение"
	} else if len(imageLabel) > 400 {
		imageLabel = imageLabel[:400] + "…"
	}
	if p.loadImage == nil && !strings.HasPrefix(src, "data:") {
		p.text("Изображение: " + imageLabel)
		return
	}
	var data []byte
	var err error
	if strings.HasPrefix(src, "data:") {
		_, payload, ok := strings.Cut(src, ",")
		if !ok || len(payload) > 12<<20 {
			err = fmt.Errorf("invalid embedded image")
		} else {
			data, err = base64.StdEncoding.DecodeString(payload)
		}
	} else {
		data, err = p.loadImage(p.ctx, src)
	}
	if err != nil || len(data) == 0 || len(data) > encx.DefaultResourceMaxBytes {
		p.text("Изображение недоступно: " + imageLabel)
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		p.text("Изображение недоступно: " + imageLabel)
		return
	}
	imageType := "JPEG"
	if format != "jpeg" {
		decoded, _, decodeErr := image.Decode(bytes.NewReader(data))
		if decodeErr != nil {
			p.text("Изображение недоступно: " + imageLabel)
			return
		}
		var converted bytes.Buffer
		if err = png.Encode(&converted, decoded); err != nil {
			p.text("Изображение недоступно: " + imageLabel)
			return
		}
		data = converted.Bytes()
		imageType = "PNG"
	}
	p.imageSeq++
	name := fmt.Sprintf("image-%d", p.imageSeq)
	info := p.pdf.RegisterImageOptionsReader(name, gofpdf.ImageOptions{ImageType: imageType}, bytes.NewReader(data))
	if info == nil || p.pdf.Error() != nil {
		return
	}
	w := 178.0
	h := w * float64(config.Height) / float64(config.Width)
	if h > 135 {
		h = 135
		w = h * float64(config.Width) / float64(config.Height)
	}
	_, pageH := p.pdf.GetPageSize()
	if p.pdf.GetY()+h > pageH-18 {
		p.pdf.AddPage()
	}
	p.pdf.ImageOptions(name, 16, p.pdf.GetY(), w, h, false, gofpdf.ImageOptions{ImageType: imageType}, 0, "")
	p.pdf.SetY(p.pdf.GetY() + h + 3)
}
