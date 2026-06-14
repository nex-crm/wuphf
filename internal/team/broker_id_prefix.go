package team

// broker_id_prefix.go derives the Linear-style ID prefix used for new
// Issue IDs (e.g. "NEX" → NEX-1, NEX-2). The prefix is personal to each
// workspace: it comes from the workspace's company name, falling back to
// the workspace's own name, and only then to a neutral default. This keeps
// every workspace's task ids distinct instead of all reading "OFFICE-N".

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/workspaces"
)

// defaultIDPrefix is used when no company name is set yet (e.g. during
// onboarding before the human picks one) or when the registry isn't
// readable. Matches the historic "task" feel but stays short + uppercase.
const defaultIDPrefix = "OFFICE"

// idPrefixMaxLen caps the derived prefix so IDs stay scannable. Linear
// uses 3-letter prefixes by default; we go up to 5 to fit common short
// names like "ACME" or "STRIPE".
const idPrefixMaxLen = 5

// deriveIDPrefix normalises a company name into a Linear-style prefix.
// Strips non-letters, uppercases, truncates to idPrefixMaxLen. Empty or
// blank input returns defaultIDPrefix.
//
//	"Nex"          → "NEX"
//	"Acme Corp"    → "ACMEC" (would be "ACME" if we stayed under 4)
//	"a.b.c"        → "ABC"
//	"  "           → "OFFICE"
//	"!@#"          → "OFFICE"
func deriveIDPrefix(companyName string) string {
	var b strings.Builder
	for _, r := range companyName {
		if unicode.IsLetter(r) {
			b.WriteRune(unicode.ToUpper(r))
			if b.Len() >= idPrefixMaxLen {
				break
			}
		}
	}
	out := b.String()
	if out == "" {
		return defaultIDPrefix
	}
	return out
}

// workspaceIDPrefix picks a Linear-style prefix for a workspace, preferring
// the explicit company name (the brand the human picks during onboarding,
// e.g. "Nex" → NEX) and falling back to the workspace's own name so each
// workspace still gets a personal, scannable prefix. Candidates that carry
// no letters (and would only resolve to defaultIDPrefix) are skipped so a
// blank or symbol-only company name doesn't mask a usable workspace name.
// Returns "" when neither yields a real prefix, letting callers fall through.
func workspaceIDPrefix(companyName, workspaceName string) string {
	for _, candidate := range []string{companyName, workspaceName} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if p := deriveIDPrefix(candidate); p != defaultIDPrefix {
			return p
		}
	}
	return ""
}

// refreshIDPrefixFromWorkspaceLocked re-reads the workspace registry and
// updates b.idPrefix for this broker's runtime home. The prefix is derived
// from the workspace's company name, then its workspace name, then the local
// config's company name — only falling back to defaultIDPrefix when none is
// usable. Called on init and after onboarding company-name writes. Failures
// are non-fatal: a registry read error keeps the existing prefix (which falls
// back to defaultIDPrefix on first miss).
func (b *Broker) refreshIDPrefixFromWorkspaceLocked() {
	runtimeHome := config.RuntimeHomeDir()
	if reg, err := workspaces.Read(); err == nil && reg != nil {
		for _, ws := range reg.Workspaces {
			if ws == nil || ws.RuntimeHome != runtimeHome {
				continue
			}
			if p := workspaceIDPrefix(ws.CompanyName, ws.Name); p != "" {
				b.idPrefix = p
				return
			}
			break
		}
	}
	// No registry match (e.g. a single default instance not created through
	// the spaces orchestrator): derive from the local config's company name
	// before giving up on the neutral default.
	if cfg, err := config.Load(); err == nil {
		if p := workspaceIDPrefix(cfg.CompanyName, ""); p != "" {
			b.idPrefix = p
			return
		}
	}
	if b.idPrefix == "" {
		b.idPrefix = defaultIDPrefix
	}
}

// allocateIssueIDLocked mints the next Issue ID using the current
// prefix and the broker's monotonic counter. Caller is responsible for
// incrementing b.counter before calling. Returns e.g. "NEX-42".
func (b *Broker) allocateIssueIDLocked() string {
	prefix := b.idPrefix
	if prefix == "" {
		prefix = defaultIDPrefix
	}
	return fmt.Sprintf("%s-%d", prefix, b.counter)
}
