package teammcp

// app_tools.go — MCP tools for Apps (agent-generated internal tools).
//
//   list_apps     (all agents)      check what already exists before proposing
//   propose_app   (all agents)      raise a NON-BLOCKING approval to build/improve
//   get_app       (App Builder)     read an app's current source before editing
//   register_app  (App Builder)     publish the built single-file app
//
// The build itself is the App Builder agent's job — it scaffolds a real
// Vite/React/TS project, builds a single self-contained index.html, and calls
// register_app. Other agents only ever discover (list_apps) and propose
// (propose_app); they never write app bytes.

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// appBuilderSlug mirrors company.AppBuilderSlug / team's appBuilderSlug. Kept
// local so this package gates on the slug without importing the broker just for
// one identifier; the three MUST stay in sync.
const appBuilderSlug = "app-builder"

// ListAppsArgs has no inputs — it returns every app in the office.
type ListAppsArgs struct{}

// GetAppArgs identifies one app to read.
type GetAppArgs struct {
	AppID string `json:"app_id" jsonschema:"The app id (app_0123456789abcdef) from list_apps."`
}

// ProposeAppArgs raises a non-blocking approval to build or improve an app.
type ProposeAppArgs struct {
	MySlug      string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
	Channel     string `json:"channel,omitempty" jsonschema:"Channel to raise the proposal in. Defaults to your current channel."`
	Name        string `json:"name" jsonschema:"Short product name for the tool, e.g. 'Lead Scorer'."`
	Icon        string `json:"icon,omitempty" jsonschema:"Optional emoji icon for the app."`
	Summary     string `json:"summary,omitempty" jsonschema:"One-line summary of what the tool does."`
	Description string `json:"description" jsonschema:"What the app should do, the repeatable workflow it automates, and why it is worth building."`
	AppID       string `json:"app_id,omitempty" jsonschema:"Set ONLY when improving an EXISTING app you found via list_apps, instead of creating a duplicate."`
}

// RegisterAppArgs publishes the built single-file app. App Builder only.
type RegisterAppArgs struct {
	MySlug      string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
	AppID       string `json:"app_id,omitempty" jsonschema:"Set to update an existing app in place; leave empty to create a new one."`
	Name        string `json:"name" jsonschema:"The app's display name."`
	Icon        string `json:"icon,omitempty" jsonschema:"Optional emoji icon."`
	Summary     string `json:"summary,omitempty" jsonschema:"One-line summary shown in the sidebar."`
	Description string `json:"description,omitempty" jsonschema:"What the app does — keep this current as it evolves."`
	// PREFER html_path. The built single-file bundle is minified onto enormous
	// single lines (100k+ chars) that an agent cannot reliably read back and
	// re-emit as a JSON string — pass the path and let the broker read the file.
	HTMLPath string `json:"html_path,omitempty" jsonschema:"PREFERRED. Absolute path to the built dist/index.html (the broker reads it). Use this instead of pasting the bundle — minified single-file output has 100k+ char lines you cannot reliably read and re-emit."`
	HTML     string `json:"html,omitempty" jsonschema:"Fallback only. The COMPLETE self-contained index.html as a string. Prefer html_path — minified output is too large to paste reliably. All JS/CSS inlined (vite-plugin-singlefile); no external scripts/styles/fonts and no network fetches; read workspace data only through the injected WUPHF bridge (window.parent postMessage)."`
	// PREFER source_path. Hand-assembling the files map is error-prone — agents
	// drop files (e.g. App.tsx) and ship a source tree that won't build. Point at
	// the dir that just passed the verify gate and the broker copies it whole.
	SourcePath string `json:"source_path,omitempty" jsonschema:"PREFERRED. Absolute path to your source project root (the dir with package.json + src/, the one you just built). The broker copies the WHOLE tree (minus node_modules/dist/.vite/.git) so the persisted source is complete and the live preview + later edits work. Use this instead of hand-listing files."`
	// Files is the source project so future edits modify real files instead of
	// regenerating from prose. Fallback for source_path.
	Files map[string]string `json:"files,omitempty" jsonschema:"Fallback for source_path. The app's SOURCE project as a map of relative path -> file content (e.g. src/App.tsx, package.json, vite.config.ts). MUST include every file the app imports — a partial map ships a broken app. EXCLUDE node_modules and dist. Prefer source_path so nothing is dropped."`
}

