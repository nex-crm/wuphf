package provider

// MLX-LM is Apple's MLX-backed inference server for Apple Silicon. Run it
// with: `mlx_lm.server --model <hf-id> --host 127.0.0.1 --port 8080`. It
// exposes a strict OpenAI-compatible /v1/chat/completions endpoint.
//
// To override the defaults at runtime:
//   - Env: WUPHF_MLX_LM_BASE_URL=http://127.0.0.1:9000/v1, WUPHF_MLX_LM_MODEL=...
//   - Config (~/.wuphf/config.json):
//     "provider_endpoints": { "mlx-lm": { "base_url": "...", "model": "..." } }
const (
	defaultMLXLMBaseURL = "http://127.0.0.1:8080/v1"
	defaultMLXLMModel   = "mlx-community/Qwen2.5-Coder-32B-Instruct-4bit"
)

func init() {
	Register(&Entry{
		Kind:     KindMLXLM,
		StreamFn: NewOpenAICompatStreamFn(KindMLXLM, defaultMLXLMBaseURL, defaultMLXLMModel),
		Capabilities: Capabilities{
			PaneEligible:    false,
			SupportsOneShot: false,
		},
	})
}
