package action

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

func workflowStoreDir(provider string) string {
	base := filepath.Join(filepath.Dir(config.ConfigPath()), "workflows")
	if p := sanitizeWorkflowKey(provider); p != "" {
		return filepath.Join(base, p)
	}
	return base
}

func workflowDefinitionPath(provider, key string) string {
	return filepath.Join(workflowStoreDir(provider), sanitizeWorkflowKey(key)+".json")
}

func workflowRunsPath(provider, key string) string {
	return filepath.Join(workflowStoreDir(provider), sanitizeWorkflowKey(key)+".runs.jsonl")
}

func saveWorkflowDefinition(provider, key string, definition json.RawMessage) (string, error) {
	path := workflowDefinitionPath(provider, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if !json.Valid(definition) {
		return "", fmt.Errorf("workflow definition must be valid JSON")
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, definition, "", "  "); err != nil {
		return "", err
	}
	pretty.WriteByte('\n')
	if err := os.WriteFile(path, pretty.Bytes(), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func loadWorkflowDefinition(provider, keyOrPath string) (string, json.RawMessage, string, error) {
	candidate := strings.TrimSpace(keyOrPath)
	if candidate == "" {
		return "", nil, "", fmt.Errorf("workflow key or path is required")
	}
	path := candidate
	if info, err := os.Stat(candidate); err != nil || info.IsDir() {
		path = workflowDefinitionPath(provider, candidate)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, "", err
	}
	key := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	key = strings.TrimSuffix(key, ".runs")
	return key, json.RawMessage(raw), path, nil
}

func appendWorkflowRun(provider, key string, run any) error {
	path := workflowRunsPath(provider, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(run); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func listWorkflowRuns(provider, key string) (WorkflowRunsResult, error) {
	path := workflowRunsPath(provider, key)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return WorkflowRunsResult{}, nil
		}
		return WorkflowRunsResult{}, err
	}
	defer func() { _ = f.Close() }()

	var runs []json.RawMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		runs = append(runs, json.RawMessage(append([]byte(nil), line...)))
	}
	if err := scanner.Err(); err != nil {
		return WorkflowRunsResult{}, err
	}
	return WorkflowRunsResult{Runs: runs}, nil
}

func sanitizeWorkflowKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return "workflow"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "workflow"
	}
	return out
}
