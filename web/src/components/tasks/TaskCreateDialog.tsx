import {
  type KeyboardEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { FastArrowDown, Hashtag, KeyframeSolid, User } from "iconoir-react";

import { getConfig } from "../../api/client";
import { useChannels } from "../../hooks/useChannels";
import { useCreateTask } from "../../hooks/useCreateTask";
import { useOfficeMembers } from "../../hooks/useMembers";
import { router } from "../../lib/router";
import { resolveLeadSlug } from "../../lib/slashCommands";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "../ui/Dialog";
import { Kbd, MOD_KEY } from "../ui/Kbd";

const DEFAULT_CHANNEL = "general";
const AUTO_ASSIGN = "";

export interface TaskCreateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Pre-select a channel — used when opened from a channel header. */
  defaultChannel?: string;
  /** Pre-select an assignee — used when opened from an agent subspace. */
  defaultAssignee?: string;
  /** Navigate to the new task's detail page on success. Default: true. */
  navigateOnCreate?: boolean;
}

/**
 * TaskCreateDialog — Linear-shaped new-task modal.
 *
 * Built on project design tokens (--bg, --bg-card, --text, --border,
 * --accent) via named CSS classes in lifecycle.css so it renders
 * correctly across all themes without depending on the shadcn HSL bridge.
 * Uses the shared Dialog, Kbd primitives.
 */
