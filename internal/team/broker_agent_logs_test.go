package team

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newTestBroker returns a Broker backed by a temp state file.
// It follows the same pattern used throughout broker_test.go.
func newTestBroker(t *testing.T) *Broker {
	t.Helper()
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	t.Cleanup(func() { brokerStatePath = oldPathFn })
	return NewBroker()
}

func TestHandleAgentLogs_ListsRecent(t *testing.T) {
	logRoot := t.TempDir()
	taskDir := filepath.Join(logRoot, "eng-12345")
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "output.log"),
		[]byte(`{"tool_name":"grep_search","agent_slug":"eng","started_at":1700000000000}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	b := newTestBroker(t)
	b.SetAgentLogRoot(logRoot)
	srv := httptest.NewServer(b.requireAuth(b.handleAgentLogs))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, body %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(payload.Tasks))
	}
	if payload.Tasks[0]["taskId"] != "eng-12345" {
		t.Fatalf("unexpected taskId: %v", payload.Tasks[0]["taskId"])
	}
}

func TestHandleAgentLogs_ReadsSingleTask(t *testing.T) {
	logRoot := t.TempDir()
	taskDir := filepath.Join(logRoot, "eng-12345")
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "output.log"),
		[]byte(`{"tool_name":"grep_search","agent_slug":"eng"}`+"\n"+
			`{"tool_name":"write_file","agent_slug":"eng"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	b := newTestBroker(t)
	b.SetAgentLogRoot(logRoot)
	srv := httptest.NewServer(b.requireAuth(b.handleAgentLogs))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"?task=eng-12345", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, body %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(payload.Entries))
	}
}

func TestHandleAgentLogs_RejectsPathTraversal(t *testing.T) {
	b := newTestBroker(t)
	b.SetAgentLogRoot(t.TempDir())
	srv := httptest.NewServer(b.requireAuth(b.handleAgentLogs))
	defer srv.Close()

	for _, bad := range []string{"../etc/passwd", "eng/../../../etc/passwd", "a/b"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"?task="+bad, nil)
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %q: %v", bad, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("task=%q: expected 400, got %d", bad, resp.StatusCode)
		}
	}
}
