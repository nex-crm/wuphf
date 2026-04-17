package render

import (
	"math"
	"strings"
	"unicode/utf8"
)

// GraphNode represents a node in the knowledge graph.
type GraphNode struct {
	ID    string
	Label string
	Type  string // person, company, deal, task, note, email, event, product, project
}

// GraphEdge represents a directed edge between two nodes.
type GraphEdge struct {
	From  string // node ID
	To    string // node ID
	Label string
}

// nodeIcon returns the emoji icon for a given node type.
func nodeIcon(nodeType string) string {
	icons := map[string]string{
		"person":   "\U0001F464", // 👤
		"company":  "\U0001F3E2", // 🏢
		"deal":     "\U0001F4B0", // 💰
		"task":     "\u2611",     // ☑
		"note":     "\U0001F4DD", // 📝
		"email":    "\u2709",     // ✉
		"event":    "\U0001F4C5", // 📅
		"product":  "\U0001F4E6", // 📦
		"project":  "\U0001F4CB", // 📋
		"ticket":   "\U0001F3AB", // 🎫
		"location": "\U0001F4CD", // 📍
	}
	if icon, ok := icons[nodeType]; ok {
		return icon
	}
	return "\u25C6" // ◆
}

// iconDisplayWidth returns the visual column width of a node icon.
// Most emoji occupy 2 columns; the single-codepoint symbols occupy 1.
func iconDisplayWidth(nodeType string) int {
	switch nodeType {
	case "task", "email":
		return 1
	default:
		if nodeType == "" {
			// default diamond ◆ is 1 wide
			return 1
		}
		// emoji icons are 2 columns wide
		return 2
	}
}

// RenderGraph renders an ASCII knowledge graph with grid-based node layout,
// box-drawing edges, and an auto-detected legend.
func RenderGraph(nodes []GraphNode, edges []GraphEdge, width, height int) string {
	if len(nodes) == 0 {
		return MutedStyle.Render("(no graph data)")
	}

	if width < 20 {
		width = 20
	}
	// Reserve 3 lines for legend (blank + legend header + legend items).
	legendHeight := 3
	if height < 8+legendHeight {
		height = 8 + legendHeight
	}
	canvasHeight := height - legendHeight

	// Build node index.
	nodeIndex := make(map[string]int, len(nodes))
	for i, n := range nodes {
		nodeIndex[n.ID] = i
	}

	// Calculate grid positions.
	type pos struct{ x, y int }
	positions := layoutGrid(nodes, width, canvasHeight)

	// Calculate label display info for each node.
	type nodeDisplay struct {
		icon       string
		label      string
		startCol   int // leftmost column of "icon label"
		iconWidth  int
		labelWidth int
	}
	displays := make([]nodeDisplay, len(nodes))
	for i, n := range nodes {
		icon := nodeIcon(n.Type)
		iw := iconDisplayWidth(n.Type)
		label := n.Label
		maxLabel := 12
		if utf8.RuneCountInString(label) > maxLabel {
			label = string([]rune(label)[:maxLabel-1]) + "…"
		}
		lw := utf8.RuneCountInString(label)
		totalWidth := iw + 1 + lw // icon + space + label
		startCol := positions[i].x - totalWidth/2
		if startCol < 0 {
			startCol = 0
		}
		if startCol+totalWidth > width {
			startCol = width - totalWidth
			if startCol < 0 {
				startCol = 0
			}
		}
		displays[i] = nodeDisplay{
			icon:       icon,
			label:      label,
			startCol:   startCol,
			iconWidth:  iw,
			labelWidth: lw,
		}
	}

	// Create canvas.
	canvas := make([][]rune, canvasHeight)
	for r := range canvas {
		canvas[r] = make([]rune, width)
		for c := range canvas[r] {
			canvas[r][c] = ' '
		}
	}

	// Draw edges first (so nodes overwrite them).
	for _, edge := range edges {
		fromIdx, okFrom := nodeIndex[edge.From]
		toIdx, okTo := nodeIndex[edge.To]
		if !okFrom || !okTo {
			continue
		}
		drawEdge(canvas, positions[fromIdx], positions[toIdx], width, canvasHeight)
	}

	// Place nodes on canvas.
	for i := range nodes {
		d := displays[i]
		row := positions[i].y
		if row < 0 || row >= canvasHeight {
			continue
		}
		// Write icon.
		col := d.startCol
		if col >= 0 && col < width {
			canvas[row][col] = []rune(d.icon)[0]
			// Mark subsequent columns for wide chars with a zero-width placeholder.
			for w := 1; w < d.iconWidth && col+w < width; w++ {
				canvas[row][col+w] = 0 // will be skipped during rendering
			}
		}
		// Write space + label.
		labelStart := col + d.iconWidth
		if labelStart < width {
			canvas[row][labelStart] = ' '
		}
		for j, r := range []rune(d.label) {
			c := labelStart + 1 + j
			if c >= 0 && c < width {
				canvas[row][c] = r
			}
		}
	}

	// Render canvas to string.
	var sb strings.Builder
	for _, row := range canvas {
		line := renderCanvasRow(row)
		sb.WriteString(strings.TrimRight(line, " "))
		sb.WriteString("\n")
	}

	// Legend.
	legend := renderLegend(nodes)
	if legend != "" {
		sb.WriteString("\n")
		sb.WriteString(MutedStyle.Render(legend))
	}

	return strings.TrimRight(sb.String(), "\n")
}

