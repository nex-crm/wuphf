package orchestration

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// atMentionPattern matches @slug patterns in messages.
var atMentionPattern = regexp.MustCompile(`@(\S+)`)

// AgentInfo describes an available agent for message routing.
type AgentInfo struct {
	Slug      string
	Expertise []string
}

// MessageRoutingResult is the output of a Route call.
type MessageRoutingResult struct {
	Primary       string // agent slug
	Collaborators []string
	IsFollowUp    bool
	TeamLeadAware bool
}

type threadContext struct {
	agentSlug    string
	lastActivity time.Time
}

// MessageRouter routes free-text messages to the most appropriate agent.
type MessageRouter struct {
	router         *TaskRouter
	recentThreads  map[string]*threadContext
	followUpWindow time.Duration
	teamLeadSlug   string
	mu             sync.Mutex
}

// NewMessageRouter returns a MessageRouter with a 30s follow-up window.
func NewMessageRouter() *MessageRouter {
	return &MessageRouter{
		router:         NewTaskRouter(),
		recentThreads:  make(map[string]*threadContext),
		followUpWindow: 30 * time.Second,
	}
}

// SetTeamLeadSlug configures which agent slug acts as the team lead.
func (m *MessageRouter) SetTeamLeadSlug(slug string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teamLeadSlug = slug
}

// getTeamLeadSlug returns the configured team-lead slug, defaulting to "team-lead".
// Caller must hold m.mu.
func (m *MessageRouter) getTeamLeadSlug() string {
	if m.teamLeadSlug != "" {
		return m.teamLeadSlug
	}
	return "team-lead"
}

// RegisterAgent registers an agent's expertise with the underlying TaskRouter.
func (m *MessageRouter) RegisterAgent(slug string, expertise []string) {
	skills := make([]SkillDeclaration, len(expertise))
	for i, e := range expertise {
		skills[i] = SkillDeclaration{Name: e, Description: e, Proficiency: 1.0}
	}
	m.router.RegisterAgent(slug, skills)
}

// UnregisterAgent removes an agent from the message router.
func (m *MessageRouter) UnregisterAgent(slug string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.router.UnregisterAgent(slug)
	delete(m.recentThreads, slug)
}

// RecordAgentActivity marks an agent as recently active.
func (m *MessageRouter) RecordAgentActivity(agentSlug string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tc, ok := m.recentThreads[agentSlug]; ok {
		tc.lastActivity = time.Now()
	} else {
		m.recentThreads[agentSlug] = &threadContext{
			agentSlug:    agentSlug,
			lastActivity: time.Now(),
		}
	}
}

// Route decides which agent(s) should handle a message.
func (m *MessageRouter) Route(message string, availableAgents []AgentInfo) MessageRoutingResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Register all available agents so the task router can score them.
	for _, a := range availableAgents {
		skills := make([]SkillDeclaration, len(a.Expertise))
		for i, e := range a.Expertise {
			skills[i] = SkillDeclaration{Name: e, Description: e, Proficiency: 1.0}
		}
		m.router.RegisterAgent(a.Slug, skills)
	}

	result := MessageRoutingResult{}

	teamLead := m.getTeamLeadSlug()

	// 1. Check for explicit @slug mention — highest priority, outranks follow-up.
	if slug := m.detectAtMention(message, availableAgents); slug != "" {
		result.Primary = slug
		result.TeamLeadAware = slug == teamLead
		return result
	}

	// 2. Check follow-up — route to the recently active agent.
	if followUpSlug := m.detectFollowUp(message); followUpSlug != "" {
		result.Primary = followUpSlug
		result.IsFollowUp = true
		result.TeamLeadAware = true
		return result
	}

	// 3. New directive: always route to team-lead first per spec.
	// Still populate collaborators for informational purposes.
	result.Primary = teamLead
	result.TeamLeadAware = true

	skills := m.ExtractSkills(message)
	if len(skills) > 0 {
		task := TaskDefinition{
			ID:             "msg-route",
			RequiredSkills: skills,
		}
		capable := m.router.FindCapableAgents(task)
		for _, rr := range capable {
			if rr.AgentSlug != teamLead && rr.Score >= 0.25 {
				result.Collaborators = append(result.Collaborators, rr.AgentSlug)
			}
		}
	}

	return result
}

var followUpPattern = regexp.MustCompile(
	`(?i)^(also|and |too |that |it |the results|those |these |this |what about|how about|can you also)`,
)

// detectFollowUp returns the most recently active agent slug if the message
// looks like a follow-up and was within the follow-up window.
func (m *MessageRouter) detectFollowUp(message string) string {
	if !followUpPattern.MatchString(strings.TrimSpace(message)) {
		return ""
	}
	var best *threadContext
	for _, tc := range m.recentThreads {
		if time.Since(tc.lastActivity) <= m.followUpWindow {
			if best == nil || tc.lastActivity.After(best.lastActivity) {
				best = tc
			}
		}
	}
	if best != nil {
		return best.agentSlug
	}
	return ""
}

// skillKeywords maps message keywords to skill names.
var skillKeywords = []struct {
	pattern *regexp.Regexp
	skills  []string
}{
	{regexp.MustCompile(`(?i)landing page|frontend|front-end|ui|ux|hero|cta|design`), []string{"frontend", "UI-UX", "components"}},
	{regexp.MustCompile(`(?i)backend|back-end|api|endpoint|server|database`), []string{"backend", "APIs", "databases"}},
	{regexp.MustCompile(`(?i)positioning|messaging|brand|marketing|copy|launch`), []string{"positioning", "messaging", "go-to-market"}},
	{regexp.MustCompile(`(?i)brief|spec|requirements|roadmap|plan`), []string{"requirements", "roadmap", "planning"}},
	{regexp.MustCompile(`(?i)research|investigate|analyze`), []string{"market-research", "competitive-analysis"}},
	{regexp.MustCompile(`(?i)leads|prospect|outreach`), []string{"prospecting", "outreach"}},
	{regexp.MustCompile(`(?i)enrich|validate|data quality`), []string{"data-enrichment", "validation"}},
	{regexp.MustCompile(`(?i)seo|keyword|ranking|content`), []string{"seo", "content-analysis"}},
	{regexp.MustCompile(`(?i)customer|success|health|renewal`), []string{"relationship-management", "health-scoring"}},
	{regexp.MustCompile(`(?i)code|bug|fix|implement`), []string{"general", "planning"}},
}

// detectAtMention returns the slug of an explicitly @mentioned agent, if any.
// Caller must hold m.mu.
func (m *MessageRouter) detectAtMention(message string, agents []AgentInfo) string {
	matches := atMentionPattern.FindAllStringSubmatch(message, -1)
	if len(matches) == 0 {
		return ""
	}
	known := make(map[string]bool, len(agents))
	for _, a := range agents {
		known[a.Slug] = true
	}
	for _, match := range matches {
		slug := match[1]
		if known[slug] {
			return slug
		}
	}
	return ""
}

// ExtractSkills returns a deduplicated list of skills inferred from the message.
func (m *MessageRouter) ExtractSkills(message string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, kw := range skillKeywords {
		if kw.pattern.MatchString(message) {
			for _, s := range kw.skills {
				if !seen[s] {
					seen[s] = true
					out = append(out, s)
				}
			}
		}
	}
	return out
}
