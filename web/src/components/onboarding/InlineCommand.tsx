/**
 * InlineCommand — a mono "terminal" row with a copy button. Used by the
 * guided provider setup so every step that carries a shell command renders a
 * one-click-copy affordance, in WUPHF tokens.
 *
 * Copy plumbing matches PrePickScreen's #932 copy-sign-in path:
 * navigator.clipboard.writeText resolves only in secure contexts, so the
 * catch falls back to an info toast telling the user to copy the command by
 * hand. No silent failures.
 */

import { useCallback } from "react";

import { showNotice } from "../ui/Toast";

interface InlineCommandProps {
  command: string;
  /** Optional accessible label override. Defaults to "Copy command". */
  copyLabel?: string;
  "data-testid"?: string;
}

export function InlineCommand({
  command,
  copyLabel = "Copy command",
  "data-testid": dataTestId,
}: InlineCommandProps) {
  const handleCopy = useCallback(() => {
    const fallback = `Copy this command by hand: ${command}`;
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      navigator.clipboard
        .writeText(command)
        .then(() => showNotice(`Copied: ${command}`, "success"))
        .catch(() => showNotice(fallback, "info"));
    } else {
      showNotice(fallback, "info");
    }
  }, [command]);

  return (
    <div className="pre-pick-guide-command" data-testid={dataTestId}>
      <code className="pre-pick-guide-command-text">{command}</code>
      <button
        type="button"
        className="pre-pick-guide-command-copy"
        onClick={handleCopy}
        aria-label={copyLabel}
      >
        Copy
      </button>
    </div>
  );
}
