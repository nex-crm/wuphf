package action

import (
	"context"
	"errors"
	"testing"
)

func slackCandidates() []Action {
	return []Action{
		{ActionID: "SLACK_SENDS_A_MESSAGE", Title: "Send a message"},
		{ActionID: "SLACK_LIST_CHANNELS", Title: "List channels"},
	}
}

func resolverWith(search ActionSearchFunc, llm LLMCompleteFunc) *ComposioActionResolver {
	return NewComposioActionResolver(search, llm)
}

func TestResolverBindsActionFromCandidate(t *testing.T) {
	search := func(_ context.Context, _, _ string) ([]Action, error) { return slackCandidates(), nil }
	llm := func(_ context.Context, _, _ string) (string, error) {
		return "```json\n{\"action_id\":\"SLACK_SENDS_A_MESSAGE\",\"params\":{\"channel\":\"#sales\"},\"run_if\":\"steps.score.result.fit >= 80\"}\n```", nil
	}
	r := resolverWith(search, llm)

	step := PlanStep{ID: "alert", Kind: "action", Title: "Post to Slack", Integration: "Slack", Gated: true}
	bound, err := r.Resolve(context.Background(), Plan{Name: "x", Steps: []PlanStep{step}}, step)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if bound.Type != "action" || bound.Platform != "slack" || bound.ActionID != "SLACK_SENDS_A_MESSAGE" {
		t.Fatalf("unexpected bound action: %#v", bound)
	}
	if bound.Params["channel"] != "#sales" {
		t.Fatalf("params not mapped: %#v", bound.Params)
	}
	if bound.RunIf != "steps.score.result.fit >= 80" {
		t.Fatalf("run_if not authored: %q", bound.RunIf)
	}
}

func TestResolverRejectsInventedActionAndFallsBack(t *testing.T) {
	search := func(_ context.Context, _, _ string) ([]Action, error) { return slackCandidates(), nil }
	// The model picks an action id that is NOT a candidate — must not be trusted.
	llm := func(_ context.Context, _, _ string) (string, error) {
		return `{"action_id":"SLACK_DELETE_WORKSPACE","params":{}}`, nil
	}
	r := resolverWith(search, llm)
	step := PlanStep{ID: "a", Kind: "action", Title: "Post", Integration: "Slack"}
	bound, _ := r.Resolve(context.Background(), Plan{Steps: []PlanStep{step}}, step)
	if bound.Type != "template" {
		t.Fatalf("invented action should fall back to a template, got %#v", bound)
	}
}

func TestResolverDropsMalformedRunIfButKeepsAction(t *testing.T) {
	search := func(_ context.Context, _, _ string) ([]Action, error) { return slackCandidates(), nil }
	llm := func(_ context.Context, _, _ string) (string, error) {
		return `{"action_id":"SLACK_SENDS_A_MESSAGE","params":{},"run_if":"not a comparison"}`, nil
	}
	r := resolverWith(search, llm)
	step := PlanStep{ID: "a", Kind: "action", Title: "Post", Integration: "Slack"}
	bound, _ := r.Resolve(context.Background(), Plan{Steps: []PlanStep{step}}, step)
	if bound.Type != "action" || bound.RunIf != "" {
		t.Fatalf("malformed run_if should be dropped, action kept; got %#v", bound)
	}
}

func TestResolverFallsBackWhenNoCandidatesOrSearchFails(t *testing.T) {
	llm := func(_ context.Context, _, _ string) (string, error) { return `{}`, nil }
	step := PlanStep{ID: "a", Kind: "action", Title: "Post", Integration: "Acme"}

	none := resolverWith(func(_ context.Context, _, _ string) ([]Action, error) { return nil, nil }, llm)
	if b, _ := none.Resolve(context.Background(), Plan{Steps: []PlanStep{step}}, step); b.Type != "template" {
		t.Fatalf("no candidates should fall back to template, got %#v", b)
	}
	boom := resolverWith(func(_ context.Context, _, _ string) ([]Action, error) { return nil, errors.New("down") }, llm)
	if b, _ := boom.Resolve(context.Background(), Plan{Steps: []PlanStep{step}}, step); b.Type != "template" {
		t.Fatalf("search error should fall back to template, got %#v", b)
	}
}

func TestResolverMapsNonIntegrationSteps(t *testing.T) {
	r := resolverWith(
		func(_ context.Context, _, _ string) ([]Action, error) { return slackCandidates(), nil },
		func(_ context.Context, _, _ string) (string, error) { return "{}", nil },
	)
	ctx := context.Background()
	plan := Plan{}

	if b, _ := r.Resolve(ctx, plan, PlanStep{ID: "t", Kind: "trigger"}); !b.Skip {
		t.Fatal("trigger should be skipped")
	}
	if b, _ := r.Resolve(ctx, plan, PlanStep{ID: "s", Kind: "ai", Title: "Score the fit"}); b.Type != "nex_ask" || b.QueryTemplate == "" {
		t.Fatalf("ai step should become nex_ask, got %#v", b)
	}
}
