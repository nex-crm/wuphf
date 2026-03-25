package e2e

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/orchestration"
)

// fakeResolver returns a StreamFnResolver that maps agent slugs to canned responses.
func fakeResolver(responses map[string]string) agent.StreamFnResolver {
	return func(slug string) agent.StreamFn {
		return func(msgs []agent.Message, tools []agent.AgentTool) <-chan agent.StreamChunk {
			ch := make(chan agent.StreamChunk, 2)
			go func() {
				defer close(ch)
				resp := responses[slug]
				if resp == "" {
					resp = "done"
				}
				ch <- agent.StreamChunk{Type: "text", Content: resp}
			}()
			return ch
		}
	}
}

// fakeErrorResolver returns a StreamFnResolver where every agent gets an error chunk.
func fakeErrorResolver(errMsg string) agent.StreamFnResolver {
	return func(slug string) agent.StreamFn {
		return func(msgs []agent.Message, tools []agent.AgentTool) <-chan agent.StreamChunk {
			ch := make(chan agent.StreamChunk, 1)
			go func() {
				defer close(ch)
				ch <- agent.StreamChunk{Type: "error", Content: errMsg}
			}()
			return ch
		}
	}
}

// newTestAgentService creates an AgentService with fake resolver and temp session store,
// bootstraps agents from the founding-team pack, and returns the service + pack.
func newTestAgentService(t *testing.T, resolver agent.StreamFnResolver) (*agent.AgentService, *agent.PackDefinition) {
	t.Helper()
	dir := t.TempDir()
	sessions := agent.NewSessionStoreAt(dir)

	svc := agent.NewAgentService(
		agent.WithStreamFnResolver(resolver),
		agent.WithSessionStore(sessions),
	)

	pack := agent.GetPack("founding-team")
	if pack == nil {
		t.Fatal("founding-team pack not found")
	}

	for _, cfg := range pack.Agents {
		if _, err := svc.Create(cfg); err != nil {
			t.Fatalf("create agent %s: %v", cfg.Slug, err)
		}
		if err := svc.Start(cfg.Slug); err != nil {
			t.Fatalf("start agent %s: %v", cfg.Slug, err)
		}
	}

	return svc, pack
}

// --- Runtime-flow tests ---

func TestFullDelegationFlow(t *testing.T) {
	teamLeadResponse := "I'll have @fe build the UI and @be build the API."
	responses := map[string]string{
		"ceo": teamLeadResponse,
		"fe":  "UI built successfully.",
		"be":  "API endpoints created.",
	}

	svc, pack := newTestAgentService(t, fakeResolver(responses))
	delegator := orchestration.NewDelegator(3)

	// Steer team-lead (ceo) with a directive.
	if err := svc.Steer("ceo", "Build a landing page with API"); err != nil {
		t.Fatalf("steer ceo: %v", err)
	}

	// Tick the CEO agent through its full cycle: idle → build_context → stream_llm → done.
	ceoAgent, ok := svc.Get("ceo")
	if !ok {
		t.Fatal("ceo agent not found")
	}

	// We also need a follow-up so the loop has user content to process.
	if err := svc.FollowUp("ceo", "Build a landing page with API"); err != nil {
		t.Fatalf("follow-up ceo: %v", err)
	}

	// Tick until done or error (max 10 ticks to avoid infinite loop).
	for i := 0; i < 10; i++ {
		if err := ceoAgent.Loop.Tick(); err != nil {
			t.Fatalf("ceo tick %d: %v", i, err)
		}
		state := ceoAgent.Loop.GetState()
		if state.Phase == agent.PhaseDone || state.Phase == agent.PhaseError {
			break
		}
	}

	ceoState := ceoAgent.Loop.GetState()
	if ceoState.Phase != agent.PhaseDone {
		t.Fatalf("expected ceo phase done, got %s (error: %s)", ceoState.Phase, ceoState.Error)
	}

	// Extract delegations from the team-lead response.
	knownSlugs := make([]string, 0, len(pack.Agents))
	for _, cfg := range pack.Agents {
		if cfg.Slug != pack.LeadSlug {
			knownSlugs = append(knownSlugs, cfg.Slug)
		}
	}

	delegations := delegator.ExtractDelegations(teamLeadResponse, knownSlugs)
	if len(delegations) != 2 {
		t.Fatalf("expected 2 delegations, got %d: %+v", len(delegations), delegations)
	}

	// Verify the correct agents were mentioned.
	slugs := map[string]bool{}
	for _, d := range delegations {
		slugs[d.AgentSlug] = true
	}
	if !slugs["fe"] {
		t.Error("expected delegation to @fe")
	}
	if !slugs["be"] {
		t.Error("expected delegation to @be")
	}

	// Steer specialists with delegation messages and tick them to completion.
	for _, d := range delegations {
		msg := orchestration.FormatSteerMessage(d)
		if err := svc.Steer(d.AgentSlug, msg); err != nil {
			t.Errorf("steer %s: %v", d.AgentSlug, err)
		}
		if err := svc.FollowUp(d.AgentSlug, d.Task); err != nil {
			t.Errorf("follow-up %s: %v", d.AgentSlug, err)
		}

		ma, ok := svc.Get(d.AgentSlug)
		if !ok {
			t.Errorf("agent %s not found", d.AgentSlug)
			continue
		}

		for i := 0; i < 10; i++ {
			if err := ma.Loop.Tick(); err != nil {
				t.Fatalf("%s tick %d: %v", d.AgentSlug, i, err)
			}
			state := ma.Loop.GetState()
			if state.Phase == agent.PhaseDone || state.Phase == agent.PhaseError {
				break
			}
		}

		state := ma.Loop.GetState()
		if state.Phase != agent.PhaseDone {
			t.Errorf("expected %s phase done, got %s (error: %s)", d.AgentSlug, state.Phase, state.Error)
		}
	}
}

