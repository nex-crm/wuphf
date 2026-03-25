// Package api provides the HTTP client for communicating with the Nex API.
package api

import (
	"bytes"
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

	req, err := http.NewRequest(method, path, reqBody)
	if err != nil {
		return zero, fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	t := c.Timeout
	if timeout > 0 {
		t = timeout
	}
	c.HTTPClient.Timeout = t

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
	c.HTTPClient.Timeout = t

	req, err := http.NewRequest(http.MethodGet, path, nil)
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

// Register creates a new account. Does not require authentication.
// On success, sets the API key from the response if present.
func (c *Client) Register(email, name, companyName string) (map[string]interface{}, error) {
	payload := RegisterRequest{
		Email:       email,
		Name:        name,
		CompanyName: companyName,
	}
	result, err := request[map[string]interface{}](c, http.MethodPost, config.RegisterURL(), payload, 0)
	if err != nil {
		return nil, err
	}
	if key, ok := result["api_key"].(string); ok && key != "" {
		c.SetAPIKey(key)
	}
	return result, nil
}
