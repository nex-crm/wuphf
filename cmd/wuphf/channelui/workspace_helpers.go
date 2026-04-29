package channelui

import (
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/internal/team"
)

// SummarizeAwayRecovery renders the "while away" one-liner shown
// above the office feed when the human returns to a session with
// new activity. Picks the recovery focus + first next-step sentence
// (each trimmed via TrimRecoverySentence) and prepends the unread
// count when non-zero. Falls back to a generic prompt when both the
// focus and next-step are empty. Output is truncated to 120 columns.
func SummarizeAwayRecovery(unreadCount int, recovery team.SessionRecovery) string {
	parts := make([]string, 0, 3)
	if focus := TrimRecoverySentence(recovery.Focus); focus != "" {
		parts = append(parts, focus)
	}
	if len(recovery.NextSteps) > 0 {
		if next := TrimRecoverySentence(recovery.NextSteps[0]); next != "" {
			parts = append(parts, "Next: "+next)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d new since you looked. Open /recover for the full summary.", unreadCount)
	}
	summary := strings.Join(parts, " ")
	if unreadCount > 0 {
		summary = fmt.Sprintf("%d new since you looked. %s", unreadCount, summary)
	}
	return TruncateText(summary, 120)
}

// RuntimeRequestIsOpen reports whether a runtime request is in an
// open state (pending / open / draft / unknown). Closed states
// (answered / cancelled / resolved / etc.) return false.
func RuntimeRequestIsOpen(req team.RuntimeRequest) bool {
	status := strings.ToLower(strings.TrimSpace(req.Status))
	return status == "" || status == "pending" || status == "open" || status == "draft"
}

// FirstWorkspaceString returns the first non-empty trimmed value, or
// "" when none is found. Used to chain candidate next-step sentences
// where any of them might be empty.
func FirstWorkspaceString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// SidebarViewLabel maps an OfficeApp to a short human-friendly label
// used in the sidebar workspace summary line. Unknown / empty apps
// fall back to "Message lane".
func SidebarViewLabel(activeApp OfficeApp) string {
	switch activeApp {
	case OfficeAppRecovery:
		return "Recovery view"
	case OfficeAppTasks:
		return "Task board"
	case OfficeAppRequests:
		return "Decision queue"
	case OfficeAppPolicies:
		return "Insights view"
	case OfficeAppCalendar:
		return "Calendar view"
	case OfficeAppArtifacts:
		return "Artifacts view"
	case OfficeAppSkills:
		return "Skills view"
	default:
		return "Message lane"
	}
}

// FirstDoctorNextStep returns the first non-empty NextStep on a
// fail- or warn-severity check, or fallback when none is found.
// Used to surface the most urgent actionable hint in the readiness
// card without showing the full doctor report.
func FirstDoctorNextStep(report DoctorReport, fallback string) string {
	for _, check := range report.Checks {
		if strings.TrimSpace(check.NextStep) == "" {
			continue
		}
		if check.Severity == DoctorFail || check.Severity == DoctorWarn {
			return check.NextStep
		}
	}
	return fallback
}
