package team

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFakeOperationOne(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "one")
	script := `#!/bin/sh
if [ "$1" = "--agent" ]; then
  shift
fi

cmd3="$1 $2 $3"
if [ "$cmd3" = "flow create sponsor-outreach-dry-run" ]; then
  echo '{"created":true,"key":"sponsor-outreach-dry-run","path":"/tmp/.one/flows/sponsor-outreach-dry-run.flow.json"}'
elif [ "$cmd3" = "flow execute sponsor-outreach-dry-run" ]; then
  echo '{"event":"step:start","stepId":"compose-email"}'
  echo '{"event":"workflow:result","runId":"run-123","logFile":"/tmp/run.log","status":"success","steps":{"gmail-preview":{"status":"success"}}}'
else
  echo "unexpected args: $*" >&2
  exit 1
fi
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeRateLimitedOperationOne(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "one")
	script := `#!/bin/sh
if [ "$1" = "--agent" ]; then
  shift
fi

cmd3="$1 $2 $3"
if [ "$cmd3" = "flow create consulting-live-gmail-read" ]; then
  echo '{"created":true,"key":"consulting-live-gmail-read","path":"/tmp/.one/flows/consulting-live-gmail-read.flow.json"}'
elif [ "$cmd3" = "flow execute consulting-live-gmail-read" ]; then
  echo '{"event":"flow:start","runId":"run-live-1"}'
  echo '{"event":"step:error","stepId":"fetchEmails","error":"429 User-rate limit exceeded. Retry after 2026-04-14T22:55:02.178Z"}' >&2
  exit 1
else
  echo "unexpected args: $*" >&2
  exit 1
fi
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDecodeStudioGeneratedPackageHandlesFencedJSON(t *testing.T) {
	raw := "```json\n{\"topic_packet\":{\"title\":\"X\"},\"script_brief\":{\"hook\":\"Y\"},\"publish_package\":{\"description\":\"Z\"}}\n```"
	pkg, err := decodeStudioGeneratedPackage(raw, []string{"topic_packet", "script_brief", "publish_package"})
	if err != nil {
		t.Fatalf("decode package: %v", err)
	}
	if got := pkg["topic_packet"]["title"]; got != "X" {
		t.Fatalf("unexpected topic packet title: %#v", got)
	}
	if got := pkg["script_brief"]["hook"]; got != "Y" {
		t.Fatalf("unexpected script brief hook: %#v", got)
	}
	if got := pkg["publish_package"]["description"]; got != "Z" {
		t.Fatalf("unexpected publish package description: %#v", got)
	}
}