func TestProviderErrorSurfaces(t *testing.T) {
	svc, _ := newTestAgentService(t, fakeErrorResolver("provider failed"))

	// Give the agent something to process.
	if err := svc.FollowUp("ceo", "do something"); err != nil {
		t.Fatalf("follow-up: %v", err)
	}

	ma, ok := svc.Get("ceo")
	if !ok {
		t.Fatal("ceo agent not found")
	}

	// Tick until error or max ticks.
	var lastErr error
	for i := 0; i < 10; i++ {
		lastErr = ma.Loop.Tick()
		state := ma.Loop.GetState()
		if state.Phase == agent.PhaseError {
			break
		}
	}

	state := ma.Loop.GetState()
	if state.Phase != agent.PhaseError {
		t.Fatalf("expected phase error, got %s", state.Phase)
	}
	if state.Error != "provider failed" {
		t.Fatalf("expected error 'provider failed', got %q", state.Error)
	}
	if lastErr == nil {
		t.Fatal("expected Tick to return an error on provider failure")
	}
}

func TestTeamLeadFirstRouting(t *testing.T) {
	router := orchestration.NewMessageRouter()

	agents := []orchestration.AgentInfo{
		{Slug: "ceo", Expertise: []string{"strategy", "delegation"}},
		{Slug: "fe", Expertise: []string{"frontend", "React", "CSS"}},
		{Slug: "be", Expertise: []string{"backend", "APIs", "databases"}},
	}
	for _, a := range agents {
		router.RegisterAgent(a.Slug, a.Expertise)
	}
	router.SetTeamLeadSlug("ceo")

	// A generic directive should route to team-lead first.
	result := router.Route("Build a new dashboard for analytics", agents)
	if result.Primary != "ceo" {
		t.Fatalf("expected primary=ceo, got %s", result.Primary)
	}
	if !result.TeamLeadAware {
		t.Error("expected TeamLeadAware=true for team-lead routing")
	}

	// An explicit @fe mention should route directly to fe.
	result = router.Route("@fe fix the button styles", agents)
	if result.Primary != "fe" {
		t.Fatalf("expected primary=fe for @mention, got %s", result.Primary)
	}
}

func TestConcurrencyEnforced(t *testing.T) {
	delegator := orchestration.NewDelegator(1)

	response := "I need @fe to build the UI, @be to create the API, and @pm to write specs."
	knownSlugs := []string{"fe", "be", "pm"}

	delegations := delegator.ExtractDelegations(response, knownSlugs)
	if len(delegations) != 3 {
		t.Fatalf("expected 3 delegations, got %d", len(delegations))
	}

	immediate, queued := delegator.ApplyLimit(delegations)
	if len(immediate) != 1 {
		t.Fatalf("expected 1 immediate delegation, got %d", len(immediate))
	}
	if len(queued) != 2 {
		t.Fatalf("expected 2 queued delegations, got %d", len(queued))
	}

	// Verify the first delegation is the immediate one.
	if immediate[0].AgentSlug != delegations[0].AgentSlug {
		t.Errorf("expected immediate[0] to be %s, got %s", delegations[0].AgentSlug, immediate[0].AgentSlug)
	}
}

// --- Parser-level tests (preserved from original) ---

func TestDelegationParsing(t *testing.T) {
	svc := agent.NewAgentService()
	pack := agent.GetPack("founding-team")
	if pack == nil {
		t.Fatal("founding-team pack not found")
	}

	for _, cfg := range pack.Agents {
		_, err := svc.Create(cfg)
		if err != nil {
			t.Fatalf("failed to create agent %s: %v", cfg.Slug, err)
		}
	}

	d := orchestration.NewDelegator(3)

	response := "I'll have @fe build the landing page while @be sets up the API endpoints."
	knownSlugs := []string{"pm", "fe", "be", "designer", "cmo", "cro"}

	delegations := d.ExtractDelegations(response, knownSlugs)
	if len(delegations) != 2 {
		t.Fatalf("expected 2 delegations, got %d", len(delegations))
	}

	for _, del := range delegations {
		msg := orchestration.FormatSteerMessage(del)
		err := svc.Steer(del.AgentSlug, msg)
		if err != nil {
			t.Errorf("failed to steer %s: %v", del.AgentSlug, err)
		}
	}
}

func TestDelegationFlowNoDelegation(t *testing.T) {
	d := orchestration.NewDelegator(3)

	response := "I'll think about this and get back to you with a plan."
	knownSlugs := []string{"pm", "fe", "be", "designer", "cmo", "cro"}

	delegations := d.ExtractDelegations(response, knownSlugs)
	if len(delegations) != 0 {
		t.Errorf("expected 0 delegations, got %d", len(delegations))
	}
}

func TestPackBootstrap(t *testing.T) {
	pack := agent.GetPack("founding-team")
	if pack == nil {
		t.Fatal("founding-team pack not found")
	}

	svc := agent.NewAgentService()
	for _, cfg := range pack.Agents {
		_, err := svc.Create(cfg)
		if err != nil {
			t.Fatalf("failed to create agent %s: %v", cfg.Slug, err)
		}
	}

	for _, cfg := range pack.Agents {
		ma, ok := svc.Get(cfg.Slug)
		if !ok {
			t.Errorf("agent %s not found in service", cfg.Slug)
			continue
		}
		if ma.Config.Slug != cfg.Slug {
			t.Errorf("expected slug %s, got %s", cfg.Slug, ma.Config.Slug)
		}
	}

	list := svc.List()
	if len(list) != 8 {
		t.Errorf("expected 8 agents from List(), got %d", len(list))
	}
}
