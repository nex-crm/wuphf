import type { ComponentProps, ReactNode } from "react";

import { cn } from "./cn.ts";

export interface CardProps extends Omit<ComponentProps<"section">, "title"> {
  readonly title?: ReactNode;
  readonly description?: ReactNode;
}

export function Card({ className, title, description, children, ...props }: CardProps) {
  return (
    <section
      className={cn(
        "rounded-lg border border-border bg-background p-5 shadow-sm",
        className,
      )}
      {...props}
    >
      {(title !== undefined || description !== undefined) && (
        <header className="mb-4 space-y-1">
          {title !== undefined && (
            <h2 className="text-base font-semibold leading-6 text-foreground">{title}</h2>
          )}
          {description !== undefined && (
            <p className="text-sm leading-5 text-muted-foreground">{description}</p>
          )}
        </header>
      )}
      {children}
    </section>
  );
}
