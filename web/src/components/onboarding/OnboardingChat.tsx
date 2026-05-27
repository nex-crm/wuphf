/**
 * OnboardingChat — full-screen CEO wizard.
 *
 * The user is NOT yet "in the office": until the broker flips
 * `onboarded=true`, RootRoute mounts this component instead of the office
 * Shell. It masquerades as a chat with the CEO, but the only valid input
 * zone is the chip / form-field card surfaced by InterviewBar (which falls
 * back to CeoCardSection when no agent interview request is pending). No
 * sidebar, no workspace rail, no workbench panes — those are the
 * destination, not the wizard.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import type { Message } from "../../api/client";
import { useMessages } from "../../hooks/useMessages";
import { formatTime } from "../../lib/format";
import { directChannelSlug } from "../../stores/app";
import { InterviewBar } from "../messages/InterviewBar";
import { MessageBubble } from "../messages/MessageBubble";
import { HarnessBadge } from "../ui/HarnessBadge";
import { PixelAvatar } from "../ui/PixelAvatar";
import { OnboardingDMContextProvider } from "./OnboardingDMRoute";
import type { CeoSuggestion } from "./types";
import { useOnboardingState } from "./useOnboardingState";

// ─── Choreography constants ───
const ANIMATION_MS = 600;
const THINK_MS = 500;
const GAP_MS = 300;
const POST_HUMAN_MS = 700;
const WORD_FADE_MS = 600;
const WORD_STAGGER_MS = 110;
const WIZARD_EASE = "cubic-bezier(0.2, 0, 0.8, 1)";

type CeoBubbleState = "thinking" | "revealing" | "revealed";

/**
 * CEO bubble — single element with both the thinking dots and the real
 * message content stacked in the .message-text slot as overlapping grid
 * layers, cross-faded via data-pending.
 */
