/**
 * TUI service layer for the chat system.
 * Wraps ChannelManager, MessageStore, and ChatRouter into a single facade
 * that the chat view can consume without touching backend internals.
 */

import { join } from 'node:path';
import { homedir } from 'node:os';
import { ChannelManager } from '../../chat/channel.js';
import { MessageStore } from '../../chat/message-store.js';
import { ChatRouter } from '../../chat/router.js';
import type { Channel, ChatMessage } from '../../chat/types.js';

export class ChatService {
  private channelManager: ChannelManager;
  private messageStore: MessageStore;
  private chatRouter: ChatRouter;
  private listeners: Array<() => void> = [];

  constructor(baseDir?: string) {
    const dir = baseDir ?? join(process.env.NEX_CLI_DATA_DIR ?? join(homedir(), '.wuphf'), 'chat');
    this.channelManager = new ChannelManager(dir);
    this.messageStore = new MessageStore(join(dir, 'messages'));
    this.chatRouter = new ChatRouter(this.channelManager, this.messageStore);

    // Ensure a #general channel always exists
    this.channelManager.ensureDefaults();
  }

  // ── Channel operations ──

  getChannels(): Array<{ id: string; name: string; unread: number }> {
    const channels = this.channelManager.list();
    return channels.map(ch => ({
      id: ch.id,
      name: ch.name,
      unread: this.messageStore.getUnread('human', 0)
        .filter(m => m.channelId === ch.id).length,
    }));
  }

  createChannel(name: string, type: 'public' | 'private' = 'public'): Channel {
    const channel = this.channelManager.create({
      name,
      type,
      members: ['human'],
    });
    this.notify();
    return channel;
  }

  /** Get existing channel by name, or create it if it doesn't exist. */
  ensureChannel(name: string, type: 'public' | 'private' = 'public'): Channel {
    const existing = this.channelManager.list().find((ch) => ch.name === name);
    if (existing) return existing;
    return this.createChannel(name, type);
  }

  // ── Message operations ──

  getMessages(channelId: string, limit?: number): ChatMessage[] {
    return this.messageStore.getMessages(channelId, { limit });
  }

  send(channelId: string, content: string, sender: string = 'human'): ChatMessage {
    const message = this.chatRouter.route(channelId, sender, content);
    this.notify();
    return message;
  }

  // ── Routing ──

  route(channelId: string, sender: string, content: string): ChatMessage {
    const message = this.chatRouter.route(channelId, sender, content);
    this.notify();
    return message;
  }

  getPendingMentions(): ChatMessage[] {
    return this.chatRouter.hasPendingMentions();
  }

  // ── Subscription for TUI re-renders ──

  subscribe(listener: () => void): () => void {
    this.listeners.push(listener);
    return () => {
      const idx = this.listeners.indexOf(listener);
      if (idx >= 0) this.listeners.splice(idx, 1);
    };
  }

  private notify(): void {
    for (const listener of this.listeners) {
      listener();
    }
  }
}

// ── Singleton accessor ──

let instance: ChatService | undefined;

export function getChatService(): ChatService {
  if (!instance) {
    instance = new ChatService();
  }
  return instance;
}
