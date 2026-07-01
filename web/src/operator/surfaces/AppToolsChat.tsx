// AppToolsChat — the app's Ask-AI chat, as the tool-teaching agent. Describe a
// repeatable workflow and the agent calls its create_tool tool: the chat renders
// the tool-call, and the new tool lands in the app's Tools tab (shared context).
// This is the FE shape of the harness chat agent's create_tool tool
// (harness/src/harness/tools.py) — mock authoring, no backend yet. See
// docs/specs/operator-workflows-as-tools.md.

import { useEffect, useRef, useState } from "react";
import { Send, Terminal } from "lucide-react";

import { authorToolFromDescription, type Tool } from "../tools/mockTools";
import { useAppTools } from "../tools/toolsContext";

interface AppToolsChatProps {
  appName: string;
  /** Optional first instruction (e.g. from a demo hand-off), sent on mount. */
  seed?: string;
}

type ChatItem =
  | { kind: "text"; id: string; from: "you" | "nex"; body: string }
  | { kind: "call"; id: string; tool: Tool };

let uid = 0;
function nextId(): string {
  uid += 1;
  return `tc_${uid}`;
}

// The create_tool call the chat shows, with the args the agent passed.
function callSignature(tool: Tool): string {
  const args = [
    `name: "${tool.name}"`,
    `title: "${tool.title}"`,
    tool.inputs.length
      ? `inputs: [${tool.inputs.map((i) => `"${i.name}"`).join(", ")}]`
      : null,
  ]
    .filter(Boolean)
    .join(", ");
  return `create_tool(${args})`;
}

export function AppToolsChat({ appName, seed }: AppToolsChatProps) {
  const { addTool } = useAppTools();
  const [items, setItems] = useState<ChatItem[]>(() => [
    {
      kind: "text",
      id: nextId(),
      from: "nex",
      body: `Tell me a repeatable task you do in ${appName} and I'll build it a tool you can call. Anything I make shows up under Tools.`,
    },
  ]);
  const [draft, setDraft] = useState("");
  const [thinking, setThinking] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const seededRef = useRef(false);

  function scrollDown() {
    requestAnimationFrame(() => {
      const el = scrollRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    });
  }

  function send(text?: string) {
    const body = (text ?? draft).trim();
    if (!body || thinking) return;
    setDraft("");
    setItems((prev) => [
      ...prev,
      { kind: "text", id: nextId(), from: "you", body },
    ]);
    setThinking(true);
    scrollDown();
    // The agent decides to make a tool and calls create_tool.
    window.setTimeout(() => {
      const tool = authorToolFromDescription(body);
      addTool(tool);
      setItems((prev) => [
        ...prev,
        { kind: "call", id: nextId(), tool },
        {
          kind: "text",
          id: nextId(),
          from: "nex",
          body: `Done — I built “${tool.title}”. It's in your Tools now, and I'll call it when you need it.`,
        },
      ]);
      setThinking(false);
      scrollDown();
    }, 650);
  }

  // biome-ignore lint/correctness/useExhaustiveDependencies: fire the seed once
  useEffect(() => {
    if (seed && !seededRef.current) {
      seededRef.current = true;
      send(seed);
    }
  }, [seed]);

  return (
    <div className="opr-builder opr-builder-panel">
      <div className="opr-builder-chat">
        <div className="opr-builder-scroll" ref={scrollRef}>
          {items.map((item) =>
            item.kind === "text" ? (
              <div key={item.id} className="opr-edit-msgwrap">
                <div
                  className={`opr-msg ${item.from === "nex" ? "opr-msg-ai" : "opr-msg-you"}`}
                >
                  {item.body}
                </div>
              </div>
            ) : (
              <div key={item.id} className="opr-toolcall">
                <div className="opr-toolcall-line">
                  <Terminal size={12} strokeWidth={2} aria-hidden={true} />
                  <code>{callSignature(item.tool)}</code>
                </div>
                <div className="opr-toolcall-result">
                  Created {item.tool.title}
                </div>
              </div>
            ),
          )}

          {thinking ? (
            <div
              className="opr-act-working"
              role="status"
              aria-label="Nex is building a tool"
            >
              <span className="opr-work-dots" aria-hidden={true}>
                <span />
                <span />
                <span />
              </span>
              <span className="opr-work-phrase">
                Nex is calling create_tool…
              </span>
            </div>
          ) : null}
        </div>

        <div className="opr-composer">
          <input
            className="opr-composer-input"
            aria-label="Describe a task for Nex to build a tool for"
            placeholder="Describe a task… e.g. draft a follow-up for a stalled deal"
            value={draft}
            disabled={thinking}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") send();
            }}
          />
          <button
            type="button"
            className="opr-btn opr-btn-primary"
            onClick={() => send()}
            disabled={thinking || !draft.trim()}
          >
            <Send size={14} strokeWidth={1.9} aria-hidden={true} />
            Send
          </button>
        </div>
      </div>
    </div>
  );
}
