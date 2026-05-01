import { useCallback, useEffect, useState } from "react";

import { get, post } from "../../api/client";
import { useAppStore } from "../../stores/app";
import "../../styles/onboarding.css";

// ApiKeyRow is re-exported here for back-compat with onboarding/ApiKeyRow.test.tsx.
export { ApiKeyRow } from "./wizard/ApiKeyRow";

import { ProgressDots } from "./wizard/components";
import {
  LOCAL_PROVIDER_LABELS,
  MEMORY_BACKEND_OPTIONS,
  RUNTIMES,
  SCRATCH_FOUNDING_TEAM,
  STEP_ORDER,
} from "./wizard/constants";
import { detectedBinary, runtimeIsReady } from "./wizard/runtime-helpers";
import { WelcomeStep } from "./wizard/Step1Welcome";
import { TemplatesStep } from "./wizard/Step2Templates";
import { IdentityStep } from "./wizard/Step3Identity";
import { TeamStep } from "./wizard/Step4Team";
import { SetupStep } from "./wizard/Step5Setup";
import { TaskStep } from "./wizard/Step6Task";
import { ReadyStep } from "./wizard/Step7Ready";
import type {
  BlueprintAgent,
  BlueprintTemplate,
  MemoryBackend,
  NexSignupStatus,
  PrereqResult,
  ReadinessCheck,
  ReadinessStatus,
  TaskTemplate,
  WizardStep,
} from "./wizard/types";

// runtimeIsReady + detectedBinary moved to wizard/runtime-helpers.ts.
// ArrowIcon, CheckIcon, EnterHint, ProgressDots moved to wizard/components.tsx.
// Step components extracted to wizard/Step1Welcome.tsx through Step7Ready.tsx.
// runtimeIsReady + detectedBinary moved to wizard/runtime-helpers.ts.
/* ═══════════════════════════════════════════
   Main Wizard
   ═══════════════════════════════════════════ */

interface WizardProps {
  onComplete?: () => void;
}

