package gbrain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

const DefaultTimeout = 8 * time.Second

var ErrNotInstalled = errors.New("gbrain not installed")

type SearchResult struct {
	Slug        string  `json:"slug"`
	PageID      int     `json:"page_id"`
	Title       string  `json:"title"`
	Type        string  `json:"type"`
	ChunkText   string  `json:"chunk_text"`
	ChunkSource string  `json:"chunk_source"`
	ChunkID     int     `json:"chunk_id"`
	ChunkIndex  int     `json:"chunk_index"`
	Score       float64 `json:"score"`
	Stale       bool    `json:"stale"`
}

func BinaryPath() string {
	if candidate := strings.TrimSpace(os.Getenv("WUPHF_GBRAIN_COMMAND")); candidate != "" {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	if path, err := exec.LookPath("gbrain"); err == nil {
		return path
	}
	return ""
}

func IsInstalled() bool {
	return BinaryPath() != ""
}

func Run(ctx context.Context, args ...string) (string, error) {
	bin := BinaryPath()
	if bin == "" {
		return "", ErrNotInstalled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = gbrainEnv()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("gbrain %s: timeout after %s", strings.Join(args, " "), DefaultTimeout)
		}
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", fmt.Errorf("gbrain %s: %s", strings.Join(args, " "), detail)
		}
		return "", fmt.Errorf("gbrain %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func Call(ctx context.Context, tool string, params any) (string, error) {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return "", fmt.Errorf("gbrain call: tool is required")
	}
	payload, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	return Run(ctx, "call", tool, string(payload))
}

func Query(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	raw, err := Call(ctx, "query", map[string]any{
		"query":  query,
		"limit":  limit,
		"detail": "low",
	})
	if err != nil {
		return nil, err
	}
	var results []SearchResult
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		return nil, fmt.Errorf("decode gbrain query: %w", err)
	}
	return results, nil
}

func gbrainEnv() []string {
	env := os.Environ()
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(os.Getenv("HOME")) == "" {
		env = append(env, "HOME="+home)
	}
	if key := strings.TrimSpace(config.ResolveOpenAIAPIKey()); key != "" && strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
		env = append(env, "OPENAI_API_KEY="+key)
	}
	if key := strings.TrimSpace(config.ResolveAnthropicAPIKey()); key != "" && strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) == "" {
		env = append(env, "ANTHROPIC_API_KEY="+key)
	}
	return env
}
