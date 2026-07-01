// ToolsWorkspace — the "workflows as tools" spike surface (slice 1, FE mock).
//
// The reframe: no built app UI. The operator talks to Nex in chat; when they
// teach Nex a workflow, Nex WRITES A TOOL (a scripted function) and stores it in
// the Tools rail. The agent then CALLS those tools in chat to run the workflow.
// Everything here is deterministic mock (authorToolFromDescription / callTool) so
// the shape can be validated before any backend. See
// docs/specs/operator-workflows-as-tools.md.

import { useRef, useState } from "react";
import { ArrowRight, Send, Terminal, Wrench } from "lucide-react";

import { Eyebrow } from "../components/primitives";
import {
  authorToolFromDescription,
  callTool,
  sampleArgsFor,
  seedTools,
  TOOL_STARTERS,
  type Tool,
  type ToolCall,
} from "../tools/mockTools";

type ChatItem =
  | { kind: "text"; id: string; from: "you" | "nex"; body: string }
  | { kind: "tool"; id: string; toolId: string }
  | { kind: "call"; id: string; toolId: string; call: ToolCall };

let uid = 0;
function nextId(): string {
  uid += 1;
  return `m_${uid}`;
}

export function ToolsWorkspace() {
  const [tools, setTools] = useState<Tool[]>(() => seedTools());
  const [items, setItems] = useState<ChatItem[]>(() => [
    {
      kind: "text",
      id: nextId(),
      from: "nex",
      body: "Tell me a workflow you do, and I will build you a tool for it. Then just ask me to run it.",
    },
  ]);
  const [draft, setDraft] = useState("");
  const [thinking, setThinking] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  function scrollDown() {
    requestAnimationFrame(() => {
      const el = scrollRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    });
  }

  function runTool(tool: Tool) {
    const call = callTool(tool, sampleArgsFor(tool));
    setTools((prev) =>
      prev.map((t) =>
        t.id === tool.id ? { ...t, calls: [call, ...t.calls] } : t,
      ),
    );
    setItems((prev) => [
      ...prev,
      {
        kind: "text",
        id: nextId(),
        from: "nex",
        body: `Calling ${tool.name}…`,
      },
      { kind: "call", id: nextId(), toolId: tool.id, call },
    ]);
    scrollDown();
  }

  // A described workflow becomes a new tool; a "run/call <name>" phrase runs an
  // existing one. Deliberately light matching — the mock is about the shape.
  function submit(text?: string) {
    const body = (text ?? draft).trim();
    if (!body || thinking) return;
    setDraft("");
    setItems((prev) => [
      ...prev,
      { kind: "text", id: nextId(), from: "you", body },
    ]);

    const runMatch = /^(run|call|use)\b/i.test(body);
    if (runMatch) {
      const target =
        tools.find((t) => body.toLowerCase().includes(t.name.toLowerCase())) ??
        tools.find((t) =>
          body.toLowerCase().includes(t.purpose.slice(0, 12).toLowerCase()),
        ) ??
        tools[tools.length - 1];
      if (target) {
        runTool(target);
        return;
      }
    }

    setThinking(true);
    scrollDown();
    // Simulate Nex authoring the tool.
    window.setTimeout(() => {
      const tool = authorToolFromDescription(body);
      setTools((prev) => [...prev, tool]);
      setSelectedId(tool.id);
      setItems((prev) => [
        ...prev,
        {
          kind: "text",
          id: nextId(),
          from: "nex",
          body: `Done — I wrote a tool for that: ${tool.name}. It is in your Tools. Ask me to run it whenever you like.`,
        },
        { kind: "tool", id: nextId(), toolId: tool.id },
      ]);
      setThinking(false);
      scrollDown();
    }, 650);
  }

  const selected = tools.find((t) => t.id === selectedId) ?? null;

  return (
    <div className="opr-tools-workspace">
      <div className="opr-tools-chat">
        <div className="opr-builder-head">
          <div>
            <Eyebrow>Assistant</Eyebrow>
            <div className="opr-builder-title">
              Teach Nex a workflow — it builds you a tool
            </div>
          </div>
        </div>

        <div className="opr-builder-scroll" ref={scrollRef}>
          {items.map((item) => (
            <ChatRow
              key={item.id}
              item={item}
              tool={
                item.kind !== "text"
                  ? tools.find((t) => t.id === item.toolId)
                  : undefined
              }
              onRun={runTool}
              onOpen={(id) => setSelectedId(id)}
            />
          ))}

          {thinking ? (
            <div className="opr-act-working" aria-label="Nex is writing a tool">
              <span className="opr-work-dots" aria-hidden={true}>
                <span />
                <span />
                <span />
              </span>
              <span className="opr-work-phrase">Nex is writing a tool…</span>
            </div>
          ) : null}
        </div>

        {items.length <= 1 ? (
          <div className="opr-starters">
            <div className="opr-starters-label">Or start from one of these</div>
            {TOOL_STARTERS.map((s) => (
              <button
                key={s}
                type="button"
                className="opr-starter-chip"
                onClick={() => submit(s)}
              >
                {s}
              </button>
            ))}
          </div>
        ) : null}

        <div className="opr-composer">
          <input
            className="opr-composer-input"
            aria-label="Describe a workflow, or ask Nex to run a tool"
            placeholder="Describe a workflow, or say 'run <tool>'…"
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
            onClick={() => submit()}
            disabled={thinking || !draft.trim()}
          >
            <Send size={14} strokeWidth={1.9} aria-hidden={true} />
            Send
          </button>
        </div>
      </div>

      <ToolsRail
        tools={tools}
        selected={selected}
        onSelect={(id) => setSelectedId(id)}
        onRun={runTool}
      />
    </div>
  );
}

