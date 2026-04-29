package team

// office_targets.go owns the "which agent gets which pane / dispatch path"
// decision tree that used to live as a dozen methods on Launcher. The split
// (PLAN.md §C2) is justified because the logic is pure data-shape over a
// snapshot of office membership — no goroutines, no tmux, no broker writes
// — so it can be exercised by tests without a Launcher fixture.
//
// State sharing notes (PLAN.md §5 traps):
//   - paneBackedFlag is a *bool aliased to launcher.paneBackedAgents. The
//     pane-spawn path flips that bool deep inside trySpawnWebAgentPanes;
//     reading via pointer keeps the targeter in sync without callbacks.
//   - failedPaneSlugs is a shared map. Today it's read here, written by the
//     pane-spawn path (still on Launcher). No mutex — the race is dormant
//     only because trySpawnWebAgentPanes is a runtime-promotion fallback
//     that nothing currently invokes (see launcher.go:2647 / 3470). The
//     moment promotion is wired, or any concurrent reader of the targeter
//     overlaps the writer, this races. C5 (paneLifecycle) replaces this
//     with a failedPane(slug) callback and removes the shared map entirely.

import (
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/provider"
)

// officeTargeter owns office-membership-shape and routing-decision logic.
// All inputs come in via either captured-at-construction values (sessionName,
// pack, provider) or callbacks (snapshotMembers, isOneOnOne, ...). The type
// is package-internal; callers in package team get to it via Launcher.targets().
type officeTargeter struct {
	sessionName string
	pack        *agent.PackDefinition
	cwd         string
	provider    string

	// paneBackedFlag is a back-pointer to launcher.paneBackedAgents so the
	// targeter sees the live value as the spawn path flips it. Required —
	// duplicating the flag would cause every notification to route through
	// the headless path silently when panes were actually live (PLAN.md §5.2).
	paneBackedFlag *bool

	// failedPaneSlugs is the shared map of slugs whose pane spawn failed.
	// Written by the pane-spawn path on Launcher; read here. Map sharing
	// across types is a deliberate transitional state — see file header.
	failedPaneSlugs map[string]string

	// Live-state callbacks. Pulled rather than pushed so tests can stub.
	isOneOnOne         func() bool
	oneOnOneSlug       func() string
	isChannelDM        func(channelSlug string) (bool, string)
	snapshotMembers    func() []officeMember
	memberProviderKind func(slug string) string
}

// MembersSnapshot returns the current member roster. Backed by the
// snapshotMembers callback so the targeter never knows about FS reads or
// broker calls itself.
func (o *officeTargeter) MembersSnapshot() []officeMember {
	if o == nil || o.snapshotMembers == nil {
		return nil
	}
	return o.snapshotMembers()
}

// MemberBySlug walks the snapshot and returns the matching member, or a
// minimal officeMember{Slug, Name=Slug, Role=Slug} fallback so callers
// always get a well-formed value. The fallback shape matters for prompt
// generation, which interpolates Name into the system prompt header.
func (o *officeTargeter) MemberBySlug(slug string) officeMember {
	for _, m := range o.MembersSnapshot() {
		if m.Slug == slug {
			return m
		}
	}
	return officeMember{Slug: slug, Name: slug, Role: slug}
}

// LeadSlug picks the team lead. Pack-defined lead wins; otherwise fall
// back to the first BuiltIn member, then to the first member overall.
// Mirrors the previous Launcher.officeLeadSlug fallback chain so prompt
// caching keys remain stable across refactors.
func (o *officeTargeter) LeadSlug() string {
	if o == nil {
		return ""
	}
	if o.pack != nil && strings.TrimSpace(o.pack.LeadSlug) != "" {
		return o.pack.LeadSlug
	}
	return officeLeadSlugFrom(o.ActiveSessionMembers())
}

// officeLeadSlugFrom is a free function (kept package-level) so the prompt
// builder can derive the lead from an already-loaded member snapshot
// without a redundant snapshotMembers call.
func officeLeadSlugFrom(members []officeMember) string {
	for _, member := range members {
		if member.Slug == "ceo" {
			return "ceo"
		}
	}
	for _, member := range members {
		if member.BuiltIn {
			return member.Slug
		}
	}
	if len(members) > 0 {
		return members[0].Slug
	}
	return ""
}

