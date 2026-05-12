import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import type { OfficeMember } from "../../api/client";
import { post } from "../../api/client";
import { useChannelMembers, useOfficeMembers } from "../../hooks/useMembers";
import { formatAgentName } from "../../lib/agentName";
import { useAppStore } from "../../stores/app";
import { PixelAvatar } from "../ui/PixelAvatar";
import { showNotice, showUndoToast } from "../ui/Toast";

interface ChannelParticipantsProps {
  channelSlug: string;
}

function isHumanMember(slug: string): boolean {
  return slug === "human" || slug === "you" || slug.startsWith("human:");
}

function memberDisplayName(member: OfficeMember): string {
  const name = member.name?.trim();
  if (name) return name;
  return formatAgentName(member.slug);
}

function memberActivity(member: OfficeMember): string {
  if (member.disabled) return "Disabled in this channel";
  return (
    member.liveActivity?.trim() ||
    member.task?.trim() ||
    member.detail?.trim() ||
    member.role?.trim() ||
    "Idle"
  );
}

function sortParticipants(a: OfficeMember, b: OfficeMember): number {
  if (a.disabled !== b.disabled) return a.disabled ? 1 : -1;
  const aOnline = a.online === true;
  const bOnline = b.online === true;
  if (aOnline !== bOnline) return aOnline ? -1 : 1;
  return memberDisplayName(a).localeCompare(memberDisplayName(b));
}

function mergeMemberProfile(
  member: OfficeMember,
  profile: OfficeMember | undefined,
): OfficeMember {
  if (!profile) return member;
  return {
    ...profile,
    ...member,
    name: member.name || profile.name,
    role: member.role || profile.role,
    provider: member.provider ?? profile.provider,
    built_in: member.built_in ?? profile.built_in,
    online: member.online ?? profile.online,
    last_seen_at: member.last_seen_at ?? profile.last_seen_at,
  };
}

type ChannelMemberAction = "add" | "enable" | "disable" | "remove";

function isLeadMember(member: OfficeMember): boolean {
  return member.built_in === true || member.slug === "ceo";
}

function isActiveMember(member: OfficeMember): boolean {
  return (
    !member.disabled &&
    (member.status === "active" ||
      member.status === "shipping" ||
      member.status === "plotting" ||
      Boolean(member.liveActivity || member.task))
  );
}

function nextToggleAction(member: OfficeMember): ChannelMemberAction {
  return member.disabled ? "enable" : "disable";
}

function toggleLabel(action: ChannelMemberAction, pending: boolean): string {
  if (pending) return "...";
  return action === "enable" ? "Enable" : "Disable";
}

function participantNotice(
  label: string,
  channelSlug: string,
  action: ChannelMemberAction,
): string {
  switch (action) {
    case "add":
      return `${label} added to #${channelSlug}`;
    case "enable":
      return `${label} enabled in #${channelSlug}`;
    case "disable":
      return `${label} disabled in #${channelSlug}`;
    case "remove":
      return `${label} removed from #${channelSlug}`;
    default: {
      const _exhaustive: never = action;
      return _exhaustive;
    }
  }
}

interface AddMenuProps {
  agents: OfficeMember[];
  channelSlug: string;
  pendingAction: string | null;
  onAdd: (member: OfficeMember) => void;
}

function AddParticipantMenu({
  agents,
  channelSlug,
  pendingAction,
  onAdd,
}: AddMenuProps) {
  return (
    <div
      className="channel-participants-add-menu"
      id="channel-participants-add-menu"
    >
      {agents.length === 0 ? (
        <div className="channel-participants-empty">
          All agents are already here
        </div>
      ) : (
        agents.map((member) => {
          const displayName = memberDisplayName(member);
          const pending = pendingAction === `add:${member.slug}`;
          return (
            <button
              key={member.slug}
              type="button"
              className="channel-participant channel-participant-add-option"
              onClick={() => onAdd(member)}
              disabled={pending}
              aria-label={`Add ${displayName} to #${channelSlug}`}
            >
              <span className="channel-participant-avatar">
                <PixelAvatar slug={member.slug} size={24} />
              </span>
              <span className="channel-participant-body">
                <span className="channel-participant-name">{displayName}</span>
                <span className="channel-participant-role">
                  {member.role || "Agent"}
                </span>
              </span>
              <span
                className="channel-participant-add-glyph"
                aria-hidden="true"
              >
                +
              </span>
            </button>
          );
        })
      )}
    </div>
  );
}

interface ParticipantRowProps {
  member: OfficeMember;
  pendingAction: string | null;
  onOpenAgent: (slug: string) => void;
  onUpdate: (member: OfficeMember, action: ChannelMemberAction) => void;
}

