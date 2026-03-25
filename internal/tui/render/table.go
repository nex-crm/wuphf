package render

import (
	"strings"
	"unicode/utf8"
)

// RenderTable renders a table with auto-sized columns, alternating row colors,
// and a row-count footer. Returns "(no results)" for empty data.
func RenderTable(headers []string, rows [][]string, maxWidth int) string {
	if len(headers) == 0 {
		return MutedStyle.Render("(no results)")
	}
	if len(rows) == 0 {
		return MutedStyle.Render("(no results)")
	}

	cols := len(headers)

	// Calculate natural column widths.
	widths := make([]int, cols)
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i := 0; i < cols && i < len(row); i++ {
			if w := utf8.RuneCountInString(row[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	// Spacing between columns.
	gap := 2
	totalGap := gap * (cols - 1)

	// Clamp to maxWidth if needed.
	total := totalGap
	for _, w := range widths {
		total += w
	}
	if maxWidth > 0 && total > maxWidth {
		available := maxWidth - totalGap
		if available < cols {
			available = cols
		}
		// Shrink widths proportionally.
		for i, w := range widths {
			widths[i] = w * available / total
			if widths[i] < 1 {
				widths[i] = 1
			}
		}
	}

	spacer := strings.Repeat(" ", gap)
	var b strings.Builder

	// Header row.
	var headerParts []string
	for i, h := range headers {
		headerParts = append(headerParts, HeaderStyle.Render(pad(h, widths[i])))
	}
	b.WriteString(strings.Join(headerParts, spacer))
	b.WriteByte('\n')

	// Separator.
	var sepParts []string
	for _, w := range widths {
		sepParts = append(sepParts, MutedStyle.Render(strings.Repeat("─", w)))
	}
	b.WriteString(strings.Join(sepParts, spacer))
	b.WriteByte('\n')

	// Data rows.
	for ri, row := range rows {
		style := RowEvenStyle
		if ri%2 == 1 {
			style = RowOddStyle
		}
		var parts []string
		for ci := 0; ci < cols; ci++ {
			cell := ""
			if ci < len(row) {
				cell = row[ci]
			}
			parts = append(parts, style.Render(pad(cell, widths[ci])))
		}
		b.WriteString(strings.Join(parts, spacer))
		b.WriteByte('\n')
	}

	// Footer.
	b.WriteString(MutedStyle.Render(formatRowCount(len(rows))))

	return b.String()
}

// pad truncates or right-pads s to exactly width runes.
func pad(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n > width {
		// Truncate with ellipsis.
		if width <= 3 {
			return string([]rune(s)[:width])
		}
		return string([]rune(s)[:width-3]) + "..."
	}
	if n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func formatRowCount(n int) string {
	if n == 1 {
		return "(1 row)"
	}
	return "(" + strings.Repeat("", 0) + itoa(n) + " rows)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
