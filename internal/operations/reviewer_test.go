package operations

import (
	"strings"
	"testing"
)

func TestFixtureBlueprintsDeclareReviewerConfig(t *testing.T) {
	repoRoot := findRepoRoot(t)
	ids := operationFixtureIDs(t, repoRoot)
	if len(ids) == 0 {
		t.Fatalf("expected at least one operation fixture")
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			bp, err := LoadBlueprint(repoRoot, id)
			if err != nil {
				t.Fatalf("load blueprint: %v", err)
			}
			if strings.TrimSpace(bp.DefaultReviewer) == "" {
				t.Fatalf("expected default_reviewer on blueprint %s", id)
			}
			if bp.DefaultReviewer == ReviewerHumanOnly {
				return
			}
			slugs := agentSlugSet(bp)
			if _, ok := slugs[bp.DefaultReviewer]; !ok {
				t.Fatalf("blueprint %s default_reviewer %q not in starter agent slugs %v", id, bp.DefaultReviewer, keysOf(slugs))
			}
			for _, rule := range bp.ReviewerPaths {
				if rule.Reviewer == ReviewerHumanOnly {
					continue
				}
				if _, ok := slugs[rule.Reviewer]; !ok {
					t.Fatalf("blueprint %s reviewer_paths %q -> %q not in starter agent slugs %v", id, rule.Pattern, rule.Reviewer, keysOf(slugs))
				}
			}
		})
	}
}