function ParticipantRow({
  member,
  pendingAction,
  onOpenAgent,
  onUpdate,
}: ParticipantRowProps) {
  const displayName = memberDisplayName(member);
  const isLead = isLeadMember(member);
  const active = isActiveMember(member);
  const nextAction = nextToggleAction(member);
  const pending = pendingAction === `${nextAction}:${member.slug}`;
  const removePending = pendingAction === `remove:${member.slug}`;

  return (
    <div
      className={`channel-participant${member.disabled ? " is-disabled" : ""}`}
    >
      <button
        type="button"
        className="channel-participant-main"
        onClick={() => onOpenAgent(member.slug)}
        aria-label={`Open agent panel for ${displayName}`}
      >
        <span className="channel-participant-avatar">
          <PixelAvatar slug={member.slug} size={24} />
        </span>
        <span className="channel-participant-body">
          <span className="channel-participant-name">{displayName}</span>
          <span className="channel-participant-role">
            {memberActivity(member)}
          </span>
        </span>
        <span
          className={`channel-participant-presence${active ? " is-active" : ""}`}
          aria-hidden="true"
        />
      </button>
      <span className="channel-participant-controls">
        <button
          type="button"
          className="channel-participant-toggle"
          onClick={() => onUpdate(member, nextAction)}
          disabled={pending || isLead}
          title={
            isLead
              ? "Lead agents stay enabled in every channel"
              : `${nextAction === "enable" ? "Enable" : "Disable"} ${displayName}`
          }
        >
          {toggleLabel(nextAction, pending)}
        </button>
        <button
          type="button"
          className={`channel-participant-remove${removePending ? " is-pending" : ""}`}
          onClick={() => onUpdate(member, "remove")}
          disabled={removePending || isLead}
          aria-label={`Remove ${displayName} from channel`}
          title={
            isLead
              ? "Lead agents stay in every channel"
              : `Remove ${displayName} from this channel`
          }
        >
          {removePending ? "..." : "Remove"}
        </button>
      </span>
    </div>
  );
}

export function ChannelParticipants({ channelSlug }: ChannelParticipantsProps) {
  const queryClient = useQueryClient();
  const [addOpen, setAddOpen] = useState(false);
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const { data: members = [], isLoading } = useChannelMembers(channelSlug);
  const { data: officeMembers = [] } = useOfficeMembers();
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug);
  const profileBySlug = new Map(
    officeMembers.map((member) => [member.slug, member]),
  );
  const channelSlugs = new Set(members.map((member) => member.slug));
  const agents = members
    .filter((member) => member.slug && !isHumanMember(member.slug))
    .map((member) => mergeMemberProfile(member, profileBySlug.get(member.slug)))
    .sort(sortParticipants);
  const availableAgents = officeMembers
    .filter(
      (member) =>
        member.slug &&
        !isHumanMember(member.slug) &&
        !channelSlugs.has(member.slug),
    )
    .sort(sortParticipants);

  async function refreshParticipantQueries() {
    await queryClient.refetchQueries({
      queryKey: ["channel-members", channelSlug],
    });
    await queryClient.invalidateQueries({ queryKey: ["office-members"] });
    await queryClient.invalidateQueries({ queryKey: ["channels"] });
  }

  async function postChannelMember(
    member: OfficeMember,
    action: ChannelMemberAction,
  ) {
    await post("/channel-members", {
      channel: channelSlug,
      slug: member.slug,
      action,
    });
  }

  async function restoreRemovedMember(member: OfficeMember) {
    const label = memberDisplayName(member);
    try {
      await postChannelMember(member, "add");
      if (member.disabled) {
        await postChannelMember(member, "disable");
      }
      await refreshParticipantQueries();
      showNotice(`${label} restored to #${channelSlug}`, "success");
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to restore participant";
      showNotice(message, "error");
    }
  }

  async function updateChannelMember(
    member: OfficeMember,
    action: ChannelMemberAction,
  ) {
    const label = memberDisplayName(member);
    const actionKey = `${action}:${member.slug}`;
    setPendingAction(actionKey);
    try {
      await postChannelMember(member, action);
      await refreshParticipantQueries();
      if (action === "add") setAddOpen(false);
      if (action === "remove") {
        showUndoToast(
          `${label} removed from #${channelSlug}`,
          () => {
            void restoreRemovedMember(member);
          },
          5000,
        );
        return;
      }
      showNotice(participantNotice(label, channelSlug, action), "success");
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to update participants";
      showNotice(message, "error");
    } finally {
      setPendingAction(null);
    }
  }

  return (
    <aside className="channel-participants" aria-label="Channel participants">
      <div className="channel-participants-header">
        <div>
          <div className="channel-participants-title">Participants</div>
          <div className="channel-participants-subtitle">
            {isLoading
              ? "Loading agents"
              : `${agents.length} ${agents.length === 1 ? "agent" : "agents"}`}
          </div>
        </div>
        <button
          type="button"
          className="channel-participants-add"
          onClick={() => setAddOpen((open) => !open)}
          aria-expanded={addOpen}
          aria-controls="channel-participants-add-menu"
          aria-label={`Add participant to #${channelSlug}`}
          title="Add participant"
        >
          +
        </button>
      </div>

      {addOpen ? (
        <AddParticipantMenu
          agents={availableAgents}
          channelSlug={channelSlug}
          pendingAction={pendingAction}
          onAdd={(member) => updateChannelMember(member, "add")}
        />
      ) : null}

      <div className="channel-participants-list">
        {agents.map((member) => (
          <ParticipantRow
            key={member.slug}
            member={member}
            pendingAction={pendingAction}
            onOpenAgent={setActiveAgentSlug}
            onUpdate={updateChannelMember}
          />
        ))}

        {!isLoading && agents.length === 0 ? (
          <div className="channel-participants-empty">
            No agents in this channel
          </div>
        ) : null}
      </div>
    </aside>
  );
}
