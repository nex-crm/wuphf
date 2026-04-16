package company

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/operations"
)

// LoadRuntimeManifest resolves any active blueprint refs into a startup-ready
// manifest. It prefers blueprint-backed defaults when refs are available and
// falls back to the stored/default manifest otherwise.
func LoadRuntimeManifest(repoRoot string) (Manifest, error) {
	repoRoot = resolveBlueprintRepoRoot(repoRoot)
	manifest, err := LoadManifest()
	if err != nil {
		return Manifest{}, err
	}
	if resolved, ok := MaterializeManifest(manifest, repoRoot); ok {
		return normalizeManifest(resolved), nil
	}
	return manifest, nil
}

func MaterializeManifest(manifest Manifest, repoRoot string) (Manifest, bool) {
	repoRoot = resolveBlueprintRepoRoot(repoRoot)
	return materializeManifestFromBlueprintRefs(manifest, repoRoot)
}

func materializeManifestFromBlueprintRefs(manifest Manifest, repoRoot string) (Manifest, bool) {
	refs := manifest.ActiveBlueprintRefs()
	if len(refs) == 0 {
		return Manifest{}, false
	}

	operationBlueprint, ok := loadPrimaryOperationBlueprint(repoRoot, refs)
	if !ok {
		return Manifest{}, false
	}
	cfg, _ := config.Load()

	resolved := Manifest{
		Name:          firstNonTemplateNonEmpty(strings.TrimSpace(cfg.CompanyName), strings.TrimSpace(manifest.Name), strings.TrimSpace(operationBlueprint.Name), "The WUPHF Office"),
		Description:   firstNonTemplateNonEmpty(strings.TrimSpace(cfg.CompanyDescription), strings.TrimSpace(manifest.Description), strings.TrimSpace(operationBlueprint.Description), strings.TrimSpace(operationBlueprint.Objective), "Autonomous office runtime."),
		Lead:          firstNonEmpty(strings.TrimSpace(operationBlueprint.Starter.LeadSlug), strings.TrimSpace(manifest.Lead), "ceo"),
		BlueprintRefs: refs,
		UpdatedAt:     manifest.UpdatedAt,
	}

	members := buildMembersFromBlueprints(repoRoot, operationBlueprint, refs)
	if len(members) == 0 {
		return Manifest{}, false
	}
	resolved.Members = members
	resolved.Channels = buildChannelsFromBlueprint(operationBlueprint, resolved.Members, resolved.Lead)
	return resolved, true
}

func loadPrimaryOperationBlueprint(repoRoot string, refs []BlueprintRef) (operations.Blueprint, bool) {
	for _, ref := range refs {
		if ref.Kind != "operation" {
			continue
		}
		blueprint, err := operations.LoadBlueprint(repoRoot, ref.ID)
		if err == nil {
			return blueprint, true
		}
	}
	return operations.Blueprint{}, false
}

func buildMembersFromBlueprints(repoRoot string, blueprint operations.Blueprint, refs []BlueprintRef) []MemberSpec {
	if len(blueprint.Starter.Agents) > 0 {
		members := make([]MemberSpec, 0, len(blueprint.Starter.Agents))
		lead := firstNonEmpty(strings.TrimSpace(blueprint.Starter.LeadSlug), "ceo")
		for _, starter := range blueprint.Starter.Agents {
			if employeeID := normalizeSlug(starter.EmployeeBlueprint); employeeID != "" {
				if employeeBlueprint, err := operations.LoadEmployeeBlueprint(repoRoot, employeeID); err == nil {
					members = append(members, memberSpecFromEmployeeBlueprint(employeeBlueprint, starter, lead))
					continue
				}
			}
			if slug := normalizeSlug(starter.Slug); slug != "" {
				members = append(members, memberSpecFromStarterAgent(starter, lead))
			}
		}
		return members
	}

	employeeIDs := make([]string, 0, len(blueprint.EmployeeBlueprints)+len(refs))
	employeeIDs = append(employeeIDs, blueprint.EmployeeBlueprints...)
	for _, ref := range refs {
		if ref.Kind == "employee" {
			employeeIDs = append(employeeIDs, ref.ID)
		}
	}
	employeeIDs = normalizeSlugs(employeeIDs)
	if len(employeeIDs) > 0 {
		members := make([]MemberSpec, 0, len(employeeIDs))
		for _, id := range employeeIDs {
			employeeBlueprint, err := operations.LoadEmployeeBlueprint(repoRoot, id)
			if err != nil {
				continue
			}
			members = append(members, memberSpecFromEmployeeBlueprint(employeeBlueprint, operations.StarterAgent{}, "ceo"))
		}
		if len(members) > 0 {
			lead := firstNonEmpty(strings.TrimSpace(blueprint.Starter.LeadSlug), "ceo")
			for i := range members {
				members[i].System = members[i].Slug == lead || members[i].Slug == "ceo" || members[i].System
			}
			sort.SliceStable(members, func(i, j int) bool {
				if members[i].System != members[j].System {
					return members[i].System
				}
				return members[i].Slug < members[j].Slug
			})
			return members
		}
	}
	return nil
}

