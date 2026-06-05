package teammcp

import "testing"

// resolve_slug_test.go covers the impersonation guard on the model-supplied
// my_slug argument (multi-agent security review CRITICAL): an agent must not
// be able to claim a reserved human/system identity (→ forge created_by=human
// to clear the Plan-mode gate) or a privileged built-in slug it was not
// launched as (→ my_slug=librarian for direct wiki-write authority). The
// launcher-set WUPHF_AGENT_SLUG env is the trusted identity.

func TestResolveSlug_RejectsImpersonationViaArg(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "")
	t.Setenv("NEX_AGENT_SLUG", "")
	for _, bad := range []string{
		"human", "you", "system", "nex", "broker", "librarian",
		"human:nazz", "Human", "LIBRARIAN",
	} {
		if got, err := resolveSlug(bad); err == nil {
			t.Errorf("resolveSlug(%q) must be rejected as impersonation; got %q, nil", bad, got)
		}
	}
}

func TestResolveSlug_AllowsNormalAgentSlug(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "")
	t.Setenv("NEX_AGENT_SLUG", "")
	for _, ok := range []string{"executor", "planner", "reviewer", "pm"} {
		if got, err := resolveSlug(ok); err != nil || got != ok {
			t.Errorf("resolveSlug(%q) = %q, %v; want %q, nil", ok, got, err, ok)
		}
	}
}

func TestResolveSlug_PrivilegedSlugOnlyFromTrustedEnv(t *testing.T) {
	// The launcher stamps WUPHF_AGENT_SLUG for the real librarian process; a
	// my_slug arg matching the trusted env slug is allowed, and env alone
	// (no arg) resolves to it.
	t.Setenv("WUPHF_AGENT_SLUG", "librarian")
	t.Setenv("NEX_AGENT_SLUG", "")
	if got, err := resolveSlug("librarian"); err != nil || got != "librarian" {
		t.Errorf("resolveSlug(\"librarian\") with env=librarian = %q, %v; want librarian, nil", got, err)
	}
	if got, err := resolveSlug(""); err != nil || got != "librarian" {
		t.Errorf("resolveSlug(\"\") with env=librarian = %q, %v; want librarian, nil", got, err)
	}

	// An executor process cannot claim librarian via the model-supplied arg.
	t.Setenv("WUPHF_AGENT_SLUG", "executor")
	if got, err := resolveSlug("librarian"); err == nil {
		t.Errorf("executor claiming my_slug=librarian must be rejected; got %q, nil", got)
	}
}
