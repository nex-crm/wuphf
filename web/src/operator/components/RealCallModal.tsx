// RealCallModal — the REAL "Demo workflow to Nex" call: live screen share +
// realtime voice (OpenAI Realtime over WebRTC). Same props and same handoff as
// the mock CallModal, so OperatorApp can swap them based on whether a Realtime
// key is configured. See realtimeClient.ts and docs/specs/operator-demo-call-real.md.

import { useEffect, useRef, useState } from "react";
import { ArrowRight, PhoneOff } from "lucide-react";

import {
  captureCounts,
  type DemoCapture,
  type DemoCaptureLine,
  demoCaptureFromDraft,
} from "../apps/demoCapture";
import {
  type RealtimeController,
  type RealtimeStatus,
  startRealtimeCall,
} from "../apps/realtimeClient";

interface RealCallModalProps {
  onClose: () => void;
  onBuild: (capture: DemoCapture) => void;
  tool?: { id: string; name: string };
}

const PHASE_LABEL: Record<RealtimeStatus["phase"], string> = {
  "requesting-screen": "Pick a screen to share",
  connecting: "Connecting to Nex",
  live: "Live · Nex is watching and listening",
  drafted: "Nex drafted it",
  ended: "Call ended",
  error: "Something went wrong",
};

export function RealCallModal({ onClose, onBuild, tool }: RealCallModalProps) {
  const isModify = Boolean(tool);
  const audioRef = useRef<HTMLAudioElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const transcriptRef = useRef<DemoCaptureLine[]>([]);
  // The two call avatars glow with live audio. We drive them via a CSS variable
  // straight from the meter callback so the squares animate at 60fps without
  // re-rendering the modal on every frame.
  const youAvatarRef = useRef<HTMLDivElement>(null);
  const aiAvatarRef = useRef<HTMLDivElement>(null);

  const [status, setStatus] = useState<RealtimeStatus>({
    phase: "requesting-screen",
  });
  // Committed transcript lines plus the in-progress AI line (cumulative text).
  const [lines, setLines] = useState<DemoCaptureLine[]>([]);
  const [liveAi, setLiveAi] = useState("");
  const [draft, setDraft] = useState<DemoCapture | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let controller: RealtimeController | null = null;
    let cancelled = false;

    const audioEl = audioRef.current;
    const videoEl = videoRef.current;
    if (!audioEl) return;

    startRealtimeCall({
      mode: isModify ? "modify" : "build",
      tool,
      audioEl,
      videoEl: videoEl ?? undefined,
      onStatus: (s) => !cancelled && setStatus(s),
      onTranscript: (line) => {
        if (cancelled) return;
        if (line.who === "ai" && !line.final) {
          setLiveAi(line.text);
          return;
        }
        const committed: DemoCaptureLine = { who: line.who, text: line.text };
        transcriptRef.current = [...transcriptRef.current, committed];
        setLines((prev) => [...prev, committed]);
        if (line.who === "ai") setLiveAi("");
      },
      onDraft: (args) => {
        if (cancelled) return;
        setDraft(
          demoCaptureFromDraft(args, {
            mode: isModify ? "modify" : "build",
            tool,
            transcript: transcriptRef.current,
          }),
        );
      },
      onLevels: (you, ai) => {
        youAvatarRef.current?.style.setProperty("--lvl", you.toFixed(3));
        aiAvatarRef.current?.style.setProperty("--lvl", ai.toFixed(3));
      },
      onError: (message) => !cancelled && setError(message),
    })
      .then((c) => {
        if (cancelled) {
          c.stop();
          return;
        }
        controller = c;
        if (videoRef.current) videoRef.current.srcObject = c.screenStream;
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err));
          setStatus({ phase: "error" });
        }
      });

    return () => {
      cancelled = true;
      controller?.stop();
    };
    // Start exactly once on mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const counts = draft ? captureCounts(draft) : null;
  const ctaLabel = isModify ? "Make the change" : "Build it with Nex";
  const dialogLabel = isModify
    ? `Demo a change to ${tool?.name}`
    : "Demo your workflow to Nex";

  return (
    <div
      className="opr-modal-scrim"
      role="dialog"
      aria-modal="true"
      aria-label={dialogLabel}
      onClick={onClose}
    >
      <div className="opr-call" onClick={(e) => e.stopPropagation()}>
        <div className="opr-call-stage">
          <div className="opr-call-rec">
            <span className="opr-led" />
            rec · screen share
          </div>
          {/* The live screen the operator is sharing. */}
          <video
            ref={videoRef}
            className="opr-call-video"
            autoPlay={true}
            muted={true}
            playsInline={true}
          />
          {/* Call avatars — each square glows with its speaker's live audio. */}
          <div className="opr-call-avatars">
            <div ref={youAvatarRef} className="opr-call-avatar">
              <span className="opr-call-avatar-face">You</span>
            </div>
            <div ref={aiAvatarRef} className="opr-call-avatar is-ai">
              <span className="opr-call-avatar-face">Nex</span>
            </div>
          </div>
          <div className="opr-call-caption">
            <b>{status.phase === "live" ? "live" : "nex"}</b>{" "}
            {liveAi || PHASE_LABEL[status.phase]}
          </div>
        </div>

        <div className="opr-call-body">
          <div className="opr-eyebrow">{PHASE_LABEL[status.phase]}</div>

          {error ? (
            <div className="opr-call-error">{error}</div>
          ) : (
            <div className="opr-call-transcript">
              {lines.map((l, i) => (
                <div className="opr-call-line" key={i}>
                  <b>{l.who === "ai" ? "Nex" : "You"}</b>
                  {l.text}
                </div>
              ))}
              {liveAi ? (
                <div className="opr-call-line">
                  <b>Nex</b>
                  {liveAi}
                </div>
              ) : null}
            </div>
          )}

          {draft && counts ? (
            <div className="opr-call-capture">
              <div className="opr-call-capture-head">
                Captured from your screen · {counts.screens} screens ·{" "}
                {counts.selectors} elements · {counts.apiCalls} API calls ·{" "}
                {counts.entities} entities
              </div>
              <div className="opr-call-capture-chips">
                {draft.apiCalls.map((c) => (
                  <span
                    className="opr-call-capture-chip"
                    key={`${c.integration}-${c.endpoint}`}
                  >
                    {c.integration} {c.endpoint}
                  </span>
                ))}
                {draft.entities.map((e) => (
                  <span
                    className="opr-call-capture-chip is-entity"
                    key={`${e.kind}-${e.value}`}
                  >
                    {e.value}
                  </span>
                ))}
              </div>
              <div className="opr-call-capture-note">
                {isModify
                  ? "Nex will rework the workflow from this."
                  : "Nex will build the workflow from this."}
              </div>
            </div>
          ) : null}

          <div
            className="opr-detail-actions"
            style={{ justifyContent: "flex-end" }}
          >
            <button type="button" className="opr-btn" onClick={onClose}>
              <PhoneOff size={14} strokeWidth={1.9} aria-hidden={true} />
              End call
            </button>
            <button
              type="button"
              className="opr-btn opr-btn-primary"
              onClick={() => draft && onBuild(draft)}
              disabled={!draft}
            >
              {ctaLabel}
              <ArrowRight size={14} strokeWidth={1.9} aria-hidden={true} />
            </button>
          </div>
        </div>
      </div>
      {/* Model voice. */}
      <audio ref={audioRef} autoPlay={true}>
        <track kind="captions" />
      </audio>
    </div>
  );
}
