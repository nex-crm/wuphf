package team

import (
	"strings"
	"testing"
)

func TestSurfaceStoreWidgetRenderReadAndPatch(t *testing.T) {
	store := NewSurfaceStore(t.TempDir())
	surface, err := store.CreateSurface(SurfaceManifest{
		Title:     "Launch Command Center",
		Channel:   "general",
		CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create surface: %v", err)
	}

	record, err := store.UpsertWidget(surface.ID, SurfaceWidgetFile{
		ID:    "open-blockers",
		Title: "Open blockers",
		Kind:  "checklist",
		Source: `kind: checklist
items:
  - id: scope
    label: Lock scope
    checked: true
  - id: tests
    label: Add tests
    checked: false
`,
	}, "ceo")
	if err != nil {
		t.Fatalf("upsert widget: %v", err)
	}
	if !record.Render.SchemaOK || !record.Render.RenderOK {
		t.Fatalf("render failed: %+v", record.Render)
	}
	if len(record.SourceLines) < 7 {
		t.Fatalf("expected numbered source lines, got %d", len(record.SourceLines))
	}

	patched, err := store.PatchWidget(surface.ID, "open-blockers", WidgetPatchRequest{
		Actor:       "ceo",
		Mode:        "snippet",
		Search:      "label: Add tests",
		Replacement: "label: Add render tests",
	})
	if err != nil {
		t.Fatalf("patch widget: %v", err)
	}
	if !strings.Contains(patched.Widget.Source, "Add render tests") {
		t.Fatalf("patch did not update source:\n%s", patched.Widget.Source)
	}
	if !strings.Contains(patched.Render.PreviewText, "Add render tests") {
		t.Fatalf("preview did not reflect patch: %q", patched.Render.PreviewText)
	}

	reloaded := NewSurfaceStore(store.root)
	detail, err := reloaded.ReadSurface(surface.ID)
	if err != nil {
		t.Fatalf("read from new store: %v", err)
	}
	if len(detail.Widgets) != 1 || !strings.Contains(detail.Widgets[0].Widget.Source, "Add render tests") {
		t.Fatalf("persisted widget missing patch: %+v", detail.Widgets)
	}
}

func TestSurfaceStoreRejectsMalformedWidgetBeforeWrite(t *testing.T) {
	store := NewSurfaceStore(t.TempDir())
	surface, err := store.CreateSurface(SurfaceManifest{Title: "Bad Source", Channel: "general", CreatedBy: "ceo"})
	if err != nil {
		t.Fatalf("create surface: %v", err)
	}
	_, err = store.UpsertWidget(surface.ID, SurfaceWidgetFile{
		ID:     "bad",
		Title:  "Bad",
		Kind:   "checklist",
		Source: "kind: checklist\nitems: [",
	}, "ceo")
	if err == nil {
		t.Fatal("expected malformed widget to be rejected")
	}
	if _, readErr := store.ReadWidget(surface.ID, "bad"); !strings.Contains(readErr.Error(), "widget not found") {
		t.Fatalf("widget should not have been written, got %v", readErr)
	}
}

func TestSurfaceStoreRejectsAmbiguousSnippetPatch(t *testing.T) {
	store := NewSurfaceStore(t.TempDir())
	surface, err := store.CreateSurface(SurfaceManifest{Title: "Ambiguous", Channel: "general", CreatedBy: "ceo"})
	if err != nil {
		t.Fatalf("create surface: %v", err)
	}
	_, err = store.UpsertWidget(surface.ID, SurfaceWidgetFile{
		ID:    "notes",
		Title: "Notes",
		Kind:  "markdown",
		Source: `kind: markdown
markdown: |
  todo
  todo
`,
	}, "ceo")
	if err != nil {
		t.Fatalf("upsert widget: %v", err)
	}
	_, err = store.PatchWidget(surface.ID, "notes", WidgetPatchRequest{
		Actor:       "ceo",
		Mode:        "snippet",
		Search:      "todo",
		Replacement: "done",
	})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous patch error, got %v", err)
	}
	record, err := store.ReadWidget(surface.ID, "notes")
	if err != nil {
		t.Fatalf("read widget: %v", err)
	}
	if strings.Contains(record.Widget.Source, "done") {
		t.Fatalf("ambiguous patch should not write:\n%s", record.Widget.Source)
	}
}

func TestRenderWidgetTableCapsRows(t *testing.T) {
	var b strings.Builder
	b.WriteString("kind: table\ncolumns: [name]\nrows:\n")
	for i := 0; i < maxTableRowsRendered+1; i++ {
		b.WriteString("  - name: row\n")
	}
	result := RenderWidget(SurfaceWidgetFile{
		ID:     "too-big",
		Title:  "Too big",
		Kind:   "table",
		Source: b.String(),
	})
	if result.SchemaOK || result.RenderOK {
		t.Fatalf("expected oversized table to fail: %+v", result)
	}
}
