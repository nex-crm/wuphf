// Edit-with-AI — a chat docked beside a tool to change it in place, the way the
// app builder builds-with-you. The operator describes a change; the AI proposes
// the concrete edits and the operator applies them as a new version. Mock: the
// AI replies with a canned proposal and "Apply" bumps the version locally.

import { useEffect, useRef, useState } from "react";

import type { InternalTool, ToolVersion } from "../mock/data";

interface Proposal {
  summary: string[];
  version: number;
}

interface EditMessage {
  id: string;
  from: "you" | "ai";
  body: string;
  proposal?: Proposal;
  applied?: boolean;
}

interface ToolEditChatProps {
  tool: InternalTool;
  onClose: () => void;
  // Lift an applied edit back to the parent tool model so the detail view's
  // version meta and version history reflect the publish, not just this chat.
  onApply: (version: ToolVersion) => void;
}

// Read the operator's request and turn it into a concrete, named change list.
// Keyword-driven so the mock feels like it understood, not a stock reply.
function proposeFrom(text: string): string[] {
  const t = text.toLowerCase();
  const changes: string[] = [];
  const threshold = t.match(/\b(\d{2,3})\b/);
  if (t.includes("threshold") || (t.includes("score") && threshold)) {
    changes.push(`Fit threshold becomes ${threshold ? threshold[1] : "70"}`);
  }
  if (t.includes("slack") || t.includes("message") || t.includes("notify")) {
    changes.push("Update the Slack handoff message");
  }
  if (t.includes("industr") || t.includes("vertical")) {
    changes.push("Adjust the industries used for scoring");
  }
  if (t.includes("nurtur")) {
    changes.push("Change the nurture branch");
  }
  if (changes.length === 0) {
    changes.push(`Apply: "${text.trim()}"`);
  }
  return changes;
}

export function ToolEditChat({ tool, onClose, onApply }: ToolEditChatProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [draft, setDraft] = useState("");
  const [version, setVersion] = useState(tool.version);
  const [messages, setMessages] = useState<EditMessage[]>([
    {
      id: "seed-1",
      from: "ai",
      body: "I can edit this tool with you. Tell me what to change: the fit threshold, who gets routed, the Slack message, anything.",
    },
  ]);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight });
  }, [messages]);

  function send() {
    const text = draft.trim();
    if (!text) return;
    const nextVersion = version + 1;
    setMessages((prev) => [
      ...prev,
      { id: `you-${prev.length}`, from: "you", body: text },
      {
        id: `ai-${prev.length}`,
        from: "ai",
        body: "Here's what I'll change. Want me to apply it and publish a new version?",
        proposal: { summary: proposeFrom(text), version: nextVersion },
      },
    ]);
    setDraft("");
  }

  function apply(msgId: string, proposal: Proposal) {
    const v = proposal.version;
    setVersion(v);
    setMessages((prev) =>
      prev.map((m) => (m.id === msgId ? { ...m, applied: true } : m)),
    );
    setMessages((prev) => [
      ...prev,
      {
        id: `done-${prev.length}`,
        from: "ai",
        body: `Done. Published v${v}. The change is live and shows in the version history.`,
      },
    ]);
    // Hand the published version up so the parent tool model is the source of
    // truth; the detail view's "v{n}" meta and version history update for real.
    onApply({
      version: v,
      label: proposal.summary[0] ?? `Edited to v${v}`,
      at: "Just now",
      author: "you",
      note: proposal.summary.join("; "),
    });
  }

  return (
    <aside className="opr-edit-panel" aria-label="Edit with AI">
      <div className="opr-edit-head">
        <div>
          <div className="opr-eyebrow">Edit with AI</div>
          <div className="opr-edit-tool">{tool.name}</div>
        </div>
        <button
          type="button"
          className="opr-btn opr-btn-ghost opr-btn-sm"
          onClick={onClose}
          aria-label="Close editor"
        >
          Close
        </button>
      </div>

      <div className="opr-edit-scroll" ref={scrollRef}>
        {messages.map((m) => (
          <div key={m.id} className="opr-edit-msgwrap">
            <div
              className={`opr-msg ${
                m.from === "ai" ? "opr-msg-ai" : "opr-msg-you"
              }`}
            >
              {m.body}
            </div>
            {m.proposal ? (
              <div className="opr-proposal">
                <div className="opr-eyebrow">
                  Proposed change · v{m.proposal.version}
                </div>
                <ul className="opr-proposal-list">
                  {m.proposal.summary.map((s, i) => (
                    <li key={i}>{s}</li>
                  ))}
                </ul>
                {m.applied ? (
                  <div className="opr-proposal-applied">Applied</div>
                ) : (
                  <div className="opr-proposal-actions">
                    <button
                      type="button"
                      className="opr-btn opr-btn-primary opr-btn-sm"
                      onClick={() => apply(m.id, m.proposal!)}
                    >
                      Apply &amp; publish
                    </button>
                    <button type="button" className="opr-btn opr-btn-sm">
                      Not yet
                    </button>
                  </div>
                )}
              </div>
            ) : null}
          </div>
        ))}
      </div>

      <div className="opr-composer">
        <input
          className="opr-composer-input"
          aria-label="Describe a change to this tool"
          placeholder="e.g. lower the threshold to 65, add finance"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") send();
          }}
        />
        <button
          type="button"
          className="opr-btn opr-btn-primary"
          onClick={send}
        >
          Send
        </button>
      </div>
    </aside>
  );
}
