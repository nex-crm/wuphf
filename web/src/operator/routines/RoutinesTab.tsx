// RoutinesTab — the agent's workflows, Claude-Routines style: each routine is a
// PROMPT the agent runs in its own chat on a schedule. No compiled diagram —
// the chat (which knows the agent's tools) is the runtime. Disable and Publish
// new version live on EACH routine, not on the agent. FE-first mock; the real
// scheduler + persistence land next. See docs/specs/operator-agent-routines.md.

import { useState } from "react";
import {
  CalendarClock,
  CheckCircle2,
  MessageSquareText,
  Plus,
  Power,
} from "lucide-react";

import { Eyebrow } from "../components/primitives";
import { newRoutine, type Routine, seedRoutines } from "./routines";

interface RoutinesTabProps {
  agentName: string;
  /** Open the routine's chat session in the Ask Agent dock. */
  onOpenSession?: (sessionId: string, title: string) => void;
}

export function RoutinesTab({ agentName, onOpenSession }: RoutinesTabProps) {
  const [routines, setRoutines] = useState<Routine[]>(() => seedRoutines());
  const [name, setName] = useState("");
  const [prompt, setPrompt] = useState("");
  const [schedule, setSchedule] = useState("Every Monday 9:00");

  function patch(id: string, up: (r: Routine) => Routine) {
    setRoutines((prev) => prev.map((r) => (r.id === id ? up(r) : r)));
  }

  function add() {
    const p = prompt.trim();
    if (!p) return;
    const n = name.trim() || p.slice(0, 40);
    setRoutines((prev) => [...prev, newRoutine(n, p, schedule)]);
    setName("");
    setPrompt("");
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
                {r.enabled
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
            />

            <div className="opr-routine-actions">
              <button
                type="button"
                className="opr-btn opr-btn-sm"
                onClick={() =>
                  patch(r.id, (x) => ({ ...x, enabled: !x.enabled }))
                }
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
                onClick={() =>
                  patch(r.id, (x) => ({
                    ...x,
                    version: x.version + 1,
                    draft: false,
                  }))
                }
              >
                <CheckCircle2 size={12} strokeWidth={2} aria-hidden={true} />
                Publish new version
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
