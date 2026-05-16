package team

// broker_workspaces_test.go — table-driven tests for the multi-workspace
// HTTP surface. Three concerns covered:
//
//  1. withAuth middleware — rejects unauthenticated, allows valid bearer,
//     applies uniformly to every /workspaces/* and /admin/* route.
//  2. Each handler — JSON shape, validation, error paths, fake orchestrator
//     wiring.
//  3. Pause proxy — handleWorkspacesPause forwards to a fake target broker
//     via httptest.Server with the sibling token from disk.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/workspaces"
)

// fakeOrchestrator is a programmable workspaceOrchestrator for table tests.
// Each call records its inputs and returns the configured response, so
// tests can assert both the response shape and the orchestrator-call
// arguments.
type fakeOrchestrator struct {
	mu sync.Mutex

	listResp    []Workspace
	listErr     error
	createResp  Workspace
	createErr   error
	switchErr   error
	resumeErr   error
	shredErr    error
	restoreResp Workspace
	restoreErr  error
	trashResp   []TrashEntry
	trashErr    error
	onboardErr  error

	calls []string // human-readable trace, e.g. "list", "create:demo"
}

func (f *fakeOrchestrator) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeOrchestrator) callTrace() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]string, len(f.calls))
	copy(cp, f.calls)
	return cp
}

func (f *fakeOrchestrator) List(_ context.Context) ([]Workspace, error) {
	f.record("list")
	return f.listResp, f.listErr
}

func (f *fakeOrchestrator) Create(_ context.Context, req CreateRequest) (Workspace, error) {
	f.record("create:" + req.Name)
	return f.createResp, f.createErr
}

func (f *fakeOrchestrator) Switch(_ context.Context, name string) error {
	f.record("switch:" + name)
	return f.switchErr
}

func (f *fakeOrchestrator) Pause(_ context.Context, name string) error {
	f.record("pause:" + name)
	return nil
}

func (f *fakeOrchestrator) Resume(_ context.Context, name string) error {
	f.record("resume:" + name)
	return f.resumeErr
}

func (f *fakeOrchestrator) Shred(_ context.Context, name string, permanent bool) (string, error) {
	f.record(fmt.Sprintf("shred:%s:permanent=%v", name, permanent))
	if f.shredErr != nil {
		return "", f.shredErr
	}
	if permanent {
		return "", nil
	}
	return name + "-1714305600", nil
}

func (f *fakeOrchestrator) Restore(_ context.Context, trashID string) (Workspace, error) {
	f.record("restore:" + trashID)
	return f.restoreResp, f.restoreErr
}

func (f *fakeOrchestrator) Trash(_ context.Context) ([]TrashEntry, error) {
	f.record("trash")
	return f.trashResp, f.trashErr
}

func (f *fakeOrchestrator) Onboard(_ context.Context, name string, fields OnboardingFields) error {
	f.record(fmt.Sprintf("onboard:%s:%s/%s/%s/%s",
		name,
		fields.CompanyDescription,
		fields.CompanyPriority,
		fields.LLMProvider,
		fields.TeamLeadSlug))
	return f.onboardErr
}

// recordingDrainer records Drain calls; tests assert it was called.
type recordingDrainer struct {
	called atomic.Bool
}

func (d *recordingDrainer) Drain(_ context.Context) error {
	d.called.Store(true)
	return nil
}

// newWorkspaceTestBroker returns a broker preconfigured with a fake
// orchestrator + recording drainer + no-op exit hook. The orchestrator
// and drainer are returned so tests can inspect calls.
func newWorkspaceTestBroker(t *testing.T) (*Broker, *fakeOrchestrator, *recordingDrainer) {
	t.Helper()
	b := newTestBroker(t)
	o := &fakeOrchestrator{}
	d := &recordingDrainer{}
	b.SetWorkspaceOrchestrator(o)
	b.SetLauncherDrainer(d)
	b.SetAdminPauseExitFn(func(int) {}) // never let admin pause kill the test process
	return b, o, d
}

// --- 1. withAuth middleware ---------------------------------------------------

func TestWithAuth_RejectsUnauthenticated(t *testing.T) {
	b := newTestBroker(t)
	called := false
	srv := httptest.NewServer(b.withAuth(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", resp.StatusCode)
	}
	if called {
		t.Fatalf("inner handler should not be invoked on auth failure")
	}
}

func TestWithAuth_AcceptsValidBearer(t *testing.T) {
	b := newTestBroker(t)
	called := false
	srv := httptest.NewServer(b.withAuth(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	if !called {
		t.Fatalf("inner handler should be invoked on valid auth")
	}
}

func TestWithAuth_AcceptsTokenQueryParam(t *testing.T) {
	// The query-param form exists for EventSource (which can't set
	// headers). It is part of the documented contract — assert it works.
	b := newTestBroker(t)
	srv := httptest.NewServer(b.withAuth(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "?token=" + url.QueryEscape(b.Token()))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
}

func TestWithAuth_RequireAuthAlias(t *testing.T) {
	// requireAuth is the legacy name; both should behave identically.
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// No bearer
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("require auth no-bearer: want 401, got %d", resp.StatusCode)
	}

	// Valid bearer
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("require auth bearer: want 200, got %d", resp.StatusCode)
	}
}

// --- 2. Per-handler shape + validation tests --------------------------------

func TestHandleWorkspacesList_ReturnsJSONShape(t *testing.T) {
	b, o, _ := newWorkspaceTestBroker(t)
	o.listResp = []Workspace{
		{Name: "main", BrokerPort: 7890, WebPort: 7891, State: "running"},
		{Name: "demo", BrokerPort: 7910, WebPort: 7911, State: "paused"},
	}

	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesList))
	defer srv.Close()

	resp := mustGet(t, srv.URL, b.Token())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: %d body: %s", resp.StatusCode, string(body))
	}
	var payload struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Workspaces) != 2 {
		t.Fatalf("workspaces: want 2, got %d", len(payload.Workspaces))
	}
	if payload.Workspaces[0].Name != "main" {
		t.Fatalf("first workspace name: %q", payload.Workspaces[0].Name)
	}
	if payload.Workspaces[1].State != "paused" {
		t.Fatalf("second workspace state: %q", payload.Workspaces[1].State)
	}
}

