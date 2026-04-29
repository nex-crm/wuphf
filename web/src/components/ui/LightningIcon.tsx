interface LightningIconProps {
  size?: number;
  title?: string;
}

/**
 * Inline monochrome lightning bolt. Replaces the cross-OS-inconsistent ⚡
 * emoji. Inherits `currentColor` so it tracks text color on hover/active.
 */
export function LightningIcon({ size = 16, title }: LightningIconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="currentColor"
      role={title ? "img" : "presentation"}
      aria-hidden={title ? undefined : true}
      aria-label={title}
      style={{
        display: "inline-block",
        flexShrink: 0,
        verticalAlign: "-0.15em",
      }}
    >
      <path d="M9.5 1L3 9h4l-1 6 6.5-8H8.5l1-6z" />
    </svg>
  );
}
