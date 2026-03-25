package render

import (
	"strings"
	"testing"
)

func TestRenderGraphBasic(t *testing.T) {
	nodes := []GraphNode{
		{ID: "1", Label: "Alice", Type: "person"},
		{ID: "2", Label: "Acme Corp", Type: "company"},
		{ID: "3", Label: "Big Deal", Type: "deal"},
	}
	edges := []GraphEdge{
		{From: "1", To: "2", Label: "works at"},
		{From: "1", To: "3", Label: "owns"},
	}

	result := RenderGraph(nodes, edges, 60, 20)

	// All node labels should appear.
	if !strings.Contains(result, "Alice") {
		t.Error("expected 'Alice' in output")
	}
	if !strings.Contains(result, "Acme Corp") {
		t.Error("expected 'Acme Corp' in output")
	}
	if !strings.Contains(result, "Big Deal") {
		t.Error("expected 'Big Deal' in output")
	}

	// Should contain box-drawing characters for edges.
	hasEdgeChar := strings.ContainsAny(result, "─│┌┐└┘┼")
	if !hasEdgeChar {
		t.Error("expected box-drawing characters for edges")
	}

	// Should contain node type icons.
	if !strings.Contains(result, "👤") {
		t.Error("expected person icon 👤")
	}
	if !strings.Contains(result, "🏢") {
		t.Error("expected company icon 🏢")
	}
	if !strings.Contains(result, "💰") {
		t.Error("expected deal icon 💰")
	}
}

func TestRenderGraphEmpty(t *testing.T) {
	result := RenderGraph(nil, nil, 60, 20)
	if !strings.Contains(result, "(no graph data)") {
		t.Errorf("expected '(no graph data)', got: %s", result)
	}

	result2 := RenderGraph([]GraphNode{}, []GraphEdge{}, 60, 20)
	if !strings.Contains(result2, "(no graph data)") {
		t.Errorf("expected '(no graph data)' for empty slices, got: %s", result2)
	}
}

func TestRenderGraphLegend(t *testing.T) {
	nodes := []GraphNode{
		{ID: "1", Label: "Alice", Type: "person"},
		{ID: "2", Label: "Acme Corp", Type: "company"},
	}

	result := RenderGraph(nodes, nil, 60, 20)

	// Legend should show used types.
	if !strings.Contains(result, "person") {
		t.Error("expected 'person' in legend")
	}
	if !strings.Contains(result, "company") {
		t.Error("expected 'company' in legend")
	}

	// Legend should NOT show unused types.
	if strings.Contains(result, "deal") {
		t.Error("legend should not contain unused type 'deal'")
	}
	if strings.Contains(result, "task") {
		t.Error("legend should not contain unused type 'task'")
	}
}

func TestRenderGraphSingleNode(t *testing.T) {
	nodes := []GraphNode{
		{ID: "1", Label: "Lone Node", Type: "project"},
	}

	result := RenderGraph(nodes, nil, 40, 12)

	if !strings.Contains(result, "Lone Node") {
		t.Error("expected 'Lone Node' in output")
	}
	if !strings.Contains(result, "📋") {
		t.Error("expected project icon 📋")
	}
}

func TestRenderGraphDefaultIcon(t *testing.T) {
	nodes := []GraphNode{
		{ID: "1", Label: "Unknown", Type: "somethingelse"},
	}

	result := RenderGraph(nodes, nil, 40, 12)

	if !strings.Contains(result, "◆") {
		t.Error("expected default icon ◆ for unknown type")
	}
}

func TestRenderGraphLabelTruncation(t *testing.T) {
	nodes := []GraphNode{
		{ID: "1", Label: "A Very Long Label That Should Be Truncated", Type: "person"},
	}

	result := RenderGraph(nodes, nil, 40, 12)

	// Full label should NOT appear; truncated version should.
	if strings.Contains(result, "A Very Long Label That Should Be Truncated") {
		t.Error("expected label to be truncated")
	}
	if !strings.Contains(result, "…") {
		t.Error("expected truncation indicator '…'")
	}
}

func TestRenderGraphManyNodes(t *testing.T) {
	nodes := []GraphNode{
		{ID: "1", Label: "Alice", Type: "person"},
		{ID: "2", Label: "Bob", Type: "person"},
		{ID: "3", Label: "Acme", Type: "company"},
		{ID: "4", Label: "Beta Inc", Type: "company"},
		{ID: "5", Label: "Task 1", Type: "task"},
		{ID: "6", Label: "Note 1", Type: "note"},
	}
	edges := []GraphEdge{
		{From: "1", To: "3"},
		{From: "2", To: "4"},
		{From: "1", To: "5"},
	}

	result := RenderGraph(nodes, edges, 80, 25)

	// All labels should appear.
	for _, n := range nodes {
		if !strings.Contains(result, n.Label) {
			t.Errorf("expected label %q in output", n.Label)
		}
	}

	// Should not panic or produce empty output.
	if result == "" {
		t.Error("expected non-empty output for many nodes")
	}
}

func TestNodeIcon(t *testing.T) {
	cases := map[string]string{
		"person":  "👤",
		"company": "🏢",
		"deal":    "💰",
		"task":    "☑",
		"note":    "📝",
		"email":   "✉",
		"event":   "📅",
		"product": "📦",
		"project": "📋",
		"unknown": "◆",
		"":        "◆",
	}

	for nodeType, expected := range cases {
		got := nodeIcon(nodeType)
		if got != expected {
			t.Errorf("nodeIcon(%q) = %q, want %q", nodeType, got, expected)
		}
	}
}
