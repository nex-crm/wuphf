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
 *
 * Lifts from the onboarding spec (docs/specs/onboarding-into-office.md):
 *   "Onboarding is a wizard mocked as a CEO chat. The user is not really in
 *    the office yet until onboarding finishes."
 *
 * Component shape mirrors DMView's chat region but drops the workbench
 * (`AgentWorkbenchPane`) and the free-form `Composer`. We keep the same
 * `useMessages` / `MessageBubble` / `InterviewBar` plumbing so streaming
 * updates and pending-suggestion cards behave identically.
 */

import { useEffect, useRef } from "react";

import { useMessages } from "../../hooks/useMessages";
import { directChannelSlug } from "../../stores/app";
import { InterviewBar } from "../messages/InterviewBar";
import { MessageBubble } from "../messages/MessageBubble";
import { TypingIndicator } from "../messages/TypingIndicator";
import { OnboardingDMContextProvider } from "./OnboardingDMRoute";
import type { CeoSuggestion } from "./types";
import { useOnboardingState } from "./useOnboardingState";

/** Agent slug used in the broker for the onboarding CEO. */
const CEO_AGENT_SLUG = "ceo";

/** The broker canonicalises DM channels as pair-sorted slugs. */
const CEO_ONBOARDING_CHANNEL = directChannelSlug(CEO_AGENT_SLUG);

/**
 * Phase → human-readable label for the wizard header. Order matches the
 * deterministic state machine in broker_onboarding_phase2.go. The labels
 * are intentionally short — they replace the dot-progress indicator the
 * old wizard used so the user can see where they are without leaving the
 * conversation.
 */
const PHASE_LABELS: Record<string, string> = {
  greet: "Step 1 of 5 · Office name",
  identity: "Step 2 of 5 · Who you are",
  blueprint: "Step 3 of 5 · Pick a starting blueprint",
  team: "Step 4 of 5 · Confirm the team",
  bridge: "Step 5 of 5 · First task",
  draft: "Drafting your first issue",
  approve: "Review and approve",
  kickoff: "Starting your office",
  complete: "Done",
};

function phaseLabel(phase: string | undefined): string {
  if (!phase) return "Loading…";
  return PHASE_LABELS[phase] ?? `Phase: ${phase}`;
}

function parsePendingSuggestion(raw: unknown): CeoSuggestion | null {
  if (!raw || typeof raw !== "object") return null;
  const obj = raw as Record<string, unknown>;
  if (typeof obj.id !== "string" || typeof obj.kind !== "string") return null;
  return raw as CeoSuggestion;
}

export function OnboardingChat() {
  const { data: state } = useOnboardingState();
  const { data: messages = [] } = useMessages(CEO_ONBOARDING_CHANNEL);
  const streamRef = useRef<HTMLDivElement>(null);

  // Auto-scroll the message feed when new CEO messages arrive. We pin to the
  // bottom because the wizard is strictly forward — old messages stay visible
  // above as a transcript, but the action is always at the bottom.
  const messagesLength = messages.length;
  useEffect(() => {
    if (streamRef.current) {
      streamRef.current.scrollTop = streamRef.current.scrollHeight;
    }
  }, [messagesLength]);

  const phase = state?.phase;
  const pendingSuggestion = parsePendingSuggestion(state?.pending_suggestion);

  return (
    <OnboardingDMContextProvider value={{ phase, pendingSuggestion }}>
      <div
        className="onboarding-chat"
        data-testid="onboarding-chat"
        data-phase={phase ?? "loading"}
      >
        <header className="onboarding-chat-header">
          <span className="onboarding-chat-brand">WUPHF</span>
          <span className="onboarding-chat-phase">{phaseLabel(phase)}</span>
        </header>

        <main className="onboarding-chat-body" ref={streamRef}>
          <div className="onboarding-chat-stream">
            {messages.length === 0 ? (
              <p className="onboarding-chat-stream-empty">
                CEO is opening the office…
              </p>
            ) : (
              messages.map((msg) => (
                <MessageBubble key={msg.id} message={msg} />
              ))
            )}
            <TypingIndicator />
          </div>
        </main>

        <footer className="onboarding-chat-footer">
          <div className="onboarding-chat-footer-inner">
            <InterviewBar />
            {/* When there's no pending suggestion AND no agent interview
                request, InterviewBar renders nothing. Surface a hint so the
                user knows the wizard is mid-transition rather than stuck. */}
            {!pendingSuggestion && (
              <p className="onboarding-chat-hint">
                Hang tight — the CEO is composing the next step.
              </p>
            )}
          </div>
        </footer>
      </div>
    </OnboardingDMContextProvider>
  );
}
