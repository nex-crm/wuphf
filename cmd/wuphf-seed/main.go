// wuphf-seed populates a dev broker home with the 3 Sam ICP tutorial
// scenarios so a fresh dev launch lands on a non-empty Decision Inbox.
// Honours WUPHF_RUNTIME_HOME for path isolation (mirroring the dev
// alias). Idempotent: re-running replaces tutorial tasks but leaves
// other state untouched.
//
// Usage:
//
//	WUPHF_RUNTIME_HOME="$HOME/.wuphf-dev-home" go run ./cmd/wuphf-seed
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
)

func main() {
	home := strings.TrimSpace(os.Getenv("WUPHF_RUNTIME_HOME"))
	if home == "" {
		home = config.RuntimeHomeDir()
	}
	if home == "" {
		log.Fatal("wuphf-seed: cannot resolve runtime home")
	}
	statePath := filepath.Join(home, ".wuphf", "team", "broker-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		log.Fatalf("wuphf-seed: mkdir state dir: %v", err)
	}

	b := team.NewBrokerAt(statePath)

	// Add the personas referenced by the tutorial scenarios so they
	// resolve as channel members + reviewer routing targets.
	for _, p := range []struct{ slug, name string }{
		{"sam", "Sam (founder)"},
		{"tess", "Tess"},
		{"miles", "Miles"},
		{"nico", "Nico"},
		{"wren", "Wren"},
		{"kit", "Kit"},
	} {
		if err := b.EnsureBridgedMember(p.slug, p.name, "seed"); err != nil {
			log.Fatalf("wuphf-seed: ensure member %q: %v", p.slug, err)
		}
	}

	channel := "general"

	tut1, err := seedTutorial1(b, channel)
	must(err, "tutorial 1")
	tut2, err := seedTutorial2(b, channel)
	must(err, "tutorial 2")
	tut3a, tut3b, err := seedTutorial3(b, channel)
	must(err, "tutorial 3")

	fmt.Printf("seeded:\n")
	fmt.Printf("  tutorial 1 (single-task happy path)   task=%s state=decision\n", tut1)
	fmt.Printf("  tutorial 2 (reviewer timeout)         task=%s state=review (1 of 3 reviewers graded)\n", tut2)
	fmt.Printf("  tutorial 3a (blocked task)            task=%s state=blocked_on_pr_merge\n", tut3a)
	fmt.Printf("  tutorial 3b (mergeable blocker)       task=%s state=decision\n", tut3b)
	fmt.Printf("\nstate path: %s\n", statePath)
}

func must(err error, where string) {
	if err != nil {
		log.Fatalf("wuphf-seed: %s: %v", where, err)
	}
}

func seedTutorial1(b *team.Broker, channel string) (string, error) {
	t, _, err := b.EnsureTask(
		channel,
		"Refactor agent rail to use shared severity tokens",
		"Three reviewers all graded the agent-rail refactor; the changes look tight but tess flagged one issue worth a look before merge.",
		"tess",
		"sam",
		"",
	)
	if err != nil {
		return "", err
	}
	id := t.ID
	must(b.SetSpec(id, team.Spec{
		Problem:       "Agent rail in /apps/tasks duplicates the severity-token logic from lifecycle.css",
		TargetOutcome: "One source of truth for severity tokens; no visual drift between surfaces",
		AcceptanceCriteria: []team.ACItem{
			{Statement: "All severity badges use STATE_PILL_TOKENS from lifecycle.ts", Done: true},
			{Statement: "Vitest snapshot tests pass on agent rail + decision inbox + packet view", Done: true},
			{Statement: "lifecycle.css's local severity vars removed", Done: false},
		},
		Assignment: "Refactor agent rail to import severity tokens from lifecycle.ts. Keep all visual states identical.",
		AutoAssign: "tess,nico,wren",
	}), "tutorial 1 SetSpec")
	must(b.TransitionLifecycle(id, team.LifecycleStateRunning, "tutorial 1: implementation started"), "tutorial 1 → running")
	must(b.AssignReviewers(id, []string{"tess", "nico", "wren"}), "tutorial 1 AssignReviewers")
	must(b.TransitionLifecycle(id, team.LifecycleStateReview, "tutorial 1: implementation done"), "tutorial 1 → review")
	must(b.AppendReviewerGrade(id, team.ReviewerGrade{
		ReviewerSlug: "tess",
		Severity:     team.SeverityMinor,
		Suggestion:   "Add a short comment on the export line explaining the shared-token rule",
		Reasoning:    "The token rename is correct but future contributors will not know the rule is enforced by tests",
		FilePath:     "web/src/lib/types/lifecycle.ts",
		Line:         164,
		SubmittedAt:  time.Now().UTC().Add(-30 * time.Minute),
	}), "tutorial 1 tess grade")
	must(b.AppendReviewerGrade(id, team.ReviewerGrade{
		ReviewerSlug: "nico",
		Severity:     team.SeverityNitpick,
		Suggestion:   "Sort the token exports alphabetically",
		Reasoning:    "Easier to scan in the diff",
		SubmittedAt:  time.Now().UTC().Add(-22 * time.Minute),
	}), "tutorial 1 nico grade")
	must(b.AppendReviewerGrade(id, team.ReviewerGrade{
		ReviewerSlug: "wren",
		Severity:     team.SeverityMinor,
		Suggestion:   "Local css var removal looks good; consider also dropping the legacy comment block",
		Reasoning:    "Comment refers to an old name",
		FilePath:     "web/src/styles/lifecycle.css",
		Line:         284,
		SubmittedAt:  time.Now().UTC().Add(-12 * time.Minute),
	}), "tutorial 1 wren grade")
	// Three graded grades trigger convergence → decision. EvaluateConvergence
	// is called by AppendReviewerGrade's routing-side mirror.
	return id, nil
}

