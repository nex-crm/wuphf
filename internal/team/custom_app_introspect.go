package team

// custom_app_introspect.go — deterministic, source-derived understanding of what
// an app ACTUALLY is.
//
// The App Builder edits apps from prose, which invites hallucination: claiming an
// app already reads Slack, or has a data type, or exposes a screen it does not.
// This derives the app's real shape — data model, APIs, office writes, UI — by
// statically reading its persisted source (comment/string-aware, reusing the
// guard's lexer). It is attached to get_app and to the edit brief so the agent
// builds on GROUND TRUTH instead of guessing. Informational, not a gate, so it
// errs toward over-reporting rather than blocking.

import (
	"regexp"
	"sort"
	"strings"
)

// AppIntegrationUsage is one external platform the app touches and the action ids
// it calls on it.
type AppIntegrationUsage struct {
	Platform string   `json:"platform"`
	Actions  []string `json:"actions,omitempty"`
}

// AppCapabilities is the deterministic description of an app's current shape,
// grouped the way the App Builder reasons: data model, APIs/data access, office
// writes (the "workflow" side effects), UI, and the source files themselves.
type AppCapabilities struct {
	BridgeAPIs   []string              `json:"bridge_apis,omitempty"`
	Integrations []AppIntegrationUsage `json:"integrations,omitempty"`
	Resources    []string              `json:"resources,omitempty"`
	DataTypes    []string              `json:"data_types,omitempty"`
	UIComponents []string              `json:"ui_components,omitempty"`
	OfficeWrites []string              `json:"office_writes,omitempty"`
	SourceFiles  []string              `json:"source_files,omitempty"`
}

// bridgeAPINames are the canonical wuphf-bridge helpers an app calls. Usage of
// each (as a call) is detected in the app's own source.
var bridgeAPINames = []string{
	"callBroker", "getTasks", "getOfficeMembers", "getEmails",
	"createTask", "callIntegration", "listIntegrations", "ai",
}

var (
	bridgeAPIRegexps  = compileBridgeAPIRegexps()
	reCallIntegration = regexp.MustCompile("callIntegration\\s*\\(\\s*[\"'`]([a-zA-Z0-9_.-]+)[\"'`]\\s*,\\s*[\"'`]([A-Za-z0-9_]+)[\"'`]")
	reMetaIntegration = regexp.MustCompile("platform\\s*:\\s*[\"'`]([a-zA-Z0-9_.-]+)[\"'`][\\s\\S]{0,120}?action\\s*:\\s*[\"'`]([A-Za-z0-9_]+)[\"'`]")
	reResourceHook    = regexp.MustCompile("resource\\s*:\\s*[\"'`]([a-zA-Z0-9_-]+)[\"'`]")
	reResourcesBlock  = regexp.MustCompile(`resources\s*=\s*\{\s*\[([\s\S]*?)\]`)
	reNameField       = regexp.MustCompile("name\\s*:\\s*[\"'`]([a-zA-Z0-9_-]+)[\"'`]")
	reTypeDecl        = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:interface|type)\s+([A-Z][A-Za-z0-9_]*)`)
	reMantineImport   = regexp.MustCompile(`import\s*(?:type\s*)?\{([^}]*)\}\s*from\s*["']@mantine/core["']`)
)

