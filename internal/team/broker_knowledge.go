package team

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/gbrain"
)

// Install state values for the gbrain on-demand installer. These are the wire
// values surfaced through GET /knowledge/embedding-options (install_state) and
// returned by POST /knowledge/install.
const (
	installStateIdle       = "idle"
	installStateInstalling = "installing"
	installStateInstalled  = "installed"
	installStateError      = "error"
)

// installTimeout bounds the whole gbrain install (Bun bootstrap + global
// install + migrations). It is generous: a cold install pulls Bun and compiles
// gbrain from source. The goroutine derives its context from
// context.Background() (NOT the HTTP request) so the install survives the POST
// returning 202 immediately.
const installTimeout = 12 * time.Minute

// Test seams. Production wires the real gbrain implementations; unit tests
// override these so POST /knowledge/install can be exercised without a live
// network install or a real gbrain binary.
var (
	gbrainEnsureInstalled = gbrain.EnsureInstalled
	gbrainEnsureBrain     = gbrain.EnsureBrain
)

// knowledgeEmbeddingOptions is the wire shape returned by
// GET /knowledge/embedding-options. The onboarding UI reads it to decide which
// embedding options to present (ask for an OpenAI key, offer a detected local
// Ollama model, or fall back to keyword-only retrieval).
//
// Keep these JSON field names in sync with the frontend type that consumes this
// endpoint. All fields are derived live from the host environment on each call;
// nothing is persisted by this handler.
type knowledgeEmbeddingOptions struct {
	GBrainInstalled    bool   `json:"gbrain_installed"`
	OpenAIKeySet       bool   `json:"openai_key_set"`
	OllamaAvailable    bool   `json:"ollama_available"`
	OllamaModel        string `json:"ollama_model"`
	ActiveEmbedder     string `json:"active_embedder"`
	EmbeddingAvailable bool   `json:"embedding_available"`
	Recommended        string `json:"recommended"`
	// Install lifecycle. The onboarding UI polls THIS endpoint to drive the
	// "Install gbrain" progress affordance, so capabilities and install
	// progress arrive in one request.
	//   - install_state:    "idle" | "installing" | "installed" | "error".
	//   - install_progress: last human-readable progress line, "" when none.
	//   - install_error:    last install error message, "" when none.
	// gbrain_installed flips true on a successful install (it reflects
	// gbrain.IsInstalled()), so the UI can also key off that.
	InstallState    string `json:"install_state"`
	InstallProgress string `json:"install_progress"`
	InstallError    string `json:"install_error"`
}

// handleKnowledgeEmbeddingOptions serves GET /knowledge/embedding-options.
//
// It reports the host's embedding capabilities so the onboarding wizard can
// present the right knowledge-setup choice without running any CLI:
//
//   - gbrain_installed:    whether the gbrain CLI is on PATH.
//   - openai_key_set:      whether an OpenAI key is configured (strongest embedder).
//   - ollama_available:    whether a local Ollama embedding model is pulled.
//   - ollama_model:        the detected local model name ("" when none).
//   - active_embedder:     what gbrain.EnsureBrain would select right now
//     ("" means keyword-only retrieval).
//   - embedding_available: whether semantic retrieval is possible at all.
//   - recommended:         the option the UI should surface first.
//
// recommended choice (documented per design ambiguity): we always recommend
// "openai" as the top suggestion when semantic retrieval is not already wired
// to a key — text-embedding-3-large is the strongest available embedder, so
// asking for an OpenAI key is the highest-value prompt. When an OpenAI key is
// already set we recommend "openai" too (it is the active path, nothing to
// change). The endpoint still exposes ollama_available so the UI can offer the
// local Ollama model as the privacy-preserving alternative, and "keyword" is
// only recommended when gbrain itself is absent (no embedding backend exists at
// all, so there is nothing to ask for).
func (b *Broker) handleKnowledgeEmbeddingOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ollamaModel := gbrain.OllamaEmbeddingModel()
	state, progress, errMsg := b.installSnapshot()
	opts := knowledgeEmbeddingOptions{
		GBrainInstalled:    gbrain.IsInstalled(),
		OpenAIKeySet:       strings.TrimSpace(config.ResolveOpenAIAPIKey()) != "",
		OllamaAvailable:    ollamaModel != "",
		OllamaModel:        ollamaModel,
		ActiveEmbedder:     gbrain.SelectEmbeddingModel(),
		EmbeddingAvailable: gbrain.EmbeddingAvailable(),
		InstallState:       state,
		InstallProgress:    progress,
		InstallError:       errMsg,
	}
	opts.Recommended = recommendedEmbedder(opts.GBrainInstalled)

	writeJSON(w, http.StatusOK, opts)
}

