import { useEffect, useState } from "react";

import type { LocalProviderStatus } from "../../api/client";
import { getLocalProvidersStatus } from "../../api/client";
import { LOCAL_PROVIDER_LABELS } from "./runtimeConstants";

interface LocalProviderPickerProps {
  /** Currently selected provider kinds. */
  selected: readonly string[];
  onToggle: (kind: string) => void;
}

/**
 * Multi-select grid of local OpenAI-compatible runtime tiles.
 *
 * Ported from the deleted wizard/LocalLLMPicker.tsx. Probes
 * /status/local-providers on mount to show install status per tile.
 * Falls open (all tiles selectable) when the probe fails so users are
 * never hard-blocked — Settings doctor shows the real gap post-onboarding.
 */
export function LocalProviderPicker({
  selected,
  onToggle,
}: LocalProviderPickerProps) {
  const [status, setStatus] = useState<LocalProviderStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string>("");

  useEffect(() => {
    let alive = true;
    setLoading(true);
    setFetchError("");
    getLocalProvidersStatus()
      .then((data) => {
        if (!alive) return;
        setStatus(Array.isArray(data) ? data : []);
      })
      .catch((err: unknown) => {
        if (!alive) return;
        const msg = err instanceof Error ? err.message : String(err);
        setFetchError(msg);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, []);

  const byKind = new Map<string, LocalProviderStatus>();
  for (const s of status) byKind.set(s.kind, s);

  return (
    <div className="pre-pick-local-picker" data-testid="pre-pick-local-picker">
      {loading ? (
        <div className="pre-pick-local-detecting">
          Detecting local runtimes…
        </div>
      ) : null}
      {!loading && fetchError ? (
        <div
          className="pre-pick-local-error"
          data-testid="pre-pick-local-fetch-error"
        >
          Could not reach the broker to check installed runtimes:{" "}
          <code>{fetchError}</code>. You can still pick providers — install
          commands are in <strong>Settings &rarr; Local LLMs</strong> after
          onboarding.
        </div>
      ) : null}
      {!loading ? (
        <div className="runtime-grid runtime-grid-local">
          {/* biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Ported from wizard/LocalLLMPicker.tsx which baselined this complexity for a focused follow-up refactor. */}
          {LOCAL_PROVIDER_LABELS.map((meta) => {
            const s = byKind.get(meta.kind);
            // When status is unknown (broker unreachable) fall OPEN so the
            // user is not blocked. When status IS known, require
            // platform_supported and binary_installed for a non-disabled tile.
            const statusKnown = s !== undefined;
            const installed = statusKnown ? Boolean(s.binary_installed) : true;
            const running = Boolean(s?.reachable);
            const supported = statusKnown ? s.platform_supported : true;
            const selectable = supported && installed;
            const isSelected = selected.includes(meta.kind);

            const classes = [
              "runtime-tile",
              isSelected ? "selected" : "",
              selectable ? "" : "disabled",
            ]
              .filter(Boolean)
              .join(" ");

            const statusText = !statusKnown
              ? "Status unknown"
              : !supported
                ? "Not supported on this OS"
                : running
                  ? "Running"
                  : installed
                    ? "Installed (not started)"
                    : "Not installed";

            return (
              <button
                key={meta.kind}
                type="button"
                className={classes}
                onClick={() => {
                  if (!selectable) return;
                  onToggle(meta.kind);
                }}
                disabled={!selectable}
                aria-pressed={isSelected}
                data-testid={`pre-pick-local-tile-${meta.kind}`}
              >
                <div className="runtime-tile-head">
                  <span
                    className={`runtime-tile-status${running ? " installed" : ""}`}
                    aria-hidden="true"
                  />
                  {meta.label}
                </div>
                <div className="runtime-tile-meta">
                  {meta.blurb} &middot; {statusText}
                </div>
              </button>
            );
          })}
        </div>
      ) : null}
      <p className="pre-pick-local-hint">
        No cloud key required. Best on macOS or Linux.
      </p>
    </div>
  );
}
