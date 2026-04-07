package team

import "testing"

func TestPlanOfficeActionsCreatesTasksAndTags(t *testing.T) {
	signals := []officeSignal{
		{
			ID:      "ins-1",
			Source:  "nex_insights",
			Kind:    "opportunity",
			Content: "Launch messaging is drifting toward enterprise buyers",
			Channel: "general",
			Owner:   "cmo",
		},
		{
			ID:      "ins-2",
			Source:  "nex_insights",
			Kind:    "risk",
			Content: "Signup flow has backend reliability issues",
			Channel: "general",
			Owner:   "be",
		},
	}

	plan := planOfficeActions(signals)
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(plan.Tasks))
	}
	if len(plan.Tagged) != 2 {
		t.Fatalf("expected 2 tagged owners, got %d", len(plan.Tagged))
	}
	if plan.Summary == "" {
		t.Fatal("expected summary to be populated")
	}
	// Tasks without explicit RequiresHuman should still get a confirmation
	// request injected so humans approve before agents act.
	if len(plan.Requests) < 1 {
		t.Fatalf("expected at least 1 confirmation request for task routing, got %d", len(plan.Requests))
	}
	if plan.DecisionKind != "ask_human_and_create_task" {
		t.Fatalf("expected ask_human_and_create_task decision, got %q", plan.DecisionKind)
	}
}

func TestPlanOfficeActionsCreatesHumanRequestForApprovalSignal(t *testing.T) {
	signals := []officeSignal{
		{
			ID:            "ins-3",
			Source:        "nex_insights",
			Kind:          "risk",
			Content:       "Contract approval is needed before we can proceed",
			Channel:       "general",
			Owner:         "cro",
			RequiresHuman: true,
			Blocking:      true,
		},
	}

	plan := planOfficeActions(signals)
	if len(plan.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(plan.Requests))
	}
	if !plan.Requests[0].Blocking {
		t.Fatal("expected request to stay blocking")
	}
	if plan.Requests[0].Kind != "approval" {
		t.Fatalf("expected approval request, got %q", plan.Requests[0].Kind)
	}
	if len(plan.Requests[0].Options) == 0 {
		t.Fatal("expected approval request options")
	}
}

func TestBuildNotificationSignalsInfersOwner(t *testing.T) {
	signals := buildNotificationSignals([]nexFeedItem{{
		ID:   "feed-1",
		Type: "context_alert",
		Content: nexFeedItemContent{
			ImportantItems: []nexFeedItemContentItem{{
				Title:   "Pricing pressure",
				Context: "Budget pushback is growing",
			}},
		},
	}})

	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].Owner != "cro" {
		t.Fatalf("expected CRO owner, got %q", signals[0].Owner)
	}
}

func TestPlanOfficeActionsDedupeSameOwnerTask(t *testing.T) {
	signals := []officeSignal{
		{ID: "1", Source: "nex_insights", Kind: "risk", Content: "Budget pressure is growing in pipeline", Channel: "general", Owner: "cro"},
		{ID: "2", Source: "nex_notifications", Kind: "context_alert", Content: "Budget pressure is growing in pipeline", Channel: "general", Owner: "cro"},
	}

	plan := planOfficeActions(signals)
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected deduped tasks, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].Owner != "cro" {
		t.Fatalf("expected CRO owner, got %q", plan.Tasks[0].Owner)
	}
	if plan.Tasks[0].Details == "" || plan.Tasks[0].Details == signals[0].Content {
		t.Fatalf("expected richer task details, got %q", plan.Tasks[0].Details)
	}
}

func TestRequestKindForSignalChoice(t *testing.T) {
	signal := officeSignal{
		Content: "We need to choose between speed and caution for this launch decision",
		Kind:    "decision",
	}
	if got := requestKindForSignal(signal); got != "choice" {
		t.Fatalf("expected choice request, got %q", got)
	}
}

func TestSignalNeedsHumanDefaultsTrue(t *testing.T) {
	// Generic signals with no special keywords should route to human.
	cases := []struct {
		content string
		kind    string
	}{
		{"Enterprise pipeline is growing", "opportunity"},
		{"New competitor entered the market", "risk"},
		{"Signup funnel conversion dropped 5%", "metric"},
	}
	for _, tc := range cases {
		requiresHuman, _ := signalNeedsHuman(tc.content, tc.kind)
		if !requiresHuman {
			t.Errorf("expected human routing for %q (%s)", tc.content, tc.kind)
		}
	}
}

func TestSignalNeedsHumanAutonomousPatterns(t *testing.T) {
	// Informational signals matching autonomous-safe patterns should not
	// require human intervention.
	cases := []struct {
		content string
		kind    string
	}{
		{"FYI: weekly metrics are in", "summary"},
		{"Status update on the deploy", "notification"},
		{"Issue resolved — pipeline is green again", "resolution"},
		{"No action needed on this alert", "context_alert"},
		{"Task completed successfully", "notification"},
	}
	for _, tc := range cases {
		requiresHuman, _ := signalNeedsHuman(tc.content, tc.kind)
		if requiresHuman {
			t.Errorf("expected autonomous for %q (%s)", tc.content, tc.kind)
		}
	}
}

func TestSignalNeedsHumanBlockingPatterns(t *testing.T) {
	cases := []struct {
		content string
		kind    string
	}{
		{"Contract approval is required", "risk"},
		{"Legal review pending", "compliance"},
		{"Security review flagged an issue", "alert"},
		{"Need permission to access production", "request"},
	}
	for _, tc := range cases {
		requiresHuman, blocking := signalNeedsHuman(tc.content, tc.kind)
		if !requiresHuman || !blocking {
			t.Errorf("expected blocking human routing for %q (%s), got requiresHuman=%v blocking=%v",
				tc.content, tc.kind, requiresHuman, blocking)
		}
	}
}

