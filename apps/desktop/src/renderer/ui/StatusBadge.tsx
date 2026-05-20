import type { ComponentProps } from "react";

import { cn } from "./cn.ts";

export type StatusBadgeTone = "neutral" | "pending" | "ok" | "error";

export interface StatusBadgeProps extends Omit<ComponentProps<"span">, "children"> {
  readonly label: string;
  readonly tone: StatusBadgeTone;
  readonly busy?: boolean;
}

const toneClassName: Record<StatusBadgeTone, string> = {
  neutral: "border-border bg-muted text-muted-foreground",
  pending: "border-amber-200 bg-amber-50 text-amber-800",
  ok: "border-emerald-200 bg-emerald-50 text-emerald-800",
  error: "border-red-200 bg-red-50 text-red-800",
};

export function StatusBadge({ className, label, tone, busy = false, ...props }: StatusBadgeProps) {
  return (
    <span
      aria-busy={busy}
      className={cn(
        "inline-flex h-6 items-center rounded-full border px-2 text-xs font-medium",
        toneClassName[tone],
        className,
      )}
      data-tone={tone}
      role="status"
      {...props}
    >
      {label}
    </span>
  );
}