func TestHandleWorkspacesList_MarksActiveWorkspaceWithCanonicalRuntimeHome(t *testing.T) {
	runtimeHome := t.TempDir()
	parent := t.TempDir()
	linkPath := filepath.Join(parent, "runtime-link")
	if err := os.Symlink(runtimeHome, linkPath); err != nil {
		t.Fatalf("symlink runtime home: %v", err)
	}
	t.Setenv("WUPHF_RUNTIME_HOME", linkPath)

	b, o, _ := newWorkspaceTestBroker(t)
	o.listResp = []Workspace{
		{Name: "main", RuntimeHome: runtimeHome, BrokerPort: 7890, WebPort: 7891, State: "running"},
		{Name: "demo", RuntimeHome: t.TempDir(), BrokerPort: 7910, WebPort: 7911, State: "paused"},
	}

	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesList))
	defer srv.Close()

	resp := mustGet(t, srv.URL, b.Token())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: %d body: %s", resp.StatusCode, string(body))
	}
	var payload struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Workspaces) != 2 {
		t.Fatalf("workspaces: want 2, got %d", len(payload.Workspaces))
	}
	if !payload.Workspaces[0].IsActive {
		t.Fatalf("main workspace should be active after symlink canonicalization")
	}
	if payload.Workspaces[1].IsActive {
		t.Fatalf("demo workspace should not be active")
	}
}

func TestHandleWorkspacesList_NoOrchestratorReturns503(t *testing.T) {
	b := newTestBroker(t)
	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesList))
	defer srv.Close()

	resp := mustGet(t, srv.URL, b.Token())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status: want 503, got %d", resp.StatusCode)
	}
}

func TestHandleWorkspacesCreate_ValidatesAndDelegates(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		fakeResp   Workspace
		fakeErr    error
		wantStatus int
		wantCall   string // expected fake.calls entry (or "" for no call)
	}{
		{
			name:       "happy path",
			body:       `{"name":"demo-launch","blueprint":"founding-team"}`,
			fakeResp:   Workspace{Name: "demo-launch", BrokerPort: 7910, WebPort: 7911, State: "running"},
			wantStatus: http.StatusCreated,
			wantCall:   "create:demo-launch",
		},
		{
			name:       "invalid name (uppercase)",
			body:       `{"name":"Demo"}`,
			wantStatus: http.StatusBadRequest,
			wantCall:   "",
		},
		{
			name:       "invalid name (starts with digit)",
			body:       `{"name":"1demo"}`,
			wantStatus: http.StatusBadRequest,
			wantCall:   "",
		},
		{
			name:       "invalid name (empty)",
			body:       `{"name":""}`,
			wantStatus: http.StatusBadRequest,
			wantCall:   "",
		},
		{
			name:       "orchestrator failure",
			body:       `{"name":"demo-launch"}`,
			fakeErr:    errors.New("port allocation failed"),
			wantStatus: http.StatusInternalServerError,
			wantCall:   "create:demo-launch",
		},
		{
			name:       "malformed json",
			body:       `{"name":}`,
			wantStatus: http.StatusBadRequest,
			wantCall:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, o, _ := newWorkspaceTestBroker(t)
			o.createResp = tc.fakeResp
			o.createErr = tc.fakeErr

			srv := httptest.NewServer(b.withAuth(b.handleWorkspacesCreate))
			defer srv.Close()

			resp := mustPost(t, srv.URL, b.Token(), tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status: want %d got %d body=%s", tc.wantStatus, resp.StatusCode, string(body))
			}

			calls := o.callTrace()
			if tc.wantCall == "" {
				if len(calls) != 0 {
					t.Fatalf("orchestrator should not be called; got %v", calls)
				}
			} else {
				if len(calls) != 1 || calls[0] != tc.wantCall {
					t.Fatalf("orchestrator call: want [%q], got %v", tc.wantCall, calls)
				}
			}
		})
	}
}

