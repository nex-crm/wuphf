package workspace

import (
	"encoding/json"
	"net/http"
)

// RouteOptions controls optional side effects around workspace wipe routes.
type RouteOptions struct {
	AuthMiddleware func(http.HandlerFunc) http.HandlerFunc
	ResetRuntime   func()
}

// RegisterRoutes attaches the two workspace wipe endpoints to mux.
//
//	POST /workspace/reset  — ClearRuntime (narrow: broker state only)
//	POST /workspace/shred  — Shred (full wipe, reopens onboarding)
//
// By default both endpoints only touch disk. RegisterRoutesWithOptions can add
// a live runtime reset after a successful wipe so the broker stays up without
// repersisting stale in-memory state.
//
// authMiddleware wraps each handler. Pass the broker's requireAuth so local
// scripts cannot POST without the broker token — these operations are strictly
// more destructive than /config or /company, which are already auth-gated. Pass
// a nil middleware only in tests — RegisterRoutes substitutes a passthrough.
func RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.HandlerFunc) http.HandlerFunc) {
	RegisterRoutesWithOptions(mux, RouteOptions{AuthMiddleware: authMiddleware})
}

// RegisterRoutesWithOptions attaches the workspace wipe endpoints and runs any
// configured callbacks after a successful disk wipe.
func RegisterRoutesWithOptions(mux *http.ServeMux, opts RouteOptions) {
	authMiddleware := opts.AuthMiddleware
	if authMiddleware == nil {
		authMiddleware = func(h http.HandlerFunc) http.HandlerFunc { return h }
	}
	mux.HandleFunc("/workspace/reset", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		handleResetWithOptions(w, r, opts)
	}))
	mux.HandleFunc("/workspace/shred", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		handleShredWithOptions(w, r, opts)
	}))
}

func handleReset(w http.ResponseWriter, r *http.Request) {
	handleResetWithOptions(w, r, RouteOptions{})
}

func handleResetWithOptions(w http.ResponseWriter, r *http.Request, opts RouteOptions) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res, err := ClearRuntime()
	if err == nil && opts.ResetRuntime != nil {
		opts.ResetRuntime()
	}
	writeResult(w, res, err, "/", opts.ResetRuntime == nil)
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
	if err == nil && opts.ResetRuntime != nil {
		opts.ResetRuntime()
	}
	writeResult(w, res, err, "/", opts.ResetRuntime == nil)
}

func writeResult(w http.ResponseWriter, res Result, err error, redirect string, restartRequired bool) {
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
		"restart_required": restartRequired,
		"redirect":         redirect,
		"removed":          res.Removed,
		"errors":           res.Errors,
	})
}
