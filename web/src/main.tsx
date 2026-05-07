import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";

import { JoinPage } from "./components/join/JoinPage";
import { rootRoute, router } from "./lib/router";
import RootRoute from "./routes/RootRoute";
import "./styles/shadcn.css";
import "./styles/global.css";
import "./styles/layout.css";
import "./styles/messages.css";
import "./styles/agents.css";
import "./styles/search.css";
import "./styles/wiki-shell.css";
import "./styles/kbd.css";
import "./styles/console.css";

// Attach the root route's component at startup. Defining the component
// inside `lib/router.ts` would create a circular import: RootRoute reads
// route ids from `lib/router` to dispatch URL→store hydration, and
// `lib/router` would in turn import RootRoute. Attaching here keeps the
// router module dependency-free of route components.
rootRoute.update({ component: RootRoute });

// Hoisted out of the React tree so hooks called during the first render
// (e.g. useKeyboardShortcuts → useQueryClient) find a client immediately.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 2000 },
  },
});

declare global {
  interface Window {
    __wuphfBootDone?: () => void;
  }
}

function showFatalError(title: string, detail: string) {
  const existing = document.getElementById("fatal-error");
  if (existing) existing.remove();
  const box = document.createElement("div");
  box.id = "fatal-error";
  box.style.cssText =
    "position:fixed;top:0;left:0;right:0;padding:16px 20px;background:#fee;color:#900;font-family:-apple-system,BlinkMacSystemFont,sans-serif;font-size:13px;border-bottom:2px solid #900;z-index:10000;white-space:pre-wrap;word-break:break-word;max-height:50vh;overflow-y:auto;";
  const h = document.createElement("h2");
  h.textContent = title;
  h.style.cssText = "margin:0 0 8px 0;font-size:14px;";
  box.appendChild(h);
  const pre = document.createElement("pre");
  pre.textContent = detail;
  pre.style.cssText =
    "margin:8px 0 0;font-family:SFMono-Regular,Menlo,monospace;font-size:11px;color:#600;";
  box.appendChild(pre);
  document.body.appendChild(box);
}

try {
  const root = document.getElementById("root");
  if (!root) {
    throw new Error("#root element not found in DOM");
  }
  // The share handler redirects /join/<token> to /?invite=<token> so the
  // SPA's relative asset URLs need no path rewrite. A present-but-empty
  // value means the link was truncated; pass it through so JoinPage can
  // show the missing-token state instead of mounting the main app.
  const rawInvite = new URLSearchParams(window.location.search).get("invite");
  const inviteToken = rawInvite === null ? null : rawInvite.trim();
  createRoot(root).render(
    <QueryClientProvider client={queryClient}>
      {inviteToken === null ? (
        <RouterProvider router={router} />
      ) : (
        <JoinPage token={inviteToken} />
      )}
    </QueryClientProvider>,
  );
  window.__wuphfBootDone?.();
} catch (err) {
  const message = err instanceof Error ? err.message : String(err);
  const stack = err instanceof Error && err.stack ? err.stack : "";
  showFatalError("React failed to mount", `${message}\n\n${stack}`);
  window.__wuphfBootDone?.();
  // eslint-disable-next-line no-console
  console.error("[WUPHF boot]", err);
}