func TestHandleWorkspacesSwitch_DelegatesAndReturnsName(t *testing.T) {
	b, o, _ := newWorkspaceTestBroker(t)

	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesSwitch))
	defer srv.Close()

	resp := mustPost(t, srv.URL, b.Token(), `{"name":"main"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: %d body: %s", resp.StatusCode, string(body))
	}
	var payload struct {
		Ok   bool   `json:"ok"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.Ok || payload.Name != "main" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if got := o.callTrace(); len(got) != 1 || got[0] != "switch:main" {
		t.Fatalf("orchestrator call: %v", got)
	}
}

func TestHandleWorkspacesResume_HappyAndError(t *testing.T) {
	t.Run("happy", func(t *testing.T) {
		b, _, _ := newWorkspaceTestBroker(t)
		srv := httptest.NewServer(b.withAuth(b.handleWorkspacesResume))
		defer srv.Close()
		resp := mustPost(t, srv.URL, b.Token(), `{"name":"demo"}`)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: %d", resp.StatusCode)
		}
	})
	t.Run("orchestrator error", func(t *testing.T) {
		b, o, _ := newWorkspaceTestBroker(t)
		o.resumeErr = errors.New("spawn timeout")
		srv := httptest.NewServer(b.withAuth(b.handleWorkspacesResume))
		defer srv.Close()
		resp := mustPost(t, srv.URL, b.Token(), `{"name":"demo"}`)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("status: want 500 got %d", resp.StatusCode)
		}
	})
}

func TestHandleWorkspacesShred_RespectsPermanentFlag(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantCall    string
		wantTrashID string // empty means trash_id must be absent
	}{
		{"default false", `{"name":"demo"}`, "shred:demo:permanent=false", "demo-1714305600"},
		{"explicit true", `{"name":"demo","permanent":true}`, "shred:demo:permanent=true", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, o, _ := newWorkspaceTestBroker(t)
			srv := httptest.NewServer(b.withAuth(b.handleWorkspacesShred))
			defer srv.Close()
			resp := mustPost(t, srv.URL, b.Token(), tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status: %d body: %s", resp.StatusCode, string(body))
			}
			if calls := o.callTrace(); len(calls) != 1 || calls[0] != tc.wantCall {
				t.Fatalf("orchestrator call: want [%q] got %v", tc.wantCall, calls)
			}
			var got map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			gotTrashID, _ := got["trash_id"].(string)
			if gotTrashID != tc.wantTrashID {
				t.Fatalf("trash_id: want %q got %q", tc.wantTrashID, gotTrashID)
			}
		})
	}
}

func TestHandleWorkspacesRestore_RequiresTrashID(t *testing.T) {
	b, o, _ := newWorkspaceTestBroker(t)
	o.restoreResp = Workspace{Name: "demo", BrokerPort: 7912, WebPort: 7913, State: "running"}

	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesRestore))
	defer srv.Close()

	t.Run("missing trash_id", func(t *testing.T) {
		resp := mustPost(t, srv.URL, b.Token(), `{}`)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status: want 400 got %d", resp.StatusCode)
		}
	})
	t.Run("happy path", func(t *testing.T) {
		o.calls = nil // reset
		resp := mustPost(t, srv.URL, b.Token(), `{"trash_id":"demo-1714000000"}`)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status: %d body: %s", resp.StatusCode, string(body))
		}
		var got Workspace
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Name != "demo" {
			t.Fatalf("workspace name: %q", got.Name)
		}
	})
}

// TestHandleWorkspacesTrash_ReturnsListShape pins the wire shape of
// GET /workspaces/trash: {"trash": [...]} with each entry mirroring the
// internal/workspaces.TrashEntry contract.
func TestHandleWorkspacesTrash_ReturnsListShape(t *testing.T) {
	b, o, _ := newWorkspaceTestBroker(t)
	o.trashResp = []TrashEntry{
		{Name: "demo", TrashID: "demo-1714000000", Path: "/tmp/x/demo-1714000000", ShredAt: "2024-04-25T00:00:00Z"},
		{Name: "scratch", TrashID: "scratch-1715000000", Path: "/tmp/x/scratch-1715000000"},
	}

	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesTrash))
	defer srv.Close()

	resp := mustGet(t, srv.URL, b.Token())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: %d body: %s", resp.StatusCode, string(body))
	}
	var payload struct {
		Trash []TrashEntry `json:"trash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Trash) != 2 {
		t.Fatalf("trash entries: want 2 got %d", len(payload.Trash))
	}
	if payload.Trash[0].Name != "demo" || payload.Trash[0].TrashID != "demo-1714000000" {
		t.Fatalf("entry 0 mismatch: %+v", payload.Trash[0])
	}
	calls := o.callTrace()
	if len(calls) != 1 || calls[0] != "trash" {
		t.Fatalf("orchestrator call: want [trash], got %v", calls)
	}
}

func TestHandleWorkspacesTrash_NilOrchestratorReturns503(t *testing.T) {
	b := newTestBroker(t)
	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesTrash))
	defer srv.Close()
	resp := mustGet(t, srv.URL, b.Token())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status: want 503 got %d", resp.StatusCode)
	}
}

func TestHandleWorkspacesTrash_NilEntriesEmitsEmptyArray(t *testing.T) {
	b, o, _ := newWorkspaceTestBroker(t)
	o.trashResp = nil
	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesTrash))
	defer srv.Close()
	resp := mustGet(t, srv.URL, b.Token())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"trash":[]`) {
		t.Fatalf("expected empty trash array in body: %s", string(body))
	}
}