function CeoOnboardingBubble({
  message,
  pending,
}: {
  message: Message;
  pending: boolean;
}) {
  return (
    <div
      className="message"
      data-msg-id={message.id}
      data-author-kind="agent"
      data-author-slug={CEO_AGENT_SLUG}
    >
      <div className="message-avatar avatar-with-harness" aria-hidden="true">
        <PixelAvatar slug={CEO_AGENT_SLUG} size={24} />
        <HarnessBadge
          kind="claude-code"
          size={14}
          className="harness-badge-on-avatar"
        />
      </div>
      <div className="message-content">
        <div className="message-header">
          <span className="message-author">CEO</span>
          <span className="badge badge-green">CEO</span>
          {!pending && message.timestamp ? (
            <span className="message-time" title={message.timestamp}>
              {formatTime(message.timestamp)}
            </span>
          ) : null}
        </div>
        <div className="message-text ceo-text-slot" data-pending={pending}>
          <div
            className="ceo-text-layer ceo-text-dots"
            aria-label="CEO is thinking"
            aria-hidden={!pending}
          >
            <span className="onboarding-thinking-dot" />
            <span className="onboarding-thinking-dot" />
            <span className="onboarding-thinking-dot" />
          </div>
          <div
            className="ceo-text-layer ceo-text-content"
            aria-hidden={pending}
          >
            <span aria-label={message.content}>
              {message.content.split(/(\s+)/).map((token, i) =>
                /^\s+$/.test(token) ? (
                  // biome-ignore lint/suspicious/noArrayIndexKey: positional tokens
                  <span key={`s${i}`} aria-hidden="true">
                    {token}
                  </span>
                ) : (
                  <span
                    // biome-ignore lint/suspicious/noArrayIndexKey: positional tokens
                    key={`w${i}`}
                    className="ceo-word"
                    aria-hidden="true"
                    style={{
                      transitionDelay: pending
                        ? "0ms"
                        : `${(i / 2) * WORD_STAGGER_MS}ms`,
                    }}
                  >
                    {token}
                  </span>
                ),
              )}
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}

function CeoMessageSlot({
  message,
  state,
  instant,
}: {
  message: Message;
  state: CeoBubbleState;
  instant?: boolean;
}) {
  const instantRef = useRef(instant === true);
  return (
    <div
      className={`onboarding-message-slot onboarding-ceo-slot${
        instantRef.current ? " onboarding-slot-instant" : ""
      }`}
      data-state={state}
      data-msg-id={message.id}
    >
      <CeoOnboardingBubble message={message} pending={state === "thinking"} />
    </div>
  );
}

function HumanMessageSlot({
  message,
  instant,
}: {
  message: Message;
  instant?: boolean;
}) {
  const instantRef = useRef(instant === true);
  return (
    <div
      className={`onboarding-message-slot onboarding-human-slot${
        instantRef.current ? " onboarding-slot-instant" : ""
      }`}
      data-msg-id={message.id}
    >
      <MessageBubble message={message} />
    </div>
  );
}

/** Agent slug used in the broker for the onboarding CEO. */
const CEO_AGENT_SLUG = "ceo";

/** The broker canonicalises DM channels as pair-sorted slugs. */
const CEO_ONBOARDING_CHANNEL = directChannelSlug(CEO_AGENT_SLUG);

function isHumanSender(from: string | undefined): boolean {
  if (!from) return false;
  const slug = from.toLowerCase();
  return slug === "you" || slug === "human";
}

function parsePendingSuggestion(raw: unknown): CeoSuggestion | null {
  if (!raw || typeof raw !== "object") return null;
  const obj = raw as Record<string, unknown>;
  if (typeof obj.id !== "string" || typeof obj.kind !== "string") return null;
  return raw as CeoSuggestion;
}

interface OnboardingChatProps {
  onBack?: () => void;
}

export function OnboardingChat({ onBack }: OnboardingChatProps = {}) {
  const { data: state } = useOnboardingState();
  const { data: messages = [] } = useMessages(CEO_ONBOARDING_CHANNEL);
  const queryClient = useQueryClient();
  const streamRef = useRef<HTMLDivElement>(null);
  const pairRef = useRef<HTMLDivElement>(null);
  const footerRef = useRef<HTMLElement>(null);

  const [atBottom, setAtBottom] = useState(true);
  const [hasOverflow, setHasOverflow] = useState(false);

  // Force-refresh the onboarding state whenever messages list changes,
  // collapsing the 3-second polling lag on pending_suggestion updates.
  useEffect(() => {
    queryClient.invalidateQueries({ queryKey: ["onboarding-state"] });
  }, [messages.length, queryClient]);

  // Measure footer height for the body's reserved padding-bottom and
  // bottom-fade overlay's `bottom` offset.
  useEffect(() => {
    if (!(pairRef.current && footerRef.current)) return;
    const pair = pairRef.current;
    const footer = footerRef.current;
    const update = () => {
      pair.style.setProperty(
        "--onboarding-footer-h",
        `${footer.offsetHeight}px`,
      );
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(footer);
    return () => observer.disconnect();
  }, []);

  // ─── Staged message reveal — three-state machine ───
  const [revealedIds, setRevealedIds] = useState<Set<string>>(
    () => new Set<string>(),
  );
  const [pendingMsg, setPendingMsg] = useState<Message | null>(null);
  const [pendingState, setPendingState] = useState<
    "idle" | "thinking" | "revealing"
  >("idle");
  const [postHumanDelay, setPostHumanDelay] = useState(false);
  const mountTimeRef = useRef<number>(Date.now());
  const hasSeededRef = useRef(false);

  // Effect 1: backlog flush + queue next CEO sequence.
  useEffect(() => {
    if (!hasSeededRef.current && messages.length > 0) {
      hasSeededRef.current = true;
      const cutoff = mountTimeRef.current;
      const backlog = messages
        .filter((m) => {
          const ts = m.timestamp ? Date.parse(m.timestamp) : NaN;
          return Number.isFinite(ts) && ts < cutoff;
        })
        .map((m) => m.id);
      if (backlog.length > 0) {
        setRevealedIds(new Set(backlog));
        return;
      }
    }

    // Human messages reveal immediately + raise the post-human delay.
    const unrevealedHuman = messages.find(
      (m) => !revealedIds.has(m.id) && isHumanSender(m.from),
    );
    if (unrevealedHuman) {
      setRevealedIds((prev) => {
        const out = new Set(prev);
        out.add(unrevealedHuman.id);
        return out;
      });
      setPostHumanDelay(true);
      return;
    }

    if (pendingState !== "idle" || postHumanDelay) return;

    const nextCeo = messages.find((m) => !revealedIds.has(m.id));
    if (!nextCeo || isHumanSender(nextCeo.from)) return;

    // Consecutive CEO messages skip thinking — flow back-to-back.
    let lastRevealedWasCeo = false;
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i];
      if (revealedIds.has(m.id)) {
        lastRevealedWasCeo = !isHumanSender(m.from);
        break;
      }
    }

    if (lastRevealedWasCeo) {
      const gapTimer = window.setTimeout(
        () => {
          setPendingMsg(nextCeo);
          setPendingState("revealing");
        },
        Math.round(GAP_MS / 2),
      );
      return () => window.clearTimeout(gapTimer);
    }

    const gapTimer = window.setTimeout(() => {
      setPendingMsg(nextCeo);
      setPendingState("thinking");
    }, GAP_MS);
    return () => window.clearTimeout(gapTimer);
  }, [messages, revealedIds, pendingState, postHumanDelay]);

  // Effect 1b: own the post-human breather timer.
  useEffect(() => {
    if (!postHumanDelay) return undefined;
    const t = window.setTimeout(() => {
      setPostHumanDelay(false);
    }, POST_HUMAN_MS);
    return () => window.clearTimeout(t);
  }, [postHumanDelay]);

  // Effect 2: advance through thinking → revealing → done.
  useEffect(() => {
    if (pendingState === "thinking" && pendingMsg) {
      const t = window.setTimeout(() => {
        setPendingState("revealing");
      }, THINK_MS);
      return () => window.clearTimeout(t);
    }
    if (pendingState === "revealing" && pendingMsg) {
      const revealingMsgId = pendingMsg.id;
      const t = window.setTimeout(() => {
        setRevealedIds((prev) => {
          const out = new Set(prev);
          out.add(revealingMsgId);
          return out;
        });
        setPendingMsg(null);
        setPendingState("idle");
      }, ANIMATION_MS);
      return () => window.clearTimeout(t);
    }
    return undefined;
  }, [pendingState, pendingMsg]);

  // Display list sorted by timestamp.
  const displayList = useMemo<
    Array<{
      msg: Message;
      state: CeoBubbleState;
      isHuman: boolean;
      isBacklog: boolean;
    }>
  >(() => {
    const cutoff = mountTimeRef.current;
    const out: Array<{
      msg: Message;
      state: CeoBubbleState;
      isHuman: boolean;
      isBacklog: boolean;
    }> = [];
    const ordered = [...messages].sort((a, b) => {
      const ta = a.timestamp ? Date.parse(a.timestamp) : 0;
      const tb = b.timestamp ? Date.parse(b.timestamp) : 0;
      if (ta === tb) return 0;
      return ta - tb;
    });
    for (const m of ordered) {
      const isHuman = isHumanSender(m.from);
      const ts = m.timestamp ? Date.parse(m.timestamp) : NaN;
      const isBacklog = Number.isFinite(ts) && ts < cutoff;
      if (revealedIds.has(m.id)) {
        out.push({ msg: m, state: "revealed", isHuman, isBacklog });
      } else if (
        !isHuman &&
        pendingMsg?.id === m.id &&
        pendingState !== "idle"
      ) {
        out.push({
          msg: m,
          state: pendingState as CeoBubbleState,
          isHuman: false,
          isBacklog: false,
        });
      }
    }
    return out;
  }, [messages, revealedIds, pendingMsg, pendingState]);

  // Track scroll position + overflow + auto-pin to bottom on stream growth.
  useEffect(() => {
    const el = streamRef.current;
    if (!el) return;
    const isAtBottom = () =>
      el.scrollHeight - el.scrollTop - el.clientHeight <= 2;
    const onScroll = () => setAtBottom(isAtBottom());
    const onResize = () => {
      setHasOverflow(el.scrollHeight - el.clientHeight > 2);
      el.scrollTop = el.scrollHeight;
      setAtBottom(true);
    };
    onScroll();
    onResize();
    el.addEventListener("scroll", onScroll, { passive: true });
    const observer = new ResizeObserver(onResize);
    observer.observe(el);
    const stream = el.firstElementChild;
    if (stream) observer.observe(stream);
    return () => {
      el.removeEventListener("scroll", onScroll);
      observer.disconnect();
    };
  }, []);

  const phase = state?.phase;
  const pendingSuggestion = parsePendingSuggestion(state?.pending_suggestion);

  // First greet (Office name?) is the only phase that should NOT blur
  // the input — the user hasn't interacted yet.
  const hasUnrevealedMessages = messages.some((m) => !revealedIds.has(m.id));
  const isFirstGreet = phase === "greet" && messages.length <= 1;
  const inputLocked =
    !isFirstGreet &&
    (pendingState !== "idle" ||
      postHumanDelay ||
      hasUnrevealedMessages ||
      !pendingSuggestion);

  // Blur active element on lock so a focused input can't sneak typing through.
  useEffect(() => {
    if (inputLocked && typeof document !== "undefined") {
      const active = document.activeElement as HTMLElement | null;
      if (active && typeof active.blur === "function") {
        active.blur();
      }
    }
  }, [inputLocked]);

  return (
    <OnboardingDMContextProvider value={{ phase, pendingSuggestion }}>
      <div
        className="onboarding-chat"
        data-testid="onboarding-chat"
        data-phase={phase ?? "loading"}
        data-ceo-typing={pendingState !== "idle" ? "true" : "false"}
        data-input-locked={inputLocked ? "true" : "false"}
        style={
          {
            "--onboarding-anim-ms": `${ANIMATION_MS}ms`,
            "--onboarding-anim-ease": WIZARD_EASE,
            "--onboarding-word-fade-ms": `${WORD_FADE_MS}ms`,
          } as React.CSSProperties
        }
      >
        <header className="onboarding-chat-header">
          {onBack ? (
            <button
              type="button"
              className="onboarding-chat-back"
              onClick={onBack}
              aria-label="Restart onboarding"
              title="Restart onboarding"
            >
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <line x1="19" y1="12" x2="5" y2="12" />
                <polyline points="12 19 5 12 12 5" />
              </svg>
              <span className="onboarding-chat-back-label">Restart</span>
            </button>
          ) : null}
          <span className="onboarding-chat-brand">WUPHF</span>
          <span className="onboarding-chat-spacer" aria-hidden="true" />
        </header>

        <div className="onboarding-chat-pair" ref={pairRef}>
          <main
            className="onboarding-chat-body"
            ref={streamRef}
            data-has-overflow={hasOverflow ? "true" : "false"}
          >
            <div className="onboarding-chat-stream">
              {displayList.map(({ msg, state, isHuman, isBacklog }) =>
                isHuman ? (
                  <HumanMessageSlot
                    key={msg.id}
                    message={msg}
                    instant={isBacklog}
                  />
                ) : (
                  <CeoMessageSlot
                    key={msg.id}
                    message={msg}
                    state={state}
                    instant={isBacklog}
                  />
                ),
              )}
            </div>
          </main>

          <div
            className="onboarding-chat-bottom-fade"
            data-visible={!atBottom}
            aria-hidden="true"
          />

          <footer className="onboarding-chat-footer" ref={footerRef}>
            <div className="onboarding-chat-footer-inner">
              <div
                className="onboarding-card-shell"
                data-empty={!pendingSuggestion ? "true" : "false"}
              >
                <InterviewBar />
                {!pendingSuggestion && (
                  <p className="onboarding-chat-hint">
                    Hang tight — the CEO is composing the next step.
                  </p>
                )}
              </div>
            </div>
          </footer>
        </div>
      </div>
    </OnboardingDMContextProvider>
  );
}
