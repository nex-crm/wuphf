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
 * Provider/model selection writes the OWNER agent's runtime binding (that is
 * where WUPHF stores the model an agent runs on), so changing them updates the
 * owner's default — surfaced in the hint line. Effort is stored on the task
 * itself and applies to this task only.
 */

import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getConfig,
  getLocalProvidersStatus,
  type LLMRuntimeKind,
  type LocalProviderStatus,
  post,
} from "../../api/client";
import { createTasks } from "../../api/tasks";
import { useTeamLeadSlug } from "../../hooks/useConfig";
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
  const leadSlug = useTeamLeadSlug(members);

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
  const globalDefaultKind = (configQuery.data?.llm_provider ??
    "claude-code") as LLMRuntimeKind;

  const [prompt, setPrompt] = useState("");
  const [ownerSlug, setOwnerSlug] = useState<string>("");
  const [providerKind, setProviderKind] = useState<"" | LLMRuntimeKind>("");
  const [model, setModel] = useState("");
  const [effort, setEffort] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const promptRef = useRef<HTMLTextAreaElement | null>(null);
  const submitLockRef = useRef(false);

  // Default the owner to the team lead once members + config resolve.
  useEffect(() => {
    if (!ownerSlug && leadSlug) setOwnerSlug(leadSlug);
  }, [ownerSlug, leadSlug]);

  // Track the owner's stored binding so we only PATCH when the user actually
  // changes provider/model away from it.
  const ownerMember = members.find((m) => m.slug === ownerSlug);
  const ownerBinding = bindingFromMember(ownerMember?.provider);
  const ownerKind = runtimeKindFromMember(ownerMember?.provider, llmKinds);
  const ownerModel = ownerBinding.model ?? "";

  // Sync the chips to the owner's stored runtime. Keyed on the binding's
  // primitive values (ownerKind/ownerModel) rather than just ownerSlug so the
  // chips populate once members finish loading on cold boot, not only when the
  // user switches owner. These are stable strings — the 5s members refetch
  // returns identical values, so it does NOT re-run and clobber a user's chip
  // edits; the effect only fires when the owner or their binding truly changes.
  useEffect(() => {
    if (!ownerSlug) return;
    const kind = ownerKind || globalDefaultKind;
    setProviderKind(kind);
    setModel(ownerModel);
    setEffort((prev) => normalizeEffortForKind(kind, prev));
  }, [ownerSlug, ownerKind, ownerModel, globalDefaultKind]);

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

  const providerChanged =
    providerKind !== ownerKind || model.trim() !== ownerModel.trim();

  async function persistOwnerBindingIfChanged() {
    // Only registered agents carry a runtime binding; skip for the human owner
    // and when nothing changed.
    if (!ownerSlug || ownerSlug === "human" || !ownerMember) return;
    if (!providerChanged) return;
    const provider =
      providerKind === ""
        ? { kind: "", model: "" }
        : { kind: providerKind, model: model.trim() };
    await post("/office-members", {
      action: "update",
      slug: ownerSlug,
      provider,
    });
    void queryClient.invalidateQueries({ queryKey: ["office-members"] });
  }

  // createAndRoute persists any binding change, creates the task, and hands
  // the user off. Split out of handleCreate so the latter stays a thin
  // validate-then-dispatch shell. `mode` here is "start" | "backlog".
  async function createAndRoute(mode: "start" | "backlog", owner: string) {
    setError(null);
    submitLockRef.current = true;
    setSubmitting(true);
    try {
      await persistOwnerBindingIfChanged();
      const response = await createTasks(
        [
          {
            title: deriveTitle(prompt.trim()),
            assignee: owner,
            details: prompt.trim(),
            task_type: "issue",
            effort: effort || undefined,
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
      // board to queue more. (3c: Backlog should create without dispatching.)
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
    if (!prompt.trim()) {
      setError("Describe what you want to get done.");
      promptRef.current?.focus();
      return;
    }
    const owner = ownerSlug.trim() || leadSlug || "ceo";

    // Routine work routes to the recurring-routine composer rather than
    // creating a one-off task. (3c: prefill the routine from this prompt.)
    if (mode === "routine") {
      void router.navigate({ to: "/routines/new" });
      return;
    }
    void createAndRoute(mode, owner);
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void handleCreate("start");
  }

  const ownerLabel = ownerMember?.name || ownerSlug || "CEO";

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
                {members.length === 0 ? (
                  <option value="">CEO</option>
                ) : (
                  members.map((m) => (
                    <option key={m.slug} value={m.slug}>
                      {m.name || m.slug}
                    </option>
                  ))
                )}
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
            Runs <strong>@{ownerSlug || "ceo"}</strong> ({ownerLabel}).
            {providerChanged
              ? " Provider/model update this agent's default runtime."
              : null}{" "}
            Effort applies to this task only.
          </p>

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
              data-testid="task-composer-start"
            >
              {submitting ? "Creating…" : "Start now"}
            </button>
            <button
              type="button"
              className="task-composer-btn"
              onClick={() => void handleCreate("backlog")}
              disabled={submitting}
              data-testid="task-composer-backlog"
            >
              Backlog
            </button>
            <button
              type="button"
              className="task-composer-btn"
              onClick={() => void handleCreate("routine")}
              disabled={submitting}
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