export function TaskCreateDialog({
  open,
  onOpenChange,
  defaultChannel,
  defaultAssignee,
  navigateOnCreate = true,
}: TaskCreateDialogProps) {
  const titleId = useId();
  const detailsId = useId();
  const channelId = useId();
  const assigneeId = useId();

  const [title, setTitle] = useState("");
  const [details, setDetails] = useState("");
  const [channel, setChannel] = useState(defaultChannel ?? DEFAULT_CHANNEL);
  const [assignee, setAssignee] = useState(defaultAssignee ?? AUTO_ASSIGN);
  const [createMore, setCreateMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const titleRef = useRef<HTMLInputElement | null>(null);

  const channelsQuery = useChannels();
  const membersQuery = useOfficeMembers();
  const createTask = useCreateTask();
  const { data: cfg } = useQuery({
    queryKey: ["config"],
    queryFn: getConfig,
    staleTime: 60_000,
  });

  const channels = channelsQuery.data ?? [];
  const members = membersQuery.data ?? [];
  const leadSlug = useMemo(
    () => resolveLeadSlug(cfg?.team_lead_slug, members),
    [cfg?.team_lead_slug, members],
  );

  useEffect(() => {
    if (open) {
      setTitle("");
      setDetails("");
      setChannel(defaultChannel ?? DEFAULT_CHANNEL);
      setAssignee(defaultAssignee ?? AUTO_ASSIGN);
      setError(null);
      queueMicrotask(() => titleRef.current?.focus());
    }
  }, [open, defaultChannel, defaultAssignee]);

  const canSubmit = title.trim().length > 0 && !createTask.isPending;

  const assigneeLabel = useMemo(() => {
    if (!assignee) {
      const lead = members.find((m) => m.slug === leadSlug);
      return lead ? `Auto-assign (via @${lead.slug})` : "Auto-assign";
    }
    const member = members.find((m) => m.slug === assignee);
    return member ? member.name : assignee;
  }, [assignee, members, leadSlug]);

  const channelLabel = useMemo(() => {
    const c = channels.find((entry) => entry.slug === channel);
    return c ? c.slug : channel;
  }, [channel, channels]);

  const handleSubmit = useCallback(async () => {
    if (!canSubmit) return;
    setError(null);
    try {
      const result = await createTask.mutateAsync({
        title,
        details,
        channel,
        // Empty selection routes to the office lead so the CEO's scoping
        // interview fires and picks a specialist owner.
        assignee: assignee || leadSlug || undefined,
      });
      if (createMore) {
        setTitle("");
        setDetails("");
        queueMicrotask(() => titleRef.current?.focus());
      } else {
        onOpenChange(false);
        if (navigateOnCreate && result.task?.id) {
          void router.navigate({
            to: "/tasks/$taskId",
            params: { taskId: result.task.id },
          });
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not file task.");
    }
  }, [
    canSubmit,
    createTask,
    title,
    details,
    channel,
    assignee,
    leadSlug,
    createMore,
    onOpenChange,
    navigateOnCreate,
  ]);

  const handleKeyDown = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
        event.preventDefault();
        void handleSubmit();
      }
    },
    [handleSubmit],
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        // `.issue-create-dialog` styles in lifecycle.css are loaded after
        // tailwind utilities so they override the DialogContent defaults
        // (bg/border/padding/radius/gap/display) while the primitive's
        // positioning + animation utility classes still apply.
        className="issue-create-dialog"
        onKeyDown={handleKeyDown}
        aria-describedby={undefined}
      >
        <DialogTitle className="sr-only">Create a new task</DialogTitle>
        <DialogDescription className="sr-only">
          File a new task. Use {MOD_KEY} plus Enter to submit.
        </DialogDescription>

        <div className="issue-create-eyebrow">
          <span className="issue-create-eyebrow-icon">
            <KeyframeSolid width={14} height={14} aria-hidden="true" />
          </span>
          <span className="issue-create-eyebrow-label">New task</span>
          <span className="issue-create-eyebrow-sep" aria-hidden="true">
            ·
          </span>
          <span>#{channelLabel}</span>
          {defaultAssignee ? (
            <>
              <span className="issue-create-eyebrow-sep" aria-hidden="true">
                ·
              </span>
              <span>for @{defaultAssignee}</span>
            </>
          ) : null}
        </div>

        <div className="issue-create-body">
          <label htmlFor={titleId} className="sr-only">
            Task title
          </label>
          <input
            ref={titleRef}
            id={titleId}
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Task title"
            className="issue-create-title-input"
            data-testid="issue-create-title"
            autoComplete="off"
            required={true}
          />

          <label htmlFor={detailsId} className="sr-only">
            Task details
          </label>
          <textarea
            id={detailsId}
            value={details}
            onChange={(e) => setDetails(e.target.value)}
            placeholder="Add description…"
            rows={4}
            className="issue-create-description"
            data-testid="issue-create-details"
          />
        </div>

        <div className="issue-create-chips">
          <PropertyChip
            icon={<Hashtag width={14} height={14} aria-hidden="true" />}
            label={channelLabel}
            htmlFor={channelId}
            disabled={channelsQuery.isPending}
          >
            <select
              id={channelId}
              value={channel}
              onChange={(e) => setChannel(e.target.value)}
              className="issue-create-chip-select"
              data-testid="issue-create-channel"
              aria-label="Channel"
            >
              {!channels.some((c) => c.slug === channel) ? (
                <option value={channel}>#{channel}</option>
              ) : null}
              {channels.map((c) => (
                <option key={c.slug} value={c.slug}>
                  #{c.slug}
                </option>
              ))}
            </select>
          </PropertyChip>

          <PropertyChip
            icon={<User width={14} height={14} aria-hidden="true" />}
            label={assigneeLabel}
            htmlFor={assigneeId}
            disabled={membersQuery.isPending}
            muted={!assignee}
          >
            <select
              id={assigneeId}
              value={assignee}
              onChange={(e) => setAssignee(e.target.value)}
              className="issue-create-chip-select"
              data-testid="issue-create-assignee"
              aria-label="Assignee"
            >
              <option value={AUTO_ASSIGN}>Auto-assign</option>
              {members.map((m) => (
                <option key={m.slug} value={m.slug}>
                  {m.name}
                </option>
              ))}
            </select>
          </PropertyChip>
        </div>

        {error ? (
          <p
            role="alert"
            className="issue-create-error"
            data-testid="issue-create-error"
          >
            {error}
          </p>
        ) : null}

        <div className="issue-create-footer">
          <label className="issue-create-more">
            <input
              type="checkbox"
              checked={createMore}
              onChange={(e) => setCreateMore(e.target.checked)}
              className="issue-create-more-checkbox"
              data-testid="issue-create-more"
            />
            <span>Create more</span>
          </label>
          <div className="issue-create-actions">
            <button
              type="button"
              className="issue-create-cancel"
              onClick={() => onOpenChange(false)}
              disabled={createTask.isPending}
            >
              Cancel
            </button>
            <button
              type="button"
              className="issue-create-submit"
              onClick={() => void handleSubmit()}
              disabled={!canSubmit}
              data-testid="issue-create-submit"
            >
              <span>{createTask.isPending ? "Creating…" : "Create task"}</span>
              <Kbd size="sm" variant="inverse">{`${MOD_KEY}⏎`}</Kbd>
            </button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

interface PropertyChipProps {
  icon: ReactNode;
  label: string;
  htmlFor: string;
  disabled?: boolean;
  /** Renders the chip label muted (used for "Auto-assign" default state). */
  muted?: boolean;
  children: ReactNode;
}

/**
 * Borderless chip — icon + label + chevron, hover-fill. The transparent
 * native <select> sits on top so we get the OS picker until Popover lands.
 */
function PropertyChip({
  icon,
  label,
  htmlFor,
  disabled,
  muted,
  children,
}: PropertyChipProps) {
  const cls =
    `issue-create-chip${muted ? " issue-create-chip--muted" : ""}`.trim();
  return (
    <label
      htmlFor={htmlFor}
      className={cls}
      aria-disabled={disabled ? "true" : undefined}
    >
      <span className="issue-create-chip-icon">{icon}</span>
      <span>{label}</span>
      <FastArrowDown
        className="issue-create-chip-chevron"
        width={12}
        height={12}
        aria-hidden="true"
      />
      {children}
    </label>
  );
}
