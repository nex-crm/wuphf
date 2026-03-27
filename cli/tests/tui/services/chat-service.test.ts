import { describe, it, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { ChatService } from '../../../src/tui/services/chat-service.js';

describe('ChatService', () => {
  let tmpDir: string;
  let service: ChatService;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'wuphf-chat-service-test-'));
    service = new ChatService(tmpDir);
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it('constructor creates default #general channel', () => {
    const channels = service.getChannels();
    assert.ok(channels.length >= 1, 'should have at least one channel');
    assert.ok(
      channels.some(ch => ch.name === '#general'),
      'should contain #general channel',
    );
  });

  it('send() persists message and can be retrieved', () => {
    const channels = service.getChannels();
    const generalId = channels.find(ch => ch.name === '#general')!.id;

    const msg = service.send(generalId, 'Hello world');
    assert.equal(msg.content, 'Hello world');
    assert.equal(msg.sender, 'human');
    assert.equal(msg.channelId, generalId);

    const messages = service.getMessages(generalId);
    assert.equal(messages.length, 1);
    assert.equal(messages[0].content, 'Hello world');
    assert.equal(messages[0].id, msg.id);
  });

  it('send() with explicit sender sets correct senderType', () => {
    const channels = service.getChannels();
    const generalId = channels.find(ch => ch.name === '#general')!.id;

    const agentMsg = service.send(generalId, 'Agent report', 'seo-agent');
    assert.equal(agentMsg.sender, 'seo-agent');
    assert.equal(agentMsg.senderType, 'agent');
  });

  it('getChannels() returns channel list with unread counts', () => {
    const channels = service.getChannels();
    assert.ok(Array.isArray(channels));

    for (const ch of channels) {
      assert.ok(typeof ch.id === 'string');
      assert.ok(typeof ch.name === 'string');
      assert.ok(typeof ch.unread === 'number');
    }
  });

  it('createChannel() adds a new channel', () => {
    const before = service.getChannels().length;
    const channel = service.createChannel('#dev', 'private');
    assert.equal(channel.name, '#dev');
    assert.equal(channel.type, 'private');

    const after = service.getChannels().length;
    assert.equal(after, before + 1);
  });

  it('getMessages() respects limit parameter', () => {
    const generalId = service.getChannels().find(ch => ch.name === '#general')!.id;
    service.send(generalId, 'Message 1');
    service.send(generalId, 'Message 2');
    service.send(generalId, 'Message 3');

    const limited = service.getMessages(generalId, 2);
    assert.equal(limited.length, 2);
    // Should return the last 2 messages (most recent)
    assert.equal(limited[0].content, 'Message 2');
    assert.equal(limited[1].content, 'Message 3');
  });

  it('route() parses mentions and delivers message', () => {
    const generalId = service.getChannels().find(ch => ch.name === '#general')!.id;
    const msg = service.route(generalId, 'human', 'Hey @seo-agent check this');
    assert.deepEqual(msg.mentions, ['seo-agent']);
    assert.equal(msg.content, 'Hey @seo-agent check this');
  });

  it('subscribe/notify works', () => {
    let callCount = 0;
    const unsubscribe = service.subscribe(() => {
      callCount++;
    });

    const generalId = service.getChannels().find(ch => ch.name === '#general')!.id;
    service.send(generalId, 'trigger notify');
    assert.equal(callCount, 1);

    service.send(generalId, 'trigger again');
    assert.equal(callCount, 2);

    // Unsubscribe should stop notifications
    unsubscribe();
    service.send(generalId, 'no notify');
    assert.equal(callCount, 2);
  });

  it('subscribe notifies on createChannel', () => {
    let called = false;
    service.subscribe(() => { called = true; });
    service.createChannel('#alerts');
    assert.ok(called, 'listener should be called on createChannel');
  });
});