func TestHandleStudioGeneratePackagePersistsAction(t *testing.T) {
	restore := studioPackageGenerator
	studioPackageGenerator = func(systemPrompt, prompt, cwd string) (string, error) {
		return `{"topic_packet":{"title":"AI automation channel"},"script_brief":{"hook":"Start with the costly mistake."},"publish_package":{"description":"Ship with affiliate CTA.","title_options":["Option A","Option B"]}}`, nil
	}
	defer func() { studioPackageGenerator = restore }()

	tmpDir := t.TempDir()
	prevStatePath := brokerStatePath
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = prevStatePath }()

	b := NewBroker()
	body := map[string]any{
		"channel": "general",
		"actor":   "eng",
		"workspace": map[string]any{
			"name": "Faceless Foundry",
		},
		"run": map[string]any{
			"id":    "run-1",
			"title": "Automate weekly founder ops",
		},
		"artifacts": []map[string]any{
			{"id": "topic_packet", "name": "Topic packet"},
			{"id": "script_brief", "name": "Script brief"},
			{"id": "publish_package", "name": "Publish package"},
		},
		"offers": []any{
			map[string]any{"name": "AI Toolkit"},
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/studio/generate-package", bytes.NewReader(raw))
	rec := httptest.NewRecorder()

	b.handleStudioGeneratePackage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK             bool                      `json:"ok"`
		Package        studioGeneratedPackage    `json:"package"`
		Artifacts      []studioGeneratedArtifact `json:"artifacts"`
		StubExecutions []studioStubExecution     `json:"stub_executions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected ok response")
	}
	if got := resp.Package["topic_packet"]["title"]; got != "AI automation channel" {
		t.Fatalf("unexpected topic packet title: %#v", got)
	}
	if len(resp.StubExecutions) != 3 {
		t.Fatalf("expected 3 stub executions, got %d", len(resp.StubExecutions))
	}
	if len(resp.Artifacts) != 3 || resp.Artifacts[0].Kind != "topic_packet" || resp.Artifacts[2].Kind != "publish_package" {
		t.Fatalf("expected generic generated artifacts, got %+v", resp.Artifacts)
	}
	if got := resp.StubExecutions[0].Provider; got != "one" {
		t.Fatalf("unexpected stub execution provider: %#v", got)
	}
	actions := b.Actions()
	if len(actions) != 4 {
		t.Fatalf("expected package action plus 3 follow-up stub actions, got %d", len(actions))
	}
	if actions[0].Kind != "studio_package_generated" {
		t.Fatalf("unexpected first action kind: %#v", actions[0])
	}
	last := actions[len(actions)-1]
	if last.Kind != "studio_followup_stub_executed" {
		t.Fatalf("unexpected last action kind: %#v", last)
	}
}

func TestHandleMemoryRoundTripScopedStudioRecords(t *testing.T) {
	tmpDir := t.TempDir()
	prevStatePath := brokerStatePath
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = prevStatePath }()

	b := NewBroker()

	writeRecord := func(namespace, key string, value any) {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"namespace": namespace,
			"key":       key,
			"value":     mustJSON(t, value),
		})
		if err != nil {
			t.Fatalf("marshal write body: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/memory", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		b.handleMemory(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected write 200 for %s/%s, got %d: %s", namespace, key, rec.Code, rec.Body.String())
		}
	}

	writeRecord("youtube_factory/workspaces/faceless-foundry", "config", map[string]any{
		"workspace": map[string]any{"name": "Faceless Foundry"},
	})
	writeRecord("youtube_factory/workspaces/faceless-foundry", "runs", []map[string]any{
		{"id": "run-1", "title": "Automate founder ops"},
	})
	writeRecord("youtube_factory/workspaces/faceless-foundry", "offers", []map[string]any{
		{"id": "offer-1", "name": "AI Toolkit"},
	})
	writeRecord("youtube_factory/workspaces/faceless-foundry", "artifacts", map[string]any{
		"publishPackages": []map[string]any{{"id": "pkg-1"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/memory?channel=general", nil)
	rec := httptest.NewRecorder()
	b.handleMemory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected read 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Memory map[string]map[string]string `json:"memory"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode memory response: %v", err)
	}

	ns := resp.Memory["youtube_factory/workspaces/faceless-foundry"]
	if ns == nil {
		t.Fatal("expected scoped workspace namespace in memory response")
	}
	if ns["config"] == "" || ns["runs"] == "" || ns["offers"] == "" || ns["artifacts"] == "" {
		t.Fatalf("expected scoped studio records, got %#v", ns)
	}
}

