// AppToolsChat — the app's Ask-AI chat, as the tool-teaching AND tool-calling
// agent. Describe a repeatable workflow and it calls the pi-mono agent's
// /tools/build endpoint, where the chat agent's create_tool tool authors it
// (agent/src/tools.ts); the tool lands in the app's Tools tab (shared context).
// Ask it to RUN one ("run the weekly summary") and the chat calls the tool via
// /tools/call — sandboxed execution (agent/src/toolRuntime.ts). A gated
// capability (external send/assign) pauses with an inline approval card:
// Approve re-calls with approved=true, Not now skips (CQ1, default deny).
// There is NO Run button anywhere — the chat is the only caller.
// See docs/specs/operator-workflows-as-tools.md.

import { useEffect, useRef, useState } from "react";
import { Send, Terminal } from "lucide-react";

import type { Tool, ToolCall } from "../tools/mockTools";
import {
  buildToolFromChat,
  callToolViaAgent,
  type ToolCallGate,
  type ToolCallOutcome,
} from "../tools/toolAgentClient";
import { useAppTools } from "../tools/toolsContext";

interface AppToolsChatProps {
  appName: string;
  /** Optional first instruction (e.g. from a demo hand-off), sent on mount. */
  seed?: string;
}

type ChatItem =
  | { kind: "text"; id: string; from: "you" | "nex"; body: string }
  | { kind: "call"; id: string; tool: Tool }
  | {
      kind: "invoke";
      id: string;
      tool: Tool;
      args: Record<string, string>;
      outcome: ToolCallOutcome;
    };

interface PendingApproval {
  tool: Tool;
  args: Record<string, string>;
  gate: ToolCallGate;
}

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

// The tool call the chat shows: toolName(args).
function invokeSignature(tool: Tool, args: Record<string, string>): string {
  const parts = tool.inputs
    .map((i) => (args[i.name] ? `${i.name}: "${args[i.name]}"` : null))
    .filter(Boolean)
    .join(", ");
  return `${tool.name}(${parts})`;
}

// Does this message invoke an existing tool (vs teach a new one)? Heuristic:
// an exact mention of a tool's callable name or full title, OR a run/call/use
// prefix plus the strongest title-word overlap. Conversational, kept light.
function matchTool(text: string, tools: Tool[]): Tool | null {
  const lower = text.toLowerCase();
  const exact = tools.find(
    (t) =>
      lower.includes(t.name.toLowerCase()) ||
      lower.includes(t.title.toLowerCase()),
  );
  if (exact) return exact;
  if (!/^\s*(run|call|use)\b/i.test(text)) return null;
  let best: Tool | null = null;
  let bestScore = 0;
  for (const t of tools) {
    const words = t.title
      .toLowerCase()
      .split(/[^a-z0-9]+/)
      .filter((w) => w.length > 2);
    const score = words.filter((w) => lower.includes(w)).length;
    if (score > bestScore) {
      best = t;
      bestScore = score;
    }
  }
  return best;
}

// Light conversational arg extraction: for each declared input take a quoted
// string if present, else the last capitalized word, else the raw remainder.
function extractArgs(text: string, tool: Tool): Record<string, string> {
  const args: Record<string, string> = {};
  const quoted = [...text.matchAll(/["“”']([^"“”']+)["“”']/g)].map((m) => m[1]);
  const caps = [...text.matchAll(/\b([A-Z][A-Za-z0-9]+)\b/g)].map((m) => m[1]);
  const remainder = text.replace(/^\s*(run|call|use)\b\s*/i, "").trim();
  let qi = 0;
  for (const input of tool.inputs) {
    if (qi < quoted.length) {
      args[input.name] = quoted[qi];
      qi += 1;
    } else {
      args[input.name] = caps.at(-1) ?? remainder;
    }
  }
  return args;
}

