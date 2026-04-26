package provider

// Ollama runs as a local daemon (`brew services start ollama`) on :11434
// and exposes both its native API and an OpenAI-compatible surface at
// /v1/chat/completions. The default model below is a coder-tuned Qwen2.5
// at the 7B size so first-run installs work on 16 GB Macs; pull it with
// `ollama pull qwen2.5-coder:7b-instruct-q4_K_M`. 64 GB+ users can pin
// 32b in their config:
//
//	"provider_endpoints": { "ollama": { "model": "qwen2.5-coder:32b-instruct-q4_K_M" } }
//
// or env: WUPHF_OLLAMA_MODEL=qwen2.5-coder:32b-instruct-q4_K_M.
const (
	defaultOllamaBaseURL = "http://127.0.0.1:11434/v1"
	defaultOllamaModel   = "qwen2.5-coder:7b-instruct-q4_K_M"
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
