package operations

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var nonTemplateSlug = regexp.MustCompile(`[^a-z0-9._-]+`)

func LoadBlueprint(repoRoot, templateID string) (Blueprint, error) {
	templateID = normalizeTemplateID(templateID)
	if templateID == "" {
		return Blueprint{}, fmt.Errorf("template id required")
	}
	rel := path.Join("templates", "operations", templateID, "blueprint.yaml")
	raw, err := readTemplateFile(repoRoot, rel)
	if err != nil {
		return Blueprint{}, err
	}
	var blueprint Blueprint
	if err := yaml.Unmarshal(raw, &blueprint); err != nil {
		return Blueprint{}, err
	}
	blueprint = normalizeBlueprint(templateID, blueprint)
	if err := validateBlueprint(repoRoot, blueprint); err != nil {
		return Blueprint{}, err
	}
	return blueprint, nil
}

func ListBlueprints(repoRoot string) ([]Blueprint, error) {
	names, err := listTemplateDirs(repoRoot, path.Join("templates", "operations"))
	if err != nil {
		return nil, err
	}
	blueprints := make([]Blueprint, 0, len(names))
	for _, name := range names {
		blueprint, err := LoadBlueprint(repoRoot, name)
		if err != nil {
			return nil, err
		}
		blueprints = append(blueprints, blueprint)
	}
	sort.Slice(blueprints, func(i, j int) bool {
		return blueprints[i].ID < blueprints[j].ID
	})
	return blueprints, nil
}

func normalizeTemplateID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, " ", "-")
	value = nonTemplateSlug.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	return value
}

func normalizeBlueprint(templateID string, blueprint Blueprint) Blueprint {
	blueprint.ID = normalizeTemplateID(firstOperationValue(blueprint.ID, templateID))
	blueprint.Name = strings.TrimSpace(blueprint.Name)
	blueprint.Kind = strings.TrimSpace(blueprint.Kind)
	blueprint.Description = strings.TrimSpace(blueprint.Description)
	blueprint.Objective = strings.TrimSpace(blueprint.Objective)
	blueprint.EmployeeBlueprints = normalizeTemplateIDs(blueprint.EmployeeBlueprints)
	blueprint.Starter = normalizeStarterPlan(blueprint.Starter)
	blueprint.EmployeeBlueprints = appendUniqueTemplateIDs(blueprint.EmployeeBlueprints, starterEmployeeBlueprintIDs(blueprint.Starter.Agents)...)
	blueprint.DefaultReviewer = strings.TrimSpace(blueprint.DefaultReviewer)
	blueprint.ReviewerPaths = normalizeReviewerPaths(blueprint.ReviewerPaths)
	return blueprint
}

func normalizeReviewerPaths(rules ReviewerPathMap) ReviewerPathMap {
	if len(rules) == 0 {
		return nil
	}
	out := make(ReviewerPathMap, 0, len(rules))
	for _, rule := range rules {
		rule.Pattern = strings.TrimSpace(rule.Pattern)
		rule.Reviewer = strings.TrimSpace(rule.Reviewer)
		out = append(out, rule)
	}
	return out
}

func normalizeStarterPlan(plan StarterPlan) StarterPlan {
	plan.LeadSlug = normalizeTemplateID(plan.LeadSlug)
	plan.GeneralChannelDescription = strings.TrimSpace(plan.GeneralChannelDescription)
	plan.KickoffPrompt = strings.TrimSpace(plan.KickoffPrompt)
	plan.Agents = normalizeStarterAgents(plan.Agents)
	plan.Channels = normalizeStarterChannels(plan.Channels)
	plan.Tasks = normalizeStarterTasks(plan.Tasks)
	return plan
}

func normalizeStarterAgents(agents []StarterAgent) []StarterAgent {
	out := make([]StarterAgent, 0, len(agents))
	for _, agent := range agents {
		agent.Slug = normalizeTemplateID(agent.Slug)
		agent.Emoji = strings.TrimSpace(agent.Emoji)
		agent.Name = strings.TrimSpace(agent.Name)
		agent.Role = strings.TrimSpace(agent.Role)
		agent.EmployeeBlueprint = normalizeTemplateID(agent.EmployeeBlueprint)
		agent.Type = strings.TrimSpace(agent.Type)
		agent.Personality = strings.TrimSpace(agent.Personality)
		agent.Expertise = trimStringSlice(agent.Expertise)
		out = append(out, agent)
	}
	return out
}

func normalizeStarterChannels(channels []StarterChannel) []StarterChannel {
	out := make([]StarterChannel, 0, len(channels))
	for _, channel := range channels {
		channel.Slug = normalizeStarterIdentifier(channel.Slug)
		channel.Name = strings.TrimSpace(channel.Name)
		channel.Description = strings.TrimSpace(channel.Description)
		channel.Members = normalizeStarterIdentifiers(channel.Members)
		out = append(out, channel)
	}
	return out
}

func normalizeStarterTasks(tasks []StarterTask) []StarterTask {
	out := make([]StarterTask, 0, len(tasks))
	for _, task := range tasks {
		task.Channel = normalizeStarterIdentifier(task.Channel)
		task.Owner = normalizeStarterIdentifier(task.Owner)
		task.Title = strings.TrimSpace(task.Title)
		task.Details = strings.TrimSpace(task.Details)
		out = append(out, task)
	}
	return out
}

func normalizeStarterIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "{{") && strings.Contains(value, "}}") {
		return value
	}
	return normalizeTemplateID(value)
}