// --- /workspaces/onboarding ---------------------------------------------------

func TestHandleWorkspacesOnboarding_DelegatesToOrchestrator(t *testing.T) {
	b, o, _ := newWorkspaceTestBroker(t)
	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesOnboarding))
	defer srv.Close()

	body := `{"name":"demo","company_description":"d","company_priority":"p","llm_provider":"openai","team_lead_slug":"alice"}`
	resp := mustPost(t, srv.URL, b.Token(), body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: %d body: %s", resp.StatusCode, string(bodyBytes))
	}
	calls := o.callTrace()
	want := "onboard:demo:d/p/openai/alice"
	if len(calls) != 1 || calls[0] != want {
		t.Fatalf("orchestrator call: want [%q] got %v", want, calls)
	}
}

func TestHandleWorkspacesOnboarding_RejectsInvalidName(t *testing.T) {
	b, _, _ := newWorkspaceTestBroker(t)
	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesOnboarding))
	defer srv.Close()
	resp := mustPost(t, srv.URL, b.Token(), `{"name":"BAD"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: want 400 got %d", resp.StatusCode)
	}
}

func TestHandleWorkspacesOnboarding_NotFoundFromOrchestrator(t *testing.T) {
	b, o, _ := newWorkspaceTestBroker(t)
	o.onboardErr = workspaces.ErrWorkspaceNotFound
	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesOnboarding))
	defer srv.Close()
	resp := mustPost(t, srv.URL, b.Token(), `{"name":"missing","company_description":"x"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: want 404 got %d", resp.StatusCode)
	}
}

// Method-not-allowed coverage: every handler that takes POST rejects GET, the
// list handler takes GET and rejects POST.
func TestWorkspaceHandlers_RejectWrongMethod(t *testing.T) {
	b, _, _ := newWorkspaceTestBroker(t)

	tests := []struct {
		name    string
		handler http.HandlerFunc
		method  string
	}{
		{"list rejects POST", b.handleWorkspacesList, http.MethodPost},
		{"create rejects GET", b.handleWorkspacesCreate, http.MethodGet},
		{"switch rejects GET", b.handleWorkspacesSwitch, http.MethodGet},
		{"pause rejects GET", b.handleWorkspacesPause, http.MethodGet},
		{"resume rejects GET", b.handleWorkspacesResume, http.MethodGet},
		{"shred rejects GET", b.handleWorkspacesShred, http.MethodGet},
		{"restore rejects GET", b.handleWorkspacesRestore, http.MethodGet},
		{"trash rejects POST", b.handleWorkspacesTrash, http.MethodPost},
		{"onboarding rejects GET", b.handleWorkspacesOnboarding, http.MethodGet},
		{"admin pause rejects GET", b.handleAdminPause, http.MethodGet},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(b.withAuth(tc.handler))
			defer srv.Close()
			req, _ := http.NewRequest(tc.method, srv.URL, nil)
			req.Header.Set("Authorization", "Bearer "+b.Token())
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("status: want 405 got %d", resp.StatusCode)
			}
		})
	}
}

// --- 3. Pause delegates to orchestrator ---------------------------------------

// pauseRecorder is a workspaceOrchestrator stub that records the inbound
// Pause call so the handler-level test can assert delegation without
// re-running the orchestrator's HTTP machinery.
type pauseRecorder struct {
	fakeOrchestrator
	pausedName atomic.Value // string
	pauseErr   error
}

func (p *pauseRecorder) Pause(_ context.Context, name string) error {
	p.pausedName.Store(name)
	return p.pauseErr
}

func TestHandleWorkspacesPause_DelegatesToOrchestrator(t *testing.T) {
	b := newTestBroker(t)
	rec := &pauseRecorder{}
	b.SetWorkspaceOrchestrator(rec)
	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesPause))
	defer srv.Close()

	resp := mustPost(t, srv.URL, b.Token(), `{"name":"demo"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 202 got %d body=%s", resp.StatusCode, string(body))
	}
	if got, _ := rec.pausedName.Load().(string); got != "demo" {
		t.Fatalf("orchestrator.Pause name: want %q got %q", "demo", got)
	}
}

func TestHandleWorkspacesPause_NotFoundFromOrchestrator(t *testing.T) {
	// When the orchestrator surfaces ErrWorkspaceNotFound the handler must
	// translate it through errorToStatus to 404. This is the same surface
	// the previous hand-rolled proxy gave us via the missing-token branch,
	// just expressed through the typed-error mapper now that the
	// orchestrator owns the cross-broker call.
	b := newTestBroker(t)
	rec := &pauseRecorder{pauseErr: workspaces.ErrWorkspaceNotFound}
	b.SetWorkspaceOrchestrator(rec)
	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesPause))
	defer srv.Close()

	resp := mustPost(t, srv.URL, b.Token(), `{"name":"missing"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: want 404 got %d", resp.StatusCode)
	}
}