// registerAppTools wires the Apps tools. Discovery + proposal are open to every
// agent; the build/publish tools are gated to the App Builder.
func registerAppTools(server *mcp.Server, slug string) {
	mcp.AddTool(server, readOnlyTool(
		"list_apps",
		"List the internal tools (Apps) that already exist in this office. ALWAYS call this before proposing a new app: if a related app exists, propose improving it (propose_app with app_id) instead of creating a duplicate.",
	), handleListApps)
	mcp.AddTool(server, officeWriteTool(
		"propose_app",
		"Propose building (or improving) an internal tool when you notice a repeatable workflow. Raises a NON-BLOCKING approval card the human can Approve, Approve-with-note, or Reject. Do NOT use this when the human used /create-app, /update-app, or explicitly asked you to build — in that case the build is already authorized. After proposing, keep working; do not block waiting for the answer. On approval the App Builder builds it automatically.",
	), handleProposeApp)
	if strings.EqualFold(strings.TrimSpace(slug), appBuilderSlug) {
		mcp.AddTool(server, readOnlyTool(
			"get_app",
			"Read an existing app's manifest and current HTML so you can edit it. App Builder only.",
		), handleGetApp)
		mcp.AddTool(server, officeWriteTool(
			"register_app",
			"Publish a built app so it appears under Apps. Pass html_path (absolute path to the built dist/index.html) and source_path (absolute path to the project root) — the broker reads both from disk, so you never paste the minified bundle or hand-list files. Set app_id to update an existing app in place. App Builder only.",
		), handleRegisterApp)
	}
}

func handleListApps(ctx context.Context, _ *mcp.CallToolRequest, _ ListAppsArgs) (*mcp.CallToolResult, any, error) {
	var result struct {
		Apps []map[string]any `json:"apps"`
	}
	if err := brokerGetJSON(ctx, "/apps", &result); err != nil {
		return toolError(err), nil, nil
	}
	if result.Apps == nil {
		result.Apps = []map[string]any{}
	}
	payload, err := json.Marshal(result.Apps)
	if err != nil {
		return toolError(fmt.Errorf("marshal apps list: %w", err)), nil, nil
	}
	return textResult(string(payload)), nil, nil
}

func handleGetApp(ctx context.Context, _ *mcp.CallToolRequest, args GetAppArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.AppID)
	if err := validateCustomAppIDLocal(id); err != nil {
		return toolError(err), nil, nil
	}
	var result map[string]any
	// ?source=1 so the App Builder gets the editable source project back, not
	// just the built bundle — this is what makes an edit reliable.
	if err := brokerGetJSON(ctx, "/apps/"+url.PathEscape(id)+"?source=1", &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return toolError(fmt.Errorf("marshal app: %w", err)), nil, nil
	}
	return textResult(string(payload)), nil, nil
}

func handleProposeApp(ctx context.Context, _ *mcp.CallToolRequest, args ProposeAppArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return toolError(fmt.Errorf("name is required")), nil, nil
	}
	description := strings.TrimSpace(args.Description)
	if description == "" {
		return toolError(fmt.Errorf("description is required: explain what the app does and the workflow it automates")), nil, nil
	}
	appID := strings.TrimSpace(args.AppID)
	if appID != "" {
		if err := validateCustomAppIDLocal(appID); err != nil {
			return toolError(err), nil, nil
		}
	}
	location := resolveConversationContext(ctx, slug, args.Channel, "")
	channel := location.Channel

	verb := "Build a new internal tool"
	if appID != "" {
		verb = "Improve the app"
	}
	question := fmt.Sprintf("%s: %s?", verb, name)
	var contextText strings.Builder
	if summary := strings.TrimSpace(args.Summary); summary != "" {
		contextText.WriteString(summary)
		contextText.WriteString("\n\n")
	}
	contextText.WriteString("What it does:\n")
	contextText.WriteString(description)
	contextText.WriteString("\n\nOn approval, the App Builder will scaffold a small React/TypeScript app and register it under Apps. Approve, Approve with note (to add constraints), or Reject.")

	dedupeKey := "app-proposal:" + slug + ":" + strings.ToLower(name)
	if appID != "" {
		dedupeKey += ":" + appID
	}

	var created struct {
		ID      string `json:"id"`
		Deduped bool   `json:"deduped"`
	}
	if err := brokerPostJSON(ctx, "/requests", map[string]any{
		"kind":       "approval",
		"channel":    channel,
		"from":       slug,
		"title":      question,
		"question":   question,
		"context":    contextText.String(),
		"blocking":   false,
		"required":   false,
		"dedupe_key": dedupeKey,
		"app_proposal": map[string]any{
			"name":        name,
			"icon":        strings.TrimSpace(args.Icon),
			"summary":     strings.TrimSpace(args.Summary),
			"description": description,
			"app_id":      appID,
		},
	}, &created); err != nil {
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(created.ID) == "" {
		return toolError(fmt.Errorf("proposal did not return an ID")), nil, nil
	}
	return textResult(fmt.Sprintf(
		"Raised a non-blocking app proposal (%s) in #%s. The human can Approve, Approve-with-note, or Reject; on approval the App Builder builds it. Keep working — do not block waiting for the decision.",
		created.ID, channel,
	)), nil, nil
}

