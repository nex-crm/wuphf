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

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import type { Message } from "../../api/client";
import { useMessages } from "../../hooks/useMessages";
import { directChannelSlug } from "../../stores/app";
import { InterviewBar } from "../messages/InterviewBar";
import { MessageBubble } from "../messages/MessageBubble";
import { OnboardingDMContextProvider } from "./OnboardingDMRoute";
import type { CeoSuggestion } from "./types";
import { useOnboardingState } from "./useOnboardingState";

// ─── Choreography constants ───
const ANIMATION_MS = 600;
const THINK_MS = 500;
const GAP_MS = 300;
const POST_HUMAN_MS = 200;
const WORD_FADE_MS = 600;
const WORD_STAGGER_MS = 90;
const WIZARD_EASE = "cubic-bezier(0.2, 0, 0.8, 1)";

type CeoBubbleState = "thinking" | "revealing" | "revealed";

/**
 * Single slot used for both CEO and human messages. The bubble itself is
 * always `MessageBubble`, so chrome (avatar, name, badge, timestamp,
 * spacing) is byte-for-byte identical across both sides. The only CEO-
 * specific additions are:
 *   1. A thinking-dot overlay rendered while `data-state="thinking"`. It
 *      appears/disappears instantly (no fade) when state flips.
 *   2. Per-word opacity reveal on the message text: after MessageBubble
 *      mounts, text nodes inside `.message-text` are split into
 *      `<span class="ceo-word">` spans with a per-word `transition-delay`,
 *      so CSS can fade each word in one-by-one when the slot transitions
 *      to `revealing` / `revealed`.
 */
