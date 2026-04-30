package workspaces

// errors.go consolidates the typed sentinel errors the orchestrator returns
// so the broker (and any other caller) can centrally map them to HTTP status
// codes via errors.Is / errors.As. Some sentinels also live next to the code
// that creates them (slug.go, registry.go, ports.go, doctor_fix.go) — those
// are re-referenced or aliased here for discoverability rather than moved.
//
// Keep this list in lockstep with internal/team/broker_workspaces.go's
// errorToStatus mapper; adding a sentinel here without updating the mapper
// will silently fall back to 500.

import "errors"

// ErrWorkspaceConflict is returned when a workspace name collides with an
// already-registered entry on Create. Maps to 409.
//
// The orchestrator's Create currently returns a non-typed error for the
// dup-name case (see orchestrator.go::Create); the broker mapper falls back
// to a substring match until Create is rewrapped to use this sentinel.
var ErrWorkspaceConflict = errors.New("workspaces: workspace name conflict")

// ErrPortExhausted is the typed alias the broker mapper looks for when the
// port pool is full. The underlying sentinel is ErrPortPoolExhausted in
// ports.go; we re-export it under the broker-facing name so callers don't
// have to know the historical spelling.
var ErrPortExhausted = ErrPortPoolExhausted
