package team

import "strings"

// classifyActivityKind maps a (tool, status, detail) triple to the activity
// "kind" surfaced on agentActivitySnapshot.Kind. Two values are produced here:
//
//   - "milestone" — events the human in the office should notice ambient: an
//     error, a build/test/deploy run, or other user-visible progress.
//   - "routine"   — every other in-flight signal (text drafting, plain edits,
//     bookkeeping tool calls).
//
// "stuck" is intentionally NOT classified here. Stuck is a state-machine
// observation — the broker's stale-while-active reaper and watchdog alert
// hooks own the only emission paths into Kind="stuck". Letting the per-event
// classifier guess "stuck" would race the reaper and produce ping-pong
// stuck/routine flips on routine events that happen to mention "blocked".
//
// The match table is intentionally small and verb-led, sorted from most
// specific (status==error) to most generic (default). Frontend treats an
// empty string the same as "routine".
//
// TODO: tune from dogfood — the seed table is conservative; once we have a
// week of agent activity data we should pull common false-routine cases (e.g.
// "opened PR #N" via gh CLI) into the milestone bucket.
func classifyActivityKind(tool, status, detail string) string {
	statusLower := strings.ToLower(strings.TrimSpace(status))
	toolLower := strings.ToLower(strings.TrimSpace(tool))
	detailLower := strings.ToLower(strings.TrimSpace(detail))

	// Errors are always milestones — the human needs to see them ambient
	// without opening the workspace.
	if statusLower == "error" {
		return "milestone"
	}

	// Tool-name based matches. Build/test/deploy tools are milestone-worthy
	// regardless of detail. The MCP-style tool names we see in practice are
	// "Bash" (everything goes through bash) plus the file-edit tools; for
	// Bash we have to inspect the detail to decide.
	switch toolLower {
	case "bash":
		if detailContainsAny(detailLower, "test", "build", "deploy", "release", "publish", "make ", "npm run", "go test", "go build", "cargo test", "cargo build", "pytest", "vitest") {
			return "milestone"
		}
	case "grep", "glob", "find", "read", "ls":
		// Pure read tools never qualify as milestones on their own.
		return "routine"
	}

	// Detail-keyword fallback. Catches cases where the tool field is empty
	// or generic (e.g. headless launchers pass activity="tool" + a detail
	// string like "running go test"). Same milestone keywords as the Bash
	// branch above so behaviour is consistent regardless of launcher.
	if detailContainsAny(detailLower,
		"deploy", "released", "published",
		"merged pr", "opened pr",
		"running test", "running build",
		"go test", "go build",
		"npm run", "cargo test", "cargo build",
		"pytest", "vitest",
	) {
		return "milestone"
	}

	return "routine"
}

// detailContainsAny returns true if haystack contains any of the substrings.
// Caller is expected to lowercase haystack already.
func detailContainsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if n == "" {
			continue
		}
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