export function Wizard({ onComplete }: WizardProps) {
  const setOnboardingComplete = useAppStore((s) => s.setOnboardingComplete);

  // When creating a new workspace from an existing one, identity is skipped —
  // the user already exists, they just need blueprint/team/setup/task.
  const skipIdentity =
    new URLSearchParams(window.location.search).get("skip_identity") === "1";
  const activeSteps: readonly WizardStep[] = skipIdentity
    ? STEP_ORDER.filter((s) => s !== "identity")
    : STEP_ORDER;

  // Navigation
  const [step, setStep] = useState<WizardStep>("welcome");

  // Step 2: templates
  const [blueprints, setBlueprints] = useState<BlueprintTemplate[]>([]);
  const [blueprintsLoading, setBlueprintsLoading] = useState(true);
  const [selectedBlueprint, setSelectedBlueprint] = useState<string | null>(
    null,
  );

  // Step 3: identity
  const [company, setCompany] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState("");
  // Optional in-wizard Nex registration. Mirrors the TUI's InitNexRegister
  // phase — we POST /nex/register which shells out to `nex-cli setup <email>`.
  // If nex-cli isn't installed we flip to `fallback` (external link to
  // nex.ai/register, key pasted on the Setup step).
  const [nexEmail, setNexEmail] = useState("");
  const [nexSignupStatus, setNexSignupStatus] =
    useState<NexSignupStatus>("hidden");
  const [nexSignupError, setNexSignupError] = useState("");

  // Step 4: team
  const [agents, setAgents] = useState<BlueprintAgent[]>([]);

  // Step 5: setup
  const [prereqs, setPrereqs] = useState<PrereqResult[]>([]);
  const [prereqsLoading, setPrereqsLoading] = useState(true);
  const [prereqsError, setPrereqsError] = useState("");
  // Ordered list of runtime labels (matches RUNTIMES[].label). Position in
  // the array is the fallback priority. Initially empty — we auto-populate
  // with the first installed CLI once prereqs land so the happy path still
  // works with zero clicks.
  const [runtimePriority, setRuntimePriority] = useState<string[]>([]);
  // localProvider is the local OpenAI-compat kind the user opted into in
  // the SetupStep subsection (mlx-lm | ollama | exo | "" for none).
  // When non-empty, it overrides whatever cloud CLI was selected and is
  // applied as llm_provider on /onboarding/complete.
  const [localProvider, setLocalProvider] = useState<string>("");
  const [apiKeys, setApiKeys] = useState<Record<string, string>>({});
  // Matches MEMORY_BACKEND_OPTIONS[0] (the "Markdown (default)" tile) and the
  // server-side `config.ResolveMemoryBackend` default. Shipping 'nex' here
  // contradicted the label and meant a user who clicked through got a
  // different backend than the one marked default.
  const [memoryBackend, setMemoryBackend] = useState<MemoryBackend>("markdown");
  // Nex API key (maps to `api_key` on /config). Parity with TUI's InitAPIKey
  // phase. Kept separate from `apiKeys` because the latter is the per-runtime
  // fallback set (Anthropic/OpenAI/Google) while this one unlocks hosted
  // memory and managed integrations. Empty = skipped, not an error.
  const [nexApiKey, setNexApiKey] = useState("");
  // GBrain-specific key inputs. Only rendered when memoryBackend === 'gbrain'.
  // Mirrors the TUI's InitGBrainOpenAIKey (required) + InitGBrainAnthropKey
  // (optional) phases.
  const [gbrainOpenAIKey, setGbrainOpenAIKey] = useState("");
  const [gbrainAnthropicKey, setGbrainAnthropicKey] = useState("");

  // Step 6: first task
  const [taskTemplates, setTaskTemplates] = useState<TaskTemplate[]>([]);
  const [selectedTaskTemplate, setSelectedTaskTemplate] = useState<
    string | null
  >(null);
  const [taskText, setTaskText] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");

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

  // Fetch prereqs on mount so the runtime picker shows which CLIs are
  // actually installed. Auto-select the first detected runtime so users
  // with a single CLI installed don't have to click.
  useEffect(() => {
    let cancelled = false;
    setPrereqsLoading(true);

    get<{ prereqs?: PrereqResult[] } | PrereqResult[]>("/onboarding/prereqs")
      .then((data) => {
        if (cancelled) return;
        const list = Array.isArray(data) ? data : (data.prereqs ?? []);
        setPrereqs(list);
        setRuntimePriority((current) => {
          if (current.length > 0) return current;
          const firstInstalled = RUNTIMES.find((spec) => {
            const det = list.find((p) => p.name === spec.binary);
            return Boolean(det?.found);
          });
          return firstInstalled ? [firstInstalled.label] : [];
        });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        const msg = err instanceof Error ? err.message : String(err);
        setPrereqsError(msg);
      })
      .finally(() => {
        if (!cancelled) setPrereqsLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const toggleRuntime = useCallback((label: string) => {
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
  useEffect(() => {
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
    const bpTasks = (bp as unknown as { tasks?: TaskTemplate[] } | undefined)
      ?.tasks;
    setTaskTemplates(Array.isArray(bpTasks) ? bpTasks : []);
    // Clear any task-template selection and suggestion-derived text when the
    // blueprint changes. Without this, switching from (say) Consulting to
    // YouTube Factory leaves "Turn the directive..." stuck in the textarea —
    // nonsensical in the new context. User-typed custom text is preserved,
    // since selectedTaskTemplate is null for that path.
    setSelectedTaskTemplate((prevSel) => {
      if (prevSel !== null) setTaskText("");
      return null;
    });
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

  // Open the in-wizard Nex signup affordance. A separate handler (not just
  // `setNexSignupStatus('open')` inline) keeps the error/email state reset in
  // one place — reopening after a failed attempt shouldn't leak the old error.
  const openNexSignup = useCallback(() => {
    setNexSignupError("");
    setNexSignupStatus("open");
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

  // Close the Nex signup panel via Escape — keeps the outer Escape handler
  // in `useKeyboardShortcuts` free to act on app-level panels without
  // fighting the wizard's internal affordance.
  const closeNexSignup = useCallback(() => {
    if (
      nexSignupStatus === "open" ||
      nexSignupStatus === "ok" ||
      nexSignupStatus === "fallback"
    ) {
      setNexSignupStatus("hidden");
      setNexSignupError("");
    }
  }, [nexSignupStatus]);

  // Compute readiness checks. Runs at render time for the 'ready' step — no
  // useMemo because the surface is small (6 checks) and recomputation only
  // happens when one of these inputs changes. Matches the TUI's six-item
  // list in init_flow.go readinessChecks().
  const readinessChecks: ReadinessCheck[] = (() => {
    const checks: ReadinessCheck[] = [];

    // 1. Nex API key
    const hasNexKey = nexApiKey.trim().length > 0;
    checks.push({
      label: "Nex API key",
      status: hasNexKey ? "ready" : "next",
      detail: hasNexKey
        ? "Configured. Hosted memory and integrations unlocked."
        : "Skipped. Paste a key later from Settings to enable hosted memory.",
    });

    // 2. Tmux / web session. The web app doesn't need tmux — that's the
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

    // 4. Memory backend
    const memoryLabel =
      MEMORY_BACKEND_OPTIONS.find((o) => o.value === memoryBackend)?.label ??
      memoryBackend;
    let memoryStatus: ReadinessStatus = "ready";
    let memoryDetail = memoryLabel;
    if (memoryBackend === "gbrain") {
      if (gbrainOpenAIKey.trim().length === 0) {
        memoryStatus = "missing";
        memoryDetail = "GBrain selected but OpenAI key is missing.";
      } else {
        memoryDetail = "GBrain with OpenAI embeddings.";
      }
    } else if (memoryBackend === "nex") {
      if (!hasNexKey) {
        memoryStatus = "next";
        memoryDetail =
          "Nex selected — add a Nex API key to enable hosted memory.";
      } else {
        memoryDetail = "Hosted memory via Nex.";
      }
    } else if (memoryBackend === "markdown") {
      memoryDetail = "Git-native team wiki in ~/.wuphf/wiki.";
    } else {
      memoryStatus = "next";
      memoryDetail = "No shared memory — agents only see per-turn context.";
    }
    checks.push({
      label: "Memory backend",
      status: memoryStatus,
      detail: memoryDetail,
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
    const keyCount =
      Object.values(apiKeys).filter((v) => v.trim().length > 0).length +
      (gbrainOpenAIKey.trim().length > 0 ? 1 : 0) +
      (gbrainAnthropicKey.trim().length > 0 ? 1 : 0);
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
          memory_backend: memoryBackend,
        };
        if (providerPriority.length > 0) {
          // First entry is the active provider; the full ordered list
          // is the fallback chain. The user controls the order via the
          // arrow buttons — if they move a local kind to slot 0 it
          // becomes the primary; if they keep it last it's the
          // out-of-credits fallback.
          configPayload.llm_provider = providerPriority[0];
          configPayload.llm_provider_priority = providerPriority;
        }
        // Nex API key (optional — empty string not sent so we don't clobber
        // an existing value with a blank one).
        const trimmedNex = nexApiKey.trim();
        if (trimmedNex.length > 0) {
          configPayload.api_key = trimmedNex;
        }
        // GBrain-conditional keys. Only forwarded when GBrain is the active
        // backend; other backends don't need these and sending would
        // overwrite any user-configured values on GET.
        if (memoryBackend === "gbrain") {
          const trimmedOAI = gbrainOpenAIKey.trim();
          if (trimmedOAI.length > 0) {
            configPayload.openai_api_key = trimmedOAI;
          }
          const trimmedAnthropic = gbrainAnthropicKey.trim();
          if (trimmedAnthropic.length > 0) {
            configPayload.anthropic_api_key = trimmedAnthropic;
          }
        }
        // Generic per-provider API keys from the fallback grid. Legacy
        // env-var-style keys (ANTHROPIC_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY)
        // mapped to the broker's config field names. Google key has no
        // /config field yet — drop it silently rather than fail.
        const genericAnthropic = (apiKeys.ANTHROPIC_API_KEY ?? "").trim();
        if (genericAnthropic.length > 0 && memoryBackend !== "gbrain") {
          configPayload.anthropic_api_key = genericAnthropic;
        }
        const genericOpenAI = (apiKeys.OPENAI_API_KEY ?? "").trim();
        if (genericOpenAI.length > 0 && memoryBackend !== "gbrain") {
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
          memory_backend: memoryBackend,
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

      setOnboardingComplete(true);
      onComplete?.();
    },
    [
      company,
      description,
      priority,
      runtimePriority,
      memoryBackend,
      selectedBlueprint,
      agents,
      apiKeys,
      nexApiKey,
      gbrainOpenAIKey,
      gbrainAnthropicKey,
      taskText,
      setOnboardingComplete,
      onComplete,
    ],
  );

  // Keyboard: Enter advances each step when the step's own gate allows it,
  // so the whole wizard can be run without reaching for the mouse. Textarea
  // steps (TaskStep) keep Enter for newlines; ⌘/Ctrl+Enter advances there.
  // The NexSignupPanel handles its own Enter inside the email input via an
  // onKeyDown below, so we bail out when that's the focused target.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        closeNexSignup();
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
      const tag = target?.tagName;
      if (tag === "BUTTON" || tag === "A" || tag === "SELECT") return;
      const inTextarea = tag === "TEXTAREA";
      const isSubmitCombo = e.metaKey || e.ctrlKey;
      if (inTextarea && !isSubmitCombo) return;

      const canIdentityContinue =
        company.trim().length > 0 && description.trim().length > 0;
      const hasInstalledSelection = runtimePriority.some((label) =>
        runtimeIsReady(label, prereqs, prereqsError),
      );
      const hasAnyApiKey = Object.values(apiKeys).some(
        (v) => v.trim().length > 0,
      );
      // Keyboard gate must mirror SetupStep's own canContinue logic
      // (line ~1176): a keyboard-only user who picks just `mlx-lm` /
      // `ollama` / `exo` should be able to advance with Enter even
      // though they didn't install a cloud CLI or paste an API key.
      // Without this, the primary-CTA enabled state and the Enter
      // gate disagreed — visually advanceable, keyboardly stuck.
      const hasLocalProvider = localProvider.trim().length > 0;
      const gbrainOpenAIMissing =
        memoryBackend === "gbrain" && gbrainOpenAIKey.trim().length === 0;
      const canSetupContinue =
        (hasInstalledSelection || hasAnyApiKey || hasLocalProvider) &&
        !gbrainOpenAIMissing;

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
          if (canSetupContinue) {
            e.preventDefault();
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
    memoryBackend,
    gbrainOpenAIKey,
    submitting,
    taskText,
    goTo,
    nextStep,
    finishOnboarding,
    closeNexSignup,
    localProvider,
  ]);

  return (
    <div className="wizard-container">
      <div className="wizard-body">
        <ProgressDots current={step} steps={activeSteps} />

        {step === "welcome" && (
          <WelcomeStep onNext={() => goTo(activeSteps[1] ?? "templates")} />
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
            nexEmail={nexEmail}
            nexSignupStatus={nexSignupStatus}
            nexSignupError={nexSignupError}
            onChangeCompany={setCompany}
            onChangeDescription={setDescription}
            onChangePriority={setPriority}
            onChangeNexEmail={setNexEmail}
            onSubmitNexSignup={submitNexSignup}
            onOpenNexSignup={openNexSignup}
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
            prereqs={prereqs}
            prereqsLoading={prereqsLoading}
            prereqsError={prereqsError}
            runtimePriority={runtimePriority}
            onToggleRuntime={toggleRuntime}
            onReorderRuntime={reorderRuntime}
            apiKeys={apiKeys}
            onChangeApiKey={handleApiKeyChange}
            memoryBackend={memoryBackend}
            onChangeMemoryBackend={setMemoryBackend}
            nexApiKey={nexApiKey}
            onChangeNexApiKey={setNexApiKey}
            gbrainOpenAIKey={gbrainOpenAIKey}
            onChangeGBrainOpenAIKey={setGbrainOpenAIKey}
            gbrainAnthropicKey={gbrainAnthropicKey}
            onChangeGBrainAnthropicKey={setGbrainAnthropicKey}
            localProvider={localProvider}
            onSelectLocalProvider={selectLocalProvider}
            onNext={nextStep}
            onBack={prevStep}
          />
        )}

        {step === "task" && (
          <TaskStep
            taskTemplates={taskTemplates}
            selectedTaskTemplate={selectedTaskTemplate}
            onSelectTaskTemplate={setSelectedTaskTemplate}
            taskText={taskText}
            onChangeTaskText={setTaskText}
            onNext={nextStep}
            onSkip={() => {
              setTaskText("");
              setSelectedTaskTemplate(null);
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
