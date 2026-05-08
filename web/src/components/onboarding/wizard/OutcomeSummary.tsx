// OutcomeSummary renders the post-completion screen shown after
// /onboarding/complete succeeds. It synthesises a human-readable
// account of what was actually created from the wizard's local state
// (the same values that were submitted) rather than from the backend
// response, which only returns {"ok": true}. This is correct: whatever
// the user confirmed in the wizard is what the broker seeded.
import type { BlueprintAgent, BlueprintTemplate } from "./types";

// ── Types ────────────────────────────────────────────────────────────────

export interface OutcomeSummaryProps {
  /** Agents that were checked when the user hit "Get started". */
  agents: BlueprintAgent[];
  /** The blueprint that was selected, or null for "start from scratch". */
  selectedBlueprint: string | null;
  /** Full blueprint list so we can resolve the name from the id. */
  blueprints: BlueprintTemplate[];
  /** Primary runtime label, e.g. "Claude Code". Empty when none selected. */
  primaryRuntime: string;
  /** The task text the user submitted. Empty when the task was skipped. */
  taskText: string;
  /** True when the user pressed "Get started" with an empty task. */
  taskSkipped: boolean;
  /** Number of API keys that were configured. */
  apiKeyCount: number;
  /** Called when the user navigates into the live office. */
  onEnter: () => void;
}

// ── Helpers ───────────────────────────────────────────────────────────────

// The broker always creates a #general channel. Blueprints may describe
// additional channels, but the broker's channel-seed logic is internal and
// not surfaced through the complete response — we conservatively show
// #general as the guaranteed channel and let users discover the rest.
const GUARANTEED_CHANNELS = ["general"];

function blueprintName(
  selectedBlueprint: string | null,
  blueprints: BlueprintTemplate[],
): string {
  if (selectedBlueprint === null) return "Start from scratch";
  const bp = blueprints.find((b) => b.id === selectedBlueprint);
  return bp?.name ?? selectedBlueprint;
}

function checkedAgents(agents: BlueprintAgent[]): BlueprintAgent[] {
  return agents.filter((a) => a.checked !== false);
}

// ── Component ─────────────────────────────────────────────────────────────