// renderCanvasRow converts a row of runes into a string, skipping zero-value
// runes used as placeholders for wide characters.
func renderCanvasRow(row []rune) string {
	var sb strings.Builder
	for _, r := range row {
		if r == 0 {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// layoutGrid places nodes in a grid pattern spread across the canvas.
func layoutGrid(nodes []GraphNode, width, height int) []struct{ x, y int } {
	n := len(nodes)
	positions := make([]struct{ x, y int }, n)

	if n == 1 {
		positions[0] = struct{ x, y int }{width / 2, height / 2}
		return positions
	}

	// Calculate grid dimensions.
	cols := int(math.Ceil(math.Sqrt(float64(n))))
	rows := int(math.Ceil(float64(n) / float64(cols)))

	// Cell size.
	cellW := width / cols
	if cellW < 4 {
		cellW = 4
	}
	cellH := height / rows
	if cellH < 2 {
		cellH = 2
	}

	for i := range nodes {
		col := i % cols
		row := i / cols
		x := cellW/2 + col*cellW
		y := cellH/2 + row*cellH
		if x >= width {
			x = width - 1
		}
		if y >= height {
			y = height - 1
		}
		positions[i] = struct{ x, y int }{x, y}
	}

	return positions
}

// drawEdge draws a line between two positions on the canvas using box-drawing
// characters. Uses an L-shaped path (horizontal then vertical).
func drawEdge(canvas [][]rune, from, to struct{ x, y int }, width, height int) {
	// L-shaped routing: go horizontal from 'from' to to.x, then vertical to to.y.
	midX := to.x
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}

	// Horizontal segment: from.x -> midX at from.y
	if from.x != midX {
		startX := from.x
		endX := midX
		if startX > endX {
			startX, endX = endX, startX
		}
		row := clamp(from.y, 0, height-1)
		for x := startX; x <= endX; x++ {
			cx := clamp(x, 0, width-1)
			if canvas[row][cx] == ' ' {
				canvas[row][cx] = '\u2500' // ─
			} else if canvas[row][cx] == '\u2502' { // │
				canvas[row][cx] = '\u253C' // ┼
			}
		}
	}

	// Vertical segment: from.y -> to.y at midX
	if from.y != to.y {
		startY := from.y
		endY := to.y
		if startY > endY {
			startY, endY = endY, startY
		}
		col := clamp(midX, 0, width-1)
		for y := startY; y <= endY; y++ {
			row := clamp(y, 0, height-1)
			if canvas[row][col] == ' ' {
				canvas[row][col] = '\u2502' // │
			} else if canvas[row][col] == '\u2500' { // ─
				canvas[row][col] = '\u253C' // ┼
			}
		}
	}

	// Place corner at the bend point.
	if from.x != midX && from.y != to.y {
		cornerRow := clamp(from.y, 0, height-1)
		cornerCol := clamp(midX, 0, width-1)
		corner := pickCorner(from, to)
		canvas[cornerRow][cornerCol] = corner
	}
}

// pickCorner selects the appropriate box-drawing corner character based on
// the relative positions of source and destination.
func pickCorner(from, to struct{ x, y int }) rune {
	if from.x < to.x {
		if from.y < to.y {
			return '\u250C' // ┌ (going right and down → corner opens down-right)
			// Actually the bend is at (to.x, from.y): came from left, going down
		}
		return '\u2514' // └ (going right and up → corner opens up-right)
	}
	if from.y < to.y {
		return '\u2510' // ┐ (going left and down)
	}
	return '\u2518' // ┘ (going left and up)
}

// renderLegend builds a legend line showing icons for all node types present.
func renderLegend(nodes []GraphNode) string {
	// Collect unique types in order of first appearance.
	seen := make(map[string]bool)
	var types []string
	for _, n := range nodes {
		t := n.Type
		if t == "" {
			t = "other"
		}
		if !seen[t] {
			seen[t] = true
			types = append(types, t)
		}
	}

	if len(types) == 0 {
		return ""
	}

	var parts []string
	for _, t := range types {
		icon := nodeIcon(t)
		parts = append(parts, icon+" "+t)
	}
	return strings.Join(parts, "  ")
}
