package provider

// OpenClaw Gateway can expose an OpenAI-compatible HTTP surface on the same
// port as its WebSocket gateway. This kind is intentionally distinct from
// KindOpenclaw: "openclaw" bridges existing OpenClaw sessions into the office,
// while "openclaw-http" runs WUPHF-created office members through the gateway's
// /v1/chat/completions endpoint.
const (
	defaultOpenclawHTTPBaseURL = "http://127.0.0.1:18789/v1"
	defaultOpenclawHTTPModel   = "openclaw/default"
)

func init() {
	Register(&Entry{
		Kind:     KindOpenclawHTTP,
		StreamFn: NewOpenAICompatStreamFn(KindOpenclawHTTP, defaultOpenclawHTTPBaseURL, defaultOpenclawHTTPModel),
		Capabilities: Capabilities{
			PaneEligible:    false,
			SupportsOneShot: false,
		},
	})
}
