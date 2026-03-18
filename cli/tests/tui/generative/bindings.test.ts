import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  resolvePointer,
  setPointer,
  isPointer,
  applyUpdates,
} from '../../../src/tui/generative/bindings.js';

describe('resolvePointer', () => {
  it('handles simple top-level key', () => {
    const data = { name: 'Alice' };
    assert.equal(resolvePointer(data, '/name'), 'Alice');
  });

  it('handles nested paths', () => {
    const data = { user: { name: 'Alice', age: 30 } };
    assert.equal(resolvePointer(data, '/user/name'), 'Alice');
    assert.equal(resolvePointer(data, '/user/age'), 30);
  });

  it('handles array indices', () => {
    const data = { items: [{ status: 'done' }, { status: 'pending' }] };
    assert.equal(resolvePointer(data, '/items/0/status'), 'done');
    assert.equal(resolvePointer(data, '/items/1/status'), 'pending');
  });

  it('returns undefined for missing paths', () => {
    const data = { user: { name: 'Alice' } };
    assert.equal(resolvePointer(data, '/user/email'), undefined);
    assert.equal(resolvePointer(data, '/missing/deep/path'), undefined);
  });

  it('returns undefined when traversing through a primitive', () => {
    const data = { name: 'Alice' };
    assert.equal(resolvePointer(data, '/name/first'), undefined);
  });

  it('handles empty pointer', () => {
    const data = { name: 'Alice' };
    // Empty pointer returns the root
    assert.deepEqual(resolvePointer(data, ''), data);
  });

  it('handles null data gracefully', () => {
    assert.equal(resolvePointer(null, '/name'), undefined);
  });

  it('handles deeply nested paths', () => {
    const data = { a: { b: { c: { d: 'deep' } } } };
    assert.equal(resolvePointer(data, '/a/b/c/d'), 'deep');
  });
});

describe('setPointer', () => {
  it('sets a top-level key', () => {
    const data: Record<string, unknown> = {};
    setPointer(data, '/name', 'Alice');
    assert.equal(data.name, 'Alice');
  });

  it('creates nested structure', () => {
    const data: Record<string, unknown> = {};
    setPointer(data, '/user/name', 'Alice');
    assert.deepEqual(data, { user: { name: 'Alice' } });
  });

  it('overwrites existing values', () => {
    const data: Record<string, unknown> = { name: 'Alice' };
    setPointer(data, '/name', 'Bob');
    assert.equal(data.name, 'Bob');
  });

  it('creates intermediate objects for deep paths', () => {
    const data: Record<string, unknown> = {};
    setPointer(data, '/a/b/c', 'deep');
    assert.equal((data as any).a.b.c, 'deep');
  });
});

describe('isPointer', () => {
  it('recognizes "/" prefix as a pointer', () => {
    assert.equal(isPointer('/user/name'), true);
    assert.equal(isPointer('/items'), true);
    assert.equal(isPointer('/'), true);
  });

  it('rejects non-pointer strings', () => {
    assert.equal(isPointer('plain text'), false);
    assert.equal(isPointer(''), false);
    assert.equal(isPointer('user/name'), false);
  });
});

describe('applyUpdates', () => {
  it('applies set operations', () => {
    const model = { count: 0 };
    const result = applyUpdates(model, [
      { type: 'set', path: '/count', value: 42 },
    ]);
    assert.equal(result.count, 42);
  });

  it('applies merge operations on objects', () => {
    const model = { user: { name: 'Alice', age: 30 } };
    const result = applyUpdates(model, [
      { type: 'merge', path: '/user', value: { email: 'a@b.com' } },
    ]);
    const user = result.user as Record<string, unknown>;
    assert.equal(user.name, 'Alice');
    assert.equal(user.email, 'a@b.com');
  });

  it('merge falls back to set for non-object targets', () => {
    const model = { count: 5 };
    const result = applyUpdates(model, [
      { type: 'merge', path: '/count', value: 10 },
    ]);
    assert.equal(result.count, 10);
  });

  it('applies delete operations', () => {
    const model = { name: 'Alice', age: 30 };
    const result = applyUpdates(model, [
      { type: 'delete', path: '/age' },
    ]);
    assert.equal('age' in result, false);
    assert.equal(result.name, 'Alice');
  });

  it('applies multiple updates in sequence', () => {
    const model = { x: 1, y: 2 };
    const result = applyUpdates(model, [
      { type: 'set', path: '/x', value: 10 },
      { type: 'set', path: '/z', value: 3 },
      { type: 'delete', path: '/y' },
    ]);
    assert.equal(result.x, 10);
    assert.equal(result.z, 3);
    assert.equal('y' in result, false);
  });

  it('does not mutate the original model', () => {
    const model = { count: 0 };
    const result = applyUpdates(model, [
      { type: 'set', path: '/count', value: 42 },
    ]);
    // The returned object is a shallow copy, so top-level props differ
    assert.notEqual(model, result);
  });
});
