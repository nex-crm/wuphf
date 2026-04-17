package operations

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadEmployeeBlueprint(repoRoot, templateID string) (EmployeeBlueprint, error) {
	templateID = normalizeTemplateID(templateID)
	if templateID == "" {
		return EmployeeBlueprint{}, fmt.Errorf("template id required")
	}
	rel := path.Join("templates", "employees", templateID, "blueprint.yaml")
	raw, err := readTemplateFile(repoRoot, rel)
	if err != nil {
		return EmployeeBlueprint{}, err
	}
	var blueprint EmployeeBlueprint
	if err := yaml.Unmarshal(raw, &blueprint); err != nil {
		return EmployeeBlueprint{}, err
	}
	blueprint = normalizeEmployeeBlueprint(templateID, blueprint)
	if err := validateEmployeeBlueprint(blueprint); err != nil {
		return EmployeeBlueprint{}, err
	}
	return blueprint, nil
}

func ListEmployeeBlueprints(repoRoot string) ([]EmployeeBlueprint, error) {
	names, err := listTemplateDirs(repoRoot, path.Join("templates", "employees"))
	if err != nil {
		return nil, err
	}
	blueprints := make([]EmployeeBlueprint, 0, len(names))
	for _, name := range names {
		blueprint, err := LoadEmployeeBlueprint(repoRoot, name)
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

func normalizeEmployeeBlueprint(templateID string, blueprint EmployeeBlueprint) EmployeeBlueprint {
	blueprint.ID = strings.TrimSpace(blueprint.ID)
	if blueprint.ID == "" {
		blueprint.ID = templateID
	}
	blueprint.Name = strings.TrimSpace(blueprint.Name)
	blueprint.Kind = strings.TrimSpace(blueprint.Kind)
	if blueprint.Kind == "" {
		blueprint.Kind = "employee"
	}
	blueprint.Description = strings.TrimSpace(blueprint.Description)
	blueprint.Summary = strings.TrimSpace(blueprint.Summary)
	blueprint.Role = strings.TrimSpace(blueprint.Role)
	blueprint.Responsibilities = trimStringSlice(blueprint.Responsibilities)
	blueprint.StartingTasks = trimStringSlice(blueprint.StartingTasks)
	blueprint.AutomatedLoops = trimStringSlice(blueprint.AutomatedLoops)
	blueprint.Skills = trimStringSlice(blueprint.Skills)
	blueprint.Tools = trimStringSlice(blueprint.Tools)
	blueprint.ExpectedResults = trimStringSlice(blueprint.ExpectedResults)
	blueprint.UsedBy = trimStringSlice(blueprint.UsedBy)
	return blueprint
}

func validateEmployeeBlueprint(blueprint EmployeeBlueprint) error {
	if strings.TrimSpace(blueprint.ID) == "" {
		return fmt.Errorf("employee blueprint id required")
	}
	if strings.TrimSpace(blueprint.Name) == "" {
		return fmt.Errorf("employee blueprint %q name required", blueprint.ID)
	}
	if strings.TrimSpace(blueprint.Summary) == "" && strings.TrimSpace(blueprint.Description) == "" {
		return fmt.Errorf("employee blueprint %q summary required", blueprint.ID)
	}
	if len(blueprint.Responsibilities) == 0 {
		return fmt.Errorf("employee blueprint %q responsibilities required", blueprint.ID)
	}
	if len(blueprint.StartingTasks) == 0 {
		return fmt.Errorf("employee blueprint %q starting tasks required", blueprint.ID)
	}
	if len(blueprint.AutomatedLoops) == 0 {
		return fmt.Errorf("employee blueprint %q automated loops required", blueprint.ID)
	}
	if len(blueprint.Skills) == 0 {
		return fmt.Errorf("employee blueprint %q skills required", blueprint.ID)
	}
	if len(blueprint.Tools) == 0 {
		return fmt.Errorf("employee blueprint %q tools required", blueprint.ID)
	}
	if len(blueprint.ExpectedResults) == 0 {
		return fmt.Errorf("employee blueprint %q expected results required", blueprint.ID)
	}
	return nil
}

func trimStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}
