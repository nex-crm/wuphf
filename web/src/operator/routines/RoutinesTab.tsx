// RoutinesTab — the agent's workflows, Claude-Routines style: each routine is a
// PROMPT the agent runs in its own chat on a schedule. No compiled diagram —
// the chat (which knows the agent's tools) is the runtime. Disable and Publish
// new version live on EACH routine, not on the agent. With a REAL agent id
// (app_…) routines persist via the agent service (/agent proxy) — create,
// enable/disable, prompt drafts, publish, and Run now all round-trip; when the
// service is unreachable the tab falls back to the local seeded state so the
// FE keeps working offline. See docs/specs/operator-agent-routines.md.

import { useEffect, useState } from "react";
import {
  CalendarClock,
  CheckCircle2,
  MessageSquareText,
  Play,
  Plus,
  Power,
} from "lucide-react";

import {
  tryCreateRoutine,
  tryListRoutines,
  tryPatchRoutine,
  tryRunRoutineNow,
  type WireRoutine,
} from "../agents/agentStateClient";
import { isRealAppId } from "../apps/useOperatorApps";
import { Eyebrow } from "../components/primitives";
import { newRoutine, type Routine, seedRoutines } from "./routines";

interface RoutinesTabProps {
  agentName: string;
  /** Real agent id (app_…). When set, routines persist via the agent service;
   * without it (mock agents) the tab keeps its local seeded state. */
  agentId?: string;
  /** Open the routine's chat session in the Ask Agent dock. */
  onOpenSession?: (sessionId: string, title: string) => void;
}

// The wire routine carries an extra `agent` field; the FE shape is the rest.
function fromWire(w: WireRoutine): Routine {
  return {
    id: w.id,
    name: w.name,
    prompt: w.prompt,
    schedule: w.schedule,
    enabled: w.enabled,
    version: w.version,
    draft: w.draft,
    lastRun: w.lastRun,
    sessionId: w.sessionId,
  };
}