func compileBridgeAPIRegexps() map[string]*regexp.Regexp {
	out := make(map[string]*regexp.Regexp, len(bridgeAPINames))
	for _, name := range bridgeAPINames {
		out[name] = regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\(`)
	}
	return out
}

// introspectAppSource derives an app's capabilities from its source files. Only
// agent-authored TS/JS is scanned (the same predicate the guard uses, so the
// host-owned protected files and build artifacts are skipped) — bridge USAGE
// lives in App.tsx, not in the bridge definition.
func introspectAppSource(files map[string]string) AppCapabilities {
	var caps AppCapabilities
	apiSet := map[string]bool{}
	resSet := map[string]bool{}
	typeSet := map[string]bool{}
	uiSet := map[string]bool{}
	integ := map[string]map[string]bool{}

	addInteg := func(platform, action string) {
		p := strings.ToLower(strings.TrimSpace(platform))
		if p == "" {
			return
		}
		if integ[p] == nil {
			integ[p] = map[string]bool{}
		}
		if a := strings.TrimSpace(action); a != "" {
			integ[p][a] = true
		}
	}

	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, rel := range keys {
		if !appGuardShouldScan(rel) {
			continue
		}
		caps.SourceFiles = append(caps.SourceFiles, rel)
		code := appCommentStripped(files[rel])

		for name, re := range bridgeAPIRegexps {
			if re.MatchString(code) {
				apiSet[name] = true
			}
		}
		for _, m := range reCallIntegration.FindAllStringSubmatch(code, -1) {
			addInteg(m[1], m[2])
		}
		for _, m := range reMetaIntegration.FindAllStringSubmatch(code, -1) {
			addInteg(m[1], m[2])
		}
		for _, m := range reResourceHook.FindAllStringSubmatch(code, -1) {
			resSet[m[1]] = true
		}
		for _, blk := range reResourcesBlock.FindAllStringSubmatch(code, -1) {
			for _, n := range reNameField.FindAllStringSubmatch(blk[1], -1) {
				resSet[n[1]] = true
			}
		}
		for _, m := range reTypeDecl.FindAllStringSubmatch(code, -1) {
			typeSet[m[1]] = true
		}
		for _, m := range reMantineImport.FindAllStringSubmatch(code, -1) {
			for _, comp := range strings.Split(m[1], ",") {
				comp = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comp), "type "))
				comp = strings.TrimSpace(strings.SplitN(comp, " as ", 2)[0])
				if comp != "" {
					uiSet[comp] = true
				}
			}
		}
	}
	// getEmails() is the bridge's thin Gmail wrapper, so its use implies a gmail read.
	if apiSet["getEmails"] {
		addInteg("gmail", "GMAIL_FETCH_EMAILS")
	}

	caps.BridgeAPIs = sortedSetKeys(apiSet)
	caps.Resources = sortedSetKeys(resSet)
	caps.DataTypes = sortedSetKeys(typeSet)
	caps.UIComponents = sortedSetKeys(uiSet)
	for platform, acts := range integ {
		caps.Integrations = append(caps.Integrations, AppIntegrationUsage{
			Platform: platform,
			Actions:  sortedSetKeys(acts),
		})
	}
	sort.Slice(caps.Integrations, func(i, j int) bool {
		return caps.Integrations[i].Platform < caps.Integrations[j].Platform
	})
	if apiSet["createTask"] {
		caps.OfficeWrites = append(caps.OfficeWrites, "createTask")
	}
	return caps
}

// appCommentStripped returns content with // and /* */ comments removed (string
// contents kept), threading the guard's lexer state across lines so multi-line
// templates and block comments are handled.
func appCommentStripped(content string) string {
	var b strings.Builder
	var st lexState
	for _, line := range strings.Split(content, "\n") {
		var code string
		code, _, st = codeView(line, st)
		b.WriteString(code)
		b.WriteByte('\n')
	}
	return b.String()
}

func sortedSetKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// renderAppCapabilities formats the capabilities as a compact ground-truth brief
// the App Builder reads before editing. Empty when there is no scannable source
// (an html-only app), so callers can omit the section entirely.
func renderAppCapabilities(c AppCapabilities) string {
	if len(c.SourceFiles) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("CURRENT APP SHAPE (source-derived ground truth — build on what's here; do NOT claim capabilities it does not have):\n")

	dm := []string{}
	if len(c.Resources) > 0 {
		dm = append(dm, "resources: "+strings.Join(c.Resources, ", "))
	}
	if len(c.DataTypes) > 0 {
		dm = append(dm, "types: "+strings.Join(c.DataTypes, ", "))
	}
	if len(dm) > 0 {
		b.WriteString("- Data model: " + strings.Join(dm, "; ") + "\n")
	} else {
		b.WriteString("- Data model: none declared\n")
	}

	apis := []string{}
	if len(c.BridgeAPIs) > 0 {
		apis = append(apis, "bridge: "+strings.Join(c.BridgeAPIs, ", "))
	}
	if len(c.Integrations) > 0 {
		parts := make([]string, 0, len(c.Integrations))
		for _, it := range c.Integrations {
			if len(it.Actions) > 0 {
				parts = append(parts, it.Platform+" ["+strings.Join(it.Actions, ", ")+"]")
			} else {
				parts = append(parts, it.Platform)
			}
		}
		apis = append(apis, "integrations: "+strings.Join(parts, "; "))
	}
	if len(apis) > 0 {
		b.WriteString("- APIs / data access: " + strings.Join(apis, "; ") + "\n")
	} else {
		b.WriteString("- APIs / data access: none\n")
	}

	if len(c.OfficeWrites) > 0 {
		b.WriteString("- Office writes: " + strings.Join(c.OfficeWrites, ", ") + " (human-confirmed)\n")
	} else {
		b.WriteString("- Office writes: none (read-only app)\n")
	}
	if len(c.UIComponents) > 0 {
		b.WriteString("- UI (Mantine): " + strings.Join(c.UIComponents, ", ") + "\n")
	}
	b.WriteString("- Source files: " + strings.Join(c.SourceFiles, ", ") + "\n")
	return b.String()
}
