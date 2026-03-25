/**
 * Generative view wrapper.
 * Wraps the GenerativeRenderer with a title bar, error boundary,
 * and back-navigation hint for use as a routed TUI view.
 */

import React from 'react';
import { Box, Text } from 'ink';
import { GenerativeRenderer, validateSchema } from '../generative/renderer.js';
import type { A2UIComponent, A2UIDataModel } from '../generative/types.js';

// ── Types ──

export interface GenerativeViewProps {
  schema: A2UIComponent;
  data: A2UIDataModel;
  title?: string;
  onAction?: (action: string, payload?: unknown) => void;
}

// ── Error boundary ──

interface ErrorBoundaryState {
  hasError: boolean;
  error?: Error;
}

class SchemaErrorBoundary extends React.Component<
  { children: React.ReactNode },
  ErrorBoundaryState
> {
  constructor(props: { children: React.ReactNode }) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  render(): React.ReactNode {
    if (this.state.hasError) {
      return (
        <Box flexDirection="column" padding={1}>
          <Text color="red" bold>
            Render Error
          </Text>
          <Text color="red">
            {this.state.error?.message ?? 'An unexpected error occurred while rendering the schema.'}
          </Text>
          <Box height={1} />
          <Text color="gray">[Esc=back]</Text>
        </Box>
      );
    }
    return this.props.children;
  }
}

// ── Main view ──

export function GenerativeView({
  schema,
  data,
  title,
  onAction,
}: GenerativeViewProps): React.JSX.Element {
  // Pre-validate before rendering
  if (!schema) {
    return (
      <Box flexDirection="column" padding={1}>
        <Text color="red" bold>
          No Schema
        </Text>
        <Text color="gray">No A2UI schema was provided to render.</Text>
        <Box height={1} />
        <Text color="gray">[Esc=back]</Text>
      </Box>
    );
  }

  const validation = validateSchema(schema);

  if (!validation.valid) {
    return (
      <Box flexDirection="column" padding={1}>
        {title && (
          <Text bold color="cyan">
            {title}
          </Text>
        )}
        <Text color="red" bold>
          Invalid Schema
        </Text>
        {validation.errors?.map((err, i) => (
          <Text key={i} color="red">
            {'  '}- {err}
          </Text>
        ))}
        <Box height={1} />
        <Text color="gray">[Esc=back]</Text>
      </Box>
    );
  }

  return (
    <Box flexDirection="column" paddingX={1}>
      {/* Title bar */}
      {title && (
        <Box marginBottom={1}>
          <Text bold color="cyan">
            {title}
          </Text>
        </Box>
      )}

      {/* Schema renderer wrapped in error boundary */}
      <SchemaErrorBoundary>
        <GenerativeRenderer
          schema={schema}
          data={data ?? {}}
          onAction={onAction}
        />
      </SchemaErrorBoundary>

      {/* Back hint */}
      <Box marginTop={1}>
        <Text color="gray">[Esc=back]</Text>
      </Box>
    </Box>
  );
}