// knowledgeInstallResponse is the wire shape returned by POST
// /knowledge/install. It carries only the current install state — the UI then
// polls GET /knowledge/embedding-options for streamed progress and completion.
type knowledgeInstallResponse struct {
	InstallState string `json:"install_state"`
}

// handleKnowledgeInstall serves POST /knowledge/install. It kicks off the
// gbrain installer on an explicit user opt-in and returns immediately so the UI
// can poll for progress.
//
// Single-flight: a POST while an install is already running is a no-op that
// returns the current state (200). Otherwise it flips state to "installing",
// clears any prior error, spawns ONE background goroutine, and returns 202
// without blocking on it. The goroutine derives its own context from
// context.Background() with a generous timeout, so the HTTP response returning
// does NOT cancel the install. The whole flow is best-effort: a failed install
// surfaces as install_state="error" with a message; it never panics or crashes
// the broker.
func (b *Broker) handleKnowledgeInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	b.installMu.Lock()
	if b.installState == installStateInstalling {
		// Already running — single-flight no-op. Report the live state.
		current := b.installState
		b.installMu.Unlock()
		writeJSON(w, http.StatusOK, knowledgeInstallResponse{InstallState: current})
		return
	}
	b.installState = installStateInstalling
	b.installProgress = ""
	b.installError = ""
	b.installMu.Unlock()

	go b.runGBrainInstall()

	writeJSON(w, http.StatusAccepted, knowledgeInstallResponse{InstallState: installStateInstalling})
}

// runGBrainInstall executes the gbrain install end to end on its own context,
// streaming progress lines into broker state under installMu. On success it
// best-effort initializes a brain (a brain-init failure does NOT flip the state
// to error — installation itself succeeded). It is the single goroutine spawned
// per single-flight install.
func (b *Broker) runGBrainInstall() {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()

	progress := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		b.installMu.Lock()
		b.installProgress = line
		b.installMu.Unlock()
	}

	if err := gbrainEnsureInstalled(ctx, progress); err != nil {
		b.installMu.Lock()
		b.installState = installStateError
		b.installError = err.Error()
		b.installMu.Unlock()
		return
	}

	// Installation succeeded. Initialize a brain best-effort: its failure must
	// not flip the overall state to error, because the install itself worked.
	if _, err := gbrainEnsureBrain(ctx); err != nil {
		progress("brain init skipped: " + err.Error())
	}

	b.installMu.Lock()
	b.installState = installStateInstalled
	b.installMu.Unlock()
}

// installSnapshot returns the current install lifecycle state under installMu.
// An empty (zero-value) state is normalized to "idle" so the wire contract
// always carries a meaningful value.
func (b *Broker) installSnapshot() (state, progress, errMsg string) {
	b.installMu.Lock()
	defer b.installMu.Unlock()
	state = b.installState
	if state == "" {
		state = installStateIdle
	}
	return state, b.installProgress, b.installError
}

// recommendedEmbedder returns the embedding option the onboarding UI should
// surface first. See handleKnowledgeEmbeddingOptions for the rationale: OpenAI
// is the strongest embedder and is always the top suggestion when gbrain is
// present; "keyword" is only recommended when gbrain is absent (no embedding
// backend can be initialized at all).
func recommendedEmbedder(gbrainInstalled bool) string {
	if !gbrainInstalled {
		return "keyword"
	}
	return "openai"
}
