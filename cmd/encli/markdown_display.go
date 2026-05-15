package main

import (
	"strings"
	"unicode/utf8"
)

// formatMarkdownForTerminal improves readability of common LLM markdown in the CLI.
// Code blocks and inline formatting are left as-is; pipe tables are rendered as aligned text.
func formatMarkdownForTerminal(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var out []string
	for i := 0; i < len(lines); {
		if isMarkdownTableRow(lines[i]) {
			block, n := collectMarkdownTable(lines[i:])
			out = append(out, formatASCIITable(block)...)
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			i += n
			continue
		}
		out = append(out, lines[i])
		i++
	}
	return strings.Join(out, "\n")
}

func isMarkdownTableRow(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "|") && strings.Contains(t, "|")
}

func isMarkdownTableSeparator(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.Contains(t, "-") {
		return false
	}
	t = strings.Trim(t, "|")
	for _, part := range strings.Split(t, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		for _, r := range part {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

func parseMarkdownTableRow(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.Trim(t, "|")
	parts := strings.Split(t, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

func collectMarkdownTable(lines []string) ([][]string, int) {
	if len(lines) == 0 || !isMarkdownTableRow(lines[0]) {
		return nil, 0
	}
	rows := [][]string{parseMarkdownTableRow(lines[0])}
	i := 1
	if i < len(lines) && isMarkdownTableSeparator(lines[i]) {
		i++
	}
	for i < len(lines) && isMarkdownTableRow(lines[i]) {
		rows = append(rows, parseMarkdownTableRow(lines[i]))
		i++
	}
	return rows, i
}

func formatASCIITable(rows [][]string) []string {
	if len(rows) == 0 {
		return nil
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	widths := make([]int, cols)
	for _, r := range rows {
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(r) {
				cell = r[c]
			}
			if w := utf8.RuneCountInString(cell); w > widths[c] {
				widths[c] = w
			}
		}
	}
	var out []string
	for ri, r := range rows {
		parts := make([]string, cols)
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(r) {
				cell = r[c]
			}
			parts[c] = padRunes(cell, widths[c])
		}
		out = append(out, "  "+strings.Join(parts, "  "))
		if ri == 0 && len(rows) > 1 {
			seps := make([]string, cols)
			for c := 0; c < cols; c++ {
				seps[c] = strings.Repeat("-", widths[c])
			}
			out = append(out, "  "+strings.Join(seps, "  "))
		}
	}
	return out
}

func padRunes(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}
