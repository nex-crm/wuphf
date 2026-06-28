// The "magical call" — primary activation surface (mock).
//
// In the real product this is a screen-share + free-voice session where the
// operator narrates their process while the AI watches and asks questions,
// then drafts a deterministic tool. Here it is a presentational mock so the
// shape of the hero moment can be seen and reacted to. Nothing is captured.

import { useEffect, useRef, useState } from "react";
import { ArrowRight, PhoneOff, SkipForward } from "lucide-react";

interface CallLine {
  who: "you" | "ai";
  text: string;
}

const SCRIPT: CallLine[] = [
  { who: "ai", text: "Walk me through how you handle a new demo request. I'm watching your screen." },
  { who: "you", text: "So a request comes into this form. First I check the company in HubSpot..." },
  { who: "ai", text: "What makes one worth sending to an AE versus nurturing?" },
  { who: "you", text: "Size and industry, mostly. 200+ people in our core verticals, and they named a use case." },
  { who: "ai", text: "And where does a hot one go from there?" },
  { who: "you", text: "I post it in #ae-handoffs in Slack with the reason, and it gets assigned." },
  { who: "ai", text: "Got it. I've drafted a tool: enrich from HubSpot, score fit 0 to 100, route 70 and up to an AE, nurture the rest. Want to see it?" },
];

interface CallModalProps {
  onClose: () => void;
  onBuild: () => void;
}

export function CallModal({ onClose, onBuild }: CallModalProps) {
  const dialogRef = useRef<HTMLDivElement>(null);

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

  // Reveal the scripted exchange progressively so the call feels alive.
  const [revealed, setRevealed] = useState(SCRIPT.length);
  const lines = SCRIPT.slice(0, revealed);
  const last = lines[lines.length - 1];
  const done = last?.who === "ai" && revealed === SCRIPT.length;

  return (
    <div
      className="opr-modal-scrim"
      role="dialog"
      aria-modal="true"
      aria-label="Build a tool on a call"
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
          <div className="opr-call-screenshare">
            operator screen: inbound demo requests
          </div>
          <div className="opr-call-wave" aria-hidden>
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

          <div className="opr-detail-actions" style={{ justifyContent: "flex-end" }}>
            {!done ? (
              <button
                type="button"
                className="opr-btn opr-btn-sm"
                onClick={() => setRevealed(SCRIPT.length)}
              >
                <SkipForward size={13} strokeWidth={1.9} aria-hidden />
                Skip ahead
              </button>
            ) : null}
            <button type="button" className="opr-btn" onClick={onClose}>
              <PhoneOff size={14} strokeWidth={1.9} aria-hidden />
              End call
            </button>
            <button
              type="button"
              className="opr-btn opr-btn-primary"
              onClick={onBuild}
              disabled={!done}
            >
              See the drafted tool
              <ArrowRight size={14} strokeWidth={1.9} aria-hidden />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