export function RoutinesTab({
  agentName,
  agentId,
  onOpenSession,
}: RoutinesTabProps) {
  const [routines, setRoutines] = useState<Routine[]>(() => seedRoutines());
  // True once the agent service answered a list — from then on writes go to it.
  const [live, setLive] = useState(false);
  const [name, setName] = useState("");
  const [prompt, setPrompt] = useState("");
  const [schedule, setSchedule] = useState("Every Monday 9:00");
  // Run-now feedback: which routine is mid-run, and which just finished.
  const [runningId, setRunningId] = useState<string | null>(null);
  const [ranJustNowId, setRanJustNowId] = useState<string | null>(null);

  const realId = isRealAppId(agentId) ? agentId : undefined;

  useEffect(() => {
    if (!realId) return;
    let cancelled = false;
    void tryListRoutines(realId).then((remote) => {
      if (cancelled || !remote) return; // unreachable — keep the seeded state
      setLive(true);
      setRoutines(remote.map(fromWire));
    });
    return () => {
      cancelled = true;
    };
  }, [realId]);

  function patch(id: string, up: (r: Routine) => Routine) {
    setRoutines((prev) => prev.map((r) => (r.id === id ? up(r) : r)));
  }

  function toggleEnabled(r: Routine) {
    const local = () => patch(r.id, (x) => ({ ...x, enabled: !x.enabled }));
    if (!(live && realId)) {
      local();
      return;
    }
    void tryPatchRoutine(r.id, { agent: realId, enabled: !r.enabled }).then(
      (updated) => (updated ? patch(r.id, () => fromWire(updated)) : local()),
    );
  }

  // A prompt edit is a local draft while typing; blur persists it as a draft
  // on the service (the service keeps vN published until Publish).
  function savePromptDraft(r: Routine) {
    if (!(live && realId && r.draft)) return;
    void tryPatchRoutine(r.id, { agent: realId, prompt: r.prompt }).then(
      (updated) => {
        if (updated) patch(r.id, () => fromWire(updated));
        // Unreachable: the draft stays local — publish will retry the write.
      },
    );
  }

  function publish(r: Routine) {
    const local = () =>
      patch(r.id, (x) => ({ ...x, version: x.version + 1, draft: false }));
    if (!(live && realId)) {
      local();
      return;
    }
    // Send the current prompt with publish so an unsaved draft can't race the
    // freeze (blur and click can interleave).
    void tryPatchRoutine(r.id, {
      agent: realId,
      prompt: r.prompt,
      publish: true,
    }).then((updated) =>
      updated ? patch(r.id, () => fromWire(updated)) : local(),
    );
  }

  // Run now — runs the routine's prompt through the agent, gated server-side.
  async function runNow(r: Routine) {
    if (runningId) return;
    setRunningId(r.id);
    setRanJustNowId(null);
    try {
      if (live && realId) {
        const outcome = await tryRunRoutineNow(r.id, realId);
        if (outcome) {
          const remote = await tryListRoutines(realId);
          if (remote) setRoutines(remote.map(fromWire));
          else patch(r.id, () => fromWire(outcome.routine));
          setRanJustNowId(r.id);
          return;
        }
      }
      // Offline / mock agent: record the run locally so the row reflects it.
      patch(r.id, (x) => ({ ...x, lastRun: "just now" }));
      setRanJustNowId(r.id);
    } finally {
      setRunningId(null);
    }
  }

  function add() {
    const p = prompt.trim();
    if (!p) return;
    const n = name.trim() || p.slice(0, 40);
    const clear = () => {
      setName("");
      setPrompt("");
    };
    if (live && realId) {
      void tryCreateRoutine({
        agent: realId,
        name: n,
        prompt: p,
        schedule,
      }).then((created) => {
        setRoutines((prev) => [
          ...prev,
          created ? fromWire(created) : newRoutine(n, p, schedule),
        ]);
      });
      clear();
      return;
    }
    setRoutines((prev) => [...prev, newRoutine(n, p, schedule)]);
    clear();
  }

  return (
    <div className="opr-tool-scoped opr-routines">
      <div className="opr-data-intro">
        <Eyebrow>Routines</Eyebrow>
        <p className="opr-scoped-note">
          A routine is a prompt {agentName} runs in its own chat on a schedule —
          it uses the agent's tools and its outcomes land in Artifacts. Pause or
          publish each routine on its own.
        </p>
      </div>

      <div className="opr-routine-list">
        {routines.map((r) => (
          <div
            key={r.id}
            className={`opr-routine${r.enabled ? "" : " is-disabled"}`}
          >
            <div className="opr-routine-head">
              <span className="opr-routine-name">{r.name}</span>
              <span className="opr-routine-version">
                v{r.version}
                {r.draft ? " · draft" : ""}
              </span>
              <span className="opr-routine-schedule">
                <CalendarClock size={11} strokeWidth={2} aria-hidden={true} />
                {r.schedule}
              </span>
              <span className="opr-routine-lastrun">
                {ranJustNowId === r.id
                  ? "ran just now"
                  : r.enabled
                    ? r.lastRun
                      ? `last ran ${r.lastRun}`
                      : "not run yet"
                    : "paused"}
              </span>
            </div>

            <textarea
              className="opr-routine-prompt"
              aria-label={`Prompt for ${r.name}`}
              value={r.prompt}
              rows={2}
              onChange={(e) =>
                patch(r.id, (x) => ({
                  ...x,
                  prompt: e.target.value,
                  draft: true,
                }))
              }
              onBlur={() => savePromptDraft(r)}
            />

            <div className="opr-routine-actions">
              <button
                type="button"
                className="opr-btn opr-btn-sm"
                onClick={() => toggleEnabled(r)}
              >
                <Power size={12} strokeWidth={2} aria-hidden={true} />
                {r.enabled ? "Disable" : "Enable"}
              </button>
              <button
                type="button"
                className="opr-btn opr-btn-primary opr-btn-sm"
                disabled={!r.draft}
                title={
                  r.draft
                    ? "Freeze the edited prompt as the next version"
                    : "No changes since the last publish"
                }
                onClick={() => publish(r)}
              >
                <CheckCircle2 size={12} strokeWidth={2} aria-hidden={true} />
                Publish new version
              </button>
              <button
                type="button"
                className="opr-btn opr-btn-sm"
                disabled={runningId !== null}
                title="Run this routine's prompt through the agent now"
                onClick={() => void runNow(r)}
              >
                <Play size={12} strokeWidth={2} aria-hidden={true} />
                {runningId === r.id ? "Running…" : "Run now"}
              </button>
              <button
                type="button"
                className="opr-btn opr-btn-ghost opr-btn-sm"
                onClick={() => onOpenSession?.(r.sessionId, r.name)}
              >
                <MessageSquareText
                  size={12}
                  strokeWidth={2}
                  aria-hidden={true}
                />
                Open its chat
              </button>
            </div>
          </div>
        ))}
      </div>

      <div className="opr-routine-new">
        <div className="opr-tool-teach-label">
          <Plus size={12} strokeWidth={2} aria-hidden={true} />
          New routine
        </div>
        <div className="opr-routine-new-grid">
          <input
            className="opr-composer-input"
            aria-label="Routine name"
            placeholder="Name (optional)"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <select
            className="opr-conn-select"
            aria-label="Schedule"
            value={schedule}
            onChange={(e) => setSchedule(e.target.value)}
          >
            <option>Every Monday 9:00</option>
            <option>Weekdays 8:00</option>
            <option>Every day 18:00</option>
            <option>Every 30 minutes</option>
            <option>Every hour</option>
          </select>
        </div>
        <div className="opr-composer">
          <input
            className="opr-composer-input"
            aria-label="Routine prompt"
            placeholder="The prompt to run… e.g. summarize last week's pipeline and save it as a doc"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") add();
            }}
          />
          <button
            type="button"
            className="opr-btn opr-btn-primary"
            onClick={add}
            disabled={!prompt.trim()}
          >
            Add routine
          </button>
        </div>
      </div>
    </div>
  );
}
