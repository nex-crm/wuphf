package team

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/provider"
	"github.com/nex-crm/wuphf/internal/runtimebin"
)

// LocalProviderStatus is the doctor-panel payload for one local OpenAI-
// compatible runtime. The Settings UI renders one card per kind: green
// (Reachable=true), yellow (BinaryInstalled=true but Reachable=false —
// installed but not started), or red (BinaryInstalled=false). The
// Install field carries copy-paste shell snippets the user runs
// themselves; the broker never executes them.
type LocalProviderStatus struct {
	Kind              string            `json:"kind"`
	BinaryInstalled   bool              `json:"binary_installed"`
	BinaryPath        string            `json:"binary_path,omitempty"`
	BinaryVersion     string            `json:"binary_version,omitempty"`
	Endpoint          string            `json:"endpoint"`
	Model             string            `json:"model"`
	Reachable         bool              `json:"reachable"`
	LoadedModel       string            `json:"loaded_model,omitempty"`
	Probed            bool              `json:"probed"`
	ProbeSkippedNote  string            `json:"probe_skipped_note,omitempty"`
	PlatformSupported bool              `json:"platform_supported"`
	WindowsNote       string            `json:"windows_note,omitempty"`
	Install           map[string]string `json:"install,omitempty"`
	Start             map[string]string `json:"start,omitempty"`
	Notes             []string          `json:"notes,omitempty"`
}

// localProviderSpec describes the per-kind detection inputs.
type localProviderSpec struct {
	kind            string
	binaryName      string
	versionArgs     []string
	platformAllowed func(goos string) bool
	install         map[string]string
	start           map[string]string
	notes           []string
}

var localProviderSpecs = []localProviderSpec{
	{
		kind:        provider.KindMLXLM,
		binaryName:  "mlx_lm.server",
		versionArgs: []string{"--version"},
		platformAllowed: func(goos string) bool {
			// MLX is Apple-Silicon only; users on Linux/Windows get a
			// clear "not supported here" so they don't waste time
			// trying to install it.
			return goos == "darwin"
		},
		install: map[string]string{
			"macos": "pipx install mlx-lm",
		},
		start: map[string]string{
			"macos": "mlx_lm.server --model mlx-community/Qwen2.5-Coder-7B-Instruct-4bit --host 127.0.0.1 --port 8080",
		},
		notes: []string{
			"Requires Apple Silicon. The 7B model is the safe default for first-run; bump to 32B on a 64GB+ Mac via the Model field above.",
		},
	},
	{
		kind:        provider.KindOllama,
		binaryName:  "ollama",
		versionArgs: []string{"--version"},
		platformAllowed: func(goos string) bool {
			// Ollama runs on macOS / Linux natively. On Windows we
			// recommend WSL2 rather than the native installer to keep
			// the supported surface narrow.
			return goos == "darwin" || goos == "linux"
		},
		install: map[string]string{
			"macos": "brew install ollama && brew services start ollama",
			"linux": "curl -fsSL https://ollama.com/install.sh | sh",
		},
		start: map[string]string{
			"macos": "ollama pull qwen2.5-coder:7b-instruct-q4_K_M",
			"linux": "ollama pull qwen2.5-coder:7b-instruct-q4_K_M",
		},
	},
	{
		kind:        provider.KindExo,
		binaryName:  "exo",
		versionArgs: []string{"--version"},
		platformAllowed: func(goos string) bool {
			return goos == "darwin" || goos == "linux"
		},
		install: map[string]string{
			"macos": "pipx install exo",
			"linux": "pipx install exo",
		},
		start: map[string]string{
			"macos": "exo",
			"linux": "exo",
		},
		notes: []string{
			"Exo distributes inference across multiple machines. On a single Mac it offers little over MLX-LM directly; install on a second machine and run `exo` on each to enable.",
		},
	},
}

// localProvidersStatusOverrides lets tests stub binary detection,
// version probing, HTTP probing, and runtime.GOOS without exec'ing.
type localProvidersStatusOverrides struct {
	lookPath func(name string) (string, error)
	runVer   func(ctx context.Context, path string, args []string) (string, error)
	probe    func(ctx context.Context, baseURL string) (reachable bool, loadedModel string, ok bool)
	goos     string
}

// defaultLocalProvidersOverrides returns the production wiring.
func defaultLocalProvidersOverrides() localProvidersStatusOverrides {
	return localProvidersStatusOverrides{
		lookPath: runtimebin.LookPath,
		runVer:   runVersionCommand,
		probe:    probeOpenAICompatEndpoint,
		goos:     runtime.GOOS,
	}
}

