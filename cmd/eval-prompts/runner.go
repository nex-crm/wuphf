package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

// systemPrompt is the fixed system message sent before every eval prompt.
const systemPrompt = "You are a structured data extraction assistant. Follow all instructions exactly. Return only what the prompt requests."

// caseResult is the outcome of running a single eval case.
type caseResult struct {
	suite    string
	caseID   string
	pass     bool
	failures []string
	elapsed  time.Duration
	err      error
}

// evalCase mirrors the schema in evals/harness/schema.json.
type evalCase struct {
	ID           string         `json:"id"`
	Prompt       string         `json:"prompt"`
	Description  string         `json:"description"`
	TemplateVars map[string]any `json:"template_vars"`
	Expected     expectedBlock  `json:"expected"`
}

// expectedBlock mirrors the expected object in the eval case schema.
type expectedBlock struct {
	MustInclude    []string       `json:"must_include"`
	MustNotInclude []string       `json:"must_not_include"`
	Structured     map[string]any `json:"structured"`
	Notes          string         `json:"notes"`
}

// runCase loads, renders, calls the LLM, and asserts an eval case.
// repoRoot is the absolute path to the repository root.
func runCase(casePath, repoRoot string) caseResult {
	start := time.Now()

	// Parse the case file.
	raw, err := os.ReadFile(casePath)
	if err != nil {
		return caseResult{err: fmt.Errorf("read case: %w", err)}
	}
	var ec evalCase
	if err := json.Unmarshal(raw, &ec); err != nil {
		return caseResult{err: fmt.Errorf("parse case: %w", err)}
	}

	suite := suiteFromID(ec.ID)

	// Render the prompt template.
	rendered, err := renderTemplate(repoRoot, ec.Prompt, ec.TemplateVars)
	if err != nil {
		return caseResult{
			suite:  suite,
			caseID: ec.ID,
			err:    fmt.Errorf("render template %s: %w", ec.Prompt, err),
		}
	}

	// Call the LLM.
	output, err := provider.RunConfiguredOneShot(systemPrompt, rendered, repoRoot)
	if err != nil {
		return caseResult{
			suite:   suite,
			caseID:  ec.ID,
			elapsed: time.Since(start),
			err:     fmt.Errorf("llm call: %w", err),
		}
	}

	// Strip code fences.
	cleaned := stripCodeFences(output)

	// Parse output per prompt contract.
	parsed, parseErr := parseOutput(ec.Prompt, cleaned)

	// Build failure list from parse error first.
	var failures []string
	if parseErr != nil {
		failures = append(failures, fmt.Sprintf("json parse failed: %v — raw output: %s", parseErr, truncate(cleaned, 200)))
	}

	// Run matchers.
	mr := assertExpected(cleaned, parsed, ec.Expected)
	failures = append(failures, mr.failures...)

	return caseResult{
		suite:   suite,
		caseID:  ec.ID,
		pass:    len(failures) == 0,
		failures: failures,
		elapsed: time.Since(start),
	}
}

// renderTemplate loads prompts/{name}.tmpl and executes it with vars.
func renderTemplate(repoRoot, name string, vars map[string]any) (string, error) {
	tmplPath := filepath.Join(repoRoot, "prompts", name+".tmpl")
	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", tmplPath, err)
	}

	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
	}

	tmpl, err := template.New(name).Funcs(funcMap).Parse(string(tmplBytes))
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	// text/template accesses map keys via dot notation, but needs the map
	// values to be accessible as .Key. We wrap vars in a struct-like data
	// value using a map[string]any passed as dot.
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// parseOutput returns a decoded JSON value for JSON-returning prompts, or nil
// for synthesis prompts (raw markdown). On JSON parse failure it returns an
// error.
func parseOutput(promptName, output string) (any, error) {
	switch promptName {
	case "synthesis_v2":
		// Synthesis returns raw markdown — no JSON parsing.
		return nil, nil
	default:
		// extract_entities_lite, answer_query, lint_contradictions all return JSON.
		var v any
		if err := json.Unmarshal([]byte(output), &v); err != nil {
			return nil, err
		}
		return v, nil
	}
}

// stripCodeFences removes leading/trailing markdown code fences from LLM output.
// Handles both ```json ... ``` and ``` ... ``` wrappers.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)

	// Check for opening fence.
	if strings.HasPrefix(s, "```") {
		// Remove the opening fence line.
		idx := strings.Index(s, "\n")
		if idx == -1 {
			return s
		}
		s = s[idx+1:]
		// Remove the closing fence.
		s = strings.TrimSuffix(strings.TrimRight(s, "\n"), "```")
		s = strings.TrimSpace(s)
	}
	return s
}

// suiteFromID extracts the suite name from an ID like "extract_001_foo" → "extract".
func suiteFromID(id string) string {
	parts := strings.SplitN(id, "_", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return id
}

// truncate shortens a string to at most n runes for display purposes.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
