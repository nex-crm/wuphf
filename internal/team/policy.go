package team

import (
	"fmt"
	"strings"
	"time"
)

// officeSignal is an internal audit record used by watchdog monitoring and
// relay event tracking. It is NOT used for policy generation.
type officeSignal struct {
	ID            string
	Source        string
	Kind          string
	Title         string
	Content       string
	Confidence    string
	Urgency       string
	Channel       string
	Owner         string
	RequiresHuman bool
	Blocking      bool
}

// officePolicy is a named operating rule for the office.
// Source is either "human_directed" (explicitly set by the human via message
// or command) or "auto_detected" (inferred from a recurring working pattern).
type officePolicy struct {
	ID        string `json:"id"`
	Source    string `json:"source"`   // "human_directed" | "auto_detected"
	Rule      string `json:"rule"`     // plain-English description of the rule
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at"`
}

func newOfficePolicy(source, rule string) officePolicy {
	rule = strings.TrimSpace(rule)
	source = strings.TrimSpace(source)
	if source == "" {
		source = "human_directed"
	}
	return officePolicy{
		ID:        fmt.Sprintf("policy-%d", time.Now().UnixNano()),
		Source:    source,
		Rule:      rule,
		Active:    true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}
