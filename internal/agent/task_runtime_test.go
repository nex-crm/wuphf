package agent

import "testing"

func TestCompactionTokenLimitPerSlug(t *testing.T) {
	t.Setenv(compactionTokenLimitEnv, "")

	if got := compactionTokenLimit("eng"); got != defaultTokenLimit {
		t.Errorf("specialist limit: got %d, want %d", got, defaultTokenLimit)
	}
	if got := compactionTokenLimit(ceoSlug); got != ceoTokenLimit {
		t.Errorf("ceo limit: got %d, want %d", got, ceoTokenLimit)
	}

	// Env override wins for both, so operators retain control.
	t.Setenv(compactionTokenLimitEnv, "5000")
	if got := compactionTokenLimit("eng"); got != 5000 {
		t.Errorf("specialist env override: got %d, want 5000", got)
	}
	if got := compactionTokenLimit(ceoSlug); got != 5000 {
		t.Errorf("ceo env override: got %d, want 5000", got)
	}
}
