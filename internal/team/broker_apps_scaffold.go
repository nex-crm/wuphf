package team

// broker_apps_scaffold.go — pre-scaffold a new app the moment its build task is
// created, so the live preview boots a real running scaffold in seconds instead
// of showing minutes of "Building…" dead air.
//
// Both app-build entry points land here through MutateTask("create"):
//   - the explicit /create-app slash command (human → POST /tasks)
//   - the approved propose_app gate (broker → MutateTask in
//     maybeSpawnAppBuilderTaskFromProposal)
// Both format the title as "Build app: <name>" / "Create app: <name>", so a
// single hook covers them. "Improve"/"Update" titles are skipped — they already
// target an existing app the agent reads with get_app.

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// newAppBuildTitleRe matches a NEW-app build task title and captures the name.
// It deliberately excludes "improve"/"update" (those edit an existing app).
var newAppBuildTitleRe = regexp.MustCompile(`(?i)^\s*(?:build|create)\s+app:\s*(.+?)\s*$`)

// parseNewAppBuildTitle returns the app name when title is a NEW-app build task,
// or ("", false) otherwise.
func parseNewAppBuildTitle(title string) (string, bool) {
	m := newAppBuildTitleRe.FindStringSubmatch(title)
	if m == nil {
		return "", false
	}
	name := strings.TrimSpace(m[1])
	if name == "" {
		return "", false
	}
	return name, true
}

// maybePrescaffoldAppForCreate scaffolds the app for a new app-build task and
// appends a workspace brief (the pre-created app id + "publish with this id")
// to the task details. It is a cheap no-op for every non-app-builder create and
// degrades gracefully: any scaffold failure leaves the task untouched so the
// build still proceeds the old (slower) way rather than failing to start.
//
// Runs OUTSIDE b.mu (the app store has its own lock); callers invoke it before
// taking the broker lock.
func (b *Broker) maybePrescaffoldAppForCreate(action, channel string, body TaskPostRequest) TaskPostRequest {
	if !strings.EqualFold(strings.TrimSpace(action), "create") {
		return body
	}
	if !strings.EqualFold(strings.TrimSpace(body.Owner), appBuilderSlug) {
		return body
	}
	name, ok := parseNewAppBuildTitle(body.Title)
	if !ok {
		return body
	}
	// Already carries a workspace brief (e.g. a retried create) — don't append a
	// second one.
	if strings.Contains(body.Details, appWorkspaceBriefMarker) {
		return body
	}

	// Identity is (name, channel): re-issuing the same build continues the same
	// app instead of spawning a duplicate, and a deduped/retried create maps to
	// the same scaffold. No timestamp, so the id is stable across retries.
	slug := slugifyNotebookEntry(name)
	if slug == "" {
		slug = "app"
	}
	id := customAppID(slug, name, channel)

	actor := strings.TrimSpace(body.CreatedBy)
	if actor == "" {
		actor = appBuilderSlug
	}
	if _, err := b.appStore().Scaffold(id, name, "", actor, time.Now()); err != nil {
		// Pre-scaffold is an enhancement; never block task creation on it.
		return body
	}

	body.Details = strings.TrimRight(body.Details, "\n") + "\n\n" + appWorkspaceBrief(id)
	return body
}

// appBuilderTaskAppIDRe extracts the app id an App Builder task targets from its
// details prose. BOTH entry points name the id in a stable, parseable form:
//   - a NEW-app build appends "register_app(app_id=app_xxxx)" (appWorkspaceBrief)
//   - an IMPROVE task carries "register_app (app_id=app_xxxx)" and/or
//     "Improve the existing app `app_xxxx`" (composeAppBrief / appBuilderTaskBrief)
//
// We match the register_app form (optional space + optional quotes) because it
// is present in every app-builder task and is the canonical id the agent
// publishes under. The capture is the validated 16-hex app id shape.
var appBuilderTaskAppIDRe = regexp.MustCompile(`register_app\s*\(\s*app_id\s*=\s*["'` + "`" + `]?(app_[0-9a-f]{16})`)

// parseAppBuilderTaskAppID returns the target app id named in an App Builder
// task's details, or ("", false) when none is present (e.g. a malformed brief).
func parseAppBuilderTaskAppID(details string) (string, bool) {
	m := appBuilderTaskAppIDRe.FindStringSubmatch(details)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// stampAppEditChannelForTaskLocked records the App Builder task's channel as the
// owning app's persistent edit thread, so the FE can bind a per-app "chat to
// edit" panel to it. Runs for every app-builder task create AFTER its
// `task-<id>` channel is minted: it parses the target app id from the task
// details and stamps the channel onto that app's manifest.
//
// Best-effort and decoupled: the app store has its own lock, so this takes no
// broker state; a parse miss or unknown app is a silent no-op (the app just has
// no edit thread). Idempotent via the store (SetEditChannel no-ops when equal),
// so a retried create never churns the manifest. Skipped for the lobby channel —
// only a dedicated per-task channel is a usable edit thread (a human note in
// #general would not wake the owner).
func (b *Broker) stampAppEditChannelForTaskLocked(owner, channel, details string) {
	if !strings.EqualFold(strings.TrimSpace(owner), appBuilderSlug) {
		return
	}
	channel = strings.TrimSpace(channel)
	if channel == "" || channel == "general" {
		return
	}
	id, ok := parseAppBuilderTaskAppID(details)
	if !ok {
		return
	}
	// Tolerate "not found": an improve task can reference an app that was deleted
	// between create and now; nothing to bind then.
	_ = b.appStore().SetEditChannel(id, channel)
}

// appWorkspaceBriefMarker is a stable sentinel so the brief is appended at most
// once per task.
const appWorkspaceBriefMarker = "App workspace ready:"

// appWorkspaceBrief is the instruction appended to a pre-scaffolded app's task:
// build your version, then publish with this exact id so the live preview and
// version history stay on one app.
func appWorkspaceBrief(id string) string {
	return fmt.Sprintf(
		"%s a project for this app is already scaffolded and showing a LIVE preview as `%s`. "+
			"Build your version from the scaffold, then publish with register_app(app_id=%s) — "+
			"keep that exact id so the preview and version history stay on this one app. "+
			"Publish early and iterate; every register_app hot-reloads the live preview.",
		appWorkspaceBriefMarker, id, id,
	)
}
