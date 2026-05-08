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
import { stripV } from "../layout/upgradeBanner.utils";

type RunPhase = "idle" | "running" | "done";

interface RunState {
  phase: RunPhase;
  result?: UpgradeRunResult;
}

type StatusKind = "ok" | "outdated" | "dev" | "error" | "loading" | "unknown";

interface Status {
  kind: StatusKind;
  label: string;
}

interface VersionModalProps {
  open: boolean;
  onClose: () => void;
}

function deriveStatus(
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
  if (check?.install_command && check.install_method !== "unknown") {
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
    staleTime: 60_000,
  });

  const [run, setRun] = useState<RunState>({ phase: "idle" });
  const [restarting, setRestarting] = useState(false);

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
  useEffect(() => {
    if (!open) {
      setRun({ phase: "idle" });
      setRestarting(false);
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
    setRun({ phase: "running" });
    try {
      const result = await runUpgrade();
      setRun({ phase: "done", result });
      // Re-fetch /upgrade-check so the chip's freshness indicator updates
      // once the modal closes.
      void refetch();
    } catch (e: unknown) {
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
    setRestarting(true);
    try {
      await restartBroker();
      // The broker exits and respawns; the SSE client reconnects on its own.
      // Close the modal optimistically so the user is back in the app while
      // the listener comes back up.
      onClose();
    } catch {
      setRestarting(false);
    }
  }, [restarting, onClose]);

  if (!open) return null;

  const current = check?.current ? stripV(check.current) : null;
  const latest = check?.latest ? stripV(check.latest) : null;
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
            current={current}
            latest={latest}
            status={status}
            isFetching={isFetching}
            compareUrl={check?.compare_url}
          />

          <ActionsSection
            installCommand={installCommand}
            latestRaw={check?.latest}
            running={run.phase === "running"}
            restarting={restarting}
            onForceUpdate={() => {
              void triggerRun();
            }}
            onRestart={() => {
              void triggerRestart();
            }}
          />

          {run.phase === "done" && run.result ? (
            <VersionRunOutcome
              result={run.result}
              latest={latest ?? ""}
              onRestart={() => {
                void triggerRestart();
              }}
              restarting={restarting}
            />
          ) : null}
        </div>
      </div>
    </div>
  );
}

function BuildSection({
  current,
  latest,
  status,
  isFetching,
  compareUrl,
}: {
  current: string | null;
  latest: string | null;
  status: Status;
  isFetching: boolean;
  compareUrl: string | undefined;
}) {
  const latestLabel = latest ? `v${latest}` : isFetching ? "…" : "unknown";
  return (
    <section className="version-modal-section">
      <h3 className="help-section-title">Build</h3>
      <dl className="version-modal-grid">
        <dt>Current</dt>
        <dd>
          <code className="version-modal-version">
            {current ? `v${current}` : "unknown"}
          </code>
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
  latestRaw,
  running,
  restarting,
  onForceUpdate,
  onRestart,
}: {
  installCommand: string;
  latestRaw: string | undefined;
  running: boolean;
  restarting: boolean;
  onForceUpdate: () => void;
  onRestart: () => void;
}) {
  return (
    <section className="version-modal-section">
      <h3 className="help-section-title">Actions</h3>
      <p className="version-modal-help">
        Force-update reinstalls{" "}
        <code>{stripV(latestRaw ?? "wuphf@latest")}</code> via your package
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
  latest: string;
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
  latest,
  onRestart,
  restarting,
}: {
  result: UpgradeRunResult;
  latest: string;
  onRestart: () => void;
  restarting: boolean;
}) {
  return (
    <section className="version-modal-section version-modal-outcome version-modal-outcome--ok">
      <h3 className="help-section-title">Install complete</h3>
      <p className="version-modal-help">
        Installed{latest ? <> v{latest}</> : null}. Restart the broker to pick
        up the new binary.
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
