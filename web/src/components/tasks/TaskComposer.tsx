/**
 * TaskComposer — the new-task home composer.
 *
 * This is the app's landing surface: a single centered chatbox where a human
 * describes an outcome, picks who owns it and how it runs (owner agent /
 * provider / model / reasoning effort), and chooses how to start it — now, on
 * the backlog, or as a recurring routine.
 *
 * Model + effort are coupled and model-specific: selecting a provider sets the
 * runtime, the model dropdown lists that runtime's models, and the effort chip
 * offers only the levels that runtime applies at dispatch (see effortCatalog).
 *
 * The provider/model/effort are sent ON THE TASK — the model is a property of
 * the task, not the agent, so nothing here mutates an agent's binding. Dispatch
 * prefers the task's runtime over the owner's (soft-default) binding.
 *
 * Every task is assigned: the owner chip defaults to "Auto" (the CEO picks the
 * best specialist) and also lists the CEO + specialists. "Start now" dispatches
 * the owner (Auto → CEO triages); "Backlog" parks the task assigned until the
 * user starts it; "Routine" hands off to the recurring-routine composer.
 */

import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getConfig,
  getLocalProvidersStatus,
  type LLMRuntimeKind,
  type LocalProviderStatus,
} from "../../api/client";
import { createTasks } from "../../api/tasks";
import { useOfficeMembers } from "../../hooks/useMembers";
import {
  effortOptionsForKind,
  normalizeEffortForKind,
  runtimeSupportsEffort,
} from "../../lib/effortCatalog";
import { isCatalogModel, modelOptionsForKind } from "../../lib/modelCatalog";
import {
  bindingFromMember,
  DEFAULT_LLM_KINDS,
  PROVIDER_LABELS,
  runtimeKindFromMember,
} from "../../lib/providerBinding";
import { router } from "../../lib/router";

type CreateMode = "start" | "backlog" | "routine";

const DEFAULT_CHANNEL = "general";

// "auto" owner sentinel — the CEO picks the real specialist (the floor
// assignment; every task is assigned). Mirrors isAutoOwner on the Go side.
const AUTO_OWNER = "auto";

// firstLine returns a task title from the free-text prompt: the first
// non-empty line, trimmed to a sane title length. The full prompt is kept as
// the task details so nothing the user typed is lost.
function deriveTitle(prompt: string): string {
  const line = prompt
    .split("\n")
    .map((l) => l.trim())
    .find((l) => l.length > 0);
  const title = (line ?? prompt).trim();
  return title.length > 140 ? `${title.slice(0, 137)}…` : title;
}

