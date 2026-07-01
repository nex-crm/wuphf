// AppToolsTab — the app's Tools tab (spike). Shows every tool the app can use in
// PLAIN LANGUAGE for a non-technical operator (title + what it does), with the
// code tucked behind a "View code" toggle for anyone who wants it. Tools are made
// by the built-in **createTool** tool (the tool that creates tools) — the app's
// builder chat uses it; here its composer lives in its own card. The app's chat
// can call any tool (the Run button stands in). FE-first mock
// (authorToolFromDescription / callTool); no backend. Additive to the tab model —
// the Workflow tab is untouched. See docs/specs/operator-workflows-as-tools.md.

import { useState } from "react";
import { ChevronDown, Play, Send, Sparkles, Wrench } from "lucide-react";

import { Eyebrow } from "../components/primitives";
import {
  authorToolFromDescription,
  callTool,
  createToolMetaTool,
  sampleArgsFor,
  seedToolsForApp,
  type Tool,
} from "../tools/mockTools";

interface AppToolsTabProps {
  appName: string;
}

const META_ID = createToolMetaTool().id;

export function AppToolsTab({ appName }: AppToolsTabProps) {
  const [tools, setTools] = useState<Tool[]>(() => seedToolsForApp(appName));
  const [openId, setOpenId] = useState<string | null>(null);

  function runTool(tool: Tool) {
    const call = callTool(tool, sampleArgsFor(tool));
    setTools((prev) =>
      prev.map((t) =>
        t.id === tool.id ? { ...t, calls: [call, ...t.calls] } : t,
      ),
    );
  }

  // createTool makes a new tool AND logs a "run" on itself (the tool it built).
  function createTool(description: string) {
    const built = authorToolFromDescription(description);
    setTools((prev) => [
      ...prev.map((t) =>
        t.id === META_ID
          ? {
              ...t,
              calls: [
                {
                  id: `c_${built.id}`,
                  args: { workflow: description },
                  result: `Built ${built.title}`,
                  at: "just now",
                },
                ...t.calls,
              ],
            }
          : t,
      ),
      built,
    ]);
    setOpenId(built.id);
  }

  const meta = tools.find((t) => t.id === META_ID);
  const made = tools.filter((t) => t.id !== META_ID);

  return (
    <div className="opr-tool-scoped opr-app-tools">
      <div className="opr-data-intro">
        <Eyebrow>Tools this app can use</Eyebrow>
        <p className="opr-scoped-note">
          Each tool does one job. {appName}'s chat picks the right tool and runs
          it for you — or you can run one yourself.
        </p>
      </div>

      {meta ? <CreateToolCard meta={meta} onCreate={createTool} /> : null}

      <div className="opr-tool-catalog">
        {made.map((tool) => (
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

// The built-in create-a-tool tool: its own card, with the describe-a-workflow
// composer inside it. This is the tool the builder chat uses to make tools.
function CreateToolCard({
  meta,
  onCreate,
}: {
  meta: Tool;
  onCreate: (description: string) => void;
}) {
  const [draft, setDraft] = useState("");
  const [thinking, setThinking] = useState(false);
  const [showCode, setShowCode] = useState(false);

  function submit() {
    const body = draft.trim();
    if (!body || thinking) return;
    setDraft("");
    setThinking(true);
    window.setTimeout(() => {
      onCreate(body);
      setThinking(false);
    }, 600);
  }

  return (
    <div className="opr-tool-card opr-tool-card-meta">
      <div className="opr-tool-card-head">
        <span className="opr-tool-glyph" aria-hidden={true}>
          <Sparkles size={14} strokeWidth={2} />
        </span>
        <span className="opr-tool-title">{meta.title}</span>
        <span className="opr-tool-builtin">built in</span>
        {meta.calls.length > 0 ? (
          <span className="opr-tool-runs">
            made {meta.calls.length} tool{meta.calls.length === 1 ? "" : "s"}
          </span>
        ) : null}
      </div>
      <p className="opr-tool-purpose">{meta.purpose}</p>

      <div className="opr-composer opr-tool-teach-composer">
        <input
          className="opr-composer-input"
          aria-label="Describe a workflow for the builder to turn into a tool"
          placeholder="Describe a workflow… e.g. draft a follow-up for a stalled deal"
          value={draft}
          disabled={thinking}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
          }}
        />
        <button
          type="button"
          className="opr-btn opr-btn-primary"
          onClick={submit}
          disabled={thinking || !draft.trim()}
        >
          <Send size={14} strokeWidth={1.9} aria-hidden={true} />
          {thinking ? "Building…" : "Build tool"}
        </button>
      </div>

      <CodeToggle open={showCode} onToggle={() => setShowCode((v) => !v)} />
      {showCode ? <ToolCode tool={meta} /> : null}
    </div>
  );
}

// One made tool, in plain language: title + what it does, run it, last result,
// and the code tucked behind a toggle.
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

      <CodeToggle open={open} onToggle={onToggle} />
      {open ? <ToolCode tool={tool} /> : null}
    </div>
  );
}

function CodeToggle({
  open,
  onToggle,
}: {
  open: boolean;
  onToggle: () => void;
}) {
  return (
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
  );
}

function ToolCode({ tool }: { tool: Tool }) {
  const signature = `${tool.name}(${tool.inputs.map((i) => i.name).join(", ")})`;
  return (
    <div className="opr-tool-code">
      <code className="opr-tool-sig">{signature}</code>
      <pre className="opr-tool-script">
        <code>{tool.script}</code>
      </pre>
    </div>
  );
}
