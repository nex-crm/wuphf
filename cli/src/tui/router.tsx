/**
 * View stack router for the TUI.
 * Renders the top-of-stack view and provides push/pop via context.
 */

import React, { createContext, useContext, useCallback } from "react";
import type { ReactNode } from "react";
import { Box, Text } from "ink";
import type { ViewEntry, Dispatch } from "./store.js";
import { useTheme } from "./theme.js";

// ── Router context ─────────────────────────────────────────────────

interface RouterContextValue {
  push: (view: ViewEntry) => void;
  pop: () => void;
  currentView: ViewEntry | undefined;
  viewStack: ViewEntry[];
}

const RouterContext = createContext<RouterContextValue>({
  push: () => {},
  pop: () => {},
  currentView: undefined,
  viewStack: [],
});

export function useRouter(): RouterContextValue {
  return useContext(RouterContext);
}

// ── View registry ──────────────────────────────────────────────────

type ViewComponent = React.FC<{ props?: Record<string, unknown> }>;

const viewRegistry = new Map<string, ViewComponent>();

/**
 * Register a view component for a given name.
 * Call this from the component layer (Agent 2) to wire up real views.
 */
export function registerView(name: string, component: ViewComponent): void {
  viewRegistry.set(name, component);
}

// ── Placeholder component ──────────────────────────────────────────

function PlaceholderView({ name }: { name: string }) {
  const t = useTheme();
  return (
    <Box flexDirection="column" padding={1}>
      <Text color={t.colors.primary} bold>
        {name}
      </Text>
      <Text color={t.colors.muted}>
        View not yet implemented. Press Escape to go back.
      </Text>
    </Box>
  );
}

// ── Supported view names ───────────────────────────────────────────

const SUPPORTED_VIEWS = new Set([
  "home",
  "help",
  "record-list",
  "record-detail",
  "ask-chat",
  "agent-list",
  "chat",
  "calendar",
  "orchestration",
  "generative",
  "task-board",
  "timeline",
  "insights",
]);

// ── Breadcrumb ─────────────────────────────────────────────────────

function Breadcrumb({ stack }: { stack: ViewEntry[] }) {
  const t = useTheme();

  if (stack.length <= 1) return null;

  return (
    <Box marginBottom={1}>
      {stack.map((entry, i) => {
        const isLast = i === stack.length - 1;
        return (
          <React.Fragment key={`${entry.name}-${i}`}>
            <Text
              color={
                isLast ? t.breadcrumb.activeColor : t.breadcrumb.color
              }
              bold={isLast}
            >
              {entry.name}
            </Text>
            {!isLast && (
              <Text color={t.breadcrumb.color}>
                {t.breadcrumb.separator}
              </Text>
            )}
          </React.Fragment>
        );
      })}
    </Box>
  );
}

// ── Router component ───────────────────────────────────────────────

interface RouterProps {
  viewStack: ViewEntry[];
  dispatch: Dispatch;
  children?: ReactNode;
}

export function Router({ viewStack, dispatch, children }: RouterProps) {
  const push = useCallback(
    (view: ViewEntry) => dispatch({ type: "PUSH_VIEW", view }),
    [dispatch],
  );

  const pop = useCallback(
    () => dispatch({ type: "POP_VIEW" }),
    [dispatch],
  );

  const currentView = viewStack[viewStack.length - 1];

  const contextValue: RouterContextValue = {
    push,
    pop,
    currentView,
    viewStack,
  };

  // Resolve the component for the current view
  let ViewContent: React.ReactElement;
  if (currentView) {
    const registered = viewRegistry.get(currentView.name);
    if (registered) {
      const Component = registered;
      ViewContent = <Component props={currentView.props} />;
    } else if (SUPPORTED_VIEWS.has(currentView.name)) {
      ViewContent = <PlaceholderView name={currentView.name} />;
    } else {
      ViewContent = <PlaceholderView name={`unknown: ${currentView.name}`} />;
    }
  } else {
    ViewContent = <PlaceholderView name="home" />;
  }

  return (
    <RouterContext.Provider value={contextValue}>
      <Box flexDirection="column">
        <Breadcrumb stack={viewStack} />
        {ViewContent}
        {children}
      </Box>
    </RouterContext.Provider>
  );
}
