package team

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/imagegen"
)

// handleImageProviders is GET/PUT /image-providers — Settings UI uses GET to
// render the per-provider card list and PUT to save the API key + base URL +
// default model the user pasted in.
func (b *Broker) handleImageProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		statuses := imagegen.AllStatuses(r.Context())
		writeJSON(w, http.StatusOK, map[string]any{"providers": statuses})
	case http.MethodPut:
		var body struct {
			Kind    string `json:"kind"`
			APIKey  string `json:"api_key"`
			BaseURL string `json:"base_url"`
			Model   string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		kind := strings.TrimSpace(body.Kind)
		if kind == "" {
			http.Error(w, "kind required", http.StatusBadRequest)
			return
		}
		// Validate the kind against the registered providers so a typo
		// doesn't silently land in config.
		if _, err := imagegen.ParseKind(kind); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Re-canonicalize so we always store the slug form.
		canon, _ := imagegen.ParseKind(kind)
		kind = string(canon)

		cfg, _ := config.Load()
		if cfg.ImageEndpoints == nil {
			cfg.ImageEndpoints = map[string]config.ImageEndpoint{}
		}
		ep := cfg.ImageEndpoints[kind]
		if v := strings.TrimSpace(body.APIKey); v != "" {
			ep.APIKey = v
		}
		if v := strings.TrimSpace(body.BaseURL); v != "" {
			ep.BaseURL = v
		}
		if v := strings.TrimSpace(body.Model); v != "" {
			ep.Model = v
		}
		cfg.ImageEndpoints[kind] = ep
		if err := config.Save(cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, imagegen.AllStatuses(r.Context()))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
