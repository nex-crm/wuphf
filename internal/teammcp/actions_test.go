package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/team"
)

type stubActionProvider struct{}

func (stubActionProvider) Name() string                    { return "one" }
func (stubActionProvider) Configured() bool                { return true }
func (stubActionProvider) Supports(action.Capability) bool { return true }
func (stubActionProvider) Guide(context.Context, string) (action.GuideResult, error) {
	return action.GuideResult{}, nil
}
func (stubActionProvider) ListConnections(context.Context, action.ListConnectionsOptions) (action.ConnectionsResult, error) {
	return action.ConnectionsResult{}, nil
}
func (stubActionProvider) SearchActions(context.Context, string, string, string) (action.ActionSearchResult, error) {
	return action.ActionSearchResult{}, nil
}
func (stubActionProvider) ActionKnowledge(context.Context, string, string) (action.KnowledgeResult, error) {
	return action.KnowledgeResult{}, nil
}
func (stubActionProvider) ExecuteAction(context.Context, action.ExecuteRequest) (action.ExecuteResult, error) {
	return action.ExecuteResult{
		DryRun: true,
		Request: action.ExecuteEnvelope{
			Method: "POST",
			URL:    "https://api.withone.ai/send",
		},
	}, nil
}
func (stubActionProvider) CreateWorkflow(context.Context, action.WorkflowCreateRequest) (action.WorkflowCreateResult, error) {
	return action.WorkflowCreateResult{Created: true, Key: "daily-digest"}, nil
}
func (stubActionProvider) ExecuteWorkflow(context.Context, action.WorkflowExecuteRequest) (action.WorkflowExecuteResult, error) {
	return action.WorkflowExecuteResult{}, nil
}
func (stubActionProvider) ListWorkflowRuns(context.Context, string) (action.WorkflowRunsResult, error) {
	return action.WorkflowRunsResult{}, nil
}
func (stubActionProvider) ListRelays(context.Context, action.ListRelaysOptions) (action.RelayListResult, error) {
	return action.RelayListResult{}, nil
}
func (stubActionProvider) RelayEventTypes(context.Context, string) (action.RelayEventTypesResult, error) {
	return action.RelayEventTypesResult{}, nil
}
func (stubActionProvider) CreateRelay(context.Context, action.RelayCreateRequest) (action.RelayResult, error) {
	return action.RelayResult{}, nil
}
func (stubActionProvider) ActivateRelay(context.Context, action.RelayActivateRequest) (action.RelayResult, error) {
	return action.RelayResult{}, nil
}
func (stubActionProvider) ListRelayEvents(context.Context, action.RelayEventsOptions) (action.RelayEventsResult, error) {
	return action.RelayEventsResult{}, nil
}
func (stubActionProvider) GetRelayEvent(context.Context, string) (action.RelayEventDetail, error) {
	return action.RelayEventDetail{}, nil
}

func TestHandleTeamActionExecuteLogsBrokerAction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	prev := externalActionProvider
	externalActionProvider = stubActionProvider{}
	defer func() { externalActionProvider = prev }()

	if _, _, err := handleTeamActionExecute(context.Background(), nil, TeamActionExecuteArgs{
		Platform:      "gmail",
		ActionID:      "send-email",
		ConnectionKey: "live::gmail::default::abc123",
		DryRun:        true,
		MySlug:        "ceo",
		Channel:       "general",
	}); err != nil {
		t.Fatalf("execute action: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/actions", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get actions: %v", err)
	}
	defer resp.Body.Close()
	if got := len(b.Actions()); got != 1 {
		t.Fatalf("expected 1 action, got %d", got)
	}
	if action := b.Actions()[0]; action.Kind != "external_action_planned" || action.Source != "one" {
		t.Fatalf("unexpected action %+v", action)
	}
}

func TestHandleTeamActionWorkflowCreateMirrorsSkill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	prev := externalActionProvider
	externalActionProvider = stubActionProvider{}
	defer func() { externalActionProvider = prev }()

	_, _, err := handleTeamActionWorkflowCreate(context.Background(), nil, TeamActionWorkflowCreateArgs{
		Key:              "daily-digest",
		DefinitionJSON:   `{"steps":[]}`,
		MySlug:           "ceo",
		Channel:          "general",
		SkillName:        "daily-digest",
		SkillTitle:       "Daily Digest",
		SkillDescription: "Send the daily digest.",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/skills?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get skills: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Skills []struct {
			Name             string `json:"name"`
			WorkflowProvider string `json:"workflow_provider"`
			WorkflowKey      string `json:"workflow_key"`
		} `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	if len(result.Skills) != 1 {
		t.Fatalf("expected mirrored skill, got %+v", result.Skills)
	}
	if result.Skills[0].WorkflowProvider != "one" || result.Skills[0].WorkflowKey != "daily-digest" {
		t.Fatalf("unexpected skill metadata %+v", result.Skills[0])
	}
}

func TestHandleTeamActionWorkflowScheduleCreatesSchedulerJob(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	prev := externalActionProvider
	externalActionProvider = stubActionProvider{}
	defer func() { externalActionProvider = prev }()

	_, _, err := handleTeamActionWorkflowSchedule(context.Background(), nil, TeamActionWorkflowScheduleArgs{
		Key:      "daily-digest",
		Schedule: "daily",
		MySlug:   "ceo",
		Channel:  "general",
	})
	if err != nil {
		t.Fatalf("schedule workflow: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/scheduler", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get scheduler: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Jobs []struct {
			Kind         string `json:"kind"`
			TargetType   string `json:"target_type"`
			TargetID     string `json:"target_id"`
			Provider     string `json:"provider"`
			ScheduleExpr string `json:"schedule_expr"`
		} `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode jobs: %v", err)
	}
	if len(result.Jobs) != 1 {
		t.Fatalf("expected one scheduled job, got %+v", result.Jobs)
	}
	job := result.Jobs[0]
	if job.Kind != "one_workflow" || job.TargetType != "workflow" || job.TargetID != "daily-digest" || job.Provider != "one" || job.ScheduleExpr != "daily" {
		t.Fatalf("unexpected scheduler job %+v", job)
	}
}
