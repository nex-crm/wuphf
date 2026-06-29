// AppBuilderChat — the operator's "describe an app and watch it build" surface.
// It reuses the shipped app-builder backend end-to-end: requestAppBuild kicks
// off an app-builder task, the live HeadlessEvent stream renders as a compact
// activity feed, and once the freshly-registered app appears under /apps we hand
// its id back so the shell opens its detail. No new build pipeline — only an
// operator-skinned front door onto the existing one.

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Check, Send, X } from "lucide-react";

import { listApps } from "../../api/apps";
import {
  extractBuildEvents,
  reduceBuildActivity,
} from "../../components/apps/buildActivity";
import { useAgentStream } from "../../hooks/useAgentStream";
import { APP_BUILDER_SLUG } from "../../lib/constants";
import {
  deriveAppName,
  resolveNewAppId,
  useBuildApp,
} from "../apps/useOperatorApps";
import { Eyebrow } from "../components/primitives";

const BUILD_POLL_MS = 3000;

type Phase = "intro" | "building" | "done";

interface ChatMessage {
  id: string;
  from: "you" | "ai";
  body: string;
}

const STARTERS: readonly string[] = [
  "A dashboard of our open tasks with their status",
  "A refund-approval form that posts to Slack",
  "A weekly pipeline summary I can glance at",
];

interface AppBuilderChatProps {
  onClose: () => void;
  /** Called once the built app has appeared under /apps, with its id. */
  onFinish: (appId: string) => void;
}

export function AppBuilderChat({ onClose, onFinish }: AppBuilderChatProps) {
  const [phase, setPhase] = useState<Phase>("intro");
  const [draft, setDraft] = useState("");
  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: "intro",
      from: "ai",
      body: "Tell me what this app should do. I will build it, and it will appear under Apps the moment it is ready.",
    },
  ]);
  const [appName, setAppName] = useState("");
  const [taskId, setTaskId] = useState<string | null>(null);
  const [newAppId, setNewAppId] = useState<string | null>(null);
  const beforeIdsRef = useRef<ReadonlySet<string>>(new Set());
  const seqRef = useRef(0);
  const scrollRef = useRef<HTMLDivElement>(null);

  const build = useBuildApp();

  // Poll the app list only while a build is in flight; the new app is the one
  // whose id was not present when we started (robust to a renamed display name).
  const appsQuery = useQuery({
    queryKey: ["operator-apps"],
    queryFn: listApps,
    refetchInterval: phase === "building" ? BUILD_POLL_MS : false,
    enabled: phase === "building",
  });

  useEffect(() => {
    if (phase !== "building") return;
    const found = resolveNewAppId(beforeIdsRef.current, appsQuery.data ?? []);
    if (found) {
      setNewAppId(found);
      setPhase("done");
      setMessages((prev) => [
        ...prev,
        {
          id: `ai-${seqRef.current++}`,
          from: "ai",
          body: `“${appName}” is ready. Open it to use and edit it.`,
        },
      ]);
    }
  }, [appsQuery.data, phase, appName]);

  const lastMsgId = messages[messages.length - 1]?.id;
  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll on each new message
  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lastMsgId, phase]);

  async function send(text?: string): Promise<void> {
    const description = (text ?? draft).trim();
    if (!description || phase === "building") return;
    setDraft("");
    const name = deriveAppName(description);
    setAppName(name);
    setMessages((prev) => [
      ...prev,
      { id: `you-${seqRef.current++}`, from: "you", body: description },
      {
        id: `ai-${seqRef.current++}`,
        from: "ai",
        body: `On it — building “${name}”. You can watch it come together below.`,
      },
    ]);
    setPhase("building");
    try {
      // Snapshot the existing app ids so the new one is unambiguous on arrival.
      const before = await listApps();
      beforeIdsRef.current = new Set(before.map((a) => a.id));
      const task = await build.mutateAsync({ name, description });
      setTaskId(task.id);
    } catch {
      setPhase("intro");
      setMessages((prev) => [
        ...prev,
        {
          id: `ai-${seqRef.current++}`,
          from: "ai",
          body: "I could not start the build just now. Check the workspace is running and try again.",
        },
      ]);
    }
  }

  return (
    <div className="opr-builder opr-builder-panel">
      <div className="opr-builder-chat">
        <header className="opr-builder-head">
          <div>
            <Eyebrow>Build an app</Eyebrow>
            <div className="opr-builder-title">
              {phase === "intro"
                ? "Describe it, I will build it"
                : appName || "Building your app"}
            </div>
          </div>
          <button
            type="button"
            className="opr-btn opr-btn-ghost opr-btn-sm"
            onClick={onClose}
            aria-label="Close builder"
          >
            <X size={13} strokeWidth={1.9} aria-hidden={true} />
            Close
          </button>
        </header>

        <div className="opr-builder-scroll" ref={scrollRef}>
          {messages.map((m) => (
            <div key={m.id} className="opr-edit-msgwrap">
              <div
                className={`opr-msg ${
                  m.from === "ai" ? "opr-msg-ai" : "opr-msg-you"
                }`}
              >
                {m.body}
              </div>
            </div>
          ))}

          {phase !== "intro" && taskId ? <BuildFeed taskId={taskId} /> : null}

          {phase === "done" && newAppId ? (
            <div className="opr-finish-card">
              <div className="opr-finish-row">
                <span className="opr-finish-glyph" aria-hidden={true}>
                  <Check size={15} strokeWidth={2.2} />
                </span>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="opr-finish-name">{appName}</div>
                  <div className="opr-finish-sub">
                    <span>Ready to use</span>
                  </div>
                </div>
              </div>
              <div className="opr-finish-actions">
                <button
                  type="button"
                  className="opr-btn opr-btn-primary opr-btn-sm"
                  onClick={() => onFinish(newAppId)}
                >
                  Open the app
                  <ArrowRight size={13} strokeWidth={1.9} aria-hidden={true} />
                </button>
              </div>
            </div>
          ) : null}
        </div>

        {phase === "intro" ? (
          <div className="opr-starters">
            <div className="opr-starters-label">Or start from one of these</div>
            {STARTERS.map((s) => (
              <button
                key={s}
                type="button"
                className="opr-starter-chip"
                onClick={() => void send(s)}
              >
                {s}
              </button>
            ))}
          </div>
        ) : null}

        <div className="opr-composer">
          <input
            className="opr-composer-input"
            aria-label="Describe the app you want to build"
            placeholder="Describe what this app should do..."
            value={draft}
            disabled={phase === "building"}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void send();
            }}
          />
          <button
            type="button"
            className="opr-btn opr-btn-primary"
            onClick={() => void send()}
            disabled={phase === "building" || !draft.trim()}
          >
            <Send size={14} strokeWidth={1.9} aria-hidden={true} />
            Send
          </button>
        </div>
      </div>
    </div>
  );
}

