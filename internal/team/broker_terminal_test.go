package team

import (
	"net/http/httptest"
	"testing"
)

func TestTerminalOriginAllowed(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		want   bool
	}{
		{"no origin", "", true},
		{"localhost dev server", "http://localhost:5173", true},
		{"loopback ipv4", "http://127.0.0.1:5173", true},
		{"loopback ipv6", "http://[::1]:5173", true},
		{"remote origin", "https://example.com", false},
		{"malformed origin", "://bad", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/terminal/agents/ceo", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if got := terminalOriginAllowed(req); got != tc.want {
				t.Fatalf("terminalOriginAllowed(%q) = %v, want %v", tc.origin, got, tc.want)
			}
		})
	}
}
