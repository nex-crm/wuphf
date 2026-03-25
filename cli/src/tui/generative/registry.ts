/**
 * Component registry: maps A2UI component type strings to their
 * Ink renderer components.
 */

import type React from 'react';
import type { A2UIComponent } from './types.js';

// Forward-declared type for renderer components.
// Actual implementations live in renderer.tsx.
export type RendererComponent = React.ComponentType<{
  schema: A2UIComponent;
  data: Record<string, unknown>;
  onAction?: (action: string, payload?: unknown) => void;
}>;

// Registry is populated by renderer.tsx at module load time
const registry = new Map<string, RendererComponent>();

/** Register a renderer for a component type. */
export function registerRenderer(
  type: string,
  component: RendererComponent,
): void {
  registry.set(type, component);
}

/** Look up the renderer for a component type. */
export function getRenderer(type: string): RendererComponent | undefined {
  return registry.get(type);
}

/** Get all registered type names. */
export function getRegisteredTypes(): string[] {
  return [...registry.keys()];
}

/** Known A2UI component types. */
export const COMPONENT_TYPES = [
  'row',
  'column',
  'card',
  'text',
  'textfield',
  'list',
  'table',
  'progress',
  'spacer',
] as const;
