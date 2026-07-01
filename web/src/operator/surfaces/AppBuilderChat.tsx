// AppBuilderChat — the operator's "describe an app and watch it build" surface.
// It reuses the shipped app-builder backend end-to-end: requestAppBuild kicks
// off an app-builder task, the live HeadlessEvent stream renders as a compact
// activity feed, and once the freshly-registered app appears under /apps we hand
// its id back so the shell opens its detail. No new build pipeline — only an
// operator-skinned front door onto the existing one.

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Check, Send, X } from "lucide-react";

import { type CustomApp, listApps, submitAppEdit } from "../../api/apps";
import { AppActivity } from "../../components/apps/AppActivity";
import {
  appBuildState,
  deriveAppName,
  resolveNewAppId,
  useBuildApp,
} from "../apps/useOperatorApps";
import { Eyebrow } from "../components/primitives";

const BUILD_POLL_MS = 3000;
// How long a terminal-on-first-sight candidate must STAY terminal before we
// accept it as the completed build (covers a build faster than one poll tick).
const TERMINAL_GRACE_MS = 15_000;

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

// A per-app chat transcript, persisted across the build chat -> app detail ->
// "Ask AI" edit chat transition (each is a SEPARATE AppBuilderChat instance) so
// clicking Ask AI after a build continues the same conversation instead of
// starting blank. Module-level (session-lived) is enough: this is UI history,
// not server state, and it should not survive a full reload.
const appChatHistory = new Map<string, ChatMessage[]>();

interface AppBuilderChatProps {
  onClose: () => void;
  /** Called once the built/updated app is ready, with its id. */
  onFinish: (appId: string) => void;
  /**
   * Called as soon as the new app's id is known — even while it is still
   * building — so the host can open its live preview beside the chat. Fires
   * once per build; not used in edit mode (the app already exists).
   */
  onBuildingApp?: (appId: string) => void;
  /**
   * When set, the chat edits an existing app instead of building a new one:
   * each message becomes an "improve" instruction to the app-builder, which
   * re-reads the app and republishes a new version.
   */
  editApp?: { id: string; name: string };
  /**
   * Render inside a docked drawer: hide the chat's own header (the drawer's
   * Ask-AI bar provides the title + close/resize controls instead).
   */
  panelMode?: boolean;
}

