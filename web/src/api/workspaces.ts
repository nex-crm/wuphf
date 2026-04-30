/**
 * Workspaces API — TanStack Query hooks for the multi-workspace surface.
 *
 * Architecture: page-reload-on-switch. The SPA only ever talks to its
 * served broker; cross-broker orchestration happens server-side. There
 * is no peer-token map, no cross-origin auth, and CORS stays restricted
 * to the broker's own web UI origin (see internal/team/broker.go).
 *
 * All endpoints below sit on the broker's `/workspaces/*` namespace and
 * are authenticated via the existing bearer-token flow in client.ts —
 * no new auth path is introduced here.
 */
import {
  type UseMutationOptions,
  type UseQueryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import { get, post } from "./client";

export type WorkspaceState =
  | "running"
  | "paused"
  | "starting"
  | "stopping"
  | "never_started"
  | "error";

/**
 * Mirrors `internal/workspaces/registry.json` workspace entries plus the
 * fields the broker decorates onto each row when serving `/workspaces/list`
 * (live `state` from parallel HEAD probes, etc.). Lane C owns the wire
 * shape — keep these field names in lockstep.
 */
export interface Workspace {
  name: string;
  runtime_home: string;
  broker_port: number;
  web_port: number;
  state: WorkspaceState;
  blueprint?: string;
  company_name?: string;
  created_at?: string;
  last_used_at?: string | null;
  paused_at?: string | null;
  /** Active workspace flag — broker sets this on the row matching its own runtime. */
  is_active?: boolean;
  /** Optional last-known cumulative cost for the workspace. */
  cost_usd?: number;
  /** Optional tokens-today counter (broker may emit if cheap). */
  tokens_today?: number;
}

export interface WorkspaceListResponse {
  workspaces: Workspace[];
  /** The active workspace name as understood by the served broker. */
  active?: string;
}

/**
 * Mirrors `internal/workspaces.TrashEntry` (Go side) — the orchestrator
 * canonical shape. `shred_at` is RFC3339 from the broker, empty string
 * when the trash directory name lacks a parseable timestamp.
 */
export interface TrashEntry {
  name: string;
  trash_id: string;
  path: string;
  shred_at?: string;
  original_runtime_home?: string;
  /**
   * Legacy field preserved for callers that still read `shredded_at`. The
   * broker no longer emits this; clients should migrate to `shred_at`.
   */
  shredded_at?: string;
  size_bytes?: number;
}

export interface TrashListResponse {
  entries: TrashEntry[];
}

/**
 * Keys are namespaced under `workspaces` so cache-busting after a
 * lifecycle mutation is a single `invalidateQueries({ queryKey:
 * workspaceKeys.all })` call.
 */
export const workspaceKeys = {
  all: ["workspaces"] as const,
  list: () => [...workspaceKeys.all, "list"] as const,
  trash: () => [...workspaceKeys.all, "trash"] as const,
  usage: () => ["usage"] as const,
};

/* ─── Reads ─────────────────────────────────────────────────── */

export function useWorkspacesList(
  options?: Partial<UseQueryOptions<WorkspaceListResponse>>,
) {
  return useQuery<WorkspaceListResponse>({
    queryKey: workspaceKeys.list(),
    queryFn: () => get<WorkspaceListResponse>("/workspaces/list"),
    // Refresh in the background so paused/running state stays roughly
    // current without spamming the broker. Lane C's parallel HEAD probe
    // is bounded to ~200ms regardless of N, so this stays cheap.
    refetchInterval: 30_000,
    staleTime: 10_000,
    ...options,
  });
}

/**
 * Lists the contents of `~/.wuphf-spaces/.trash/` via the broker's
 * dedicated `/workspaces/trash` endpoint (CodeRabbit #3164366654). The
 * earlier shape piggy-backed on `/workspaces/list?include=trash`; that
 * has been retired in favour of a clean GET on its own path.
 */
export function useWorkspaceTrash(
  options?: Partial<UseQueryOptions<TrashListResponse>>,
) {
  return useQuery<TrashListResponse>({
    queryKey: workspaceKeys.trash(),
    queryFn: () =>
      get<{ trash: TrashEntry[] }>("/workspaces/trash").then((d) => ({
        entries: d.trash ?? [],
      })),
    staleTime: 30_000,
    ...options,
  });
}

/* ─── Mutations ─────────────────────────────────────────────── */

/**
 * Broker accepts: {name, blueprint?, inherit_from?, company_name?, from_scratch?}.
 * The decoder is strict (DisallowUnknownFields), so any field not on this
 * list will 400 the request. Richer onboarding fields (company_description,
 * company_priority, llm_provider*, team_lead_slug) are scoped to the
 * subsequent /onboarding/* calls per the design's two-step flow; they are
 * intentionally NOT part of the create payload.
 *
 * Wire shape mirrors `internal/team/broker_workspaces.go::CreateRequest`.
 */
export interface CreateWorkspaceInput {
  name: string;
  blueprint?: string;
  /** Source workspace for inherited fields (default: cli_current). */
  inherit_from?: string;
  company_name?: string;
  /** Skip blueprint inheritance — start blank, run full onboarding. */
  from_scratch?: boolean;
}

/**
 * Broker `handleWorkspacesCreate` returns the freshly created Workspace
 * row directly (status 201) — there is no envelope object. Lane C's
 * design opted for this shape so the client can pass the row straight
 * into the workspace cache without unwrapping.
 */
export type CreateWorkspaceResponse = Workspace;

export function useCreateWorkspace(
  options?: UseMutationOptions<
    CreateWorkspaceResponse,
    Error,
    CreateWorkspaceInput
  >,
) {
  const qc = useQueryClient();
  return useMutation<CreateWorkspaceResponse, Error, CreateWorkspaceInput>({
    ...options,
    mutationFn: (body) =>
      post<CreateWorkspaceResponse>("/workspaces/create", body),
    onSuccess: (data, vars, onMutate, ctx) => {
      void qc.invalidateQueries({ queryKey: workspaceKeys.all });
      options?.onSuccess?.(data, vars, onMutate, ctx);
    },
  });
}

export interface PauseWorkspaceInput {
  name: string;
}

export function usePauseWorkspace(
  options?: UseMutationOptions<{ ok: boolean }, Error, PauseWorkspaceInput>,
) {
  const qc = useQueryClient();
  return useMutation<{ ok: boolean }, Error, PauseWorkspaceInput>({
    ...options,
    mutationFn: (body) => post<{ ok: boolean }>("/workspaces/pause", body),
    onSuccess: (data, vars, onMutate, ctx) => {
      void qc.invalidateQueries({ queryKey: workspaceKeys.all });
      options?.onSuccess?.(data, vars, onMutate, ctx);
    },
  });
}

export interface ResumeWorkspaceInput {
  name: string;
}

/**
 * Broker `handleWorkspacesResume` returns {ok, name} after a successful
 * spawn — the SPA already knows the runtime_home/web_port from the prior
 * list response, so the resume RPC stays minimal. If a richer payload is
 * needed later, the broker can add fields without breaking this shape.
 */
export interface ResumeWorkspaceResponse {
  ok: boolean;
  name: string;
}

export function useResumeWorkspace(
  options?: UseMutationOptions<
    ResumeWorkspaceResponse,
    Error,
    ResumeWorkspaceInput
  >,
) {
  const qc = useQueryClient();
  return useMutation<ResumeWorkspaceResponse, Error, ResumeWorkspaceInput>({
    ...options,
    mutationFn: (body) =>
      post<ResumeWorkspaceResponse>("/workspaces/resume", body),
    onSuccess: (data, vars, onMutate, ctx) => {
      void qc.invalidateQueries({ queryKey: workspaceKeys.all });
      options?.onSuccess?.(data, vars, onMutate, ctx);
    },
  });
}

export interface ShredWorkspaceInput {
  name: string;
  permanent?: boolean;
}

export interface ShredWorkspaceResponse {
  ok: boolean;
  /** Trash ID for /workspaces/restore. Absent when `permanent: true`. */
  trash_id?: string;
}

export function useShredWorkspace(
  options?: UseMutationOptions<
    ShredWorkspaceResponse,
    Error,
    ShredWorkspaceInput
  >,
) {
  const qc = useQueryClient();
  return useMutation<ShredWorkspaceResponse, Error, ShredWorkspaceInput>({
    ...options,
    mutationFn: (body) =>
      post<ShredWorkspaceResponse>("/workspaces/shred", body),
    onSuccess: (data, vars, onMutate, ctx) => {
      void qc.invalidateQueries({ queryKey: workspaceKeys.all });
      options?.onSuccess?.(data, vars, onMutate, ctx);
    },
  });
}

/* ─── Per-workspace onboarding ──────────────────────────────── */

/**
 * Rich onboarding fields applied to a freshly created workspace via the
 * broker's `/workspaces/onboarding` proxy. The broker's `CreateRequest`
 * decoder is strict (DisallowUnknownFields), so these fields can't ride on
 * the create payload — instead we run a two-step "create then onboard"
 * flow per CodeRabbit #3164366659 and #3164366660.
 *
 * Wire shape mirrors `internal/team/broker_workspaces.go::OnboardingFields`.
 */
export interface ApplyOnboardingInput {
  name: string;
  company_description?: string;
  company_priority?: string;
  llm_provider?: string;
  team_lead_slug?: string;
}

export interface ApplyOnboardingResponse {
  ok: boolean;
  name: string;
}

export function useApplyOnboarding(
  options?: UseMutationOptions<
    ApplyOnboardingResponse,
    Error,
    ApplyOnboardingInput
  >,
) {
  const qc = useQueryClient();
  return useMutation<ApplyOnboardingResponse, Error, ApplyOnboardingInput>({
    mutationFn: (body) =>
      post<ApplyOnboardingResponse>("/workspaces/onboarding", body),
    onSuccess: (data, vars, onMutate, ctx) => {
      void qc.invalidateQueries({ queryKey: workspaceKeys.all });
      options?.onSuccess?.(data, vars, onMutate, ctx);
    },
    ...options,
  });
}

export interface RestoreWorkspaceInput {
  trash_id: string;
}

/**
 * Broker `handleWorkspacesRestore` returns the restored Workspace row
 * directly (same shape as Create). Caller computes the URL from web_port.
 */
export type RestoreWorkspaceResponse = Workspace;

export function useRestoreWorkspace(
  options?: UseMutationOptions<
    RestoreWorkspaceResponse,
    Error,
    RestoreWorkspaceInput
  >,
) {
  const qc = useQueryClient();
  return useMutation<RestoreWorkspaceResponse, Error, RestoreWorkspaceInput>({
    ...options,
    mutationFn: (body) =>
      post<RestoreWorkspaceResponse>("/workspaces/restore", body),
    onSuccess: (data, vars, onMutate, ctx) => {
      void qc.invalidateQueries({ queryKey: workspaceKeys.all });
      options?.onSuccess?.(data, vars, onMutate, ctx);
    },
  });
}

/* ─── Slug validation ──────────────────────────────────────── */

/**
 * Mirrors `internal/workspaces`'s reserved-name list. Keep this list in
 * lockstep with the Go side — the broker is the source of truth and will
 * also reject these on /workspaces/create, but we surface the rejection
 * inline before the user has paid for a 30-second spawn round-trip.
 */
export const RESERVED_WORKSPACE_NAMES: readonly string[] = [
  "main",
  "dev",
  "prod",
  "default",
  "current",
  "tokens",
  "trash",
] as const;

const SLUG_REGEX = /^[a-z][a-z0-9-]{0,30}$/;

export interface SlugValidation {
  ok: boolean;
  reason?: string;
}

export function validateWorkspaceSlug(input: string): SlugValidation {
  const trimmed = input.trim();
  if (trimmed.length === 0) {
    return { ok: false, reason: "Workspace name is required." };
  }
  if (trimmed.startsWith(".") || trimmed.startsWith("__")) {
    return {
      ok: false,
      reason: "Names starting with '.' or '__' are reserved.",
    };
  }
  if (!SLUG_REGEX.test(trimmed)) {
    return {
      ok: false,
      reason:
        "Use lowercase letters, digits, and hyphens. Must start with a letter. Max 31 chars.",
    };
  }
  if (RESERVED_WORKSPACE_NAMES.includes(trimmed)) {
    return {
      ok: false,
      reason: `'${trimmed}' is reserved. Try a different name.`,
    };
  }
  return { ok: true };
}
