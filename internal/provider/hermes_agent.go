package provider

// Hermes Agent exposes an OpenAI-compatible API server on :8642 when its
// gateway api_server platform is enabled. WUPHF uses that supported HTTP
// surface rather than OpenClaw's gateway WebSocket protocol, which Hermes does
// not expose.
const (
	defaultHermesAgentBaseURL = "http://127.0.0.1:8642/v1"
	defaultHermesAgentModel   = "hermes-agent"
)

func init() {
	Register(&Entry{
		Kind:     KindHermesAgent,
		StreamFn: NewOpenAICompatStreamFn(KindHermesAgent, defaultHermesAgentBaseURL, defaultHermesAgentModel),
		Capabilities: Capabilities{
			PaneEligible:    false,
			SupportsOneShot: false,
		},
	})
}
