package tui

import (
	"io"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/orchestration"
	"github.com/nex-crm/wuphf/internal/provider"
)

// Runtime owns the live agent infrastructure and can rebuild it when config changes.
type Runtime struct {
	AgentService  *agent.AgentService
	MessageRouter *orchestration.MessageRouter
	Delegator     *orchestration.Delegator
	TeamLeadSlug  string
	PackSlug      string
	Events        chan tea.Msg
}

// NewRuntime creates a Runtime and bootstraps agents from the current config.
func NewRuntime(events chan tea.Msg) *Runtime {
	rt := &Runtime{
		Events: events,
	}
	rt.BootstrapFromConfig()
	return rt
}

// BootstrapFromConfig loads config, creates the agent service, message router,
// delegator, and populates agents from the configured pack.
func (rt *Runtime) BootstrapFromConfig() {
	cfg, _ := config.Load()

	apiKey := config.ResolveAPIKey("")
	apiClient := api.NewClient(apiKey)
	streamResolver := provider.DefaultStreamFnResolver(apiClient)
	agentSvc := agent.NewAgentService(
		agent.WithStreamFnResolver(streamResolver),
		agent.WithClient(apiClient),
	)
	msgRouter := orchestration.NewMessageRouter()

	packSlug := cfg.Pack
	if packSlug == "" {
		packSlug = "founding-team"
	}

	teamLeadSlug := cfg.TeamLeadSlug

	// Bootstrap agents from pack definition
	pack := agent.GetPack(packSlug)
	if pack != nil {
		teamLeadSlug = pack.LeadSlug
		for _, agentCfg := range pack.Agents {
			enriched := agentCfg
			if agentCfg.Slug == pack.LeadSlug {
				enriched.Personality = agent.BuildTeamLeadPrompt(agentCfg, pack.Agents, pack.Name)
			} else {
				enriched.Personality = agent.BuildSpecialistPrompt(agentCfg)
			}
			if _, err := agentSvc.Create(enriched); err == nil {
				_ = agentSvc.Start(agentCfg.Slug)
			}
			msgRouter.RegisterAgent(agentCfg.Slug, agentCfg.Expertise)
		}
	} else {
		// Fallback: create single team-lead
		teamLeadSlug = "team-lead"
		if _, err := agentSvc.CreateFromTemplate("team-lead", "team-lead"); err == nil {
			_ = agentSvc.Start("team-lead")
		}
		if tmpl, ok := agentSvc.GetTemplate("team-lead"); ok {
			msgRouter.RegisterAgent("team-lead", tmpl.Expertise)
		}
	}
	msgRouter.SetTeamLeadSlug(teamLeadSlug)

	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	delegator := orchestration.NewDelegator(maxConcurrent)

	rt.AgentService = agentSvc
	rt.MessageRouter = msgRouter
	rt.Delegator = delegator
	rt.TeamLeadSlug = teamLeadSlug
	rt.PackSlug = packSlug
}

// Reconfigure tears down all running agents and rebuilds from current config.
func (rt *Runtime) Reconfigure() {
	// Stop and remove all existing agents.
	if rt.AgentService != nil {
		for _, ma := range rt.AgentService.List() {
			_ = rt.AgentService.Remove(ma.Config.Slug)
		}
	}
	rt.BootstrapFromConfig()
}

// HasClaude reports whether the claude binary is available in PATH.
func HasClaude() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

// BootstrapTmuxChannel creates a TmuxManager, spawns all agents in tmux windows,
// and wires them to a GossipBus and ChannelAdapter. Returns the components for
// the model to use.
func (rt *Runtime) BootstrapTmuxChannel() (*TmuxManager, *GossipBus, *ChannelAdapter, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, nil, err
	}

	tm := NewTmuxManager("wuphf-agents")
	if err := tm.CreateSession(); err != nil {
		return nil, nil, nil, err
	}

	bus := NewGossipBus(rt.TeamLeadSlug)
	adapter := NewChannelAdapter()

	pack := agent.GetPack(rt.PackSlug)
	if pack == nil {
		return tm, bus, adapter, nil
	}

	for _, agentCfg := range pack.Agents {
		adapter.SetAgentName(agentCfg.Slug, agentCfg.Name)

		var sysPrompt string
		if agentCfg.Slug == pack.LeadSlug {
			sysPrompt = agent.BuildTeamLeadPrompt(agentCfg, pack.Agents, pack.Name)
		} else {
			sysPrompt = agent.BuildSpecialistPrompt(agentCfg)
		}

		args := []string{
			"-p", sysPrompt,
			"--output-format", "stream-json",
			"--verbose",
			"--max-turns", "50",
			"--no-session-persistence",
		}

		if err := tm.SpawnAgent(agentCfg.Slug, "claude", args, []string{"CWD=" + cwd}); err != nil {
			continue // non-fatal
		}
		reader, err := tm.AttachObserverPipe(agentCfg.Slug)
		if err != nil {
			continue
		}
		NewOutputObserver(agentCfg.Slug, bus, reader).Start()
	}

	return tm, bus, adapter, nil
}

// BootstrapPanes creates TerminalPanes for each agent in the pack,
// spawns claude processes, and wires them to the GossipBus.
// Returns panes in order (leader first), the bus, and cwd used.
func (rt *Runtime) BootstrapPanes(pm *PaneManager, bus *GossipBus, w, h int) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	pack := agent.GetPack(rt.PackSlug)
	if pack == nil {
		return nil
	}

	// Leader pane first, then specialists.
	ordered := make([]agent.AgentConfig, 0, len(pack.Agents))
	for _, a := range pack.Agents {
		if a.Slug == pack.LeadSlug {
			ordered = append([]agent.AgentConfig{a}, ordered...)
		} else {
			ordered = append(ordered, a)
		}
	}

	for _, agentCfg := range ordered {
		// Build system prompt.
		var sysPrompt string
		if agentCfg.Slug == pack.LeadSlug {
			sysPrompt = agent.BuildTeamLeadPrompt(agentCfg, pack.Agents, pack.Name)
		} else {
			sysPrompt = agent.BuildSpecialistPrompt(agentCfg)
		}

		pane := NewTerminalPane(agentCfg.Slug, agentCfg.Name, w, h)

		// Set up observer pipe: PTY output -> pipe writer -> observer reader -> GossipBus.
		pr, pw := io.Pipe()
		pane.SetObserverWriter(pw)
		obs := NewOutputObserver(agentCfg.Slug, bus, pr)
		obs.Start()

		// Register pane with bus and manager.
		bus.RegisterTarget(pane)
		pm.AddPane(pane)

		// Spawn claude process.
		args := []string{
			"-p", sysPrompt,
			"--output-format", "stream-json",
			"--verbose",
			"--max-turns", "50",
		}
		if err := pane.Spawn("claude", args, nil, cwd); err != nil {
			// Non-fatal: pane will show as dead.
			continue
		}
	}

	// Focus leader pane.
	pm.FocusPane(rt.TeamLeadSlug)

	return nil
}
