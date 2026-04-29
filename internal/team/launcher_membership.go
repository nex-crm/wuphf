package team

// launcher_membership.go owns the office membership snapshot helpers
// (PLAN.md §C14). officeMembersSnapshot is the canonical roster
// builder used by both targeter wiring and prompt construction;
// agentConfigFromMember is the pure transform from the broker member
// shape to the agent.AgentConfig shape; agentActiveTask resolves the
// "what is this slug currently working on?" task; PackName /
// AgentCount / activeSessionMembers / officeMemberBySlug are
// straightforward accessors used by the channel TUI and tests.

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/company"
)

// brokerMemberProviderKind reads the per-member provider override from the
// broker, or "" when no broker is wired or no override is set.
func (l *Launcher) brokerMemberProviderKind(slug string) string {
	if l == nil || l.broker == nil {
		return ""
	}
	return l.broker.MemberProviderKind(slug)
}

// agentActiveTask returns the first in_progress task owned by the given agent slug.
// AllTasks() is used so agents working in non-general channels still get their
// worktree set up correctly.
func (l *Launcher) agentActiveTask(slug string) *teamTask {
	if l.broker == nil {
		return nil
	}
	tasks := l.broker.AllTasks()
	for i := range tasks {
		if tasks[i].Owner == slug && tasks[i].Status == "in_progress" {
			return &tasks[i]
		}
	}
	return nil
}

func (l *Launcher) officeMembersSnapshot() []officeMember {
	mergePackMembers := func(members []officeMember) []officeMember {
		if l == nil || l.pack == nil || len(l.pack.Agents) == 0 {
			return members
		}
		bySlug := make(map[string]struct{}, len(members))
		for _, member := range members {
			bySlug[member.Slug] = struct{}{}
		}
		for _, cfg := range l.pack.Agents {
			if _, ok := bySlug[cfg.Slug]; ok {
				continue
			}
			member := officeMember{
				Slug:           cfg.Slug,
				Name:           cfg.Name,
				Role:           cfg.Name,
				Expertise:      append([]string(nil), cfg.Expertise...),
				Personality:    cfg.Personality,
				PermissionMode: cfg.PermissionMode,
				AllowedTools:   append([]string(nil), cfg.AllowedTools...),
				BuiltIn:        cfg.Slug == l.pack.LeadSlug || cfg.Slug == "ceo",
			}
			applyOfficeMemberDefaults(&member)
			members = append(members, member)
		}
		return members
	}
	if l.broker != nil {
		if members := l.broker.OfficeMembers(); len(members) > 0 {
			return mergePackMembers(members)
		}
	}
	path := defaultBrokerStatePath()
	data, err := os.ReadFile(path)
	if err == nil {
		var state brokerState
		if json.Unmarshal(data, &state) == nil && len(state.Members) > 0 {
			for i := range state.Members {
				applyOfficeMemberDefaults(&state.Members[i])
			}
			return state.Members
		}
	}
	if l.pack != nil && len(l.pack.Agents) > 0 {
		members := make([]officeMember, 0, len(l.pack.Agents))
		for _, cfg := range l.pack.Agents {
			member := officeMember{
				Slug:           cfg.Slug,
				Name:           cfg.Name,
				Role:           cfg.Name,
				Expertise:      append([]string(nil), cfg.Expertise...),
				Personality:    cfg.Personality,
				PermissionMode: cfg.PermissionMode,
				AllowedTools:   append([]string(nil), cfg.AllowedTools...),
				BuiltIn:        cfg.Slug == l.pack.LeadSlug || cfg.Slug == "ceo",
			}
			applyOfficeMemberDefaults(&member)
			members = append(members, member)
		}
		return mergePackMembers(members)
	}
	if manifest, err := company.LoadRuntimeManifest(resolveRepoRoot(l.cwd)); err == nil && len(manifest.Members) > 0 {
		members := make([]officeMember, 0, len(manifest.Members))
		for _, cfg := range manifest.Members {
			member := officeMember{
				Slug:           cfg.Slug,
				Name:           cfg.Name,
				Role:           cfg.Role,
				Expertise:      append([]string(nil), cfg.Expertise...),
				Personality:    cfg.Personality,
				PermissionMode: cfg.PermissionMode,
				AllowedTools:   append([]string(nil), cfg.AllowedTools...),
				BuiltIn:        cfg.System,
			}
			applyOfficeMemberDefaults(&member)
			members = append(members, member)
		}
		return mergePackMembers(members)
	}
	return mergePackMembers(defaultOfficeMembers())
}

func (l *Launcher) isFocusModeEnabled() bool {
	if l != nil && l.broker != nil {
		return l.broker.FocusModeEnabled()
	}
	if l == nil {
		return false
	}
	return l.focusMode
}

// officeMemberBySlug / officeLeadSlug / activeSessionMembers / getAgentName
// live on officeTargeter (PLAN.md §C2); thin wrappers keep current callers
// working without a rename sweep.
func (l *Launcher) officeMemberBySlug(slug string) officeMember {
	return l.targeter().MemberBySlug(slug)
}

func agentConfigFromMember(member officeMember) agent.AgentConfig {
	cfg := agent.AgentConfig{
		Slug:           member.Slug,
		Name:           member.Name,
		Expertise:      append([]string(nil), member.Expertise...),
		Personality:    member.Personality,
		PermissionMode: member.PermissionMode,
		AllowedTools:   append([]string(nil), member.AllowedTools...),
	}
	if cfg.Name == "" {
		cfg.Name = humanizeSlug(member.Slug)
	}
	if len(cfg.Expertise) == 0 {
		cfg.Expertise = inferOfficeExpertise(member.Slug, member.Role)
	}
	if cfg.Personality == "" {
		cfg.Personality = inferOfficePersonality(member.Slug, member.Role)
	}
	return cfg
}

func (l *Launcher) activeSessionMembers() []officeMember {
	return l.targeter().ActiveSessionMembers()
}

// PackName returns the display name of the pack.
func (l *Launcher) PackName() string {
	if l.isOneOnOne() {
		return "1:1 with " + l.targeter().NameFor(l.oneOnOneAgent())
	}
	return "WUPHF Office"
}

// AgentCount returns the number of agents in the pack.
func (l *Launcher) AgentCount() int {
	if l.isOneOnOne() {
		return 1
	}
	return len(l.officeMembersSnapshot())
}

// filterEnv returns env with the given key removed.
func filterEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return out
}
