package team

import (
	"strings"
	"testing"
)

func staticResolver(table map[string]struct {
	kind EntityKind
	slug string
}) func(string) (EntityKind, string, bool) {
	return func(s string) (EntityKind, string, bool) {
		v, ok := table[strings.TrimSpace(s)]
		if !ok {
			return "", "", false
		}
		return v.kind, v.slug, true
	}
}

func TestNormalizeLooseWikilinks_RewritesLooseDisplay(t *testing.T) {
	body := "Met with [[Acme Corp]] today."
	resolve := staticResolver(map[string]struct {
		kind EntityKind
		slug string
	}{
		"Acme Corp": {EntityKindCompanies, "acme-corp"},
	})
	got, changed := NormalizeLooseWikilinks(body, resolve)
	if !changed {
		t.Fatalf("expected changed=true; body unchanged")
	}
	want := "Met with [[companies/acme-corp|Acme Corp]] today."
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalizeLooseWikilinks_LeavesKindedLinksAlone(t *testing.T) {
	body := "Already kinded: [[people/sarah]] and [[companies/acme|Acme Corp]]"
	resolve := staticResolver(map[string]struct {
		kind EntityKind
		slug string
	}{
		"sarah": {EntityKindPeople, "sarah"},
		"acme":  {EntityKindCompanies, "acme"},
	})
	got, changed := NormalizeLooseWikilinks(body, resolve)
	if changed {
		t.Fatalf("expected no change; got %q", got)
	}
	if got != body {
		t.Fatalf("body mutated: %q", got)
	}
}

func TestNormalizeLooseWikilinks_SkipsUnresolved(t *testing.T) {
	body := "Bare [[unknown]] should pass through."
	resolve := staticResolver(map[string]struct {
		kind EntityKind
		slug string
	}{})
	got, changed := NormalizeLooseWikilinks(body, resolve)
	if changed {
		t.Fatalf("unexpected change: %q", got)
	}
	if got != body {
		t.Fatalf("body mutated: %q", got)
	}
}

func TestNormalizeLooseWikilinks_PreservesDisplayWithExistingPipe(t *testing.T) {
	body := "[[Acme|Big Acme]]"
	resolve := staticResolver(map[string]struct {
		kind EntityKind
		slug string
	}{
		"Acme": {EntityKindCompanies, "acme"},
	})
	got, changed := NormalizeLooseWikilinks(body, resolve)
	if !changed {
		t.Fatalf("expected change")
	}
	want := "[[companies/acme|Big Acme]]"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalizeLooseWikilinks_DropsPipeWhenDisplayEqualsCanonical(t *testing.T) {
	body := "[[acme|companies/acme]]"
	resolve := staticResolver(map[string]struct {
		kind EntityKind
		slug string
	}{
		"acme": {EntityKindCompanies, "acme"},
	})
	got, changed := NormalizeLooseWikilinks(body, resolve)
	if !changed {
		t.Fatalf("expected change")
	}
	want := "[[companies/acme]]"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalizeLooseWikilinks_SkipsImageEmbeds(t *testing.T) {
	body := "Image: ![[diagram.png]] and link: [[Acme]]"
	resolve := staticResolver(map[string]struct {
		kind EntityKind
		slug string
	}{
		"Acme":        {EntityKindCompanies, "acme"},
		"diagram.png": {EntityKindCompanies, "should-not-trigger"},
	})
	got, changed := NormalizeLooseWikilinks(body, resolve)
	if !changed {
		t.Fatalf("expected change for link only")
	}
	if !strings.Contains(got, "![[diagram.png]]") {
		t.Fatalf("image embed mutated: %q", got)
	}
	if !strings.Contains(got, "[[companies/acme|Acme]]") {
		t.Fatalf("link not normalized: %q", got)
	}
}

func TestNormalizeLooseWikilinks_NilResolverIsNoOp(t *testing.T) {
	body := "[[anything]]"
	got, changed := NormalizeLooseWikilinks(body, nil)
	if changed || got != body {
		t.Fatalf("nil resolver should be no-op; got %q changed=%v", got, changed)
	}
}

func TestIsBriefPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"team/people/sarah.md", true},
		{"team/companies/acme-corp.md", true},
		{"team/customers/foo.md", true},
		{"team/projects/proj1.md", true},
		{"team/learnings/lesson.md", true},
		{"team/decisions/d-2026-01.md", true},
		{"team/playbooks/onboard.md", true},
		{"team/playbooks/.compiled/skill/SKILL.md", false},
		{"team/inbox/raw/foo.md", false},
		{"team/agents/foo.md", false},
		{"team/entities/.graph.jsonl", false},
		{"team/people/Capitalized.md", false},
	}
	for _, c := range cases {
		got := isBriefPath(c.path)
		if got != c.want {
			t.Errorf("isBriefPath(%q) = %v want %v", c.path, got, c.want)
		}
	}
}