func TestSignalRequestOptionsRichApproval(t *testing.T) {
	signal := officeSignal{Content: "Need approval to proceed", Blocking: true}
	opts := signalRequestOptions(signal)
	if len(opts) != 5 {
		t.Fatalf("expected 5 approval options, got %d", len(opts))
	}
	ids := map[string]bool{}
	for _, o := range opts {
		ids[o.ID] = true
		if o.Label == "" || o.Description == "" {
			t.Fatalf("option %q missing label or description", o.ID)
		}
	}
	for _, required := range []string{"approve", "approve_with_note", "needs_more_info", "reject", "reject_with_steer"} {
		if !ids[required] {
			t.Errorf("missing required approval option %q", required)
		}
	}
}

func TestSignalRequestOptionsRichChoice(t *testing.T) {
	signal := officeSignal{Content: "We need to choose a direction", Kind: "decision"}
	opts := signalRequestOptions(signal)
	if len(opts) != 5 {
		t.Fatalf("expected 5 choice options, got %d", len(opts))
	}
	ids := map[string]bool{}
	for _, o := range opts {
		ids[o.ID] = true
	}
	for _, required := range []string{"move_fast", "balanced", "be_careful", "needs_more_info", "delegate"} {
		if !ids[required] {
			t.Errorf("missing required choice option %q", required)
		}
	}
}

func TestSignalRequestOptionsConfirm(t *testing.T) {
	signal := officeSignal{Content: "Please confirm the deployment plan", Kind: "confirm"}
	opts := signalRequestOptions(signal)
	if len(opts) != 4 {
		t.Fatalf("expected 4 confirm options, got %d", len(opts))
	}
	ids := map[string]bool{}
	for _, o := range opts {
		ids[o.ID] = true
	}
	for _, required := range []string{"confirm_proceed", "adjust", "reassign", "hold"} {
		if !ids[required] {
			t.Errorf("missing required confirm option %q", required)
		}
	}
}

func TestSignalRequestOptionsDefault(t *testing.T) {
	// A generic signal that doesn't match approval/choice/confirm keywords
	// should still get fallback options.
	signal := officeSignal{Content: "Something happened", Kind: "unknown"}
	// requestKindForSignal returns "approval" for generic signals, but let's
	// test the default branch directly by crafting a non-matching signal.
	// Since the default case in signalRequestOptions returns options, we
	// verify via a signal that hits "approval" (the common default path).
	opts := signalRequestOptions(signal)
	if len(opts) == 0 {
		t.Fatal("expected fallback options for generic signal, got none")
	}
	for _, o := range opts {
		if o.Label == "" || o.Description == "" {
			t.Fatalf("option %q has empty label or description", o.ID)
		}
	}
}

func TestPlanOfficeActionsTasksGetConfirmationRequest(t *testing.T) {
	// Signals with clear owners but no RequiresHuman flag should still
	// generate a confirmation request routing the decision to a human.
	signals := []officeSignal{
		{
			ID:      "sig-1",
			Source:  "nex_insights",
			Kind:    "opportunity",
			Content: "Campaign performance is exceeding targets",
			Channel: "general",
			Owner:   "cmo",
		},
	}

	plan := planOfficeActions(signals)
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}
	if len(plan.Requests) != 1 {
		t.Fatalf("expected 1 confirmation request, got %d", len(plan.Requests))
	}
	req := plan.Requests[0]
	if req.Kind != "confirm" {
		t.Fatalf("expected confirm request kind, got %q", req.Kind)
	}
	if !req.Blocking {
		t.Fatal("expected confirmation request to be blocking")
	}
	if req.RecommendedID != "confirm_proceed" {
		t.Fatalf("expected recommended_id confirm_proceed, got %q", req.RecommendedID)
	}
	if len(req.Options) != 4 {
		t.Fatalf("expected 4 confirm options, got %d", len(req.Options))
	}
	if plan.DecisionKind != "ask_human_and_create_task" {
		t.Fatalf("expected ask_human_and_create_task, got %q", plan.DecisionKind)
	}
}

func TestRecommendedIDForKind(t *testing.T) {
	cases := map[string]string{
		"approval": "approve",
		"choice":   "balanced",
		"confirm":  "confirm_proceed",
		"unknown":  "proceed",
		"":         "proceed",
	}
	for kind, expected := range cases {
		if got := recommendedIDForKind(kind); got != expected {
			t.Errorf("recommendedIDForKind(%q) = %q, want %q", kind, got, expected)
		}
	}
}

func TestRequestKindForSignalConfirm(t *testing.T) {
	cases := []struct {
		content string
		kind    string
	}{
		{"Please confirm this plan", "action"},
		{"We need to verify the numbers", "metric"},
		{"Time to review the proposal", "review"},
	}
	for _, tc := range cases {
		signal := officeSignal{Content: tc.content, Kind: tc.kind}
		if got := requestKindForSignal(signal); got != "confirm" {
			t.Errorf("requestKindForSignal(%q, %q) = %q, want confirm", tc.content, tc.kind, got)
		}
	}
}

func TestApprovalRequestHasRecommendedID(t *testing.T) {
	signals := []officeSignal{
		{
			ID:            "ins-4",
			Source:        "nex_insights",
			Kind:          "risk",
			Content:       "Legal approval needed for partner agreement",
			Channel:       "general",
			Owner:         "cro",
			RequiresHuman: true,
			Blocking:      true,
		},
	}

	plan := planOfficeActions(signals)
	if len(plan.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(plan.Requests))
	}
	if plan.Requests[0].RecommendedID != "approve" {
		t.Fatalf("expected recommended_id approve, got %q", plan.Requests[0].RecommendedID)
	}
}
