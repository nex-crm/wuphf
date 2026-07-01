// AgentSessions — the agent's chats, plural. Every routine runs in its own
// session; the operator can browse them all and start new manual ones (Claude
// Routines-style). A session strip sits atop the Ask Agent dock; each session's
// transcript stays mounted (hidden, not unmounted) so switching never loses
// state. FE-first mock; persisted sessions land with the scheduler slice.
// See docs/specs/operator-agent-routines.md.

import { useEffect, useState } from "react";
import { CalendarClock, MessageSquareText, Plus } from "lucide-react";

import {
  type ChatSessionMeta,
  newSession,
  seedSessions,
} from "../routines/routines";
import { AppToolsChat } from "../surfaces/AppToolsChat";

interface AgentSessionsProps {
  agentName: string;
  /** Open this session (e.g. from a routine's "Open its chat"). */
  requestedSessionId?: string | null;
  /** One-shot instruction for the manual session (demo hand-off). */
  seed?: string;
}

// A mock transcript for a routine session: the scheduled prompt going in and the
// agent's outcome — the shape a real run will persist.
function routineTranscript(
  title: string,
): { from: "you" | "nex"; body: string }[] {
  return [
    { from: "you", body: `(scheduled) ${title}` },
    {
      from: "nex",
      body: "Ran the routine with this agent's tools — the outcome is saved under Artifacts.",
    },
  ];
}

export function AgentSessions({
  agentName,
  requestedSessionId,
  seed,
}: AgentSessionsProps) {
  const [sessions, setSessions] = useState<ChatSessionMeta[]>(() =>
    seedSessions(),
  );
  const [activeId, setActiveId] = useState<string>(sessions[0]?.id ?? "");
  // Mount a session's pane on first visit, then keep it alive.
  const [mounted, setMounted] = useState<string[]>([activeId]);

  function open(id: string) {
    setActiveId(id);
    setMounted((prev) => (prev.includes(id) ? prev : [...prev, id]));
  }

  useEffect(() => {
    if (!requestedSessionId) return;
    open(requestedSessionId);
  }, [requestedSessionId]);

  function addManual() {
    const s = newSession(`Chat ${sessions.length + 1}`, "manual");
    setSessions((prev) => [...prev, s]);
    open(s.id);
  }

  return (
    <div className="opr-agent-sessions">
      <div className="opr-session-strip" aria-label="Chat sessions">
        {sessions.map((s) => (
          <button
            key={s.id}
            type="button"
            className={`opr-session-chip${s.id === activeId ? " is-active" : ""}`}
            onClick={() => open(s.id)}
            title={`${s.title} · ${s.at}`}
          >
            {s.kind === "routine" ? (
              <CalendarClock size={11} strokeWidth={2} aria-hidden={true} />
            ) : (
              <MessageSquareText size={11} strokeWidth={2} aria-hidden={true} />
            )}
            <span className="opr-session-chip-title">{s.title}</span>
          </button>
        ))}
        <button
          type="button"
          className="opr-session-chip opr-session-new"
          onClick={addManual}
          aria-label="New chat"
        >
          <Plus size={11} strokeWidth={2} aria-hidden={true} />
          New chat
        </button>
      </div>

      <div className="opr-session-panes">
        {sessions
          .filter((s) => mounted.includes(s.id))
          .map((s) => (
            <div
              key={s.id}
              style={s.id === activeId ? undefined : { display: "none" }}
            >
              <AppToolsChat
                appName={agentName}
                seed={
                  s.kind === "manual" && s.id === sessions[0]?.id
                    ? seed
                    : undefined
                }
                initialTranscript={
                  s.kind === "routine" ? routineTranscript(s.title) : undefined
                }
              />
            </div>
          ))}
      </div>
    </div>
  );
}
