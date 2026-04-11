package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/api"
)

// newTestClient creates an api.Client pointed at the given test server URL.
func newTestClient(serverURL string) *api.Client {
	c := api.NewClient("test-api-key")
	c.BaseURL = serverURL
	return c
}

// newTestCtx creates a SlashContext wired to a test API client with output capture.
func newTestCtx(serverURL string) (*SlashContext, *[]string) {
	var outputs []string
	ctx := &SlashContext{
		APIClient:  newTestClient(serverURL),
		Format:     "text",
		AddMessage: func(role, content string) { outputs = append(outputs, content) },
		SetLoading: func(bool) {},
		ShowPicker: nil,
		SendResult: func(out string, err error) {
			if out != "" {
				outputs = append(outputs, out)
			}
		},
	}
	return ctx, &outputs
}

func TestCmdManagedAgentNoArgs_ShowsUsage(t *testing.T) {
	ctx, outputs := newTestCtx("http://unused")
	if err := cmdManagedAgent(ctx, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*outputs) == 0 {
		t.Fatal("expected usage output, got none")
	}
	if !contains((*outputs)[0], "Usage") {
		t.Errorf("expected usage in output, got: %s", (*outputs)[0])
	}
}

func TestCmdManagedAgentApprove_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/approve") {
			t.Errorf("expected /approve in path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"approved"}`)
	}))
	defer srv.Close()

	ctx, outputs := newTestCtx(srv.URL)
	if err := cmdManagedAgentApprove(ctx, []string{"run-123"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*outputs) == 0 {
		t.Fatal("expected output, got none")
	}
	combined := strings.Join(*outputs, " ")
	if !contains(combined, "run-123") {
		t.Errorf("expected run ID in output, got: %s", combined)
	}
	if !contains(combined, "approved") {
		t.Errorf("expected 'approved' in output, got: %s", combined)
	}
}

func TestCmdManagedAgentApprove_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error":"run not found"}`)
	}))
	defer srv.Close()

	ctx, _ := newTestCtx(srv.URL)
	err := cmdManagedAgentApprove(ctx, []string{"run-999"})
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !contains(err.Error(), "404") {
		t.Errorf("expected 404 in error message, got: %s", err.Error())
	}
}

func TestCmdManagedAgentRespond_Success(t *testing.T) {
	var capturedBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/respond") {
			t.Errorf("expected /respond in path, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"responded"}`)
	}))
	defer srv.Close()

	ctx, outputs := newTestCtx(srv.URL)
	if err := cmdManagedAgentRespond(ctx, []string{"run-456", "yes", "proceed"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedBody["message"] != "yes proceed" {
		t.Errorf("expected message='yes proceed', got %q", capturedBody["message"])
	}

	combined := strings.Join(*outputs, " ")
	if !contains(combined, "run-456") {
		t.Errorf("expected run ID in output, got: %s", combined)
	}
}

func TestCmdManagedAgentDeny_Success(t *testing.T) {
	var capturedBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/deny") {
			t.Errorf("expected /deny in path, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"denied"}`)
	}))
	defer srv.Close()

	ctx, outputs := newTestCtx(srv.URL)
	// Simulate how strings.Fields splits the user input:
	// "/managed-agent deny run-789 --reason not safe" → parts[1:] = ["run-789","--reason","not","safe"]
	if err := cmdManagedAgentDeny(ctx, []string{"run-789", "--reason", "not", "safe"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedBody["deny_message"] != "not safe" {
		t.Errorf("expected deny_message='not safe', got %q", capturedBody["deny_message"])
	}

	combined := strings.Join(*outputs, " ")
	if !contains(combined, "denied") {
		t.Errorf("expected 'denied' in output, got: %s", combined)
	}
}

func TestCmdManagedAgentStop_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/stop") {
			t.Errorf("expected /stop in path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"stopped"}`)
	}))
	defer srv.Close()

	ctx, outputs := newTestCtx(srv.URL)
	if err := cmdManagedAgentStop(ctx, []string{"run-stop-1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	combined := strings.Join(*outputs, " ")
	if !contains(combined, "stopped") {
		t.Errorf("expected 'stopped' in output, got: %s", combined)
	}
}

func TestCmdManagedAgentEvents_ApprovalNeeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/events") {
			t.Errorf("expected /events in path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("response writer does not support flushing")
			return
		}
		fmt.Fprintf(w, "data: {\"type\":\"task_started\",\"run_id\":\"run-abc\"}\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "data: {\"type\":\"approval_needed\",\"run_id\":\"run-abc\"}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	ctx, outputs := newTestCtx(srv.URL)
	err := cmdManagedAgentEvents(ctx, []string{"run-abc"})

	if err == nil {
		t.Fatal("expected exit-code error for approval_needed, got nil")
	}
	exitErr, ok := err.(*exitCodeError)
	if !ok {
		t.Fatalf("expected *exitCodeError, got %T: %v", err, err)
	}
	if exitErr.code != 2 {
		t.Errorf("expected exit code 2, got %d", exitErr.code)
	}

	// Should have received events in output.
	if len(*outputs) == 0 {
		t.Error("expected event lines in output, got none")
	}
	combined := strings.Join(*outputs, "\n")
	if !contains(combined, "task_started") {
		t.Errorf("expected task_started event in output, got: %s", combined)
	}
	if !contains(combined, "approval_needed") {
		t.Errorf("expected approval_needed event in output, got: %s", combined)
	}
}

func TestCmdManagedAgentEvents_IdleExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		fmt.Fprintf(w, "data: {\"type\":\"session.status_idle\"}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	ctx, _ := newTestCtx(srv.URL)
	err := cmdManagedAgentEvents(ctx, []string{"run-idle"})
	if err != nil {
		t.Fatalf("expected clean exit for idle, got error: %v", err)
	}
}

func TestCmdManagedAgentUnknownSubcommand(t *testing.T) {
	ctx, outputs := newTestCtx("http://unused")
	if err := cmdManagedAgent(ctx, "invalid-sub run-id"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	combined := strings.Join(*outputs, " ")
	if !contains(combined, "Unknown subcommand") {
		t.Errorf("expected 'Unknown subcommand' in output, got: %s", combined)
	}
}

func TestCmdManagedAgentRegistered(t *testing.T) {
	r := NewRegistry()
	RegisterAllCommands(r)
	if _, ok := r.Get("managed-agent"); !ok {
		t.Error("expected 'managed-agent' to be registered")
	}
}
