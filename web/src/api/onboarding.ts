import { get, post } from "./client";

export interface OSScanResponse {
  facts: string[];
  articles_written: string[];
  warnings?: string[];
}

/**
 * VerifyStatus is the classified outcome of a live runtime check. Mirrors
 * internal/onboarding/verify.go (VerifyStatus const block) 1:1 so the union
 * stays in lockstep with the wire. payment_required and quota_exceeded are
 * deliberately absent: the backend will not run a billable request during
 * verification, so it cannot honestly detect them.
 */
export type VerifyStatus =
  | "pass"
  | "not_installed"
  | "auth_required"
  | "other_error";

/**
 * VerifyResult is the response shape of POST /onboarding/verify. Mirrors
 * the VerifyResult Go struct. Optional fields are omitted by the backend on
 * a pass (a ready runtime needs no nudge), so treat empty/absent as "no
 * guidance for this field".
 */
export interface VerifyResult {
  /** The classified outcome. */
  status: VerifyStatus;
  /** Echoes the runtime name that was verified. */
  runtime: string;
  /**
   * The suggested next command to run: the install hint for not_installed,
   * the sign-in command for auth_required. Empty on pass.
   */
  command?: string;
  /** The runtime's sign-in command, surfaced separately. Set on auth_required. */
  sign_in_command?: string;
  /** One line of plain-language guidance for the classified status. */
  hint?: string;
  /** Names the guided step that did not pass, so the UI can highlight it. */
  failed_step?: string;
  /** The parsed --version string when the binary is present. */
  version?: string;
}

/**
 * InstallStep is one numbered step in a runtime's guided setup. Mirrors the
 * InstallStep Go struct. detail/command/link_label/link_url are all optional
 * (omitempty on the Go side).
 */
export interface InstallStep {
  /** Short step heading (e.g. "Install Claude Code"). Always present. */
  title: string;
  /** One line expanding on the step. Empty when the title says it all. */
  detail?: string;
  /** A copyable shell command. Empty for browser actions / no single command. */
  command?: string;
  /** Label for the doc link (e.g. "Install guide"). */
  link_label?: string;
  /** Doc/install URL for the step. */
  link_url?: string;
}

export interface InstallStepsResponse {
  runtime: string;
  steps: InstallStep[];
}

/**
 * Live runtime classification. POSTs the runtime name to /onboarding/verify
 * and returns the classified result. The backend forks subprocess probes
 * (CheckOne + a session probe) behind a 10s deadline, so this is the
 * expensive path: render the static guided setup from fetchInstallSteps and
 * only call this when the user presses Verify.
 */
export function verifyRuntime(
  runtime: string,
  signal?: AbortSignal,
): Promise<VerifyResult> {
  return post<VerifyResult>("/onboarding/verify", { runtime }, { signal });
}

/**
 * Static guided-setup steps for a runtime. GETs /onboarding/install-steps.
 * Cheap static metadata (no subprocess), so the picker can render the guide
 * without paying for a live probe. Unknown runtimes return an empty steps
 * list rather than an error.
 */
export function fetchInstallSteps(
  runtime: string,
): Promise<InstallStepsResponse> {
  return get<InstallStepsResponse>("/onboarding/install-steps", { runtime });
}

// ── Wizard: blueprints + answers + complete ────────────────────────────────

/**
 * One agent row in a blueprint roster. Mirrors `blueprintAgentSummary` in
 * internal/onboarding/handlers.go. `built_in` marks the blueprint's lead
 * agent — the wizard must keep it checked because the broker refuses to seed
 * an office without its lead.
 */
export interface BlueprintAgentSummary {
  slug: string;
  name: string;
  role?: string;
  emoji?: string;
  /** Whether the agent is checked (kept) by default. */
  checked: boolean;
  /** The lead agent; cannot be unchecked. */
  built_in?: boolean;
}

/**
 * One blueprint (starter roster) returned by GET /onboarding/blueprints.
 * Mirrors the additive `blueprintSummary` Go shape; the wizard only consumes
 * the id/name/description/emoji/agents subset, so every other field is
 * optional and ignored here.
 */
export interface BlueprintSummary {
  id: string;
  name: string;
  description?: string;
  emoji?: string;
  outcome?: string;
  category?: string;
  agents?: BlueprintAgentSummary[];
}

interface BlueprintsResponse {
  templates: BlueprintSummary[];
}

/**
 * Fetch the selectable starter blueprints. GETs /onboarding/blueprints.
 * The Go handler always returns `{ templates: [...] }` (possibly empty when
 * the loader finds no repo and no embedded fallbacks); callers should treat an
 * empty list as "scratch only".
 */
export async function fetchBlueprints(): Promise<BlueprintSummary[]> {
  const resp = await get<BlueprintsResponse>("/onboarding/blueprints");
  return Array.isArray(resp?.templates) ? resp.templates : [];
}

/**
 * Persist a single staged form-answer field. POSTs /onboarding/answer.
 * The broker sanitizes string values and writes them into FormAnswers
 * (mirroring company_name into the top-level CompanyName). Mirrors the
 * `field`/`value` body the Go HandleAnswer expects.
 */
export function postOnboardingAnswer(
  field: string,
  value: string | string[] | boolean,
): Promise<{ status: string }> {
  return post<{ status: string }>("/onboarding/answer", { field, value });
}

/**
 * Persist partial-progress answers for a wizard step. POSTs
 * /onboarding/progress. The broker merges `answers` into Partial.Answers[step].
 *
 * This is the channel that lands the company name where HandleComplete reads it
 * back at complete time: HandleComplete derives the seed companyName from
 * Partial.Answers["identity"]["company_name"] (see onboardingPartialCompanyName
 * in internal/onboarding/handlers.go), NOT from FormAnswers. The wizard writes
 * the office name via step "identity" so the office seed picks it up.
 */
export function postOnboardingProgress(
  step: string,
  answers: Record<string, string>,
): Promise<{ status: string }> {
  return post<{ status: string }>("/onboarding/progress", { step, answers });
}

/**
 * Request body for POST /onboarding/complete. Mirrors the Go HandleComplete
 * struct. `blueprint` is empty for the scratch path; `agents` filters the
 * blueprint roster (empty array = user unchecked everyone but the lead, which
 * the broker still keeps).
 */
export interface CompleteOnboardingBody {
  task: string;
  skip_task: boolean;
  blueprint: string;
  agents: string[];
  owner_name?: string;
  owner_role?: string;
}

/**
 * Response of POST /onboarding/complete. The broker returns `{ ok, redirect }`
 * on a fresh complete or `{ already_completed, redirect }` when the user was
 * already onboarded (idempotent). Both are success cases for the wizard.
 */
export interface CompleteOnboardingResult {
  ok?: boolean;
  already_completed?: boolean;
  redirect?: string;
}

/**
 * Finish onboarding: seeds the team from the picked blueprint (or scratch),
 * posts the first CEO turn, and flips onboarded=true. POSTs
 * /onboarding/complete. The company name is NOT in this body — the broker
 * reads it from the persisted Partial state, so callers must persist it via
 * postOnboardingAnswer("company_name", ...) BEFORE calling this.
 */
export function completeOnboarding(
  body: CompleteOnboardingBody,
): Promise<CompleteOnboardingResult> {
  return post<CompleteOnboardingResult>("/onboarding/complete", body);
}
