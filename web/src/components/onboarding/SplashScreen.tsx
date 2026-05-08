import { useCallback, useEffect, useId, useRef, useState } from "react";

const AUTO_DISMISS_MS = 3000;
const FADE_OUT_MS = 500;

interface SplashScreenProps {
  onDone: () => void;
}

export function SplashScreen({ onDone }: SplashScreenProps) {
  const [dismissing, setDismissing] = useState(false);
  const dismissedRef = useRef(false);
  const doneTimerRef = useRef<number | null>(null);
  // useId guards against id collisions if SplashScreen is ever rendered twice
  // (e.g., a transient HMR remount holding a stale instance).
  const clipId = useId();

  const dismiss = useCallback(() => {
    if (dismissedRef.current) return;
    dismissedRef.current = true;
    setDismissing(true);
    doneTimerRef.current = window.setTimeout(onDone, FADE_OUT_MS);
  }, [onDone]);

  useEffect(() => {
    const timer = window.setTimeout(dismiss, AUTO_DISMISS_MS);
    return () => {
      window.clearTimeout(timer);
      if (doneTimerRef.current !== null) {
        window.clearTimeout(doneTimerRef.current);
      }
    };
  }, [dismiss]);

  return (
    <button
      type="button"
      className={`launch-screen${dismissing ? " launch-screen-out" : ""}`}
      onClick={dismiss}
      aria-label="Dismiss splash screen"
    >
      <div className="launch-spinner" />
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          gap: 16,
        }}
      >
        <div className="launch-logo">
          <svg
            width="56"
            height="56"
            viewBox="0 0 80 80"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            style={{ borderRadius: 14, flexShrink: 0 }}
            aria-hidden="true"
          >
            <g clipPath={`url(#${clipId})`}>
              <path d="M80 0H0V80H80V0Z" fill="#FFB3E6" />
              <path d="M25 15H15V65H25V15Z" fill="#612A92" />
              <path d="M65 15H55V65H65V15Z" fill="#612A92" />
              <path d="M45 35H35V55H45V35Z" fill="#612A92" />
              <path d="M35 55H25V65H35V55Z" fill="#612A92" />
              <path d="M55 55H45V65H55V55Z" fill="#612A92" />
            </g>
            <defs>
              <clipPath id={clipId}>
                <rect width="80" height="80" rx="20" fill="white" />
              </clipPath>
            </defs>
          </svg>
          WUPHF
        </div>
        <p className="launch-text">Opening the office&hellip;</p>
      </div>
      <p className="launch-sub">Preparing a live operating loop</p>
    </button>
  );
}
