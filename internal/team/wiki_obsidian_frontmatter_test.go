package team

import (
	"strings"
	"testing"
	"time"
)

func TestApplyHumanEditSentinel_NoFrontmatterUnchanged(t *testing.T) {
	body := "# Plain markdown\n\nNo frontmatter here.\n"
	out, err := applyHumanEditSentinel(body, time.Unix(1700000000, 0).UTC())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out != body {
		t.Fatalf("expected unchanged; got %q", out)
	}
}

func TestApplyHumanEditSentinel_AppendsWhenMissing(t *testing.T) {
	body := "---\nkind: people\nslug: sarah\n---\n\n# Sarah\n\nBody\n"
	ts := time.Date(2026, 5, 18, 12, 34, 56, 0, time.UTC)
	out, err := applyHumanEditSentinel(body, ts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := "last_human_edit_ts: " + ts.Format(time.RFC3339)
	if !strings.Contains(out, want) {
		t.Fatalf("missing sentinel: %q", out)
	}
	if !strings.Contains(out, "kind: people") || !strings.Contains(out, "slug: sarah") {
		t.Fatalf("dropped existing keys: %q", out)
	}
	if !strings.Contains(out, "# Sarah\n\nBody\n") {
		t.Fatalf("body changed: %q", out)
	}
}

func TestApplyHumanEditSentinel_RewritesExisting(t *testing.T) {
	body := "---\nkind: people\nlast_human_edit_ts: 2020-01-01T00:00:00Z\nslug: sarah\n---\n\nbody\n"
	ts := time.Date(2026, 5, 18, 12, 34, 56, 0, time.UTC)
	out, err := applyHumanEditSentinel(body, ts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.Contains(out, "2020-01-01T00:00:00Z") {
		t.Fatalf("old timestamp not replaced: %q", out)
	}
	if strings.Count(out, "last_human_edit_ts:") != 1 {
		t.Fatalf("sentinel duplicated: %q", out)
	}
	if !strings.Contains(out, "last_human_edit_ts: "+ts.Format(time.RFC3339)) {
		t.Fatalf("expected new timestamp: %q", out)
	}
	// Preserved key ordering: kind, then slug.
	kindIdx := strings.Index(out, "kind: people")
	slugIdx := strings.Index(out, "slug: sarah")
	if kindIdx < 0 || slugIdx < 0 || kindIdx > slugIdx {
		t.Fatalf("ordering disturbed: %q", out)
	}
}

func TestApplyHumanEditSentinel_MalformedFrontmatterUnchanged(t *testing.T) {
	body := "---\nkind: people\n\nno closing delimiter\n"
	ts := time.Now().UTC()
	out, err := applyHumanEditSentinel(body, ts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out != body {
		t.Fatalf("expected unchanged for malformed: %q", out)
	}
}