function ChatRow({
  item,
  tool,
  onRun,
  onOpen,
}: {
  item: ChatItem;
  tool?: Tool;
  onRun: (tool: Tool) => void;
  onOpen: (id: string) => void;
}) {
  if (item.kind === "text") {
    return (
      <div className="opr-edit-msgwrap">
        <div
          className={`opr-msg ${item.from === "nex" ? "opr-msg-ai" : "opr-msg-you"}`}
        >
          {item.body}
        </div>
      </div>
    );
  }
  if (!tool) return null;
  if (item.kind === "tool") {
    return (
      <div className="opr-tool-card">
        <div className="opr-tool-card-head">
          <span className="opr-tool-glyph" aria-hidden={true}>
            <Wrench size={13} strokeWidth={2} />
          </span>
          <code className="opr-tool-name">{tool.name}</code>
          <span className="opr-tool-badge">new tool</span>
        </div>
        <p className="opr-tool-purpose">{tool.purpose}</p>
        <pre className="opr-tool-script">
          <code>{tool.script}</code>
        </pre>
        <div className="opr-tool-card-actions">
          <button
            type="button"
            className="opr-btn opr-btn-primary opr-btn-sm"
            onClick={() => onRun(tool)}
          >
            <Terminal size={12} strokeWidth={2} aria-hidden={true} />
            Call it
          </button>
          <button
            type="button"
            className="opr-btn opr-btn-sm"
            onClick={() => onOpen(tool.id)}
          >
            Open
            <ArrowRight size={12} strokeWidth={1.9} aria-hidden={true} />
          </button>
        </div>
      </div>
    );
  }
  // A tool-call block: the agent invoking the tool with args and its result.
  const argStr = Object.entries(item.call.args)
    .map(([k, v]) => `${k}: "${v}"`)
    .join(", ");
  return (
    <div className="opr-toolcall">
      <div className="opr-toolcall-line">
        <Terminal size={12} strokeWidth={2} aria-hidden={true} />
        <code>
          {tool.name}({argStr})
        </code>
      </div>
      <div className="opr-toolcall-result">{item.call.result}</div>
    </div>
  );
}

function ToolsRail({
  tools,
  selected,
  onSelect,
  onRun,
}: {
  tools: Tool[];
  selected: Tool | null;
  onSelect: (id: string) => void;
  onRun: (tool: Tool) => void;
}) {
  return (
    <aside className="opr-tools-rail">
      <div className="opr-tools-rail-head">
        <Eyebrow>Tools</Eyebrow>
        <span className="opr-scoped-note">
          {tools.length} workflow{tools.length === 1 ? "" : "s"} Nex built for
          you
        </span>
      </div>

      <div className="opr-tools-list">
        {tools.map((t) => (
          <button
            type="button"
            key={t.id}
            className={`opr-tool-row${selected?.id === t.id ? " is-active" : ""}`}
            onClick={() => onSelect(t.id)}
          >
            <span className="opr-tool-glyph" aria-hidden={true}>
              <Wrench size={12} strokeWidth={2} />
            </span>
            <span className="opr-tool-row-body">
              <code className="opr-tool-name">{t.name}</code>
              <span className="opr-tool-row-purpose">{t.purpose}</span>
            </span>
            {t.calls.length > 0 ? (
              <span className="opr-tool-runs">{t.calls.length}</span>
            ) : null}
          </button>
        ))}
      </div>

      {selected ? (
        <div className="opr-tool-detail">
          <div className="opr-tool-detail-head">
            <code className="opr-tool-name">{selected.name}</code>
            <button
              type="button"
              className="opr-btn opr-btn-primary opr-btn-sm"
              onClick={() => onRun(selected)}
            >
              <Terminal size={12} strokeWidth={2} aria-hidden={true} />
              Call
            </button>
          </div>
          <p className="opr-tool-purpose">{selected.purpose}</p>
          <div className="opr-tool-detail-label">Taught from</div>
          <p className="opr-tool-taught">“{selected.createdFrom}”</p>
          <div className="opr-tool-detail-label">Script</div>
          <pre className="opr-tool-script">
            <code>{selected.script}</code>
          </pre>
          {selected.calls.length > 0 ? (
            <>
              <div className="opr-tool-detail-label">Recent calls</div>
              <ul className="opr-tool-calls">
                {selected.calls.map((c) => (
                  <li key={c.id} className="opr-tool-call-item">
                    <span className="opr-tool-call-result">{c.result}</span>
                    <span className="opr-tool-call-at">{c.at}</span>
                  </li>
                ))}
              </ul>
            </>
          ) : null}
        </div>
      ) : null}
    </aside>
  );
}
