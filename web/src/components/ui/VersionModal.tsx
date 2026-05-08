// biome-ignore-all lint/a11y/useKeyWithClickEvents: Backdrop click-to-dismiss matches HelpModal — keyboard path is the document-level Escape handler.
import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { restartBroker } from "../../api/client";
import {
  getUpgradeCheck,
  runUpgrade,
  type UpgradeCheckResponse,
  type UpgradeRunResult,
} from "../../api/upgrade";
import { useAppStore } from "../../stores/app";
import { formatVersion } from "../layout/upgradeBanner.utils";

type RunPhase = "idle" | "running" | "done";

interface RunState {
  phase: RunPhase;
  result?: UpgradeRunResult;
}

export type StatusKind =
  | "ok"
  | "outdated"
  | "dev"
  | "error"
  | "loading"
  | "unknown";

export interface Status {
  kind: StatusKind;
  label: string;
}

interface VersionModalProps {
  open: boolean;
  onClose: () => void;
}

// deriveStatus is exported so the StatusBar chip and the modal classify the
// upgrade-check response identically. A divergence here would let the chip
// show "unknown" while the modal said "check failed" for the same payload.
export function deriveStatus(
  check: UpgradeCheckResponse | undefined,
  isFetching: boolean,
): Status {
  if (check?.error) return { kind: "error", label: "check failed" };
  if (!check) {
    return isFetching
      ? { kind: "loading", label: "checking…" }
      : { kind: "unknown", label: "unknown" };
  }
  if (check.is_dev_build) return { kind: "dev", label: "dev build" };
  if (check.upgrade_available) {
    return { kind: "outdated", label: "update available" };
  }
  return { kind: "ok", label: "up to date" };
}

function deriveInstallCommand(check: UpgradeCheckResponse | undefined): string {
  // Match UpgradeBanner's guard exactly — install_method must be present AND
  // not "unknown" before we trust install_command. Older brokers omit
  // install_method, in which case we fall back to upgrade_command (or the
  // canonical npm command) so a malformed broker response can't spoof the
  // chip's command.
  if (
    check?.install_command &&
    check.install_method &&
    check.install_method !== "unknown"
  ) {
    return check.install_command;
  }
  return check?.upgrade_command ?? "npm install -g wuphf@latest";
}

