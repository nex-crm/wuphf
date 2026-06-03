package onboarding

import (
	"context"
	"fmt"
)

// VerifyStatus classifies the outcome of a single runtime verification.
// It is the wire-facing classification the guided provider picker renders
// as a colored result with a hint.
//
// Only the states we can honestly detect today are defined here:
//   - "pass":          the runtime is installed and (when probed) signed in.
//   - "not_installed": the runtime binary was not found on PATH.
//   - "auth_required": the runtime is installed but the session probe
//     definitively reported not-signed-in (SessionProbed && !SignedIn).
//   - "other_error":   reserved for failures we cannot classify; not emitted
//     today, but kept so the wire contract can grow without a breaking change.
//
// payment_required and quota_exceeded are deliberately absent. We do not run
// a billable request during verification, so we cannot detect them, and
// fabricating a status we cannot stand behind would be worse than omitting it.
type VerifyStatus = string

const (
	// VerifyStatusPass means the runtime is ready to claim work.
	VerifyStatusPass VerifyStatus = "pass"
	// VerifyStatusNotInstalled means the binary is not on PATH.
	VerifyStatusNotInstalled VerifyStatus = "not_installed"
	// VerifyStatusAuthRequired means the binary is present but not signed in.
	VerifyStatusAuthRequired VerifyStatus = "auth_required"
	// VerifyStatusOtherError is reserved for unclassifiable failures.
	VerifyStatusOtherError VerifyStatus = "other_error"
)

// VerifyResult is the response shape for a single runtime verification.
// It reuses the detection primitives in prereqs.go (CheckOne plus the
// per-runtime session probe) and adds a human-facing hint plus the failed
// step so the guided picker can highlight exactly where setup stalled.
type VerifyResult struct {
	// Status is the classified outcome. See VerifyStatus.
	Status VerifyStatus `json:"status"`

	// Runtime echoes the runtime name that was verified.
	Runtime string `json:"runtime"`

	// Command is the suggested next command for the user to run. For
	// not_installed this is the install hint when one is known; for
	// auth_required it is the sign-in command. Empty on pass.
	Command string `json:"command,omitempty"`

	// SignInCommand is the runtime's sign-in command, surfaced separately
	// so the frontend can render a dedicated "Sign in" affordance. Set only
	// for auth_required.
	SignInCommand string `json:"sign_in_command,omitempty"`

	// Hint is one line of plain-language guidance for the classified status.
	// Empty on pass: a ready runtime needs no nudge.
	Hint string `json:"hint,omitempty"`

	// FailedStep names the guided step that did not pass, so the frontend
	// can highlight it in the InstallSteps list. Empty on pass.
	FailedStep string `json:"failed_step,omitempty"`

	// Version is the parsed --version string when the binary is present.
	Version string `json:"version,omitempty"`
}

// InstallStep is one numbered step in a runtime's guided setup. The frontend
// renders these as a checklist with a copyable command and an optional doc
// link.
type InstallStep struct {
	// Title is the short step heading (e.g. "Install Claude Code").
	Title string `json:"title"`

	// Detail is one line expanding on the step. Empty when the title says it all.
	Detail string `json:"detail,omitempty"`

	// Command is a copyable shell command for the step. Empty when the step
	// is a browser action or has no single command.
	Command string `json:"command,omitempty"`

	// LinkLabel is the label for the doc link (e.g. "Install guide").
	LinkLabel string `json:"link_label,omitempty"`

	// LinkURL is the doc/install URL for the step.
	LinkURL string `json:"link_url,omitempty"`
}

// installCommands maps a runtime to a copyable install command when one is
// well known. Runtimes installed through a desktop download (cursor,
// windsurf) have no single shell command, so they fall back to the doc link.
var installCommands = map[string]string{
	"claude":   "npm install -g @anthropic-ai/claude-code",
	"codex":    "npm install -g @openai/codex",
	"opencode": "npm install -g opencode-ai",
}

