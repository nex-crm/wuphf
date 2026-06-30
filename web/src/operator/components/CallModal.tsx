// "Demo workflow to Nex" — the teach-by-demonstration call (mock).
//
// In the real product (operator spec S5/S6) this is a screen-share + free-voice
// session: the operator demonstrates their process while Nex watches the screen
// and asks questions, then drafts (or edits) a deterministic tool. The eventual
// mechanism is a computer-use agent (CUA) over the captured screen plus OpenAI
// Realtime for the voice (BYOK or wuphf-hosted, see Settings). Here it is a
// presentational mock so the shape of the hero moment can be seen and reacted
// to. Nothing is captured yet.
//
// Two modes: BUILD (no tool given) demos a brand-new tool; MODIFY (a tool given)
// demos a change to an existing one. Build is the default.

import { useEffect, useRef, useState } from "react";
import { ArrowRight, PhoneOff, SkipForward } from "lucide-react";

interface CallLine {
  who: "you" | "ai";
  text: string;
}

// BUILD: demonstrate a brand-new workflow from scratch.
const BUILD_SCRIPT: CallLine[] = [
  {
    who: "ai",
    text: "Walk me through how you handle a new demo request. I'm watching your screen.",
  },
  {
    who: "you",
    text: "So a request comes into this form. First I check the company in HubSpot...",
  },
  {
    who: "ai",
    text: "What makes one worth sending to an AE versus nurturing?",
  },
  {
    who: "you",
    text: "Size and industry, mostly. 200+ people in our core verticals, and they named a use case.",
  },
  { who: "ai", text: "And where does a hot one go from there?" },
  {
    who: "you",
    text: "I post it in #ae-handoffs in Slack with the reason, and it gets assigned.",
  },
  {
    who: "ai",
    text: "Got it. I've drafted a tool: enrich from HubSpot, score fit 0 to 100, route 70 and up to an AE, nurture the rest. Want to see it?",
  },
];

// MODIFY: demonstrate a change to a tool that already exists.
function modifyScript(toolName: string): CallLine[] {
  return [
    {
      who: "ai",
      text: `Show me what you want to change about ${toolName}. I'm watching your screen.`,
    },
    {
      who: "you",
      text: "When a lead scores below 40, I don't want to nurture them anymore. Just archive them.",
    },
    {
      who: "ai",
      text: "So under 40 becomes archive instead of nurture. Anything else move?",
    },
    { who: "you", text: "No, just that one branch." },
    {
      who: "ai",
      text: `Got it. I've drafted the change to ${toolName}: scores under 40 route to archive, not the nurture sequence. Want to see it?`,
    },
  ];
}

// How long each scripted line lingers before the next one reveals.
const REVEAL_MS = 1400;

interface CallModalProps {
  onClose: () => void;
  onBuild: () => void;
  // When set, the call demonstrates a CHANGE to this existing tool (modify
  // mode). When omitted, it demonstrates a brand-new tool (build mode).
  tool?: { name: string };
}

export function CallModal({ onClose, onBuild, tool }: CallModalProps) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const isModify = Boolean(tool);
  const SCRIPT = isModify
    ? modifyScript(tool?.name ?? "this tool")
    : BUILD_SCRIPT;
  const dialogLabel = isModify
    ? `Demo a change to ${tool?.name}`
    : "Demo your workflow to Nex";
  const screenLabel = isModify
    ? `operator screen: ${tool?.name}`
    : "operator screen: inbound demo requests";
  const ctaLabel = isModify ? "See the change" : "See the drafted tool";

  // a11y: close on Escape, focus the dialog on open, restore focus on close,
  // and keep Tab focus inside the dialog (a minimal focus trap).
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    dialogRef.current?.focus();
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        onClose();
        return;
      }
      if (e.key !== "Tab") return;
      const focusables = dialogRef.current?.querySelectorAll<HTMLElement>(
        'button, [href], input, [tabindex]:not([tabindex="-1"])',
      );
      if (!focusables || focusables.length === 0) return;
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      prev?.focus();
    };
  }, [onClose]);

  // Reveal the scripted exchange progressively so the call feels alive: start
  // on the first line and advance on a timer until the whole script is shown.
  const [revealed, setRevealed] = useState(1);
  useEffect(() => {
    const timer = setInterval(() => {
      setRevealed((r) => (r >= SCRIPT.length ? r : r + 1));
    }, REVEAL_MS);
    return () => clearInterval(timer);
  }, []);
  const lines = SCRIPT.slice(0, revealed);
  const last = lines[lines.length - 1];
  const done = last?.who === "ai" && revealed === SCRIPT.length;

  return (
    <div
      className="opr-modal-scrim"
      role="dialog"
      aria-modal="true"
      aria-label={dialogLabel}
      onClick={onClose}
    >
      <div
        className="opr-call"
        ref={dialogRef}
        tabIndex={-1}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="opr-call-stage">
          <div className="opr-call-rec">
            <span className="opr-led" />
            rec · screen share
          </div>
          <div className="opr-call-screenshare">{screenLabel}</div>
          <div className="opr-call-wave" aria-hidden={true}>
            ▁▂▃▅▇▅▃▂▁ ▁▂▃▅▇▅▃▂▁ ▁▂▃▅▇▅▃▂▁
          </div>
          <div className="opr-call-caption">
            <b>{last?.who === "ai" ? "your ai" : "you"}</b> {last?.text}
          </div>
        </div>

        <div className="opr-call-body">
          <div className="opr-eyebrow">Live call</div>
          <div className="opr-call-transcript">
            {lines.map((l, i) => (
              <div className="opr-call-line" key={i}>
                <b>{l.who === "ai" ? "Your AI" : "You"}</b>
                {l.text}
              </div>
            ))}
          </div>

          <div
            className="opr-detail-actions"
            style={{ justifyContent: "flex-end" }}
          >
            {!done ? (
              <button
                type="button"
                className="opr-btn opr-btn-sm"
                onClick={() => setRevealed(SCRIPT.length)}
              >
                <SkipForward size={13} strokeWidth={1.9} aria-hidden={true} />
                Skip ahead
              </button>
            ) : null}
            <button type="button" className="opr-btn" onClick={onClose}>
              <PhoneOff size={14} strokeWidth={1.9} aria-hidden={true} />
              End call
            </button>
            <button
              type="button"
              className="opr-btn opr-btn-primary"
              onClick={onBuild}
              disabled={!done}
            >
              {ctaLabel}
              <ArrowRight size={14} strokeWidth={1.9} aria-hidden={true} />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