export function AppBuilderChat({
  onClose,
  onFinish,
  onBuildingApp,
  editApp,
  panelMode,
}: AppBuilderChatProps) {
  const [phase, setPhase] = useState<Phase>("intro");
  const [draft, setDraft] = useState("");
  const [messages, setMessages] = useState<ChatMessage[]>(() => {
    // Ask AI opened on a built app: continue the build conversation if we still
    // have it, so the transcript from describing + building the app stays.
    const saved = editApp ? appChatHistory.get(editApp.id) : undefined;
    if (saved && saved.length > 0) return saved;
    return [
      {
        id: "intro",
        from: "ai",
        body: editApp
          ? `Tell me what to change about “${editApp.name}”. I will apply it and publish a new version.`
          : "Tell me what this app should do. I will build it, and it will appear under Apps the moment it is ready.",
      },
    ];
  });
  const [appName, setAppName] = useState(editApp?.name ?? "");
  const [newAppId, setNewAppId] = useState<string | null>(null);
  // The app currently building/refining, used to stream its live activity
  // (thinking + tool calls) by APP ID alone via <AppActivity/>. Known
  // immediately for a refine; resolved from the pre-scaffolded app for a new
  // build. No task id is ever threaded through the operator surface.
  const [buildingAppId, setBuildingAppId] = useState<string | null>(null);
  const beforeIdsRef = useRef<ReadonlySet<string>>(new Set());
  // Edit mode completes on a version bump of the known app, not a new id.
  const startVersionRef = useRef<number>(0);
  const seqRef = useRef(0);
  const scrollRef = useRef<HTMLDivElement>(null);
  // The new app's id, reported to the host once (even while still building).
  const reportedBuildingRef = useRef<string | null>(null);
  // The app the in-flight message refines (an existing app, or the one this
  // chat already built). null means it is a brand-new build. Drives completion
  // detection so a follow-up amends instead of spawning a second build.
  const activeRefineRef = useRef<string | null>(null);
  // App ids we have OBSERVED in a "building" state during this in-flight build.
  // A new build only completes on a building -> terminal transition: without
  // this, resolveNewAppId can latch onto a stale already-ready app and declare
  // "Done" in a second without anything actually building.
  const sawBuildingRef = useRef<Set<string>>(new Set());
  // When a candidate is TERMINAL on first sight (never observed building), when
  // we first saw it that way. A very fast build can publish entirely between two
  // polls, so after a short grace window of the candidate staying terminal we
  // accept it rather than waiting forever (see the guard below).
  const terminalSinceRef = useRef<{ id: string; at: number } | null>(null);

  const build = useBuildApp();

  // Poll the app list only while a build is in flight; a new build is the app
  // whose id was not present when we started (robust to a renamed display
  // name); an edit is the known app once its version increments.
  const appsQuery = useQuery({
    queryKey: ["operator-apps"],
    queryFn: listApps,
    refetchInterval: phase === "building" ? BUILD_POLL_MS : false,
    enabled: phase === "building",
  });

  useEffect(() => {
    if (phase !== "building") return;
    const apps = appsQuery.data ?? [];

    // The app-builder pre-scaffolds a "building" app the instant the task starts,
    // so a new id appears long before the real publish. Wait for that app to
    // reach a terminal state (ready, or failed) before declaring anything — a
    // bare "new id appeared" is NOT done.
    // A refine (existing app, or the one this chat already built) completes on a
    // version bump; a brand-new build completes when its new id publishes.
    const refineId = activeRefineRef.current;
    let candidate: CustomApp | undefined;
    if (refineId) {
      const cur = apps.find((a) => a.id === refineId);
      if (cur && cur.version > startVersionRef.current) candidate = cur;
    } else {
      const id = resolveNewAppId(beforeIdsRef.current, apps);
      candidate = id ? apps.find((a) => a.id === id) : undefined;
    }
    if (!candidate) return;

    // Scope the live activity feed to the resolved app the moment it appears
    // (new build) — a refine already set this in send().
    setBuildingAppId(candidate.id);

    // Tell the host the app exists the moment it is resolved (even building), so
    // it can show the live preview beside the chat. New builds only.
    if (!refineId && reportedBuildingRef.current !== candidate.id) {
      reportedBuildingRef.current = candidate.id;
      onBuildingApp?.(candidate.id);
    }

    const state = appBuildState(candidate);
    if (state === "building") {
      // Record that we saw this app's build in flight, so a later ready/failed
      // poll is a real transition, not a latch onto a stale already-ready app.
      sawBuildingRef.current.add(candidate.id);
      return; // still building — keep waiting
    }
    // A NEW build only completes on an observed building -> terminal transition.
    // If the resolved candidate is already terminal on first sight (a stale app
    // resolveNewAppId picked, or a race before the real pre-scaffold appears),
    // do not declare "Done" immediately — keep polling for the actual build. But
    // a very fast build can also finish entirely between polls, so if the same
    // candidate stays terminal for a whole grace window, accept it instead of
    // leaving the chat in "building" forever.
    if (!(refineId || sawBuildingRef.current.has(candidate.id))) {
      const seen = terminalSinceRef.current;
      const now = Date.now();
      if (!seen || seen.id !== candidate.id) {
        terminalSinceRef.current = { id: candidate.id, at: now };
        return;
      }
      if (now - seen.at < TERMINAL_GRACE_MS) return;
      // Consistently terminal through the grace window — treat as completed.
    }

    setNewAppId(candidate.id);
    setPhase("done");
    const failed = state === "failed";
    setMessages((prev) => [
      ...prev,
      {
        id: `ai-${seqRef.current++}`,
        from: "ai",
        body: failed
          ? `“${appName}” stopped before it finished. Tell me what to change and I will try again.`
          : refineId
            ? `Done — I updated “${appName}”. Keep refining it, or open it.`
            : `“${appName}” is ready. Tell me any change to refine it, or open it.`,
      },
    ]);
  }, [appsQuery.data, phase, appName, onBuildingApp]);

  const lastMsgId = messages[messages.length - 1]?.id;
  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll on each new message
  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lastMsgId, phase]);

  // Persist the transcript keyed by the app id (known immediately for an edit,
  // resolved once a new build publishes) so the "Ask AI" edit chat continues
  // this same conversation instead of starting blank.
  useEffect(() => {
    const id = editApp?.id ?? newAppId;
    if (id) appChatHistory.set(id, messages);
  }, [messages, editApp, newAppId]);

  async function send(text?: string): Promise<void> {
    const description = (text ?? draft).trim();
    if (!description || phase === "building") return;
    setDraft("");
    // Once this chat has produced an app (or it opened to edit one), every
    // follow-up REFINES that app instead of starting a brand-new build — so a
    // second message like "also handle event signups" amends the same app.
    const refineId = editApp?.id ?? newAppId;
    const name = refineId
      ? appName || deriveAppName(description)
      : deriveAppName(description);
    activeRefineRef.current = refineId;
    // Fresh build/refine: forget any building-state we observed for a prior run.
    sawBuildingRef.current = new Set();
    terminalSinceRef.current = null;
    // Refine: the app id is known now, so the activity feed can attach
    // immediately. New build: clear it until the pre-scaffolded app resolves.
    setBuildingAppId(refineId);
    setAppName(name);
    setMessages((prev) => [
      ...prev,
      { id: `you-${seqRef.current++}`, from: "you", body: description },
      {
        id: `ai-${seqRef.current++}`,
        from: "ai",
        body: refineId
          ? `On it — refining “${name}”. You can watch it update below.`
          : `On it — building “${name}”. You can watch it come together below.`,
      },
    ]);
    try {
      // Snapshot completion baselines BEFORE entering the building phase. The
      // completion effect runs on the phase->"building" re-render; if we flipped
      // the phase first, that effect would fire while startVersionRef/beforeIds
      // still held STALE values (e.g. version 0) and instantly declare "Done"
      // for a refine before the edit even started. Capture first, then enter the
      // building phase (which is also what enables the poll + the effect).
      const before = await listApps();
      beforeIdsRef.current = new Set(before.map((a) => a.id));
      startVersionRef.current = refineId
        ? (before.find((a) => a.id === refineId)?.version ?? 0)
        : 0;
      setPhase("building");
      if (refineId) {
        // Refine through the app's edit channel so the proven task_followup
        // wake re-engages the App Builder (a new Improve task would be created
        // Running with no agent turn attending it, and would hang).
        await submitAppEdit(refineId, description);
      } else {
        await build.mutateAsync({ name, description });
      }
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
        {panelMode ? null : (
          <header className="opr-builder-head">
            <div>
              <Eyebrow>{editApp ? "Ask AI" : "Build an agent"}</Eyebrow>
              <div className="opr-builder-title">
                {phase === "intro"
                  ? editApp
                    ? `Edit “${editApp.name}”`
                    : "Describe it, I will build it"
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
        )}

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

          {phase === "building" ? (
            <div className="opr-build-activity">
              {/* Live thinking + tool-call chain, scoped to the app. Renders
                  nothing until the first event arrives, so the working
                  indicator below carries the initial seconds. */}
              <AppActivity appId={buildingAppId} />
              <div className="opr-act-working" aria-label="Building your app">
                <span className="opr-work-dots" aria-hidden={true}>
                  <span />
                  <span />
                  <span />
                </span>
                <span className="opr-work-phrase">
                  {editApp ? "Applying your change…" : "Building your app…"}
                </span>
              </div>
            </div>
          ) : null}

          {phase === "done" && newAppId ? (
            <div className="opr-finish-card">
              <div className="opr-finish-row">
                <span className="opr-finish-glyph" aria-hidden={true}>
                  <Check size={15} strokeWidth={2.2} />
                </span>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="opr-finish-name">{appName}</div>
                  <div className="opr-finish-sub">
                    <span>
                      {editApp ? "New version published" : "Ready to use"}
                    </span>
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

        {phase === "intro" && !editApp ? (
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
