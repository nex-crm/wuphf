import { type ReactNode, useMemo, useState } from "react";

import type { StreamLine } from "../../hooks/useAgentStream";

interface StreamLineViewProps {
  line: StreamLine;
  /** Compact mode collapses arrays and objects beyond the first level. */
  compact?: boolean;
}

/**
 * Renders one SSE line from the agent stream. Understands the broker's
 * OpenAI-Responses-style events — agent messages render as dim thinking
 * lines, tool calls render as collapsible cards, token totals render as
 * a single line. Everything else falls back to pretty-printed JSON.
 */
export function StreamLineView({ line, compact = false }: StreamLineViewProps) {
  if (!line.parsed) {
    // Raw chunks from agentStream.Push (local-LLM streaming text). The
    // useAgentStream hook coalesces consecutive raw events into a
    // single StreamLine, so this branch renders the running model
    // output as a continuous text block \u2014 not one chunk per row.
    // Trailing ellipsis keeps the live-output panel visually capped.
    const text =
      line.data.length > 1200 ? `${line.data.slice(0, 1200)}\u2026` : line.data;
    return <div className="stream-line stream-line-raw">{text}</div>;
  }

  const parsed = line.parsed;
  const evtType = typeof parsed.type === "string" ? parsed.type : "";

  // Skip noise events entirely
  if (
    evtType === "thread.started" ||
    evtType === "turn.started" ||
    evtType === "item.started"
  ) {
    return null;
  }

  // Token total line for turn/response completed
  if (evtType === "turn.completed" || evtType === "response.completed") {
    const tokens = renderTokens(parsed);
    if (tokens) return <div className="cc-token-line">{tokens}</div>;
    return null;
  }

  if (evtType === "mcp_tool_event") {
    const phase = stringish(parsed.phase);
    const tool = stringish(parsed.tool) || "tool";
    return (
      <ToolCallCard
        item={{
          type: "tool_call",
          name: phase ? `${phase}: ${tool}` : tool,
          arguments: parsed.arguments ?? parsed.args,
          result: parsed.result,
          error: parsed.error,
        }}
        compact={compact}
      />
    );
  }

  if (evtType === "assistant") {
    return <ClaudeAssistantEvent parsed={parsed} compact={compact} />;
  }

  if (evtType === "user") {
    return <ClaudeUserEvent parsed={parsed} compact={compact} />;
  }

  if (evtType === "result") {
    const text = stringish(parsed.result).trim();
    if (text) return <div className="cc-thinking">{text}</div>;
  }

  if (evtType === "response.output_text.delta") {
    const text = stringish(parsed.delta ?? parsed.text).trim();
    if (text) return <div className="cc-thinking">{text}</div>;
  }

  // item.completed → agent_message / tool call
  if (
    evtType === "item.completed" &&
    parsed.item &&
    typeof parsed.item === "object"
  ) {
    const item = parsed.item as Record<string, unknown>;
    const itemType = typeof item.type === "string" ? item.type : "";

    if (
      itemType === "agent_message" ||
      itemType === "message" ||
      itemType === "assistant"
    ) {
      const text = codexItemText(item);
      if (!text) return null;
      const truncated =
        text.length > 500 ? `${text.slice(0, 500)}\u2026` : text;
      return <div className="cc-thinking">{truncated}</div>;
    }

    if (
      itemType === "mcp_tool_call" ||
      itemType === "tool_call" ||
      itemType === "function_call"
    ) {
      return <ToolCallCard item={item} compact={compact} />;
    }

    // Other completed items are bookkeeping; drop.
    return null;
  }

  // Fallback: structured event with type/phase/agent + detail + extras
  return <GenericEventCard parsed={parsed} compact={compact} />;
}

function ClaudeAssistantEvent({
  parsed,
  compact,
}: {
  parsed: Record<string, unknown>;
  compact: boolean;
}) {
  const blocks = messageContentBlocks(parsed);
  const rendered = blocks
    .map((block, index) => {
      const blockType = stringish(block.type);
      if (blockType === "text") {
        const text = stringish(block.text).trim();
        return text ? (
          <div key={index} className="cc-thinking">
            {text}
          </div>
        ) : null;
      }
      if (blockType === "thinking") {
        const text = stringish(block.thinking).trim();
        return text ? (
          <div key={index} className="stream-card-detail">
            {text}
          </div>
        ) : null;
      }
      if (blockType === "tool_use") {
        return (
          <ToolCallCard
            key={index}
            item={{
              type: "tool_call",
              name: block.name,
              arguments: block.input,
            }}
            compact={compact}
          />
        );
      }
      return null;
    })
    .filter(Boolean);

  if (rendered.length === 0) return null;
  if (rendered.length === 1) return <>{rendered[0]}</>;
  return <div className="stream-event-stack">{rendered}</div>;
}