func handleRegisterApp(ctx context.Context, _ *mcp.CallToolRequest, args RegisterAppArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return toolError(fmt.Errorf("name is required")), nil, nil
	}
	html, err := resolveRegisterAppHTML(args)
	if err != nil {
		return toolError(err), nil, nil
	}
	files, err := resolveRegisterAppFiles(args)
	if err != nil {
		return toolError(err), nil, nil
	}
	appID := strings.TrimSpace(args.AppID)
	if appID != "" {
		if err := validateCustomAppIDLocal(appID); err != nil {
			return toolError(err), nil, nil
		}
	}
	var result map[string]any
	if err := brokerPostJSON(ctx, "/apps", map[string]any{
		"id":          appID,
		"name":        name,
		"icon":        strings.TrimSpace(args.Icon),
		"summary":     strings.TrimSpace(args.Summary),
		"description": strings.TrimSpace(args.Description),
		"html":        html,
		"files":       files,
		"actor":       slug,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return toolError(fmt.Errorf("marshal register_app response: %w", err)), nil, nil
	}
	return textResult(string(payload)), nil, nil
}

// registerAppMaxHTMLBytes caps the bundle read from html_path (mirrors the
// broker store's own ceiling, so we fail here with a clear message rather than
// after a wasted round-trip).
const registerAppMaxHTMLBytes = 4 * 1024 * 1024

// resolveRegisterAppHTML returns the bundle to publish: the literal html when
// the agent passed one, otherwise the contents of html_path read by the broker.
// Reading the file here is the whole point — the minified single-file bundle has
// 100k+ char lines an agent can't reliably read back into a JSON string, so the
// agent passes a path and the broker (same filesystem) reads it directly.
func resolveRegisterAppHTML(args RegisterAppArgs) (string, error) {
	if html := strings.TrimSpace(args.HTML); html != "" {
		return args.HTML, nil
	}
	path := strings.TrimSpace(args.HTMLPath)
	if path == "" {
		return "", fmt.Errorf("provide html_path (absolute path to the built dist/index.html) or, as a fallback, html")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("html_path must be an absolute path to the built dist/index.html; got %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read html_path %q: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("html_path %q is a directory; point it at the built index.html", path)
	}
	if info.Size() > registerAppMaxHTMLBytes {
		return "", fmt.Errorf("built bundle is %d bytes, over the %d cap; trim it before publishing", info.Size(), registerAppMaxHTMLBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read html_path %q: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf("html_path %q is empty", path)
	}
	return string(data), nil
}

const (
	registerAppMaxSourceFiles     = 300
	registerAppMaxSourceFileBytes = 512 * 1024
	registerAppMaxSourceTotal     = 8 * 1024 * 1024
)

// registerAppSkipDirs are never persisted as source: build/install artifacts and
// VCS metadata. Skipping them (rather than relying on the agent to omit them) is
// what makes source_path a complete-but-not-bloated copy.
var registerAppSkipDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	".vite":        true,
	".git":         true,
}

// resolveRegisterAppFiles returns the source project to persist. When the agent
// passed source_path, the broker copies the WHOLE tree (minus build/VCS dirs) so
// the persisted source always builds — closing the "agent dropped a file from
// the map and shipped a broken app" gap. Falls back to an explicit files map.
func resolveRegisterAppFiles(args RegisterAppArgs) (map[string]string, error) {
	if len(args.Files) > 0 {
		return args.Files, nil
	}
	root := strings.TrimSpace(args.SourcePath)
	if root == "" {
		return nil, nil // html-only publish; no editable source persisted
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("source_path must be an absolute path to the project root; got %q", root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("read source_path %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source_path %q must be the project directory (with package.json + src/)", root)
	}
	files := make(map[string]string)
	var total int64
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && registerAppSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.Size() > registerAppMaxSourceFileBytes {
			return nil // skip oversized files (e.g. a giant lockfile); not app source
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(body)
		total += int64(len(body))
		if len(files) > registerAppMaxSourceFiles {
			return fmt.Errorf("source_path has more than %d files; is node_modules excluded?", registerAppMaxSourceFiles)
		}
		if total > registerAppMaxSourceTotal {
			return fmt.Errorf("source_path exceeds %d bytes", registerAppMaxSourceTotal)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("copy source_path %q: %w", root, walkErr)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("source_path %q has no source files", root)
	}
	return files, nil
}

// validateCustomAppIDLocal mirrors the broker's validateCustomAppID so the tool
// rejects a malformed id before a round-trip.
func validateCustomAppIDLocal(id string) error {
	if len(id) != len("app_")+16 || !strings.HasPrefix(id, "app_") {
		return fmt.Errorf("app_id must look like app_0123456789abcdef; got %q", id)
	}
	for _, ch := range strings.TrimPrefix(id, "app_") {
		if !((ch >= 'a' && ch <= 'f') || (ch >= '0' && ch <= '9')) {
			return fmt.Errorf("app_id must look like app_0123456789abcdef; got %q", id)
		}
	}
	return nil
}