export function TaskComposer() {
  const queryClient = useQueryClient();
  const membersQuery = useOfficeMembers();
  const members = useMemo(() => membersQuery.data ?? [], [membersQuery.data]);

  const configQuery = useQuery({
    queryKey: ["config"],
    queryFn: getConfig,
    staleTime: 60_000,
  });
  const localStatusQuery = useQuery({
    queryKey: ["local-providers"],
    queryFn: getLocalProvidersStatus,
    staleTime: 30_000,
  });
  const localStatuses: LocalProviderStatus[] = localStatusQuery.data ?? [];
  const llmKinds = (configQuery.data?.llm_provider_kinds ??
    DEFAULT_LLM_KINDS) as LLMRuntimeKind[];
  // configQuery.data.llm_provider is an LLMProvider (LLMRuntimeKind |
  // GatewayKind). Casting it straight to LLMRuntimeKind is unsound: if the
  // install's default provider is a gateway (e.g. "openrouter"), downstream
  // model/effort lookups receive a kind they don't recognize. Narrow through
  // the runtime allow-list and fall back to the first available kind.
  const rawDefaultProvider = configQuery.data?.llm_provider;
  const globalDefaultKind: LLMRuntimeKind =
    rawDefaultProvider &&
    (llmKinds as readonly string[]).includes(rawDefaultProvider)
      ? (rawDefaultProvider as LLMRuntimeKind)
      : (llmKinds[0] ?? "claude-code");

  const [prompt, setPrompt] = useState("");
  const [ownerSlug, setOwnerSlug] = useState<string>(AUTO_OWNER);
  const [providerKind, setProviderKind] = useState<"" | LLMRuntimeKind>("");
  const [model, setModel] = useState("");
  const [effort, setEffort] = useState("");
  // Plan mode (Phase 5): default ON. When on, the owner plans autonomously
  // (writes a plan to its notebook) and waits for "Approve & Start" before
  // executing. When off, the task runs immediately with no plan/approval gate.
  const [planFirst, setPlanFirst] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const promptRef = useRef<HTMLTextAreaElement | null>(null);
  const submitLockRef = useRef(false);

  // The selected owner's stored binding seeds the chips as a starting point.
  // "auto" (and any owner without a binding) seeds from the global default
  // runtime. Whatever the user picks is sent ON THE TASK — it never mutates the
  // agent's binding (the model is a property of the task, not the agent).
  const ownerMember = members.find((m) => m.slug === ownerSlug);
  const ownerBinding = bindingFromMember(ownerMember?.provider);
  const ownerKind = runtimeKindFromMember(ownerMember?.provider, llmKinds);
  const ownerModel = ownerBinding.model ?? "";

  // Seed the runtime chips when the owner (or their binding) changes. Keyed on
  // primitive values so the 5s members refetch — which returns identical
  // strings — does NOT re-run and clobber the user's chip edits.
  useEffect(() => {
    const kind = ownerKind || globalDefaultKind;
    setProviderKind(kind);
    setModel(ownerModel);
    setEffort((prev) => normalizeEffortForKind(kind, prev));
  }, [ownerKind, ownerModel, globalDefaultKind]);

  useEffect(() => {
    promptRef.current?.focus();
  }, []);

  const modelOptions = useMemo(
    () => modelOptionsForKind(providerKind, localStatuses),
    [providerKind, localStatuses],
  );
  const effortOptions = useMemo(
    () => effortOptionsForKind(providerKind),
    [providerKind],
  );
  const effortEnabled = runtimeSupportsEffort(providerKind);

  function handleProviderChange(nextKind: "" | LLMRuntimeKind) {
    setProviderKind(nextKind);
    // Clamp model + effort to the new runtime so we never submit a stale value.
    setModel((prev) =>
      isCatalogModel(nextKind, prev, localStatuses) ? prev : "",
    );
    setEffort((prev) => normalizeEffortForKind(nextKind, prev));
  }

  // createAndRoute creates the task and hands the user off. The task carries
  // its own provider/model/effort, so nothing here mutates an agent binding.
  //   Start now → dispatch the owner (a real owner runs; Auto → CEO triages).
  //   Backlog   → park=true: assigned but parked in the backlog until started.
  async function createAndRoute(mode: "start" | "backlog", owner: string) {
    setError(null);
    submitLockRef.current = true;
    setSubmitting(true);
    try {
      const response = await createTasks(
        [
          {
            title: deriveTitle(prompt.trim()),
            assignee: owner,
            details: prompt.trim(),
            task_type: "issue",
            provider: providerKind || undefined,
            model: model.trim() || undefined,
            effort: effort || undefined,
            park: mode === "backlog",
            plan_first: planFirst,
          },
        ],
        { channel: DEFAULT_CHANNEL, createdBy: "human" },
      );
      const created = response.tasks?.[0];
      void queryClient.invalidateQueries({ queryKey: ["issues"] });
      void queryClient.invalidateQueries({ queryKey: ["office-tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["lifecycle"] });
      setPrompt("");
      // Start-now hands the user to the live task; Backlog keeps them on the
      // board to queue more.
      if (mode === "start" && created?.id) {
        void router.navigate({
          to: "/tasks/$taskId",
          params: { taskId: created.id },
        });
      } else {
        void router.navigate({ to: "/tasks" });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create task.");
    } finally {
      submitLockRef.current = false;
      setSubmitting(false);
    }
  }

  function handleCreate(mode: CreateMode) {
    if (submitLockRef.current) return;
    const trimmedPrompt = prompt.trim();
    if (!trimmedPrompt) {
      setError("Describe what you want to get done.");
      promptRef.current?.focus();
      return;
    }

    // Routine work routes to the recurring-routine composer rather than
    // creating a one-off task. Carry the prompt through as the routine's
    // title + instructions so the user does not retype it.
    if (mode === "routine") {
      void router.navigate({
        to: "/routines/new",
        search: {
          label: deriveTitle(trimmedPrompt),
          instructions: trimmedPrompt,
        },
      });
      return;
    }
    void createAndRoute(mode, ownerSlug.trim() || AUTO_OWNER);
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void handleCreate("start");
  }

  const isAuto = ownerSlug === AUTO_OWNER;
  const ownerLabel = isAuto
    ? "Auto — CEO picks the specialist"
    : ownerMember?.name || ownerSlug || "CEO";
  const runtimeLabel = model.trim() || "runtime default";

  return (
    <main className="task-composer-screen" data-testid="task-composer">
      <div className="task-composer">
        <h1 className="task-composer-title">What do you want to get done?</h1>
        <p className="task-composer-subtitle">
          Describe the outcome. The team specs it, gets your approval, then runs
          it.
        </p>
        <form className="task-composer-form" onSubmit={handleSubmit}>
          <textarea
            ref={promptRef}
            className="task-composer-input"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="e.g. Draft a Q3 outbound sequence for mid-market RevOps leaders and book a review."
            rows={4}
            data-testid="task-composer-input"
          />

          <div className="task-composer-chips">
            <label className="task-chip">
              <span className="task-chip-label">Owner</span>
              <select
                className="task-chip-select"
                value={ownerSlug}
                onChange={(e) => setOwnerSlug(e.target.value)}
                data-testid="task-composer-owner"
              >
                {/* Auto is the floor: the CEO picks the best specialist. */}
                <option value={AUTO_OWNER}>Auto</option>
                {members.map((m) => (
                  <option key={m.slug} value={m.slug}>
                    {m.name || m.slug}
                  </option>
                ))}
              </select>
            </label>

            <label className="task-chip">
              <span className="task-chip-label">Provider</span>
              <select
                className="task-chip-select"
                value={providerKind}
                onChange={(e) =>
                  handleProviderChange(e.target.value as "" | LLMRuntimeKind)
                }
                data-testid="task-composer-provider"
              >
                {llmKinds.map((kind) => (
                  <option key={kind} value={kind}>
                    {PROVIDER_LABELS[kind] ?? kind}
                  </option>
                ))}
              </select>
            </label>

            <label className="task-chip">
              <span className="task-chip-label">Model</span>
              <select
                className="task-chip-select"
                value={
                  isCatalogModel(providerKind, model, localStatuses)
                    ? model
                    : ""
                }
                onChange={(e) => setModel(e.target.value)}
                data-testid="task-composer-model"
              >
                {modelOptions.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </label>

            <label
              className="task-chip"
              data-disabled={effortEnabled ? undefined : "true"}
            >
              <span className="task-chip-label">Effort</span>
              <select
                className="task-chip-select"
                value={effort}
                onChange={(e) => setEffort(e.target.value)}
                disabled={!effortEnabled}
                title={
                  effortEnabled
                    ? undefined
                    : `${PROVIDER_LABELS[providerKind as LLMRuntimeKind] ?? "This runtime"} uses its default effort`
                }
                data-testid="task-composer-effort"
              >
                {effortOptions.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <p className="task-composer-hint">
            Owner <strong>{ownerLabel}</strong> · runs on{" "}
            <strong>{runtimeLabel}</strong>
            {effort ? ` · ${effort} effort` : ""}. Model and effort apply to
            this task only.
          </p>

          <label
            className="task-composer-planfirst"
            data-testid="task-composer-planfirst"
          >
            <input
              type="checkbox"
              checked={planFirst}
              onChange={(e) => setPlanFirst(e.target.checked)}
              data-testid="task-composer-planfirst-input"
            />
            <span>
              <strong>Plan first</strong> —{" "}
              {planFirst
                ? "the owner writes a plan for your approval before starting the work."
                : "off: the owner starts the work immediately, no plan or approval."}
            </span>
          </label>

          {error ? (
            <p
              className="task-composer-error"
              role="alert"
              data-testid="task-composer-error"
            >
              {error}
            </p>
          ) : null}

          <div className="task-composer-actions">
            <button
              type="submit"
              className="task-composer-btn task-composer-btn-primary"
              disabled={submitting}
              title={
                planFirst
                  ? isAuto
                    ? "Create and have the CEO assign an owner who plans it first for your approval"
                    : `Assign @${ownerSlug} to plan it first for your approval`
                  : isAuto
                    ? "Create and have the CEO assign + start it now"
                    : `Assign @${ownerSlug} and start now`
              }
              data-testid="task-composer-start"
            >
              {submitting
                ? "Creating…"
                : planFirst
                  ? "Plan & start"
                  : "Start now"}
            </button>
            <button
              type="button"
              className="task-composer-btn"
              onClick={() => void handleCreate("backlog")}
              disabled={submitting}
              title="Park in the backlog (assigned) — nobody starts until you activate it"
              data-testid="task-composer-backlog"
            >
              Backlog
            </button>
            <button
              type="button"
              className="task-composer-btn"
              onClick={() => void handleCreate("routine")}
              disabled={submitting}
              title="Set up as a recurring routine on a schedule"
              data-testid="task-composer-routine"
            >
              Routine
            </button>
          </div>
        </form>
      </div>
    </main>
  );
}