func TestResolveReviewerNicheCRM(t *testing.T) {
	repoRoot := findRepoRoot(t)
	bp, err := LoadBlueprint(repoRoot, "niche-crm")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cases := []struct {
		path string
		want string
	}{
		{"team/customers/acme-co.md", "growth"},
		{"team/product/roadmap.md", "builder"},
		{"team/reviews/q4-retro.md", "reviewer"},
		{"team/random/other.md", "operator"},
		{"", "operator"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := bp.ResolveReviewer(tc.path)
			if got != tc.want {
				t.Fatalf("ResolveReviewer(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestResolveReviewerYoutubeFactory(t *testing.T) {
	repoRoot := findRepoRoot(t)
	bp, err := LoadBlueprint(repoRoot, "youtube-factory")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cases := []struct {
		path string
		want string
	}{
		{"team/research/topic-map.md", "research-lead"},
		{"team/scripts/ep-01.md", "scriptwriter"},
		{"team/production/ep-01-edl.md", "editor"},
		{"team/packaging/ep-01-thumb.md", "packaging-lead"},
		{"team/growth/shorts-plan.md", "growth-ops"},
		{"team/revenue/sponsor-tracker.md", "monetization"},
		{"team/unmatched/thing.md", "ceo"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := bp.ResolveReviewer(tc.path); got != tc.want {
				t.Fatalf("ResolveReviewer(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestResolveReviewerFallsBackToCEOWhenUnconfigured(t *testing.T) {
	bp := Blueprint{}
	if got := bp.ResolveReviewer("team/anything.md"); got != "ceo" {
		t.Fatalf("expected fallback 'ceo', got %q", got)
	}
}

func TestResolveReviewerFirstMatchWins(t *testing.T) {
	bp := Blueprint{
		DefaultReviewer: "fallback-agent",
		ReviewerPaths: ReviewerPathMap{
			{Pattern: "team/special/urgent.md", Reviewer: "first"},
			{Pattern: "team/special/**", Reviewer: "second"},
			{Pattern: "team/**", Reviewer: "third"},
		},
	}
	if got := bp.ResolveReviewer("team/special/urgent.md"); got != "first" {
		t.Fatalf("first rule should win, got %q", got)
	}
	if got := bp.ResolveReviewer("team/special/other.md"); got != "second" {
		t.Fatalf("second rule should match wildcard, got %q", got)
	}
	if got := bp.ResolveReviewer("team/regular/doc.md"); got != "third" {
		t.Fatalf("broad rule should catch, got %q", got)
	}
}

func TestResolveReviewerHumanOnlyPropagates(t *testing.T) {
	bp := Blueprint{
		DefaultReviewer: "operator",
		ReviewerPaths: ReviewerPathMap{
			{Pattern: "team/finance/**", Reviewer: ReviewerHumanOnly},
		},
	}
	if got := bp.ResolveReviewer("team/finance/payroll.md"); got != ReviewerHumanOnly {
		t.Fatalf("expected human-only, got %q", got)
	}
	if got := bp.ResolveReviewer("team/misc/note.md"); got != "operator" {
		t.Fatalf("expected operator fallback, got %q", got)
	}
}

func TestLoadBlueprintRejectsUnknownDefaultReviewer(t *testing.T) {
	root := t.TempDir()
	writeEmployeeBlueprint(t, root, "planner", minimalEmployeeBlueprint("planner"))
	writeOperationBlueprint(t, root, "bad-default-reviewer", `
id: bad-default-reviewer
name: Bad Default Reviewer
kind: general
objective: Should fail to load.
default_reviewer: ghost-agent
employee_blueprints:
  - planner
starter:
  lead_slug: planner
  agents:
    - slug: planner
      name: Planner
      employee_blueprint: planner
      checked: true
      type: lead
`)

	_, err := LoadBlueprint(root, "bad-default-reviewer")
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "default_reviewer") || !strings.Contains(msg, "ghost-agent") {
		t.Fatalf("error should mention field + value, got %q", msg)
	}
	if !strings.Contains(msg, "templates/operations/bad-default-reviewer/blueprint.yaml") {
		t.Fatalf("error should include blueprint filename, got %q", msg)
	}
}

func TestLoadBlueprintRejectsUnknownReviewerInPaths(t *testing.T) {
	root := t.TempDir()
	writeEmployeeBlueprint(t, root, "planner", minimalEmployeeBlueprint("planner"))
	writeOperationBlueprint(t, root, "bad-path-reviewer", `
id: bad-path-reviewer
name: Bad Path Reviewer
kind: general
objective: Should fail to load.
default_reviewer: planner
reviewer_paths:
  "team/playbooks/**": missing-agent
employee_blueprints:
  - planner
starter:
  lead_slug: planner
  agents:
    - slug: planner
      name: Planner
      employee_blueprint: planner
      checked: true
      type: lead
`)

	_, err := LoadBlueprint(root, "bad-path-reviewer")
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "missing-agent") || !strings.Contains(msg, "team/playbooks/**") {
		t.Fatalf("error should mention bad reviewer + pattern, got %q", msg)
	}
	if !strings.Contains(msg, "templates/operations/bad-path-reviewer/blueprint.yaml") {
		t.Fatalf("error should include blueprint filename, got %q", msg)
	}
}

func TestLoadBlueprintAcceptsHumanOnlyEverywhere(t *testing.T) {
	root := t.TempDir()
	writeEmployeeBlueprint(t, root, "planner", minimalEmployeeBlueprint("planner"))
	writeOperationBlueprint(t, root, "human-only-ok", `
id: human-only-ok
name: Human Only OK
kind: general
objective: Should load.
default_reviewer: human-only
reviewer_paths:
  "team/finance/**": human-only
  "team/other/**": planner
employee_blueprints:
  - planner
starter:
  lead_slug: planner
  agents:
    - slug: planner
      name: Planner
      employee_blueprint: planner
      checked: true
      type: lead
`)

	bp, err := LoadBlueprint(root, "human-only-ok")
	if err != nil {
		t.Fatalf("expected blueprint to load, got %v", err)
	}
	if bp.DefaultReviewer != ReviewerHumanOnly {
		t.Fatalf("expected human-only default, got %q", bp.DefaultReviewer)
	}
	if got := bp.ResolveReviewer("team/finance/budget.md"); got != ReviewerHumanOnly {
		t.Fatalf("expected human-only for finance, got %q", got)
	}
	if got := bp.ResolveReviewer("team/other/x.md"); got != "planner" {
		t.Fatalf("expected planner for other, got %q", got)
	}
}

func TestLoadBlueprintPreservesReviewerPathDeclarationOrder(t *testing.T) {
	root := t.TempDir()
	writeEmployeeBlueprint(t, root, "planner", minimalEmployeeBlueprint("planner"))
	writeEmployeeBlueprint(t, root, "builder", minimalEmployeeBlueprint("builder"))
	writeOperationBlueprint(t, root, "order-matters", `
id: order-matters
name: Order Matters
kind: general
objective: Should preserve declared order.
default_reviewer: planner
reviewer_paths:
  "team/a/specific.md": planner
  "team/a/**": builder
  "team/**": planner
employee_blueprints:
  - planner
  - builder
starter:
  lead_slug: planner
  agents:
    - slug: planner
      name: Planner
      employee_blueprint: planner
      checked: true
      type: lead
    - slug: builder
      name: Builder
      employee_blueprint: builder
      checked: true
      type: specialist
`)

	bp, err := LoadBlueprint(root, "order-matters")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(bp.ReviewerPaths) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(bp.ReviewerPaths))
	}
	if bp.ReviewerPaths[0].Pattern != "team/a/specific.md" {
		t.Fatalf("expected first rule to be the specific match, got %q", bp.ReviewerPaths[0].Pattern)
	}
	if got := bp.ResolveReviewer("team/a/specific.md"); got != "planner" {
		t.Fatalf("specific match wins, got %q", got)
	}
	if got := bp.ResolveReviewer("team/a/other.md"); got != "builder" {
		t.Fatalf("second rule should match, got %q", got)
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"*.md", "note.md", true},
		{"*.md", "nested/note.md", false},
		{"team/**", "team/notes/a.md", true},
		{"team/**", "team/notes/nested/deep/a.md", true},
		{"team/**", "other/a.md", false},
		{"team/playbooks/**", "team/playbooks/x.md", true},
		{"team/playbooks/**", "team/playbooks/", true},
		{"team/playbooks/**", "team/other/x.md", false},
		{"team/customers/*.md", "team/customers/acme.md", true},
		{"team/customers/*.md", "team/customers/acme/details.md", false},
		{"team/exact.md", "team/exact.md", true},
		{"team/exact.md", "team/exact.mdx", false},
		{"team/**/summary.md", "team/books/summary.md", true},
		{"team/**/summary.md", "team/books/q1/summary.md", true},
		{"team/**/summary.md", "team/summary.md", true},
		{"**", "anything/goes/here.md", true},
		{"", "", true},
		{"", "anything", false},
	}
	for _, tc := range cases {
		t.Run(tc.pattern+"_vs_"+tc.name, func(t *testing.T) {
			if got := matchGlob(tc.pattern, tc.name); got != tc.want {
				t.Fatalf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
			}
		})
	}
}

func TestReviewerPathMapAsMap(t *testing.T) {
	m := ReviewerPathMap{
		{Pattern: "a", Reviewer: "x"},
		{Pattern: "b", Reviewer: "y"},
	}
	flat := m.AsMap()
	if flat["a"] != "x" || flat["b"] != "y" || len(flat) != 2 {
		t.Fatalf("unexpected AsMap result: %+v", flat)
	}
	if ReviewerPathMap(nil).AsMap() != nil {
		t.Fatalf("empty map should return nil")
	}
}

// helpers below.

func agentSlugSet(bp Blueprint) map[string]struct{} {
	out := make(map[string]struct{}, len(bp.Starter.Agents))
	for _, a := range bp.Starter.Agents {
		out[strings.TrimSpace(a.Slug)] = struct{}{}
	}
	return out
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func minimalEmployeeBlueprint(id string) string {
	return `
id: ` + id + `
name: ` + strings.Title(id) + `
kind: employee
summary: Test employee blueprint.
role: testing
responsibilities:
  - Do the thing.
starting_tasks:
  - Start the thing.
automated_loops:
  - Loop the thing.
skills:
  - testing
tools:
  - docs
expected_results:
  - Thing done.
`
}