function ClaudeUserEvent({
  parsed,
  compact,
}: {
  parsed: Record<string, unknown>;
  compact: boolean;
}) {
  const blocks = messageContentBlocks(parsed);
  const rendered = blocks
    .map((block, index) => {
      if (stringish(block.type) !== "tool_result") return null;
      const content = block.content;
      return (
        <div key={index} className="cc-tool-call">
          <div className="cc-tool-section-label">Tool result</div>
          <ToolResultContent
            text={stringFromToolContent(content)}
            compact={compact}
          />
        </div>
      );
    })
    .filter(Boolean);

  const toolUseResult = parsed.tool_use_result;
  if (toolUseResult && typeof toolUseResult === "object") {
    const result = toolUseResult as Record<string, unknown>;
    const text = [stringish(result.stdout), stringish(result.stderr)]
      .filter(Boolean)
      .join("\n");
    if (text) {
      rendered.push(
        <div key="tool-use-result" className="cc-tool-call">
          <div className="cc-tool-section-label">Tool result</div>
          <ToolResultContent text={text} compact={compact} />
        </div>,
      );
    }
  }

  if (rendered.length === 0) return null;
  if (rendered.length === 1) return <>{rendered[0]}</>;
  return <div className="stream-event-stack">{rendered}</div>;
}

function messageContentBlocks(
  parsed: Record<string, unknown>,
): Record<string, unknown>[] {
  const message = parsed.message;
  if (!message || typeof message !== "object") return [];
  const content = (message as Record<string, unknown>).content;
  if (!Array.isArray(content)) return [];
  return content.filter(
    (block): block is Record<string, unknown> =>
      !!block && typeof block === "object",
  );
}

function codexItemText(item: Record<string, unknown>): string {
  const direct = stringish(item.text).trim();
  if (direct) return direct;
  const content = item.content;
  if (!Array.isArray(content)) return "";
  return content
    .map((part) => {
      if (!part || typeof part !== "object") return "";
      const p = part as Record<string, unknown>;
      const typ = stringish(p.type);
      if (typ && typ !== "output_text" && typ !== "text") return "";
      return stringish(p.text).trim();
    })
    .filter(Boolean)
    .join("\n");
}

function stringFromToolContent(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .map((item) => {
        if (typeof item === "string") return item;
        if (item && typeof item === "object") {
          const obj = item as Record<string, unknown>;
          return stringish(obj.text ?? obj.content);
        }
        return "";
      })
      .filter(Boolean)
      .join("\n");
  }
  if (content && typeof content === "object") {
    return JSON.stringify(content);
  }
  return "";
}

function renderTokens(parsed: Record<string, unknown>): string | null {
  const u = extractUsage(parsed);
  if (!u) return null;
  const inTok = toNum(u.input_tokens);
  const outTok = toNum(u.output_tokens);
  const cacheRead = toNum(
    u.cached_input_tokens ?? u.cache_read_input_tokens ?? u.cache_read_tokens,
  );
  const cacheCreate = toNum(
    u.cache_creation_input_tokens ?? u.cache_creation_tokens,
  );
  const total = inTok + outTok + cacheRead + cacheCreate;
  if (total === 0) return null;
  const parts = [`${formatTokens(inTok)} in`, `${formatTokens(outTok)} out`];
  if (cacheRead > 0) parts.push(`${formatTokens(cacheRead)} cache read`);
  if (cacheCreate > 0) parts.push(`${formatTokens(cacheCreate)} cache write`);
  return `\u2500\u2500 ${formatTokens(total)} tokens (${parts.join(", ")})`;
}

function extractUsage(
  parsed: Record<string, unknown>,
): Record<string, unknown> | null {
  const candidates: unknown[] = [
    parsed.usage,
    (parsed.response as Record<string, unknown> | undefined)?.usage,
    (parsed.turn as Record<string, unknown> | undefined)?.usage,
  ];
  for (const c of candidates) {
    if (c && typeof c === "object") return c as Record<string, unknown>;
  }
  return null;
}

