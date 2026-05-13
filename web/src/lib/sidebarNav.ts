import { router } from "./router";

/**
 * Map a sidebar app entry id to its TanStack route target. Both the
 * expanded (`AppList`) and collapsed (`CollapsedSidebar`) sidebars call
 * into this so the canonical route map lives in one place — adding a
 * new first-class app surface (or moving an existing app id off the
 * generic `/apps/$appId` route) is a single edit instead of two
 * easy-to-drift copies.
 */
export function navigateToSidebarApp(appId: string): void {
  if (appId === "wiki") {
    void router.navigate({ to: "/wiki" });
    return;
  }
  if (appId === "inbox") {
    void router.navigate({ to: "/inbox" });
    return;
  }
  void router.navigate({
    to: "/apps/$appId",
    params: { appId },
  });
}