// ActiveSessionMembers returns the current member list ordered by pack
// position, with broker-only (wizard-hired) members appended at the end.
// Pack-order matters because the UI/pane layout uses this list directly:
// reordering would shuffle pane indices on every Launch.
func (o *officeTargeter) ActiveSessionMembers() []officeMember {
	members := o.MembersSnapshot()
	if o == nil || o.pack == nil || len(o.pack.Agents) == 0 {
		return members
	}
	bySlug := make(map[string]officeMember, len(members))
	for _, member := range members {
		bySlug[member.Slug] = member
	}
	filtered := make([]officeMember, 0, len(members))
	seen := make(map[string]struct{}, len(members))
	for _, cfg := range o.pack.Agents {
		if member, ok := bySlug[cfg.Slug]; ok {
			filtered = append(filtered, member)
			seen[cfg.Slug] = struct{}{}
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
			BuiltIn:        cfg.Slug == o.pack.LeadSlug || cfg.Slug == "ceo",
		}
		applyOfficeMemberDefaults(&member)
		filtered = append(filtered, member)
		seen[cfg.Slug] = struct{}{}
	}
	for _, member := range members {
		if _, ok := seen[member.Slug]; ok {
			continue
		}
		filtered = append(filtered, member)
	}
	if len(filtered) > 0 {
		return filtered
	}
	return members
}

// NameFor returns the display name for a slug; falls back to the slug
// itself when no member or an unnamed member matches.
func (o *officeTargeter) NameFor(slug string) string {
	if member := o.MemberBySlug(slug); member.Name != "" {
		return member.Name
	}
	return slug
}

// AgentOrder returns the member roster with the lead slug always first.
// Used by pane spawn to guarantee the CEO pane lands at index 1.
func (o *officeTargeter) AgentOrder() []officeMember {
	var agentOrder []officeMember
	lead := o.LeadSlug()
	for _, member := range o.MembersSnapshot() {
		if member.Slug == lead {
			agentOrder = append([]officeMember{member}, agentOrder...)
		}
	}
	for _, member := range o.MembersSnapshot() {
		if member.Slug != lead {
			agentOrder = append(agentOrder, member)
		}
	}
	return agentOrder
}

// PaneSlugs returns the slug list in spawn order. In 1:1 mode it returns
// just the active 1:1 agent.
func (o *officeTargeter) PaneSlugs() []string {
	if o.isOneOnOne != nil && o.isOneOnOne() {
		return []string{o.oneOnOneSlug()}
	}
	members := o.MembersSnapshot()
	lead := o.LeadSlug()
	var slugs []string
	if lead != "" {
		slugs = append(slugs, lead)
	}
	for _, member := range members {
		if member.Slug == lead {
			continue
		}
		slugs = append(slugs, member.Slug)
	}
	return slugs
}

// VisibleMembers returns the agents that occupy the visible tmux pane grid
// (lead first, then up to maxVisibleOfficeAgents-1 others). 1:1 mode
// collapses to a single member.
func (o *officeTargeter) VisibleMembers() []officeMember {
	if o.isOneOnOne != nil && o.isOneOnOne() {
		return []officeMember{o.MemberBySlug(o.oneOnOneSlug())}
	}
	ordered := o.PaneEligibleMembers()
	if len(ordered) <= maxVisibleOfficeAgents {
		return ordered
	}
	return ordered[:maxVisibleOfficeAgents]
}

// OverflowMembers returns agents beyond the visible grid; each gets its
// own dedicated tmux window (named via overflowWindowName).
func (o *officeTargeter) OverflowMembers() []officeMember {
	if o.isOneOnOne != nil && o.isOneOnOne() {
		return nil
	}
	ordered := o.PaneEligibleMembers()
	if len(ordered) <= maxVisibleOfficeAgents {
		return nil
	}
	return ordered[maxVisibleOfficeAgents:]
}

