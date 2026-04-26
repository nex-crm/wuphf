// Package api provides the HTTP client for communicating with the Nex API.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

const defaultTimeout = 120 * time.Second

// Client is the Nex HTTP client.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// NewClient creates a Client with the given API key and default timeout.
func NewClient(apiKey string) *Client {
	return &Client{
		BaseURL:    config.APIBase(),
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
		Timeout:    defaultTimeout,
	}
}

// IsAuthenticated reports whether an API key has been set.
func (c *Client) IsAuthenticated() bool {
	return c.APIKey != ""
}

// SetAPIKey updates the API key on the client.
func (c *Client) SetAPIKey(key string) {
	c.APIKey = key
}

// request performs an HTTP request and decodes the JSON response into T.
func request[T any](c *Client, method, path string, body any, timeout time.Duration) (T, error) {
	var zero T

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return zero, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	t := c.Timeout
	if timeout > 0 {
		t = timeout
	}
	// The api.Client is invoked from many call sites that don't currently
	// thread a context. Use a per-request background context with the
	// configured timeout as the deadline — equivalent behaviour to the
	// previous c.HTTPClient.Timeout but satisfies noctx and lets the request
	// be cancelled when the deadline elapses. Don't write to
	// c.HTTPClient.Timeout: that's a shared field on a shared client and
	// concurrent callers (e.g. agent.AgentService.client) would race; the
	// per-request ctx deadline already enforces the same wall-clock bound.
	ctx, cancel := context.WithTimeout(context.Background(), t)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, path, reqBody)
	if err != nil {
		return zero, fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return zero, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, fmt.Errorf("read response body: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return zero, &AuthError{Message: string(respBytes)}
	case resp.StatusCode == http.StatusTooManyRequests:
		var retryAfter time.Duration
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				retryAfter = time.Duration(secs) * time.Second
			}
		}
		return zero, &RateLimitError{RetryAfter: retryAfter}
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return zero, &ServerError{Status: resp.StatusCode, Body: string(respBytes)}
	}

	if err := json.Unmarshal(respBytes, &zero); err != nil {
		return zero, fmt.Errorf("decode response: %w", err)
	}
	return zero, nil
}

// getRaw performs an HTTP GET and returns the raw response body as a string.
func (c *Client) getRaw(path string, timeout time.Duration) (string, error) {
	t := c.Timeout
	if timeout > 0 {
		t = timeout
	}
	// See comment in request[T] above: don't write c.HTTPClient.Timeout —
	// shared client, concurrent callers would race. Per-request ctx
	// deadline enforces the same bound.
	ctx, cancel := context.WithTimeout(context.Background(), t)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", &AuthError{Message: string(b)}
	case resp.StatusCode == http.StatusTooManyRequests:
		var retryAfter time.Duration
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				retryAfter = time.Duration(secs) * time.Second
			}
		}
		return "", &RateLimitError{RetryAfter: retryAfter}
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return "", &ServerError{Status: resp.StatusCode, Body: string(b)}
	}

	return string(b), nil
}

// Get performs a GET request and decodes JSON into T.
func Get[T any](c *Client, path string, timeout time.Duration) (T, error) {
	return request[T](c, http.MethodGet, c.BaseURL+path, nil, timeout)
}

// GetRaw performs a GET request and returns the raw response body.
func (c *Client) GetRaw(path string, timeout time.Duration) (string, error) {
	return c.getRaw(c.BaseURL+path, timeout)
}

// Post performs a POST request and decodes JSON into T.
func Post[T any](c *Client, path string, body any, timeout time.Duration) (T, error) {
	return request[T](c, http.MethodPost, c.BaseURL+path, body, timeout)
}

// Put performs a PUT request and decodes JSON into T.
func Put[T any](c *Client, path string, body any, timeout time.Duration) (T, error) {
	return request[T](c, http.MethodPut, c.BaseURL+path, body, timeout)
}

// Patch performs a PATCH request and decodes JSON into T.
func Patch[T any](c *Client, path string, body any, timeout time.Duration) (T, error) {
	return request[T](c, http.MethodPatch, c.BaseURL+path, body, timeout)
}

// Delete performs a DELETE request and decodes JSON into T.
func Delete[T any](c *Client, path string, timeout time.Duration) (T, error) {
	return request[T](c, http.MethodDelete, c.BaseURL+path, nil, timeout)
}

// Registration has moved off the legacy HTTP API. Callers that need to
// register a WUPHF user now shell out via internal/nex.Register, which
// drives the nex-cli binary. RegisterRequest is kept in this package for
// backwards compatibility with existing JSON callers only.
