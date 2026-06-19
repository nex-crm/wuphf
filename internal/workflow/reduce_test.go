package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReduceCapsArrayAndRecordsOmitted(t *testing.T) {
	items := make([]map[string]any, 100)
	for i := range items {
		items[i] = map[string]any{"i": i}
	}
	raw, _ := json.Marshal(map[string]any{"data": map[string]any{"messages": items}})
	out, red := Reduce(raw, Budget{MaxItems: 10, StringCap: 1000, MaxBytes: 1 << 20})
	if red.ItemsOmitted != 90 {
		t.Fatalf("want 90 omitted, got %d", red.ItemsOmitted)
	}
	if !red.Truncated {
		t.Fatal("reduction should be marked truncated")
	}
	var back struct {
		Data struct {
			Messages []any `json:"messages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("reduced output must stay valid JSON: %v", err)
	}
	if len(back.Data.Messages) != 10 {
		t.Fatalf("want 10 kept, got %d", len(back.Data.Messages))
	}
}

func TestReduceTruncatesLongStrings(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"body": strings.Repeat("x", 5000)})
	out, red := Reduce(raw, Budget{MaxItems: 50, StringCap: 100, MaxBytes: 1 << 20})
	if !red.Truncated {
		t.Fatal("long string should be truncated")
	}
	var back map[string]string
	_ = json.Unmarshal(out, &back)
	if len([]rune(back["body"])) > 101 { // 100 + ellipsis
		t.Fatalf("string not capped: %d runes", len([]rune(back["body"])))
	}
}

func TestReduceEnforcesByteCeiling(t *testing.T) {
	items := make([]map[string]any, 200)
	for i := range items {
		items[i] = map[string]any{"body": strings.Repeat("y", 400)}
	}
	raw, _ := json.Marshal(items)
	out, red := Reduce(raw, Budget{MaxItems: 100, StringCap: 400, MaxBytes: 4096})
	if len(out) > 4096 {
		t.Fatalf("reduced output %d bytes exceeds the 4096 ceiling", len(out))
	}
	if !red.Truncated || red.BytesAfter > red.BytesBefore {
		t.Fatalf("reduction metadata wrong: %+v", red)
	}
}

func TestReduceLeavesNonJSONAlone(t *testing.T) {
	raw := json.RawMessage(`not json at all`)
	out, red := Reduce(raw, DefaultPromptBudget)
	if string(out) != string(raw) || red.Truncated {
		t.Fatalf("non-JSON should pass through untouched: %q %+v", out, red)
	}
}
