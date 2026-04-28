package upgradecheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestParity_NotableAndIsMajorBump locks the Go side of the upgrade-banner
// show-gate rules against testdata/upgrade-parity.json. The same fixture
// is read by the TS side (web/src/components/layout/upgradeBanner.utils.parity.test.ts)
// so a unilateral tweak to either implementation fails the corresponding
// language's test until the fixture AND the other side catch up.
//
// Why this exists: Notable/IsMajorBump and the version regex now live in
// three places (this package, internal/team.upgradeVersionParam, and
// web/src/components/layout/upgradeBanner.utils.ts). Without a shared
// gate, the next "just tweak the regex" PR would silently desync the
// banner from the broker; the symptom would be the banner under- or
// over-triggering on a release with a class of commits the two sides
// disagreed about.
func TestParity_NotableAndIsMajorBump(t *testing.T) {
	// Path is relative to this test file (internal/upgradecheck/) — two
	// hops up to the repo root, then into the cross-language testdata.
	path := filepath.Join("..", "..", "testdata", "upgrade-parity.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read parity fixture %q: %v", path, err)
	}
	var fix struct {
		Notable []struct {
			Name  string        `json:"name"`
			Input []CommitEntry `json:"input"`
			Want  bool          `json:"want"`
		} `json:"notable"`
		IsMajorBump []struct {
			Name string `json:"name"`
			From string `json:"from"`
			To   string `json:"to"`
			Want bool   `json:"want"`
		} `json:"isMajorBump"`
	}
	if err := json.Unmarshal(data, &fix); err != nil {
		t.Fatalf("decode parity fixture: %v", err)
	}
	if len(fix.Notable) == 0 || len(fix.IsMajorBump) == 0 {
		// Belt and suspenders: a typo in the JSON keys would silently
		// pass with zero cases. Force the fixture to actually carry
		// content so this test can't accidentally become a no-op.
		t.Fatalf("parity fixture appears empty (notable=%d, isMajorBump=%d)",
			len(fix.Notable), len(fix.IsMajorBump))
	}

	for _, c := range fix.Notable {
		t.Run("notable/"+c.Name, func(t *testing.T) {
			if got := Notable(c.Input); got != c.Want {
				t.Errorf("Notable(%+v) = %v, fixture wants %v", c.Input, got, c.Want)
			}
		})
	}
	for _, c := range fix.IsMajorBump {
		t.Run("isMajorBump/"+c.Name, func(t *testing.T) {
			if got := IsMajorBump(c.From, c.To); got != c.Want {
				t.Errorf("IsMajorBump(%q,%q) = %v, fixture wants %v", c.From, c.To, got, c.Want)
			}
		})
	}
}
