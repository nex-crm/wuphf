import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import React from 'react';
import { render } from 'ink-testing-library';
import { GenerativeRenderer, validateSchema } from '../../../src/tui/generative/renderer.js';
import type { A2UIComponent, A2UIDataModel } from '../../../src/tui/generative/types.js';

describe('GenerativeRenderer', () => {
  it('renders a simple text component', () => {
    const schema: A2UIComponent = {
      type: 'text',
      content: 'Hello World',
    };
    const { lastFrame } = render(
      <GenerativeRenderer schema={schema} data={{}} />,
    );
    assert.ok(lastFrame()?.includes('Hello World'));
  });

  it('renders a row with children', () => {
    const schema: A2UIComponent = {
      type: 'row',
      children: [
        { type: 'text', content: 'Left' },
        { type: 'text', content: 'Right' },
      ],
    };
    const { lastFrame } = render(
      <GenerativeRenderer schema={schema} data={{}} />,
    );
    const frame = lastFrame() ?? '';
    assert.ok(frame.includes('Left'));
    assert.ok(frame.includes('Right'));
  });

  it('resolves data bindings in text content', () => {
    const schema: A2UIComponent = {
      type: 'text',
      content: '/user/name',
    };
    const data: A2UIDataModel = {
      user: { name: 'Alice' },
    };
    const { lastFrame } = render(
      <GenerativeRenderer schema={schema} data={data} />,
    );
    assert.ok(lastFrame()?.includes('Alice'));
  });

  it('renders a column with nested children', () => {
    const schema: A2UIComponent = {
      type: 'column',
      children: [
        { type: 'text', content: 'Line 1' },
        { type: 'text', content: 'Line 2' },
      ],
    };
    const { lastFrame } = render(
      <GenerativeRenderer schema={schema} data={{}} />,
    );
    const frame = lastFrame() ?? '';
    assert.ok(frame.includes('Line 1'));
    assert.ok(frame.includes('Line 2'));
  });

  it('renders a card with title', () => {
    const schema: A2UIComponent = {
      type: 'card',
      title: 'My Card',
      children: [
        { type: 'text', content: 'Card body' },
      ],
    };
    const { lastFrame } = render(
      <GenerativeRenderer schema={schema} data={{}} />,
    );
    const frame = lastFrame() ?? '';
    assert.ok(frame.includes('My Card'));
    assert.ok(frame.includes('Card body'));
  });

  it('renders a progress bar', () => {
    const schema: A2UIComponent = {
      type: 'progress',
      value: 50,
      label: 'Upload',
    };
    const { lastFrame } = render(
      <GenerativeRenderer schema={schema} data={{}} />,
    );
    const frame = lastFrame() ?? '';
    assert.ok(frame.includes('50%'));
    assert.ok(frame.includes('Upload'));
  });

  it('renders a list with items', () => {
    const schema: A2UIComponent = {
      type: 'list',
      items: ['Apple', 'Banana', 'Cherry'],
      selected: 1,
    };
    const { lastFrame } = render(
      <GenerativeRenderer schema={schema} data={{}} />,
    );
    const frame = lastFrame() ?? '';
    assert.ok(frame.includes('Apple'));
    assert.ok(frame.includes('Banana'));
    assert.ok(frame.includes('Cherry'));
  });

  it('renders a table with headers and rows', () => {
    const schema: A2UIComponent = {
      type: 'table',
      headers: ['Name', 'Status'],
      rows: [
        ['Alice', 'Active'],
        ['Bob', 'Inactive'],
      ],
    };
    const { lastFrame } = render(
      <GenerativeRenderer schema={schema} data={{}} />,
    );
    const frame = lastFrame() ?? '';
    assert.ok(frame.includes('Name'));
    assert.ok(frame.includes('Status'));
    assert.ok(frame.includes('Alice'));
    assert.ok(frame.includes('Bob'));
  });

  it('shows error for invalid schema', () => {
    const schema = { type: 'bogus' } as unknown as A2UIComponent;
    const { lastFrame } = render(
      <GenerativeRenderer schema={schema} data={{}} />,
    );
    const frame = lastFrame() ?? '';
    assert.ok(frame.includes('Invalid A2UI schema'));
  });

  it('resolves list items from JSON Pointer', () => {
    const schema: A2UIComponent = {
      type: 'list',
      items: '/fruits',
    };
    const data: A2UIDataModel = {
      fruits: ['Apple', 'Banana'],
    };
    const { lastFrame } = render(
      <GenerativeRenderer schema={schema} data={data} />,
    );
    const frame = lastFrame() ?? '';
    assert.ok(frame.includes('Apple'));
    assert.ok(frame.includes('Banana'));
  });
});

describe('validateSchema', () => {
  it('accepts a valid text component', () => {
    const result = validateSchema({ type: 'text', content: 'hello' });
    assert.equal(result.valid, true);
  });

  it('rejects non-object schema', () => {
    const result = validateSchema('not an object');
    assert.equal(result.valid, false);
  });

  it('rejects null schema', () => {
    const result = validateSchema(null);
    assert.equal(result.valid, false);
  });

  it('rejects schema without type', () => {
    const result = validateSchema({ content: 'hello' });
    assert.equal(result.valid, false);
  });

  it('rejects unknown component type', () => {
    const result = validateSchema({ type: 'unknown-widget' });
    assert.equal(result.valid, false);
    assert.ok(result.errors?.[0].includes('Unknown component type'));
  });

  it('rejects row without children', () => {
    const result = validateSchema({ type: 'row' });
    assert.equal(result.valid, false);
  });

  it('rejects text without content', () => {
    const result = validateSchema({ type: 'text' });
    assert.equal(result.valid, false);
  });

  it('rejects progress without numeric value', () => {
    const result = validateSchema({ type: 'progress', value: 'not a number' });
    assert.equal(result.valid, false);
  });

  it('accepts a valid progress component', () => {
    const result = validateSchema({ type: 'progress', value: 50 });
    assert.equal(result.valid, true);
  });

  it('accepts a valid spacer component', () => {
    const result = validateSchema({ type: 'spacer' });
    assert.equal(result.valid, true);
  });
});
