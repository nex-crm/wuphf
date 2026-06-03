/**
 * useRuntimeVerify — drives the live "Verify" loop for a single runtime in
 * the guided provider picker (docs/specs/office-onboarding-uplift.md section
 * "B. Verified, guided provider picker").
 *
 * The hook owns one verify lifecycle:
 *   idle    -> nothing checked yet
 *   running -> POST /onboarding/verify is in flight
 *   done    -> the backend returned a classified VerifyResult (pass /
 *              not_installed / auth_required / other_error)
 *   error   -> the request itself failed (broker unreachable, timeout)
 *
 * It is intentionally per-runtime: PrePickScreen instantiates one hook for the
 * runtime the user has expanded. The backend probe forks subprocesses behind a
 * 10s deadline, so a single in-flight verify is aborted before a re-run to
 * avoid stacking probes.
 */

import { useCallback, useEffect, useRef, useState } from "react";

import type { VerifyResult } from "../../api/onboarding";
import { verifyRuntime } from "../../api/onboarding";

export type RuntimeVerifyPhase = "idle" | "running" | "done" | "error";

export interface RuntimeVerifyState {
  /** Current lifecycle phase. */
  phase: RuntimeVerifyPhase;
  /** The classified result once phase === "done". */
  result: VerifyResult | undefined;
  /** A user-facing message when phase === "error". */
  errorMessage: string | undefined;
  /** Kick off (or re-run) a verify for `runtime`. */
  verify: (runtime: string) => void;
  /** Clear the result back to idle (e.g. when the user collapses the panel). */
  reset: () => void;
}

export function useRuntimeVerify(): RuntimeVerifyState {
  const [phase, setPhase] = useState<RuntimeVerifyPhase>("idle");
  const [result, setResult] = useState<VerifyResult | undefined>(undefined);
  const [errorMessage, setErrorMessage] = useState<string | undefined>(
    undefined,
  );

  // Track the in-flight controller so a re-run aborts the previous probe and a
  // late response from an abandoned runtime cannot overwrite a newer one.
  const controllerRef = useRef<AbortController | null>(null);

  const verify = useCallback((runtime: string) => {
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;

    setPhase("running");
    setErrorMessage(undefined);

    verifyRuntime(runtime, controller.signal)
      .then((next) => {
        if (controller.signal.aborted) return;
        setResult(next);
        setPhase("done");
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        const message =
          err instanceof Error
            ? err.message
            : "Could not reach the runtime check";
        setErrorMessage(message);
        setPhase("error");
      });
  }, []);

  const reset = useCallback(() => {
    controllerRef.current?.abort();
    controllerRef.current = null;
    setPhase("idle");
    setResult(undefined);
    setErrorMessage(undefined);
  }, []);

  // Abort any in-flight probe on unmount so a resolved promise does not call
  // setState on an unmounted component.
  useEffect(() => {
    return () => {
      controllerRef.current?.abort();
    };
  }, []);

  return { phase, result, errorMessage, verify, reset };
}
