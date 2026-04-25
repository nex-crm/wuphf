import { useCallback, useEffect } from "react";

const AUTO_DISMISS_MS = 2000;

interface SplashScreenProps {
  onDone: () => void;
}

export function SplashScreen({ onDone }: SplashScreenProps) {
  const dismiss = useCallback(() => {
    onDone();
  }, [onDone]);

  useEffect(() => {
    const timer = setTimeout(dismiss, AUTO_DISMISS_MS);
    return () => clearTimeout(timer);
  }, [dismiss]);

  return (
    <div
      className="launch-screen"
      onClick={dismiss}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") dismiss();
      }}
      aria-label="Dismiss splash screen"
    >
      <div className="launch-logo">WUPHF</div>
      <div className="launch-spinner" />
      <p className="launch-text">Opening the office&hellip;</p>
      <p className="launch-sub">Preparing a live operating loop</p>
    </div>
  );
}
