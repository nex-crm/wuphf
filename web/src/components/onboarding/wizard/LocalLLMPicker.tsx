import { useEffect, useState } from "react";

import {
  getLocalProvidersStatus,
  type LocalProviderStatus,
} from "../../../api/client";
import { LOCAL_PROVIDER_LABELS } from "./constants";

// LocalLLMPicker is the second-step grid of mlx-lm / ollama / exo tiles
// revealed when the "Run a local model" tile in the primary runtime
// grid is on, so it reads as a peer of the cloud CLIs rather than a
// tucked-away "advanced" toggle.

interface LocalLLMPickerProps {
  // selected: kind that the user picked here, if any. Stored in wizard
  // state so the parent can flip the active llm_provider on Continue
  // without committing to /config until the user explicitly says so.
  selected: string;
  onSelect: (kind: string) => void;
}

export function LocalLLMPicker({ selected, onSelect }: LocalLLMPickerProps) {
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
        setStatus(data ?? []);
      })
      .catch((err: unknown) => {
        if (!alive) return;
        // Surfaced inline below the tiles so a 401 / 5xx / missing
        // endpoint isn't silently rendered as "every runtime is
        // platform_supported but not installed". Lets users distinguish
        // "the broker can't reach detection" from "you have nothing
        // installed".
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
    <div
      data-testid="onboarding-local-llm-picker"
      style={{
        marginTop: 12,
        marginLeft: 12,
        paddingLeft: 16,
        borderLeft: "2px solid var(--accent, #b88efb)",
      }}
    >
      <p
        style={{
          fontSize: 12,
          fontWeight: 600,
          color: "var(--text)",
          margin: "0 0 4px 0",
        }}
      >
        Pick a local runtime
      </p>
      <p
        style={{
          fontSize: 12,
          color: "var(--text-secondary)",
          margin: "0 0 10px 0",
        }}
      >
        No cloud key required. Best on macOS or Linux. Need install commands?
        See <strong>Settings → Local LLMs</strong> after onboarding for the
        doctor panel with copy-paste shell snippets.
      </p>

      {loading ? (
        <div
          style={{
            fontSize: 12,
            color: "var(--text-tertiary)",
            padding: "4px 0",
          }}
        >
          Detecting local runtimes…
        </div>
      ) : null}
      {!loading && fetchError && (
        <div
          data-testid="onboarding-local-llm-fetch-error"
          style={{
            fontSize: 12,
            color: "var(--danger-500, #c33)",
            padding: "6px 8px",
            background: "var(--danger-50, #fee)",
            border: "1px solid var(--danger-200, #fcc)",
            borderRadius: 4,
            marginBottom: 8,
          }}
        >
          Couldn't reach the broker to check installed runtimes:{" "}
          <code style={{ fontFamily: "var(--font-mono)" }}>{fetchError}</code>.
          You can still pick a runtime — install/start commands live in{" "}
          <strong>Settings → Local LLMs</strong> after onboarding.
        </div>
      )}
      {!loading && (
        <div className="runtime-grid">
          {/* biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor. */}
          {LOCAL_PROVIDER_LABELS.map((meta) => {
            const s = byKind.get(meta.kind);
            // When `s` is undefined the status probe didn't return a
            // record for this kind — either the broker is unreachable
            // (fetchError set) or the response was malformed. In that
            // "unknown" state we fall OPEN so the banner copy ("you
            // can still pick a runtime — install commands live in
            // Settings") matches what the UI actually does. The
            // agent-turn surface and the Settings doctor card will
            // catch a real install gap later with a clear error;
            // here we just don't trap the user.
            //
            // When `s` IS defined we trust it: a tile is selectable
            // only when the platform supports the runtime AND the
            // binary is on PATH. Selecting an un-installed runtime
            // would land the user in a shell where every agent turn
            // fails connection-refused, so we route them to Settings
            // instead of letting them commit to a broken default.
            const statusKnown = s !== undefined;
            const installed = statusKnown ? Boolean(s.binary_installed) : true;
            const running = Boolean(s?.reachable);
            const supported = statusKnown ? s.platform_supported : true;
            const selectable = supported && installed;
            const isSelected = selected === meta.kind;
            const classes = [
              "runtime-tile",
              isSelected ? "selected" : "",
              selectable ? "" : "disabled",
            ]
              .filter(Boolean)
              .join(" ");
            const statusText = !statusKnown
              ? "Status unknown — verify in Settings"
              : !supported
                ? "Not supported on this OS"
                : running
                  ? "Running"
                  : installed
                    ? "Installed (server not started)"
                    : "Not installed — install via Settings";
            return (
              <button
                key={meta.kind}
                type="button"
                className={classes}
                onClick={() => {
                  if (!selectable) return;
                  onSelect(isSelected ? "" : meta.kind);
                }}
                disabled={!selectable}
                aria-pressed={isSelected}
                data-testid={`onboarding-local-llm-tile-${meta.kind}`}
              >
                <div className="runtime-tile-head">
                  <span
                    className={`runtime-tile-status ${running ? "installed" : ""}`}
                    aria-hidden="true"
                  />
                  {meta.label}
                </div>
                <div className="runtime-tile-meta">
                  {meta.blurb} · {statusText}
                </div>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
