/**
 * Channel management for the Slack-style chat system.
 * Channels are persisted to ~/.wuphf/chat/channels.json
 */

import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { homedir } from 'node:os';
import { randomUUID } from 'node:crypto';
import type { Channel } from './types.js';

export class ChannelManager {
  private baseDir: string;
  private filePath: string;
  private channels: Channel[];

  constructor(baseDir?: string) {
    this.baseDir = baseDir ?? join(homedir(), '.wuphf', 'chat');
    this.filePath = join(this.baseDir, 'channels.json');
    this.channels = this.load();
  }

  private load(): Channel[] {
    try {
      if (!existsSync(this.filePath)) return [];
      const raw = readFileSync(this.filePath, 'utf-8');
      return JSON.parse(raw) as Channel[];
    } catch {
      return [];
    }
  }

  private save(): void {
    mkdirSync(this.baseDir, { recursive: true });
    writeFileSync(this.filePath, JSON.stringify(this.channels, null, 2) + '\n', 'utf-8');
  }

  create(channel: Omit<Channel, 'id' | 'createdAt'>): Channel {
    const full: Channel = {
      ...channel,
      id: randomUUID(),
      createdAt: Date.now(),
    };
    this.channels.push(full);
    this.save();
    return full;
  }

  get(channelId: string): Channel | undefined {
    return this.channels.find(c => c.id === channelId);
  }

  list(): Channel[] {
    return [...this.channels];
  }

  join(channelId: string, member: string): void {
    const channel = this.channels.find(c => c.id === channelId);
    if (!channel) return;
    if (!channel.members.includes(member)) {
      channel.members.push(member);
      this.save();
    }
  }

  leave(channelId: string, member: string): void {
    const channel = this.channels.find(c => c.id === channelId);
    if (!channel) return;
    const idx = channel.members.indexOf(member);
    if (idx >= 0) {
      channel.members.splice(idx, 1);
      this.save();
    }
  }

  getMembers(channelId: string): string[] {
    const channel = this.channels.find(c => c.id === channelId);
    return channel ? [...channel.members] : [];
  }

  ensureDefaults(): void {
    const hasGeneral = this.channels.some(c => c.name === '#general');
    if (!hasGeneral) {
      this.create({
        name: '#general',
        type: 'public',
        members: ['human'],
        topic: 'General discussion',
      });
    }
  }
}
