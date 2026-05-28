package team

// broker_onboarding_scan_test.go — unit tests for the website-scan recovery
// path (#934).
//
// Covers:
//   - scanFailureReason returns the first non-empty warning when present
//   - scanFailureReason falls back to the raw scan error
//   - scanFailureReason returns a generic message when both are empty
//   - ceoDeterministicMessages(PhaseBlueprint) acknowledges a skipped scan
//   - PhaseScan → PhaseWebsite is a legal transition (retry path)

import (
	"errors"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/onboarding"
	"github.com/nex-crm/wuphf/internal/operations"
)

func TestScanFailureReason_PrefersFirstWarning(t *testing.T) {
	result := &operations.CompanySeedResult{
		Warnings: []string{
			"URL fetch failed: 404 Not Found",
			"LLM extraction failed: empty body",
		},
	}
	got := scanFailureReason(errors.New("wrapper error"), result)
	want := "URL fetch failed: 404 Not Found"
	if got != want {
		t.Errorf("scanFailureReason = %q, want %q", got, want)
	}
}

func TestScanFailureReason_SkipsEmptyWarnings(t *testing.T) {
	result := &operations.CompanySeedResult{
		Warnings: []string{"   ", "", "the real reason"},
	}
	got := scanFailureReason(nil, result)
	if got != "the real reason" {
		t.Errorf("scanFailureReason = %q, want %q", got, "the real reason")
	}
}

func TestScanFailureReason_FallsBackToErr(t *testing.T) {
	got := scanFailureReason(errors.New("dial tcp: i/o timeout"), nil)
	if got != "dial tcp: i/o timeout" {
		t.Errorf("scanFailureReason = %q, want raw error", got)
	}
}

func TestScanFailureReason_NoWarningsNoErr(t *testing.T) {
	got := scanFailureReason(nil, &operations.CompanySeedResult{})
	if got == "" {
		t.Errorf("scanFailureReason returned empty; want fallback message")
	}
	// Should not panic; should be a complete sentence.
	if !strings.HasSuffix(got, ".") {
		t.Errorf("scanFailureReason fallback %q should end with a period", got)
	}
}

func TestCeoDeterministicMessages_BlueprintAcksSkippedScan(t *testing.T) {
	s := &onboarding.State{
		FormAnswers: onboarding.FormAnswers{
			CompanyName:  "Acme Test QA",
			WebsiteURL:   "acme.test",
			ScanComplete: false,
		},
	}
	msgs := ceoDeterministicMessages(onboarding.PhaseBlueprint, s)
	if len(msgs) != 2 {
		t.Fatalf("expected ack + chip row (2 messages); got %d", len(msgs))
	}
	if msgs[0].Kind != "text" {
		t.Fatalf("expected first message to be plain text; got kind=%s", msgs[0].Kind)
	}
	if !strings.Contains(msgs[0].Content, "skipping the scan") {
		t.Errorf("ack should mention skipping the scan; got %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, "Acme Test QA") {
		t.Errorf("ack should mention the company name; got %q", msgs[0].Content)
	}
	if msgs[1].Kind != "ceo_chip_row" {
		t.Errorf("expected second message to be ceo_chip_row; got %s", msgs[1].Kind)
	}
}

func TestCeoDeterministicMessages_BlueprintNoAckOnSuccess(t *testing.T) {
	s := &onboarding.State{
		FormAnswers: onboarding.FormAnswers{
			CompanyName:  "Acme",
			WebsiteURL:   "acme.test",
			ScanComplete: true,
		},
	}
	msgs := ceoDeterministicMessages(onboarding.PhaseBlueprint, s)
	if len(msgs) != 1 {
		t.Fatalf("expected only the chip row when scan succeeded; got %d", len(msgs))
	}
	if msgs[0].Kind != "ceo_chip_row" {
		t.Errorf("got kind=%s, want ceo_chip_row", msgs[0].Kind)
	}
}

func TestCeoDeterministicMessages_BlueprintNoAckWhenNoUrl(t *testing.T) {
	// The "skip website" path enters PhaseBlueprint with no URL ever set;
	// the ack should not fire because there was no scan to skip.
	s := &onboarding.State{
		FormAnswers: onboarding.FormAnswers{
			CompanyName: "Acme",
			WebsiteURL:  "",
		},
	}
	msgs := ceoDeterministicMessages(onboarding.PhaseBlueprint, s)
	if len(msgs) != 1 {
		t.Fatalf("expected only the chip row when no URL was provided; got %d", len(msgs))
	}
}

// TestBlueprintChipOptions_LoadsFullCatalog locks in the regression that the
// onboarding blueprint picker has to surface every blueprint shipped under
// templates/operations, each with its icon and description, plus the
// "Start from scratch" sentinel last. The old PhaseBlueprint emit used a
// hardcoded three-entry slice and dropped the rest of the catalog from the
// UI; this test fails if anyone falls back to that shape.
func TestBlueprintChipOptions_LoadsFullCatalog(t *testing.T) {
	opts := blueprintChipOptions()
	if len(opts) < 4 {
		t.Fatalf("expected catalog + scratch (>=4 options); got %d", len(opts))
	}

	last := opts[len(opts)-1]
	if last["id"] != "" {
		t.Errorf("scratch sentinel must come last with id=\"\"; got %v", last["id"])
	}
	if got, _ := last["label"].(string); got != "Start from scratch" {
		t.Errorf("scratch label = %q, want %q", got, "Start from scratch")
	}

	bookkeeping := findOptionByID(opts, "bookkeeping-invoicing-service")
	if bookkeeping == nil {
		t.Fatal("expected bookkeeping-invoicing-service in options")
	}
	if got, _ := bookkeeping["icon"].(string); got == "" {
		t.Errorf("bookkeeping option must carry an icon; got empty")
	}
	if got, _ := bookkeeping["description"].(string); got == "" {
		t.Errorf("bookkeeping option must carry a description; got empty")
	}

	// Every catalog entry must have id, label, icon, description.
	for _, opt := range opts[:len(opts)-1] {
		if id, _ := opt["id"].(string); id == "" {
			t.Errorf("catalog entry has empty id: %v", opt)
		}
		if label, _ := opt["label"].(string); label == "" {
			t.Errorf("catalog entry has empty label: %v", opt)
		}
		if desc, _ := opt["description"].(string); desc == "" {
			t.Errorf("catalog entry has empty description: %v", opt)
		}
	}
}

func findOptionByID(opts []map[string]interface{}, id string) map[string]interface{} {
	for _, opt := range opts {
		if got, _ := opt["id"].(string); got == id {
			return opt
		}
	}
	return nil
}

func TestLegalPhaseTransitions_PhaseScanToPhaseWebsite(t *testing.T) {
	// Recovery: PhaseScan → PhaseWebsite must be legal so the user can
	// retry with a different URL after a scan failure. (#934)
	if !onboarding.IsLegalTransition(onboarding.PhaseScan, onboarding.PhaseWebsite) {
		t.Errorf("expected PhaseScan → PhaseWebsite to be legal for retry")
	}
	// Skip path is unchanged.
	if !onboarding.IsLegalTransition(onboarding.PhaseScan, onboarding.PhaseBlueprint) {
		t.Errorf("expected PhaseScan → PhaseBlueprint to be legal")
	}
}
