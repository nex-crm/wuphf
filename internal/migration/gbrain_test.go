package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// fakeGBrainCaller is a scriptable stand-in for the real gbrain.Call.
// Each entry in responses matches one call in order by tool name.
type fakeGBrainCaller struct {
	responses map[string][]string
	errors    map[string][]error
	calls     map[string]int
}

func (f *fakeGBrainCaller) Call(ctx context.Context, tool string, params any) (string, error) {
	idx := f.calls[tool]
	f.calls[tool] = idx + 1
	if errs, ok := f.errors[tool]; ok && idx < len(errs) && errs[idx] != nil {
		return "", errs[idx]
	}
	if resps, ok := f.responses[tool]; ok && idx < len(resps) {
		return resps[idx], nil
	}
	return "", nil
}

func TestDecodeGBrainPagesShapes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{"bare array", `[{"slug":"a"},{"slug":"b"}]`, 2},
		{"pages envelope", `{"pages":[{"slug":"a"}]}`, 1},
		{"data envelope", `{"data":[{"slug":"a"},{"slug":"b"}]}`, 2},
		{"items envelope", `{"items":[{"slug":"a"}]}`, 1},
		{"empty string", ``, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeGBrainPages(tc.raw)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if len(got) != tc.want {
				t.Fatalf("want %d pages, got %d", tc.want, len(got))
			}
		})
	}
}

func TestGBrainAdapterListPath(t *testing.T) {
	pages := []GBrainPage{
		{Slug: "nazz", Title: "Nazz", Type: "person", Content: "Founder"},
		{Slug: "hubspot", Title: "HubSpot", Type: "company", Content: "Prior life"},
	}
	payload, err := json.Marshal(pages)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	caller := &fakeGBrainCaller{
		responses: map[string][]string{
			"list_pages": {string(payload), `[]`},
		},
		calls: map[string]int{},
	}
	adapter := NewGBrainAdapter(WithGBrainCaller(caller), WithGBrainPageSize(10))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := adapter.Iter(ctx)
	if err != nil {
		t.Fatalf("iter: %v", err)
	}
	var got []MigrationRecord
	for rec := range ch {
		got = append(got, rec)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 records, got %d", len(got))
	}
	if got[0].Kind != KindPeople || got[0].Slug != "nazz" {
		t.Errorf("unexpected record 0: %+v", got[0])
	}
	if got[1].Kind != KindCompanies || got[1].Slug != "hubspot" {
		t.Errorf("unexpected record 1: %+v", got[1])
	}
	for _, r := range got {
		if r.Source != "gbrain" {
			t.Errorf("source=%q, want gbrain", r.Source)
		}
	}
}

func TestGBrainAdapterFallbackToQuery(t *testing.T) {
	// list_pages returns an error → adapter falls back to query.
	caller := &fakeGBrainCaller{
		responses: map[string][]string{
			"query": {`[{"slug":"seed","title":"Seed","type":"topic","chunk_text":"body"}]`},
		},
		errors: map[string][]error{
			"list_pages": {fmt.Errorf("unsupported tool")},
		},
		calls: map[string]int{},
	}
	adapter := NewGBrainAdapter(WithGBrainCaller(caller))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := adapter.Iter(ctx)
	if err != nil {
		t.Fatalf("iter: %v", err)
	}
	var got []MigrationRecord
	for rec := range ch {
		got = append(got, rec)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record from fallback, got %d", len(got))
	}
	if got[0].Kind != KindTopics || got[0].Slug != "seed" {
		t.Errorf("unexpected fallback record: %+v", got[0])
	}
}
