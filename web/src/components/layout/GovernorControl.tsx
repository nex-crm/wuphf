import { useCallback } from "react";

import type { GovernorStatus } from "../../api/governor";
import { useGovernor, useGovernorAction } from "../../hooks/useGovernor";
import { meterSummary } from "./governorFormat";

interface GovernorControlViewProps {
  status: GovernorStatus;
  busy: boolean;
  onPause: () => void;
  onStop: () => void;
}

/**
 * Presentational running meter. Split out so Storybook/tests can render it with
 * a fixed status.
 */
export function GovernorControlView({
  status,
  busy,
  onPause,
  onStop,
}: GovernorControlViewProps) {
  return (
    <div className="governor-control" title="Session run control">
      <span className="governor-control-meter">{meterSummary(status)}</span>
      <button
        className="governor-control-btn"
        onClick={onPause}
        disabled={busy}
        type="button"
        title="Pause after the current turn"
      >
        Pause
      </button>
      <button
        className="governor-control-btn governor-control-stop"
        onClick={onStop}
        disabled={busy}
        type="button"
        title="Stop now and cancel in-flight work"
      >
        Stop
      </button>
    </div>
  );
}

/**
 * GovernorControl is the always-available interrupt: a compact live meter
 * (turns · tokens · cost since the last checkpoint) plus Pause and Stop. Lives
 * in the StatusBar. Hidden while paused — the GovernorBanner owns that state —
 * and when auto-pausing is disabled, where the meter would be noise.
 */
export function GovernorControl() {
  const { data: status } = useGovernor();
  const { mutate, isPending } = useGovernorAction();

  const onPause = useCallback(() => mutate({ action: "pause" }), [mutate]);
  const onStop = useCallback(() => mutate({ action: "stop" }), [mutate]);

  if (!status || status.paused) return null;

  return (
    <GovernorControlView
      status={status}
      busy={isPending}
      onPause={onPause}
      onStop={onStop}
    />
  );
}
