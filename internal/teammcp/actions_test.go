package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	return action.WorkflowExecuteResult{RunID: "run-1", Status: "completed"}, nil
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

// TestActionIsReadOnly pins the behaviour of the approval-gate allow-list.
// The original implementation used strings.Contains and would classify any
// action_id that contained a substring from the read-verb list as read-only,
// which made mutating actions like "budget_update" or "findone_and_update"
// bypass the gate. The fixed implementation tokenizes on common separators
// and requires a whole-token match AND no mutating verb elsewhere in the ID.
//
// Every "mutation bypass" case in this table corresponds to a real Composio-
// shaped action name or a near-miss of one. A regression here silently
// reopens finding #7 of the CSO audit.
func TestActionIsReadOnly(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		// Read actions — must bypass the approval gate.
		{"GMAIL_FETCH_MAILS", true},
		{"GMAIL_SEARCH_EMAILS", true},
		{"HUBSPOT_GET_CONTACT", true},
		{"SLACKBOT_LIST_CHANNELS", true},
		{"GOOGLECALENDAR_EVENTS_LIST", true},
		{"describe_schema", true},
		{"browse.docs", true},
		{"list-contacts", true},

		// Mutating actions — must NOT be classified read-only.
		{"GMAIL_SEND_EMAIL", false},
		{"GMAIL_CREATE_DRAFT", false},
		{"HUBSPOT_UPDATE_CONTACT", false},
		{"SLACKBOT_SEND_MESSAGE", false},
		{"GOOGLECALENDAR_CREATE_EVENT", false},
		{"delete-user", false},
		{"post.message", false},
		{"set_status", false},

		// Substring bypasses the old implementation was vulnerable to.
		{"budget_update", false},         // contains "get" as substring
		{"findone_and_update", false},    // contains "find" + mutating "update"
		{"review_delete", false},         // contains "view" + mutating "delete"
		{"account_create", false},        // contains "count" + mutating "create"
		{"send_status_update", false},    // contains "status" + mutating "send"/"update"
		{"POST_SUMMARY", false},          // contains "summary" + mutating "post"
		{"archive_report", false},        // "archive" is mutating
		{"GMAIL_LIST_AND_DELETE", false}, // read AND mutate → must gate

		// Empty / unknown / ambiguous — default is GATED (return false).
		{"", false},
		{"mystery_action", false},
		{"do_thing", false},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			if got := actionIsReadOnly(tc.id); got != tc.want {
				t.Fatalf("actionIsReadOnly(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

// TestRequireTeamActionApprovalBypasses exercises the three bypass paths
// that must never require a human click: DryRun=true, WUPHF_UNSAFE=1, and
// read-only action_ids. If any of these regress, agents either can't take
// safe actions (read operations pile up approval requests) or the gate
// fails open entirely.
func TestRequireTeamActionApprovalBypasses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Ensure the default bypass state — no WUPHF_UNSAFE unless a subtest sets it.
	t.Setenv("WUPHF_UNSAFE", "")

	t.Run("DryRun bypasses", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := requireTeamActionApproval(ctx, "ceo", "general", TeamActionExecuteArgs{
			Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL", DryRun: true,
		})
		if err != nil {
			t.Fatalf("DryRun must bypass approval, got err=%v", err)
		}
	})

	t.Run("WUPHF_UNSAFE bypasses", func(t *testing.T) {
		t.Setenv("WUPHF_UNSAFE", "1")
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := requireTeamActionApproval(ctx, "ceo", "general", TeamActionExecuteArgs{
			Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL", DryRun: false,
		})
		if err != nil {
			t.Fatalf("WUPHF_UNSAFE=1 must bypass approval, got err=%v", err)
		}
	})

	t.Run("read-only action_id bypasses", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := requireTeamActionApproval(ctx, "ceo", "general", TeamActionExecuteArgs{
			Platform: "gmail", ActionID: "GMAIL_FETCH_MAILS", DryRun: false,
		})
		if err != nil {
			t.Fatalf("read-only action_id must bypass approval, got err=%v", err)
		}
	})
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

func TestHandleTeamActionWorkflowScheduleRunNowExecutesImmediately(t *testing.T) {
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

	res, _, err := handleTeamActionWorkflowSchedule(context.Background(), nil, TeamActionWorkflowScheduleArgs{
		Key:      "daily-digest",
		Schedule: "daily",
		RunNow:   true,
		MySlug:   "ceo",
		Channel:  "general",
	})
	if err != nil {
		t.Fatalf("schedule workflow with run_now: %v", err)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text response, got %+v", res.Content)
	}
	if got := text.Text; !strings.Contains(got, "\"run_now\"") || !strings.Contains(got, "\"ok\": true") {
		t.Fatalf("expected run_now block in response, got %s", got)
	}
	var sawScheduled, sawExecuted bool
	for _, entry := range b.Actions() {
		if entry.Kind == "external_workflow_scheduled" {
			sawScheduled = true
		}
		if entry.Kind == "external_workflow_executed" {
			sawExecuted = true
		}
	}
	if !sawScheduled || !sawExecuted {
		t.Fatalf("expected scheduled and executed actions, got %+v", b.Actions())
	}
}

func TestSelectedActionProviderIncludesCapabilityGuidance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_NO_NEX", "1")

	prev := externalActionProvider
	externalActionProvider = nil
	defer func() { externalActionProvider = prev }()

	_, err := selectedActionProvider(action.CapabilityActionExecute)
	if err == nil {
		t.Fatal("expected provider selection to fail when Nex is disabled")
	}
	if !strings.Contains(err.Error(), "Restart without --no-nex") {
		t.Fatalf("expected readiness next step in %q", err)
	}
}
