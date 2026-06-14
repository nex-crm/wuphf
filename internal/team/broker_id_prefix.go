package team

// broker_id_prefix.go derives the Linear-style ID prefix used for new
// Issue IDs (e.g. "NEX" → NEX-1, NEX-2). The prefix comes from the
// workspace's company name; if absent, falls back to a neutral default.

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

// refreshIDPrefixFromWorkspaceLocked re-reads the workspace registry and
// updates b.idPrefix based on the current company name for this broker's
// runtime home. Called on init and after onboarding company-name writes.
// Failures are non-fatal — keeps the existing prefix (which falls back to
// defaultIDPrefix on first miss).
func (b *Broker) refreshIDPrefixFromWorkspaceLocked() {
	runtimeHome := config.RuntimeHomeDir()
	reg, err := workspaces.Read()
	if err != nil || reg == nil {
		if b.idPrefix == "" {
			b.idPrefix = defaultIDPrefix
		}
		return
	}
	for _, ws := range reg.Workspaces {
		if ws == nil {
			continue
		}
		if ws.RuntimeHome == runtimeHome {
			name := strings.TrimSpace(ws.CompanyName)
			if name == "" {
				if b.idPrefix == "" {
					b.idPrefix = defaultIDPrefix
				}
				return
			}
			b.idPrefix = deriveIDPrefix(name)
			return
		}
	}
	if b.idPrefix == "" {
		b.idPrefix = defaultIDPrefix
	}
}

// IDPrefix returns the active Linear-style ID prefix (e.g. "NEX", "OFFICE")
// under b.mu, falling back to defaultIDPrefix when unset. Exposed so surfaces
// that render task ids into links (e.g. the Slack thread reporter) can match
// the same "<PREFIX>-<digits>" shape the broker mints.
func (b *Broker) IDPrefix() string {
	if b == nil {
		return defaultIDPrefix
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if p := strings.TrimSpace(b.idPrefix); p != "" {
		return p
	}
	return defaultIDPrefix
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
