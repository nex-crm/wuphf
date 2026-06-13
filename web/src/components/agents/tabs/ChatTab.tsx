/**
 * Chat tab — DM conversation with this agent.
 * Reuses DMView with the canonical direct-channel slug.
 */

import type { OfficeMember } from "../../../api/client";
import { directChannelSlug } from "../../../stores/app";
import { DMView } from "../../messages/DMView";

interface ChatTabProps {
  agent: OfficeMember;
}

export function ChatTab({ agent }: ChatTabProps) {
  const channelSlug = directChannelSlug(agent.slug);
  return (
    <div className="agent-subspace-tab-content">
      <DMView agentSlug={agent.slug} channelSlug={channelSlug} />
    </div>
  );
}
