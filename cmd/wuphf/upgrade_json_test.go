package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/nex-crm/wuphf/internal/upgradecheck"
)

// TestUpgradeJSONOutput locks the wire shape that `wuphf upgrade --json`
// emits. Catches:
//   - A future addition of an `Error` field to upgradecheck.Result that
//     would silently collide with our own Error field on the embedded
//     anonymous struct.
//   - Field rename or tag drop on upgradecheck.Result that would break
//     scripted callers reading via `jq`.
func TestUpgradeJSONOutput(t *testing.T) {
	res := upgradecheck.Result{
		Current:          "0.79.10",
		Latest:           "0.79.15",
		UpgradeAvailable: true,
		IsDevBuild:       false,
		CompareURL:       "https://github.com/nex-crm/wuphf/compare/v0.79.10...v0.79.15",
		UpgradeCommand:   "npm install -g wuphf@latest",
	}

	// Successful Check → no Error field in the JSON.
	out := upgradeJSONOutput(res, nil)
	buf, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal success: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf, &decoded); err != nil {
		t.Fatalf("unmarshal success: %v", err)
	}
	for _, want := range []string{
		"current", "latest", "upgrade_available", "is_dev_build",
		"compare_url", "upgrade_command",
	} {
		if _, ok := decoded[want]; !ok {
			t.Errorf("expected field %q in success JSON, got %v", want, decoded)
		}
	}
	if _, hasError := decoded["error"]; hasError {
		t.Errorf("expected NO error field on success, got %v", decoded["error"])
	}

	// Failed Check → Error field populated, embedded Result still
	// present so observability tools see what we did know.
	out2 := upgradeJSONOutput(res, errors.New("npm registry status 503"))
	buf2, _ := json.Marshal(out2)
	var decoded2 map[string]any
	_ = json.Unmarshal(buf2, &decoded2)
	if got, _ := decoded2["error"].(string); got != "npm registry status 503" {
		t.Errorf("expected error field %q, got %v", "npm registry status 503", decoded2["error"])
	}
	if got, _ := decoded2["current"].(string); got != "0.79.10" {
		t.Errorf("expected current field preserved on failure, got %v", decoded2["current"])
	}
}
