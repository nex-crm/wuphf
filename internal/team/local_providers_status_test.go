package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

// stubLookPath maps binary names to canned (path, err) results.
func stubLookPath(t *testing.T, results map[string]struct {
	path string
	err  error
}) func(string) (string, error) {
	t.Helper()
	return func(name string) (string, error) {
		r, ok := results[name]
		if !ok {
			return "", errors.New("not stubbed")
		}
		return r.path, r.err
	}
}

// TestComputeLocalProviderStatuses_AllInstalledAndReachable is the
// happy path: all three binaries on PATH, all three endpoints
// reachable, version + model are surfaced.
func TestComputeLocalProviderStatuses_AllInstalledAndReachable(t *testing.T) {
	ov := localProvidersStatusOverrides{
		lookPath: stubLookPath(t, map[string]struct {
			path string
			err  error
		}{
			"mlx_lm.server": {path: "/Users/x/.local/bin/mlx_lm.server"},
			"ollama":        {path: "/usr/local/bin/ollama"},
			"exo":           {path: "/Users/x/.local/bin/exo"},
		}),
		runVer: func(_ context.Context, path string, _ []string) (string, error) {
			return "v1.2.3", nil
		},
		probe: func(_ context.Context, baseURL string) (bool, string, bool) {
			return true, "fake-loaded-model:" + baseURL, true
		},
		goos: "darwin",
	}
	got := computeLocalProviderStatuses(context.Background(), ov)
	if len(got) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(got))
	}
	gotByKind := map[string]LocalProviderStatus{}
	for _, s := range got {
		gotByKind[s.Kind] = s
	}
	for _, k := range []string{provider.KindMLXLM, provider.KindOllama, provider.KindExo} {
		s, ok := gotByKind[k]
		if !ok {
			t.Fatalf("missing kind %q", k)
		}
		if !s.BinaryInstalled {
			t.Errorf("%s: BinaryInstalled = false, want true", k)
		}
		if !s.Reachable {
			t.Errorf("%s: Reachable = false, want true", k)
		}
		if !s.PlatformSupported {
			t.Errorf("%s: PlatformSupported = false on darwin, want true", k)
		}
		if s.BinaryVersion == "" {
			t.Errorf("%s: BinaryVersion empty, version probe should have populated it", k)
		}
		if !strings.HasPrefix(s.LoadedModel, "fake-loaded-model:") {
			t.Errorf("%s: LoadedModel = %q, want fake prefix", k, s.LoadedModel)
		}
	}
}

// TestComputeLocalProviderStatuses_NoneInstalled simulates a fresh
// machine: nothing on PATH, all three kinds report
// BinaryInstalled=false but still surface install commands so the
// doctor panel can render them.
func TestComputeLocalProviderStatuses_NoneInstalled(t *testing.T) {
	ov := localProvidersStatusOverrides{
		lookPath: func(string) (string, error) { return "", errors.New("not found") },
		runVer:   func(_ context.Context, _ string, _ []string) (string, error) { return "", nil },
		probe:    func(_ context.Context, _ string) (bool, string, bool) { return false, "", true },
		goos:     "darwin",
	}
	got := computeLocalProviderStatuses(context.Background(), ov)
	for _, s := range got {
		if s.BinaryInstalled {
			t.Errorf("%s: BinaryInstalled = true on a fresh box", s.Kind)
		}
		if len(s.Install) == 0 {
			t.Errorf("%s: Install map empty — UI has nothing to show", s.Kind)
		}
		if s.Endpoint == "" {
			t.Errorf("%s: Endpoint empty — UI can't render the URL", s.Kind)
		}
	}
}

// TestComputeLocalProviderStatuses_LinuxHidesMLXButShowsOllamaExo
// confirms the platform gate: MLX-LM is Apple Silicon only and must
// surface PlatformSupported=false on Linux. Ollama and Exo stay
// supported.
func TestComputeLocalProviderStatuses_LinuxHidesMLXButShowsOllamaExo(t *testing.T) {
	ov := localProvidersStatusOverrides{
		lookPath: func(string) (string, error) { return "", errors.New("not found") },
		runVer:   func(_ context.Context, _ string, _ []string) (string, error) { return "", nil },
		probe:    func(_ context.Context, _ string) (bool, string, bool) { return false, "", false },
		goos:     "linux",
	}
	got := computeLocalProviderStatuses(context.Background(), ov)
	for _, s := range got {
		switch s.Kind {
		case provider.KindMLXLM:
			if s.PlatformSupported {
				t.Errorf("mlx-lm: PlatformSupported = true on linux")
			}
		case provider.KindOllama, provider.KindExo:
			if !s.PlatformSupported {
				t.Errorf("%s: PlatformSupported = false on linux", s.Kind)
			}
		}
	}
}

// TestComputeLocalProviderStatuses_WindowsAddsWSL2Note covers the
// Windows path: every kind is unsupported and gets a one-liner
// nudging the user toward WSL2.
func TestComputeLocalProviderStatuses_WindowsAddsWSL2Note(t *testing.T) {
	ov := localProvidersStatusOverrides{
		lookPath: func(string) (string, error) { return "", errors.New("not found") },
		runVer:   func(_ context.Context, _ string, _ []string) (string, error) { return "", nil },
		probe:    func(_ context.Context, _ string) (bool, string, bool) { return false, "", false },
		goos:     "windows",
	}
	got := computeLocalProviderStatuses(context.Background(), ov)
	for _, s := range got {
		if s.PlatformSupported {
			t.Errorf("%s: PlatformSupported = true on windows; WSL2 path expected", s.Kind)
		}
		if !strings.Contains(strings.ToLower(s.WindowsNote), "wsl") {
			t.Errorf("%s: WindowsNote missing WSL hint: %q", s.Kind, s.WindowsNote)
		}
	}
}

