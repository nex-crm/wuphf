package migration

import (
	"context"
	"testing"
	"time"
)

func TestCoerceNexRecordsList(t *testing.T) {
	tests := []struct {
		name  string
		raw   any
		want  int
		isErr bool
	}{
		{"bare list", []any{map[string]any{"id": "1"}, map[string]any{"id": "2"}}, 2, false},
		{"data envelope", map[string]any{"data": []any{map[string]any{"id": "1"}}}, 1, false},
		{"records envelope", map[string]any{"records": []any{map[string]any{"id": "1"}}}, 1, false},
		{"items envelope", map[string]any{"items": []any{map[string]any{"id": "1"}}}, 1, false},
		{"empty list", []any{}, 0, false},
		{"nil", nil, 0, false},
		{"unknown shape", "not an object", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := coerceNexRecordsList(tc.raw)
			if tc.isErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.want {
				t.Fatalf("want %d records, got %d (%v)", tc.want, len(got), got)
			}
		})
	}
}

func TestTranslateNexRecord(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		row      map[string]any
		wantKind Kind
		wantSlug string
		wantOK   bool
	}{
		{
			name:     "happy path person",
			kind:     "person",
			row:      map[string]any{"slug": "Nazz", "name": "Nazz Mohammad", "content": "Founder"},
			wantKind: KindPeople,
			wantSlug: "nazz",
			wantOK:   true,
		},
		{
			name:     "slug fallback to name",
			kind:     "company",
			row:      map[string]any{"name": "HubSpot Inc", "description": "CRM"},
			wantKind: KindCompanies,
			wantSlug: "hubspot-inc",
			wantOK:   true,
		},
		{
			name:     "empty content uses json dump",
			kind:     "note",
			row:      map[string]any{"slug": "meeting-notes", "tags": []any{"important"}},
			wantKind: KindNotes,
			wantSlug: "meeting-notes",
			wantOK:   true,
		},
		{
			name:   "no slug, no title -> skip",
			kind:   "note",
			row:    map[string]any{"content": "something"},
			wantOK: false,
		},
		{
			name:     "unknown type falls back to misc",
			kind:     "zonked",
			row:      map[string]any{"slug": "z", "content": "x"},
			wantKind: KindMisc,
			wantSlug: "z",
			wantOK:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec, ok := translateNexRecord(tc.kind, tc.row)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v (rec=%+v)", ok, tc.wantOK, rec)
			}
			if !ok {
				return
			}
			if rec.Kind != tc.wantKind {
				t.Errorf("kind=%q, want %q", rec.Kind, tc.wantKind)
			}
			if rec.Slug != tc.wantSlug {
				t.Errorf("slug=%q, want %q", rec.Slug, tc.wantSlug)
			}
			if rec.Source != "nex" {
				t.Errorf("source=%q, want nex", rec.Source)
			}
		})
	}
}

func TestNexAdapterIter(t *testing.T) {
	pages := map[string][][]map[string]any{
		"person": {
			{
				{"slug": "alice", "name": "Alice", "content": "eng lead"},
				{"slug": "bob", "name": "Bob", "content": "designer"},
			},
			{}, // end
		},
		"company": {
			{
				{"slug": "acme", "name": "Acme", "content": "customer"},
			},
			{},
		},
	}
	adapter := NewNexAdapter(nil,
		WithNexTypes("person", "company"),
		WithNexPageSize(2),
		WithNexFetcher(func(ctx context.Context, objectType string, limit, offset int) ([]map[string]any, error) {
			batches := pages[objectType]
			idx := offset / limit
			if idx >= len(batches) {
				return nil, nil
			}
			return batches[idx], nil
		}),
	)

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
	if len(got) != 3 {
		t.Fatalf("want 3 records, got %d: %+v", len(got), got)
	}
	// Order matters: persons before companies because that's how we walk.
	if got[0].Kind != KindPeople || got[0].Slug != "alice" {
		t.Errorf("record 0 = %+v", got[0])
	}
	if got[2].Kind != KindCompanies || got[2].Slug != "acme" {
		t.Errorf("record 2 = %+v", got[2])
	}
}