function toNum(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

const ARG_SKIP = new Set(["my_slug", "new_topic", "viewer_slug", "tagged"]);

function ToolCallCard({
  item,
  compact,
}: {
  item: Record<string, unknown>;
  compact: boolean;
}) {
  const [open, setOpen] = useState(false);
  const toolName =
    (item.tool as string | undefined) ||
    (item.name as string | undefined) ||
    "tool";
  const args = objectFromToolField(item.arguments ?? item.args);
  const result = normalizeToolResult(item.result);
  // The tool-event payload omits `error` for successful calls, so the
  // field arrives as undefined (not null). The previous `!== null`
  // gate let undefined through and rendered an "× ERROR / undefined"
  // chip for every successful tool call. Treat null AND undefined AND
  // the empty string as "no error".
  const hasError =
    item.error !== null && item.error !== undefined && item.error !== "";
  const errorField = hasError ? item.error : null;

  const { summaryArg, summaryResult, summaryError } = useMemo(() => {
    const pick = [
      args.content,
      args.command,
      args.text,
      args.query,
      args.channel,
    ].find((v) => typeof v === "string" && v.length > 0);
    const sumArg =
      typeof pick === "string"
        ? pick.length > 80
          ? `${pick.slice(0, 80)}\u2026`
          : pick
        : "";

    let sumResult = "";
    if (result && Array.isArray(result.content)) {
      for (const c of result.content) {
        if (c.text) {
          let short = c.text;
          try {
            const rp = JSON.parse(c.text);
            if (rp && typeof rp === "object") {
              short =
                rp.message ||
                rp.status ||
                rp.result ||
                rp.text ||
                `${Object.keys(rp).length} fields`;
            }
          } catch {
            // keep plain text
          }
          sumResult = short.length > 60 ? `${short.slice(0, 60)}\u2026` : short;
          break;
        }
      }
    }

    let sumError = "";
    if (hasError) {
      sumError =
        typeof errorField === "string" ? errorField.slice(0, 60) : "Error";
    }

    return {
      summaryArg: sumArg,
      summaryResult: sumResult,
      summaryError: sumError,
    };
  }, [args, result, errorField, hasError]);

  const cleanArgs = useMemo<Record<string, unknown>>(() => {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(args)) {
      if (!ARG_SKIP.has(k) && v !== null && v !== "") out[k] = v;
    }
    return out;
  }, [args]);

  return (
    <div className="cc-tool-call">
      <button
        type="button"
        className="cc-tool-header"
        onClick={() => setOpen((o) => !o)}
      >
        <span className={`cc-tool-chevron${open ? " open" : ""}`}>▸</span>
        <span className="cc-tool-name">{toolName}</span>
        {summaryArg && <span className="cc-tool-summary">{summaryArg}</span>}
      </button>
      {summaryResult && !open && (
        <div className="cc-tool-result-summary">
          {"\u2713 "}
          {summaryResult}
        </div>
      )}
      {summaryError && !open && (
        <div className="cc-tool-error">
          {"\u2717 "}
          {summaryError}
        </div>
      )}
      {open && (
        <div className="cc-tool-body">
          {Object.keys(cleanArgs).length > 0 && (
            <>
              <div className="cc-tool-section-label">Args</div>
              <Value value={cleanArgs} depth={1} compact={compact} />
            </>
          )}
          {result &&
            Array.isArray(result.content) &&
            result.content.length > 0 && (
              <>
                <div className="cc-tool-section-label cc-tool-result-label">
                  {"\u2713 Response"}
                </div>
                {result.content.map((c, i) => (
                  <ToolResultContent key={i} text={c.text} compact={compact} />
                ))}
              </>
            )}
          {hasError && (
            <>
              <div className="cc-tool-section-label cc-tool-error">
                {"\u2717 Error"}
              </div>
              <ToolErrorContent error={errorField} compact={compact} />
            </>
          )}
        </div>
      )}
    </div>
  );
}

function objectFromToolField(value: unknown): Record<string, unknown> {
  if (!value) return {};
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      // keep as scalar below
    }
    return { value };
  }
  if (typeof value === "object" && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  return { value };
}

function normalizeToolResult(
  value: unknown,
): { content?: Array<{ text?: string }> } | undefined {
  if (value === null || value === "") return undefined;
  if (typeof value === "object" && !Array.isArray(value)) {
    const obj = value as { content?: Array<{ text?: string }> };
    if (Array.isArray(obj.content)) return obj;
    return { content: [{ text: JSON.stringify(value) }] };
  }
  return {
    content: [
      { text: typeof value === "string" ? value : JSON.stringify(value) },
    ],
  };
}

function ToolResultContent({
  text,
  compact,
}: {
  text?: string;
  compact: boolean;
}) {
  if (!text) return null;
  try {
    const parsed = JSON.parse(text);
    if (parsed && typeof parsed === "object") {
      return <Value value={parsed} depth={1} compact={compact} />;
    }
  } catch {
    // fall through
  }
  return <div className="cc-tool-result-inline">{text}</div>;
}

function ToolErrorContent({
  error,
  compact,
}: {
  error: unknown;
  compact: boolean;
}) {
  if (typeof error === "string") {
    try {
      const parsed = JSON.parse(error);
      if (parsed && typeof parsed === "object") {
        return <Value value={parsed} depth={1} compact={compact} />;
      }
    } catch {
      // fall through
    }
    return <div className="cc-tool-error-text">{error}</div>;
  }
  return <Value value={error} depth={1} compact={compact} />;
}

