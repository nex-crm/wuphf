/**
 * Pam placement contract — regression for the founder smoke-run gap #5:
 * the fixed Pam sprite sat OVER the wiki tab row because `.pam-wrap`
 * was an absolutely-positioned overlay anchored inside the tab bar.
 *
 * jsdom does not apply stylesheets, so this pins the CSS source itself:
 * `.pam-wrap` must be a flex child the tab row reserves space for
 * (margin-left: auto), never an absolute overlay floating over the tabs.
 */

import { describe, expect, it } from "vitest";

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// Vitest runs with cwd = web/ (import.meta.url is not file-scheme under
// the jsdom environment, so resolve from the project root instead).
const PAM_CSS = readFileSync(
  resolve(process.cwd(), "src/styles/pam.css"),
  "utf8",
);

/** Extract the body of the first top-level rule for `selector`, comments stripped. */
function ruleBody(selector: string): string {
  const start = PAM_CSS.indexOf(`${selector} {`);
  if (start < 0) {
    throw new Error(`rule ${selector} not found in pam.css`);
  }
  const open = PAM_CSS.indexOf("{", start);
  const close = PAM_CSS.indexOf("}", open);
  return PAM_CSS.slice(open + 1, close).replace(/\/\*[\s\S]*?\*\//g, "");
}

describe("pam.css placement contract", () => {
  it("renders Pam in-flow at the right of the tab bar, not as an overlay over the tabs", () => {
    const wrap = ruleBody(".pam-wrap");
    expect(wrap).not.toContain("position: absolute");
    expect(wrap).toContain("position: relative");
    expect(wrap).toContain("margin-left: auto");
  });

  it("keeps the sprite within the 42px tab-bar height (avatar + desk fit)", () => {
    // 40px avatar − 14px desk overlap + 16px desk = 42px (the bar height).
    const desk = ruleBody(".pam-desk");
    expect(desk).toContain("margin-top: -14px");
    expect(desk).toContain("height: 16px");
  });
});
