package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/api"
)

// ToolRegistry manages a set of named AgentTools.
type ToolRegistry struct {
	tools map[string]AgentTool
}

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]AgentTool)}
}

// Register adds or replaces a tool in the registry.
func (r *ToolRegistry) Register(tool AgentTool) {
	r.tools[tool.Name] = tool
}

// Unregister removes a tool from the registry.
func (r *ToolRegistry) Unregister(name string) {
	delete(r.tools, name)
}

// Get looks up a tool by name.
func (r *ToolRegistry) Get(name string) (AgentTool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tools.
func (r *ToolRegistry) List() []AgentTool {
	tools := make([]AgentTool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

// Has reports whether a tool with the given name is registered.
func (r *ToolRegistry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// Validate checks whether params are valid for the named tool.
// Checks: tool exists, required params present, no unknown params.
// Returns (true, nil) on success; (false, []errors) on failure.
func (r *ToolRegistry) Validate(toolName string, params map[string]any) (bool, []string) {
	tool, ok := r.tools[toolName]
	if !ok {
		return false, []string{fmt.Sprintf("unknown tool: %q", toolName)}
	}

	props := map[string]any{}
	if p, ok := tool.Schema["properties"].(map[string]any); ok {
		props = p
	}

	var errs []string

	if req, ok := tool.Schema["required"].([]any); ok {
		for _, v := range req {
			if name, ok := v.(string); ok {
				if _, present := params[name]; !present {
					errs = append(errs, fmt.Sprintf("missing required param: %q", name))
				}
			}
		}
	}

	for k := range params {
		if _, known := props[k]; !known {
			errs = append(errs, fmt.Sprintf("unknown param: %q", k))
		}
	}

	if len(errs) > 0 {
		return false, errs
	}
	return true, nil
}

// marshalResult marshals v to a JSON string.
func marshalResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(b), nil
}

type localToolResult struct {
	Path        string   `json:"path,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Command     string   `json:"command,omitempty"`
	WorkingDir  string   `json:"working_directory,omitempty"`
	Append      bool     `json:"append,omitempty"`
	Bytes       int      `json:"bytes,omitempty"`
	Lines       int      `json:"lines,omitempty"`
	Files       []string `json:"files,omitempty"`
	Matches     []string `json:"matches,omitempty"`
	MatchCount  int      `json:"match_count,omitempty"`
	FileCount   int      `json:"file_count,omitempty"`
	Recipient   string   `json:"recipient,omitempty"`
	Channel     string   `json:"channel,omitempty"`
	Message     string   `json:"message,omitempty"`
	Stdout      string   `json:"stdout,omitempty"`
	Stderr      string   `json:"stderr,omitempty"`
	Combined    string   `json:"combined,omitempty"`
	ExitCode    int      `json:"exit_code"`
	Status      string   `json:"status,omitempty"`
	Timestamp   string   `json:"timestamp,omitempty"`
	Description string   `json:"description,omitempty"`
}

func localToolDefinitions() []AgentTool {
	return []AgentTool{
		{
			Name:        "read_file",
			Description: "Read a local file from disk.",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"path"},
				"properties": map[string]any{
					"path":              map[string]any{"type": "string"},
					"working_directory": map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Reading file...")
				path, err := resolveToolPath(params)
				if err != nil {
					return "", err
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return "", err
				}
				return marshalResult(localToolResult{
					Path:       path,
					Bytes:      len(data),
					Lines:      countLines(string(data)),
					Status:     "ok",
					WorkingDir: resolvedWorkingDirectory(params),
					Combined:   string(data),
				})
			},
		},
		{
			Name:        "grep_search",
			Description: "Search local files for a regexp pattern.",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"pattern"},
				"properties": map[string]any{
					"pattern":           map[string]any{"type": "string"},
					"path":              map[string]any{"type": "string"},
					"working_directory": map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Searching files...")
				pattern, _ := params["pattern"].(string)
				if strings.TrimSpace(pattern) == "" {
					return "", fmt.Errorf("pattern is required")
				}
				re, err := regexp.Compile(pattern)
				if err != nil {
					return "", fmt.Errorf("compile pattern: %w", err)
				}
				root, err := resolveSearchRoot(params)
				if err != nil {
					return "", err
				}

				var matches []string
				fileSet := make(map[string]struct{})
				err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}
					if d.IsDir() {
						return nil
					}
					data, err := os.ReadFile(path)
					if err != nil {
						return nil
					}
					lines := strings.Split(string(data), "\n")
					for i, line := range lines {
						if re.MatchString(line) {
							rel, relErr := filepath.Rel(root, path)
							if relErr != nil {
								rel = path
							}
							matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, i+1, line))
							fileSet[path] = struct{}{}
						}
					}
					return nil
				})
				if err != nil {
					return "", err
				}

				files := make([]string, 0, len(fileSet))
				for path := range fileSet {
					rel, relErr := filepath.Rel(root, path)
					if relErr != nil {
						rel = path
					}
					files = append(files, rel)
				}

				return marshalResult(localToolResult{
					Pattern:    pattern,
					Path:       root,
					Matches:    matches,
					Files:      files,
					MatchCount: len(matches),
					FileCount:  len(files),
					Status:     "ok",
					WorkingDir: resolvedWorkingDirectory(params),
				})
			},
		},
		{
			Name:        "glob",
			Description: "Expand a filepath glob from the local filesystem.",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"pattern"},
				"properties": map[string]any{
					"pattern":           map[string]any{"type": "string"},
					"working_directory": map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Expanding glob...")
				pattern, _ := params["pattern"].(string)
				if strings.TrimSpace(pattern) == "" {
					return "", fmt.Errorf("pattern is required")
				}
				absPattern, err := resolvePath(resolvedWorkingDirectory(params), pattern)
				if err != nil {
					return "", err
				}
				files, err := filepath.Glob(absPattern)
				if err != nil {
					return "", err
				}
				base := resolvedWorkingDirectory(params)
				if base == "" {
					base, _ = os.Getwd()
				}
				relFiles := make([]string, 0, len(files))
				for _, file := range files {
					rel, relErr := filepath.Rel(base, file)
					if relErr != nil {
						rel = file
					}
					relFiles = append(relFiles, rel)
				}
				return marshalResult(localToolResult{
					Pattern:    pattern,
					WorkingDir: base,
					Files:      relFiles,
					FileCount:  len(relFiles),
					Status:     "ok",
				})
			},
		},
		{
			Name:        "write_file",
			Description: "Write or append local file content.",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"path", "content"},
				"properties": map[string]any{
					"path":              map[string]any{"type": "string"},
					"content":           map[string]any{"type": "string"},
					"append":            map[string]any{"type": "boolean"},
					"working_directory": map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Writing file...")
				path, err := resolveToolPath(params)
				if err != nil {
					return "", err
				}
				content, _ := params["content"].(string)
				appendMode, _ := params["append"].(bool)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return "", err
				}
				flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
				if appendMode {
					flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
				}
				f, err := os.OpenFile(path, flags, 0o644)
				if err != nil {
					return "", err
				}
				defer f.Close()
				if _, err := f.WriteString(content); err != nil {
					return "", err
				}
				return marshalResult(localToolResult{
					Path:       path,
					Append:     appendMode,
					Bytes:      len(content),
					Lines:      countLines(content),
					Status:     "ok",
					WorkingDir: resolvedWorkingDirectory(params),
				})
			},
		},
		{
			Name:        "bash",
			Description: "Run a local shell command and capture stdout/stderr.",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"command"},
				"properties": map[string]any{
					"command":           map[string]any{"type": "string"},
					"working_directory": map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Running bash...")
				command, _ := params["command"].(string)
				if strings.TrimSpace(command) == "" {
					return "", fmt.Errorf("command is required")
				}
				wd := resolvedWorkingDirectory(params)
				if wd == "" {
					wd, _ = os.Getwd()
				}
				cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", command)
				cmd.Dir = wd
				var stdout bytes.Buffer
				var stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				err := cmd.Run()
				exitCode := 0
				if err != nil {
					var exitErr *exec.ExitError
					if errors.As(err, &exitErr) {
						exitCode = exitErr.ExitCode()
					} else {
						return "", err
					}
				}
				if exitCode == 0 && cmd.ProcessState != nil {
					exitCode = cmd.ProcessState.ExitCode()
				}
				result := localToolResult{
					Command:     command,
					WorkingDir:  wd,
					Stdout:      stdout.String(),
					Stderr:      stderr.String(),
					Combined:    stdout.String() + stderr.String(),
					ExitCode:    exitCode,
					Lines:       countLines(stdout.String() + stderr.String()),
					Status:      "ok",
					Description: "Shell command completed",
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "send_message",
			Description: "Queue a lightweight agent-to-agent message on disk.",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"recipient", "message"},
				"properties": map[string]any{
					"recipient": map[string]any{"type": "string"},
					"message":   map[string]any{"type": "string"},
					"channel":   map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Sending message...")
				recipient, _ := params["recipient"].(string)
				message, _ := params["message"].(string)
				channel, _ := params["channel"].(string)
				if strings.TrimSpace(recipient) == "" {
					return "", fmt.Errorf("recipient is required")
				}
				if strings.TrimSpace(message) == "" {
					return "", fmt.Errorf("message is required")
				}
				home, err := os.UserHomeDir()
				if err != nil {
					return "", err
				}
				outboxDir := filepath.Join(home, ".wuphf", "office", "messages")
				if err := os.MkdirAll(outboxDir, 0o755); err != nil {
					return "", err
				}
				entry := localToolResult{
					Recipient: strings.TrimSpace(recipient),
					Channel:   strings.TrimSpace(channel),
					Message:   message,
					Status:    "queued",
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				}
				payload, err := json.Marshal(entry)
				if err != nil {
					return "", err
				}
				f, err := os.OpenFile(filepath.Join(outboxDir, "outbox.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
				if err != nil {
					return "", err
				}
				defer f.Close()
				if _, err := f.Write(append(payload, '\n')); err != nil {
					return "", err
				}
				return string(payload), nil
			},
		},
	}
}

func resolvedWorkingDirectory(params map[string]any) string {
	wd, _ := params["working_directory"].(string)
	return strings.TrimSpace(wd)
}

func resolveToolPath(params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	return resolvePath(resolvedWorkingDirectory(params), path)
}

func resolveSearchRoot(params map[string]any) (string, error) {
	if path, ok := params["path"].(string); ok && strings.TrimSpace(path) != "" {
		return resolvePath(resolvedWorkingDirectory(params), path)
	}
	wd := resolvedWorkingDirectory(params)
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(wd)
}

func resolvePath(base, target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target), nil
	}
	if strings.TrimSpace(base) == "" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(filepath.Join(base, target))
}

func countLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

// CreateBuiltinTools returns the 7 standard Nex tools backed by the API client.
func CreateBuiltinTools(client *api.Client) []AgentTool {
	tools := []AgentTool{
		{
			Name:        "nex_search",
			Description: "Search organizational knowledge base",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"query"},
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"limit": map[string]any{"type": "number"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Searching...")
				result, err := api.Post[any](client, "/search", map[string]any{
					"query": params["query"],
					"limit": params["limit"],
				}, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_ask",
			Description: "Ask a question to the Nex knowledge base",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"question"},
				"properties": map[string]any{
					"question": map[string]any{"type": "string"},
					"context":  map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Asking...")
				result, err := api.Post[any](client, "/ask", map[string]any{
					"question": params["question"],
					"context":  params["context"],
				}, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_remember",
			Description: "Store information in the Nex knowledge base",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"content"},
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
					"tags":    map[string]any{"type": "array"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Remembering...")
				result, err := api.Post[any](client, "/remember", map[string]any{
					"content": params["content"],
					"tags":    params["tags"],
				}, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_record_list",
			Description: "List records of a given object type",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"objectType"},
				"properties": map[string]any{
					"objectType": map[string]any{"type": "string"},
					"limit":      map[string]any{"type": "number"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Fetching records...")
				objectType, _ := params["objectType"].(string)
				path := "/records/" + objectType
				if limit, ok := params["limit"]; ok {
					path += fmt.Sprintf("?limit=%v", limit)
				}
				result, err := api.Get[any](client, path, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_record_get",
			Description: "Get a specific record by type and ID",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"objectType", "recordId"},
				"properties": map[string]any{
					"objectType": map[string]any{"type": "string"},
					"recordId":   map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Fetching record...")
				objectType, _ := params["objectType"].(string)
				recordId, _ := params["recordId"].(string)
				result, err := api.Get[any](client, "/records/"+objectType+"/"+recordId, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_record_create",
			Description: "Create a new record of a given object type",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"objectType", "properties"},
				"properties": map[string]any{
					"objectType": map[string]any{"type": "string"},
					"properties": map[string]any{"type": "object"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Creating record...")
				objectType, _ := params["objectType"].(string)
				result, err := api.Post[any](client, "/records/"+objectType, map[string]any{
					"properties": params["properties"],
				}, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_record_update",
			Description: "Update an existing record by type and ID",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"objectType", "recordId", "properties"},
				"properties": map[string]any{
					"objectType": map[string]any{"type": "string"},
					"recordId":   map[string]any{"type": "string"},
					"properties": map[string]any{"type": "object"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Updating record...")
				objectType, _ := params["objectType"].(string)
				recordId, _ := params["recordId"].(string)
				result, err := api.Patch[any](client, "/records/"+objectType+"/"+recordId, map[string]any{
					"properties": params["properties"],
				}, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
	}
	tools = append(tools, localToolDefinitions()...)
	return tools
}

// CreateGossipTools returns tools for publishing and querying the gossip network.
func CreateGossipTools(gossipLayer *GossipLayer, agentSlug string) []AgentTool {
	return []AgentTool{
		{
			Name:        "nex_gossip_publish",
			Description: "Publish an insight for other agents",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"insight"},
				"properties": map[string]any{
					"insight": map[string]any{"type": "string"},
					"context": map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Publishing insight...")
				insight, _ := params["insight"].(string)
				contextStr, _ := params["context"].(string)
				return gossipLayer.Publish(agentSlug, insight, contextStr)
			},
		},
		{
			Name:        "nex_gossip_query",
			Description: "Query gossip network for insights",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"topic"},
				"properties": map[string]any{
					"topic": map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Querying gossip network...")
				topic, _ := params["topic"].(string)
				insights, err := gossipLayer.Query(agentSlug, topic)
				if err != nil {
					return "", err
				}
				b, err := json.Marshal(insights)
				if err != nil {
					return "", err
				}
				return string(b), nil
			},
		},
	}
}
