package team

import (
	"net/http"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/gbrain"
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
	opts := knowledgeEmbeddingOptions{
		GBrainInstalled:    gbrain.IsInstalled(),
		OpenAIKeySet:       strings.TrimSpace(config.ResolveOpenAIAPIKey()) != "",
		OllamaAvailable:    ollamaModel != "",
		OllamaModel:        ollamaModel,
		ActiveEmbedder:     gbrain.SelectEmbeddingModel(),
		EmbeddingAvailable: gbrain.EmbeddingAvailable(),
	}
	opts.Recommended = recommendedEmbedder(opts.GBrainInstalled)

	writeJSON(w, http.StatusOK, opts)
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
