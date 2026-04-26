import type { ReactNode } from "react";

// Single source of truth for "what does shred actually do?". Any UI that
// warns about shred — banner buttons, danger-zone cards, confirm modals —
// pulls copy from here so the description never drifts from
// `internal/workspace/workspace.go:Shred`. When that function changes,
// update this file (and only this file).

const DELETIONS_PROSE =
  "your team, company identity, office task receipts, and saved workflows, plus local logs, sessions, provider state (including codex-headless scratch), calendar, and wiki memory";

const PRESERVED_PROSE =
  "Task worktrees, your global config and API keys, and your OpenClaw device identity are kept.";

export interface ShredWarningCopyProps {
  // Sentence describing what happens after the wipe — varies by call site
  // (e.g. "WUPHF will stop after the wipe; relaunch it to reopen onboarding."
  // for the inline restart banner vs. "Onboarding will reopen immediately."
  // for the Danger Zone modal where the broker is about to keep running).
  aftermath: ReactNode;
}

// ShredWarningCopy renders the canonical destructive-warning paragraph used
// in confirm modals. Pass `aftermath` for the trailing context sentence.
export function ShredWarningCopy({ aftermath }: ShredWarningCopyProps) {
  return (
    <>
      This permanently deletes {DELETIONS_PROSE}. {aftermath} {PRESERVED_PROSE}{" "}
      <strong>This cannot be undone.</strong>
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
