package provider

// Exo distributes inference across heterogeneous Apple/Linux devices and
// presents a single OpenAI-compatible HTTP surface (default :52415). On a
// single Mac it offers little over MLX-LM directly, but registering the
// Kind here means a multi-node setup is just `pip install exo && exo` on
// each machine — switch wuphf with `--provider exo` and you're routed.
//
// Model defaults to "default" because Exo's discovery layer picks the best
// installed model for the request; explicit names work too. Override with
// WUPHF_EXO_BASE_URL / WUPHF_EXO_MODEL or config.ProviderEndpoints["exo"].
const (
	defaultExoBaseURL = "http://127.0.0.1:52415/v1"
	defaultExoModel   = "default"
)

func init() {
	Register(&Entry{
		Kind:     KindExo,
		StreamFn: NewOpenAICompatStreamFn(KindExo, defaultExoBaseURL, defaultExoModel),
		Capabilities: Capabilities{
			PaneEligible:    false,
			SupportsOneShot: false,
		},
	})
}