func seedTutorial2(b *team.Broker, channel string) (string, error) {
	t, _, err := b.EnsureTask(
		channel,
		"Reviewer-timeout demo: notebook review queue migration",
		"Two of three reviewers have graded; the third has not responded. The 10-minute timeout will fill the missing slot with a skipped placeholder.",
		"miles",
		"sam",
		"",
	)
	if err != nil {
		return "", err
	}
	id := t.ID
	must(b.SetSpec(id, team.Spec{
		Problem:       "Notebook review queue overlaps with the wiki Reviews tab. One surface should win.",
		TargetOutcome: "Drop the notebook-specific review queue; fold per-agent filter into /reviews.",
		AcceptanceCriteria: []team.ACItem{
			{Statement: "Single /reviews surface with per-agent filter via query param", Done: true},
			{Statement: "Existing notebook review API redirects to /reviews", Done: true},
			{Statement: "Vitest E2E covers the consolidated flow", Done: false},
		},
		Assignment: "Migrate notebook review queue into /reviews. Add the per-agent filter as a search param. Deprecate the notebook-specific API.",
		AutoAssign: "tess,wren,kit",
	}), "tutorial 2 SetSpec")
	must(b.TransitionLifecycle(id, team.LifecycleStateRunning, "tutorial 2: implementation started"), "tutorial 2 → running")
	must(b.AssignReviewers(id, []string{"tess", "wren", "kit"}), "tutorial 2 AssignReviewers")
	must(b.TransitionLifecycle(id, team.LifecycleStateReview, "tutorial 2: implementation done"), "tutorial 2 → review")
	must(b.AppendReviewerGrade(id, team.ReviewerGrade{
		ReviewerSlug: "tess",
		Severity:     team.SeverityMajor,
		Suggestion:   "Add migration guard so existing /notebooks/reviews requests redirect cleanly",
		Reasoning:    "Stale bookmarks would 404 without an explicit redirect",
		FilePath:     "internal/team/broker_notebook.go",
		Line:         412,
		SubmittedAt:  time.Now().UTC().Add(-9 * time.Minute),
	}), "tutorial 2 tess grade")
	must(b.AppendReviewerGrade(id, team.ReviewerGrade{
		ReviewerSlug: "wren",
		Severity:     team.SeverityMinor,
		Suggestion:   "The query param name should match the existing wiki convention (?agent=<slug>)",
		Reasoning:    "Consistency with the wiki search params",
		FilePath:     "web/src/routes/reviewsRoute.tsx",
		Line:         48,
		SubmittedAt:  time.Now().UTC().Add(-4 * time.Minute),
	}), "tutorial 2 wren grade")
	// Kit has NOT graded; this task stays in `review` until the
	// convergence sweeper fires its 10-minute timeout. The user will see
	// it sitting in `review` state with 2/3 grades visible.
	return id, nil
}

