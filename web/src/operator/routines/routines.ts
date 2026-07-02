// Routines — a workflow IS a scheduled prompt run in the agent's chat (Claude
// Routines-style). Nothing to compile: the prompt goes into a chat session on a
// schedule, the agent calls its tools, the outcome lands as messages/artifacts.
// Disable and Publish-new-version belong to EACH routine, not the agent.
// FE-first mock; persistence + the real scheduler are the next slice.
// See docs/specs/operator-agent-routines.md.

export interface Routine {
  id: string;
  /** Plain-language name, e.g. "Monday pipeline recap". */
  name: string;
  /** The prompt the agent runs in its chat. */
  prompt: string;
  /** Human schedule label, e.g. "Every Monday 9:00". */
  schedule: string;
  enabled: boolean;
  /** Published version of the prompt; Publish freezes the draft as vN+1. */
  version: number;
  /** True when the prompt changed since the last publish. */
  draft?: boolean;
  lastRun?: string;
  /** The chat session this routine runs in. */
  sessionId: string;
}

export interface ChatSessionMeta {
  id: string;
  title: string;
  /** "routine" sessions are created by a schedule; "manual" by the operator. */
  kind: "routine" | "manual";
  at: string;
}

let seq = 0;
function nextId(prefix: string): string {
  seq += 1;
  return `${prefix}_${seq.toString(36)}`;
}

export function newRoutine(
  name: string,
  prompt: string,
  schedule: string,
): Routine {
  return {
    id: nextId("rt"),
    name,
    prompt,
    schedule,
    enabled: true,
    version: 1,
    sessionId: nextId("sess"),
  };
}

export function newSession(
  title: string,
  kind: ChatSessionMeta["kind"],
): ChatSessionMeta {
  return { id: nextId("sess"), title, kind, at: "just now" };
}

/** Seeded routines so the tab shows the shape (mirrors the ICP examples). */
export function seedRoutines(): Routine[] {
  return [
    {
      id: "rt_recap",
      name: "Monday pipeline recap",
      prompt:
        "Summarize last week's pipeline movement into a glanceable recap and save it as a doc.",
      schedule: "Every Monday 9:00",
      enabled: true,
      version: 3,
      lastRun: "Monday 9:02",
      sessionId: "sess_recap",
    },
    {
      id: "rt_route",
      name: "Route new leads",
      prompt:
        "Score every new inbound lead and route hot ones to the right AE.",
      schedule: "Every 30 minutes",
      enabled: true,
      version: 5,
      lastRun: "12 minutes ago",
      sessionId: "sess_route",
    },
    {
      id: "rt_chase",
      name: "Chase stalled deals",
      prompt:
        "Find deals with no touch in 7 days and draft a follow-up for each.",
      schedule: "Weekdays 8:00",
      enabled: false,
      version: 1,
      lastRun: "Jun 24",
      sessionId: "sess_chase",
    },
  ];
}

/** Seeded sessions: one per seeded routine + the operator's manual chat. */
export function seedSessions(): ChatSessionMeta[] {
  return [
    {
      id: "sess_manual",
      title: "Chat with your agent",
      kind: "manual",
      at: "now",
    },
    {
      id: "sess_recap",
      title: "Monday pipeline recap",
      kind: "routine",
      at: "Monday 9:02",
    },
    {
      id: "sess_route",
      title: "Route new leads",
      kind: "routine",
      at: "12 min ago",
    },
    {
      id: "sess_chase",
      title: "Chase stalled deals",
      kind: "routine",
      at: "Jun 24",
    },
  ];
}
