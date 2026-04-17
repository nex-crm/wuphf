package workspace

import (
	"encoding/json"
	"net/http"
)

// RegisterRoutes attaches the two workspace wipe endpoints to mux.
//
//	POST /workspace/reset  — ClearRuntime (narrow: broker state only)
//	POST /workspace/shred  — Shred (full wipe, reopens onboarding)
//
// Both endpoints only touch disk. They do NOT kill the running broker process,
// so callers must surface the "restart_required" hint — the web UI reloads
// the tab, the TUI quits the session, the CLI is already the restart path.
//
// authMiddleware wraps each handler. Pass the broker's requireAuth so local
// scripts cannot POST without the broker token — these operations are strictly
// more destructive than /config or /company, which are already auth-gated. Pass
// a nil middleware only in tests — RegisterRoutes substitutes a passthrough.
func RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.HandlerFunc) http.HandlerFunc) {
	if authMiddleware == nil {
		authMiddleware = func(h http.HandlerFunc) http.HandlerFunc { return h }
	}
	mux.HandleFunc("/workspace/reset", authMiddleware(handleReset))
	mux.HandleFunc("/workspace/shred", authMiddleware(handleShred))
}

func handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res, err := ClearRuntime()
	writeResult(w, res, err, "/")
}

func handleShred(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res, err := Shred()
	writeResult(w, res, err, "/")
}

func writeResult(w http.ResponseWriter, res Result, err error, redirect string) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":               true,
		"restart_required": true,
		"redirect":         redirect,
		"removed":          res.Removed,
		"errors":           res.Errors,
	})
}
