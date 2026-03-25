package orchestration

import "time"

// SkillDeclaration describes one skill an agent possesses.
type SkillDeclaration struct {
	Name        string
	Description string
	Proficiency float64 // 0-1
}

// BudgetLimit defines token and cost limits.
type BudgetLimit struct {
	MaxTokens  int
	MaxCostUsd float64
}

// TaskDefinition is a single unit of work that can be assigned to an agent.
type TaskDefinition struct {
	ID             string
	Title          string
	Description    string
	RequiredSkills []string
	ParentGoalID   string
	Priority       string // "low", "medium", "high", "critical"
	Status         string // "pending", "locked", "in_progress", "completed", "failed"
	AssignedAgent  string
	Budget         *BudgetLimit
	CreatedAt      int64
	CompletedAt    int64
	Result         string
}

// GoalDefinition groups related tasks under a shared objective.
type GoalDefinition struct {
	ID          string
	Title       string
	Description string
	ProjectID   string
	Tasks       []string // task IDs
	Status      string   // "active", "completed", "paused"
	CreatedAt   int64
}

// OrchestratorConfig is the top-level configuration for the orchestration layer.
type OrchestratorConfig struct {
	MaxConcurrentAgents int
	GlobalBudget        BudgetLimit
	TaskTimeout         time.Duration
	AutoRetry           bool
	MaxRetries          int
}

// BudgetSnapshot is a point-in-time view of one agent's budget usage.
type BudgetSnapshot struct {
	AgentSlug   string
	TokensUsed  int
	CostUsd     float64
	BudgetLimit BudgetLimit
	PercentUsed float64
	Warning     bool // > 80%
	Exceeded    bool // > 100%
}