export function AppToolsChat({ appName, seed }: AppToolsChatProps) {
  const { tools, addTool, logCall } = useAppTools();
  const [items, setItems] = useState<ChatItem[]>(() => [
    {
      kind: "text",
      id: nextId(),
      from: "nex",
      body: `Tell me a repeatable task you do in ${appName} and I'll build it a tool you can call. Anything I make shows up under Tools — ask me to run one any time.`,
    },
  ]);
  const [draft, setDraft] = useState("");
  const [working, setWorking] = useState<string | null>(null);
  const [pending, setPending] = useState<PendingApproval | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const seededRef = useRef(false);

  function scrollDown() {
    requestAnimationFrame(() => {
      const el = scrollRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    });
  }

  // A completed (ok/error) call: render it, and log ok calls on the tool's
  // shared history so the Tools tab shows the last run.
  function finishCall(
    tool: Tool,
    args: Record<string, string>,
    outcome: ToolCallOutcome,
  ) {
    setItems((prev) => [
      ...prev,
      { kind: "invoke", id: nextId(), tool, args, outcome },
    ]);
    if (outcome.status === "ok") {
      const call: ToolCall = {
        id: nextId(),
        args,
        result: outcome.result ?? "done",
        at: "just now",
      };
      logCall(tool.id, call);
    }
  }

  async function invokeTool(tool: Tool, args: Record<string, string>) {
    setWorking(`Nex is calling ${tool.name}…`);
    scrollDown();
    const outcome = await callToolViaAgent(tool, args, false);
    if (outcome.status === "needs_approval" && outcome.gate) {
      // Paused at the send-gate: show the call, then the approval card. The
      // pending call lives in React state until Approve / Not now.
      setItems((prev) => [
        ...prev,
        { kind: "invoke", id: nextId(), tool, args, outcome },
      ]);
      setPending({ tool, args, gate: outcome.gate });
    } else {
      finishCall(tool, args, outcome);
    }
    setWorking(null);
    scrollDown();
  }

  async function approvePending() {
    const p = pending;
    if (!p || working) return;
    setPending(null);
    setWorking(`Nex is calling ${p.tool.name}…`);
    const outcome = await callToolViaAgent(p.tool, p.args, true);
    finishCall(p.tool, p.args, outcome);
    setWorking(null);
    scrollDown();
  }

  function declinePending() {
    if (!pending || working) return;
    setPending(null);
    setItems((prev) => [
      ...prev,
      {
        kind: "text",
        id: nextId(),
        from: "nex",
        body: "Okay — I didn't send it. Nothing left this app.",
      },
    ]);
    scrollDown();
  }

  async function send(text?: string) {
    const body = (text ?? draft).trim();
    if (!body || working || pending) return;
    setDraft("");
    setItems((prev) => [
      ...prev,
      { kind: "text", id: nextId(), from: "you", body },
    ]);
    // Invoking an existing tool? The chat CALLS it (there is no Run button).
    const invoked = matchTool(body, tools);
    if (invoked) {
      await invokeTool(invoked, extractArgs(body, invoked));
      return;
    }
    setWorking("Nex is calling create_tool…");
    scrollDown();
    // The pi-mono chat agent decides to make a tool and calls create_tool; we
    // render that call and drop the tool into the shared Tools state.
    const { tool, offline } = await buildToolFromChat(body, appName);
    addTool(tool);
    setItems((prev) => [
      ...prev,
      { kind: "call", id: nextId(), tool },
      {
        kind: "text",
        id: nextId(),
        from: "nex",
        body: `Done — I built “${tool.title}”. It's in your Tools now, and I'll call it when you need it.${
          offline
            ? " (built offline — start the agent to use the live one.)"
            : ""
        }`,
      },
    ]);
    setWorking(null);
    scrollDown();
  }

  // biome-ignore lint/correctness/useExhaustiveDependencies: fire the seed once
  useEffect(() => {
    if (seed && !seededRef.current) {
      seededRef.current = true;
      void send(seed);
    }
  }, [seed]);

  return (
    <div className="opr-builder opr-builder-panel">
      <div className="opr-builder-chat">
        <div className="opr-builder-scroll" ref={scrollRef}>
          {items.map((item) => {
            if (item.kind === "text") {
              return (
                <div key={item.id} className="opr-edit-msgwrap">
                  <div
                    className={`opr-msg ${item.from === "nex" ? "opr-msg-ai" : "opr-msg-you"}`}
                  >
                    {item.body}
                  </div>
                </div>
              );
            }
            if (item.kind === "call") {
              return (
                <div key={item.id} className="opr-toolcall">
                  <div className="opr-toolcall-line">
                    <Terminal size={12} strokeWidth={2} aria-hidden={true} />
                    <code>{callSignature(item.tool)}</code>
                  </div>
                  <div className="opr-toolcall-result">
                    Created {item.tool.title}
                  </div>
                </div>
              );
            }
            return (
              <div key={item.id} className="opr-toolcall">
                <div className="opr-toolcall-line">
                  <Terminal size={12} strokeWidth={2} aria-hidden={true} />
                  <code>{invokeSignature(item.tool, item.args)}</code>
                </div>
                <div className="opr-toolcall-result">
                  {item.outcome.status === "ok"
                    ? item.outcome.result
                    : item.outcome.status === "needs_approval"
                      ? "Paused — this needs your approval."
                      : (item.outcome.detail ?? "Something went wrong.")}
                </div>
                {item.outcome.actions.length > 0 ? (
                  <div className="opr-toolcall-actions">
                    {item.outcome.actions.map((a) => (
                      <code key={a}>{a}</code>
                    ))}
                  </div>
                ) : null}
              </div>
            );
          })}

          {pending ? (
            <div className="opr-browser-ask">
              <div className="opr-browser-ask-head">
                {pending.gate.capability === "nex.browser"
                  ? "Control your browser?"
                  : "Confirm this send"}
              </div>
              <p className="opr-browser-ask-body">
                This will {pending.gate.detail}.{" "}
                {pending.gate.capability === "nex.browser"
                  ? "Allow it?"
                  : "Send it?"}
              </p>
              <div className="opr-browser-ask-actions">
                <button
                  type="button"
                  className="opr-btn opr-btn-primary opr-btn-sm"
                  onClick={() => void approvePending()}
                  disabled={working !== null}
                >
                  Approve
                </button>
                <button
                  type="button"
                  className="opr-btn opr-btn-ghost opr-btn-sm"
                  onClick={declinePending}
                  disabled={working !== null}
                >
                  Not now
                </button>
              </div>
            </div>
          ) : null}

          {working ? (
            <div
              className="opr-act-working"
              role="status"
              aria-label="Nex is working"
            >
              <span className="opr-work-dots" aria-hidden={true}>
                <span />
                <span />
                <span />
              </span>
              <span className="opr-work-phrase">{working}</span>
            </div>
          ) : null}
        </div>

        <div className="opr-composer">
          <input
            className="opr-composer-input"
            aria-label="Describe a task for Nex to build a tool for"
            placeholder="Describe a task… or “run the weekly summary”"
            value={draft}
            disabled={working !== null || pending !== null}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void send();
            }}
          />
          <button
            type="button"
            className="opr-btn opr-btn-primary"
            onClick={() => void send()}
            disabled={working !== null || pending !== null || !draft.trim()}
          >
            <Send size={14} strokeWidth={1.9} aria-hidden={true} />
            Send
          </button>
        </div>
      </div>
    </div>
  );
}