// handleLocalProvidersStatus serves GET /status/local-providers. The
// response is the doctor-panel payload for the Settings UI: one entry
// per registered local OpenAI-compatible kind, with binary-installed
// and reachable flags plus copy-paste install commands.
func (b *Broker) handleLocalProvidersStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	out := computeLocalProviderStatuses(r.Context(), defaultLocalProvidersOverrides())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// computeLocalProviderStatuses runs the detection probes and returns
// the response payload. Pulled out as its own function so tests can
// drive it with a stubbed overrides struct without spinning up a
// broker.
func computeLocalProviderStatuses(ctx context.Context, ov localProvidersStatusOverrides) []LocalProviderStatus {
	out := make([]LocalProviderStatus, 0, len(localProviderSpecs))
	for _, spec := range localProviderSpecs {
		out = append(out, computeOneStatus(ctx, spec, ov))
	}
	return out
}

func computeOneStatus(ctx context.Context, spec localProviderSpec, ov localProvidersStatusOverrides) LocalProviderStatus {
	st := LocalProviderStatus{
		Kind:              spec.kind,
		PlatformSupported: spec.platformAllowed(ov.goos),
		Install:           spec.install,
		Start:             spec.start,
		Notes:             append([]string(nil), spec.notes...),
	}
	if ov.goos == "windows" {
		st.WindowsNote = "Local LLM runtimes don't have native Windows support today. Run wuphf inside WSL2 (Ubuntu) and install the runtime there — the broker will then detect it from inside WSL."
	}

	// Resolve the effective endpoint by layering env > config > the
	// kind's registered compile-time defaults. Without the third layer
	// the UI would show a blank URL until the user typed something.
	defBaseURL, defModel := provider.OpenAICompatDefaults(spec.kind)
	endpoint, model := config.ResolveProviderEndpoint(spec.kind, defBaseURL, defModel)
	st.Endpoint = endpoint
	st.Model = model

	if path, err := ov.lookPath(spec.binaryName); err == nil {
		st.BinaryInstalled = true
		st.BinaryPath = path
		// Best-effort version capture; failures are silent — we already
		// know the binary exists, the version is just nice-to-have.
		verCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if v, err := ov.runVer(verCtx, path, spec.versionArgs); err == nil {
			st.BinaryVersion = strings.TrimSpace(v)
		}
		cancel()
	}

	// Probe the endpoint for reachability — but only if it's loopback.
	// Probing arbitrary remote URLs from the broker would let a hostile
	// config trigger outbound traffic to anywhere, and a slow remote
	// can hang the Settings page render.
	if st.PlatformSupported && isLoopbackBaseURL(endpoint) {
		probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		reachable, loadedModel, _ := ov.probe(probeCtx, endpoint)
		cancel()
		st.Reachable = reachable
		st.LoadedModel = loadedModel
		st.Probed = true
	} else if endpoint != "" {
		st.Probed = false
		st.ProbeSkippedNote = "Reachability probe is loopback-only; configured endpoint is non-local."
	}

	return st
}

// isLoopbackBaseURL reports whether the configured base URL points at
// 127.0.0.1, ::1, or localhost. Any DNS-resolvable hostname is treated
// as non-loopback (we don't want to resolve in the probe path).
func isLoopbackBaseURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	// DNS resolution is intentionally not done here — we don't want
	// the broker resolving arbitrary hostnames during a UI poll, and
	// localhost can be remapped (`/etc/hosts`) anyway. Treat the
	// literal name (case-insensitively, matching browser behavior) as
	// loopback; treat `0.0.0.0` as loopback too because servers bound
	// there are reachable from 127.0.0.1 in practice.
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if host == "0.0.0.0" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// runVersionCommand runs `<binary> <args...>` with a bounded ctx and
// returns the first line of output as the reported version. Heavy
// formatting differences across runtimes (e.g. mlx_lm.server prints
// `0.31.3`, ollama prints `ollama version is 0.1.31`) are surfaced
// verbatim — the UI doesn't try to parse them.
func runVersionCommand(ctx context.Context, path string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return first, nil
}

// probeOpenAICompatEndpoint hits `<baseURL>/models` (the OpenAI list-
// models endpoint, which all three runtimes support) and reports
// whether the server is reachable plus, if the response is the
// expected shape, the first model id it advertises. Any error or
// non-2xx → reachable=false.
func probeOpenAICompatEndpoint(ctx context.Context, baseURL string) (bool, string, bool) {
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, "", false
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 750 * time.Millisecond}).DialContext,
		},
		Timeout: 1500 * time.Millisecond,
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, "", true
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, "", true
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && len(body.Data) > 0 {
		return true, body.Data[0].ID, true
	}
	// Reachable but unrecognised JSON shape — still counts as reachable.
	return true, "", true
}