function OnboardingMessageSlot({
  message,
  state,
  isCeo,
  instant,
}: {
  message: Message;
  state: CeoBubbleState;
  isCeo: boolean;
  instant?: boolean;
}) {
  const instantRef = useRef(instant === true);
  const slotRef = useRef<HTMLDivElement>(null);

  // After the bubble mounts, split CEO message text into per-word spans
  // so CSS can stagger the opacity reveal. Runs once per message.id.
  useEffect(() => {
    if (!isCeo) return;
    const slot = slotRef.current;
    if (!slot) return;
    const textEl = slot.querySelector(".message-text");
    if (!textEl || textEl.querySelector(".ceo-word")) return;

    const walker = document.createTreeWalker(textEl, NodeFilter.SHOW_TEXT);
    const textNodes: Text[] = [];
    let node: Node | null = walker.nextNode();
    while (node) {
      textNodes.push(node as Text);
      node = walker.nextNode();
    }

    let wordIndex = 0;
    for (const textNode of textNodes) {
      const text = textNode.textContent ?? "";
      if (!text) continue;
      const tokens = text.split(/(\s+)/);
      const fragment = document.createDocumentFragment();
      for (const token of tokens) {
        if (!token) continue;
        if (/^\s+$/.test(token)) {
          fragment.appendChild(document.createTextNode(token));
        } else {
          const span = document.createElement("span");
          span.className = "ceo-word";
          span.textContent = token;
          // For backlog (instant) messages, skip the stagger so they
          // settle into final state immediately.
          if (!instantRef.current) {
            span.style.transitionDelay = `${wordIndex * WORD_STAGGER_MS}ms`;
          }
          wordIndex++;
          fragment.appendChild(span);
        }
      }
      textNode.parentNode?.replaceChild(fragment, textNode);
    }
  }, [isCeo, message.id]);

  const overlayRef = useRef<HTMLDivElement>(null);

  // Anchor the thinking overlay to wherever `.message-text` actually sits
  // inside MessageBubble's layout — hardcoded offsets drift the moment
  // MessageBubble's internal padding/grid changes. Runs while thinking
  // because the text element exists (visibility-hidden, but laid out).
  useEffect(() => {
    if (!isCeo || state !== "thinking") return;
    const slot = slotRef.current;
    const overlay = overlayRef.current;
    if (!(slot && overlay)) return;
    const textEl = slot.querySelector(".message-text");
    if (!(textEl instanceof HTMLElement)) return;
    const place = () => {
      const slotRect = slot.getBoundingClientRect();
      const textRect = textEl.getBoundingClientRect();
      overlay.style.left = `${textRect.left - slotRect.left}px`;
      overlay.style.top = `${textRect.top - slotRect.top}px`;
    };
    place();
    const ro = new ResizeObserver(place);
    ro.observe(textEl);
    ro.observe(slot);
    return () => ro.disconnect();
  }, [isCeo, state]);

  return (
    <div
      ref={slotRef}
      className={`onboarding-message-slot ${
        isCeo ? "onboarding-ceo-slot" : "onboarding-human-slot"
      }${instantRef.current ? " onboarding-slot-instant" : ""}`}
      data-state={state}
      data-msg-id={message.id}
    >
      <MessageBubble message={message} />
      {isCeo && state === "thinking" ? (
        <div
          ref={overlayRef}
          className="onboarding-thinking-overlay"
          aria-label="CEO is thinking"
        >
          <span className="onboarding-thinking-dot" />
          <span className="onboarding-thinking-dot" />
          <span className="onboarding-thinking-dot" />
        </div>
      ) : null}
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

  // Ref to a sentinel div at the very end of the stream — see the JSX
  // below. We call scrollIntoView on it instead of writing scrollTop
  // arithmetic, because Safari and Chrome compute scrollHeight slightly
  // differently across layout passes (Safari leaves the last message
  // hidden under the footer; Chrome handles it). scrollIntoView is the
  // browser-native "make this visible" call and works consistently.
  const endOfStreamRef = useRef<HTMLDivElement>(null);

  // Single source of truth for "should we auto-pin to the bottom on the
  // next layout change." Mutated by the scroll listener further down.
  // Declared early so the footer-measurement effect (which fires before
  // the scroll-listener effect mounts) can also gate its re-pin on it.
  const wasAtBottomRef = useRef(true);

  // Pin the sentinel into view. Called from every effect that could
  // cause the bottom-of-stream to move out of frame (new message,
  // footer height change, animation tick). Always block:"end" so the
  // sentinel lines up just above the absolute-positioned footer, since
  // the body's padding-bottom already reserves footer-height of space.
  const scrollToBottom = useCallback(() => {
    if (!wasAtBottomRef.current) return;
    endOfStreamRef.current?.scrollIntoView({
      behavior: "auto",
      block: "end",
    });
  }, []);

  // Force-refresh the onboarding state whenever messages list changes,
  // collapsing the 3-second polling lag on pending_suggestion updates.
  useEffect(() => {
    queryClient.invalidateQueries({ queryKey: ["onboarding-state"] });
  }, [messages.length, queryClient]);

  // Measure footer height for the body's reserved padding-bottom and
  // bottom-fade overlay's `bottom` offset. When the footer grows (e.g.
  // form-field card → multi-option chip-row), the body's padding-bottom
  // grows along with it — which pushes the last message out of view.
  // We re-pin the scroll right after writing the new height so the
  // bottom of the stream stays in sight.
  useEffect(() => {
    if (!(pairRef.current && footerRef.current)) return;
    const pair = pairRef.current;
    const footer = footerRef.current;
    const update = () => {
      pair.style.setProperty(
        "--onboarding-footer-h",
        `${footer.offsetHeight}px`,
      );
      // rAF so the layout has applied the new padding-bottom before we
      // ask the browser to scroll the sentinel into view; otherwise we
      // pin to the pre-resize position and end up short by exactly the
      // height delta.
      requestAnimationFrame(scrollToBottom);
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

    // Every CEO message goes through thinking → revealing. We used to
    // skip thinking for "consecutive CEO" messages (when the last
    // revealed was also CEO), but that branch fired incorrectly when
    // the CEO message arrived in `messages` before the human echo
    // had finished posting: the just-revealed previous CEO message
    // looked like a CEO→CEO transition, so the next CEO popped in
    // with no thinking dots even though it was a real reply to a
    // human submit. Removing the skip means every CEO gets the
    // dots — slightly more motion for true CEO→CEO bursts (scan
    // → wiki updated → pick template) but consistent rhythm overall.
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
  // `wasAtBottomRef` (declared above) is the source of truth: we only
  // auto-pin if the user was at the bottom right before the growth, so a
  // deliberate scroll-up to re-read history is preserved across new
  // messages.
  useEffect(() => {
    const el = streamRef.current;
    if (!el) return;
    // "At bottom" = the end-of-stream sentinel is visible inside the
    // body's viewport. The naïve `scrollTop === scrollHeight -
    // clientHeight` check fails here because the body has 140px of
    // `padding-bottom` reserved for the absolute-positioned footer,
    // so the "true" scroll bottom is below the visible content area.
    // After scrollIntoView pins the sentinel at the viewport bottom,
    // scrollTop is ~140px short of true bottom and the naïve check
    // would flip wasAtBottomRef to false, killing auto-pin for the
    // next message.
    const isAtBottom = () => {
      const sentinel = endOfStreamRef.current;
      const footer = footerRef.current;
      if (!(sentinel && footer)) return true;
      // The body's rect bottom includes the padding-bottom reserved
      // for the footer, so it's NOT the right reference — the footer
      // visually covers that padding. Compare against the footer's
      // top: if the sentinel sits at or above it, the latest message
      // is visible above the footer and the user is "at bottom."
      return (
        sentinel.getBoundingClientRect().bottom <=
        footer.getBoundingClientRect().top + 4
      );
    };
    const onScroll = () => {
      const at = isAtBottom();
      wasAtBottomRef.current = at;
      setAtBottom(at);
    };
    const pinIfNeeded = () => {
      setHasOverflow(el.scrollHeight - el.clientHeight > 2);
      if (wasAtBottomRef.current) {
        scrollToBottom();
        setAtBottom(true);
      }
    };
    onScroll();
    pinIfNeeded();
    el.addEventListener("scroll", onScroll, { passive: true });
    const observer = new ResizeObserver(pinIfNeeded);
    observer.observe(el);
    const stream = el.firstElementChild;
    if (stream) observer.observe(stream);
    return () => {
      el.removeEventListener("scroll", onScroll);
      observer.disconnect();
    };
  }, []);

  // Pin sentinel into view whenever the message list grows. Runs after
  // every new message lands in the DOM. scrollIntoView is browser-
  // native "make this visible," which is consistent across Safari and
  // Chrome — unlike scrollTop arithmetic, which Safari computed
  // slightly differently and left the last message hidden behind the
  // footer.
  useEffect(() => {
    scrollToBottom();
  }, [messages.length, scrollToBottom]);

  // rAF pin loop while anything is animating. The sentinel may move as
  // slots reveal / footer height adjusts / images load; re-pin every
  // frame for the duration of the longest reveal step. Only fires if
  // the user was at the bottom (preserves scroll-back).
  useEffect(() => {
    if (pendingState === "idle" && !postHumanDelay) return;
    let rafId = 0;
    const stopAt = performance.now() + ANIMATION_MS + 200;
    const tick = (now: number) => {
      scrollToBottom();
      if (now < stopAt) {
        rafId = requestAnimationFrame(tick);
      }
    };
    rafId = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafId);
  }, [pendingState, postHumanDelay, displayList.length, scrollToBottom]);

  const phase = state?.phase;
  const pendingSuggestion = parsePendingSuggestion(state?.pending_suggestion);

  // Lock interaction while the CEO is mid-reveal or in the post-human
  // breather. Critically, we DO NOT lock on `!pendingSuggestion` anymore
  // — CeoCardSection now keeps the last suggestion sticky, so the
  // footer is never empty across phase transitions and there's nothing
  // to gate visibility on.
  //
  // First-greet exemption: on the very first render of the wizard, the
  // CEO's "Office name?" message is technically still "unrevealed" (it's
  // about to flip from thinking → revealing), but the user hasn't
  // interacted yet so muting their first input on arrival feels jarring.
  // Skip the lock when we're in the greet phase and only one CEO message
  // exists.
  const hasUnrevealedMessages = messages.some((m) => !revealedIds.has(m.id));
  const isFirstGreet = phase === "greet" && messages.length <= 1;
  const inputLocked =
    !isFirstGreet &&
    (pendingState !== "idle" || postHumanDelay || hasUnrevealedMessages);

  // Blur active element on lock so a focused input can't sneak typing
  // through. When the lock RELEASES, jump focus into the footer's first
  // focusable element (text input for form-field cards, first chip for
  // chip-row / checklist, etc.) so the user can keep typing without
  // clicking back into the field every turn.
  useEffect(() => {
    if (typeof document === "undefined") return;
    if (inputLocked) {
      const active = document.activeElement as HTMLElement | null;
      if (active && typeof active.blur === "function") {
        active.blur();
      }
      return;
    }
    // Lock just released. Wait a frame for the muted-state CSS
    // transition to start and for the card to become interactive
    // (pointer-events flips off via [data-input-locked] attribute),
    // then focus the first input/button inside the footer.
    const id = requestAnimationFrame(() => {
      const shell = document.querySelector(".onboarding-card-shell");
      if (!shell) return;
      const target = shell.querySelector<HTMLElement>(
        'input:not([disabled]),textarea:not([disabled]),[role="option"]:not([disabled]),button:not([disabled]):not(.ceo-card-skip)',
      );
      target?.focus();
    });
    return () => cancelAnimationFrame(id);
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
              {displayList.map(({ msg, state, isHuman, isBacklog }) => (
                <OnboardingMessageSlot
                  key={msg.id}
                  message={msg}
                  state={state}
                  isCeo={!isHuman}
                  instant={isBacklog}
                />
              ))}
              {/* Bottom-of-stream sentinel. Scroll-into-view'd whenever
                  new content lands. `scrollMarginBottom` is critical:
                  scrollIntoView pins the sentinel to the body's bottom
                  edge, which is behind the absolutely-positioned
                  footer. The scroll-margin tells the browser "leave
                  this much room below me when scrolling me into view,"
                  so the sentinel (and therefore the last message)
                  lands ABOVE the footer instead of behind it. */}
              <div
                ref={endOfStreamRef}
                aria-hidden="true"
                style={{
                  height: 1,
                  scrollMarginBottom:
                    "calc(var(--onboarding-footer-h, 120px) + 16px)",
                }}
              />
            </div>
          </main>

          <div
            className="onboarding-chat-bottom-fade"
            data-visible={!atBottom}
            aria-hidden="true"
          />

          <footer className="onboarding-chat-footer" ref={footerRef}>
            <div className="onboarding-chat-footer-inner">
              <div className="onboarding-card-shell">
                <InterviewBar />
              </div>
            </div>
          </footer>
        </div>
      </div>
    </OnboardingDMContextProvider>
  );
}
