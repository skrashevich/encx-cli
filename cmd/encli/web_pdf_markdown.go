package main

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	tableast "github.com/yuin/goldmark/extension/ast"
	goldmarktext "github.com/yuin/goldmark/text"
	"golang.org/x/image/font/gofont/gomono"
)

var pdfHTMLTagRE = regexp.MustCompile(`(?i)</?[a-z][a-z0-9]*(?:\s[^<>]*)?/?>`)

// markdown renders the same authored structure that the WebUI shows, including
// headings, emphasis, lists, links, code, tables and inline images.
func (p *scenarioPDF) markdown(raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	source := []byte(raw)
	root := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(goldmarktext.NewReader(source))
	for node := root.FirstChild(); node != nil; node = node.NextSibling() {
		p.markdownBlock(node, source, 0)
	}
}

func (p *scenarioPDF) markdownBlock(node ast.Node, source []byte, depth int) {
	switch n := node.(type) {
	case *ast.Heading:
		size := 16.0 - float64(n.Level-1)*1.25
		if size < 10.5 {
			size = 10.5
		}
		p.pdf.Ln(2)
		p.heading(markdownPlain(n, source), size)
	case *ast.Paragraph, *ast.TextBlock:
		p.pdf.SetFont("Go", "", 10)
		p.pdf.SetTextColor(33, 37, 40)
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			p.markdownInline(child, source, pdfInlineStyle{})
		}
		p.pdf.Ln(7)
	case *ast.List:
		index := n.Start
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			marker := "•"
			if n.IsOrdered() {
				marker = strconv.Itoa(index) + "."
				index++
			}
			left := 16.0 + float64(depth)*7
			p.pdf.SetLeftMargin(left + 7)
			p.pdf.SetX(left)
			p.pdf.SetFont("Go", "", 10)
			p.pdf.Write(5.5, pdfSafeText(marker)+" ")
			p.pdf.SetX(left + 7)
			for child := item.FirstChild(); child != nil; child = child.NextSibling() {
				p.markdownBlock(child, source, depth+1)
			}
			p.pdf.SetLeftMargin(16)
		}
	case *ast.Blockquote:
		left := 16.0 + float64(depth)*7
		start := p.pdf.GetY()
		p.pdf.SetLeftMargin(left + 6)
		p.pdf.SetX(left + 6)
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			p.markdownBlock(child, source, depth+1)
		}
		end := p.pdf.GetY()
		p.pdf.SetLeftMargin(16)
		p.pdf.SetDrawColor(75, 141, 154)
		p.pdf.SetLineWidth(0.65)
		p.pdf.Line(left+2, start, left+2, end-3)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		var code strings.Builder
		for i := 0; i < node.Lines().Len(); i++ {
			segment := node.Lines().At(i)
			code.Write(segment.Value(source))
		}
		p.code(code.String())
	case *ast.ThematicBreak:
		y := p.pdf.GetY() + 2
		p.pdf.SetDrawColor(200, 209, 212)
		p.pdf.Line(16, y, 194, y)
		p.pdf.Ln(6)
	case *tableast.Table:
		p.markdownTable(n, source)
	case *ast.HTMLBlock:
		var raw strings.Builder
		for i := 0; i < n.Lines().Len(); i++ {
			segment := n.Lines().At(i)
			raw.Write(segment.Value(source))
		}
		p.fragment(raw.String())
	default:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			p.markdownBlock(child, source, depth)
		}
	}
}

func (p *scenarioPDF) code(value string) {
	p.pdf.AddUTF8FontFromBytes("GoMono", "", gomono.TTF)
	p.pdf.SetFont("GoMono", "", 8.5)
	p.pdf.SetTextColor(38, 46, 54)
	p.pdf.SetFillColor(239, 243, 245)
	p.pdf.MultiCell(0, 4.6, pdfSafeText(strings.TrimRight(value, "\n")), "", "L", true)
	p.pdf.Ln(3)
}

type pdfInlineStyle struct {
	bold, italic, code bool
	link               string
}

