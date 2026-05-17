import { useEffect, useMemo, useState } from "react";

import { get, post } from "../../api/client";
import { RUNTIMES } from "./wizard/constants";
import { RuntimeLogo } from "./wizard/RuntimeLogos";
import type { PrereqResult, RuntimeSpec } from "./wizard/types";

// Phase 1 of the onboarding-into-office redesign
// (docs/specs/onboarding-into-office.md). The 9-step wizard collapses
// into one pre-office screen — runtime selection. Everything else
// (company name, blueprint pick, team trim, first task) moves into a
// CEO DM inside the real Shell in Phase 2 and later.
//
// The user picks a detected runtime OR opts into sandbox mode. Either
// path posts /onboarding/complete with skip_task:true so the broker
// runs its existing scratch-path setup; deeper changes to the broker
// (seedMinimalScratchLocked, deterministic phase machine) are Phase 2.
//
// `onComplete` is invoked after both the /config and /onboarding/complete
// requests succeed. RootRoute then flips onboardingComplete and routes the
// user to the Shell.

interface PrePickScreenProps {
  onComplete: () => void;
}

interface RuntimeCardState {
  spec: RuntimeSpec;
  detected: PrereqResult | undefined;
  available: boolean;
}

interface RuntimeCardProps {
  state: RuntimeCardState;
  prereqsLoaded: boolean;
  isSubmitting: boolean;
  anySubmitting: boolean;
  onPick: (spec: RuntimeSpec) => void;
  onInstall: (url: string) => void;
}

function cardStatusLabel({
  prereqsLoaded,
  available,
  version,
}: {
  prereqsLoaded: boolean;
  available: boolean;
  version: string | undefined;
}): string {
  if (!prereqsLoaded) return "Checking…";
  if (!available) return "Not installed";
  return version ? `Detected · ${version}` : "Detected";
}

function RuntimeCard({
  state,
  prereqsLoaded,
  isSubmitting,
  anySubmitting,
  onPick,
  onInstall,
}: RuntimeCardProps) {
  const { spec, detected, available } = state;
  const statusLabel = isSubmitting
    ? "Starting your office…"
    : cardStatusLabel({
        prereqsLoaded,
        available,
        version: detected?.version,
      });
  const installHint =
    !available && prereqsLoaded ? (
      <div className="pre-pick-card-install">Install →</div>
    ) : null;
  return (
    <button
      key={spec.label}
      type="button"
      className={`pre-pick-card${available ? " available" : " missing"}`}
      data-testid={`pre-pick-card-${spec.provider}`}
      disabled={anySubmitting}
      onClick={() => {
        if (!available) {
          onInstall(spec.installUrl);
          return;
        }
        onPick(spec);
      }}
    >
      <div className="pre-pick-card-head">
        <RuntimeLogo label={spec.label} />
        <span className="pre-pick-card-name">{spec.label}</span>
      </div>
      <div className="pre-pick-card-status">{statusLabel}</div>
      {installHint}
    </button>
  );
}

type PrereqsPayload = { prereqs?: PrereqResult[] } | PrereqResult[];

function prereqsFromPayload(data: PrereqsPayload): PrereqResult[] {
  if (Array.isArray(data)) return data;
  return data.prereqs ?? [];
}

// Phase 1 ships the three runtimes the broker can dispatch to: Claude
// Code, Codex, Opencode. Cursor and Windsurf (provider:null in RUNTIMES)
// have no dispatch path so we omit them here — they would land the user
// in sandbox mode anyway.
const DISPATCHABLE_RUNTIMES = RUNTIMES.filter((r) => r.provider !== null);

export function PrePickScreen({ onComplete }: PrePickScreenProps) {
  const [prereqs, setPrereqs] = useState<PrereqResult[]>([]);
  const [prereqsLoaded, setPrereqsLoaded] = useState(false);
  const [submitting, setSubmitting] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string>("");

  useEffect(() => {
    let cancelled = false;
    get<PrereqsPayload>("/onboarding/prereqs")
      .then((data) => {
        if (cancelled) return;
        setPrereqs(prereqsFromPayload(data));
      })
      .catch(() => {
        // Prereqs unreachable — render runtime cards as "Install" so the
        // user can still proceed. The wizard takes the same posture.
      })
      .finally(() => {
        if (!cancelled) setPrereqsLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const runtimes = useMemo<RuntimeCardState[]>(
    () =>
      DISPATCHABLE_RUNTIMES.map((spec) => {
        const detected = prereqs.find((p) => p.name === spec.binary);
        return {
          spec,
          detected,
          available: Boolean(detected?.found),
        };
      }),
    [prereqs],
  );

  async function commitChoice(
    provider: RuntimeSpec["provider"],
    runtimeLabel: string,
  ): Promise<void> {
    setSubmitError("");
    setSubmitting(runtimeLabel);
    try {
      // /config persists runtime selection. Skip when the user opts into
      // sandbox mode (provider === null) — they'll wire one up later via
      // Settings → Runtimes and the broker treats missing llm_provider as
      // "no dispatch capability."
      if (provider) {
        await post("/config", {
          memory_backend: "markdown",
          llm_provider: provider,
          llm_provider_priority: [provider],
        });
      }
      // Phase 1 sends only the minimum the existing /onboarding/complete
      // handler needs. Company name, blueprint, team trim, and first task
      // all move into the in-office CEO conversation (Phase 2+).
      await post("/onboarding/complete", {
        company: "",
        description: "",
        priority: "",
        website: "",
        owner_name: "",
        owner_role: "",
        scan_completed: false,
        runtime: provider ?? "",
        runtime_priority: provider ? [provider] : [],
        memory_backend: "markdown",
        blueprint: "",
        agents: [],
        api_keys: {},
        task: "",
        skip_task: true,
      });
      onComplete();
    } catch (err: unknown) {
      const msg =
        err instanceof Error ? err.message : "Failed to start the office";
      setSubmitError(msg);
      setSubmitting(null);
    }
  }

  function openInstallPage(url: string): void {
    if (typeof window === "undefined") return;
    window.open(url, "_blank", "noopener,noreferrer");
  }

  return (
    <div className="pre-pick-screen" data-testid="pre-pick-screen">
      <div className="pre-pick-body">
        <div className="pre-pick-hero">
          <div className="pre-pick-eyebrow">WUPHF</div>
          <h1 className="pre-pick-headline">Pick a runtime.</h1>
          <p className="pre-pick-subhead">
            Your office needs an AI runtime. Pick one of the three below, or
            skip to look around first.
          </p>
        </div>

        <div className="pre-pick-card-grid">
          {runtimes.map((state) => (
            <RuntimeCard
              key={state.spec.label}
              state={state}
              prereqsLoaded={prereqsLoaded}
              isSubmitting={submitting === state.spec.label}
              anySubmitting={Boolean(submitting)}
              onPick={(spec) => void commitChoice(spec.provider, spec.label)}
              onInstall={openInstallPage}
            />
          ))}
        </div>

        <div className="pre-pick-secondary-row">
          <button
            type="button"
            className="pre-pick-secondary-button"
            data-testid="pre-pick-skip"
            disabled={Boolean(submitting)}
            onClick={() => void commitChoice(null, "skip")}
          >
            {submitting === "skip"
              ? "Opening your office…"
              : "I’ll add one later  →"}
          </button>
        </div>

        <p className="pre-pick-helper">
          You can change this any time in <strong>Settings → Runtimes</strong>.
        </p>

        {submitError ? (
          <div role="alert" className="pre-pick-error">
            {submitError}
          </div>
        ) : null}
      </div>
    </div>
  );
}
