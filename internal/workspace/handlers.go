package workspace

import (
	"encoding/json"
	"net/http"
)

// RouteOptions controls optional side effects around workspace wipe routes.
type RouteOptions struct {
	AuthMiddleware func(http.HandlerFunc) http.HandlerFunc
	AfterShred     func(Result)
}

// RegisterRoutes attaches the two workspace wipe endpoints to mux.
//
//	POST /workspace/reset  — ClearRuntime (narrow: broker state only)
//	POST /workspace/shred  — Shred (full wipe, reopens onboarding)
//
// By default both endpoints only touch disk. RegisterRoutesWithOptions can add
// process-level side effects after a successful wipe response; the web broker
// uses that to stop itself after shred so live memory cannot repersist.
//
// authMiddleware wraps each handler. Pass the broker's requireAuth so local
// scripts cannot POST without the broker token — these operations are strictly
// more destructive than /config or /company, which are already auth-gated. Pass
// a nil middleware only in tests — RegisterRoutes substitutes a passthrough.
func RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.HandlerFunc) http.HandlerFunc) {
	RegisterRoutesWithOptions(mux, RouteOptions{AuthMiddleware: authMiddleware})
}

// RegisterRoutesWithOptions attaches the workspace wipe endpoints and runs any
// configured callbacks only after a successful wipe response has been written.
func RegisterRoutesWithOptions(mux *http.ServeMux, opts RouteOptions) {
	authMiddleware := opts.AuthMiddleware
	if authMiddleware == nil {
		authMiddleware = func(h http.HandlerFunc) http.HandlerFunc { return h }
	}
	mux.HandleFunc("/workspace/reset", authMiddleware(handleReset))
	mux.HandleFunc("/workspace/shred", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		handleShredWithOptions(w, r, opts)
	}))
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
	handleShredWithOptions(w, r, RouteOptions{})
}

func handleShredWithOptions(w http.ResponseWriter, r *http.Request, opts RouteOptions) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res, err := Shred()
	writeResult(w, res, err, "/")
	if err != nil || opts.AfterShred == nil {
		return
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	go opts.AfterShred(res)
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
