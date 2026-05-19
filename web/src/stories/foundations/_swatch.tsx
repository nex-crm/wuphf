import type { CSSProperties, ReactNode } from "react";

export function Grid({
  children,
  cols = 4,
  style,
}: {
  children: ReactNode;
  cols?: number;
  style?: CSSProperties;
}) {
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
        gap: 12,
        padding: 16,
        maxWidth: 920,
        ...style,
      }}
    >
      {children}
    </div>
  );
}

export function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <section style={{ padding: "16px 16px 4px" }}>
      <header style={{ marginBottom: 8 }}>
        <h3
          style={{
            margin: 0,
            fontSize: 13,
            fontWeight: 600,
            color: "var(--text)",
            letterSpacing: "0.02em",
            textTransform: "uppercase",
          }}
        >
          {title}
        </h3>
        {description ? (
          <p
            style={{
              margin: "4px 0 0",
              fontSize: 12,
              color: "var(--text-secondary)",
            }}
          >
            {description}
          </p>
        ) : null}
      </header>
      {children}
    </section>
  );
}

interface SwatchProps {
  /** CSS custom property name, e.g. "--accent". */
  token: string;
  /** Optional override of the visible label below the swatch. */
  label?: string;
  /** Optional alias (e.g. "--purple → --tertiary-400"). */
  alias?: string;
  /** Force light/dark foreground on the chip — defaults to auto. */
  fg?: "light" | "dark" | "auto";
  /** Swatch height. */
  height?: number;
}

export function Swatch({ token, label, alias, fg = "auto", height = 56 }: SwatchProps) {
  const display = label ?? token.replace(/^--/, "");
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 6,
        fontSize: 11,
        lineHeight: 1.4,
      }}
    >
      <div
        style={{
          height,
          borderRadius: 6,
          border: "1px solid var(--border)",
          background: `var(${token})`,
          color:
            fg === "light"
              ? "#fff"
              : fg === "dark"
                ? "#000"
                : "transparent",
        }}
        aria-label={display}
      />
      <div>
        <div
          style={{
            fontFamily: "var(--font-mono)",
            color: "var(--text)",
          }}
        >
          {display}
        </div>
        {alias ? (
          <div
            style={{
              color: "var(--text-tertiary)",
              fontFamily: "var(--font-mono)",
            }}
          >
            {alias}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export function TokenRow({
  token,
  preview,
  note,
}: {
  token: string;
  preview: ReactNode;
  note?: ReactNode;
}) {
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "200px 1fr 160px",
        alignItems: "center",
        gap: 16,
        padding: "10px 0",
        borderBottom: "1px solid var(--border-light)",
        fontSize: 12,
      }}
    >
      <code
        style={{
          fontFamily: "var(--font-mono)",
          color: "var(--text)",
          fontSize: 12,
        }}
      >
        {token}
      </code>
      <div>{preview}</div>
      <div style={{ color: "var(--text-tertiary)" }}>{note}</div>
    </div>
  );
}
