package teammcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/team"
)

var defaultBrokerTokenFile = brokeraddr.DefaultTokenFile

func reconfigureLiveOffice() error {
	if !team.HasLiveTmuxSession() {
		// Web mode: no tmux session to reconfigure. The broker state is already
		// updated, and the headless turn system picks up new members by slug.
		return nil
	}
	l, err := team.NewLauncher("")
	if err != nil {
		return err
	}
	return l.ReconfigureSession()
}

func brokerBaseURL() string {
	base := strings.TrimSpace(os.Getenv("WUPHF_TEAM_BROKER_URL"))
	if base == "" {
		base = strings.TrimSpace(os.Getenv("NEX_TEAM_BROKER_URL"))
	}
	if base == "" {
		base = brokeraddr.ResolveBaseURL()
	}
	return strings.TrimRight(base, "/")
}

func authHeaders() http.Header {
	headers := http.Header{}
	token := strings.TrimSpace(os.Getenv("WUPHF_BROKER_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("NEX_BROKER_TOKEN"))
	}
	if token == "" {
		token = readBrokerTokenFile()
	}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	// Identify the agent behind this MCP process so the broker can apply a
	// per-agent rate limit. A prompt-injected agent that loops on tool calls
	// will otherwise bypass the IP-scoped limiter because it holds the broker
	// token. Operator traffic from the web UI never sets this header.
	if slug := strings.TrimSpace(os.Getenv("WUPHF_AGENT_SLUG")); slug != "" {
		headers.Set("X-WUPHF-Agent", slug)
	} else if slug := strings.TrimSpace(os.Getenv("NEX_AGENT_SLUG")); slug != "" {
		headers.Set("X-WUPHF-Agent", slug)
	}
	return headers
}

func readBrokerTokenFile() string {
	path := strings.TrimSpace(os.Getenv("WUPHF_BROKER_TOKEN_FILE"))
	if path == "" {
		path = strings.TrimSpace(os.Getenv("NEX_BROKER_TOKEN_FILE"))
	}
	if path == "" {
		path = defaultBrokerTokenFile
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func isOneOnOneMode() bool {
	value := strings.TrimSpace(os.Getenv("WUPHF_ONE_ON_ONE"))
	return strings.EqualFold(value, "1") || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func resolveSlug(input string) (string, error) {
	if slug := strings.TrimSpace(resolveSlugOptional(input)); slug != "" {
		return slug, nil
	}
	return "", fmt.Errorf("missing agent slug; pass my_slug explicitly or set WUPHF_AGENT_SLUG")
}

func resolveSlugOptional(input string) string {
	if slug := strings.TrimSpace(input); slug != "" {
		return slug
	}
	if slug := strings.TrimSpace(os.Getenv("WUPHF_AGENT_SLUG")); slug != "" {
		return slug
	}
	return strings.TrimSpace(os.Getenv("NEX_AGENT_SLUG"))
}

func brokerGetJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, brokerBaseURL()+path, nil)
	if err != nil {
		return err
	}
	req.Header = authHeaders()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("broker GET %s failed: %s %s", path, res.Status, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// brokerGetRaw is like brokerGetJSON but returns the raw response body for
// endpoints that serve text/plain (the wiki read / list endpoints).
func brokerGetRaw(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, brokerBaseURL()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header = authHeaders()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4*1024*1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("broker GET %s failed: %s %s", path, res.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func brokerPostJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, brokerBaseURL()+path, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header = authHeaders()
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("broker POST %s failed: %s %s", path, res.Status, strings.TrimSpace(string(respBody)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func brokerPutJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, brokerBaseURL()+path, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header = authHeaders()
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("broker PUT %s failed: %s %s", path, res.Status, strings.TrimSpace(string(respBody)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