func normalizeStarterIdentifiers(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = normalizeStarterIdentifier(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func validateBlueprint(repoRoot string, blueprint Blueprint) error {
	if strings.TrimSpace(blueprint.ID) == "" {
		return fmt.Errorf("operation blueprint id required")
	}
	if strings.TrimSpace(blueprint.Name) == "" {
		return fmt.Errorf("operation blueprint %q name required", blueprint.ID)
	}
	if len(blueprint.Starter.Agents) == 0 {
		return fmt.Errorf("operation blueprint %q starter agents required", blueprint.ID)
	}
	refs := append([]string(nil), blueprint.EmployeeBlueprints...)
	for _, agent := range blueprint.Starter.Agents {
		if strings.TrimSpace(agent.EmployeeBlueprint) == "" && strings.TrimSpace(agent.PermissionMode) == "" {
			return fmt.Errorf("operation blueprint %q starter agent %q requires employee_blueprint or permission_mode", blueprint.ID, agent.Slug)
		}
		if strings.TrimSpace(agent.EmployeeBlueprint) != "" {
			refs = append(refs, agent.EmployeeBlueprint)
		}
	}
	refs = normalizeTemplateIDs(refs)
	for _, ref := range refs {
		if _, err := LoadEmployeeBlueprint(repoRoot, ref); err != nil {
			return fmt.Errorf("operation blueprint %q employee blueprint %q: %w", blueprint.ID, ref, err)
		}
	}
	if err := validateReviewerConfig(blueprint); err != nil {
		return err
	}
	return nil
}

// validateReviewerConfig enforces that every reviewer slug referenced by
// DefaultReviewer or ReviewerPaths either matches a starter agent slug on
// the blueprint or is the sentinel "human-only". Invalid glob patterns in
// ReviewerPaths keys are also rejected here so misconfigurations surface
// at load time instead of at promotion time.
func validateReviewerConfig(blueprint Blueprint) error {
	file := blueprintYAMLPath(blueprint.ID)
	agentSlugs := make(map[string]struct{}, len(blueprint.Starter.Agents))
	for _, agent := range blueprint.Starter.Agents {
		slug := strings.TrimSpace(agent.Slug)
		if slug == "" {
			continue
		}
		agentSlugs[slug] = struct{}{}
	}
	validReviewer := func(value string) bool {
		if value == ReviewerHumanOnly {
			return true
		}
		_, ok := agentSlugs[value]
		return ok
	}

	if value := strings.TrimSpace(blueprint.DefaultReviewer); value != "" {
		if !validReviewer(value) {
			// Include the slugs we actually saw in the error so CI-only
			// failures become debuggable without attaching a debugger.
			// CI env (Linux amd64 on ubuntu-latest) has shown this fire
			// despite the YAML declaring all slugs; the diff lists what
			// the loader sees right before rejection.
			seen := make([]string, 0, len(agentSlugs))
			for k := range agentSlugs {
				seen = append(seen, k)
			}
			sort.Strings(seen)
			return fmt.Errorf("blueprint %s default_reviewer %q does not match any agent slug or %q (file: %s; saw slugs: %v, agents_count=%d)", blueprint.ID, value, ReviewerHumanOnly, file, seen, len(blueprint.Starter.Agents))
		}
	}

	for _, rule := range blueprint.ReviewerPaths {
		pattern := strings.TrimSpace(rule.Pattern)
		reviewer := strings.TrimSpace(rule.Reviewer)
		if pattern == "" {
			return fmt.Errorf("blueprint %s reviewer_paths contains an empty pattern (file: %s)", blueprint.ID, file)
		}
		if _, err := filepath.Match(stripDoubleStar(pattern), ""); err != nil {
			return fmt.Errorf("blueprint %s reviewer_paths pattern %q is not a valid glob: %v (file: %s)", blueprint.ID, pattern, err, file)
		}
		if reviewer == "" {
			return fmt.Errorf("blueprint %s reviewer_paths %q has empty reviewer value (file: %s)", blueprint.ID, pattern, file)
		}
		if !validReviewer(reviewer) {
			return fmt.Errorf("blueprint %s reviewer_paths %q reviewer %q does not match any agent slug or %q (file: %s)", blueprint.ID, pattern, reviewer, ReviewerHumanOnly, file)
		}
	}
	return nil
}

// stripDoubleStar maps our "**" (recursive) syntax to "*" so we can reuse
// filepath.Match for cheap syntactic validation. filepath.Match does not
// understand "**" natively, so we would otherwise reject valid patterns.
func stripDoubleStar(pattern string) string {
	return strings.ReplaceAll(pattern, "**", "*")
}

// blueprintYAMLPath returns the repo-relative path to the blueprint YAML
// so validation errors can tell the user which file to edit.
func blueprintYAMLPath(id string) string {
	return path.Join("templates", "operations", id, "blueprint.yaml")
}

func normalizeTemplateIDs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = normalizeTemplateID(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func appendUniqueTemplateIDs(base []string, extras ...string) []string {
	out := append([]string(nil), base...)
	seen := make(map[string]struct{}, len(out))
	for _, value := range out {
		seen[value] = struct{}{}
	}
	for _, value := range extras {
		value = normalizeTemplateID(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func starterEmployeeBlueprintIDs(agents []StarterAgent) []string {
	out := make([]string, 0, len(agents))
	seen := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		id := normalizeTemplateID(agent.EmployeeBlueprint)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
