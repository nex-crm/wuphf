import type { ComponentProps } from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "./cn.ts";

const buttonVariants = cva(
  "inline-flex h-9 items-center justify-center gap-2 rounded-md border px-3 text-sm font-medium transition-colors disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        primary:
          "border-transparent bg-primary text-primary-foreground hover:bg-foreground/90",
        secondary:
          "border-border bg-background text-foreground hover:bg-muted",
        ghost:
          "border-transparent bg-transparent text-foreground hover:bg-muted",
      },
      size: {
        sm: "h-8 px-2.5 text-xs",
        md: "h-9 px-3 text-sm",
      },
    },
    defaultVariants: {
      variant: "primary",
      size: "md",
    },
  },
);

export type ButtonProps = ComponentProps<"button"> & VariantProps<typeof buttonVariants>;

export function Button({ className, variant, size, type = "button", ...props }: ButtonProps) {
  return (
    <button
      className={cn(buttonVariants({ variant, size }), className)}
      type={type}
      {...props}
    />
  );
}
