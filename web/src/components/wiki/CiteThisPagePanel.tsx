import { useState } from "react";

/** Right-rail "Cite this page" panel: shows the canonical wikilink + copy button. */

interface CiteThisPagePanelProps {
  slug: string;
}

export default function CiteThisPagePanel({ slug }: CiteThisPagePanelProps) {
  const [copied, setCopied] = useState(false);
  const wikilink = `[[${slug}]]`;

  async function handleCopy() {
    try {
      const clip = (
        typeof navigator !== "undefined" ? navigator.clipboard : null
      ) as { writeText?: (v: string) => Promise<void> } | null;
      if (clip && typeof clip.writeText === "function") {
        await clip.writeText(wikilink);
      }
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="wk-cite-panel">
      <h4>Cite this page</h4>
      <div className="wk-wikilink-code">
        <code>{wikilink}</code>
        <button
          type="button"
          className="wk-copy-btn"
          onClick={handleCopy}
          aria-label="Copy wikilink"
        >
          {copied ? "copied" : "copy"}
        </button>
      </div>
      <div className="wk-hint">Paste this in any article to link here.</div>
    </div>
  );
}