const NOISE_KEYS = new Set([
  "type",
  "activity",
  "phase",
  "status",
  "event",
  "agent",
  "from",
  "slug",
  "timestamp",
  "ts",
  "time",
  "detail",
  "content",
  "message",
  "text",
  "summary",
  "thread_id",
  "item",
  "id",
  "error",
  "result",
  "structured_content",
]);

function GenericEventCard({
  parsed,
  compact,
}: {
  parsed: Record<string, unknown>;
  compact: boolean;
}) {
  const phase = stringish(
    parsed.activity ?? parsed.phase ?? parsed.status ?? parsed.type,
  );
  const agent = stringish(parsed.agent ?? parsed.from ?? parsed.slug);
  const detail = stringish(
    parsed.detail ??
      parsed.content ??
      parsed.message ??
      parsed.text ??
      parsed.summary,
  );

  const extras = useMemo<Record<string, unknown>>(() => {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(parsed)) {
      if (NOISE_KEYS.has(k)) continue;
      if (v === null || v === "" || v === false) continue;
      out[k] = v;
    }
    return out;
  }, [parsed]);

  return (
    <div className="stream-card">
      {(phase || agent) && (
        <div className="stream-card-header">
          {phase && (
            <span
              className={`stream-card-phase stream-phase-${phase.replace(/[^a-z]/gi, "").toLowerCase()}`}
            >
              {phase}
            </span>
          )}
          {agent && <span className="stream-card-agent">{agent}</span>}
        </div>
      )}
      {detail && (
        <div className="stream-card-detail">
          {detail.length > 300 ? `${detail.slice(0, 300)}\u2026` : detail}
        </div>
      )}
      {Object.keys(extras).length > 0 && Object.keys(extras).length <= 8 && (
        <div className="stream-line-json">
          <Value value={extras} depth={0} compact={compact} />
        </div>
      )}
    </div>
  );
}

function stringish(v: unknown): string {
  return typeof v === "string" ? v : "";
}

/* ───── JSON tree primitive (shared by both card and fallback paths) ───── */

function Value({
  value,
  depth,
  compact,
}: {
  value: unknown;
  depth: number;
  compact: boolean;
}): ReactNode {
  if (value === null) return <span className="sv-null">null</span>;
  if (typeof value === "boolean")
    return <span className="sv-bool">{String(value)}</span>;
  if (typeof value === "number")
    return <span className="sv-num">{String(value)}</span>;
  if (typeof value === "string") {
    if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/.test(value)) {
      let display = value;
      try {
        display = new Date(value).toLocaleString([], {
          hour: "numeric",
          minute: "2-digit",
          second: "2-digit",
        });
      } catch {
        // keep raw
      }
      return (
        <span className="sv-ts" title={value}>
          {display}
        </span>
      );
    }
    const truncated =
      depth > 0 && value.length > 200 ? `${value.slice(0, 200)}\u2026` : value;
    return <span className="sv-str">{truncated}</span>;
  }
  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="sv-null">[]</span>;
    if ((compact && depth >= 1) || depth > 3)
      return <span className="sv-str">[{value.length} items]</span>;
    return (
      <Collapsible label={`[${value.length}]`} startOpen={depth === 0}>
        <div className="sv-array">
          {value.map((item, idx) => (
            <div key={idx} className="sv-array-item">
              <Value value={item} depth={depth + 1} compact={compact} />
            </div>
          ))}
        </div>
      </Collapsible>
    );
  }
  if (typeof value === "object") {
    const keys = Object.keys(value as Record<string, unknown>);
    if (keys.length === 0) return <span className="sv-null">{"{}"}</span>;
    if ((compact && depth >= 1) || depth > 3)
      return <span className="sv-str">{`{${keys.length} fields}`}</span>;
    return (
      <Collapsible label={`{${keys.length}}`} startOpen={depth === 0}>
        <div className="sv-obj">
          {keys.map((k) => (
            <div key={k} className="sv-obj-row">
              <span className="sv-key">{k}</span>
              <Value
                value={(value as Record<string, unknown>)[k]}
                depth={depth + 1}
                compact={compact}
              />
            </div>
          ))}
        </div>
      </Collapsible>
    );
  }
  return <span className="sv-str">{String(value)}</span>;
}

function Collapsible({
  label,
  startOpen,
  children,
}: {
  label: string;
  startOpen: boolean;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(startOpen);
  if (open) {
    return (
      <span className="sv-collapsible">
        <button
          type="button"
          className="sv-toggle"
          onClick={() => setOpen(false)}
          title="Collapse"
        >
          ▾ {label}
        </button>
        {children}
      </span>
    );
  }
  return (
    <button
      type="button"
      className="sv-toggle"
      onClick={() => setOpen(true)}
      title="Expand"
    >
      ▸ {label}
    </button>
  );
}
