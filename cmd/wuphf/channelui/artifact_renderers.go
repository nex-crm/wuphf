package channelui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/team"
)

// RenderArtifactSection renders one labeled "Execution artifacts"
// subsection — date separator + per-artifact card. The first
// rendered line for a Task / TaskLog artifact carries TaskID; the
// first line for a Request artifact carries RequestID; this is
// what wires the click-target routing in the artifacts view.
func RenderArtifactSection(contentWidth int, title string, artifacts []team.RuntimeArtifact) []RenderedLine {
	if len(artifacts) == 0 {
		return nil
	}
	lines := []RenderedLine{{Text: ""}, {Text: RenderDateSeparator(contentWidth, title)}}
	for _, artifact := range artifacts {
		header, accent := RenderArtifactHeader(artifact)
		extra := ArtifactExtraLines(artifact)
		for i, line := range RenderRuntimeEventCard(contentWidth, header, artifact.EffectiveSummary(), accent, extra) {
			rendered := RenderedLine{Text: "  " + line}
			if (artifact.Kind == team.RuntimeArtifactTask || artifact.Kind == team.RuntimeArtifactTaskLog) && i == 0 {
				rendered.TaskID = artifact.ID
			}
			if artifact.Kind == team.RuntimeArtifactRequest && i == 0 {
				rendered.RequestID = artifact.ID
			}
			lines = append(lines, rendered)
		}
	}
	return lines
}

// RenderArtifactHeader returns the (header string, accent color)
// pair for one artifact row. The header is "<clock pill>
// <lifecycle pill> <bold title>"; the accent is the rounded-card
// border color picked by ArtifactAccentColor based on the
// artifact's State and the kind's default. TaskLog artifacts get
// a flat "log" accent pill since they don't have an interesting
// lifecycle.
func RenderArtifactHeader(artifact team.RuntimeArtifact) (string, string) {
	clock := SubtlePill(ArtifactClock(artifact.UpdatedAt, ParseArtifactTimestamp(artifact.UpdatedAt, artifact.StartedAt)), "#E2E8F0", "#0F172A")
	title := lipgloss.NewStyle().Bold(true).Render(artifact.EffectiveTitle())
	switch artifact.Kind {
	case team.RuntimeArtifactTask:
		return clock + " " + ArtifactLifecyclePill(artifact.State, "#0F766E", "#B45309", "#B91C1C", "#15803D") + " " + title, ArtifactAccentColor(artifact.State, "#0F766E")
	case team.RuntimeArtifactTaskLog:
		return clock + " " + AccentPill("log", "#0F766E") + " " + title, "#0F766E"
	case team.RuntimeArtifactWorkflowRun:
		return clock + " " + ArtifactLifecyclePill(artifact.State, "#7C3AED", "#B45309", "#B91C1C", "#15803D") + " " + title, ArtifactAccentColor(artifact.State, "#7C3AED")
	case team.RuntimeArtifactRequest:
		return clock + " " + ArtifactLifecyclePill(artifact.State, "#B45309", "#B45309", "#B91C1C", "#15803D") + " " + title, ArtifactAccentColor(artifact.State, "#B45309")
	case team.RuntimeArtifactExternalAction:
		return clock + " " + ArtifactLifecyclePill(artifact.State, "#1D4ED8", "#B45309", "#B91C1C", "#15803D") + " " + title, ArtifactAccentColor(artifact.State, "#1D4ED8")
	default:
		return clock + " " + ArtifactLifecyclePill(artifact.State, "#475569", "#B45309", "#B91C1C", "#15803D") + " " + title, ArtifactAccentColor(artifact.State, "#475569")
	}
}

// ArtifactExtraLines collects the optional extra rows under each
// artifact card: progress (when distinct from the summary), partial
// output, owner, channel, worktree, path, related-id, blocking
// flag, review/resume hints. Order is fixed; entries with empty
// trimmed strings are elided.
func ArtifactExtraLines(artifact team.RuntimeArtifact) []string {
	extra := []string{}
	if progress := strings.TrimSpace(artifact.EffectiveProgress()); progress != "" && !strings.EqualFold(progress, strings.TrimSpace(artifact.EffectiveSummary())) {
		extra = append(extra, "Progress: "+progress)
	}
	if output := strings.TrimSpace(artifact.PartialOutput); output != "" {
		extra = append(extra, "Output: "+output)
	}
	if owner := strings.TrimSpace(artifact.Owner); owner != "" {
		extra = append(extra, "@"+owner)
	}
	if channel := strings.TrimSpace(artifact.Channel); channel != "" {
		extra = append(extra, "#"+channel)
	}
	if worktree := strings.TrimSpace(artifact.Worktree); worktree != "" {
		extra = append(extra, "Worktree: "+worktree)
	}
	if path := strings.TrimSpace(artifact.Path); path != "" {
		extra = append(extra, "Path: "+path)
	}
	if related := strings.TrimSpace(artifact.RelatedID); related != "" {
		extra = append(extra, "Related: "+related)
	}
	if artifact.Blocking {
		extra = append(extra, "Blocking")
	}
	if hint := strings.TrimSpace(artifact.ReviewHint); hint != "" {
		extra = append(extra, hint)
	}
	if hint := strings.TrimSpace(artifact.ResumeHint); hint != "" {
		extra = append(extra, hint)
	}
	return extra
}
