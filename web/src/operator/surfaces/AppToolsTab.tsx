// AppToolsTab — the app's Tools tab (spike). Lists the tools the app has, in
// PLAIN LANGUAGE for a non-technical operator (title + what it does), with the
// code tucked behind a "View code" toggle. These are AGENT tools: the app's chat
// calls them (with the right inputs, handling their output) — a human does not run
// them by hand, so there is no Run button here. Tools are authored by the chat
// agent's create_tool tool (harness) and shared via ToolsProvider. See
// docs/specs/operator-workflows-as-tools.md.

import { useState } from "react";
import { ChevronDown, Wrench } from "lucide-react";

import { Eyebrow } from "../components/primitives";
import type { Tool } from "../tools/mockTools";
import { useAppTools } from "../tools/toolsContext";

interface AppToolsTabProps {
  appName: string;
}

export function AppToolsTab({ appName }: AppToolsTabProps) {
  const { tools } = useAppTools();
  const [openId, setOpenId] = useState<string | null>(null);

  return (
    <div className="opr-tool-scoped opr-app-tools">
      <div className="opr-data-intro">
        <Eyebrow>Tools this app can use</Eyebrow>
        <p className="opr-scoped-note">
          Each tool does one job. {appName}'s chat picks the right tool and
          calls it when it needs to. Teach a new one in the chat.
        </p>
      </div>

      {tools.length === 0 ? (
        <div className="opr-empty-hint">
          No tools yet. Open the chat and teach {appName} a task — it'll build a
          tool for it.
        </div>
      ) : (
        <div className="opr-tool-catalog">
          {tools.map((tool) => (
            <ToolEntry
              key={tool.id}
              tool={tool}
              open={openId === tool.id}
              onToggle={() =>
                setOpenId((cur) => (cur === tool.id ? null : tool.id))
              }
            />
          ))}
        </div>
      )}
    </div>
  );
}

// One tool, in plain language: title + what it does + what it needs, with the code
// behind a toggle. No Run — the chat agent is the only caller.
function ToolEntry({
  tool,
  open,
  onToggle,
}: {
  tool: Tool;
  open: boolean;
  onToggle: () => void;
}) {
  const needs = tool.inputs.map((i) => i.name).join(", ");
  return (
    <div className="opr-tool-card">
      <div className="opr-tool-card-head">
        <span className="opr-tool-glyph" aria-hidden={true}>
          <Wrench size={13} strokeWidth={2} />
        </span>
        <span className="opr-tool-title">{tool.title}</span>
        <span className="opr-tool-agentonly">the chat calls this</span>
      </div>

      <p className="opr-tool-purpose">{tool.purpose}</p>
      {needs ? (
        <p className="opr-tool-needs">
          Needs: <span>{needs}</span>
        </p>
      ) : null}

      <button
        type="button"
        className="opr-tool-code-toggle"
        onClick={onToggle}
        aria-expanded={open}
      >
        <ChevronDown
          size={12}
          strokeWidth={2}
          aria-hidden={true}
          className={open ? "is-open" : ""}
        />
        {open ? "Hide code" : "View code"}
      </button>
      {open ? (
        <div className="opr-tool-code">
          <code className="opr-tool-sig">
            {tool.name}({tool.inputs.map((i) => i.name).join(", ")})
          </code>
          <pre className="opr-tool-script">
            <code>{tool.script}</code>
          </pre>
        </div>
      ) : null}
    </div>
  );
}
