// AgentName — the agent's display name, renameable inline. Click the pencil,
// type, Enter saves (Escape cancels; empty restores the default). Renames apply
// everywhere a name renders (header, sidebar rail, lists) via the agentNames
// store; the broker has no rename field yet, so this is client-side for now.

import { useEffect, useRef, useState } from "react";
import { Check, Pencil } from "lucide-react";

import { setAgentName, useAgentName } from "./agentNames";

interface AgentNameProps {
  id: string;
  fallback: string;
}

export function AgentName({ id, fallback }: AgentNameProps) {
  const name = useAgentName(id, fallback);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(name);
  const inputRef = useRef<HTMLInputElement>(null);

  // Focus follows the operator's explicit "rename" click into the input.
  useEffect(() => {
    if (editing) inputRef.current?.focus();
  }, [editing]);

  function start() {
    setDraft(name);
    setEditing(true);
  }
  function save() {
    setAgentName(id, draft);
    setEditing(false);
  }

  if (editing) {
    return (
      <span className="opr-agent-name-edit">
        <input
          ref={inputRef}
          className="opr-agent-name-input"
          aria-label="Agent name"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={save}
          onKeyDown={(e) => {
            if (e.key === "Enter") save();
            if (e.key === "Escape") setEditing(false);
          }}
        />
        <button
          type="button"
          className="opr-icon-btn"
          onMouseDown={(e) => e.preventDefault()}
          onClick={save}
          aria-label="Save agent name"
        >
          <Check size={13} strokeWidth={2} aria-hidden={true} />
        </button>
      </span>
    );
  }

  return (
    <span className="opr-agent-name">
      {name}
      <button
        type="button"
        className="opr-icon-btn opr-agent-name-pencil"
        onClick={start}
        aria-label={`Rename ${name}`}
        title="Rename this agent"
      >
        <Pencil size={12} strokeWidth={1.9} aria-hidden={true} />
      </button>
    </span>
  );
}
