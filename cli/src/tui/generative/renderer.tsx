/**
 * Generative TUI renderer: converts A2UI JSON schema into an Ink
 * component tree at runtime.
 *
 * Agents emit JSON matching the A2UIComponent schema; this module
 * validates it, resolves JSON Pointer data bindings, and renders
 * the corresponding Ink components.
 */

import React from 'react';
import { Box, Text } from 'ink';
import { resolvePointer, isPointer } from './bindings.js';
import { COMPONENT_TYPES } from './registry.js';
import type {
  A2UIComponent,
  A2UIDataModel,
  A2UIRow,
  A2UIColumn,
  A2UICard,
  A2UIText,
  A2UITextField,
  A2UIList,
  A2UITable,
  A2UIProgress,
  A2UISpacer,
} from './types.js';

// ── Schema validation ───────────────────────────────────────────────

export function validateSchema(
  schema: unknown,
): { valid: boolean; errors?: string[] } {
  const errors: string[] = [];

  if (typeof schema !== 'object' || schema === null) {
    return { valid: false, errors: ['Schema must be a non-null object'] };
  }

  const obj = schema as Record<string, unknown>;
  if (typeof obj.type !== 'string') {
    errors.push('Component must have a "type" string property');
    return { valid: false, errors };
  }

  if (!(COMPONENT_TYPES as readonly string[]).includes(obj.type)) {
    errors.push(
      `Unknown component type "${obj.type}". Valid types: ${COMPONENT_TYPES.join(', ')}`,
    );
  }

  // Container types must have children array
  if (['row', 'column', 'card'].includes(obj.type)) {
    if (!Array.isArray(obj.children)) {
      errors.push(`"${obj.type}" component must have a "children" array`);
    }
  }

  // Text must have content
  if (obj.type === 'text') {
    if (typeof obj.content !== 'string') {
      errors.push('"text" component must have a "content" string');
    }
  }

  // Progress must have numeric value
  if (obj.type === 'progress') {
    if (typeof obj.value !== 'number') {
      errors.push('"progress" component must have a numeric "value"');
    }
  }

  // Table must have headers
  if (obj.type === 'table') {
    if (!Array.isArray(obj.headers)) {
      errors.push('"table" component must have a "headers" array');
    }
  }

  // List must have items
  if (obj.type === 'list') {
    if (!Array.isArray(obj.items) && typeof obj.items !== 'string') {
      errors.push('"list" component must have "items" (array or string pointer)');
    }
  }

  return errors.length === 0 ? { valid: true } : { valid: false, errors };
}

// ── Resolve helper ──────────────────────────────────────────────────

function resolveString(value: string, data: A2UIDataModel): string {
  if (isPointer(value)) {
    const resolved = resolvePointer(data, value);
    return resolved !== undefined ? String(resolved) : value;
  }
  return value;
}

// ── Individual renderers ────────────────────────────────────────────

interface ChildProps {
  data: A2UIDataModel;
  onAction?: (action: string, payload?: unknown) => void;
}

function RenderChildren({
  children,
  data,
  onAction,
}: {
  children: A2UIComponent[];
} & ChildProps): React.JSX.Element {
  return (
    <>
      {children.map((child, i) => (
        <RenderComponent key={i} schema={child} data={data} onAction={onAction} />
      ))}
    </>
  );
}

function RowRenderer({
  schema,
  data,
  onAction,
}: { schema: A2UIRow } & ChildProps): React.JSX.Element {
  return (
    <Box flexDirection="row" gap={schema.gap} padding={schema.padding}>
      <RenderChildren children={schema.children} data={data} onAction={onAction} />
    </Box>
  );
}

function ColumnRenderer({
  schema,
  data,
  onAction,
}: { schema: A2UIColumn } & ChildProps): React.JSX.Element {
  return (
    <Box flexDirection="column" gap={schema.gap} padding={schema.padding}>
      <RenderChildren children={schema.children} data={data} onAction={onAction} />
    </Box>
  );
}

function CardRenderer({
  schema,
  data,
  onAction,
}: { schema: A2UICard } & ChildProps): React.JSX.Element {
  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor={schema.borderColor ?? 'white'}
      paddingX={1}
    >
      {schema.title && (
        <Text bold>{resolveString(schema.title, data)}</Text>
      )}
      <RenderChildren children={schema.children} data={data} onAction={onAction} />
    </Box>
  );
}

function TextRenderer({
  schema,
  data,
}: { schema: A2UIText } & ChildProps): React.JSX.Element {
  const content = resolveString(schema.content, data);
  return (
    <Text bold={schema.bold} color={schema.color} dimColor={schema.dimmed}>
      {content}
    </Text>
  );
}

