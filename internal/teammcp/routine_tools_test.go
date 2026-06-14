package teammcp

// routine_tools_test.go — live-layer regression tests for team_routine
// (Wave D / D1). Every test runs the real MCP handler against a real
// broker over HTTP — the exact path a live agent turn takes.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func startRoutineBroker(t *testing.T) *brokerHandle {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(b.Stop)
	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	return &brokerHandle{t: t, addr: b.Addr(), token: b.Token()}
}

type brokerHandle struct {
	t     *testing.T
	addr  string
	token string
}

type routineWireJob struct {
	Slug          string `json:"slug"`
	Label         string `json:"label"`
	TargetType    string `json:"target_type"`
	TargetID      string `json:"target_id"`
	ScheduleExpr  string `json:"schedule_expr"`
	Payload       string `json:"payload"`
	Enabled       bool   `json:"enabled"`
	SystemManaged bool   `json:"system_managed"`
}

func (h *brokerHandle) schedulerJobs() []routineWireJob {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+h.addr+"/scheduler", nil)
	if err != nil {
		h.t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("get scheduler: %v", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		h.t.Fatalf("read: %v", err)
	}
	var parsed struct {
		Jobs []routineWireJob `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		h.t.Fatalf("decode: %v (%s)", err, raw)
	}
	return parsed.Jobs
}

// TestHandleTeamRoutine_RegistersPersistentSchedulerJob pins the live MCP
// path: team_routine → POST /scheduler/routines → job visible on GET
// /scheduler (the Scheduled Tasks app's data source) as a user routine.
func TestHandleTeamRoutine_RegistersPersistentSchedulerJob(t *testing.T) {
	h := startRoutineBroker(t)

	res, _, err := handleTeamRoutine(context.Background(), nil, TeamRoutineArgs{
		Purpose:  "Weekly Monday 9am renewal risk summary to #general",
		Schedule: "0 9 * * 1",
		Channel:  "general",
		Owner:    "eng",
		Prompt:   "Pull renewals within 60 days and post a risk summary.",
		MySlug:   "ceo",
	})
	if err != nil {
		t.Fatalf("handleTeamRoutine: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}

	var routine *routineWireJob
	for _, j := range h.schedulerJobs() {
		if j.TargetType == "agent" && !j.SystemManaged {
			cp := j
			routine = &cp
		}
	}
	if routine == nil {
		t.Fatal("registered routine missing from GET /scheduler")
	}
	if routine.TargetID != "eng" || routine.ScheduleExpr != "0 9 * * 1" || !routine.Enabled {
		t.Fatalf("unexpected routine shape: %+v", routine)
	}
}

// TestHandleTeamRoutine_ConfirmationIsTruthful pins the registration
// confirmation contract (D1 item 4): the result must state persistence
// and Scheduled Tasks visibility, and must NOT carry the v3 session-death
// caveat ("7 days", "re-register", session-scoped).
func TestHandleTeamRoutine_ConfirmationIsTruthful(t *testing.T) {
	startRoutineBroker(t)

	res, _, err := handleTeamRoutine(context.Background(), nil, TeamRoutineArgs{
		Purpose:  "Weekly Monday 9am renewal risk summary to #general",
		Schedule: "0 9 * * 1",
		Channel:  "general",
		MySlug:   "ceo",
	})
	if err != nil {
		t.Fatalf("handleTeamRoutine: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}
	text := toolErrorText(res)
	for _, want := range []string{"Scheduled Tasks", "Survives restarts", "no re-registration is ever needed"} {
		if !strings.Contains(text, want) {
			t.Errorf("confirmation missing %q:\n%s", want, text)
		}
	}
	lower := strings.ToLower(text)
	for _, forbidden := range []string{"7 days", "expire", "session-scoped", "dies with the session"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("confirmation carries a session-death caveat %q:\n%s", forbidden, text)
		}
	}
}

// TestHandleTeamRoutine_SecondAgentDedupes pins the v3 two-jobs-for-one-ask
// fix at the tool layer: a second agent registering the same purpose +
// schedule updates the first agent's routine instead of minting another.
func TestHandleTeamRoutine_SecondAgentDedupes(t *testing.T) {
	h := startRoutineBroker(t)

	if res, _, err := handleTeamRoutine(context.Background(), nil, TeamRoutineArgs{
		Purpose:  "Weekly Monday 9am renewal risk summary to #general",
		Schedule: "0 9 * * 1",
		Channel:  "general",
		Owner:    "analyst",
		MySlug:   "ceo",
	}); err != nil || res.IsError {
		t.Fatalf("first registration failed: err=%v res=%s", err, toolErrorText(res))
	}
	res, _, err := handleTeamRoutine(context.Background(), nil, TeamRoutineArgs{
		Purpose:  "Monday 9am weekly renewal-risk summary to #general",
		Schedule: "0 9 * * 1",
		Channel:  "general",
		Owner:    "analyst",
		MySlug:   "outreach",
	})
	if err != nil || res.IsError {
		t.Fatalf("second registration failed: err=%v res=%s", err, toolErrorText(res))
	}
	if text := toolErrorText(res); !strings.Contains(text, `"updated": true`) {
		t.Fatalf("second registration must report updated=true:\n%s", text)
	}

	count := 0
	for _, j := range h.schedulerJobs() {
		if j.TargetType == "agent" && !j.SystemManaged {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 standing automation after both agents registered, got %d", count)
	}
}

// TestTeamRoutine_RegisteredInEveryAgentMode pins the wiring: the
// standing-automation tool must be reachable from office channels, DMs,
// and 1:1 sessions — the v3 duplicate crons were minted from a task
// channel AND a specialist lane, so no mode may fall back to the
// provider's session cron for lack of the tool.
func TestTeamRoutine_RegisteredInEveryAgentMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	modes := []struct {
		name     string
		channel  string
		oneOnOne bool
	}{
		{"office", "general", false},
		{"dm", "dm-eng", false},
		{"one-on-one", "general", true},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			names := listRegisteredTools(t, mode.channel, mode.oneOnOne)
			found := false
			for _, n := range names {
				if n == "team_routine" {
					found = true
				}
			}
			if !found {
				t.Fatalf("team_routine not registered in %s mode; tools=%v", mode.name, names)
			}
		})
	}
}

// TestHandleTeamRoutine_RejectsBadSchedule pins local validation: a
// malformed schedule errors before crossing the wire.
func TestHandleTeamRoutine_RejectsBadSchedule(t *testing.T) {
	startRoutineBroker(t)

	res, _, err := handleTeamRoutine(context.Background(), nil, TeamRoutineArgs{
		Purpose:  "Weekly summary",
		Schedule: "whenever feels right",
		MySlug:   "ceo",
	})
	if err != nil {
		t.Fatalf("handleTeamRoutine: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a tool error for a malformed schedule")
	}
	if text := toolErrorText(res); !strings.Contains(text, "invalid schedule") {
		t.Fatalf("unexpected error text: %s", text)
	}
}
