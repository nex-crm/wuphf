package api

import (
	"fmt"
	"time"
)

// AuthError is returned on 401 or 403 responses.
type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "authentication failed: invalid or missing API key"
}

// RateLimitError is returned on 429 responses.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded: retry after %s", e.RetryAfter)
}

// ServerError is returned on non-2xx responses not covered by AuthError or RateLimitError.
type ServerError struct {
	Status int
	Body   string
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("server error %d: %s", e.Status, e.Body)
}
