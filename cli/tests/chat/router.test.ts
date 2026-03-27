import { describe, it, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { ChatRouter } from '../../src/chat/router.js';
import { ChannelManager } from '../../src/chat/channel.js';
import { MessageStore } from '../../src/chat/message-store.js';

describe('ChatRouter', () => {
  let tmpDir: string;
  let channels: ChannelManager;
  let messages: MessageStore;
  let router: ChatRouter;
  let channelId: string;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'wuphf-chat-router-test-'));
    channels = new ChannelManager(tmpDir);
    messages = new MessageStore(join(tmpDir, 'messages'));
    router = new ChatRouter(channels, messages);

    const channel = channels.create({
      name: '#test',
      type: 'public',
      members: ['human'],
    });
    channelId = channel.id;
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it('parseMentions extracts @agent-name from content', () => {
    const mentions = router.parseMentions('Hey @seo-agent check this @lead-gen');
    assert.deepEqual(mentions.sort(), ['lead-gen', 'seo-agent']);
  });

  it('parseMentions returns empty for no mentions', () => {
    const mentions = router.parseMentions('No mentions here');
    assert.deepEqual(mentions, []);
  });

  it('parseMentions deduplicates', () => {
    const mentions = router.parseMentions('@bot hello @bot again');
    assert.deepEqual(mentions, ['bot']);
  });

  it('route delivers message to correct channel', () => {
    const msg = router.route(channelId, 'human', 'Hello @seo-agent');
    assert.equal(msg.channelId, channelId);
    assert.equal(msg.sender, 'human');
    assert.equal(msg.senderType, 'human');
    assert.equal(msg.content, 'Hello @seo-agent');
    assert.deepEqual(msg.mentions, ['seo-agent']);
  });

  it('route sets senderType to agent for non-human', () => {
    const msg = router.route(channelId, 'seo-agent', 'Report ready');
    assert.equal(msg.senderType, 'agent');
  });

  it('route auto-joins sender and mentioned agents to channel', () => {
    router.route(channelId, 'new-agent', 'Hey @another-agent');
    const members = channels.getMembers(channelId);
    assert.ok(members.includes('new-agent'));
    assert.ok(members.includes('another-agent'));
  });

  it('route persists messages', () => {
    router.route(channelId, 'human', 'Message 1');
    router.route(channelId, 'bot', 'Message 2');
    const msgs = messages.getMessages(channelId);
    assert.equal(msgs.length, 2);
    assert.equal(msgs[0].content, 'Message 1');
    assert.equal(msgs[1].content, 'Message 2');
  });
});
