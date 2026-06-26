package action

import "strings"

// ConnectionState is the deterministic lifecycle state of an integration
// connection for a single platform within a single workspace. It is the spine
// of the connection resolver: every external action is classified against the
// state of the connection it needs BEFORE the action can run, so an unresolved
// connection can never reach the provider's execute call.
type ConnectionState string

const (
	// StateUnknown means the connection has never been probed (cache cold).
	StateUnknown ConnectionState = "unknown"
	// StateChecking means a probe is in flight.
	StateChecking ConnectionState = "checking"
	// StateConnected means an active connected account is present.
	StateConnected ConnectionState = "connected"
	// StatePending means an OAuth flow was started but has not completed.
	StatePending ConnectionState = "pending"
	// StateMissing means the platform is connectable (in the Composio catalog)
	// but no active account exists. Includes accounts that were disconnected.
	StateMissing ConnectionState = "missing"
	// StateFailed means an account exists but is unhealthy (revoked/expired).
	StateFailed ConnectionState = "failed"
	// StateUnsupported means the platform is not in the Composio catalog, so no
	// OAuth path exists — fallback (manual handoff) territory.
	StateUnsupported ConnectionState = "unsupported"
	// StateIndeterminate means the probe CALL itself failed (Composio API
	// unreachable: 5xx, timeout, network). It is distinct from missing/failed:
	// treating an outage as missing would manufacture false "connect" prompts
	// and recreate the dead-end this resolver exists to kill.
	StateIndeterminate ConnectionState = "indeterminate"
)

// MapConnectionState maps the provider-level connection state string (as
// produced by connectionState in composio.go: connected/pending/failed/
// disconnected/available/<raw>) onto a resolver ConnectionState. It only covers
// states derivable from a SUCCESSFUL probe of a known platform; StateUnsupported
// and StateIndeterminate are decided by the resolver from probe success and
// catalog membership, not from a per-connection state string.
func MapConnectionState(providerState string) ConnectionState {
	switch strings.ToLower(strings.TrimSpace(providerState)) {
	case "connected", "active", "enabled":
		return StateConnected
	case "pending", "initiated", "in_progress":
		return StatePending
	case "failed", "error":
		return StateFailed
	case "disconnected", "disabled", "inactive":
		// An account existed but is no longer usable: treat as needing a fresh
		// connection, which routes to the same "connect" decision as missing.
		return StateMissing
	case "available", "":
		return StateMissing
	default:
		// A successful probe returned a state we do not recognize. Conservative
		// default: not connected, so a mutating action is gated, never fired.
		return StateMissing
	}
}

// Decision is the resolver's verdict for a single external action attempt. It is
// the only thing the action gate acts on: there is no path from a non-connected
// state to provider.ExecuteAction for a mutating action.
type Decision string

const (
	// DecisionProceed runs the action with no human in the loop (read-only, or
	// connected + covered by a scoped grant).
	DecisionProceed Decision = "proceed"
	// DecisionApprove raises the dedicated external-action approval modal.
	DecisionApprove Decision = "approve"
	// DecisionConnect raises a typed connect decision (OAuth), then re-resolves.
	DecisionConnect Decision = "connect"
	// DecisionWait blocks briefly and re-resolves: the connection is mid-probe
	// (unknown/checking) or mid-OAuth (pending). Never falls through to proceed.
	DecisionWait Decision = "wait"
	// DecisionFailSafe means the provider API is unreachable: serve last-known-
	// good connected within TTL, else block-with-retry. Never downgrade to
	// connect, which would manufacture a false prompt during an outage.
	DecisionFailSafe Decision = "fail_safe"
	// DecisionFallback raises a manual-handoff decision for platforms with no
	// OAuth path (not in the Composio catalog).
	DecisionFallback Decision = "fallback"
)

// ClassifyInput is the deterministic input to Classify. It is intentionally a
// plain value with no I/O so the classification rules are exhaustively testable.
type ClassifyInput struct {
	// ReadOnly reports whether the action is a pure information read (no human
	// approval needed). Computed via ActionIsReadOnly.
	ReadOnly bool
	// State is the resolved connection state for the platform the action needs.
	State ConnectionState
	// HasGrant reports whether a live, in-scope, non-revoked grant already
	// authorizes this (agent, platform, action) so the modal can be skipped.
	HasGrant bool
}

// Classify is the pure heart of the resolver: given whether an action is
// read-only, the connection state, and whether a grant covers it, it returns
// exactly one Decision. Read-only does NOT bypass the connection gate — a read
// against an unconnected integration is meaningless and would fail reactively,
// so it routes through connect/wait/fail-safe/fallback like any other action.
// Read-only's only privilege is skipping the approval modal when connected.
func Classify(in ClassifyInput) Decision {
	switch in.State {
	case StateConnected:
		if in.ReadOnly || in.HasGrant {
			return DecisionProceed
		}
		return DecisionApprove
	case StateMissing, StateFailed:
		return DecisionConnect
	case StatePending, StateUnknown, StateChecking:
		return DecisionWait
	case StateIndeterminate:
		return DecisionFailSafe
	case StateUnsupported:
		return DecisionFallback
	default:
		// An unmodeled state must never proceed: block and re-resolve.
		return DecisionWait
	}
}