export function OutcomeSummary({
  agents,
  selectedBlueprint,
  blueprints,
  primaryRuntime,
  taskText,
  taskSkipped,
  apiKeyCount,
  onEnter,
}: OutcomeSummaryProps) {
  const created = checkedAgents(agents);
  const blueprint = blueprintName(selectedBlueprint, blueprints);
  const trimmedTask = taskText.trim();

  return (
    <div className="wizard-step" data-testid="outcome-summary">
      <div className="wizard-hero">
        <h1
          className="wizard-headline"
          style={{ fontSize: 28 }}
          data-testid="outcome-headline"
        >
          Office is open
        </h1>
        <p className="wizard-subhead">
          Here is what was created. Your team is already running.
        </p>
      </div>

      {/* Agents */}
      <div className="wizard-panel">
        <p className="wizard-panel-title">
          Team created ({created.length}{" "}
          {created.length === 1 ? "agent" : "agents"})
        </p>
        {created.length > 0 ? (
          <ul
            className="outcome-agent-list"
            data-testid="outcome-agents"
            style={{ listStyle: "none", margin: 0, padding: 0 }}
          >
            {created.map((a) => (
              <li
                key={a.slug}
                style={{
                  display: "flex",
                  alignItems: "baseline",
                  gap: 10,
                  padding: "8px 0",
                  borderBottom: "1px solid var(--border-light, var(--border))",
                  fontSize: 13,
                }}
              >
                {a.emoji ? (
                  <span style={{ fontSize: 16 }}>{a.emoji}</span>
                ) : null}
                <span style={{ fontWeight: 600, color: "var(--text)" }}>
                  {a.name}
                </span>
                <span
                  style={{
                    fontFamily: "var(--font-mono, monospace)",
                    fontSize: 11,
                    color: "var(--text-tertiary)",
                  }}
                >
                  @{a.slug}
                </span>
                {a.built_in ? (
                  <span
                    className="wiz-team-lead-badge"
                    style={{ marginLeft: "auto" }}
                  >
                    lead
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
        ) : (
          <p
            style={{ fontSize: 13, color: "var(--text-secondary)", margin: 0 }}
          >
            No agents selected — the broker seeds a default team on first run.
          </p>
        )}
      </div>

      {/* What was set up */}
      <div className="wizard-panel">
        <p className="wizard-panel-title">Setup</p>
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <OutcomeRow
            label="Blueprint"
            value={blueprint}
            testId="outcome-blueprint"
          />
          <OutcomeRow
            label="Runtime"
            value={primaryRuntime || "Not selected"}
            testId="outcome-runtime"
          />
          <OutcomeRow
            label="Channels"
            value={GUARANTEED_CHANNELS.map((c) => `#${c}`).join(", ")}
            testId="outcome-channels"
          />
          <OutcomeRow
            label="Wiki"
            value="Git-native team wiki seeded in ~/.wuphf/wiki"
            testId="outcome-wiki"
          />
          {apiKeyCount > 0 ? (
            <OutcomeRow
              label="Provider keys"
              value={`${apiKeyCount} key${apiKeyCount === 1 ? "" : "s"} saved`}
              testId="outcome-api-keys"
            />
          ) : null}
          <OutcomeRow
            label="First task"
            value={
              taskSkipped || trimmedTask.length === 0
                ? "Skipped — start from the general channel"
                : trimmedTask
            }
            testId="outcome-task"
            truncate={!taskSkipped && trimmedTask.length > 0}
          />
        </div>
      </div>

      {/* Next actions */}
      <div className="wizard-panel">
        <p className="wizard-panel-title">Where to go next</p>
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          <OutcomeLink
            href="#/channels/general"
            label="Open general channel"
            hint="Where agents post updates and you can give tasks"
            testId="outcome-link-general"
          />
          <OutcomeLink
            href="#/tasks"
            label="Live run view"
            hint="Watch agents execute and see task status"
            testId="outcome-link-tasks"
          />
          <OutcomeLink
            href="#/wiki"
            label="Team wiki"
            hint="Agents write here; you can read, edit, and promote entries"
            testId="outcome-link-wiki"
          />
          <OutcomeLink
            href="#/apps/settings"
            label="Provider settings"
            hint="Add or rotate API keys and change the runtime"
            testId="outcome-link-settings"
          />
        </div>
      </div>

      <div className="wizard-nav">
        <div className="wizard-nav-right" style={{ width: "100%" }}>
          <button
            className="btn btn-primary"
            style={{ width: "100%" }}
            data-testid="outcome-enter-button"
            type="button"
            onClick={onEnter}
          >
            Go to the office
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Sub-components ────────────────────────────────────────────────────────

interface OutcomeRowProps {
  label: string;
  value: string;
  testId?: string;
  /** Clamp long values to two lines so the panel stays compact. */
  truncate?: boolean;
}

function OutcomeRow({ label, value, testId, truncate }: OutcomeRowProps) {
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "120px 1fr",
        gap: 12,
        alignItems: "baseline",
        fontSize: 13,
      }}
      data-testid={testId}
    >
      <span style={{ fontWeight: 600, color: "var(--text-secondary)" }}>
        {label}
      </span>
      <span
        style={{
          color: "var(--text)",
          ...(truncate
            ? {
                display: "-webkit-box",
                WebkitLineClamp: 2,
                WebkitBoxOrient: "vertical",
                overflow: "hidden",
              }
            : {}),
        }}
      >
        {value}
      </span>
    </div>
  );
}

interface OutcomeLinkProps {
  href: string;
  label: string;
  hint: string;
  testId?: string;
}

function OutcomeLink({ href, label, hint, testId }: OutcomeLinkProps) {
  return (
    <a
      href={href}
      data-testid={testId}
      style={{
        display: "flex",
        alignItems: "baseline",
        gap: 10,
        padding: "10px 12px",
        border: "1px solid var(--border)",
        borderRadius: "var(--radius-md, 6px)",
        background: "var(--bg-card)",
        textDecoration: "none",
        color: "inherit",
        transition: "border-color 0.15s, background 0.15s",
        fontSize: 13,
      }}
      onMouseEnter={(e) => {
        (e.currentTarget as HTMLAnchorElement).style.borderColor =
          "var(--accent)";
        (e.currentTarget as HTMLAnchorElement).style.background =
          "var(--accent-bg)";
      }}
      onMouseLeave={(e) => {
        (e.currentTarget as HTMLAnchorElement).style.borderColor =
          "var(--border)";
        (e.currentTarget as HTMLAnchorElement).style.background =
          "var(--bg-card)";
      }}
    >
      <span
        style={{ fontWeight: 600, color: "var(--accent)", flex: "0 0 auto" }}
      >
        {label}
      </span>
      <span style={{ color: "var(--text-secondary)", fontSize: 12 }}>
        {hint}
      </span>
    </a>
  );
}