func (p *scenarioPDF) markdownInline(node ast.Node, source []byte, style pdfInlineStyle) {
	switch n := node.(type) {
	case *ast.Text:
		p.writeMarkdownText(html.UnescapeString(string(n.Segment.Value(source))), style)
		if n.HardLineBreak() {
			p.pdf.Ln(5.5)
		} else if n.SoftLineBreak() {
			p.writeMarkdownText(" ", style)
		}
	case *ast.String:
		p.writeMarkdownText(html.UnescapeString(string(n.Value)), style)
	case *ast.Emphasis:
		if n.Level == 2 {
			style.bold = true
		} else {
			style.italic = true
		}
		p.markdownInlineChildren(node, source, style)
	case *ast.CodeSpan:
		style.code = true
		p.markdownInlineChildren(node, source, style)
	case *ast.Link:
		if isPDFLink(string(n.Destination)) {
			style.link = string(n.Destination)
		}
		p.markdownInlineChildren(node, source, style)
	case *ast.AutoLink:
		destination := string(n.URL(source))
		if isPDFLink(destination) {
			style.link = destination
		}
		p.writeMarkdownText(string(n.Label(source)), style)
	case *ast.Image:
		p.pdf.Ln(2)
		p.image(string(n.Destination))
		p.pdf.SetX(16)
	default:
		p.markdownInlineChildren(node, source, style)
	}
}

func (p *scenarioPDF) markdownInlineChildren(node ast.Node, source []byte, style pdfInlineStyle) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		p.markdownInline(child, source, style)
	}
}

func (p *scenarioPDF) writeMarkdownText(value string, style pdfInlineStyle) {
	if value == "" {
		return
	}
	fontStyle := ""
	if style.bold {
		fontStyle += "B"
	}
	if style.italic {
		fontStyle += "I"
	}
	if style.code {
		p.pdf.AddUTF8FontFromBytes("GoMono", "", gomono.TTF)
		p.pdf.SetFont("GoMono", "", 9)
	} else {
		p.pdf.SetFont("Go", fontStyle, 10)
	}
	if style.link != "" {
		p.pdf.SetTextColor(15, 99, 148)
		p.pdf.WriteLinkString(5.5, pdfSafeText(value), style.link)
	} else {
		p.pdf.SetTextColor(33, 37, 40)
		p.pdf.Write(5.5, pdfSafeText(value))
	}
}

func markdownPlain(node ast.Node, source []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		switch value := n.(type) {
		case *ast.Text:
			b.Write(value.Segment.Value(source))
		case *ast.String:
			b.Write(value.Value)
		}
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			walk(child)
		}
	}
	walk(node)
	return html.UnescapeString(b.String())
}

func (p *scenarioPDF) markdownTable(table *tableast.Table, source []byte) {
	columns := len(table.Alignments)
	if columns == 0 {
		return
	}
	width := 178 / float64(columns)
	if width < 15 {
		for row := table.FirstChild(); row != nil; row = row.NextSibling() {
			p.text(markdownPlain(row, source))
		}
		return
	}
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		isHeader := row.Kind() == tableast.KindTableHeader
		style := ""
		if isHeader {
			style = "B"
		}
		p.pdf.SetFont("Go", style, 9)
		var cells []string
		maxLines := 1
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			value := pdfSafeText(markdownPlain(cell, source))
			cells = append(cells, value)
			if lines := len(p.pdf.SplitText(value, width-4)); lines > maxLines {
				maxLines = lines
			}
		}
		rowHeight := float64(maxLines)*5 + 3
		_, pageH := p.pdf.GetPageSize()
		if p.pdf.GetY()+rowHeight > pageH-16 {
			p.pdf.AddPage()
		}
		x, y := 16.0, p.pdf.GetY()
		for i, value := range cells {
			p.pdf.SetXY(x+2, y+1.5)
			p.pdf.SetFillColor(232, 240, 242)
			p.pdf.SetDrawColor(205, 215, 218)
			p.pdf.Rect(x, y, width, rowHeight, "D")
			if isHeader {
				p.pdf.Rect(x, y, width, rowHeight, "F")
			}
			p.pdf.MultiCell(width-4, 5, value, "", "L", false)
			x = 16 + float64(i+1)*width
		}
		p.pdf.SetXY(16, y+rowHeight)
	}
	p.pdf.Ln(3)
}