// ResolveInput combines a fresh probe with the last-known-good registry state so
// the resolver can decide deterministically even when the provider API is
// unreachable. It is pure data (no I/O) so the fail-safe logic — the bug-prone
// part — is exhaustively testable.
type ResolveInput struct {
	// ReadOnly reports whether the action is a pure information read.
	ReadOnly bool
	// Probed is the state returned by the live probe. Ignored when ProbeOK is
	// false.
	Probed ConnectionState
	// ProbeOK is false when the probe CALL itself failed (provider unreachable).
	ProbeOK bool
	// LastKnown is the registry's cached state for this platform, or
	// StateUnknown if the registry has no entry.
	LastKnown ConnectionState
	// LastKnownFresh reports whether LastKnown was verified within the staleness
	// TTL. Only a fresh, connected last-known state is trusted during an outage.
	LastKnownFresh bool
	// HasGrant reports whether a live, in-scope grant authorizes this action.
	HasGrant bool
}

// Resolve folds a fresh probe and the cached registry state into a single
// Decision plus the effective ConnectionState that produced it. When the probe
// call fails, it serves a fresh, connected last-known-good state so a provider
// outage does not manufacture a false "connect" prompt; otherwise it surfaces
// the outage as indeterminate (→ fail-safe, block-with-retry). It never trusts a
// stale or non-connected last-known state during an outage.
func Resolve(in ResolveInput) (Decision, ConnectionState) {
	effective := in.Probed
	if !in.ProbeOK {
		if in.LastKnown == StateConnected && in.LastKnownFresh {
			effective = StateConnected
		} else {
			effective = StateIndeterminate
		}
	}
	return Classify(ClassifyInput{ReadOnly: in.ReadOnly, State: effective, HasGrant: in.HasGrant}), effective
}

// readOnlyActionVerbs are unambiguous information-read verbs. Matched as WHOLE
// TOKENS (splitting action_id on - _ . / space), never as substrings —
// substring matching is too permissive (e.g. "get" inside "budget", "find"
// inside "findone_and_update"). Kept deliberately narrow: ambiguous nouns like
// "status", "count", "view", "query", "find", "summary" appear in both read and
// write action names and are excluded so mutating actions can never be
// misclassified.
//
// NOTE: this is the canonical home for action read/write classification. The
// copy in internal/teammcp/actions.go is superseded and is switched to call
// ActionIsReadOnly in slice 2.
var readOnlyActionVerbs = map[string]struct{}{
	"search":    {},
	"list":      {},
	"read":      {},
	"get":       {},
	"fetch":     {},
	"browse":    {},
	"describe":  {},
	"show":      {},
	"lookup":    {},
	"summarize": {},
}

// mutatingActionVerbs are unambiguous state-changing verbs. If ANY appears as a
// whole token in the action_id, the action is never classified read-only — even
// if a read verb is also present. Guards composite names like
// "GMAIL_LIST_AND_DELETE": a single mutating verb vetoes.
var mutatingActionVerbs = map[string]struct{}{
	"send": {}, "create": {}, "update": {}, "delete": {}, "post": {},
	"put": {}, "patch": {}, "remove": {}, "insert": {}, "write": {},
	"clear": {}, "reset": {}, "archive": {}, "star": {}, "unstar": {},
	"mark": {}, "publish": {}, "add": {}, "move": {}, "invite": {},
	"accept": {}, "reject": {}, "approve": {}, "cancel": {}, "refund": {},
	"charge": {}, "pay": {}, "enable": {}, "disable": {}, "revoke": {},
	"grant": {}, "set": {}, "draft": {}, "schedule": {}, "upload": {},
	"replace": {}, "transfer": {}, "merge": {}, "split": {},
}

func actionIDTokenBoundary(r rune) bool {
	return r == '-' || r == '_' || r == '.' || r == '/' || r == ' '
}

// ActionIsReadOnly reports whether an action_id is a pure information read,
// safe to run without human approval. True iff at least one read verb AND no
// mutating verb appears as a whole token. An empty action_id is not read-only.
func ActionIsReadOnly(actionID string) bool {
	id := strings.ToLower(strings.TrimSpace(actionID))
	if id == "" {
		return false
	}
	hasRead := false
	for _, tok := range strings.FieldsFunc(id, actionIDTokenBoundary) {
		if _, ok := mutatingActionVerbs[tok]; ok {
			return false
		}
		if _, ok := readOnlyActionVerbs[tok]; ok {
			hasRead = true
		}
	}
	return hasRead
}
