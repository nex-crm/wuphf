// AppToolsTab — the app's Tools tab (spike). Lists the tools the app already has,
// in PLAIN LANGUAGE for a non-technical operator (title + what it does), with the
// code tucked behind a "View code" toggle for anyone who wants it. Tools are
// authored by the chat agent's own create_tool tool (in the harness), not here —
// this tab only shows what exists and lets you run one. FE-first mock
// (seedToolsForApp / callTool); no backend. Additive to the tab model — the
// Workflow tab is untouched. See docs/specs/operator-workflows-as-tools.md.

import { useState } from "react";
import { ChevronDown, Play, Wrench } from "lucide-react";

import { Eyebrow } from "../components/primitives";
import type { Tool } from "../tools/mockTools";
import { useAppTools } from "../tools/toolsContext";

interface AppToolsTabProps {
  appName: string;
}

export function AppToolsTab({ appName }: AppToolsTabProps) {
  const { tools, runTool } = useAppTools();
  const [openId, setOpenId] = useState<string | null>(null);

  return (
    <div className="opr-tool-scoped opr-app-tools">
      <div className="opr-data-intro">
        <Eyebrow>Tools this app can use</Eyebrow>
        <p className="opr-scoped-note">
          Each tool does one job. {appName}'s chat picks the right tool and runs
          it for you — or you can run one yourself. Teach a new one in the chat.
        </p>
      </div>

      <div className="opr-tool-catalog">
        {tools.map((tool) => (
          <ToolEntry
            key={tool.id}
            tool={tool}
            open={openId === tool.id}
            onToggle={() =>
              setOpenId((cur) => (cur === tool.id ? null : tool.id))
            }
            onRun={() => runTool(tool)}
          />
        ))}
      </div>
    </div>
  );
}

// One tool, in plain language: title + what it does, run it, last result, and the
// code tucked behind a toggle.
function ToolEntry({
  tool,
  open,
  onToggle,
  onRun,
}: {
  tool: Tool;
  open: boolean;
  onToggle: () => void;
  onRun: () => void;
}) {
  const needs = tool.inputs.map((i) => i.name).join(", ");
  return (
    <div className="opr-tool-card">
      <div className="opr-tool-card-head">
        <span className="opr-tool-glyph" aria-hidden={true}>
          <Wrench size={13} strokeWidth={2} />
        </span>
        <span className="opr-tool-title">{tool.title}</span>
        {tool.calls.length > 0 ? (
          <span className="opr-tool-runs">run {tool.calls.length}×</span>
        ) : null}
        <button
          type="button"
          className="opr-btn opr-btn-sm opr-tool-run-btn"
          onClick={onRun}
        >
          <Play size={12} strokeWidth={2} aria-hidden={true} />
          Run
        </button>
      </div>

      <p className="opr-tool-purpose">{tool.purpose}</p>
      {needs ? (
        <p className="opr-tool-needs">
          Needs: <span>{needs}</span>
        </p>
      ) : null}

      {tool.calls.length > 0 ? (
        <div className="opr-toolcall">
          <div className="opr-toolcall-line">Last run</div>
          <div className="opr-toolcall-result">{tool.calls[0].result}</div>
        </div>
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
