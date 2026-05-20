import type { ReactNode } from "react";
import { HomeSimple, ServerConnection, ViewColumns3 } from "iconoir-react";

import { useBrokerBootstrap } from "../bootstrap/useBrokerBootstrap.ts";
import { StatusBadge, type StatusBadgeTone } from "../ui/StatusBadge.tsx";

export interface AppShellProps {
  readonly children: ReactNode;
}

export function AppShell({ children }: AppShellProps) {
  const bootstrap = useBrokerBootstrap();
  const tone = brokerTone(bootstrap.status);
  const busy = bootstrap.status === "loading";

  return (
    <div className="grid min-h-screen grid-cols-[220px_1fr] bg-muted text-foreground">
      <aside className="border-r border-border bg-background px-4 py-5">
        <div className="mb-6 flex items-center gap-2 text-sm font-semibold">
          <ServerConnection aria-hidden="true" height={18} width={18} />
          <span>WUPHF</span>
        </div>
        <nav aria-label="Primary" className="space-y-1">
          <a
            className="flex h-9 items-center gap-2 rounded-md px-2 text-sm text-foreground hover:bg-muted"
            href="#/"
          >
            <HomeSimple aria-hidden="true" height={17} width={17} />
            Status
          </a>
          <a
            className="flex h-9 items-center gap-2 rounded-md px-2 text-sm text-foreground hover:bg-muted"
            href="#/threads"
          >
            <ViewColumns3 aria-hidden="true" height={17} width={17} />
            Work
          </a>
        </nav>
        <div className="mt-6">
          <StatusBadge busy={busy} label={`Broker ${bootstrap.status}`} tone={tone} />
        </div>
      </aside>
      <main className="min-w-0 px-8 py-7">{children}</main>
    </div>
  );
}

function brokerTone(status: "loading" | "ready" | "error"): StatusBadgeTone {
  if (status === "ready") return "ok";
  if (status === "error") return "error";
  return "pending";
}