func TestHandleWorkspacesPause_OrchestratorErrorReturns500(t *testing.T) {
	b := newTestBroker(t)
	rec := &pauseRecorder{pauseErr: errors.New("broker still alive after kill ladder")}
	b.SetWorkspaceOrchestrator(rec)
	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesPause))
	defer srv.Close()

	resp := mustPost(t, srv.URL, b.Token(), `{"name":"demo"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status: want 500 got %d", resp.StatusCode)
	}
}

// TestHandleWorkspacesPause_BrokerHTTPCallStillCarriesSiblingToken pins the
// invariant that the cross-broker /admin/pause call uses the sibling
// workspace's token from ~/.wuphf-spaces/tokens/<name>.token. Since the
// orchestrator now owns that wiring, the assertion is exercised against
// orchestrator-level coverage rather than the handler. We repeat it here as
// a smoke test that catches a future regression where the handler tries to
// shortcut around the orchestrator.
func TestHandleWorkspacesPause_BrokerHTTPCallStillCarriesSiblingToken(t *testing.T) {
	// Stand up a fake target broker that records the Authorization header.
	receivedAuth := ""
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/pause" {
			receivedAuth = r.Header.Get("Authorization")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	// Pretend the sibling token is staged at the well-known path so the
	// orchestrator-shaped fake can read it back. We don't run the real
	// orchestrator here — we simulate the contract: the handler calls
	// orch.Pause with the workspace name; the orchestrator (stub) reads
	// the token and forwards it.
	tokenDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tokenDir, "demo.token"), []byte("sibling-token"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	// Wire a stub Pause that simulates what the real orchestrator does:
	// reads the sibling token + POSTs to the target's /admin/pause.
	stub := &orchestratorPauseStub{tokenDir: tokenDir, targetURL: target.URL}
	b := newTestBroker(t)
	b.SetWorkspaceOrchestrator(stub)
	srv := httptest.NewServer(b.withAuth(b.handleWorkspacesPause))
	defer srv.Close()

	resp := mustPost(t, srv.URL, b.Token(), `{"name":"demo"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 202 got %d body=%s", resp.StatusCode, string(body))
	}
	if receivedAuth != "Bearer sibling-token" {
		t.Fatalf("target authorization: want %q got %q", "Bearer sibling-token", receivedAuth)
	}
}

// orchestratorPauseStub is a workspaceOrchestrator that mimics the real
// orchestrator's Pause: read sibling token, POST to target /admin/pause.
type orchestratorPauseStub struct {
	fakeOrchestrator
	tokenDir  string
	targetURL string
}

func (s *orchestratorPauseStub) Pause(ctx context.Context, name string) error {
	tokenPath := filepath.Join(s.tokenDir, name+".token")
	tok, err := os.ReadFile(tokenPath)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.targetURL+"/admin/pause", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// --- handleAdminPause ---------------------------------------------------------

func TestHandleAdminPause_DrainsAndCallsExit(t *testing.T) {
	b, _, drainer := newWorkspaceTestBroker(t)

	// Replace the exit hook with one that records the code and signals
	// completion via a channel — the goroutine path must complete before
	// the test ends.
	exited := make(chan int, 1)
	b.SetAdminPauseExitFn(func(code int) { exited <- code })

	// Bind to a real port so RemoteAddr is loopback.
	srv := httptest.NewServer(b.withAuth(b.handleAdminPause))
	defer srv.Close()

	resp := mustPost(t, srv.URL, b.Token(), "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 202 got %d body=%s", resp.StatusCode, string(body))
	}

	select {
	case code := <-exited:
		if code != 0 {
			t.Fatalf("exit code: want 0 got %d", code)
		}
	case <-makeTimeoutCh(2):
		t.Fatalf("admin pause exit hook never fired")
	}
	if !drainer.called.Load() {
		t.Fatalf("Drain was never called")
	}
}

func TestHandleAdminPause_RejectsNonLoopback(t *testing.T) {
	// httptest.Server always binds 127.0.0.1, so we exercise the loopback
	// guard by injecting a non-loopback RemoteAddr directly on a recorder.
	b, _, _ := newWorkspaceTestBroker(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/pause", strings.NewReader(""))
	r.Header.Set("Authorization", "Bearer "+b.Token())
	r.RemoteAddr = "8.8.8.8:55555"

	b.withAuth(b.handleAdminPause)(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: want 403 got %d", w.Code)
	}
}

// --- 4. Auth-route assertion --------------------------------------------------

// TestEveryProtectedRouteRequiresAuth iterates every workspace + admin route
// and asserts the unauthenticated request returns 401. This is the design's
// "every protected route requires bearer" assertion in test form, scoped to
// Lane C's new routes.
//
// For the whole-mux assertion (every route registered on the broker, not
// just the workspace ones), see TestBrokerMuxAuthCoverage below — it boots a
// real broker on a real port and probes the full path list.
//
// The unauth allowlist is intentionally small and tested separately — it
// covers /web-token (which has its own loopback/Host guards), /health,
// /version, and /events (which validates auth inline since it streams).
func TestEveryProtectedRouteRequiresAuth(t *testing.T) {
	b, _, _ := newWorkspaceTestBroker(t)

	// Each route is registered with the same withAuth wrapping that
	// production uses (mirrors broker.go's HandleFunc block).
	routes := map[string]http.HandlerFunc{
		"/workspaces/list":       b.handleWorkspacesList,
		"/workspaces/create":     b.handleWorkspacesCreate,
		"/workspaces/switch":     b.handleWorkspacesSwitch,
		"/workspaces/pause":      b.handleWorkspacesPause,
		"/workspaces/resume":     b.handleWorkspacesResume,
		"/workspaces/shred":      b.handleWorkspacesShred,
		"/workspaces/restore":    b.handleWorkspacesRestore,
		"/workspaces/trash":      b.handleWorkspacesTrash,
		"/workspaces/onboarding": b.handleWorkspacesOnboarding,
		"/admin/pause":           b.handleAdminPause,
	}

	for path, handler := range routes {
		t.Run(path, func(t *testing.T) {
			srv := httptest.NewServer(b.withAuth(handler))
			defer srv.Close()

			// No Authorization header / no ?token=.
			req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("path %s: want 401 unauthenticated, got %d", path, resp.StatusCode)
			}

			// Wrong token should also be rejected.
			req2, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
			req2.Header.Set("Authorization", "Bearer wrong-token")
			resp2, err := http.DefaultClient.Do(req2)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp2.Body.Close()
			if resp2.StatusCode != http.StatusUnauthorized {
				t.Fatalf("path %s: want 401 wrong-token, got %d", path, resp2.StatusCode)
			}
		})
	}
}