// PaneEligibleMembers is AgentOrder() minus members whose runtime is not
// pane-eligible (Codex, Opencode). Filtering upstream keeps visible/overflow
// indices in sync with PaneTargets().
func (o *officeTargeter) PaneEligibleMembers() []officeMember {
	ordered := o.AgentOrder()
	filtered := make([]officeMember, 0, len(ordered))
	for _, m := range ordered {
		if o.MemberUsesHeadlessOneShotRuntime(m.Slug) {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

// PaneTargets returns slug→pane-address for every agent that should
// receive notifications by typing into a live tmux pane. Returns empty
// when panes aren't live (paneBackedFlag=false) so callers consistently
// fall through to the headless dispatcher.
func (o *officeTargeter) PaneTargets() map[string]notificationTarget {
	targets := make(map[string]notificationTarget)
	if o == nil || o.paneBackedFlag == nil || !*o.paneBackedFlag {
		return targets
	}
	if o.isOneOnOne != nil && o.isOneOnOne() {
		slug := o.oneOnOneSlug()
		if slug != "" && !o.SkipPane(slug) {
			targets[slug] = notificationTarget{
				Slug:       slug,
				PaneTarget: fmt.Sprintf("%s:team.1", o.sessionName),
			}
		}
		return targets
	}
	for i, member := range o.VisibleMembers() {
		if o.SkipPane(member.Slug) {
			continue
		}
		targets[member.Slug] = notificationTarget{
			Slug:       member.Slug,
			PaneTarget: fmt.Sprintf("%s:team.%d", o.sessionName, i+1),
		}
	}
	for _, member := range o.OverflowMembers() {
		if o.SkipPane(member.Slug) {
			continue
		}
		targets[member.Slug] = notificationTarget{
			Slug:       member.Slug,
			PaneTarget: fmt.Sprintf("%s:%s.0", o.sessionName, overflowWindowName(member.Slug)),
		}
	}
	return targets
}

// NotificationTargets layers headless fallback entries on top of the pane
// target map. An agent without a pane target — either because pane spawn
// failed or because its provider is non-pane — still gets a target entry
// (with empty PaneTarget) so dispatch can route it through the headless
// queue.
func (o *officeTargeter) NotificationTargets() map[string]notificationTarget {
	targets := o.PaneTargets()
	if o == nil {
		return targets
	}
	addHeadless := func(slug string) {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			return
		}
		if _, ok := targets[slug]; ok {
			return
		}
		if o.ShouldUseHeadlessForSlug(slug) {
			targets[slug] = notificationTarget{Slug: slug}
		}
	}
	if o.isOneOnOne != nil && o.isOneOnOne() {
		addHeadless(o.oneOnOneSlug())
		return targets
	}
	addHeadless(o.LeadSlug())
	for _, member := range o.ActiveSessionMembers() {
		addHeadless(member.Slug)
	}
	return targets
}

// ResolvePaneTarget returns the live pane address for slug, or
// ("", false) when slug isn't pane-backed (codex/opencode agents,
// failed-pane fallbacks, or empty slugs).
func (o *officeTargeter) ResolvePaneTarget(slug string) (string, bool) {
	if o == nil || strings.TrimSpace(slug) == "" {
		return "", false
	}
	targets := o.PaneTargets()
	target, ok := targets[slug]
	if !ok {
		return "", false
	}
	return target.PaneTarget, true
}

// IsChannelDM proxies to the wired channel-DM resolver. Returns false,
// "" when nothing is wired so callers don't need a nil-check.
func (o *officeTargeter) IsChannelDM(channelSlug string) (bool, string) {
	if o == nil || o.isChannelDM == nil {
		return false, ""
	}
	return o.isChannelDM(channelSlug)
}

// ShouldUseHeadless returns true when notifications must go through the
// headless `claude --print` queue rather than into a live pane. The
// install-wide answer based on provider + paneBacked state.
func (o *officeTargeter) ShouldUseHeadless() bool {
	if !o.UsesPaneRuntime() {
		return true
	}
	if o.paneBackedFlag == nil {
		return true
	}
	return !*o.paneBackedFlag
}

// ShouldUseHeadlessForSlug answers the same question per-slug. Layered on
// top of ShouldUseHeadless: an individual slug can be forced headless even
// in a pane-backed install (Codex member, failed pane spawn).
func (o *officeTargeter) ShouldUseHeadlessForSlug(slug string) bool {
	if o == nil || strings.TrimSpace(slug) == "" {
		return false
	}
	if o.ShouldUseHeadless() {
		return true
	}
	if o.MemberUsesHeadlessOneShotRuntime(slug) {
		return true
	}
	if _, failed := o.failedPaneSlugs[strings.TrimSpace(slug)]; failed {
		return true
	}
	return false
}

// ShouldUseHeadlessForTarget treats a target with an empty PaneTarget as
// implicitly headless. Used by the dispatch hot path.
func (o *officeTargeter) ShouldUseHeadlessForTarget(target notificationTarget) bool {
	if o == nil {
		return false
	}
	if o.ShouldUseHeadlessForSlug(target.Slug) {
		return true
	}
	return strings.TrimSpace(target.PaneTarget) == ""
}

// SkipPane reports whether slug should be excluded from pane spawn /
// pane-target maps. True when the slug is empty, when its pane spawn
// previously failed, or when its runtime isn't pane-eligible.
func (o *officeTargeter) SkipPane(slug string) bool {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return true
	}
	if _, bad := o.failedPaneSlugs[slug]; bad {
		return true
	}
	if o.MemberUsesHeadlessOneShotRuntime(slug) {
		return true
	}
	return false
}

// UsesPaneRuntime reports whether the install-wide provider supports
// interactive panes. Provider Registry-driven so future providers don't
// need code changes here.
func (o *officeTargeter) UsesPaneRuntime() bool {
	return provider.CapabilitiesFor(normalizeProviderKind(o.provider)).PaneEligible
}

// RequiresClaudeSessionReset reports whether the install-wide provider
// populates the Claude session store and therefore needs
// provider.ResetClaudeSessions to run on ResetSession / ReconfigureSession.
// Today only Claude Code does.
func (o *officeTargeter) RequiresClaudeSessionReset() bool {
	return provider.CapabilitiesFor(normalizeProviderKind(o.provider)).RequiresClaudeSessionReset
}

// MemberEffectiveProviderKind returns the provider kind that should run
// the given agent's next turn. Lookup order: per-member override (set via
// /agent create --provider=X or the hire-agent modal), then the
// install-wide provider.
func (o *officeTargeter) MemberEffectiveProviderKind(slug string) string {
	if o.memberProviderKind != nil {
		if kind := o.memberProviderKind(slug); kind != "" {
			return normalizeProviderKind(kind)
		}
	}
	return normalizeProviderKind(o.provider)
}

// MemberUsesHeadlessOneShotRuntime reports whether the given agent is bound
// to a non-pane-eligible runtime and therefore skips the tmux/claude pane
// infrastructure in favor of the broker-driven headless queue.
func (o *officeTargeter) MemberUsesHeadlessOneShotRuntime(slug string) bool {
	kind := o.MemberEffectiveProviderKind(slug)
	return !provider.CapabilitiesFor(kind).PaneEligible
}

// overflowWindowName is package-level so the pane-lifecycle code (which
// will move in C5) can keep calling it during the transitional state.
func overflowWindowName(slug string) string {
	return "agent-" + strings.TrimSpace(slug)
}

// normalizeProviderKind trims and canonicalizes provider kinds while
// preserving unknown values so dispatch code can surface explicit errors.
// Free function (used by tests, dispatch code, and the targeter itself).
func normalizeProviderKind(raw string) string {
	k := strings.ToLower(strings.TrimSpace(raw))
	switch k {
	case "claude", "":
		return provider.KindClaudeCode
	case "codex":
		return provider.KindCodex
	case "opencode":
		return provider.KindOpencode
	case "claude-code", "openclaw":
		return k
	default:
		return k
	}
}

const maxVisibleOfficeAgents = 5
