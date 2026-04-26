// Single source of truth for "what does shred actually do?". Any UI that
// warns about shred — banner buttons, danger-zone cards, confirm modals —
// pulls copy from here so the description never drifts from
// `internal/workspace/workspace.go:Shred`. When that function changes,
// update this file (and only this file).

// `internal/workspace/workspace.go:Shred` removes the entire `~/.wuphf/office`
// directory, not just task receipts — narrowing the prose here would give
// users the wrong impression that other office state survives.
const DELETIONS_PROSE =
  "your team, company identity, office state, and saved workflows, plus local logs, sessions, provider state (including codex-headless scratch), calendar, and wiki memory";

// Aftermath copy is a single canonical sentence used by every shred surface
// (inline banner, Danger Zone modal, danger-zone card subtitle). Both call
// sites trigger the same `/workspace/shred` → `b.Reset` flow on the broker
// (see `internal/team/broker.go` `RouteOptions{ResetRuntime: b.Reset}`), so
// they share user-observable behavior — the prose should match too.
const AFTERMATH_PROSE = "Onboarding will reopen immediately.";

const PRESERVED_PROSE =
  "Task worktrees, your global config and API keys, and your OpenClaw device identity are kept.";

// ShredWarningCopy renders the canonical destructive-warning paragraph used
// in confirm modals. No props by design — keeping the copy uniform across
// every shred surface is the whole point of this module.
export function ShredWarningCopy() {
  return (
    <>
      This permanently deletes {DELETIONS_PROSE}. {AFTERMATH_PROSE}{" "}
      {PRESERVED_PROSE} <strong>This cannot be undone.</strong>
    </>
  );
}

// ShredCardSubtitle renders the short summary used at the top of the Danger
// Zone shred card (above the bullet lists). Keeps the prose anchored to the
// same source of truth as the modal intro.
export function ShredCardSubtitle() {
  return (
    <>
      Full wipe. Deletes {DELETIONS_PROSE}, then returns you to onboarding. Use
      this to start completely fresh or to try a different blueprint.
    </>
  );
}

// ShredDeletionsList renders the canonical "Deletes" bullet list — used by
// the Danger Zone card to spell out exactly which files/dirs are removed.
export function ShredDeletionsList() {
  return (
    <>
      <li>
        Onboarding flag (<code>~/.wuphf/onboarded.json</code>) so the wizard
        reopens
      </li>
      <li>
        Company identity (<code>~/.wuphf/company.json</code>)
      </li>
      <li>
        Team runtime state, office, and workflows under <code>~/.wuphf/</code>
      </li>
      <li>
        Logs, sessions, provider state (incl. <code>codex-headless</code>{" "}
        scratch), calendar, and local wiki memory
      </li>
      <li>Broker runtime state (same as Reset)</li>
    </>
  );
}

// ShredPreservationList renders the canonical "Preserved" bullet list. Mirrors
// the survivors documented in `internal/workspace/workspace.go` (task-worktrees,
// config.json, openclaw/).
export function ShredPreservationList() {
  return (
    <>
      <li>
        <strong>Task worktrees</strong> — uncommitted work on branches stays on
        disk
      </li>
      <li>
        Your global config (<code>config.json</code>) and API keys
      </li>
      <li>OpenClaw device identity used for gateway pairing</li>
    </>
  );
}
