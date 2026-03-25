package orchestration

import "strings"

// RoutingResult pairs an agent slug with its match score for a task.
type RoutingResult struct {
	AgentSlug string
	Score     float64
}

type agentRegistration struct {
	slug   string
	skills []SkillDeclaration
}

// TaskRouter routes tasks to agents based on skill matching.
type TaskRouter struct {
	agents map[string]*agentRegistration
}

// NewTaskRouter returns an empty TaskRouter.
func NewTaskRouter() *TaskRouter {
	return &TaskRouter{agents: make(map[string]*agentRegistration)}
}

// RegisterAgent adds or replaces an agent's skill registration.
func (r *TaskRouter) RegisterAgent(slug string, skills []SkillDeclaration) {
	r.agents[slug] = &agentRegistration{slug: slug, skills: skills}
}

// UnregisterAgent removes an agent from the router.
func (r *TaskRouter) UnregisterAgent(slug string) {
	delete(r.agents, slug)
}

// ScoreMatch returns a 0-1 score for how well agentSlug can handle task.
// For each required skill, the best matching agent skill (sim * proficiency)
// is found; scores are averaged. Skills with no match above 0.3 contribute 0.
func (r *TaskRouter) ScoreMatch(agentSlug string, task TaskDefinition) float64 {
	reg, ok := r.agents[agentSlug]
	if !ok || len(task.RequiredSkills) == 0 {
		return 0
	}

	total := 0.0
	for _, required := range task.RequiredSkills {
		best := 0.0
		for _, skill := range reg.skills {
			sim := similarity(required, skill.Name)
			score := sim * skill.Proficiency
			if score > best {
				best = score
			}
		}
		if best >= 0.3 {
			total += best
		}
	}
	return total / float64(len(task.RequiredSkills))
}

// FindBestAgent returns the agent with the highest score for the task,
// or nil if no agent scores above 0.
func (r *TaskRouter) FindBestAgent(task TaskDefinition) *RoutingResult {
	results := r.FindCapableAgents(task)
	if len(results) == 0 {
		return nil
	}
	best := results[0]
	for _, rr := range results[1:] {
		if rr.Score > best.Score {
			best = rr
		}
	}
	return &best
}

// FindCapableAgents returns all agents with a score > 0, sorted descending.
func (r *TaskRouter) FindCapableAgents(task TaskDefinition) []RoutingResult {
	var out []RoutingResult
	for slug := range r.agents {
		score := r.ScoreMatch(slug, task)
		if score > 0 {
			out = append(out, RoutingResult{AgentSlug: slug, Score: score})
		}
	}
	// Sort descending by score.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Score > out[i].Score {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// similarity computes the Dice coefficient bigram similarity between two strings.
// Returns a value in [0, 1].
func similarity(a, b string) float64 {
	a = strings.ToLower(a)
	b = strings.ToLower(b)

	if a == b {
		return 1.0
	}
	if len(a) < 2 || len(b) < 2 {
		// For single-char strings, use exact match.
		if a == b {
			return 1.0
		}
		return 0.0
	}

	bigramsA := bigrams(a)
	bigramsB := bigrams(b)

	intersection := 0
	// Count matching bigrams (each bigram in A matched against B).
	usedB := make([]bool, len(bigramsB))
	for _, bg := range bigramsA {
		for i, bgB := range bigramsB {
			if !usedB[i] && bg == bgB {
				intersection++
				usedB[i] = true
				break
			}
		}
	}

	return float64(2*intersection) / float64(len(bigramsA)+len(bigramsB))
}

// bigrams returns the list of two-character substrings in s.
func bigrams(s string) []string {
	runes := []rune(s)
	if len(runes) < 2 {
		return nil
	}
	result := make([]string, len(runes)-1)
	for i := 0; i < len(runes)-1; i++ {
		result[i] = string(runes[i : i+2])
	}
	return result
}
