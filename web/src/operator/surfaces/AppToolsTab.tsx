// AppToolsTab — the app's Tools tab (spike). Shows every tool the app has access
// to with an explanation of what it does. Tools are built conversationally from a
// learned workflow (the composer here, and — next slice — the app's own Ask-AI
// chat), and the app's chat can CALL them. This slice is FE-first mock
// (authorToolFromDescription / callTool); no backend. It ADDS to the tab model —
// the Workflow tab is untouched. See docs/specs/operator-workflows-as-tools.md.

import { useState } from "react";
import { ChevronDown, Send, Terminal, Wrench } from "lucide-react";

import { Eyebrow } from "../components/primitives";
import {
  authorToolFromDescription,
  callTool,
  sampleArgsFor,
  seedToolsForApp,
  type Tool,
} from "../tools/mockTools";

interface AppToolsTabProps {
  appName: string;
}

export function AppToolsTab({ appName }: AppToolsTabProps) {
  const [tools, setTools] = useState<Tool[]>(() => seedToolsForApp(appName));
  const [draft, setDraft] = useState("");
  const [thinking, setThinking] = useState(false);
  const [openId, setOpenId] = useState<string | null>(null);

  function callAndLog(tool: Tool) {
    const call = callTool(tool, sampleArgsFor(tool));
    setTools((prev) =>
      prev.map((t) =>
        t.id === tool.id ? { ...t, calls: [call, ...t.calls] } : t,
      ),
    );
  }

  function teach(text?: string) {
    const body = (text ?? draft).trim();
    if (!body || thinking) return;
    setDraft("");
    setThinking(true);
    window.setTimeout(() => {
      const tool = authorToolFromDescription(body);
      setTools((prev) => [...prev, tool]);
      setOpenId(tool.id);
      setThinking(false);
    }, 600);
  }

  return (
    <div className="opr-tool-scoped opr-app-tools">
      <div className="opr-data-intro">
        <Eyebrow>Tools this app can use</Eyebrow>
        <p className="opr-scoped-note">
          Nex builds a tool from each workflow you teach it. {appName}'s chat
          knows these tools and calls them when it needs to.
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
            onCall={() => callAndLog(tool)}
          />
        ))}
      </div>

      <div className="opr-tool-teach">
        <div className="opr-tool-teach-label">
          <Wrench size={12} strokeWidth={2} aria-hidden={true} />
          Teach Nex a workflow to add a tool
        </div>
        <div className="opr-composer">
          <input
            className="opr-composer-input"
            aria-label="Describe a workflow for Nex to turn into a tool"
            placeholder="e.g. Draft a follow-up email for a stalled deal…"
            value={draft}
            disabled={thinking}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") teach();
            }}
          />
          <button
            type="button"
            className="opr-btn opr-btn-primary"
            onClick={() => teach()}
            disabled={thinking || !draft.trim()}
          >
            <Send size={14} strokeWidth={1.9} aria-hidden={true} />
            {thinking ? "Writing…" : "Build tool"}
          </button>
        </div>
      </div>
    </div>
  );
}

function ToolEntry({
  tool,
  open,
  onToggle,
  onCall,
}: {
  tool: Tool;
  open: boolean;
  onToggle: () => void;
  onCall: () => void;
}) {
  const signature = `${tool.name}(${tool.inputs.map((i) => i.name).join(", ")})`;
  return (
    <div className="opr-tool-card">
      <div className="opr-tool-card-head">
        <span className="opr-tool-glyph" aria-hidden={true}>
          <Wrench size={13} strokeWidth={2} />
        </span>
        <code className="opr-tool-sig">{signature}</code>
        {tool.calls.length > 0 ? (
          <span
            className="opr-tool-runs"
            title="Times the chat has called this"
          >
            {tool.calls.length} call{tool.calls.length === 1 ? "" : "s"}
          </span>
        ) : null}
        <button
          type="button"
          className="opr-btn opr-btn-sm opr-tool-call-btn"
          onClick={onCall}
        >
          <Terminal size={12} strokeWidth={2} aria-hidden={true} />
          Call
        </button>
      </div>

      <p className="opr-tool-purpose">{tool.purpose}</p>
      <p className="opr-tool-taught">Taught from: “{tool.createdFrom}”</p>

      {tool.calls.length > 0 ? (
        <div className="opr-toolcall">
          <div className="opr-toolcall-line">
            <Terminal size={12} strokeWidth={2} aria-hidden={true} />
            <code>Last call</code>
          </div>
          <div className="opr-toolcall-result">{tool.calls[0].result}</div>
        </div>
      ) : null}

      <button
        type="button"
        className="opr-tool-script-toggle"
        onClick={onToggle}
        aria-expanded={open}
      >
        <ChevronDown
          size={12}
          strokeWidth={2}
          aria-hidden={true}
          className={open ? "is-open" : ""}
        />
        {open ? "Hide script" : "View script"}
      </button>
      {open ? (
        <pre className="opr-tool-script">
          <code>{tool.script}</code>
        </pre>
      ) : null}
    </div>
  );
}
