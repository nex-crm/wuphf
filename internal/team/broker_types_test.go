package team

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAgentActivitySnapshotKindJSONRoundTrip pins the wire contract for the
// new Kind field on agentActivitySnapshot. Three things matter to lane B/C
// (frontend) consumers:
//
//   - The JSON key is exactly "kind" (lowercase, no prefix).
//   - omitempty is in effect: an unset Kind must not appear in the encoded
//     payload (frontend defaults to "routine" when absent).
//   - All three valid values ("routine", "milestone", "stuck") round-trip
//     untouched through encode -> decode.
func TestAgentActivitySnapshotKindJSONRoundTrip(t *testing.T) {
	t.Run("omitempty when unset", func(t *testing.T) {
		snap := agentActivitySnapshot{
			Slug:   "tess",
			Status: "active",
		}
		raw, err := json.Marshal(snap)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), `"kind"`) {
			t.Fatalf("expected kind to be omitempty when unset, got %s", raw)
		}
	})

	for _, kind := range []string{"routine", "milestone", "stuck"} {
		kind := kind
		t.Run("round trip "+kind, func(t *testing.T) {
			snap := agentActivitySnapshot{
				Slug: "tess",
				Kind: kind,
			}
			raw, err := json.Marshal(snap)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			expectFragment := `"kind":"` + kind + `"`
			if !strings.Contains(string(raw), expectFragment) {
				t.Fatalf("expected encoded payload to contain %s, got %s", expectFragment, raw)
			}
			var decoded agentActivitySnapshot
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if decoded.Kind != kind {
				t.Fatalf("decoded.Kind = %q, want %q", decoded.Kind, kind)
			}
		})
	}
}
