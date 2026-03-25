/**
 * Message routing for the chat system.
 * Parses @mentions, delivers to correct channels, and tracks pending mentions.
 */

import type { ChannelManager } from './channel.js';
import type { MessageStore } from './message-store.js';
import type { ChatMessage } from './types.js';

const MENTION_RE = /@([a-zA-Z0-9_-]+)/g;

export class ChatRouter {
  private channels: ChannelManager;
  private messages: MessageStore;
  private lastRead = new Map<string, number>();

  constructor(channels: ChannelManager, messages: MessageStore) {
    this.channels = channels;
    this.messages = messages;
  }

  route(channelId: string, sender: string, content: string): ChatMessage {
    const mentions = this.parseMentions(content);
    const channel = this.channels.get(channelId);
    const senderType = sender === 'human' ? 'human' as const : 'agent' as const;

    const message = this.messages.send({
      channelId,
      sender,
      senderType,
      content,
      mentions,
    });

    // Auto-join sender to channel if not already a member
    if (channel && !channel.members.includes(sender)) {
      this.channels.join(channelId, sender);
    }

    // Auto-join mentioned agents to channel
    for (const mention of mentions) {
      if (channel && !channel.members.includes(mention)) {
        this.channels.join(channelId, mention);
      }
    }

    // Update last read for sender
    this.lastRead.set(sender, Date.now());

    return message;
  }

  parseMentions(content: string): string[] {
    const mentions: string[] = [];
    let match: RegExpExecArray | null;
    const re = new RegExp(MENTION_RE.source, MENTION_RE.flags);

    while ((match = re.exec(content)) !== null) {
      const mention = match[1];
      if (!mentions.includes(mention)) {
        mentions.push(mention);
      }
    }

    return mentions;
  }

  hasPendingMentions(): ChatMessage[] {
    const humanLastRead = this.lastRead.get('human') ?? 0;
    return this.messages.getUnread('human', humanLastRead);
  }

  checkAutoDecide(agentSlug: string, timeout: number): boolean {
    const pending = this.hasPendingMentions();
    const agentPending = pending.filter(m => m.mentions.includes(agentSlug));

    if (agentPending.length === 0) return false;

    // Check if the oldest pending mention has exceeded the timeout
    const oldest = agentPending.reduce(
      (min, m) => (m.timestamp < min ? m.timestamp : min),
      Infinity,
    );

    return Date.now() - oldest > timeout;
  }
}