// Modal opened by clicking the version chip in the StatusBar. Mirrors the
// HelpModal frame — overlay + card with a header + body — and reuses the
// same /upgrade-check + /upgrade/run + /broker/restart endpoints the
// banner uses, but always exposes the action buttons (force update + restart)
// regardless of whether an upgrade is "needed", so a user can re-install the
// current version or kick the broker without an update being pending.
export function VersionModal({ open, onClose }: VersionModalProps) {
  const closeRef = useRef<HTMLButtonElement>(null);

  const {
    data: check,
    isFetching,
    refetch,
  } = useQuery<UpgradeCheckResponse>({
    queryKey: ["upgrade-check"],
    queryFn: () => getUpgradeCheck(),
    enabled: open,
    // Match the StatusBar's poll cadence so opening the modal within the
    // 5-min window reuses the cached value instead of firing a duplicate
    // request. (The broker caches /upgrade-check for an hour, so the
    // duplicate would be cheap, but a single source of truth keeps the
    // chip and modal in lockstep.)
    staleTime: 5 * 60_000,
  });

  const [run, setRun] = useState<RunState>({ phase: "idle" });
  const [restarting, setRestarting] = useState(false);
  const [restartError, setRestartError] = useState<string | null>(null);

  // runUpgrade() resolves on its own schedule — if the user closes the modal
  // mid-install, the resolve still fires. Without an epoch we'd store its
  // outcome and surface a stale "Install complete" + Restart prompt the next
  // time the user opens the modal. Each Force-update click bumps the epoch;
  // the resolve handler bails when its captured epoch no longer matches.
  const runEpochRef = useRef(0);

  // Esc closes — claim the event in the capture phase so we beat any
  // underlying modal or the global shortcut handler. Same pattern as
  // HelpModal.
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopImmediatePropagation();
        onClose();
      }
    }
    document.addEventListener("keydown", onKey, true);
    return () => document.removeEventListener("keydown", onKey, true);
  }, [open, onClose]);

  // Restore prior focus on close — same pattern as HelpModal so SR users
  // land back where they were.
  useEffect(() => {
    if (!open) return;
    const prevFocus = document.activeElement as HTMLElement | null;
    const id = window.requestAnimationFrame(() => closeRef.current?.focus());
    return () => {
      window.cancelAnimationFrame(id);
      if (prevFocus?.isConnected && typeof prevFocus.focus === "function") {
        prevFocus.focus();
      }
    };
  }, [open]);

  // Reset run state whenever the modal closes so a re-open starts clean.
  // Bumping the epoch invalidates any in-flight runUpgrade() so its resolve
  // can't write back into state.
  useEffect(() => {
    if (!open) {
      runEpochRef.current += 1;
      setRun({ phase: "idle" });
      setRestarting(false);
      setRestartError(null);
    }
  }, [open]);

  const handleOverlayClick = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === e.currentTarget) onClose();
    },
    [onClose],
  );

  const triggerRun = useCallback(async () => {
    if (run.phase === "running") return;
    const epoch = ++runEpochRef.current;
    setRun({ phase: "running" });
    try {
      const result = await runUpgrade();
      if (runEpochRef.current !== epoch) return;
      setRun({ phase: "done", result });
      // Re-fetch /upgrade-check so the chip's freshness indicator updates
      // once the modal closes.
      void refetch();
    } catch (e: unknown) {
      if (runEpochRef.current !== epoch) return;
      setRun({
        phase: "done",
        result: {
          ok: false,
          command: check?.install_command ?? check?.upgrade_command,
          error: e instanceof Error ? e.message : String(e),
        },
      });
    }
  }, [run.phase, refetch, check?.install_command, check?.upgrade_command]);

  const triggerRestart = useCallback(async () => {
    if (restarting) return;
    setRestartError(null);
    setRestarting(true);
    try {
      await restartBroker();
      // The broker exits and respawns; the SSE client reconnects on its own.
      // Close the modal optimistically so the user is back in the app while
      // the listener comes back up. (The 202 returns sub-second; if the call
      // throws we keep the modal open so the inline error has somewhere to
      // render.)
      onClose();
    } catch (e: unknown) {
      setRestarting(false);
      setRestartError(e instanceof Error ? e.message : String(e));
    }
  }, [restarting, onClose]);

  const onForceUpdateClick = useCallback(() => {
    void triggerRun();
  }, [triggerRun]);
  const onRestartClick = useCallback(() => {
    void triggerRestart();
  }, [triggerRestart]);

  if (!open) return null;

  const currentLabel = formatVersion(check?.current);
  const latestLabel = formatVersion(
    check?.latest,
    isFetching ? "…" : "unknown",
  );
  const status = deriveStatus(check, isFetching);
  const installCommand = deriveInstallCommand(check);

  return (
    <div
      className="help-overlay"
      role="dialog"
      aria-modal="true"
      aria-label="wuphf version"
      onClick={handleOverlayClick}
    >
      <div className="help-modal version-modal card">
        <header className="help-header">
          <div>
            <h2 className="help-title">wuphf version</h2>
            <p className="help-subtitle">
              See what's running, force a reinstall, or restart the broker.
            </p>
          </div>
          <button
            ref={closeRef}
            type="button"
            className="help-close"
            onClick={onClose}
            aria-label="Close"
          >
            Esc
          </button>
        </header>

        <div className="help-body version-modal-body">
          <BuildSection
            currentLabel={currentLabel}
            latestLabel={latestLabel}
            status={status}
            compareUrl={check?.compare_url}
          />

          <ActionsSection
            installCommand={installCommand}
            latestForCopy={formatVersion(check?.latest, "wuphf@latest")}
            running={run.phase === "running"}
            restarting={restarting}
            onForceUpdate={onForceUpdateClick}
            onRestart={onRestartClick}
          />

          {restartError ? (
            <p className="version-modal-error" role="alert">
              Couldn't restart broker: {restartError}
            </p>
          ) : null}

          {run.phase === "done" && run.result ? (
            <VersionRunOutcome
              result={run.result}
              latestLabel={latestLabel}
              onRestart={onRestartClick}
              restarting={restarting}
            />
          ) : null}
        </div>
      </div>
    </div>
  );
}

function BuildSection({
  currentLabel,
  latestLabel,
  status,
  compareUrl,
}: {
  currentLabel: string;
  latestLabel: string;
  status: Status;
  compareUrl: string | undefined;
}) {
  return (
    <section className="version-modal-section">
      <h3 className="help-section-title">Build</h3>
      <dl className="version-modal-grid">
        <dt>Current</dt>
        <dd>
          <code className="version-modal-version">{currentLabel}</code>
          <span
            className={`version-modal-dot version-modal-dot--${status.kind}`}
            aria-hidden={true}
          />
          <span className="version-modal-status">{status.label}</span>
        </dd>
        <dt>Latest</dt>
        <dd>
          <code className="version-modal-version">{latestLabel}</code>
          {compareUrl ? (
            <a
              className="version-modal-link"
              href={compareUrl}
              target="_blank"
              rel="noopener noreferrer"
            >
              Compare on GitHub
            </a>
          ) : null}
        </dd>
      </dl>
    </section>
  );
}

