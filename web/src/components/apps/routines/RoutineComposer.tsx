import type { CSSProperties, FormEvent } from "react";
import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  createSchedulerJob,
  getOfficeMembers,
  type OfficeMember,
} from "../../../api/client";
import { router } from "../../../lib/router";
import { RoutineChannelSelect } from "./RoutineChannelSelect";
import {
  compileSchedule,
  DEFAULT_SCHEDULE,
  ScheduleBuilder,
  type ScheduleValue,
} from "./ScheduleBuilder";

/**
 * Full-page composer for creating a routine. Every routine has an
 * owning agent (picked from office members) and a schedule built in
 * plain English — never raw cron.
 */
export function RoutineComposer() {
  const queryClient = useQueryClient();
  const [label, setLabel] = useState("");
  const [slug, setSlug] = useState("");
  const [schedule, setSchedule] = useState<ScheduleValue>(DEFAULT_SCHEDULE);
  const [instructions, setInstructions] = useState("");
  const [ownerSlug, setOwnerSlug] = useState<string>("");
  const [channel, setChannel] = useState<string>("");
  const [enabled, setEnabled] = useState(true);

  const membersQuery = useQuery({
    queryKey: ["office-members-for-composer"],
    queryFn: () => getOfficeMembers(),
  });
  const ownerCandidates = useMemo<OfficeMember[]>(
    () => filterOwnerCandidates(membersQuery.data?.members ?? []),
    [membersQuery.data],
  );

  // Seed the owner once members load. Prefer CEO if present; otherwise
  // pick the first eligible agent so the user never sees an empty
  // dropdown next to a required field.
  useEffect(() => {
    if (ownerSlug || ownerCandidates.length === 0) return;
    const ceo = ownerCandidates.find((m) => m.slug === "ceo");
    setOwnerSlug((ceo ?? ownerCandidates[0]).slug);
  }, [ownerCandidates, ownerSlug]);

  const derivedSlug = useMemo(() => slugFromLabel(label), [label]);
  const effectiveSlug = slug.trim() || derivedSlug;

  const mutation = useMutation({
    mutationFn: () => {
      const compiled = compileSchedule(schedule);
      return createSchedulerJob({
        label: label.trim(),
        slug: slug.trim() || undefined,
        payload: instructions.trim() || undefined,
        target_type: "agent",
        target_id: ownerSlug,
        channel: channel || undefined,
        enabled,
        ...compiled,
      });
    },
    onSuccess: (data) => {
      void queryClient.invalidateQueries({ queryKey: ["scheduler"] });
      const created = data.job;
      const next = created.slug ?? created.id ?? effectiveSlug ?? "";
      if (next) {
        void router.navigate({
          to: "/routines/$routineSlug",
          params: { routineSlug: next },
        });
      } else {
        void router.navigate({
          to: "/apps/$appId",
          params: { appId: "routines" },
        });
      }
    },
  });

  function onSubmit(e: FormEvent<HTMLFormElement>): void {
    e.preventDefault();
    if (!(label.trim() && ownerSlug)) return;
    mutation.mutate();
  }

  function goBack(): void {
    void router.navigate({ to: "/apps/$appId", params: { appId: "routines" } });
  }

  const errorMessage =
    mutation.error instanceof Error ? mutation.error.message : null;
  const noOwners = !membersQuery.isLoading && ownerCandidates.length === 0;

  return (
    <div
      data-testid="routine-composer"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-5)",
        padding: "var(--space-5) var(--space-6)",
        maxWidth: 720,
        margin: "0 auto",
        width: "100%",
      }}
    >
      <button type="button" onClick={goBack} style={backLinkStyle}>
        ← All routines
      </button>

      <header
        style={{
          display: "flex",
          flexDirection: "column",
          gap: 4,
          paddingBottom: "var(--space-4)",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <span style={eyebrowStyle}>New routine</span>
        <h1 style={titleStyle}>Create a scheduled routine</h1>
        <p
          style={{
            margin: 0,
            color: "var(--text-tertiary)",
            fontSize: "var(--text-sm)",
          }}
        >
          Routines fire on a schedule and run as a specific agent. Webhook and
          context-change triggers are coming soon.
        </p>
      </header>

      <form
        onSubmit={onSubmit}
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "var(--space-5)",
        }}
      >
        <FormField label="Title" hint="What this routine should do.">
          <input
            type="text"
            className="input"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="Daily standup digest"
            data-testid="composer-label"
            required={true}
          />
        </FormField>

        <FormField
          label="Slug"
          hint={
            slug.trim()
              ? "URL identifier — must be unique within the office."
              : `Auto-derived from the title: ${derivedSlug || "(empty)"}`
          }
        >
          <input
            type="text"
            className="input"
            value={slug}
            onChange={(e) => setSlug(e.target.value)}
            placeholder={derivedSlug || "auto"}
            data-testid="composer-slug"
            style={{ fontFamily: "var(--font-mono)" }}
          />
        </FormField>

        <FormField
          label="Owner"
          hint="The agent that runs this routine when it fires."
        >
          <OwnerSelect
            value={ownerSlug}
            onChange={setOwnerSlug}
            members={ownerCandidates}
            loading={membersQuery.isLoading}
          />
          {noOwners && (
            <span
              style={{
                fontSize: "var(--text-xs)",
                color: "var(--red)",
                marginTop: 4,
              }}
            >
              No agents available. Add a teammate before creating a routine.
            </span>
          )}
        </FormField>

        <FormField
          label="Run in"
          hint="Where the routine posts when it fires. Defaults to the owner's DM."
        >
          <RoutineChannelSelect
            value={channel}
            onChange={setChannel}
            ownerSlug={ownerSlug}
            testId="composer-channel"
          />
        </FormField>

        <FormField label="Schedule" hint="When this routine should fire.">
          <ScheduleBuilder value={schedule} onChange={setSchedule} />
        </FormField>

        <FormField
          label="Instructions"
          hint="What the agent should do on each fire."
        >
          <textarea
            className="input"
            rows={5}
            value={instructions}
            onChange={(e) => setInstructions(e.target.value)}
            placeholder="Summarize yesterday's #engineering channel and post the top three threads to #standup."
            data-testid="composer-instructions"
            style={{ resize: "vertical", minHeight: 96 }}
          />
        </FormField>

        <FormField
          label="State"
          hint="Routine can be started paused and enabled later."
        >
          <label
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: "var(--space-2)",
              fontSize: "var(--text-sm)",
              cursor: "pointer",
            }}
          >
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              data-testid="composer-enabled"
            />
            Enable immediately
          </label>
        </FormField>

        {errorMessage && (
          <div
            role="alert"
            style={{
              padding: "var(--space-3)",
              background: "var(--red-bg)",
              border: "1px solid var(--red)",
              color: "var(--red)",
              borderRadius: "var(--radius-sm)",
              fontSize: "var(--text-sm)",
            }}
          >
            {errorMessage}
          </div>
        )}

        <div
          style={{
            display: "flex",
            justifyContent: "flex-end",
            gap: "var(--space-2)",
            paddingTop: "var(--space-3)",
            borderTop: "1px solid var(--border)",
          }}
        >
          <button
            type="button"
            onClick={goBack}
            style={buttonStyle}
            disabled={mutation.isPending}
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="composer-submit"
            style={{
              ...buttonStyle,
              background: "var(--accent)",
              color: "white",
              borderColor: "var(--accent)",
            }}
            disabled={mutation.isPending || !label.trim() || !ownerSlug}
          >
            {mutation.isPending ? "Creating…" : "Create routine"}
          </button>
        </div>
      </form>
    </div>
  );
}

