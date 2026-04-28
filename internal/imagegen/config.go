package imagegen

import (
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

// configString reads config.ImageEndpoints[kind].<field> as a string.
// Returns "" if the config doesn't exist or the field isn't set.
// Centralised here so every provider has one resolution path that reads
// from the same wuphf config layer the LLM providers already use.
func configString(kind, field string) string {
	cfg, _ := config.Load()
	if cfg.ImageEndpoints == nil {
		return ""
	}
	ep, ok := cfg.ImageEndpoints[kind]
	if !ok {
		return ""
	}
	switch strings.ToLower(field) {
	case "api_key":
		return ep.APIKey
	case "base_url":
		return ep.BaseURL
	case "model":
		return ep.Model
	}
	return ""
}