/**
 * BuildFeed renders the app-builder's live tool activity as one resolving row
 * per call (running → ✓/✗), reusing the shipped reducer so there are no zombie
 * spinners. Operator-skinned with the .opr-act-* vocabulary.
 */
function BuildFeed({ taskId }: { taskId: string }) {
  const { lines, connected } = useAgentStream(APP_BUILDER_SLUG, taskId, {
    keepAlive: true,
    maxLines: 5000,
  });
  const items = useMemo(
    () => reduceBuildActivity(extractBuildEvents(lines)),
    [lines],
  );
  const running = items.some((i) => i.status === "running");

  if (items.length === 0) {
    return (
      <div className="opr-act-working" aria-label="Build activity">
        <span className="opr-work-dots" aria-hidden={true}>
          <span />
          <span />
          <span />
        </span>
        <span className="opr-work-phrase">
          {connected ? "Setting up the build…" : "Connecting to the build…"}
        </span>
      </div>
    );
  }

  return (
    <div className="opr-build-feed" aria-label="Build activity">
      {items.map((item) => (
        <div key={item.id} className="opr-act-line">
          <span
            className={`opr-act-marker opr-act-marker-${item.status}`}
            aria-hidden={true}
          >
            {item.status === "running"
              ? "▸"
              : item.status === "error"
                ? "✗"
                : "✓"}
          </span>
          <span className="opr-act-tool">{item.verb}</span>
          {item.target ? (
            <span className="opr-act-result" title={item.target}>
              {item.target}
            </span>
          ) : null}
        </div>
      ))}
      {running ? (
        <div className="opr-act-working">
          <span className="opr-work-dots" aria-hidden={true}>
            <span />
            <span />
            <span />
          </span>
          <span className="opr-work-phrase">Building…</span>
        </div>
      ) : null}
    </div>
  );
}
