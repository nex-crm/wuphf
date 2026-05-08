import { useCallback, useEffect, useRef, useState } from "react";

import type { ConfigSnapshot } from "../../api/client";
import { get, getConfig, post } from "../../api/client";
import { router } from "../../lib/router";
import { useAppStore } from "../../stores/app";
import "../../styles/onboarding.css";

// ApiKeyRow is re-exported here for back-compat with onboarding/ApiKeyRow.test.tsx.
export { ApiKeyRow } from "./wizard/ApiKeyRow";

import { ProgressDots } from "./wizard/components";
import {
  LOCAL_PROVIDER_LABELS,
  RUNTIMES,
  SCRATCH_FOUNDING_TEAM,
  STEP_ORDER,
} from "./wizard/constants";
import { OutcomeSummary } from "./wizard/OutcomeSummary";
import {
  clearDraft,
  consumeStaleBannerDays,
  loadDraft,
  seedFromDraft,
} from "./wizard/onboardingDraft";
import type { PackPreviewRequirement } from "./wizard/packPreview";
import { adaptPackPreview } from "./wizard/packPreview";
import { OnboardingBanners } from "./wizard/ResumeBanner";
import {
  canSetupContinue,
  detectedBinary,
  localProviderKindFromRuntimePriority,
  runtimeIsReady,
  runtimeLabelsFromProviderConfig,
} from "./wizard/runtime-helpers";
import { FirstTaskScreen } from "./wizard/FirstTaskScreen";
import { WelcomeStep } from "./wizard/Step1Welcome";
import { TemplatesStep } from "./wizard/Step2Templates";
import { IdentityStep } from "./wizard/Step3Identity";
import { TeamStep } from "./wizard/Step4Team";
import { SetupStep } from "./wizard/Step5Setup";
import { NexStep } from "./wizard/Step6Nex";
import { TaskStep } from "./wizard/Step6Task";
import { ReadyStep } from "./wizard/Step7Ready";
import type {
  BlueprintAgent,
  BlueprintTemplate,
  NexSignupStatus,
  PrereqResult,
  ReadinessCheck,
  TaskTemplate,
  WizardStep,
} from "./wizard/types";
import { useOnboardingDraftSync } from "./wizard/useOnboardingDraftSync";

// OutcomeMeta captures the wizard state at the moment /onboarding/complete
// succeeds. The backend only returns {"ok":true}; this snapshot is the
// source of truth for the post-completion summary screen.
interface OutcomeMeta {
  agents: BlueprintAgent[];
  selectedBlueprint: string | null;
  blueprints: BlueprintTemplate[];
  primaryRuntime: string;
  taskText: string;
  taskSkipped: boolean;
  apiKeyCount: number;
}

// runtimeIsReady + detectedBinary moved to wizard/runtime-helpers.ts.
// ArrowIcon, CheckIcon, EnterHint, ProgressDots moved to wizard/components.tsx.
// Step components extracted to wizard/Step1Welcome.tsx through Step7Ready.tsx.
/* ═══════════════════════════════════════════
   Main Wizard
   ═══════════════════════════════════════════ */

interface WizardProps {
  onComplete?: () => void;
}

type PrereqsPayload = { prereqs?: PrereqResult[] } | PrereqResult[];
type PrereqsSettled = PromiseSettledResult<PrereqsPayload>;
type ConfigSettled = PromiseSettledResult<ConfigSnapshot>;

interface PrereqsBootstrap {
  list: PrereqResult[];
  error: string;
}

interface ConfigBootstrap {
  runtimePriority: string[];
  localProvider: string | null;
}

function prereqsFromPayload(data: PrereqsPayload): PrereqResult[] {
  return Array.isArray(data) ? data : (data.prereqs ?? []);
}

// Derive pack requirements from the selected blueprint for the SetupStep
// requirements panel. Returns an empty array when no blueprint is selected
// (scratch) or when the blueprint declares no requirements, so the panel
// stays hidden in those cases.
function derivePackRequirements(
  selectedBlueprint: string | null,
  blueprints: BlueprintTemplate[],
): PackPreviewRequirement[] {
  if (!selectedBlueprint) return [];
  const bp = blueprints.find((b) => b.id === selectedBlueprint);
  if (!bp) return [];
  return adaptPackPreview(bp).requirements;
}

function errorMessage(reason: unknown): string {
  return reason instanceof Error ? reason.message : String(reason);
}

function runtimePriorityFromInstalledPrereqs(list: PrereqResult[]): string[] {
  const firstInstalled = RUNTIMES.find((spec) => {
    if (spec.provider === null) return false;
    const det = list.find((p) => p.name === spec.binary);
    return Boolean(det?.found);
  });
  return firstInstalled ? [firstInstalled.label] : [];
}

