package teammcp

import "testing"

// TestResolveSlugRejectsSpoofedCEO pins the R6 hardening: a specialist
// process (env slug != ceo) cannot claim my_slug=ceo to reach the
// CEO-only scope-shaping actions (create/define/reassign/approve/...).
// The real CEO process (WUPHF_AGENT_SLUG=ceo) is unaffected.
func TestResolveSlugRejectsSpoofedCEO(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "eng")
	if _, err := resolveSlug("ceo"); err == nil {
		t.Fatal("specialist claiming my_slug=ceo must be rejected")
	}
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")
	slug, err := resolveSlug("ceo")
	if err != nil || slug != "ceo" {
		t.Fatalf("real ceo must resolve; got %q, %v", slug, err)
	}
}
