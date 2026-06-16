# App Builder — end-to-end tutorial (Phases 3–5)

A concrete ICP walkthrough used as the spec + grading oracle for the dyad-SOTA
phases. Persona: **Sam**, a solo founder running a RevOps-style office in WUPHF.
Every step lists the exact action and the exact expected output. The feature is
"done" only when all of these hold end-to-end.

## Scenario: Sam builds, iterates, and trusts a "Lead Scorer" tool

### 1. Build — and the gate keeps it honest (Phase 3)

1. Sam types `/create-app` → describes "a lead scorer that ranks inbound leads
   against our ICP weights and shows them in a table."
   **Expected:** a task `Build app: Lead Scorer` is created owned by `app-builder`;
   the task channel opens with a **live preview pane on the right** that is already
   showing a running scaffold within ~2s (not a multi-minute "Building…").
2. The App Builder narrates as it works and, before publishing, runs the verify
   gate.
   **Expected:** the agent runs `bun run verify` (`tsc --noEmit` then
   `vite build`); if it reports a `file:line:col` type error, the agent says what
   broke, fixes it, and re-runs — up to ~2 rounds. It does **not** call
   `register_app` until the gate passes. A genuinely broken build is reported as a
   blocker, never published.
3. On a clean gate the agent calls `register_app(app_id=…)`.
   **Expected:** "Lead Scorer" appears under **Apps** in the sidebar; opening it
   shows the live tool reading real office data through the bridge (no network).

### 2. Iterate by pointing at the thing (Phase 5)

4. Sam opens Lead Scorer (Live mode) and clicks **Select to edit** in the header.
   **Expected:** the button shows pressed (`aria-pressed=true`); hovering the
   preview outlines elements with a crosshair cursor.
5. Sam clicks the table's header row.
   **Expected:** the **Edit** dialog opens prefilled with a stub like
   ``Change the <th> at `App.tsx:42` ("Score"): `` — Sam only types the rest
   ("right-align the score column"). Submitting kicks off an `Improve app` task on
   the same app id. The click did **not** trigger the app's own behavior, and no
   broker write happened from the click itself.
6. While iterating, the generated app throws at runtime.
   **Expected:** instead of a blank frame, a dismissible error banner appears over
   the preview with the error message (text only, capped). Sam dismisses it.

### 3. Trust the tool — look before you leap (Phase 4)

7. After a couple of edits Lead Scorer is on v3. Sam clicks **History**.
   **Expected:** a version-timeline rail opens beside the preview listing v3 / v2 /
   v1 newest-first, each with who built it and when; v3 is marked **Current**.
8. Sam clicks **v1** to compare.
   **Expected:** the preview swaps to a **read-only** render of v1 with a banner:
   "Viewing v1 (read-only) · <author> · <relative time>" and two buttons —
   **Restore this version** and **Back to current v3**. The current build is
   untouched while previewing.
9. Sam clicks **Restore this version**.
   **Expected:** v1's bytes are re-published as a **new** v4 (append-only — the
   restore is itself reversible); a toast confirms "Restored — now on v4." History
   now shows v4 as current with v1–v3 still present.

### 4. Safety invariants (must hold throughout)

- The rendered app can only **read** a small allowlist of broker GETs via the
  postMessage bridge; it cannot reach the network, broker writes, the parent
  session, or any non-allowlisted path.
- The select-to-edit / error-capture messages are **display-only**: they never
  reach the broker and never auto-mutate anything (the human still submits the
  edit dialog).
- All the live-preview tooling (inspector, source stamping) is **dev-only** and is
  absent from the published single-file artifact.
- Restore and version snapshots are **append-only**; no edit can destroy a prior
  version.

## What "10/10" looks like per phase

- **Phase 3:** the gate is enforced in the builder prompt + the scaffold script,
  is bounded (~2 rounds), and a failing build blocks publish with a clear reason.
- **Phase 4:** timeline with who/when, non-destructive preview of any past
  version, append-only restore, all on real endpoints with graceful degradation
  for snapshots that predate the metadata.
- **Phase 5:** click→source resolution that works on the project's React version,
  survives the agent rewriting the entry file, is tree-shaken from the sealed
  build, adds no exfiltration surface, and opens a human-gated prefilled edit.
