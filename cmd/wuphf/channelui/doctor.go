package channelui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/team"
)

// DoctorSeverity classifies a doctor check's outcome. Used to drive
// the per-row pill color and the report's headline counts.
type DoctorSeverity string

const (
	DoctorOK   DoctorSeverity = "ok"
	DoctorWarn DoctorSeverity = "warn"
	DoctorFail DoctorSeverity = "fail"
	DoctorInfo DoctorSeverity = "info"
)

// DoctorCheck is one row of the doctor card: a label, a severity, the
// underlying capability lifecycle, and free-form Detail / NextStep
// text.
type DoctorCheck struct {
	Label     string
	Severity  DoctorSeverity
	Lifecycle team.CapabilityLifecycle
	Detail    string
	NextStep  string
}

// DoctorReport is the report rendered into the doctor card. It pairs
// the check rows with the capability registry they were derived from
// and the report's generation time.
type DoctorReport struct {
	GeneratedAt time.Time
	Checks      []DoctorCheck
	Registry    team.CapabilityRegistry
}

// Counts tallies the report's checks by severity. Reused by
// StatusLine and by readiness selectors that surface the worst row.
func (r DoctorReport) Counts() (ok, warn, fail int) {
	for _, check := range r.Checks {
		switch check.Severity {
		case DoctorOK:
			ok++
		case DoctorWarn:
			warn++
		case DoctorFail:
			fail++
		}
	}
	return ok, warn, fail
}

// StatusLine renders the human-friendly counts headline used at the
// top of the doctor card and in the readiness summary.
func (r DoctorReport) StatusLine() string {
	ok, warn, fail := r.Counts()
	switch {
	case fail > 0:
		return fmt.Sprintf("%d healthy · %d warning · %d blocked", ok, warn, fail)
	case warn > 0:
		return fmt.Sprintf("%d healthy · %d warning", ok, warn)
	default:
		return fmt.Sprintf("%d healthy · ready to work", ok)
	}
}

// DoctorSeverityForCapability maps a capability descriptor's level +
// lifecycle to the matching doctor severity. CapabilityWarn at the
// "needs setup" lifecycle escalates to DoctorFail because the user
// can't proceed without acting; everything else maps to its natural
// level.
func DoctorSeverityForCapability(entry team.CapabilityDescriptor) DoctorSeverity {
	switch entry.Level {
	case team.CapabilityReady:
		return DoctorOK
	case team.CapabilityWarn:
		switch entry.Lifecycle {
		case team.CapabilityLifecycleNeedsSetup:
			return DoctorFail
		default:
			return DoctorWarn
		}
	default:
		return DoctorInfo
	}
}

// RenderDoctorCard renders the rounded slate doctor card: a title +
// status pill + generated-at meta line, a one-line description, then
// one row per check (label pill, optional lifecycle pill, Detail body,
// optional "Next: …" hint), and a closing /cancel hint. cardWidth is
// clamped to a 48-column floor.
func RenderDoctorCard(report DoctorReport, width int) string {
	cardWidth := MaxInt(48, width)
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).Render("Doctor")
	meta := lipgloss.NewStyle().Foreground(lipgloss.Color(SlackMuted)).Render(report.GeneratedAt.Format("Jan 2 15:04"))
	lines := []string{
		title + "  " + SubtlePill(report.StatusLine(), "#E5E7EB", "#334155") + "  " + meta,
		MutedText("This is the live readiness check for setup, integrations, and the agent runtime."),
		"",
	}

	for _, check := range report.Checks {
		label := RenderDoctorLabel(check)
		if strings.TrimSpace(string(check.Lifecycle)) != "" {
			label += " " + RenderDoctorLifecycle(check.Lifecycle)
		}
		lines = append(lines, label+" "+check.Detail)
		if strings.TrimSpace(check.NextStep) != "" {
			lines = append(lines, "  "+MutedText("Next: "+check.NextStep))
		}
		lines = append(lines, "")
	}
	lines = append(lines, MutedText("Esc or /cancel closes this panel."))

	return lipgloss.NewStyle().
		Width(cardWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#334155")).
		Background(lipgloss.Color("#14151B")).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}

// RenderDoctorLabel returns the colored severity pill for a check.
// Info-level checks fall back to the neutral subtle pill.
func RenderDoctorLabel(check DoctorCheck) string {
	switch check.Severity {
	case DoctorOK:
		return AccentPill(check.Label, "#15803D")
	case DoctorWarn:
		return AccentPill(check.Label, "#B45309")
	case DoctorFail:
		return AccentPill(check.Label, "#B91C1C")
	default:
		return SubtlePill(check.Label, "#E2E8F0", "#334155")
	}
}

// RenderDoctorLifecycle returns the lifecycle pill that sits beside
// each check's severity pill. Underscores in the lifecycle name are
// rewritten as spaces for readability.
func RenderDoctorLifecycle(lifecycle team.CapabilityLifecycle) string {
	label := strings.ReplaceAll(string(lifecycle), "_", " ")
	switch lifecycle {
	case team.CapabilityLifecycleReady:
		return SubtlePill(label, "#DCFCE7", "#166534")
	case team.CapabilityLifecycleDisabled:
		return SubtlePill(label, "#E2E8F0", "#334155")
	case team.CapabilityLifecycleDeferred, team.CapabilityLifecyclePartial, team.CapabilityLifecycleProvisioning:
		return SubtlePill(label, "#FEF3C7", "#92400E")
	default:
		return SubtlePill(label, "#FEE2E2", "#991B1B")
	}
}
