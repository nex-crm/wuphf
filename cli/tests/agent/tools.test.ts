import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { ToolRegistry, createBuiltinTools } from '../../src/agent/tools.js';
import { NexClient } from '../../src/lib/client.js';

describe('ToolRegistry', () => {
  it('register and get round-trip', () => {
    const registry = new ToolRegistry();
    const tool = {
      name: 'test_tool',
      description: 'A test tool',
      schema: { type: 'object', properties: { q: { type: 'string' } } },
      execute: async () => 'ok',
    };
    registry.register(tool);
    assert.equal(registry.get('test_tool')?.name, 'test_tool');
  });

  it('unregister removes a tool', () => {
    const registry = new ToolRegistry();
    registry.register({
      name: 'temp',
      description: 'temp',
      schema: {},
      execute: async () => 'ok',
    });
    assert.equal(registry.has('temp'), true);
    registry.unregister('temp');
    assert.equal(registry.has('temp'), false);
    assert.equal(registry.get('temp'), undefined);
  });

  it('list returns all registered tools', () => {
    const registry = new ToolRegistry();
    registry.register({ name: 'a', description: 'a', schema: {}, execute: async () => '' });
    registry.register({ name: 'b', description: 'b', schema: {}, execute: async () => '' });
    const names = registry.list().map(t => t.name);
    assert.deepEqual(names.sort(), ['a', 'b']);
  });

  it('has returns false for unknown tool', () => {
    const registry = new ToolRegistry();
    assert.equal(registry.has('nonexistent'), false);
  });

  it('validate catches missing required params', () => {
    const registry = new ToolRegistry();
    registry.register({
      name: 'strict_tool',
      description: 'needs params',
      schema: {
        type: 'object',
        required: ['query', 'limit'],
        properties: {
          query: { type: 'string' },
          limit: { type: 'number' },
        },
      },
      execute: async () => '',
    });

    const result = registry.validate('strict_tool', { query: 'test' });
    assert.equal(result.valid, false);
    assert.ok(result.errors);
    assert.ok(result.errors.some(e => e.includes('limit')));
  });

  it('validate returns valid for correct params', () => {
    const registry = new ToolRegistry();
    registry.register({
      name: 'ok_tool',
      description: 'ok',
      schema: {
        type: 'object',
        required: ['q'],
        properties: { q: { type: 'string' } },
      },
      execute: async () => '',
    });

    const result = registry.validate('ok_tool', { q: 'hello' });
    assert.equal(result.valid, true);
    assert.equal(result.errors, undefined);
  });

  it('validate returns error for unknown tool', () => {
    const registry = new ToolRegistry();
    const result = registry.validate('ghost', {});
    assert.equal(result.valid, false);
    assert.ok(result.errors?.some(e => e.includes('Unknown tool')));
  });
});

describe('createBuiltinTools', () => {
  it('returns expected tool names', () => {
    const client = new NexClient('test-key');
    const tools = createBuiltinTools(client);
    const names = tools.map(t => t.name).sort();
    assert.deepEqual(names, [
      'nex_ask',
      'nex_record_create',
      'nex_record_get',
      'nex_record_list',
      'nex_record_update',
      'nex_remember',
      'nex_search',
    ]);
  });

  it('each tool has a schema with required fields', () => {
    const client = new NexClient('test-key');
    const tools = createBuiltinTools(client);
    for (const tool of tools) {
      assert.ok(tool.schema, `${tool.name} missing schema`);
      assert.ok(tool.description, `${tool.name} missing description`);
      assert.equal(typeof tool.execute, 'function', `${tool.name} missing execute`);
    }
  });
});
