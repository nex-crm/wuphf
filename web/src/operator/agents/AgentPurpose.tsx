// AgentPurpose — "what this agent is for", summarized from the operator's
// conversations with it. The base line comes from the build description; every
// workflow taught in chat adds to it (each tool's purpose IS a conversation
// distilled), so the explanation deepens as the agent learns. Lives at the top of
// the agent detail, under the header. Reads the shared per-agent tools state, so
// teaching a tool in the chat updates this line live.

import { MessageCircle } from "lucide-react";

import { useAppTools } from "../tools/toolsContext";

interface AgentPurposeProps {
  /** The build-time description of the agent (may be empty). */
  summary?: string;
}

function sentenceCase(s: string): string {
  const t = s.trim().replace(/\.+$/, "");
  return t ? t[0].toUpperCase() + t.slice(1) : t;
}

function lowerLead(s: string): string {
  const t = s.trim().replace(/\.+$/, "");
  return t ? t[0].toLowerCase() + t.slice(1) : t;
}

export function AgentPurpose({ summary }: AgentPurposeProps) {
  const { tools } = useAppTools();
  const taught = tools.map((t) => lowerLead(t.purpose)).filter(Boolean);

  const base = summary?.trim() ? sentenceCase(summary) : null;
  if (!base && taught.length === 0) return null;

  return (
    <div className="opr-agent-purpose">
      <p className="opr-agent-purpose-text">
        {base ? `${base}.` : "This agent handles the workflows you teach it."}
        {taught.length > 0 ? (
          <>
            {" "}
            From your conversations, it can {taught.slice(0, 3).join("; ")}
            {taught.length > 3 ? `; and ${taught.length - 3} more` : ""}.
          </>
        ) : null}
      </p>
      {taught.length > 0 ? (
        <span
          className="opr-agent-purpose-chip"
          title="Summarized from the workflows you taught this agent in chat"
        >
          <MessageCircle size={11} strokeWidth={2} aria-hidden={true} />
          from {taught.length} conversation{taught.length === 1 ? "" : "s"}
        </span>
      ) : null}
    </div>
  );
}
