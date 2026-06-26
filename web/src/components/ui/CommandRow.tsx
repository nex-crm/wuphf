import { useState } from "react";

import { showNotice } from "./Toast";

/**
 * CommandRow renders a shell command in a monospace box with a Copy button.
 *
 * It is copy-paste ONLY — wuphf deliberately never runs install/shell
 * commands on the user's behalf from Settings panels (see the Local LLMs
 * section copy: "install commands are copy-paste only"). Shared by the
 * Local LLMs panel and the Nex integration panel so that contract — and
 * the styling — stays in one place.
 */
export function CommandRow({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      showNotice("Copy failed — select the text and copy manually.", "error");
    }
  };
  return (
    <div
      style={{
        display: "flex",
        gap: 8,
        alignItems: "center",
        padding: "6px 8px",
        background: "var(--bg-card-soft, var(--bg-card))",
        border: "1px solid var(--border-light)",
        borderRadius: 4,
        marginTop: 6,
      }}
    >
      <code
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          flex: 1,
          overflowX: "auto",
          whiteSpace: "nowrap",
        }}
      >
        {command}
      </code>
      <button
        type="button"
        className="btn btn-ghost btn-sm"
        onClick={onCopy}
        style={{ flexShrink: 0 }}
      >
        {copied ? "Copied" : "Copy"}
      </button>
    </div>
  );
}
