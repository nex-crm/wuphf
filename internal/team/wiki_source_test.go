package team

import (
	"strings"
	"testing"
	"time"
)

func TestContentHashHexDeterministicAndTrimInsensitive(t *testing.T) {
	a := ContentHashHex("hello world")
	b := ContentHashHex("hello world\n\n")
	c := ContentHashHex("hello world  ")
	if a != b || a != c {
		t.Fatalf("expected trailing whitespace to be ignored: %q %q %q", a, b, c)
	}
	if a == ContentHashHex("hello worlds") {
		t.Fatal("expected distinct content to hash differently")
	}
	if len(a) != 64 {
		t.Fatalf("expected 64-hex sha256, got %d chars", len(a))
	}
}

func TestNewSourceRecordValidation(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name               string
		id, title, content string
		kind               SourceKind
		captured           time.Time
		wantErr            bool
	}{
		{"ok", "task-wup-12", "Title", "body", SourceKindTask, now, false},
		{"empty id", "", "Title", "body", SourceKindTask, now, true},
		{"bad kind", "x", "Title", "body", SourceKind("bogus"), now, true},
		{"empty title", "x", "  ", "body", SourceKindTask, now, true},
		{"empty content", "x", "Title", "  ", SourceKindTask, now, true},
		{"zero time", "x", "Title", "body", SourceKindTask, time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSourceRecord(tc.id, tc.kind, tc.title, "origin", tc.content, tc.captured)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v got err=%v", tc.wantErr, err)
			}
		})
	}
}

func TestSourceRecordRoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 26, 9, 30, 0, 0, time.UTC)
	body := "# Launch retro\n\nDecided to ship Friday.\n\n- risk: infra"
	rec, err := NewSourceRecord("decision-launch-42", SourceKindDecision, "Ship date: Friday", "task-WUP-42", body, now)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if rec.ContentHash != ContentHashHex(body) {
		t.Fatal("hash not computed")
	}

	data, err := RenderSourceMarkdown(rec)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.HasPrefix(string(data), "---\n") {
		t.Fatal("expected leading frontmatter delimiter")
	}

	got, err := ParseSourceMarkdown(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.ID != rec.ID || got.Kind != rec.Kind || got.Title != rec.Title ||
		got.Origin != rec.Origin || got.ContentHash != rec.ContentHash {
		t.Fatalf("frontmatter mismatch: %+v vs %+v", got, rec)
	}
	if !got.CapturedAt.Equal(rec.CapturedAt) {
		t.Fatalf("time mismatch: %v vs %v", got.CapturedAt, rec.CapturedAt)
	}
	if strings.TrimRight(got.Content, "\n") != strings.TrimRight(body, "\n") {
		t.Fatalf("body mismatch:\n%q\nvs\n%q", got.Content, body)
	}
}

func TestSourceRelPathAndDetection(t *testing.T) {
	rec, err := NewSourceRecord("task-wup-7", SourceKindTask, "T", "WUP-7", "b", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want := "sources/task/task-wup-7.md"
	if rec.RelPath() != want {
		t.Fatalf("relpath = %q want %q", rec.RelPath(), want)
	}
	if !IsSourcePath(want) {
		t.Fatal("expected IsSourcePath true for sources path")
	}
	if IsSourcePath("team/people/nazz.md") {
		t.Fatal("expected IsSourcePath false for team article")
	}
	if IsSourcePath("sources/task/x.txt") {
		t.Fatal("expected IsSourcePath false for non-md")
	}
}

func TestDeriveSourceIDStableByOrigin(t *testing.T) {
	// Same origin → same id (write-once dedupe), regardless of content.
	a := DeriveSourceID(SourceKindTask, "WUP-12", "First title", "body one")
	b := DeriveSourceID(SourceKindTask, "WUP-12", "Different title", "body two")
	if a != b {
		t.Fatalf("expected stable id by origin, got %q vs %q", a, b)
	}
	if a != "task-wup-12" {
		t.Fatalf("unexpected id %q", a)
	}
	// No origin → id varies by content hash suffix so distinct notes don't collide.
	c := DeriveSourceID(SourceKindNote, "", "My note", "alpha")
	d := DeriveSourceID(SourceKindNote, "", "My note", "beta")
	if c == d {
		t.Fatalf("expected distinct ids for distinct content, got %q", c)
	}
	if !strings.HasPrefix(c, "note-my-note-") {
		t.Fatalf("unexpected note id %q", c)
	}
}