func TestHandleStudioRunWorkflowExecutesOneDraftAndUpdatesSkill(t *testing.T) {
	t.Setenv("WUPHF_ACTION_PROVIDER", "one")
	t.Setenv("WUPHF_ONE_BIN", writeFakeOperationOne(t))

	tmpDir := t.TempDir()
	prevStatePath := brokerStatePath
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = prevStatePath }()

	b := NewBroker()
	b.skills = append(b.skills, teamSkill{
		ID:                 "skill-sponsor-outreach-dry-run",
		Name:               "sponsor-outreach-dry-run",
		Title:              "Sponsor Outreach Dry-Run",
		Status:             "active",
		Channel:            "youtube-factory",
		CreatedBy:          "you",
		WorkflowProvider:   "one",
		WorkflowKey:        "sponsor-outreach-dry-run",
		WorkflowDefinition: `{"version":1,"provider":"one","key":"sponsor-outreach-dry-run","steps":[{"id":"compose-email","kind":"transform"},{"id":"gmail-preview","kind":"action","platform":"gmail","action":"users.messages.send","dry_run":true}]}`,
	})

	body := map[string]any{
		"channel":    "youtube-factory",
		"actor":      "you",
		"skill_name": "sponsor-outreach-dry-run",
		"inputs": map[string]any{
			"brand":         "Example Sponsor",
			"contact_email": "partner@example.com",
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/studio/run-workflow", bytes.NewReader(raw))
	rec := httptest.NewRecorder()

	b.handleStudioRunWorkflow(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK           bool     `json:"ok"`
		WorkflowKey  string   `json:"workflow_key"`
		Provider     string   `json:"provider"`
		Mode         string   `json:"mode"`
		Status       string   `json:"status"`
		Integrations []string `json:"integrations"`
		Execution    struct {
			RunID  string `json:"run_id"`
			Status string `json:"status"`
		} `json:"execution"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.OK || resp.WorkflowKey != "sponsor-outreach-dry-run" || resp.Provider != "one" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Mode != "dry-run" || resp.Execution.RunID != "run-123" || resp.Execution.Status != "success" {
		t.Fatalf("unexpected execution payload: %+v", resp)
	}
	if len(resp.Integrations) != 1 || resp.Integrations[0] != "gmail" {
		t.Fatalf("expected gmail integration, got %+v", resp.Integrations)
	}

	actions := b.Actions()
	if len(actions) == 0 {
		t.Fatal("expected action log entry")
	}
	lastAction := actions[len(actions)-1]
	if lastAction.Kind != "external_workflow_executed" || lastAction.Source != "one" {
		t.Fatalf("unexpected action %+v", lastAction)
	}

	if len(b.skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(b.skills))
	}
	if b.skills[0].UsageCount != 1 || b.skills[0].LastExecutionStatus != "success" {
		t.Fatalf("expected skill usage/status updated, got %+v", b.skills[0])
	}
}

func TestHandleStudioRunWorkflowReturnsRateLimitMetadata(t *testing.T) {
	t.Setenv("WUPHF_ACTION_PROVIDER", "one")
	t.Setenv("WUPHF_ONE_BIN", writeRateLimitedOperationOne(t))

	tmpDir := t.TempDir()
	prevStatePath := brokerStatePath
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = prevStatePath }()

	b := NewBroker()
	b.skills = append(b.skills, teamSkill{
		ID:               "skill-consulting-live-gmail-read",
		Name:             "consulting-live-gmail-read",
		Title:            "Consulting Live Gmail Read",
		Status:           "active",
		Channel:          "systems",
		CreatedBy:        "operator",
		WorkflowProvider: "one",
		WorkflowKey:      "consulting-live-gmail-read",
		WorkflowDefinition: `{
			"name":"Consulting Live Gmail Read",
			"version":"1",
			"inputs":{"connectionKey":{"type":"string"}},
			"steps":[{"id":"fetchEmails","type":"action","action":{"platform":"gmail","actionId":"gmail/get-emails","connectionKey":"$.input.connectionKey","data":{"connectionKey":"$.input.connectionKey"}}}]
		}`,
	})

	body := map[string]any{
		"channel":    "systems",
		"actor":      "operator",
		"skill_name": "consulting-live-gmail-read",
		"inputs": map[string]any{
			"connectionKey": "live::gmail::default::abc123",
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/studio/run-workflow", bytes.NewReader(raw))
	rec := httptest.NewRecorder()

	b.handleStudioRunWorkflow(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}

	var resp struct {
		OK         bool   `json:"ok"`
		Status     string `json:"status"`
		RetryAfter string `json:"retry_after"`
		Error      string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OK || resp.Status != "rate_limited" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	retryAt, err := time.Parse(time.RFC3339Nano, resp.RetryAfter)
	if err != nil {
		t.Fatalf("retry_after should be RFC3339, got %q (%v)", resp.RetryAfter, err)
	}
	if retryAt.IsZero() {
		t.Fatalf("retry_after should parse to a real timestamp, got %q", resp.RetryAfter)
	}
	if !strings.Contains(strings.ToLower(resp.Error), "retry after ") {
		t.Fatalf("expected retry hint in error, got %q", resp.Error)
	}

	actions := b.Actions()
	if len(actions) == 0 {
		t.Fatal("expected action log entry")
	}
	lastAction := actions[len(actions)-1]
	if lastAction.Kind != "external_workflow_rate_limited" || lastAction.Source != "one" {
		t.Fatalf("unexpected action %+v", lastAction)
	}
	if len(b.skills) != 1 || b.skills[0].LastExecutionStatus != "rate_limited" {
		t.Fatalf("expected skill status updated, got %+v", b.skills)
	}
}

func TestBuildOperationBootstrapPackageFromRepoIncludesStarterPlan(t *testing.T) {
	pkg, err := buildOperationBootstrapPackageFromRepo(context.Background(), operationCompanyProfile{
		BlueprintID: "youtube-factory",
		Name:        "WUPHF",
	})
	if err != nil {
		t.Fatalf("buildOperationBootstrapPackageFromRepo: %v", err)
	}
	if pkg.Blueprint.ID != "youtube-factory" {
		t.Fatalf("expected template blueprint loaded, got %+v", pkg.Blueprint)
	}
	if pkg.Starter.ID == "" || pkg.Starter.Name == "" {
		t.Fatalf("expected starter metadata, got %+v", pkg.Starter)
	}
	if len(pkg.Starter.Agents) < 2 || len(pkg.Starter.Channels) == 0 || len(pkg.Starter.Tasks) == 0 {
		t.Fatalf("expected starter plan to include agents, channels, and tasks, got %+v", pkg.Starter)
	}
	if len(pkg.WorkflowDrafts) != len(pkg.Blueprint.Workflows) {
		t.Fatalf("expected workflow drafts to come from blueprint, got %d drafts for %d workflows", len(pkg.WorkflowDrafts), len(pkg.Blueprint.Workflows))
	}
	if len(pkg.Connections) != len(pkg.Blueprint.Connections) {
		t.Fatalf("expected connection cards to come from blueprint, got %d cards for %d connections", len(pkg.Connections), len(pkg.Blueprint.Connections))
	}
	if len(pkg.Automation) != len(pkg.Blueprint.Automation) {
		t.Fatalf("expected automation cards to come from blueprint, got %d cards for %d modules", len(pkg.Automation), len(pkg.Blueprint.Automation))
	}
	if pkg.Starter.Defaults.Company != "WUPHF" {
		t.Fatalf("expected starter defaults to use pack brand, got %+v", pkg.Starter.Defaults)
	}
	if !strings.Contains(strings.ToLower(pkg.Starter.GeneralDesc), "wuphf") {
		t.Fatalf("expected general description to mention pack brand, got %q", pkg.Starter.GeneralDesc)
	}
	if !strings.Contains(pkg.Starter.KickoffMessage, "WUPHF") {
		t.Fatalf("expected kickoff message to render pack brand, got %q", pkg.Starter.KickoffMessage)
	}
	if pkg.BootstrapConfig.ChannelName != "WUPHF" || pkg.BootstrapConfig.ChannelSlug != "wuphf" {
		t.Fatalf("expected template bootstrap config, got %+v", pkg.BootstrapConfig)
	}
	if len(pkg.BootstrapConfig.ContentSeries) != 4 || pkg.BootstrapConfig.ContentSeries[0] != "Live Steering Demos" {
		t.Fatalf("expected content series from template, got %+v", pkg.BootstrapConfig.ContentSeries)
	}
	if got := pkg.BootstrapConfig.LeadMagnet.Name; got != "CEP Benchmark + Migration Checklist" {
		t.Fatalf("expected lead magnet from template, got %q", got)
	}
	if len(pkg.BootstrapConfig.KPITracking) != 4 {
		t.Fatalf("expected KPI tracking from template, got %+v", pkg.BootstrapConfig.KPITracking)
	}
	if len(pkg.ValueCapturePlan) != 8 || pkg.ValueCapturePlan[0].Title != "free benchmark" {
		t.Fatalf("expected value capture plan from template, got %+v", pkg.ValueCapturePlan)
	}
	if len(pkg.MonetizationLadder) != len(pkg.ValueCapturePlan) || pkg.MonetizationLadder[0].Title != pkg.ValueCapturePlan[0].Title {
		t.Fatalf("expected monetization ladder legacy alias to mirror value capture plan, got plan=%+v ladder=%+v", pkg.ValueCapturePlan, pkg.MonetizationLadder)
	}
	if len(pkg.WorkstreamSeed) != 5 || pkg.WorkstreamSeed[0].ID != "vid_01" || pkg.WorkstreamSeed[0].Score != 95 {
		t.Fatalf("expected workstream seed from template, got %+v", pkg.WorkstreamSeed)
	}
	if len(pkg.QueueSeed) != len(pkg.WorkstreamSeed) || pkg.QueueSeed[0].ID != pkg.WorkstreamSeed[0].ID {
		t.Fatalf("expected queue seed legacy alias to mirror workstream seed, got workstream=%+v queue=%+v", pkg.WorkstreamSeed, pkg.QueueSeed)
	}
	if pkg.WorkstreamSeed[0].Monetization != "ai back office starter pack + automation and docs" {
		t.Fatalf("expected workstream monetization from template, got %+v", pkg.WorkstreamSeed[0])
	}
	if !strings.Contains(filepath.ToSlash(pkg.SourcePath), "templates/operations/youtube-factory/blueprint.yaml") {
		t.Fatalf("expected template blueprint source path, got %q", pkg.SourcePath)
	}
}

func TestBuildOperationBootstrapPackageFromRepoResolvesLegacyPackIDToBlueprint(t *testing.T) {
	pkg, err := buildOperationBootstrapPackageFromRepo(context.Background(), operationCompanyProfile{
		BlueprintID: "youtube_factory_wuphf_operator_channel_pack",
		Name:        "WUPHF",
	})
	if err != nil {
		t.Fatalf("buildOperationBootstrapPackageFromRepo legacy selector: %v", err)
	}
	if pkg.Blueprint.ID != "youtube-factory" {
		t.Fatalf("expected youtube-factory blueprint for legacy selector, got %+v", pkg.Blueprint)
	}
}

func TestBuildOperationBootstrapPackageSynthesizesWhenNoPackSeedExists(t *testing.T) {
	tmpRepo := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpRepo
	cmd.Env = gitCleanEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init temp repo: %v: %s", err, strings.TrimSpace(string(out)))
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpRepo); err != nil {
		t.Fatalf("chdir temp repo: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	pkg, err := buildOperationBootstrapPackageFromRepo(context.Background(), operationCompanyProfile{
		Name:        "Blank Slate Ops",
		Description: "Stand up a new operation from a blank directive.",
		Goals:       "Prove the office can synthesize a blueprint without repo-authored seed docs.",
		Size:        "3-5",
		Priority:    "Bootstrap the first working lane.",
	})
	if err != nil {
		t.Fatalf("buildOperationBootstrapPackageFromRepo synthesized path: %v", err)
	}
	if pkg.Blueprint.ID != "blank-slate-ops" {
		t.Fatalf("expected synthesized blueprint id from profile, got %+v", pkg.Blueprint)
	}
	if !strings.Contains(pkg.Blueprint.Name, "Blank Slate Ops") {
		t.Fatalf("expected synthesized blueprint name to reflect profile, got %+v", pkg.Blueprint)
	}
	if pkg.Blueprint.Kind == "" || pkg.Blueprint.ID == "youtube-factory" {
		t.Fatalf("expected a non-template synthesized blueprint kind, got %+v", pkg.Blueprint)
	}
	if len(pkg.Blueprint.Stages) < 4 || len(pkg.Blueprint.Artifacts) < 4 || len(pkg.Blueprint.Workflows) < 1 {
		t.Fatalf("expected synthesized blueprint structure, got %+v", pkg.Blueprint)
	}
	if len(pkg.Starter.Agents) < 4 || len(pkg.Starter.Channels) < 3 || len(pkg.Starter.Tasks) < 3 {
		t.Fatalf("expected synthesized starter plan, got %+v", pkg.Starter)
	}
	if len(pkg.Connections) != len(pkg.Blueprint.Connections) {
		t.Fatalf("expected synthesized connection cards to mirror the synthesized blueprint, got cards=%d blueprint=%d", len(pkg.Connections), len(pkg.Blueprint.Connections))
	}
	for _, card := range pkg.Connections {
		if strings.TrimSpace(card.Integration) == "" || strings.TrimSpace(card.Name) == "" {
			t.Fatalf("expected synthesized connection cards to be populated, got %+v", pkg.Connections)
		}
	}
	if len(pkg.Offers) == 0 || pkg.Offers[0].Type != "lead_magnet" {
		t.Fatalf("expected synthesized offers, got %+v", pkg.Offers)
	}
	if len(pkg.ValueCapturePlan) == 0 || len(pkg.WorkstreamSeed) == 0 {
		t.Fatalf("expected synthesized generic operating artifacts, got value_capture=%d workstream=%d", len(pkg.ValueCapturePlan), len(pkg.WorkstreamSeed))
	}
	if len(pkg.WorkflowDrafts) != len(pkg.Blueprint.Workflows) || len(pkg.SmokeTests) != len(pkg.Blueprint.Workflows) {
		t.Fatalf("expected workflow artifacts to come from synthesized blueprint, got drafts=%d smoke=%d workflows=%d", len(pkg.WorkflowDrafts), len(pkg.SmokeTests), len(pkg.Blueprint.Workflows))
	}
	if pkg.SourcePath != "synthesized" {
		t.Fatalf("expected synthesized source path marker, got %q", pkg.SourcePath)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json value: %v", err)
	}
	return string(raw)
}
