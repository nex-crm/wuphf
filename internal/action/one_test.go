package action

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/config"
)

func writeFakeOne(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "one")
	script := `#!/bin/sh
if [ "$1" = "--agent" ]; then
  shift
fi

cmd2="$1 $2"
cmd3="$1 $2 $3"

if [ "$cmd2" = "connection list" ]; then
  echo '{"total":1,"showing":1,"connections":[{"platform":"gmail","state":"operational","key":"live::gmail::default::abc123"}]}'
elif [ "$cmd3" = "actions search gmail" ]; then
  echo '{"actions":[{"actionId":"act-send","title":"Send Email","method":"POST","path":"/gmail/send"}]}'
elif [ "$cmd3" = "actions knowledge gmail" ]; then
  echo '{"knowledge":"Needs to, subject, body","method":"POST"}'
elif [ "$cmd3" = "actions execute gmail" ]; then
  echo '{"dryRun":true,"request":{"method":"POST","url":"https://api.withone.ai/send","headers":{"x-test":"1"},"data":{"to":"a@example.com"}}}'
elif [ "$cmd3" = "flow create welcome-flow" ]; then
  echo '{"created":true,"key":"welcome-flow","path":"/tmp/.one/flows/welcome-flow.flow.json"}'
elif [ "$cmd3" = "flow execute welcome-flow" ]; then
  echo '{"event":"step:start","stepId":"step-1"}'
  echo '{"event":"workflow:result","runId":"run-1","logFile":"/tmp/run.log","status":"success","steps":{"step-1":{"status":"success"}}}'
elif [ "$cmd3" = "relay event-types gmail" ]; then
  echo '{"platform":"gmail","eventTypes":["message.received"]}'
elif [ "$cmd2" = "relay create" ]; then
  echo '{"id":"relay-1","url":"https://relay.example","active":false,"description":"mail relay","eventFilters":["message.received"]}'
elif [ "$cmd3" = "relay activate relay-1" ]; then
  echo '{"id":"relay-1","active":true,"actions":[{"type":"passthrough"}]}'
elif [ "$cmd2" = "relay events" ]; then
  echo '{"total":1,"showing":1,"events":[{"id":"evt-1","platform":"gmail","eventType":"message.received","timestamp":"2026-03-29T10:00:00Z"}]}'
elif [ "$cmd3" = "relay event evt-1" ]; then
  echo '{"id":"evt-1","platform":"gmail","eventType":"message.received","timestamp":"2026-03-29T10:00:00Z","payload":{"from":"a@example.com"}}'
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

func TestOneCLIHappyPath(t *testing.T) {
	oneBin := writeFakeOne(t)
	client := &OneCLI{Bin: oneBin, WorkDir: t.TempDir(), Env: []string{"ONE_SECRET=test-secret"}}
	ctx := context.Background()

	connections, err := client.ListConnections(ctx, ListConnectionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(connections.Connections); got != 1 {
		t.Fatalf("expected 1 connection, got %d", got)
	}

	search, err := client.SearchActions(ctx, "gmail", "send email", "execute")
	if err != nil {
		t.Fatal(err)
	}
	if got := search.Actions[0].ActionID; got != "act-send" {
		t.Fatalf("unexpected action id %q", got)
	}

	knowledge, err := client.ActionKnowledge(ctx, "gmail", "act-send")
	if err != nil {
		t.Fatal(err)
	}
	if knowledge.Method != "POST" {
		t.Fatalf("unexpected method %q", knowledge.Method)
	}

	executed, err := client.ExecuteAction(ctx, ExecuteRequest{
		Platform:      "gmail",
		ActionID:      "act-send",
		ConnectionKey: "live::gmail::default::abc123",
		Data: map[string]any{
			"to": "a@example.com",
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !executed.DryRun || executed.Request.Method != "POST" {
		t.Fatalf("unexpected execute result %+v", executed)
	}

	created, err := client.CreateWorkflow(ctx, WorkflowCreateRequest{
		Key:        "welcome-flow",
		Definition: []byte(`{"key":"welcome-flow","name":"Welcome","version":"1","inputs":{},"steps":[]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Created {
		t.Fatalf("expected created workflow, got %+v", created)
	}

	workflow, err := client.ExecuteWorkflow(ctx, WorkflowExecuteRequest{KeyOrPath: "welcome-flow"})
	if err != nil {
		t.Fatal(err)
	}
	if workflow.RunID != "run-1" || workflow.Status != "success" {
		t.Fatalf("unexpected workflow result %+v", workflow)
	}

	eventTypes, err := client.RelayEventTypes(ctx, "gmail")
	if err != nil {
		t.Fatal(err)
	}
	if len(eventTypes.EventTypes) != 1 {
		t.Fatalf("unexpected event types %+v", eventTypes)
	}

	relay, err := client.CreateRelay(ctx, RelayCreateRequest{
		ConnectionKey: "live::gmail::default::abc123",
		Description:   "mail relay",
		EventFilters:  []string{"message.received"},
		CreateWebhook: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if relay.ID != "relay-1" {
		t.Fatalf("unexpected relay %+v", relay)
	}

	relay, err = client.ActivateRelay(ctx, RelayActivateRequest{
		ID:      "relay-1",
		Actions: []byte(`[{"type":"passthrough"}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !relay.Active {
		t.Fatalf("expected active relay, got %+v", relay)
	}

	events, err := client.ListRelayEvents(ctx, RelayEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(events.Events); got != 1 {
		t.Fatalf("expected 1 relay event, got %d", got)
	}

	detail, err := client.GetRelayEvent(ctx, "evt-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != "evt-1" {
		t.Fatalf("unexpected relay detail %+v", detail)
	}
}

func TestNewOneCLIFromEnvUsesManagedIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := config.Save(config.Config{
		APIKey:    "nex-key",
		OneAPIKey: "one-secret",
		Email:     "ceo@example.com",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	client := NewOneCLIFromEnv()
	got := strings.Join(client.Env, " ")
	if !strings.Contains(got, "ONE_SECRET=one-secret") {
		t.Fatalf("expected ONE_SECRET env, got %q", got)
	}
	if !strings.Contains(got, "ONE_IDENTITY=ceo@example.com") {
		t.Fatalf("expected ONE_IDENTITY env, got %q", got)
	}
	if !strings.Contains(got, "ONE_IDENTITY_TYPE=user") {
		t.Fatalf("expected ONE_IDENTITY_TYPE env, got %q", got)
	}
}

func TestOneCLIRequiresManagedProvisioning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := NewOneCLIFromEnv()
	_, err := client.ListConnections(context.Background(), ListConnectionsOptions{})
	if err == nil || !strings.Contains(err.Error(), "manages One integrations automatically through Nex") {
		t.Fatalf("expected managed provisioning error, got %v", err)
	}
}
