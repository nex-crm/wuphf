import type { CSSProperties, FormEvent } from "react";
import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getOfficeMembers,
  type OfficeMember,
  patchSchedulerJob,
  type SchedulerJob,
} from "../../../api/client";
import { RoutineChannelSelect } from "./RoutineChannelSelect";
import {
  compileSchedule,
  parseSchedule,
  ScheduleBuilder,
  type ScheduleValue,
} from "./ScheduleBuilder";

interface RoutineEditPanelProps {
  routine: SchedulerJob;
  onCancel: () => void;
  onSaved: () => void;
}

/**
 * Inline editor for an existing routine. Lives inside the detail page's
 * Overview tab. PATCHes the broker on save, which snapshots a revision
 * and logs an "edited" activity event under the hood.
 */
export function RoutineEditPanel({
  routine,
  onCancel,
  onSaved,
}: RoutineEditPanelProps) {
  const slug = routine.slug ?? routine.id ?? "";
  const queryClient = useQueryClient();
  const [label, setLabel] = useState(routine.label ?? "");
  const [instructions, setInstructions] = useState(routine.payload ?? "");
  const [ownerSlug, setOwnerSlug] = useState(routine.target_id ?? "");
  const [channel, setChannel] = useState(
    isDirectChannelSlug(routine.channel ?? "") ? "" : routine.channel ?? "",
  );
  const [schedule, setSchedule] = useState<ScheduleValue>(() =>
    parseSchedule({
      schedule_expr: routine.schedule_expr,
      cron: routine.cron,
      interval_minutes: routine.interval_minutes,
      interval_override: routine.interval_override,
    }),
  );
  const [changeNote, setChangeNote] = useState("");

  const membersQuery = useQuery({
    queryKey: ["office-members-for-composer"],
    queryFn: () => getOfficeMembers(),
  });
  const ownerCandidates = useMemo<OfficeMember[]>(
    () => filterOwnerCandidates(membersQuery.data?.members ?? []),
    [membersQuery.data],
  );

  // If the current owner isn't in the candidate list (e.g. it was a
  // legacy slug), keep the value but inject a placeholder option so the
  // dropdown still reflects what's persisted instead of silently
  // changing the routine's owner the moment the user opens the editor.
  const allOwnerOptions = useMemo<OfficeMember[]>(() => {
    if (!ownerSlug) return ownerCandidates;
    if (ownerCandidates.some((m) => m.slug === ownerSlug))
      return ownerCandidates;
    return [
      { slug: ownerSlug, name: ownerSlug, role: "current" } as OfficeMember,
      ...ownerCandidates,
    ];
  }, [ownerCandidates, ownerSlug]);

  // Seed the owner once members load if it's blank (legacy routine).
  useEffect(() => {
    if (ownerSlug || ownerCandidates.length === 0) return;
    const ceo = ownerCandidates.find((m) => m.slug === "ceo");
    setOwnerSlug((ceo ?? ownerCandidates[0]).slug);
  }, [ownerCandidates, ownerSlug]);

  const mutation = useMutation({
    mutationFn: () => {
      const compiled = compileSchedule(schedule);
      const body: Parameters<typeof patchSchedulerJob>[1] = {
        label: label.trim(),
        payload: instructions,
        target_type: "agent",
        target_id: ownerSlug,
        // Empty string means "owner DM" — the dispatcher synthesises
        // the DM channel on each fire. Anything else is a literal
        // channel slug the user picked from the dropdown.
        channel: channel,
        change_note: changeNote.trim() || undefined,
      };
      if (compiled.schedule_expr !== undefined) {
        body.schedule_expr = compiled.schedule_expr;
        // Clear interval_minutes on the wire so a cron edit doesn't leave
        // a stale per-minute cadence ticking alongside the cron.
        body.interval_minutes = 0;
      } else if (compiled.interval_minutes !== undefined) {
        body.interval_minutes = compiled.interval_minutes;
        body.schedule_expr = "";
      }
      return patchSchedulerJob(slug, body);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["scheduler"] });
      void queryClient.invalidateQueries({
        queryKey: ["scheduler-activity", slug],
      });
      void queryClient.invalidateQueries({
        queryKey: ["scheduler-revisions", slug],
      });
      onSaved();
    },
  });

  function onSubmit(e: FormEvent<HTMLFormElement>): void {
    e.preventDefault();
    if (!(label.trim() && ownerSlug)) return;
    mutation.mutate();
  }

  const errorMessage =
    mutation.error instanceof Error ? mutation.error.message : null;

  return (
    <form
      onSubmit={onSubmit}
      data-testid="routine-edit-panel"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-5)",
        padding: "var(--space-4)",
        border: "1px solid var(--accent)",
        borderRadius: "var(--radius-sm)",
        background: "var(--bg-card)",
      }}
    >
      <Field label="Title">
        <input
          type="text"
          className="input"
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          data-testid="edit-label"
          required={true}
        />
      </Field>

      <Field
        label="Owner"
        hint="The agent that runs this routine when it fires."
      >
        <select
          className="input"
          value={ownerSlug}
          onChange={(e) => setOwnerSlug(e.target.value)}
          data-testid="edit-owner"
          required={true}
          disabled={membersQuery.isLoading}
        >
          {membersQuery.isLoading && <option value="">Loading…</option>}
          {!membersQuery.isLoading && allOwnerOptions.length === 0 && (
            <option value="">No agents available</option>
          )}
          {allOwnerOptions.map((m) => (
            <option key={m.slug} value={m.slug}>
              {ownerLabel(m)}
            </option>
          ))}
        </select>
      </Field>

      <Field
        label="Run in"
        hint="Where the routine posts when it fires. Defaults to the owner's DM."
      >
        <RoutineChannelSelect
          value={channel}
          onChange={setChannel}
          ownerSlug={ownerSlug}
          testId="edit-channel"
        />
      </Field>

      <Field label="Schedule">
        <ScheduleBuilder value={schedule} onChange={setSchedule} />
      </Field>

      <Field label="Instructions" hint="What the agent should do on each fire.">
        <textarea
          className="input"
          rows={5}
          value={instructions}
          onChange={(e) => setInstructions(e.target.value)}
          data-testid="edit-instructions"
          style={{ resize: "vertical", minHeight: 96 }}
        />
      </Field>

      <Field
        label="Change note"
        hint="Optional — appears next to the new revision in the Revisions tab."
      >
        <input
          type="text"
          className="input"
          value={changeNote}
          onChange={(e) => setChangeNote(e.target.value)}
          placeholder="e.g. Move daily digest from 9am to 10am"
          data-testid="edit-change-note"
        />
      </Field>

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
          borderTop: "1px solid var(--border-light)",
        }}
      >
        <button
          type="button"
          onClick={onCancel}
          style={buttonStyle}
          disabled={mutation.isPending}
        >
          Cancel
        </button>
        <button
          type="submit"
          data-testid="edit-save"
          style={{
            ...buttonStyle,
            background: "var(--accent)",
            color: "white",
            borderColor: "var(--accent)",
          }}
          disabled={mutation.isPending || !label.trim() || !ownerSlug}
        >
          {mutation.isPending ? "Saving…" : "Save changes"}
        </button>
      </div>
    </form>
  );
}

function isDirectChannelSlug(slug: string): boolean {
  // Mirrors the Go-side `isDirectChannelSlug` so the editor doesn't
  // pre-fill the channel dropdown with a stale DM slug — DMs are the
  // dropdown's implicit default option.
  const s = slug.trim();
  if (!s) return false;
  if (s.startsWith("dm-")) return true;
  if (s.includes("__")) return true;
  return false;
}

function filterOwnerCandidates(members: OfficeMember[]): OfficeMember[] {
  return members.filter((m) => {
    const slug = m.slug?.toLowerCase() ?? "";
    if (slug === "" || slug === "human" || slug === "you") return false;
    return true;
  });
}

function ownerLabel(m: OfficeMember): string {
  const name = m.name?.trim() || m.slug;
  const role = m.role?.trim();
  return role ? `${name} · ${role}` : name;
}

interface FieldProps {
  label: string;
  hint?: string;
  children: React.ReactNode;
}

function Field({ label, hint, children }: FieldProps) {
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
