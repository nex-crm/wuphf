package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	CredibilityWeight   = 0.4
	RelevanceWeight     = 0.4
	FreshnessWeight     = 0.2
	AdoptThreshold      = 0.7
	TestThreshold       = 0.4
	FreshnessHalfLifeMs = float64(7 * 24 * 60 * 60 * 1000) // 7 days in milliseconds
)

// AdoptionScore holds the computed score for a gossip insight.
type AdoptionScore struct {
	SourceCredibility float64 `json:"sourceCredibility"`
	SemanticRelevance float64 `json:"semanticRelevance"`
	TemporalFreshness float64 `json:"temporalFreshness"`
	Total             float64 `json:"total"`
	Decision          string  `json:"decision"` // "adopt" | "test" | "reject"
}

// ScoreInsight computes an adoption score for the given insight.
// If sourceCredibility is nil, 0.5 is used as the default.
func ScoreInsight(insight GossipInsight, currentContext string, sourceCredibility *float64) AdoptionScore {
	cred := 0.5
	if sourceCredibility != nil {
		cred = *sourceCredibility
	}

	rel := math.Min(math.Max(insight.Relevance, 0), 1)

	ageMilli := float64(time.Now().UnixMilli() - insight.Timestamp)
	fresh := math.Max(0, math.Exp(-ageMilli/FreshnessHalfLifeMs))

	total := cred*CredibilityWeight + rel*RelevanceWeight + fresh*FreshnessWeight

	decision := "reject"
	switch {
	case total >= AdoptThreshold:
		decision = "adopt"
	case total >= TestThreshold:
		decision = "test"
	}

	return AdoptionScore{
		SourceCredibility: cred,
		SemanticRelevance: rel,
		TemporalFreshness: fresh,
		Total:             total,
		Decision:          decision,
	}
}

// CredibilityRecord tracks success/failure counts for a single agent.
type CredibilityRecord struct {
	Successes int `json:"successes"`
	Failures  int `json:"failures"`
}

// CredibilityTracker persists per-agent credibility scores to disk.
type CredibilityTracker struct {
	baseDir  string
	filePath string
	data     map[string]CredibilityRecord
	mu       sync.Mutex
}

// NewCredibilityTracker creates a tracker that stores scores at baseDir/scores.json.
// Existing data is loaded from disk if the file exists.
func NewCredibilityTracker(baseDir string) *CredibilityTracker {
	t := &CredibilityTracker{
		baseDir:  baseDir,
		filePath: filepath.Join(baseDir, "scores.json"),
		data:     make(map[string]CredibilityRecord),
	}
	t.load()
	return t
}

func (t *CredibilityTracker) load() {
	b, err := os.ReadFile(t.filePath)
	if err != nil {
		return // file doesn't exist yet; start empty
	}
	_ = json.Unmarshal(b, &t.data)
}

func (t *CredibilityTracker) save() error {
	if err := os.MkdirAll(t.baseDir, 0755); err != nil {
		return fmt.Errorf("create credibility dir: %w", err)
	}
	b, err := json.Marshal(t.data)
	if err != nil {
		return fmt.Errorf("marshal credibility data: %w", err)
	}
	return os.WriteFile(t.filePath, b, 0644)
}

// GetCredibility returns the credibility score [0,1] for the given agent.
// Returns 0.5 if no data exists (neutral default).
func (t *CredibilityTracker) GetCredibility(agentSlug string) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	rec, ok := t.data[agentSlug]
	if !ok {
		return 0.5
	}
	total := rec.Successes + rec.Failures
	if total == 0 {
		return 0.5
	}
	return float64(rec.Successes) / float64(total)
}

// RecordOutcome increments the success or failure count for the agent and saves to disk.
func (t *CredibilityTracker) RecordOutcome(agentSlug string, success bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	rec := t.data[agentSlug]
	if success {
		rec.Successes++
	} else {
		rec.Failures++
	}
	t.data[agentSlug] = rec
	_ = t.save()
}