// TestWorkspaceTokenPath_ResolvesUnderOverride asserts the token path helper
// honors the test override and produces the design-specified shape.
func TestWorkspaceTokenPath_ResolvesUnderOverride(t *testing.T) {
	dir := t.TempDir()
	prev := workspaceTokenDirOverride
	workspaceTokenDirOverride = dir
	t.Cleanup(func() { workspaceTokenDirOverride = prev })

	got := workspaceTokenPath("demo")
	want := filepath.Join(dir, "demo.token")
	if got != want {
		t.Fatalf("path: want %q got %q", want, got)
	}
}

// TestWorkspaceNameValid covers the slug rules from the design's
// "Workspace Slug Validation" section.
func TestWorkspaceNameValid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"valid lowercase", "demo", true},
		{"valid with hyphen and digit", "demo-launch-2", true},
		{"empty", "", false},
		{"uppercase", "Demo", false},
		{"starts with digit", "1demo", false},
		{"starts with hyphen", "-demo", false},
		{"contains underscore", "demo_a", false},
		{"contains dot", "demo.a", false},
		{"too long (32 chars)", strings.Repeat("a", 32), false},
		{"max length (31 chars)", strings.Repeat("a", 31), true},
		{"contains slash", "demo/a", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := workspaceNameValid(tc.in); got != tc.ok {
				t.Fatalf("workspaceNameValid(%q) = %v, want %v", tc.in, got, tc.ok)
			}
		})
	}
}

