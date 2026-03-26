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
