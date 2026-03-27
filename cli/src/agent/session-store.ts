/**
 * DAG-based session persistence using JSONL files.
 * Each session is a JSONL file at ~/.wuphf/sessions/<agentSlug>/<sessionId>.jsonl
 * Branching creates a new session that copies history up to the branch point.
 */

import { readFileSync, appendFileSync, writeFileSync, mkdirSync, readdirSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { homedir } from 'node:os';
import { randomUUID } from 'node:crypto';
import type { SessionEntry } from './types.js';

export class AgentSessionStore {
  private baseDir: string;

  constructor(baseDir?: string) {
    this.baseDir = baseDir ?? join(homedir(), '.wuphf', 'sessions');
  }

  private agentDir(agentSlug: string): string {
    return join(this.baseDir, agentSlug);
  }

  private sessionPath(agentSlug: string, sessionId: string): string {
    return join(this.agentDir(agentSlug), `${sessionId}.jsonl`);
  }

  private extractAgentSlug(sessionId: string, fallback?: string): string {
    // Session IDs are prefixed with agent slug: <slug>_<uuid>
    const idx = sessionId.lastIndexOf('_');
    if (idx > 0) return sessionId.substring(0, idx);
    return fallback ?? 'unknown';
  }

  create(agentSlug: string): string {
    const sessionId = `${agentSlug}_${randomUUID()}`;
    const dir = this.agentDir(agentSlug);
    mkdirSync(dir, { recursive: true });
    writeFileSync(this.sessionPath(agentSlug, sessionId), '', 'utf-8');
    return sessionId;
  }

  append(
    sessionId: string,
    entry: Omit<SessionEntry, 'id' | 'timestamp'>,
  ): SessionEntry {
    const agentSlug = this.extractAgentSlug(sessionId);
    const full: SessionEntry = {
      ...entry,
      id: randomUUID(),
      timestamp: Date.now(),
    };
    const filePath = this.sessionPath(agentSlug, sessionId);
    if (!existsSync(filePath)) {
      const dir = this.agentDir(agentSlug);
      mkdirSync(dir, { recursive: true });
      writeFileSync(filePath, '', 'utf-8');
    }
    appendFileSync(filePath, JSON.stringify(full) + '\n', 'utf-8');
    return full;
  }

  getHistory(
    sessionId: string,
    options?: { limit?: number; fromId?: string },
  ): SessionEntry[] {
    const agentSlug = this.extractAgentSlug(sessionId);
    const filePath = this.sessionPath(agentSlug, sessionId);
    if (!existsSync(filePath)) return [];

    const raw = readFileSync(filePath, 'utf-8').trim();
    if (!raw) return [];

    let entries: SessionEntry[] = raw
      .split('\n')
      .filter(line => line.trim())
      .map(line => JSON.parse(line) as SessionEntry);

    if (options?.fromId) {
      const idx = entries.findIndex(e => e.id === options.fromId);
      if (idx >= 0) {
        entries = entries.slice(idx);
      }
    }

    if (options?.limit && options.limit > 0) {
      entries = entries.slice(-options.limit);
    }

    return entries;
  }

  branch(sessionId: string, fromEntryId: string): string {
    const agentSlug = this.extractAgentSlug(sessionId);
    const history = this.getHistory(sessionId);
    const idx = history.findIndex(e => e.id === fromEntryId);

    const newSessionId = this.create(agentSlug);
    if (idx >= 0) {
      const branchHistory = history.slice(0, idx + 1);
      const filePath = this.sessionPath(agentSlug, newSessionId);
      const content = branchHistory.map(e => JSON.stringify(e)).join('\n') + '\n';
      writeFileSync(filePath, content, 'utf-8');
    }

    return newSessionId;
  }

  listSessions(agentSlug: string): string[] {
    const dir = this.agentDir(agentSlug);
    if (!existsSync(dir)) return [];

    return readdirSync(dir)
      .filter(f => f.endsWith('.jsonl'))
      .map(f => f.replace('.jsonl', ''));
  }
}
