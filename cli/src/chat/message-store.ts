/**
 * JSONL-based message persistence for chat channels.
 * Messages are stored per channel at ~/.wuphf/chat/messages/<channelId>.jsonl
 */

import { readFileSync, readdirSync, appendFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { homedir } from 'node:os';
import { randomUUID } from 'node:crypto';
import type { ChatMessage } from './types.js';

export class MessageStore {
  private baseDir: string;

  constructor(baseDir?: string) {
    this.baseDir = baseDir ?? join(homedir(), '.wuphf', 'chat', 'messages');
  }

  private channelPath(channelId: string): string {
    return join(this.baseDir, `${channelId}.jsonl`);
  }

  send(message: Omit<ChatMessage, 'id' | 'timestamp'>): ChatMessage {
    const full: ChatMessage = {
      ...message,
      id: randomUUID(),
      timestamp: Date.now(),
    };

    mkdirSync(this.baseDir, { recursive: true });
    const filePath = this.channelPath(full.channelId);
    if (!existsSync(filePath)) {
      writeFileSync(filePath, '', 'utf-8');
    }
    appendFileSync(filePath, JSON.stringify(full) + '\n', 'utf-8');
    return full;
  }

  getMessages(channelId: string, options?: { limit?: number; before?: number }): ChatMessage[] {
    const filePath = this.channelPath(channelId);
    if (!existsSync(filePath)) return [];

    const raw = readFileSync(filePath, 'utf-8').trim();
    if (!raw) return [];

    let messages: ChatMessage[] = raw
      .split('\n')
      .filter(line => line.trim())
      .map(line => JSON.parse(line) as ChatMessage);

    if (options?.before) {
      messages = messages.filter(m => m.timestamp < options.before!);
    }

    if (options?.limit && options.limit > 0) {
      messages = messages.slice(-options.limit);
    }

    return messages;
  }

  getThread(messageId: string): ChatMessage[] {
    // Search across all channels for the thread
    // A thread is: the root message + all messages with replyTo === messageId
    const allMessages = this.getAllMessages();
    const root = allMessages.find(m => m.id === messageId);
    if (!root) return [];

    const replies = allMessages.filter(m => m.replyTo === messageId);
    return [root, ...replies].sort((a, b) => a.timestamp - b.timestamp);
  }

  getUnread(member: string, since: number): ChatMessage[] {
    const allMessages = this.getAllMessages();
    return allMessages.filter(
      m => m.timestamp > since && m.sender !== member && m.mentions.includes(member),
    );
  }

  private getAllMessages(): ChatMessage[] {
    if (!existsSync(this.baseDir)) return [];

    const files = readdirSync(this.baseDir).filter((f: string) => f.endsWith('.jsonl'));

    const messages: ChatMessage[] = [];
    for (const file of files) {
      const filePath = join(this.baseDir, file);
      const raw = readFileSync(filePath, 'utf-8').trim();
      if (!raw) continue;
      const lines = raw.split('\n').filter((l: string) => l.trim());
      for (const line of lines) {
        messages.push(JSON.parse(line) as ChatMessage);
      }
    }

    return messages;
  }
}