func seedTutorial3(b *team.Broker, channel string) (taskA, taskB string, err error) {
	tA, _, err := b.EnsureTask(
		channel,
		"Inbox cascade demo: depends on the badge feature",
		"This task depends on the new Inbox sidebar entry shipping first. Once the blocker merges, this task automatically transitions to review.",
		"tess",
		"sam",
		"",
	)
	if err != nil {
		return "", "", err
	}
	taskA = tA.ID

	tB, _, err := b.EnsureTask(
		channel,
		"Add Inbox sidebar entry with decisionRequired badge + Web Audio ding",
		"The Inbox feature that PR-A depends on. Two reviewers graded clean, one critical found in the keyboard nav path.",
		"miles",
		"sam",
		"",
	)
	if err != nil {
		return "", "", err
	}
	taskB = tB.ID

	must(b.SetSpec(taskA, team.Spec{
		Problem:       "Reviewer-routing E2E test fails because the test setup expected the Inbox in the sidebar but Lane G shipped it as route-only",
		TargetOutcome: "E2E test passes against the inbox-with-badge build",
		AcceptanceCriteria: []team.ACItem{
			{Statement: "E2E walks: sidebar Inbox → badge >0 → click → packet view", Done: false},
			{Statement: "Ding triggered on count increase asserted via Web Audio mock", Done: false},
		},
		Assignment: "Update reviewer-routing E2E to include the new sidebar entry and badge polling assertion.",
		AutoAssign: "tess",
	}), "tutorial 3 task A SetSpec")
	must(b.SetSpec(taskB, team.Spec{
		Problem:       "Decision Inbox lacks a sidebar entry + a badge for the decisionRequired count.",
		TargetOutcome: "Inbox is first-class: sidebar icon + badge + audio cue on new decisions.",
		AcceptanceCriteria: []team.ACItem{
			{Statement: "BellNotification icon in sidebar (iconoir-react)", Done: true},
			{Statement: "Badge polls /tasks/inbox every 5s and shows decisionRequired count", Done: true},
			{Statement: "Two-tone Web Audio ding when count strictly increases", Done: true},
			{Statement: "Vitest covers badge + sound trigger", Done: false},
		},
		Assignment: "Add Inbox to SIDEBAR_APPS. Wire BellNotification icon. Poll /tasks/inbox. Play Web Audio ding when count increases.",
		AutoAssign: "miles,tess,nico",
	}), "tutorial 3 task B SetSpec")
	must(b.TransitionLifecycle(taskB, team.LifecycleStateRunning, "tutorial 3 task B: implementation started"), "tutorial 3 task B → running")
	must(b.AssignReviewers(taskB, []string{"miles", "tess", "nico"}), "tutorial 3 task B AssignReviewers")
	must(b.TransitionLifecycle(taskB, team.LifecycleStateReview, "tutorial 3 task B: implementation done"), "tutorial 3 task B → review")
	must(b.AppendReviewerGrade(taskB, team.ReviewerGrade{
		ReviewerSlug: "miles",
		Severity:     team.SeverityCritical,
		Suggestion:   "Keyboard nav: focus is trapped on the badge button when count=0 and no badge renders. Tab order breaks.",
		Reasoning:    "Sidebar item without a visible badge still has a tabbable button child; users tabbing through the sidebar will stop on nothing",
		FilePath:     "web/src/components/sidebar/AppList.tsx",
		Line:         110,
		SubmittedAt:  time.Now().UTC().Add(-15 * time.Minute),
	}), "tutorial 3 task B miles grade")
	must(b.AppendReviewerGrade(taskB, team.ReviewerGrade{
		ReviewerSlug: "tess",
		Severity:     team.SeverityNitpick,
		Suggestion:   "Document the 0.05 gain default in notificationSound.ts comment",
		Reasoning:    "Future contributors will not know why this number",
		FilePath:     "web/src/lib/notificationSound.ts",
		Line:         34,
		SubmittedAt:  time.Now().UTC().Add(-10 * time.Minute),
	}), "tutorial 3 task B tess grade")
	must(b.AppendReviewerGrade(taskB, team.ReviewerGrade{
		ReviewerSlug: "nico",
		Severity:     team.SeverityMinor,
		Suggestion:   "Inbox badge polling at 5s is acceptable but consider SSE in v1.1",
		Reasoning:    "5s lag on count increase is fine for a single-user OSS tool; multi-user would want push",
		SubmittedAt:  time.Now().UTC().Add(-8 * time.Minute),
	}), "tutorial 3 task B nico grade")
	// Three grades → convergence → taskB lands in decision state, ready
	// for the user to Merge → cascade-unblock taskA.

	// Block taskA on taskB. The 4-arg variant populates task.BlockedOn
	// so the unblock cascade fires automatically when taskB merges.
	if _, ok, err := b.BlockTask(taskA, "sam", "tutorial 3 demo: blocked on inbox feature ("+taskB+") shipping first", taskB); err != nil || !ok {
		log.Fatalf("wuphf-seed: tutorial 3 BlockTask: ok=%v err=%v", ok, err)
	}

	return taskA, taskB, nil
}
