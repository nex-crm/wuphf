import { useEffect, useState } from "react";
import { createRoute } from "@tanstack/react-router";
import { RefreshDouble } from "iconoir-react";

import { useBrokerBootstrap } from "../../bootstrap/useBrokerBootstrap.ts";
import { Button } from "../../ui/Button.tsx";
import { Card } from "../../ui/Card.tsx";
import { StatusBadge } from "../../ui/StatusBadge.tsx";
import { rootRoute } from "./__root.tsx";

export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: IndexRoute,
});

export function IndexRoute() {
  const bootstrap = useBrokerBootstrap();
  const [version, setVersion] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    void window.wuphf
      .getAppVersion()
      .then((response) => {
        if (active) setVersion(response.version);
      })
      .catch(() => {
        // Swallow: the version chip is a nice-to-have. An IPC failure
        // here should not crash the renderer or surface as an unhandled
        // rejection; the chip simply stays hidden.
      });
    return () => {
      active = false;
    };
  }, []);

  return (
    <div className="mx-auto max-w-3xl space-y-5">
      <header className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-normal">Desktop status</h1>
          <p className="mt-1 text-sm text-muted-foreground">Loopback broker connection</p>
        </div>
        {version !== null && <StatusBadge label={`v${version}`} tone="neutral" />}
      </header>

      <Card aria-label="Broker status" title="Broker">
        {bootstrap.status === "ready" && (
          <div className="space-y-3">
            <StatusBadge label="Ready" tone="ok" />
            <p className="break-all text-sm text-muted-foreground">{bootstrap.brokerUrl}</p>
          </div>
        )}
        {bootstrap.status === "loading" && (
          <div className="space-y-3">
            <StatusBadge busy label="Starting" tone="pending" />
            <p className="text-sm text-muted-foreground">Waiting for the broker listener.</p>
          </div>
        )}
        {bootstrap.status === "error" && (
          <div className="space-y-4">
            <StatusBadge label="Unavailable" tone="error" />
            <p className="text-sm text-muted-foreground">{bootstrap.error}</p>
            <Button onClick={bootstrap.retry} variant="secondary">
              <RefreshDouble aria-hidden="true" height={16} width={16} />
              Retry
            </Button>
          </div>
        )}
      </Card>
    </div>
  );
}