// TestComputeLocalProviderStatuses_NonLoopbackEndpointSkipsProbe is
// the security guardrail test: a remote base URL set via env or
// config must NOT trigger an outbound probe from the broker. Probe
// is loopback-only.
func TestComputeLocalProviderStatuses_NonLoopbackEndpointSkipsProbe(t *testing.T) {
	t.Setenv("WUPHF_OLLAMA_BASE_URL", "http://10.0.0.99:11434/v1")

	// The probe must NEVER fire for the non-loopback ollama override.
	// mlx-lm and exo keep their loopback defaults so their probes are
	// fine — the assertion checks the URL, not the call count.
	ov := localProvidersStatusOverrides{
		lookPath: func(name string) (string, error) {
			if name == "ollama" {
				return "/usr/local/bin/ollama", nil
			}
			return "", errors.New("not found")
		},
		runVer: func(_ context.Context, _ string, _ []string) (string, error) { return "v1", nil },
		probe: func(_ context.Context, baseURL string) (bool, string, bool) {
			if strings.Contains(baseURL, "10.0.0.99") {
				t.Errorf("probe was called for non-loopback URL %q — security guardrail breached", baseURL)
			}
			return false, "", true
		},
		goos: "darwin",
	}
	got := computeLocalProviderStatuses(context.Background(), ov)
	for _, s := range got {
		if s.Kind != provider.KindOllama {
			continue
		}
		if s.Probed {
			t.Errorf("ollama Probed = true for non-loopback endpoint")
		}
		if s.Reachable {
			t.Errorf("ollama Reachable = true without probe")
		}
		if s.ProbeSkippedNote == "" {
			t.Errorf("ollama ProbeSkippedNote empty — UI has no explanation for the missing reachability dot")
		}
	}
}

// TestIsLoopbackBaseURL covers the security predicate directly. The
// test exists separately from the integration test above so a
// regression here is easy to localize.
func TestIsLoopbackBaseURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"http://127.0.0.1:8080/v1", true},
		{"http://[::1]:11434/v1", true},
		{"http://localhost:8080", true},
		{"http://10.0.0.5:8080", false},
		{"https://api.example.com/v1", false},
		{"", false},
		{"::not a url", false},
	}
	for _, tc := range cases {
		if got := isLoopbackBaseURL(tc.in); got != tc.want {
			t.Errorf("isLoopbackBaseURL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestProbeOpenAICompatEndpoint_Real exercises the production probe
// against a real httptest server, confirming the JSON shape parses
// and the first model id is surfaced.
func TestProbeOpenAICompatEndpoint_Real(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":[{"id":"qwen2.5-coder:7b"},{"id":"llama3"}]}`)
	}))
	defer srv.Close()

	reachable, model, ok := probeOpenAICompatEndpoint(context.Background(), srv.URL)
	if !ok {
		t.Fatal("ok = false; probe didn't even reach the server")
	}
	if !reachable {
		t.Fatal("reachable = false against a 200 server")
	}
	if model != "qwen2.5-coder:7b" {
		t.Errorf("model = %q, want qwen2.5-coder:7b", model)
	}
}

// TestProbeOpenAICompatEndpoint_Non2xx confirms a 503 surfaces as
// reachable=false rather than the loaded-model field getting a junk
// HTML body.
func TestProbeOpenAICompatEndpoint_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "loading", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	reachable, _, _ := probeOpenAICompatEndpoint(context.Background(), srv.URL)
	if reachable {
		t.Errorf("503 should not count as reachable")
	}
}

// TestHandleConfig_RoundTripsProviderEndpoints verifies the new
// /config GET/POST surface for the Settings UI: a partial map update
// merges into Config.ProviderEndpoints, an empty value for a kind
// clears that key (so users can drop overrides without hand-editing
// config.json), and unsupported kinds are rejected.
func TestComputeLocalProviderStatuses_DocumentedSurface(t *testing.T) {
	// Quick assertion the JSON serialization stays stable enough for
	// the frontend type to track. If a field gets renamed accidentally,
	// this catches it before web/src/api/client.ts silently breaks.
	ov := localProvidersStatusOverrides{
		lookPath: func(string) (string, error) { return "", errors.New("not found") },
		runVer:   func(_ context.Context, _ string, _ []string) (string, error) { return "", nil },
		probe:    func(_ context.Context, _ string) (bool, string, bool) { return false, "", false },
		goos:     "darwin",
	}
	got := computeLocalProviderStatuses(context.Background(), ov)
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"kind"`, `"binary_installed"`, `"endpoint"`, `"model"`,
		`"reachable"`, `"platform_supported"`, `"install"`,
	} {
		if !strings.Contains(string(body), key) {
			t.Errorf("response JSON missing field %s — frontend type will drift\nbody=%s", key, body)
		}
	}
}
