// AgentSessions — the agent's chats, plural. Every routine runs in its own
// session; the operator can browse them all and start new manual ones (Claude
// Routines-style). A session strip sits atop the Ask Agent dock; each session's
// transcript stays mounted (hidden, not unmounted) so switching never loses
// state. With a REAL agent id (app_…) sessions and their transcripts load from
// the agent service, "New chat" persists a session, and the manual chat mirrors
// each turn to the service fire-and-forget; when the service is unreachable the
// dock falls back to the seeded state. See docs/specs/operator-agent-routines.md.

import { useEffect, useState } from "react";
import { CalendarClock, MessageSquareText, Plus } from "lucide-react";

import { isRealAppId } from "../apps/useOperatorApps";
import {
  type ChatSessionMeta,
  newSession,
  seedSessions,
} from "../routines/routines";
import { AppToolsChat } from "../surfaces/AppToolsChat";
import {
  tryCreateSession,
  tryGetSession,
  tryListSessions,
  trySendSessionMessage,
  type WireSession,
  type WireSessionMessage,
} from "./agentStateClient";

interface AgentSessionsProps {
  agentName: string;
  /** Real agent id (app_…). When set, sessions persist via the agent service;
   * without it (mock agents) the dock keeps its local seeded state. */
  agentId?: string;
  /** Open this session (e.g. from a routine's "Open its chat"). */
  requestedSessionId?: string | null;
  /** One-shot instruction for the manual session (demo hand-off). */
  seed?: string;
}

type Transcript = { from: "you" | "nex"; body: string }[];

// A mock transcript for a routine session: the scheduled prompt going in and the
// agent's outcome — the shape a real run persists (and the offline fallback).
function routineTranscript(title: string): Transcript {
  return [
    { from: "you", body: `(scheduled) ${title}` },
    {
      from: "nex",
      body: "Ran the routine with this agent's tools — the outcome is saved under Artifacts.",
    },
  ];
}

function toMeta(s: WireSession): ChatSessionMeta {
  return { id: s.id, title: s.title, kind: s.kind, at: s.at };
}

function toTranscript(messages: WireSessionMessage[]): Transcript {
  return messages.map(({ from, body }) => ({ from, body }));
}

export function AgentSessions({
  agentName,
  agentId,
  requestedSessionId,
  seed,
}: AgentSessionsProps) {
  const [sessions, setSessions] = useState<ChatSessionMeta[]>(() =>
    seedSessions(),
  );
  const [activeId, setActiveId] = useState<string>(sessions[0]?.id ?? "");
  // Mount a session's pane on first visit, then keep it alive.
  const [mounted, setMounted] = useState<string[]>([activeId]);
  // True once the agent service answered — from then on writes go to it.
  const [live, setLive] = useState(false);
  // Persisted transcripts by session id; null = fetched but unavailable.
  const [transcripts, setTranscripts] = useState<
    Record<string, Transcript | null>
  >({});

  const realId = isRealAppId(agentId) ? agentId : undefined;

  useEffect(() => {
    if (!realId) return;
    let cancelled = false;
    void tryListSessions(realId).then(async (remote) => {
      if (cancelled || !remote) return; // unreachable — keep the seeded state
      const [first] = remote;
      // Fetch the first session's transcript BEFORE mounting its pane: a pane
      // reads its initialTranscript only at mount.
      const detail = first ? await tryGetSession(first.id, realId) : null;
      if (cancelled) return;
      setLive(true);
      setSessions(remote.map(toMeta));
      if (first) {
        setTranscripts({
          [first.id]: detail ? toTranscript(detail.messages) : null,
        });
        setActiveId(first.id);
        setMounted([first.id]);
      } else {
        setActiveId("");
        setMounted([]);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [realId]);

  function open(id: string) {
    setActiveId(id);
    if (mounted.includes(id)) return;
    if (live && realId && !(id in transcripts)) {
      // Load the persisted transcript first, then mount the pane on it.
      void tryGetSession(id, realId).then((detail) => {
        setTranscripts((prev) => ({
          ...prev,
          [id]: detail ? toTranscript(detail.messages) : null,
        }));
        setMounted((prev) => (prev.includes(id) ? prev : [...prev, id]));
      });
      return;
    }
    setMounted((prev) => (prev.includes(id) ? prev : [...prev, id]));
  }

  useEffect(() => {
    if (!requestedSessionId) return;
    open(requestedSessionId);
  }, [requestedSessionId]);

  function addManual() {
    const title = `Chat ${sessions.length + 1}`;
    const openNew = (s: ChatSessionMeta) => {
      setSessions((prev) => [...prev, s]);
      setTranscripts((prev) => ({ ...prev, [s.id]: [] }));
      setActiveId(s.id);
      setMounted((prev) => (prev.includes(s.id) ? prev : [...prev, s.id]));
    };
    if (live && realId) {
      void tryCreateSession(realId, title).then((created) => {
        openNew(created ? toMeta(created) : newSession(title, "manual"));
      });
      return;
    }
    openNew(newSession(title, "manual"));
  }

  // What a pane starts from: the persisted transcript when the service has
  // one, the mock routine transcript for offline routine sessions, else the
  // chat's own greeting.
  function initialTranscriptFor(s: ChatSessionMeta): Transcript | undefined {
    const fetched = transcripts[s.id];
    if (fetched && fetched.length > 0) return fetched;
    if (s.kind === "routine") return routineTranscript(s.title);
    return undefined;
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
                initialTranscript={initialTranscriptFor(s)}
                onTurn={
                  live && realId
                    ? (from, body) =>
                        trySendSessionMessage(s.id, {
                          agent: realId,
                          from,
                          body,
                        })
                    : undefined
                }
              />
            </div>
          ))}
      </div>
    </div>
  );
}
