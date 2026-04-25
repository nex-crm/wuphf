import type { ReactNode } from "react";

/** Italic cross-reference block at the top of an article. */

interface HatnoteProps {
  children: ReactNode;
}

export default function Hatnote({ children }: HatnoteProps) {
  return <div className="wk-hatnote">{children}</div>;
}
