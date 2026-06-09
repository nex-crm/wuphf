package team

import "testing"

// TestOfficeEvals runs the U0.1 outcome eval harness in CI. Checks marked
// as known gaps are allowed to be red (they are the executable form of the
// uplift plan and flip green as phases land); anything else failing is a
// harness regression and fails the build.
func TestOfficeEvals(t *testing.T) {
	report, err := RunOfficeEvals(t.TempDir())
	if err != nil {
		t.Fatalf("run office evals: %v", err)
	}
	for _, c := range report.Checks {
		status := "PASS"
		if !c.Pass {
			status = "FAIL"
			if c.KnownGap != "" {
				status = "KNOWN-GAP (red until " + c.KnownGap + ")"
			}
		}
		t.Logf("[%s] %s / %s — %s", status, c.Job, c.Check, c.Detail)
	}
	for _, c := range report.UnexpectedFailures() {
		t.Errorf("eval regression: %s / %s — %s", c.Job, c.Check, c.Detail)
	}
	// A known gap going green means a phase landed: the KnownGap marker
	// must be removed in the same PR so the check becomes a regression
	// guard. Surface that loudly instead of letting it ride.
	for _, c := range report.KnownGapStatus() {
		if c.Pass {
			t.Errorf("known gap %q now PASSES (%s / %s) — remove its KnownGap marker to lock it in as a regression guard", c.KnownGap, c.Job, c.Check)
		}
	}
}