function ActionsSection({
  installCommand,
  latestForCopy,
  running,
  restarting,
  onForceUpdate,
  onRestart,
}: {
  installCommand: string;
  latestForCopy: string;
  running: boolean;
  restarting: boolean;
  onForceUpdate: () => void;
  onRestart: () => void;
}) {
  return (
    <section className="version-modal-section">
      <h3 className="help-section-title">Actions</h3>
      <p className="version-modal-help">
        Force-update reinstalls <code>{latestForCopy}</code> via your package
        manager. Restart asks the broker to exit so a new binary comes up —
        usually only needed after an install.
      </p>
      <div className="version-modal-actions">
        <button
          type="button"
          className="version-modal-btn version-modal-btn--primary"
          onClick={onForceUpdate}
          disabled={running}
          aria-busy={running}
          title={`Runs ${installCommand}`}
        >
          {running ? "Installing…" : "Force update"}
        </button>
        <button
          type="button"
          className="version-modal-btn"
          onClick={onRestart}
          disabled={restarting || running}
          aria-busy={restarting}
          title="Exits the broker so it respawns on the next launch"
        >
          {restarting ? "Restarting…" : "Restart broker"}
        </button>
      </div>
      <code className="version-modal-cmd">{installCommand}</code>
    </section>
  );
}

function OutputToggle({ output }: { output: string | undefined }) {
  const [showOutput, setShowOutput] = useState(false);
  if (!output) return null;
  return (
    <>
      <button
        type="button"
        className="version-modal-link"
        onClick={() => setShowOutput((s) => !s)}
      >
        {showOutput ? "Hide install output" : "Show install output"}
      </button>
      {showOutput ? <pre className="version-modal-output">{output}</pre> : null}
    </>
  );
}

function VersionRunOutcome(props: {
  result: UpgradeRunResult;
  latestLabel: string;
  onRestart: () => void;
  restarting: boolean;
}) {
  return props.result.ok ? (
    <RunOutcomeOk {...props} />
  ) : (
    <RunOutcomeErr result={props.result} />
  );
}

function RunOutcomeOk({
  result,
  latestLabel,
  onRestart,
  restarting,
}: {
  result: UpgradeRunResult;
  latestLabel: string;
  onRestart: () => void;
  restarting: boolean;
}) {
  // latestLabel is already prefixed (`v0.83.10`, `dev`, or fallback like
  // `unknown`) so we drop the prefix-detection branch that used to live
  // here.
  const installedSuffix =
    latestLabel && latestLabel !== "unknown" ? ` ${latestLabel}` : "";
  return (
    <section className="version-modal-section version-modal-outcome version-modal-outcome--ok">
      <h3 className="help-section-title">Install complete</h3>
      <p className="version-modal-help">
        Installed{installedSuffix}. Restart the broker to pick up the new
        binary.
      </p>
      <div className="version-modal-actions">
        <button
          type="button"
          className="version-modal-btn version-modal-btn--primary"
          onClick={onRestart}
          disabled={restarting}
          aria-busy={restarting}
        >
          {restarting ? "Restarting…" : "Restart now"}
        </button>
      </div>
      <OutputToggle output={result.output} />
    </section>
  );
}

function errorHeadline(result: UpgradeRunResult): string {
  if (result.timed_out) return "Install timed out.";
  if (result.install_method === "unknown") {
    return "Couldn't detect how wuphf was installed — run the command below from a terminal:";
  }
  return "Install failed. You can retry, or run the command below from a terminal:";
}

function RunOutcomeErr({ result }: { result: UpgradeRunResult }) {
  return (
    <section className="version-modal-section version-modal-outcome version-modal-outcome--err">
      <h3 className="help-section-title">Install failed</h3>
      <p className="version-modal-help">{errorHeadline(result)}</p>
      {result.error ? (
        <p className="version-modal-error">{result.error}</p>
      ) : null}
      {result.command ? (
        <code className="version-modal-cmd">{result.command}</code>
      ) : null}
      <OutputToggle output={result.output} />
    </section>
  );
}

export function VersionModalHost() {
  const open = useAppStore((s) => s.versionModalOpen);
  const setOpen = useAppStore((s) => s.setVersionModalOpen);
  return <VersionModal open={open} onClose={() => setOpen(false)} />;
}