function TextFieldRenderer({
  schema,
  data,
}: { schema: A2UITextField } & ChildProps): React.JSX.Element {
  const value = schema.value
    ? resolveString(schema.value, data)
    : '';
  const display = value || schema.placeholder || '';
  return (
    <Box>
      <Text color={value ? undefined : 'gray'}>{display}</Text>
    </Box>
  );
}

function ListRenderer({
  schema,
  data,
  onAction,
}: { schema: A2UIList } & ChildProps): React.JSX.Element {
  let items: string[];
  if (typeof schema.items === 'string') {
    const resolved = resolvePointer(data, schema.items);
    items = Array.isArray(resolved) ? resolved.map(String) : [];
  } else {
    items = schema.items;
  }

  return (
    <Box flexDirection="column">
      {items.map((item, i) => {
        const isSelected = schema.selected === i;
        return (
          <Box key={i}>
            <Text color={isSelected ? 'cyan' : undefined}>
              {isSelected ? '> ' : '  '}
              {item}
            </Text>
          </Box>
        );
      })}
    </Box>
  );
}

function TableRenderer({
  schema,
  data,
}: { schema: A2UITable } & ChildProps): React.JSX.Element {
  let rows: string[][];
  if (typeof schema.rows === 'string') {
    const resolved = resolvePointer(data, schema.rows);
    rows = Array.isArray(resolved)
      ? resolved.map((r) => (Array.isArray(r) ? r.map(String) : [String(r)]))
      : [];
  } else if (Array.isArray(schema.rows)) {
    rows = schema.rows;
  } else {
    rows = [];
  }

  return (
    <Box flexDirection="column">
      {/* Header row */}
      <Box>
        {schema.headers.map((h, i) => (
          <Box key={i} minWidth={16}>
            <Text bold>{h}</Text>
          </Box>
        ))}
      </Box>
      {/* Data rows */}
      {rows.map((row, ri) => (
        <Box key={ri}>
          {row.map((cell, ci) => (
            <Box key={ci} minWidth={16}>
              <Text>{cell}</Text>
            </Box>
          ))}
        </Box>
      ))}
    </Box>
  );
}

function ProgressRenderer({
  schema,
}: { schema: A2UIProgress } & ChildProps): React.JSX.Element {
  const pct = Math.max(0, Math.min(100, schema.value));
  const filled = Math.round(pct / 5);
  const empty = 20 - filled;
  const bar = '\u2588'.repeat(filled) + '\u2591'.repeat(empty);
  const label = schema.label ? `${schema.label} ` : '';

  return (
    <Text>
      {label}[{bar}] {Math.round(pct)}%
    </Text>
  );
}

function SpacerRenderer({
  schema,
}: { schema: A2UISpacer }): React.JSX.Element {
  return <Box height={schema.height ?? 1} />;
}

// ── Main dispatcher ─────────────────────────────────────────────────

function RenderComponent({
  schema,
  data,
  onAction,
}: {
  schema: A2UIComponent;
  data: A2UIDataModel;
  onAction?: (action: string, payload?: unknown) => void;
}): React.JSX.Element {
  const childProps = { data, onAction };

  switch (schema.type) {
    case 'row':
      return <RowRenderer schema={schema} {...childProps} />;
    case 'column':
      return <ColumnRenderer schema={schema} {...childProps} />;
    case 'card':
      return <CardRenderer schema={schema} {...childProps} />;
    case 'text':
      return <TextRenderer schema={schema} {...childProps} />;
    case 'textfield':
      return <TextFieldRenderer schema={schema} {...childProps} />;
    case 'list':
      return <ListRenderer schema={schema} {...childProps} />;
    case 'table':
      return <TableRenderer schema={schema} {...childProps} />;
    case 'progress':
      return <ProgressRenderer schema={schema} {...childProps} />;
    case 'spacer':
      return <SpacerRenderer schema={schema} />;
    default:
      return <Text color="red">Unknown component: {(schema as { type: string }).type}</Text>;
  }
}

// ── Public renderer ─────────────────────────────────────────────────

export interface GenerativeRendererProps {
  schema: A2UIComponent;
  data: A2UIDataModel;
  onAction?: (action: string, payload?: unknown) => void;
}

/**
 * Validates schema, resolves data bindings, and renders the
 * A2UI component tree as Ink components.
 */
export function GenerativeRenderer({
  schema,
  data,
  onAction,
}: GenerativeRendererProps): React.JSX.Element {
  const validation = validateSchema(schema);
  if (!validation.valid) {
    return (
      <Box flexDirection="column">
        <Text color="red" bold>Invalid A2UI schema:</Text>
        {validation.errors?.map((err, i) => (
          <Text key={i} color="red">  - {err}</Text>
        ))}
      </Box>
    );
  }

  return <RenderComponent schema={schema} data={data} onAction={onAction} />;
}
