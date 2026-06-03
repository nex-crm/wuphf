/**
 * RuntimeGuidePanel — the guided setup + live verify loop that expands beneath
 * a runtime card in PrePickScreen (docs/specs/office-onboarding-uplift.md
 * section "B. Verified, guided provider picker").
 *
 * Two backend surfaces feed this panel:
 *   GET  /onboarding/install-steps?runtime=<name>  — cheap static guided setup
 *   POST /onboarding/verify {runtime}              — live classified check
 *
 * The steps render the moment the panel opens (no probe paid). Verify is the
 * expensive path: the user presses it, the backend forks subprocess probes,
 * and the classified result (pass / auth_required / not_installed) renders
 * with a real command to copy, a hint, and a highlight on the failed step.
 */

import { useCallback, useEffect, useState } from "react";

import type { InstallStep, VerifyResult } from "../../api/onboarding";
import { fetchInstallSteps } from "../../api/onboarding";
import { InlineCommand } from "./InlineCommand";
import { useRuntimeVerify } from "./useRuntimeVerify";

interface RuntimeGuidePanelProps {
  /** Backend runtime name (the prereq binary, e.g. "claude"). */
  runtime: string;
  /** Display label for the runtime (e.g. "Claude Code"). */
  label: string;
  /**
   * Called when a verify pass classifies the runtime as ready. Lets the
   * parent advance the primary button copy to "Next" without re-probing.
   */
  onVerified?: (runtime: string, result: VerifyResult) => void;
}

const STATUS_COPY: Record<
  VerifyResult["status"],
  { tone: "pass" | "auth" | "neutral"; lead: string }
> = {
  pass: { tone: "pass", lead: "Ready to go." },
  auth_required: { tone: "auth", lead: "Almost there. One sign-in to go." },
  not_installed: { tone: "neutral", lead: "Not installed yet." },
  other_error: { tone: "neutral", lead: "Could not confirm this runtime." },
};

export function RuntimeGuidePanel({
  runtime,
  label,
  onVerified,
}: RuntimeGuidePanelProps) {
  const [steps, setSteps] = useState<InstallStep[]>([]);
  const [stepsLoaded, setStepsLoaded] = useState(false);
  const { phase, result, errorMessage, verify } = useRuntimeVerify();

  // Pull the static guided setup once when the panel mounts for this runtime.
  useEffect(() => {
    let cancelled = false;
    setStepsLoaded(false);
    fetchInstallSteps(runtime)
      .then((data) => {
        if (cancelled) return;
        setSteps(data.steps ?? []);
      })
      .catch(() => {
        // Steps unreachable: render the panel with the verify button only.
        if (!cancelled) setSteps([]);
      })
      .finally(() => {
        if (!cancelled) setStepsLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, [runtime]);

  // Hand a successful classification back to the parent so it can flip the
  // primary CTA to "Next" without paying for another probe.
  useEffect(() => {
    if (phase === "done" && result?.status === "pass") {
      onVerified?.(runtime, result);
    }
  }, [phase, result, runtime, onVerified]);

  const handleVerify = useCallback(() => verify(runtime), [verify, runtime]);

  const failedStep = result?.failed_step ?? "";
  const verifying = phase === "running";

  return (
    <section
      className="pre-pick-guide"
      data-testid={`pre-pick-guide-${runtime}`}
      aria-label={`Set up ${label}`}
    >
      <p className="pre-pick-guide-heading">Set up {label}</p>

      {stepsLoaded && steps.length > 0 ? (
        <ol className="pre-pick-guide-steps">
          {steps.map((step, index) => (
            <GuideStep
              key={step.title}
              step={step}
              index={index}
              runtime={runtime}
              isFailed={failedStep !== "" && step.title === failedStep}
            />
          ))}
        </ol>
      ) : null}

      <div className="pre-pick-verify-row">
        <button
          type="button"
          className="btn btn-primary pre-pick-verify-button"
          data-testid={`pre-pick-verify-${runtime}`}
          disabled={verifying}
          onClick={handleVerify}
        >
          {verifying
            ? "Checking…"
            : phase === "done" || phase === "error"
              ? "Verify again"
              : "Verify"}
        </button>
      </div>

      {phase === "done" && result ? (
        <RuntimeVerifyResult runtime={runtime} result={result} />
      ) : null}

      {phase === "error" ? (
        <div
          role="alert"
          className="pre-pick-verify-result neutral"
          data-testid={`pre-pick-verify-result-${runtime}`}
        >
          <p className="pre-pick-verify-result-lead">
            The check did not run. {errorMessage}
          </p>
          <p className="pre-pick-verify-result-hint">
            Make sure the office is running, then verify again.
          </p>
        </div>
      ) : null}
    </section>
  );
}

interface GuideStepProps {
  step: InstallStep;
  index: number;
  runtime: string;
  isFailed: boolean;
}

function GuideStep({ step, index, runtime, isFailed }: GuideStepProps) {
  return (
    <li
      className={`pre-pick-guide-step${isFailed ? " failed" : ""}`}
      data-testid={`pre-pick-guide-step-${runtime}-${index}`}
      data-failed={isFailed ? "true" : "false"}
    >
      <span className="pre-pick-guide-step-num" aria-hidden="true">
        {index + 1}
      </span>
      <div className="pre-pick-guide-step-body">
        <span className="pre-pick-guide-step-title">{step.title}</span>
        {step.detail ? (
          <span className="pre-pick-guide-step-detail">{step.detail}</span>
        ) : null}
        {step.command ? (
          <InlineCommand
            command={step.command}
            copyLabel={`Copy command for ${step.title}`}
            data-testid={`pre-pick-guide-command-${runtime}-${index}`}
          />
        ) : null}
        {step.link_url ? (
          <a
            className="pre-pick-guide-step-link"
            href={step.link_url}
            target="_blank"
            rel="noopener noreferrer"
          >
            {step.link_label || "Open guide"} &rarr;
          </a>
        ) : null}
      </div>
    </li>
  );
}

interface RuntimeVerifyResultProps {
  runtime: string;
  result: VerifyResult;
}

function RuntimeVerifyResult({ runtime, result }: RuntimeVerifyResultProps) {
  const copy = STATUS_COPY[result.status] ?? STATUS_COPY.other_error;
  const isPass = result.status === "pass";
  return (
    <div
      className={`pre-pick-verify-result ${copy.tone}`}
      data-testid={`pre-pick-verify-result-${runtime}`}
      data-status={result.status}
      role={isPass ? "status" : "alert"}
    >
      <p className="pre-pick-verify-result-lead">
        <span className="pre-pick-verify-glyph" aria-hidden="true">
          {isPass ? "✓" : "!"}
        </span>
        {copy.lead}
        {result.version ? (
          <span className="pre-pick-verify-version"> · {result.version}</span>
        ) : null}
      </p>
      {result.hint ? (
        <p className="pre-pick-verify-result-hint">{result.hint}</p>
      ) : null}
      {result.command ? (
        <InlineCommand
          command={result.command}
          copyLabel={`Copy the next command for ${runtime}`}
          data-testid={`pre-pick-verify-command-${runtime}`}
        />
      ) : null}
    </div>
  );
}