// VerifyRuntime checks a single runtime and classifies the result by reusing
// CheckOne and the per-runtime session probe.
//
// Classification rules, kept honest and in lockstep with PrePickScreen's
// issue #932 logic:
//   - not found              -> not_installed
//   - found, SessionProbed && !SignedIn -> auth_required
//   - found, otherwise (signed in, or no session probe wired) -> pass
//
// auth_required is only emitted when the session probe definitively reported
// not-signed-in. A runtime with no session probe (cursor, windsurf) is treated
// as pass once it is on PATH, matching the "we don't know, let the agent loop
// teach" stance the detection layer already takes.
func VerifyRuntime(ctx context.Context, name string) VerifyResult {
	r := CheckOne(ctx, name)

	res := VerifyResult{
		Runtime: name,
		Version: r.Version,
	}

	if !r.Found {
		res.Status = VerifyStatusNotInstalled
		res.FailedStep = fmt.Sprintf("Install %s", name)
		res.Command = installCommands[name]
		if res.Command != "" {
			res.Hint = fmt.Sprintf("%s is not on your PATH yet. Run the install command, then verify again.", name)
		} else {
			res.Hint = fmt.Sprintf("%s is not on your PATH yet. Install it from the linked guide, then verify again.", name)
		}
		return res
	}

	// Found. Only classify auth_required when the probe definitively said
	// not-signed-in. Anything else (signed in, or no probe wired) is a pass.
	if r.SessionProbed && !r.SignedIn {
		res.Status = VerifyStatusAuthRequired
		res.Command = r.SignInCommand
		res.SignInCommand = r.SignInCommand
		res.FailedStep = fmt.Sprintf("Sign in to %s", name)
		res.Hint = "Run the sign-in command, then verify again."
		return res
	}

	res.Status = VerifyStatusPass
	return res
}

// InstallSteps returns the guided setup steps for a CLI runtime. The steps
// walk the user from install to signed-in, with a copyable command per step
// and a doc link pointing at the runtime's canonical install page (reused
// from prereqSpecs). Returns nil only for unknown names. node and git are
// prerequisites rather than pickable runtimes, but they are present in
// prereqSpecs, so they still yield a generic step rather than nil.
func InstallSteps(name string) []InstallStep {
	spec, known := prereqSpecs[name]
	if !known {
		return nil
	}

	installCmd := installCommands[name]
	signInCmd := sessionSignInCommands[name]

	switch name {
	case "claude":
		return []InstallStep{
			{
				Title:     "Install Claude Code",
				Detail:    "One npm install and the CLI is on your PATH.",
				Command:   installCmd,
				LinkLabel: "Install guide",
				LinkURL:   spec.installURL,
			},
			{
				Title:   "Sign in to Claude",
				Detail:  "Sign in once and the office can run turns on your account.",
				Command: signInCmd,
			},
			{
				Title:  "Verify",
				Detail: "Press Verify and we confirm Claude is installed and signed in.",
			},
		}
	case "codex":
		return []InstallStep{
			{
				Title:     "Install Codex",
				Detail:    "Install the OpenAI Codex CLI globally.",
				Command:   installCmd,
				LinkLabel: "Install guide",
				LinkURL:   spec.installURL,
			},
			{
				Title:   "Sign in to Codex",
				Detail:  "Log in with ChatGPT or an API key. Either one works.",
				Command: signInCmd,
			},
			{
				Title:  "Verify",
				Detail: "Press Verify and we confirm Codex is installed and signed in.",
			},
		}
	case "opencode":
		return []InstallStep{
			{
				Title:     "Install opencode",
				Detail:    "Install the opencode CLI globally.",
				Command:   installCmd,
				LinkLabel: "Install guide",
				LinkURL:   spec.installURL,
			},
			{
				Title:   "Add a provider credential",
				Detail:  "Sign in so opencode has at least one provider to call.",
				Command: signInCmd,
			},
			{
				Title:  "Verify",
				Detail: "Press Verify and we confirm opencode has a stored credential.",
			},
		}
	case "cursor":
		return []InstallStep{
			{
				Title:     "Install Cursor",
				Detail:    "Download the Cursor app, then enable its command-line tool.",
				LinkLabel: "Download Cursor",
				LinkURL:   spec.installURL,
			},
			{
				Title:  "Sign in inside Cursor",
				Detail: "Cursor handles provider auth in the app, so there is no shell login.",
			},
			{
				Title:  "Verify",
				Detail: "Press Verify and we confirm the Cursor CLI is on your PATH.",
			},
		}
	case "windsurf":
		return []InstallStep{
			{
				Title:     "Install Windsurf",
				Detail:    "Download the Windsurf app, then enable its command-line tool.",
				LinkLabel: "Download Windsurf",
				LinkURL:   spec.installURL,
			},
			{
				Title:  "Sign in inside Windsurf",
				Detail: "Windsurf handles provider auth in the app, so there is no shell login.",
			},
			{
				Title:  "Verify",
				Detail: "Press Verify and we confirm the Windsurf CLI is on your PATH.",
			},
		}
	default:
		// node and git are prerequisites, not pickable runtimes. Hand back a
		// single generic step so a caller that asks anyway gets a doc link
		// rather than an empty list.
		return []InstallStep{
			{
				Title:     fmt.Sprintf("Install %s", name),
				Detail:    "This is a prerequisite, not a pickable runtime.",
				LinkLabel: "Install guide",
				LinkURL:   spec.installURL,
			},
		}
	}
}
