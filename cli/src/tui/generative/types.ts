/**
 * A2UI-compatible component schema for the generative TUI.
 * Agents emit JSON matching these types; the renderer converts them
 * to Ink component trees at runtime.
 */

// ── Component types ─────────────────────────────────────────────────

export type A2UIComponent =
  | A2UIRow
  | A2UIColumn
  | A2UICard
  | A2UIText
  | A2UITextField
  | A2UIList
  | A2UITable
  | A2UIProgress
  | A2UISpacer;

export interface A2UIRow {
  type: 'row';
  children: A2UIComponent[];
  gap?: number;
  padding?: number;
}

export interface A2UIColumn {
  type: 'column';
  children: A2UIComponent[];
  gap?: number;
  padding?: number;
}

export interface A2UICard {
  type: 'card';
  title?: string;
  children: A2UIComponent[];
  borderColor?: string;
}

export interface A2UIText {
  type: 'text';
  content: string;           // plain text or JSON Pointer like "/user/name"
  bold?: boolean;
  color?: string;
  dimmed?: boolean;
}

export interface A2UITextField {
  type: 'textfield';
  placeholder?: string;
  value?: string;            // JSON Pointer for binding
  onSubmit?: string;         // action name
}

export interface A2UIList {
  type: 'list';
  items: string[] | string;  // array or JSON Pointer
  selected?: number;
  onSelect?: string;         // action name
}

export interface A2UITable {
  type: 'table';
  headers: string[];
  rows: string[][] | string; // array or JSON Pointer
  onRowSelect?: string;
}

export interface A2UIProgress {
  type: 'progress';
  value: number;             // 0-100
  label?: string;
}

export interface A2UISpacer {
  type: 'spacer';
  height?: number;
}

// ── Data model & updates ────────────────────────────────────────────

/** Data model that components bind to via JSON Pointers. */
export interface A2UIDataModel {
  [path: string]: unknown;
}

/** Streaming update message for modifying the data model. */
export interface A2UIUpdate {
  type: 'set' | 'merge' | 'delete';
  path: string;              // JSON Pointer (RFC 6901)
  value?: unknown;
}
