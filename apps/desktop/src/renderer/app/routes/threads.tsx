import { createRoute } from "@tanstack/react-router";

import { WorkBoard } from "../../work-board/WorkBoard.tsx";
import { rootRoute } from "./__root.tsx";

export const threadsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/threads",
  component: ThreadsRoute,
});

export function ThreadsRoute() {
  // No selection handler in R2 — the thread-detail surface ships with
  // R3. Without `onSelectThread`, the cards render as plain articles.
  return <WorkBoard />;
}
