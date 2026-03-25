package orchestration

import "sync"

type agentUsage struct {
	TokensUsed int
	CostUsd    float64
}

// GlobalUsage aggregates usage across all agents.
type GlobalUsage struct {
	Tokens        int
	Cost          float64
	PercentTokens float64
	PercentCost   float64
}

// BudgetTracker records token and cost usage per agent against a global budget.
type BudgetTracker struct {
	globalBudget BudgetLimit
	usage        map[string]*agentUsage
	mu           sync.Mutex
}

// NewBudgetTracker returns a BudgetTracker enforcing globalBudget.
func NewBudgetTracker(globalBudget BudgetLimit) *BudgetTracker {
	return &BudgetTracker{
		globalBudget: globalBudget,
		usage:        make(map[string]*agentUsage),
	}
}

// Record adds tokens and cost to an agent's running totals.
func (b *BudgetTracker) Record(agentSlug string, tokens int, costUsd float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.getOrCreate(agentSlug)
	u.TokensUsed += tokens
	u.CostUsd += costUsd
}

// GetSnapshot returns the current budget state for agentSlug.
func (b *BudgetTracker) GetSnapshot(agentSlug string) BudgetSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	u := b.getOrCreate(agentSlug)
	snap := BudgetSnapshot{
		AgentSlug:   agentSlug,
		TokensUsed:  u.TokensUsed,
		CostUsd:     u.CostUsd,
		BudgetLimit: b.globalBudget,
	}

	if b.globalBudget.MaxTokens > 0 {
		snap.PercentUsed = float64(u.TokensUsed) / float64(b.globalBudget.MaxTokens)
	} else if b.globalBudget.MaxCostUsd > 0 {
		snap.PercentUsed = u.CostUsd / b.globalBudget.MaxCostUsd
	}

	snap.Warning = snap.PercentUsed > 0.8
	snap.Exceeded = snap.PercentUsed > 1.0
	return snap
}

// GetAllSnapshots returns snapshots for every tracked agent.
func (b *BudgetTracker) GetAllSnapshots() []BudgetSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	snaps := make([]BudgetSnapshot, 0, len(b.usage))
	for slug, u := range b.usage {
		snap := BudgetSnapshot{
			AgentSlug:   slug,
			TokensUsed:  u.TokensUsed,
			CostUsd:     u.CostUsd,
			BudgetLimit: b.globalBudget,
		}
		if b.globalBudget.MaxTokens > 0 {
			snap.PercentUsed = float64(u.TokensUsed) / float64(b.globalBudget.MaxTokens)
		} else if b.globalBudget.MaxCostUsd > 0 {
			snap.PercentUsed = u.CostUsd / b.globalBudget.MaxCostUsd
		}
		snap.Warning = snap.PercentUsed > 0.8
		snap.Exceeded = snap.PercentUsed > 1.0
		snaps = append(snaps, snap)
	}
	return snaps
}

// CanProceed returns true when the agent has not exceeded its budget.
func (b *BudgetTracker) CanProceed(agentSlug string) bool {
	return !b.GetSnapshot(agentSlug).Exceeded
}

// IsWarning returns true when the agent is above the 80% warning threshold.
func (b *BudgetTracker) IsWarning(agentSlug string) bool {
	return b.GetSnapshot(agentSlug).Warning
}

// Reset clears usage data for the given agent.
func (b *BudgetTracker) Reset(agentSlug string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.usage, agentSlug)
}

// GetGlobalUsage returns the aggregate across all agents.
func (b *BudgetTracker) GetGlobalUsage() GlobalUsage {
	b.mu.Lock()
	defer b.mu.Unlock()

	g := GlobalUsage{}
	for _, u := range b.usage {
		g.Tokens += u.TokensUsed
		g.Cost += u.CostUsd
	}
	if b.globalBudget.MaxTokens > 0 {
		g.PercentTokens = float64(g.Tokens) / float64(b.globalBudget.MaxTokens)
	}
	if b.globalBudget.MaxCostUsd > 0 {
		g.PercentCost = g.Cost / b.globalBudget.MaxCostUsd
	}
	return g
}

// getOrCreate returns the usage record for agentSlug, creating it if needed.
// Must be called with mu held.
func (b *BudgetTracker) getOrCreate(agentSlug string) *agentUsage {
	if u, ok := b.usage[agentSlug]; ok {
		return u
	}
	u := &agentUsage{}
	b.usage[agentSlug] = u
	return u
}