// TestBrokerMuxAuthCoverage boots a real broker on a real port and probes the
// full set of registered routes (workspace + non-workspace) without bearer
// authentication. The expectation: every route either returns 401 (auth
// enforced) OR is in unauthAllowlist with documented justification.
//
// The probe path list mirrors broker.go's mux.HandleFunc registrations. Adding
// a new route to broker.go without updating this list will surface as a test
// failure here, forcing a deliberate decision: wrap with withAuth, or add to
// the allowlist with a justifying comment.
//
// /events is treated specially: it does its own inline auth check rather than
// going through withAuth (because it streams indefinitely after auth passes),
// so it returns 401 just like a withAuth-wrapped route. /web-token is the
// loopback-only allowlist entry and additionally requires a loopback Host
// header — it returns 200 from the test client (which has loopback
// RemoteAddr + Host) but it stays in the allowlist because its protection
// model is different.
func TestBrokerMuxAuthCoverage(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: boots a real broker listener")
	}

	b := newTestBroker(t)
	b.token = "test-mux-coverage-token"
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(b.Stop)

	base := "http://" + b.Addr()

	// All routes registered in broker.go's StartOnPort. Sourced from
	// mux.HandleFunc lines and onboarding/workspace RegisterRoutes. Keep
	// this list synchronized with broker.go — the failure mode for adding
	// a route without updating this list is intentional ("forces a
	// deliberate auth decision" — see test docstring).
	protectedRoutes := []string{
		// upgrade
		"/upgrade-check",
		"/upgrade-changelog",
		"/upgrade/run",
		// session/messages/reactions
		"/session-mode",
		"/focus-mode",
		"/messages",
		"/reactions",
		"/notifications/nex",
		// office members + channels
		"/office-members",
		"/office-members/generate",
		"/channels",
		"/channels/dm",
		"/channels/generate",
		"/channel-members",
		"/members",
		// tasks + agent
		"/tasks",
		"/tasks/ack",
		"/agent-logs",
		"/task-plan",
		"/memory",
		// wiki
		"/wiki/write",
		"/wiki/write-human",
		"/humans",
		"/wiki/read",
		"/wiki/search",
		"/wiki/lookup",
		"/wiki/list",
		"/wiki/article",
		"/wiki/catalog",
		"/wiki/audit",
		"/wiki/visual",
		"/wiki/sections",
		"/wiki/lint/run",
		"/wiki/lint/resolve",
		"/wiki/extract/replay",
		"/wiki/dlq",
		// notebook
		"/notebook/write",
		"/notebook/read",
		"/notebook/list",
		"/notebook/catalog",
		"/notebook/search",
		"/notebook/promote",
		"/notebook/visual-artifacts",
		"/notebook/visual-artifacts/ra_0123456789abcdef",
		// review
		"/review/list",
		"/review/anything", // /review/ subpath wildcard
		// entity
		"/entity/fact",
		"/entity/brief/synthesize",
		"/entity/facts",
		"/entity/briefs",
		"/entity/graph",
		"/entity/graph/all",
		// playbook
		"/playbook/list",
		"/playbook/compile",
		"/playbook/execution",
		"/playbook/executions",
		"/playbook/synthesize",
		"/playbook/synthesis-status",
		// pam
		"/pam/actions",
		"/pam/action",
		// scan
		"/scan/start",
		"/scan/status",
		// studio + operations
		"/studio/generate-package",
		"/studio/bootstrap-package",
		"/operations/bootstrap-package",
		"/studio/run-workflow",
		// requests/interview
		"/requests",
		"/requests/answer",
		"/interview",
		"/interview/answer",
		// reset/usage/policies/signals/decisions/watchdogs/actions/scheduler
		"/reset",
		"/reset-dm",
		"/usage",
		"/policies",
		"/signals",
		"/decisions",
		"/watchdogs",
		"/actions",
		"/scheduler",
		// skills
		"/skills",
		"/skills/compile",
		"/skills/compile/stats",
		"/skills/anything", // /skills/ subpath wildcard
		// commands + telegram + bridges + queue + company + config
		"/commands",
		"/telegram/groups",
		"/bridges",
		"/queue",
		"/company",
		"/config",
		"/status/local-providers",
		"/image-providers",
		"/nex/register",
		"/v1/logs",
		// events stream — inline auth in handler
		"/events",
		// agent stream + tool event
		"/agent-stream/anything",
		"/agent-tool-event",
		// multi-workspace + admin pause
		"/workspaces/list",
		"/workspaces/create",
		"/workspaces/switch",
		"/workspaces/pause",
		"/workspaces/resume",
		"/workspaces/shred",
		"/workspaces/restore",
		"/workspaces/trash",
		"/workspaces/onboarding",
		"/admin/pause",
		// onboarding (mounted via onboarding.RegisterRoutes)
		"/onboarding/state",
		"/onboarding/progress",
		"/onboarding/complete",
		"/onboarding/prereqs",
		"/onboarding/validate-key",
		"/onboarding/templates",
		"/onboarding/blueprints",
		"/onboarding/checklist/dismiss",
		"/onboarding/checklist/anything", // /onboarding/checklist/ subpath
		// workspace wipes (mounted via workspace.RegisterRoutesWithOptions)
		"/workspace/reset",
		"/workspace/shred",
	}

	for _, path := range protectedRoutes {
		t.Run(path, func(t *testing.T) {
			// No auth: must reject with 401.
			req, err := http.NewRequest(http.MethodGet, base+path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("get %s unauth: %v", path, err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("path %s unauth: want 401, got %d", path, resp.StatusCode)
			}

			// Wrong token: must also reject with 401.
			req2, err := http.NewRequest(http.MethodGet, base+path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req2.Header.Set("Authorization", "Bearer wrong-token")
			resp2, err := http.DefaultClient.Do(req2)
			if err != nil {
				t.Fatalf("get %s wrong-token: %v", path, err)
			}
			_ = resp2.Body.Close()
			if resp2.StatusCode != http.StatusUnauthorized {
				t.Fatalf("path %s wrong-token: want 401, got %d", path, resp2.StatusCode)
			}
		})
	}
}

// TestBrokerMuxUnauthAllowlist asserts the documented unauth allowlist routes
// behave as designed. /health and /version are open. /web-token is loopback +
// Host gated, returning 200 from a loopback caller and 403 otherwise.
func TestBrokerMuxUnauthAllowlist(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: boots a real broker listener")
	}

	b := newTestBroker(t)
	b.token = "test-allowlist-token"
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(b.Stop)

	base := "http://" + b.Addr()

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"health open", "/health", http.StatusOK},
		{"version open", "/version", http.StatusOK},
		// /web-token: loopback test client gets 200 (no Bearer required).
		{"web-token loopback", "/web-token", http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(base + tc.path)
			if err != nil {
				t.Fatalf("get %s: %v", tc.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("path %s: want %d got %d body=%s",
					tc.path, tc.wantStatus, resp.StatusCode, string(body))
			}
		})
	}
}

// --- helpers -----------------------------------------------------------------

func mustGet(t *testing.T, urlStr, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, urlStr, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", urlStr, err)
	}
	return resp
}

func mustPost(t *testing.T, urlStr, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, urlStr, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", urlStr, err)
	}
	return resp
}

// makeTimeoutCh returns a channel that fires after seconds elapse. Wraps
// time.After so test failure messages can reference a named helper.
func makeTimeoutCh(seconds int) <-chan time.Time {
	return time.After(time.Duration(seconds) * time.Second)
}

// --- errorToStatus mapper -----------------------------------------------------

