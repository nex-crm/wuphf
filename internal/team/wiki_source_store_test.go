package team

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"
)

// commitSourceRecord renders + commits a record straight through the repo
// (bypassing the worker), returning the record for assertions.
func commitSourceRecord(t *testing.T, repo *Repo, kind SourceKind, origin, title, content string, capturedAt time.Time) SourceRecord {
	t.Helper()
	id := DeriveSourceID(kind, origin, title, content)
	rec, err := NewSourceRecord(id, kind, title, origin, content, capturedAt)
	if err != nil {
		t.Fatalf("NewSourceRecord: %v", err)
	}
	body, err := RenderSourceMarkdown(rec)
	if err != nil {
		t.Fatalf("RenderSourceMarkdown: %v", err)
	}
	if _, _, err := repo.CommitSource(context.Background(), rec.RelPath(), string(body), ""); err != nil {
		t.Fatalf("CommitSource: %v", err)
	}
	return rec
}

func TestListSources_EmptyRepo(t *testing.T) {
	repo := newSourceCommitRepo(t)
	records, err := ListSources(repo)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records on empty repo, got %d", len(records))
	}
}

func TestListSources_SortedByCapturedAtDesc(t *testing.T) {
	repo := newSourceCommitRepo(t)
	base := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	// Commit out of chronological order across multiple kinds.
	commitSourceRecord(t, repo, SourceKindNote, "", "Oldest", "a", base)
	commitSourceRecord(t, repo, SourceKindDoc, "", "Newest", "b", base.Add(2*time.Hour))
	commitSourceRecord(t, repo, SourceKindURL, "https://example.com", "Middle", "c", base.Add(time.Hour))

	records, err := ListSources(repo)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	wantOrder := []string{"Newest", "Middle", "Oldest"}
	for i, want := range wantOrder {
		if records[i].Title != want {
			t.Fatalf("record[%d].Title = %q, want %q (full order: %v)", i, records[i].Title, want, titles(records))
		}
	}
	// List records carry their content hash; body is parsed back too.
	if records[0].Content != "b" {
		t.Fatalf("Newest content = %q, want %q", records[0].Content, "b")
	}
}

func titles(records []SourceRecord) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.Title
	}
	return out
}

func TestReadSource_RoundTrip(t *testing.T) {
	repo := newSourceCommitRepo(t)
	captured := time.Date(2026, 6, 20, 9, 30, 0, 0, time.UTC)
	rec := commitSourceRecord(t, repo, SourceKindURL, "https://nex.ai/post", "Nex post", "Hello from the URL body.", captured)

	got, err := ReadSource(repo, rec.Kind, rec.ID)
	if err != nil {
		t.Fatalf("ReadSource: %v", err)
	}
	if got.ID != rec.ID || got.Kind != rec.Kind || got.Title != rec.Title {
		t.Fatalf("metadata mismatch: got %+v want %+v", got, rec)
	}
	if got.Origin != rec.Origin {
		t.Fatalf("origin mismatch: got %q want %q", got.Origin, rec.Origin)
	}
	if got.Content != "Hello from the URL body." {
		t.Fatalf("content mismatch: got %q", got.Content)
	}
	if got.ContentHash != ContentHashHex("Hello from the URL body.") {
		t.Fatalf("content hash mismatch: got %q", got.ContentHash)
	}
	if !got.CapturedAt.Equal(captured) {
		t.Fatalf("captured_at mismatch: got %v want %v", got.CapturedAt, captured)
	}
}

func TestReadSource_NotFound(t *testing.T) {
	repo := newSourceCommitRepo(t)
	_, err := ReadSource(repo, SourceKindNote, "note-does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist, got %v", err)
	}
}

func TestReadSource_InvalidKind(t *testing.T) {
	repo := newSourceCommitRepo(t)
	_, err := ReadSource(repo, SourceKind("bogus"), "x")
	if err == nil {
		t.Fatal("expected error for invalid kind")
	}
}
