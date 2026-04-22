package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nex-crm/wuphf/internal/commands"
)

// newCommandsHTTPTest spins up a minimal broker + /commands route for the
// registry-mirror endpoint. It returns the broker token so callers can
// authenticate their requests.
func newCommandsHTTPTest(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	t.Cleanup(func() { brokerStatePath = oldPathFn })

	b := NewBroker()
	mux := http.NewServeMux()
	mux.HandleFunc("/commands", b.requireAuth(b.handleCommands))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, b.Token()
}

func doGetCommands(t *testing.T, ts *httptest.Server, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/commands", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

// TestHandleCommands_ReturnsRegistrySubset verifies the endpoint mirrors the
// real TUI registry, includes both web-supported and TUI-only commands, and
// only flips webSupported=true for the documented web handler set. This is
// the contract the web's useCommands hook depends on.
func TestHandleCommands_ReturnsRegistrySubset(t *testing.T) {
	ts, token := newCommandsHTTPTest(t)

	resp := doGetCommands(t, ts, token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type=%q, want application/json", ct)
	}

	var list []commandDescriptor
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("empty command list")
	}

	byName := make(map[string]commandDescriptor, len(list))
	for _, c := range list {
		if _, dup := byName[c.Name]; dup {
			t.Errorf("duplicate command %q", c.Name)
		}
		byName[c.Name] = c
	}

	// Web-supported: the set Composer.tsx handleSlashCommand actually
	// routes today. If you add a handler to the switch, flip its flag in
	// slash.go and add it here.
	webSupported := []string{
		"ask", "search", "remember", "help", "clear", "reset",
		"requests", "policies", "skills", "calendar", "tasks",
		"recover", "threads", "provider", "focus", "collab",
		"pause", "resume", "1o1", "task", "cancel",
	}
	for _, name := range webSupported {
		c, ok := byName[name]
		if !ok {
			t.Errorf("missing web-supported command %q", name)
			continue
		}
		if !c.WebSupported {
			t.Errorf("command %q: webSupported=false, want true", name)
		}
	}

	// TUI-only. These should show up in the payload for TUI discovery but
	// must never leak into the web autocomplete.
	tuiOnly := []string{
		"object", "record", "list", "rel", "attribute", "agent",
		"config", "detect", "init", "graph", "insights",
		"youtube-pack", "quit", "note", "chat",
	}
	for _, name := range tuiOnly {
		c, ok := byName[name]
		if !ok {
			t.Errorf("missing TUI command %q", name)
			continue
		}
		if c.WebSupported {
			t.Errorf("command %q: webSupported=true, want false (TUI-only)", name)
		}
	}
}

// TestHandleCommands_SortedAlphabetically locks in the alphabetical ordering
// contract from commands.Registry.List so the web can skip its own sort.
func TestHandleCommands_SortedAlphabetically(t *testing.T) {
	ts, token := newCommandsHTTPTest(t)
	resp := doGetCommands(t, ts, token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	var list []commandDescriptor
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].Name > list[i].Name {
			t.Fatalf("not sorted at index %d: %q > %q", i, list[i-1].Name, list[i].Name)
		}
	}
}

// TestHandleCommands_MethodNotAllowed guards against accidental POST exposure.
// The registry is read-only over HTTP.
func TestHandleCommands_MethodNotAllowed(t *testing.T) {
	ts, token := newCommandsHTTPTest(t)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/commands", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status=%d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// TestHandleCommands_RequiresAuth verifies the bearer-token gate. An
// unauthenticated caller must get 401 rather than a silent dump of the
// registry.
func TestHandleCommands_RequiresAuth(t *testing.T) {
	ts, _ := newCommandsHTTPTest(t)
	resp := doGetCommands(t, ts, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated status=%d, want 401", resp.StatusCode)
	}
}

// stubLister lets us drive handleCommands against a deterministic command
// set without loading the full TUI registry.
type stubLister struct {
	items []commands.SlashCommand
}

func (s stubLister) List() []commands.SlashCommand { return s.items }

// TestHandleCommands_UsesInjectedRegistry proves the registry seam is live:
// swapping newCommandsRegistry reshapes the response. This keeps future
// handler tweaks from silently pinning the handler to the global registry.
func TestHandleCommands_UsesInjectedRegistry(t *testing.T) {
	original := newCommandsRegistry
	t.Cleanup(func() { newCommandsRegistry = original })

	newCommandsRegistry = func() registryLister {
		return stubLister{items: []commands.SlashCommand{
			{Name: "alpha", Description: "alpha desc", WebSupported: true},
			{Name: "beta", Description: "beta desc", WebSupported: false},
		}}
	}

	ts, token := newCommandsHTTPTest(t)
	resp := doGetCommands(t, ts, token)
	defer resp.Body.Close()

	var list []commandDescriptor
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []commandDescriptor{
		{Name: "alpha", Description: "alpha desc", WebSupported: true},
		{Name: "beta", Description: "beta desc", WebSupported: false},
	}
	if len(list) != len(want) {
		t.Fatalf("len=%d, want %d (%+v)", len(list), len(want), list)
	}
	for i, c := range list {
		if c != want[i] {
			t.Errorf("index %d: got %+v, want %+v", i, c, want[i])
		}
	}
}
