package provider

// Ollama runs as a local daemon (`brew services start ollama`) on :11434
// and exposes both its native API and an OpenAI-compatible surface at
// /v1/chat/completions. The default model below is a coder-tuned Qwen2.5
// in q4_K_M GGUF; pull it with `ollama pull qwen2.5-coder:32b-instruct-q4_K_M`.
//
// Override per-run via WUPHF_OLLAMA_BASE_URL / WUPHF_OLLAMA_MODEL or via
// config.ProviderEndpoints["ollama"].
const (
	defaultOllamaBaseURL = "http://127.0.0.1:11434/v1"
	defaultOllamaModel   = "qwen2.5-coder:32b-instruct-q4_K_M"
)

func init() {
	Register(&Entry{
		Kind:     KindOllama,
		StreamFn: NewOpenAICompatStreamFn(KindOllama, defaultOllamaBaseURL, defaultOllamaModel),
		Capabilities: Capabilities{
			PaneEligible:    false,
			SupportsOneShot: false,
		},
	})
}