func TestErrorToStatus_MapsTypedSentinels(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil → 200", nil, http.StatusOK},
		{"slug invalid → 400", workspaces.ErrSlugInvalid{Slug: "Bad"}, http.StatusBadRequest},
		{"slug reserved → 400", workspaces.ErrSlugReserved{Slug: "main"}, http.StatusBadRequest},
		{"workspace not found → 404", workspaces.ErrWorkspaceNotFound, http.StatusNotFound},
		{"registry not found → 404", workspaces.ErrRegistryNotFound, http.StatusNotFound},
		{"workspace conflict → 409", workspaces.ErrWorkspaceConflict, http.StatusConflict},
		{"port pool exhausted → 503", workspaces.ErrPortPoolExhausted, http.StatusServiceUnavailable},
		{"port exhausted alias → 503", workspaces.ErrPortExhausted, http.StatusServiceUnavailable},
		{"unknown fix id → 400", workspaces.ErrUnknownFixID, http.StatusBadRequest},
		{"manual fix required → 409", workspaces.ErrManualFixRequired, http.StatusConflict},
		{
			name: "wrapped slug invalid still 400",
			err:  fmt.Errorf("create %q: %w", "x", workspaces.ErrSlugInvalid{Slug: "X"}),
			want: http.StatusBadRequest,
		},
		{
			name: "wrapped not-found still 404",
			err:  fmt.Errorf("resolve: %w", workspaces.ErrWorkspaceNotFound),
			want: http.StatusNotFound,
		},
		{
			name: "untyped already-exists falls back to 409",
			err:  errors.New(`workspaces: workspace "demo" already exists`),
			want: http.StatusConflict,
		},
		{
			name: "untyped opaque error falls back to 500",
			err:  errors.New("something exploded"),
			want: http.StatusInternalServerError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := errorToStatus(tc.err); got != tc.want {
				t.Fatalf("errorToStatus(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestHandlersSurfaceTypedSentinelsAsClientErrors hits each orchestrator-fronted
// handler with a typed sentinel and asserts the wire status matches the
// mapper. This pins the contract that the broker never papers over a 4xx-
// class orchestrator failure as 500.
func TestHandlersSurfaceTypedSentinelsAsClientErrors(t *testing.T) {
	type call struct {
		path    string
		body    string
		handler func(*Broker) http.HandlerFunc
		setErr  func(*fakeOrchestrator, error)
	}
	calls := map[string]call{
		"create": {
			path:    "/workspaces/create",
			body:    `{"name":"demo"}`,
			handler: func(b *Broker) http.HandlerFunc { return b.handleWorkspacesCreate },
			setErr:  func(f *fakeOrchestrator, e error) { f.createErr = e },
		},
		"switch": {
			path:    "/workspaces/switch",
			body:    `{"name":"demo"}`,
			handler: func(b *Broker) http.HandlerFunc { return b.handleWorkspacesSwitch },
			setErr:  func(f *fakeOrchestrator, e error) { f.switchErr = e },
		},
		"resume": {
			path:    "/workspaces/resume",
			body:    `{"name":"demo"}`,
			handler: func(b *Broker) http.HandlerFunc { return b.handleWorkspacesResume },
			setErr:  func(f *fakeOrchestrator, e error) { f.resumeErr = e },
		},
		"shred": {
			path:    "/workspaces/shred",
			body:    `{"name":"demo"}`,
			handler: func(b *Broker) http.HandlerFunc { return b.handleWorkspacesShred },
			setErr:  func(f *fakeOrchestrator, e error) { f.shredErr = e },
		},
		"restore": {
			path:    "/workspaces/restore",
			body:    `{"trash_id":"demo-1700000000"}`,
			handler: func(b *Broker) http.HandlerFunc { return b.handleWorkspacesRestore },
			setErr:  func(f *fakeOrchestrator, e error) { f.restoreErr = e },
		},
	}
	cases := []struct {
		errName string
		err     error
		want    int
	}{
		{"not found", workspaces.ErrWorkspaceNotFound, http.StatusNotFound},
		{"conflict", workspaces.ErrWorkspaceConflict, http.StatusConflict},
		{"port exhausted", workspaces.ErrPortExhausted, http.StatusServiceUnavailable},
		{"slug invalid", workspaces.ErrSlugInvalid{Slug: "x"}, http.StatusBadRequest},
		{"slug reserved", workspaces.ErrSlugReserved{Slug: "main"}, http.StatusBadRequest},
		{"opaque", errors.New("opaque"), http.StatusInternalServerError},
	}
	for handlerName, c := range calls {
		for _, tc := range cases {
			t.Run(handlerName+"_"+tc.errName, func(t *testing.T) {
				b, o, _ := newWorkspaceTestBroker(t)
				c.setErr(o, tc.err)
				srv := httptest.NewServer(b.withAuth(c.handler(b)))
				defer srv.Close()
				resp := mustPost(t, srv.URL, b.Token(), c.body)
				defer resp.Body.Close()
				if resp.StatusCode != tc.want {
					body, _ := io.ReadAll(resp.Body)
					t.Fatalf("%s/%s: want %d got %d body=%s",
						handlerName, tc.errName, tc.want, resp.StatusCode, string(body))
				}
			})
		}
	}
}
