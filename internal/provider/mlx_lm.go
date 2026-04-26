package provider

// MLX-LM is Apple's MLX-backed inference server for Apple Silicon. Run it
// with: `mlx_lm.server --model <hf-id> --host 127.0.0.1 --port 8080`. It
// exposes a strict OpenAI-compatible /v1/chat/completions endpoint.
//
// The default model is the 7B coder so a first-run install on a 16 GB
// Mac doesn't OOM during model load (an mlx_lm.server OOM gets SIGKILLed
// by the kernel, which surfaces here as a confusing "connection refused"
// error). 64 GB+ users can flip to 32B with one config line:
//
//	~/.wuphf/config.json:
//	  "provider_endpoints": { "mlx-lm": { "model": "mlx-community/Qwen2.5-Coder-32B-Instruct-4bit" } }
//
// or env: WUPHF_MLX_LM_MODEL=mlx-community/Qwen2.5-Coder-32B-Instruct-4bit
const (
	defaultMLXLMBaseURL = "http://127.0.0.1:8080/v1"
	defaultMLXLMModel   = "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit"
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