func memberSpecFromEmployeeBlueprint(blueprint operations.EmployeeBlueprint, starter operations.StarterAgent, lead string) MemberSpec {
	slug := normalizeSlug(starter.Slug)
	if slug == "" {
		slug = normalizeSlug(blueprint.ID)
	}
	if slug == "" {
		slug = normalizeSlug(blueprint.Name)
	}
	name := firstNonEmpty(strings.TrimSpace(starter.Name), strings.TrimSpace(blueprint.Name), humanizeSlug(slug))
	role := firstNonEmpty(strings.TrimSpace(starter.Role), strings.TrimSpace(blueprint.Role), name)
	personality := firstNonEmpty(strings.TrimSpace(starter.Personality), strings.TrimSpace(blueprint.Summary), strings.TrimSpace(blueprint.Description))
	permissionMode := strings.TrimSpace(starter.PermissionMode)
	if permissionMode == "" {
		permissionMode = "plan"
	}
	return MemberSpec{
		Slug:           slug,
		Name:           name,
		Role:           role,
		Expertise:      mergeUniqueStrings(normalizeStrings(blueprint.Skills), normalizeStrings(starter.Expertise)),
		Personality:    personality,
		PermissionMode: permissionMode,
		AllowedTools:   normalizeStrings(blueprint.Tools),
		System:         starter.BuiltIn || slug == lead || slug == "ceo" || slug == "operator",
	}
}

func memberSpecFromStarterAgent(starter operations.StarterAgent, lead string) MemberSpec {
	slug := normalizeSlug(starter.Slug)
	if slug == "" {
		return MemberSpec{}
	}
	permissionMode := strings.TrimSpace(starter.PermissionMode)
	if permissionMode == "" {
		permissionMode = "plan"
	}
	return MemberSpec{
		Slug:           slug,
		Name:           firstNonEmpty(strings.TrimSpace(starter.Name), humanizeSlug(slug)),
		Role:           firstNonEmpty(strings.TrimSpace(starter.Role), humanizeSlug(slug)),
		Expertise:      normalizeStrings(starter.Expertise),
		Personality:    strings.TrimSpace(starter.Personality),
		PermissionMode: permissionMode,
		System:         starter.BuiltIn || slug == lead || slug == "ceo",
	}
}

func buildChannelsFromBlueprint(blueprint operations.Blueprint, members []MemberSpec, lead string) []ChannelSpec {
	memberSlugs := make([]string, 0, len(members))
	for _, member := range members {
		memberSlugs = append(memberSlugs, member.Slug)
	}
	memberSlugs = normalizeSlugs(memberSlugs)
	replacements := blueprintTemplateReplacements(blueprint)
	generalDesc := renderBlueprintTemplateString(blueprint.Starter.GeneralChannelDescription, replacements)

	channels := []ChannelSpec{{
		Slug:        "general",
		Name:        "general",
		Description: firstNonEmpty(strings.TrimSpace(generalDesc), defaultChannelDescription("general", "general")),
		Members:     ensureLeadMember(memberSlugs, lead),
	}}
	for _, starter := range blueprint.Starter.Channels {
		slug := normalizeSlug(renderBlueprintTemplateString(starter.Slug, replacements))
		if slug == "" || slug == "general" {
			continue
		}
		name := renderBlueprintTemplateString(starter.Name, replacements)
		description := renderBlueprintTemplateString(starter.Description, replacements)
		renderedMembers := make([]string, 0, len(starter.Members))
		for _, member := range starter.Members {
			renderedMembers = append(renderedMembers, renderBlueprintTemplateString(member, replacements))
		}
		channels = append(channels, ChannelSpec{
			Slug:        slug,
			Name:        firstNonEmpty(strings.TrimSpace(name), slug),
			Description: firstNonEmpty(strings.TrimSpace(description), defaultChannelDescription(slug, name)),
			Members:     ensureLeadMember(renderedMembers, lead),
		})
	}
	return channels
}

func mergeUniqueStrings(values ...[]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, group := range values {
		for _, value := range group {
			value = strings.TrimSpace(value)
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
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonTemplateNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if blueprintTemplateValue(value) {
			continue
		}
		return value
	}
	return ""
}

func blueprintTemplateValue(value string) bool {
	value = strings.TrimSpace(value)
	return strings.Contains(value, "{{") && strings.Contains(value, "}}")
}

func blueprintTemplateReplacements(blueprint operations.Blueprint) map[string]string {
	cfg, _ := config.Load()
	brandName := firstNonTemplateNonEmpty(
		strings.TrimSpace(cfg.CompanyName),
		strings.TrimSpace(blueprint.BootstrapConfig.ChannelName),
		strings.TrimSpace(blueprint.Name),
		"Autonomous operation",
	)
	commandSlug := normalizeSlug(brandName + " command")
	if commandSlug == "" {
		commandSlug = "command"
	}
	niche := firstNonTemplateNonEmpty(
		strings.TrimSpace(blueprint.BootstrapConfig.Niche),
		strings.TrimSpace(cfg.CompanyDescription),
		strings.TrimSpace(blueprint.Description),
		"Automated operation",
	)
	return map[string]string{
		"brand_name":   brandName,
		"brand_slug":   normalizeSlug(brandName),
		"command_slug": commandSlug,
		"niche":        niche,
	}
}

func renderBlueprintTemplateString(value string, replacements map[string]string) string {
	value = strings.TrimSpace(value)
	for key, replacement := range replacements {
		if replacement == "" {
			continue
		}
		value = strings.ReplaceAll(value, "{{"+key+"}}", replacement)
	}
	return strings.TrimSpace(value)
}

func repoRootFromCWD() string {
	if cwd, err := os.Getwd(); err == nil {
		return resolveBlueprintRepoRoot(cwd)
	}
	return "."
}

func resolveBlueprintRepoRoot(repoRoot string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}
	current := repoRoot
	for {
		if looksLikeRepoRoot(current) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return repoRoot
		}
		current = parent
	}
}

func looksLikeRepoRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "templates")); err == nil {
		return true
	}
	return false
}