function prereqsBootstrapFromResult(result: PrereqsSettled): PrereqsBootstrap {
  if (result.status === "fulfilled") {
    return { list: prereqsFromPayload(result.value), error: "" };
  }
  return { list: [], error: errorMessage(result.reason) };
}

function configBootstrapFromResult(result: ConfigSettled): ConfigBootstrap {
  if (result.status !== "fulfilled") {
    return { runtimePriority: [], localProvider: null };
  }
  const runtimePriority = runtimeLabelsFromProviderConfig(result.value);
  return {
    runtimePriority,
    localProvider: localProviderKindFromRuntimePriority(runtimePriority),
  };
}

function initialRuntimePriority(
  current: string[],
  configured: string[],
  prereqs: PrereqResult[],
): string[] {
  if (current.length > 0) return current;
  if (configured.length > 0) return configured;
  return runtimePriorityFromInstalledPrereqs(prereqs);
}

// biome-ignore lint/complexity/noExcessiveLinesPerFunction: Existing function length is baselined for a focused follow-up refactor.
export function Wizard({ onComplete }: WizardProps) {
  const setOnboardingComplete = useAppStore((s) => s.setOnboardingComplete);

  // When creating a new workspace from an existing one, identity is skipped —
  // the user already exists, they just need blueprint/team/setup/task.
  const skipIdentity =
    new URLSearchParams(window.location.search).get("skip_identity") === "1";
  const activeSteps: readonly WizardStep[] = skipIdentity
    ? STEP_ORDER.filter((s) => s !== "identity")
    : STEP_ORDER;

  // Resume support: load any saved draft once on first render. Captured
  // via a useState lazy initializer so loadDraft (which has side effects
  // like clearing stale drafts and setting the session-storage banner
  // flag) runs exactly once on mount rather than on every render. The
  // stale-banner flag is consumed at the same time so it only ever shows
  // once.
  const [initialDraft] = useState(() => loadDraft());
  const seed = seedFromDraft(initialDraft);
  const [hasSavedDraft, setHasSavedDraft] = useState<boolean>(
    initialDraft !== null,
  );
  const [showResumeBanner, setShowResumeBanner] = useState<boolean>(
    initialDraft !== null,
  );
  const [staleBannerDays, setStaleBannerDays] = useState<number | null>(() =>
    consumeStaleBannerDays(),
  );

  // Navigation
  const [step, setStep] = useState<WizardStep>(seed.step);

  // Step 2: templates
  const [blueprints, setBlueprints] = useState<BlueprintTemplate[]>([]);
  const [blueprintsLoading, setBlueprintsLoading] = useState(true);
  const [selectedBlueprint, setSelectedBlueprint] = useState<string | null>(
    seed.selectedBlueprint,
  );

  // Step 3: identity
  const [company, setCompany] = useState(seed.company);
  const [description, setDescription] = useState(seed.description);
  const [priority, setPriority] = useState(seed.priority);
  // Optional in-wizard Nex registration. Mirrors the TUI's InitNexRegister
  // phase — we POST /nex/register which shells out to `nex-cli setup <email>`.
  // If nex-cli isn't installed we flip to `fallback` (external link to
  // nex.ai/register, key pasted on the Setup step).
  const [nexEmail, setNexEmail] = useState("");
  const [nexSignupStatus, setNexSignupStatus] =
    useState<NexSignupStatus>("open");
  const [nexSignupError, setNexSignupError] = useState("");

  // Step 4: team
  const [agents, setAgents] = useState<BlueprintAgent[]>([]);

  // Step 5: setup
  const [prereqs, setPrereqs] = useState<PrereqResult[]>([]);
  const [prereqsLoading, setPrereqsLoading] = useState(true);
  const [prereqsError, setPrereqsError] = useState("");
  // Ordered list of runtime labels (matches RUNTIMES[].label). Position in
  // the array is the fallback priority. Initially empty — we prefer explicit
  // config/launch choices, then auto-populate with the first installed CLI so
  // the happy path still works with zero clicks.
  const [runtimePriority, setRuntimePriority] = useState<string[]>(
    seed.runtimePriority,
  );
  // localProvider is the local OpenAI-compat kind the user opted into in
  // the SetupStep subsection (mlx-lm | ollama | exo | "" for none).
  // When non-empty, it overrides whatever cloud CLI was selected and is
  // applied as llm_provider on /onboarding/complete.
  const [localProvider, setLocalProvider] = useState<string>(
    seed.localProvider,
  );
  const [apiKeys, setApiKeys] = useState<Record<string, string>>({});
  // If we restored runtime choices from a draft, mark as user-edited so
  // the prereq bootstrap effect doesn't overwrite them with defaults.
  // Only treat the draft as edited when it actually carried a runtime —
  // an empty draft (welcome-step bail) shouldn't suppress auto-detect on
  // the next visit, otherwise the no-clicks happy path silently breaks.
  const userEditedRuntimeRef = useRef(
    initialDraft !== null &&
      (seed.runtimePriority.length > 0 || seed.localProvider !== ""),
  );

  // Step 6: first task
  const [taskTemplates, setTaskTemplates] = useState<TaskTemplate[]>([]);
  const [selectedTaskTemplate, setSelectedTaskTemplate] = useState<
    string | null
  >(seed.selectedTaskTemplate);
  const [taskText, setTaskText] = useState(seed.taskText);
  const taskTextAutofilled = useRef(false);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");
  // After a successful submission with a task, show the first-task launch
  // screen before entering the office so the user can choose to watch live.
  const [showFirstTask, setShowFirstTask] = useState(false);
  const [submittedTaskText, setSubmittedTaskText] = useState("");

  // Outcome summary — shown after /onboarding/complete succeeds.
  // The wizard stays mounted so the user can read what was created before
  // entering the office. onComplete() is called when they click "Go to the office".
  const [showOutcome, setShowOutcome] = useState(false);
  const [outcomeMeta, setOutcomeMeta] = useState<OutcomeMeta | null>(null);

  // Fetch blueprints on mount
  useEffect(() => {
    let cancelled = false;
    setBlueprintsLoading(true);

    get<{ templates?: BlueprintTemplate[] }>("/onboarding/blueprints")
      .then((data) => {
        if (cancelled) return;
        const tpls = data.templates ?? [];
        setBlueprints(tpls);
      })
      .catch(() => {
        // Endpoint may not exist yet; continue with empty list
      })
      .finally(() => {
        if (!cancelled) setBlueprintsLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  // Fetch prereqs + current config on mount so the runtime picker shows which
  // CLIs are installed while still honoring explicit launch/config choices
  // such as `wuphf --provider codex`.
  useEffect(() => {
    let cancelled = false;
    setPrereqsLoading(true);

    Promise.allSettled([
      get<PrereqsPayload>("/onboarding/prereqs"),
      getConfig(),
    ])
      .then(([prereqsResult, configResult]) => {
        if (cancelled) return;
        const prereqState = prereqsBootstrapFromResult(prereqsResult);
        const configState = configBootstrapFromResult(configResult);

        setPrereqs(prereqState.list);
        setPrereqsError(prereqState.error);
        if (!userEditedRuntimeRef.current) {
          if (configState.localProvider) {
            setLocalProvider(configState.localProvider);
          }
          setRuntimePriority((current) =>
            initialRuntimePriority(
              current,
              configState.runtimePriority,
              prereqState.list,
            ),
          );
        }
      })
      .finally(() => {
        if (!cancelled) setPrereqsLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const toggleRuntime = useCallback((label: string) => {
    userEditedRuntimeRef.current = true;
    setRuntimePriority((prev) => {
      if (prev.includes(label)) return prev.filter((l) => l !== label);
      return [...prev, label];
    });
    // Keep `localProvider` in sync with `runtimePriority`: when the
    // user removes a local-runtime row from the fallback chain via
    // the ✕ button, the picker/meta-tile must un-select too.
    // Without this, `localProvider` stays set, `canContinue` stays
    // true, and finishOnboarding() serializes a config without the
    // local runtime — the chain says one thing, the picker shows
    // another, the wizard ships the chain. Single source of truth
    // wins; clear localProvider whenever its label leaves the
    // priority list. (Adding a local label flows through
    // selectLocalProvider, which sets both sides.)
    const localMeta = LOCAL_PROVIDER_LABELS.find((m) => m.label === label);
    if (localMeta) {
      setLocalProvider((cur) => (cur === localMeta.kind ? "" : cur));
    }
  }, []);

  // selectLocalProvider keeps two pieces of state in sync: the picker's
  // own localProvider variable and runtimePriority, the canonical
  // fallback chain. Adding the local kind to the chain is what lets
  // users say "try Claude first; if I'm out of credits, fall through
  // to my local Qwen" without paying for the pay-as-you-go tier
  // implicit in a bare API-key fallback.
  const selectLocalProvider = useCallback((kind: string) => {
    userEditedRuntimeRef.current = true;
    const labels = LOCAL_PROVIDER_LABELS.map((m) => m.label);
    setRuntimePriority((prev) => {
      // Remove any prior local entry first so picking a different
      // local kind replaces rather than stacks them.
      const withoutLocal = prev.filter((l) => !labels.includes(l));
      if (!kind) return withoutLocal;
      const meta = LOCAL_PROVIDER_LABELS.find((m) => m.kind === kind);
      if (!meta) return withoutLocal;
      // Append by default (fallback after cloud CLIs); user can drag
      // it up in the priority controls to make it primary.
      return [...withoutLocal, meta.label];
    });
    setLocalProvider(kind);
  }, []);

  const reorderRuntime = useCallback((label: string, direction: -1 | 1) => {
    userEditedRuntimeRef.current = true;
    setRuntimePriority((prev) => {
      const idx = prev.indexOf(label);
      if (idx < 0) return prev;
      const next = idx + direction;
      if (next < 0 || next >= prev.length) return prev;
      const out = [...prev];
      const [item] = out.splice(idx, 1);
      out.splice(next, 0, item);
      return out;
    });
  }, []);

  // When a blueprint is selected, populate agents AND first tasks from that
  // blueprint only. Previously we flattened tasks across every blueprint, so
  // the task step showed ~26 tiles of unrelated work — including tasks from
  // blueprints the user never picked.
  // Resume guard: on the very first render after restoring a draft, the
  // selectedBlueprint state arrives non-null and would otherwise wipe the
  // restored taskText / selectedTaskTemplate. Skip exactly one run.
  const skipFirstBlueprintEffect = useRef(initialDraft !== null);
  useEffect(() => {
    if (skipFirstBlueprintEffect.current) {
      skipFirstBlueprintEffect.current = false;
      const bp = blueprints.find((b) => b.id === selectedBlueprint);
      if (selectedBlueprint !== null && bp?.tasks) {
        setTaskTemplates(bp.tasks);
      }
      return;
    }
    // Clear suggestion-derived text when the blueprint changes. Track this
    // separately from selectedTaskTemplate because re-clicking a suggestion
    // intentionally deselects the tile while leaving its autofilled text in
    // place for editing.
    if (taskTextAutofilled.current) {
      setTaskText("");
      taskTextAutofilled.current = false;
    }
    setSelectedTaskTemplate(null);

    if (selectedBlueprint === null) {
      // "Start from scratch" — preview the same 5-agent founding team the
      // broker seeds via scratchFoundingTeamBlueprint. Keep the slugs and
      // built_in flag in sync with internal/team/broker_onboarding.go.
      setAgents(SCRATCH_FOUNDING_TEAM.map((a) => ({ ...a })));
      setTaskTemplates([]);
      return;
    }
    const bp = blueprints.find((b) => b.id === selectedBlueprint);
    if (bp?.agents) {
      setAgents(
        bp.agents.map((a) => ({
          ...a,
          checked: a.checked !== false,
        })),
      );
    } else {
      setAgents([]);
    }
    const bpTasks = bp?.tasks;
    setTaskTemplates(Array.isArray(bpTasks) ? bpTasks : []);
  }, [selectedBlueprint, blueprints]);

  // Navigation helpers
  const goTo = useCallback((target: WizardStep) => {
    setStep(target);
  }, []);

  const nextStep = useCallback(() => {
    const idx = activeSteps.indexOf(step);
    if (idx < activeSteps.length - 1) {
      setStep(activeSteps[idx + 1]);
    }
  }, [step, activeSteps]);

  const prevStep = useCallback(() => {
    const idx = activeSteps.indexOf(step);
    if (idx > 0) {
      setStep(activeSteps[idx - 1]);
    }
  }, [step, activeSteps]);

  // Toggle agent selection. The lead agent (built_in) is locked: TeamStep
  // disables its button, and this guard prevents any programmatic path
  // (keyboard, devtools, future bulk toggle) from unchecking it.
  const toggleAgent = useCallback((slug: string) => {
    setAgents((prev) =>
      prev.map((a) => {
        if (a.slug !== slug) return a;
        if (a.built_in === true) return a;
        return { ...a, checked: !a.checked };
      }),
    );
  }, []);

  // API key handler
  const handleApiKeyChange = useCallback((key: string, value: string) => {
    setApiKeys((prev) => ({ ...prev, [key]: value }));
  }, []);

  // Submit the email to /nex/register. On success: mark status 'ok' so the
  // UI tells the user to check their inbox. On ErrNotInstalled (502 from the
  // broker when nex-cli isn't on PATH): flip to 'fallback' — the user gets an
  // external link to nex.ai/register and pastes the key on the Setup step.
  // Any other error: surface the message and let the user retry.
  const submitNexSignup = useCallback(async () => {
    const email = nexEmail.trim();
    if (email.length === 0) return;
    setNexSignupStatus("submitting");
    setNexSignupError("");
    try {
      await post<{ status: string; output?: string }>("/nex/register", {
        email,
      });
      setNexSignupStatus("ok");
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Registration failed";
      // nex.Register returns ErrNotInstalled (broker wraps as 502) when
      // nex-cli isn't on PATH. Detect and flip to the external-link flow.
      if (msg.toLowerCase().includes("not installed") || msg.includes("502")) {
        setNexSignupStatus("fallback");
        return;
      }
      setNexSignupStatus("open");
      setNexSignupError(msg);
    }
  }, [nexEmail]);

  // Compute readiness checks. Runs at render time for the 'ready' step — no
  // useMemo because the surface is small (6 checks) and recomputation only
  // happens when one of these inputs changes. Matches the TUI's six-item
  // list in init_flow.go readinessChecks().
  // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
  const readinessChecks: ReadinessCheck[] = (() => {
    const checks: ReadinessCheck[] = [];

    // 1. Tmux / web session. The web app doesn't need tmux — that's the
    // TUI's office runtime. Surface it as a positive "web session" rather
    // than flagging a missing dependency.
    checks.push({
      label: "Session runtime",
      status: "ready",
      detail: "Web session. No tmux required in the browser.",
    });

    // 3. LLM runtime — whatever CLI the user picked as primary, if
    //    installed (or trusted under prereqsError). Skip provider:null
    //    runtimes (Cursor/Windsurf) under prereqsError because
    //    finishOnboarding drops them from the llm_provider payload —
    //    crediting them as "ready" would tell the user the office is
    //    configured for an LLM that's silently absent on launch.
    // ReadyStep inspects only runtimePriority[0] — the active provider —
    // not the full priority list. The gate sites use .some(...) because
    // any installed entry satisfies the install gate; the readiness
    // summary should reflect what's actually about to be persisted as
    // llm_provider, which is the head of the list.
    //
    // The primary label can resolve to either a cloud-CLI RUNTIMES
    // entry or a local-provider LOCAL_PROVIDER_LABELS entry; the
    // checks below cover both. Pre-fix this block only looked at
    // RUNTIMES, so picking MLX-LM/Ollama/Exo as primary made the
    // summary report a missing LLM right before /config persisted a
    // perfectly valid local provider.
    const [primaryLabel] = runtimePriority;
    const primaryLocal = primaryLabel
      ? LOCAL_PROVIDER_LABELS.find((m) => m.label === primaryLabel)
      : undefined;
    const primarySpec = primaryLabel
      ? RUNTIMES.find((r) => r.label === primaryLabel)
      : undefined;
    const primaryDetection = primarySpec
      ? detectedBinary(prereqs, primarySpec.binary)
      : undefined;
    const primaryReady =
      !!primaryLabel && runtimeIsReady(primaryLabel, prereqs, prereqsError);
    if (primaryLocal) {
      checks.push({
        label: "LLM runtime",
        status: "ready",
        detail: `${primaryLocal.label} selected (${primaryLocal.blurb}).`,
      });
    } else if (primarySpec && primaryReady) {
      checks.push({
        label: "LLM runtime",
        status: "ready",
        detail: primaryDetection?.version
          ? `${primarySpec.label} — ${primaryDetection.version}`
          : prereqsError
            ? `${primarySpec.label} selected (detection skipped)`
            : `${primarySpec.label} installed`,
      });
    } else if (primarySpec) {
      checks.push({
        label: "LLM runtime",
        status: "next",
        detail: `${primarySpec.label} selected but not installed. Install before agents can reason.`,
      });
    } else {
      // No runtime picked — check if any API key is set so the user has a path.
      const hasAnyKey = Object.values(apiKeys).some((v) => v.trim().length > 0);
      checks.push({
        label: "LLM runtime",
        status: hasAnyKey ? "ready" : "missing",
        detail: hasAnyKey
          ? "Provider API key will drive agent runs."
          : "Pick a CLI or add a provider key on the Setup step.",
      });
    }

    // 4. Memory backend — always markdown (built-in, no configuration needed)
    checks.push({
      label: "Memory backend",
      status: "ready",
      detail: "Git-native team wiki in ~/.wuphf/wiki.",
    });

    // 5. Blueprint
    if (selectedBlueprint === null) {
      checks.push({
        label: "Blueprint",
        status: "ready",
        detail: "Start from scratch (5-person founding team).",
      });
    } else {
      const bp = blueprints.find((b) => b.id === selectedBlueprint);
      checks.push({
        label: "Blueprint",
        status: "ready",
        detail: bp?.name ?? selectedBlueprint,
      });
    }

    // 6. Integrations count
    const keyCount = Object.values(apiKeys).filter(
      (v) => v.trim().length > 0,
    ).length;
    checks.push({
      label: "Integrations",
      status: keyCount > 0 ? "ready" : "next",
      detail:
        keyCount > 0
          ? `${keyCount} provider key${keyCount === 1 ? "" : "s"} configured.`
          : "None configured. Add providers later from Settings.",
    });

    return checks;
  })();

  // Complete onboarding
  const finishOnboarding = useCallback(
    // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
    async (skipTask: boolean) => {
      setSubmitting(true);
      setSubmitError("");
      try {
        // Translate UI labels to the provider ids the broker validates.
        // Cloud CLI labels resolve via RUNTIMES; local labels (MLX-LM,
        // Ollama, Exo) resolve via LOCAL_PROVIDER_LABELS so users can
        // mix-and-match in the fallback chain — e.g. "Claude Code,
        // then if I'm out of credits, my local Qwen". Aspirational
        // runtimes (Cursor, Windsurf) map to null and are dropped.
        const providerPriority = runtimePriority
          .map((label) => {
            const cloud = RUNTIMES.find((r) => r.label === label)?.provider;
            if (cloud) return cloud;
            const local = LOCAL_PROVIDER_LABELS.find(
              (m) => m.label === label,
            )?.kind;
            return local ?? null;
          })
          .filter((p): p is string => p !== null && p !== "");

        // Persist memory backend + LLM provider choice + priority fallback
        // list + API keys so the broker reads them on next launch. Send as a
        // single POST — the broker's handleConfig does a non-atomic read-
        // mutate-write, so two parallel calls race and corrupt config.json.
        // Keys go through this path (not /onboarding/complete) because the
        // broker's /config endpoint is the canonical persistence surface
        // for config.APIKey, OpenAIAPIKey, AnthropicAPIKey, etc.
        const configPayload: Record<string, unknown> = {
          memory_backend: "markdown",
        };
        if (providerPriority.length > 0) {
          configPayload.llm_provider = providerPriority[0];
          configPayload.llm_provider_priority = providerPriority;
        }
        // Generic per-provider API keys from the fallback grid.
        const genericAnthropic = (apiKeys.ANTHROPIC_API_KEY ?? "").trim();
        if (genericAnthropic.length > 0) {
          configPayload.anthropic_api_key = genericAnthropic;
        }
        const genericOpenAI = (apiKeys.OPENAI_API_KEY ?? "").trim();
        if (genericOpenAI.length > 0) {
          configPayload.openai_api_key = genericOpenAI;
        }
        const genericGemini = (apiKeys.GOOGLE_API_KEY ?? "").trim();
        if (genericGemini.length > 0) {
          configPayload.gemini_api_key = genericGemini;
        }
        // /config persistence is fatal: we have to know the user's API keys
        // are saved to disk before the first headless turn fires, otherwise
        // agents try to authenticate with empty values and fail with opaque
        // errors. Letting this through silently was the bug PR #365 set out
        // to fix; leaving it as a console.warn would re-introduce it. The
        // outer try/catch hands the error to the submitError + Retry UI.
        await post("/config", configPayload);

        // Primary runtime label for the onboarding payload (best-effort;
        // the broker only acts on {task, skip_task} today, but the extra
        // fields are forward-compatible).
        const primaryRuntime = runtimePriority[0] ?? "";

        await post("/onboarding/complete", {
          company,
          description,
          priority,
          runtime: primaryRuntime,
          runtime_priority: runtimePriority,
          memory_backend: "markdown",
          blueprint: selectedBlueprint,
          agents: agents.filter((a) => a.checked).map((a) => a.slug),
          api_keys: apiKeys,
          task: skipTask ? "" : taskText.trim(),
          skip_task: skipTask,
        });
      } catch (err: unknown) {
        const msg =
          err instanceof Error ? err.message : "Failed to start the office";
        setSubmitError(msg);
        setSubmitting(false);
        return;
      }

      // Capture what was submitted before calling onComplete — the outcome
      // summary screen needs these values to describe what was created.
      const apiKeyCount = Object.values(apiKeys).filter(
        (v) => v.trim().length > 0,
      ).length;
      setOutcomeMeta({
        agents,
        selectedBlueprint,
        blueprints,
        primaryRuntime: runtimePriority[0] ?? "",
        taskText,
        taskSkipped: skipTask,
        apiKeyCount,
      });

      // Onboarding succeeded — discard the resumeable draft so a return
      // visit lands on the post-setup app, not a half-filled wizard.
      clearDraft();
      setOnboardingComplete(true);
      if (!skipTask && taskText.trim()) {
        setSubmittedTaskText(taskText.trim());
        setShowFirstTask(true);
        setSubmitting(false);
      } else {
        // Show the outcome summary screen instead of immediately entering the
        // office. The user clicks "Go to the office" to call onComplete().
        setShowOutcome(true);
        setSubmitting(false);
      }
    },
    [
      company,
      description,
      priority,
      runtimePriority,
      selectedBlueprint,
      agents,
      blueprints,
      apiKeys,
      taskText,
      setOnboardingComplete,
    ],
  );

  // Keyboard: Enter advances each step when the step's own gate allows it,
  // so the whole wizard can be run without reaching for the mouse. Textarea
  // steps (TaskStep) keep Enter for newlines; ⌘/Ctrl+Enter advances there.
  // The NexSignupPanel handles its own Enter inside the email input via an
  // onKeyDown below, so we bail out when that's the focused target.
  useEffect(() => {
    // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        // Escape on the optional Nex step skips it; on other steps it's a
        // no-op (outer keyboard shortcuts can act on app-level panels).
        if (step === "nex") {
          e.preventDefault();
          nextStep();
        }
        return;
      }
      if (e.key !== "Enter") return;
      // Guard against hold-Enter spamming onSubmit before React commits
      // setSubmitting(true). The broker's /config endpoint races on
      // parallel writes, so a repeat-fire on the 'ready' step could
      // corrupt config.json.
      if (e.repeat) return;
      const target = e.target as HTMLElement | null;
      if (target?.id === "wiz-nex-email") return;
      // Don't hijack Enter on interactive controls — Enter on a focused
      // Back button should go back, not advance; Enter on a runtime
      // reorder button should reorder, not advance.
      // Exception: template-card buttons should let Enter advance the step
      // (preventDefault stops the native toggle so the card stays selected).
      const tag = target?.tagName;
      const isTemplateCard =
        target?.classList?.contains("template-card") ?? false;
      if (
        (tag === "BUTTON" || tag === "A" || tag === "SELECT") &&
        !isTemplateCard
      )
        return;
      const inTextarea = tag === "TEXTAREA";
      const isSubmitCombo = e.metaKey || e.ctrlKey;
      if (inTextarea && !isSubmitCombo) return;

      const canIdentityContinue =
        company.trim().length > 0 && description.trim().length > 0;
      const setupCanContinue = canSetupContinue({
        runtimePriority,
        prereqs,
        prereqsError,
        apiKeys,
        localProvider,
      });

      switch (step) {
        case "welcome":
          e.preventDefault();
          goTo(activeSteps[1] ?? "templates");
          return;
        case "templates":
          e.preventDefault();
          nextStep();
          return;
        case "identity":
          if (canIdentityContinue) {
            e.preventDefault();
            nextStep();
          }
          return;
        case "team":
          e.preventDefault();
          nextStep();
          return;
        case "setup":
          if (setupCanContinue) {
            e.preventDefault();
            nextStep();
          }
          return;
        case "nex":
          // Mirror the step's primary CTA: register if the user has typed an
          // email, advance otherwise. This prevents a user from typing an
          // address, tabbing away, then pressing Enter and skipping signup.
          // While a registration is in flight the press is a no-op so a
          // stray Enter can't race the API call and skip the step.
          e.preventDefault();
          if (nexSignupStatus === "submitting") {
            return;
          }
          if (nexSignupStatus === "ok" || nexSignupStatus === "fallback") {
            nextStep();
          } else if (nexEmail.trim().length > 0) {
            submitNexSignup();
          } else {
            nextStep();
          }
          return;
        case "task":
          if (isSubmitCombo) {
            e.preventDefault();
            nextStep();
          }
          return;
        case "ready":
          if (!submitting && taskText.trim().length > 0) {
            e.preventDefault();
            finishOnboarding(false);
          }
          return;
      }
    }
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, [
    step,
    activeSteps,
    company,
    description,
    runtimePriority,
    prereqs,
    prereqsError,
    apiKeys,
    submitting,
    taskText,
    goTo,
    nextStep,
    finishOnboarding,
    localProvider,
    nexEmail,
    nexSignupStatus,
    submitNexSignup,
  ]);

  // Debounced persistence of the non-secret draft. extractDraftableState
  // is a pure mapper inside the hook; secret-bearing fields like
  // apiKeys are never passed in or read.
  useOnboardingDraftSync({
    step,
    selectedBlueprint,
    company,
    description,
    priority,
    runtimePriority,
    localProvider,
    selectedTaskTemplate,
    taskText,
  });

  const resetDraft = useCallback(() => {
    clearDraft();
    setHasSavedDraft(false);
    setShowResumeBanner(false);
    setStep("welcome");
    setSelectedBlueprint(null);
    setCompany("");
    setDescription("");
    setPriority("");
    setNexEmail("");
    setNexSignupStatus("open");
    setNexSignupError("");
    setRuntimePriority([]);
    setLocalProvider("");
    setApiKeys({});
    setSelectedTaskTemplate(null);
    setTaskText("");
    userEditedRuntimeRef.current = false;
    taskTextAutofilled.current = false;
  }, []);

  if (showFirstTask) {
    return (
      <div className="wizard-container">
        <div className="wizard-body">
          <FirstTaskScreen
            taskText={submittedTaskText}
            onWatchTask={async () => {
              try {
                await router.navigate({ to: "/tasks" });
              } finally {
                onComplete?.();
              }
            }}
            onSkipToOffice={() => onComplete?.()}
          />
        </div>
      </div>
    );
  }

  // Outcome summary — rendered in place of the wizard steps after
  // /onboarding/complete succeeds (no-task path). The user reads what
  // was created and then clicks "Go to the office" which calls onComplete().
  if (showOutcome && outcomeMeta !== null) {
    return (
      <div className="wizard-container">
        <div className="wizard-body">
          <OutcomeSummary
            agents={outcomeMeta.agents}
            selectedBlueprint={outcomeMeta.selectedBlueprint}
            blueprints={outcomeMeta.blueprints}
            primaryRuntime={outcomeMeta.primaryRuntime}
            taskText={outcomeMeta.taskText}
            taskSkipped={outcomeMeta.taskSkipped}
            apiKeyCount={outcomeMeta.apiKeyCount}
            onEnter={() => onComplete?.()}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="wizard-container">
      <div className="wizard-body">
        <ProgressDots current={step} steps={activeSteps} />

        <OnboardingBanners
          resumeDraft={showResumeBanner ? initialDraft : null}
          staleBannerDays={staleBannerDays}
          onResetResume={resetDraft}
          onDismissResume={() => setShowResumeBanner(false)}
          onDismissStale={() => setStaleBannerDays(null)}
        />

        {step === "welcome" && (
          <WelcomeStep
            onNext={() => goTo(activeSteps[1] ?? "templates")}
            hasSavedDraft={hasSavedDraft}
            onResetDraft={resetDraft}
          />
        )}

        {step === "templates" && (
          <TemplatesStep
            templates={blueprints}
            loading={blueprintsLoading}
            selected={selectedBlueprint}
            onSelect={setSelectedBlueprint}
            onNext={nextStep}
            onBack={prevStep}
          />
        )}

        {step === "identity" && (
          <IdentityStep
            company={company}
            description={description}
            priority={priority}
            onChangeCompany={setCompany}
            onChangeDescription={setDescription}
            onChangePriority={setPriority}
            onNext={nextStep}
            onBack={prevStep}
          />
        )}

        {step === "team" && (
          <TeamStep
            agents={agents}
            onToggle={toggleAgent}
            onNext={nextStep}
            onBack={prevStep}
          />
        )}

        {step === "setup" && (
          <SetupStep
            prereqStatus={{
              items: prereqs,
              loading: prereqsLoading,
              error: prereqsError,
            }}
            runtimeSelection={{
              priority: runtimePriority,
              onToggle: toggleRuntime,
              onReorder: reorderRuntime,
            }}
            apiKeyState={{
              values: apiKeys,
              onChange: handleApiKeyChange,
            }}
            localLLMState={{
              provider: localProvider,
              onSelectProvider: selectLocalProvider,
            }}
            packRequirements={derivePackRequirements(
              selectedBlueprint,
              blueprints,
            )}
            onNext={nextStep}
            onBack={prevStep}
          />
        )}

        {step === "nex" && (
          <NexStep
            email={nexEmail}
            status={nexSignupStatus}
            error={nexSignupError}
            onChangeEmail={setNexEmail}
            onSubmit={submitNexSignup}
            onNext={nextStep}
            onBack={prevStep}
          />
        )}

        {step === "task" && (
          <TaskStep
            taskTemplates={taskTemplates}
            selectedTaskTemplate={selectedTaskTemplate}
            onSelectTaskTemplate={setSelectedTaskTemplate}
            onApplyTaskTemplate={(id, text) => {
              setSelectedTaskTemplate(id);
              setTaskText(text);
              taskTextAutofilled.current = true;
            }}
            taskText={taskText}
            onChangeTaskText={(text) => {
              setTaskText(text);
              setSelectedTaskTemplate(null);
              taskTextAutofilled.current = false;
            }}
            onNext={nextStep}
            onSkip={() => {
              setTaskText("");
              setSelectedTaskTemplate(null);
              taskTextAutofilled.current = false;
              nextStep();
            }}
            onBack={prevStep}
            submitting={submitting}
          />
        )}

        {step === "ready" && (
          <ReadyStep
            checks={readinessChecks}
            taskText={taskText}
            submitting={submitting}
            submitError={submitError}
            onSkip={() => finishOnboarding(true)}
            onSubmit={() => finishOnboarding(false)}
            onBack={prevStep}
          />
        )}
      </div>
    </div>
  );
}
