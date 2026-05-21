import { forwardRef, type ReactNode } from "react";

export interface RailHint {
  label: string;
  y: number;
}

export interface RailIconButtonProps {
  testId: string;
  label: string;
  active: boolean;
  onClick: () => void;
  onHint: (hint: RailHint | null) => void;
  children: ReactNode;
  dataAttrs?: Record<string, string>;
  ariaExpanded?: boolean;
  /** Override the unselected icon colour. Defaults to the neutral rail tone. */
  idleColor?: string;
}

/**
 * 36×36 icon button used inside the workspace rail (tools + footer). All
 * rail buttons share the same hit-target, the cyan-400 active fill, and
 * portal a tooltip via the `onHint` callback so the hint can escape the
 * rail's clipped overflow.
 */
export const RailIconButton = forwardRef<
  HTMLButtonElement,
  RailIconButtonProps
>(function RailIconButton(
  {
    testId,
    label,
    active,
    onClick,
    onHint,
    children,
    dataAttrs,
    ariaExpanded,
    idleColor,
  },
  ref,
) {
  return (
    <button
      ref={ref}
      type="button"
      data-testid={testId}
      aria-label={label}
      aria-current={active ? "page" : undefined}
      aria-expanded={ariaExpanded}
      onClick={onClick}
      onMouseEnter={(e) => {
        const rect = (
          e.currentTarget as HTMLButtonElement
        ).getBoundingClientRect();
        onHint({ label, y: rect.top + rect.height / 2 });
      }}
      onMouseLeave={() => onHint(null)}
      onFocus={(e) => {
        const rect = (
          e.currentTarget as HTMLButtonElement
        ).getBoundingClientRect();
        onHint({ label, y: rect.top + rect.height / 2 });
      }}
      onBlur={() => onHint(null)}
      {...(dataAttrs ?? {})}
      style={{
        width: 36,
        height: 36,
        borderRadius: 10,
        border: "1px solid transparent",
        background: active ? "var(--cyan-400)" : "transparent",
        color: active
          ? "#0a1f24"
          : (idleColor ?? "var(--neutral-300, rgba(255,255,255,0.7))"),
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        cursor: "pointer",
        transition: "background 0.16s ease, color 0.16s ease",
      }}
      onMouseOver={(e) => {
        if (!active)
          (e.currentTarget as HTMLButtonElement).style.background =
            "rgba(255,255,255,0.12)";
      }}
      onMouseOut={(e) => {
        if (!active)
          (e.currentTarget as HTMLButtonElement).style.background =
            "transparent";
      }}
    >
      {children}
    </button>
  );
});