// ── Helpers + small components ────────────────────────────────────

interface OwnerSelectProps {
  value: string;
  onChange: (slug: string) => void;
  members: OfficeMember[];
  loading: boolean;
}

function OwnerSelect({ value, onChange, members, loading }: OwnerSelectProps) {
  if (loading) {
    return (
      <select className="input" disabled={true} value="" onChange={() => {}}>
        <option value="">Loading agents…</option>
      </select>
    );
  }
  return (
    <select
      className="input"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      data-testid="composer-owner"
      required={true}
    >
      {members.length === 0 && <option value="">No agents available</option>}
      {members.map((m) => (
        <option key={m.slug} value={m.slug}>
          {ownerLabel(m)}
        </option>
      ))}
    </select>
  );
}

function ownerLabel(m: OfficeMember): string {
  const name = m.name?.trim() || m.slug;
  const role = m.role?.trim();
  return role ? `${name} · ${role}` : name;
}

/**
 * Filter office members down to viable routine owners. Drops the human
 * seat (you can't assign a routine to yourself) and any built-in slots
 * that aren't agents.
 */
function filterOwnerCandidates(members: OfficeMember[]): OfficeMember[] {
  return members.filter((m) => {
    const slug = m.slug?.toLowerCase() ?? "";
    if (slug === "" || slug === "human" || slug === "you") return false;
    return true;
  });
}

function slugFromLabel(label: string): string {
  return label
    .toLowerCase()
    .replace(/[^a-z0-9\s_-]+/g, "")
    .replace(/[\s_]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
}

interface FormFieldProps {
  label: string;
  hint?: string;
  children: React.ReactNode;
}

function FormField({ label, hint, children }: FormFieldProps) {
  return (
    <label style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <span
        style={{
          fontSize: "var(--text-2xs)",
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.12em",
          color: "var(--text-tertiary)",
          fontFamily: "var(--font-mono)",
        }}
      >
        {label}
      </span>
      {children}
      {hint && (
        <span
          style={{
            fontSize: "var(--text-xs)",
            color: "var(--text-tertiary)",
            marginTop: 2,
          }}
        >
          {hint}
        </span>
      )}
    </label>
  );
}

const buttonStyle: CSSProperties = {
  padding: "5px var(--space-3)",
  fontSize: "var(--text-sm)",
  fontWeight: 500,
  border: "1px solid var(--border)",
  borderRadius: "var(--radius-sm)",
  background: "var(--bg-card)",
  color: "var(--text-secondary)",
  cursor: "pointer",
};

const backLinkStyle: CSSProperties = {
  background: "transparent",
  border: "none",
  padding: 0,
  color: "var(--text-secondary)",
  fontSize: "var(--text-sm)",
  cursor: "pointer",
  alignSelf: "flex-start",
  fontFamily: "var(--font-mono)",
  letterSpacing: "0.04em",
};

const eyebrowStyle: CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: "var(--text-2xs)",
  letterSpacing: "0.16em",
  textTransform: "uppercase",
  color: "var(--text-tertiary)",
};

const titleStyle: CSSProperties = {
  margin: 0,
  fontSize: "var(--text-2xl)",
  fontWeight: 600,
  color: "var(--text)",
  letterSpacing: "-0.015em",
};
