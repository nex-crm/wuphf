// Verify Pam the librarian wears one consistent face everywhere.
//
// Pam's Wiki desk avatar uses slug "pam" (→ hybridPam sprite), but every
// wiki edit she makes is committed under the "archivist" git identity, so
// her bylines/audit/history render `<PixelAvatar slug="archivist" />`.
// Before this fix "archivist" was absent from the slug map and fell back to
// a procedural portrait — a different face. This probe paints the REAL
// `drawPixelAvatar` output for each identity so the match is visible.
//
// Why a probe and not the live Wiki view: the wiki article route on `main`
// currently hits a pre-existing "Maximum update depth exceeded" render loop
// (channel-chat/Shell, tracked separately, unrelated to this change) that
// prevents the harness from mounting the article. The fix here is a pure
// function (resolvePortraitSprite/drawPixelAvatar), so rendering the sprite
// output directly is a faithful — and more precise — verification.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh pam-avatar-consistency <pr-number>

import process from "node:process";

import { launchBrowser, shotElement } from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const BASE = process.env.BASE_URL ?? "http://localhost:5273";

const { browser, page, errors } = await launchBrowser({
  viewport: { width: 960, height: 520 },
});

// Establish the dev-server origin so the module import below resolves, then
// discard the SPA root entirely (it would otherwise spin in the unrelated
// render loop). The avatar probe is independent of React.
await page.goto(`${BASE}/`, { waitUntil: "domcontentloaded" });

await page.evaluate(async (slugs) => {
  document.documentElement.innerHTML =
    "<head></head><body><div id='probe'></div></body>";
  const { drawPixelAvatar, resolvePortraitSprite, getAgentColor } =
    await import("/src/lib/pixelAvatar.ts");

  const probe = document.getElementById("probe");
  Object.assign(probe.style, {
    display: "flex",
    gap: "28px",
    padding: "40px",
    background: "#0d1117",
    fontFamily:
      "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
    alignItems: "flex-start",
  });

  for (const { slug, note } of slugs) {
    const cell = document.createElement("div");
    Object.assign(cell.style, {
      display: "flex",
      flexDirection: "column",
      alignItems: "center",
      gap: "10px",
      width: "150px",
      textAlign: "center",
    });

    const ring = document.createElement("div");
    const accent = getAgentColor(slug);
    Object.assign(ring.style, {
      width: "112px",
      height: "112px",
      display: "grid",
      placeItems: "center",
      borderRadius: "16px",
      background: "#161b22",
      border: `2px solid ${accent}`,
    });
    const canvas = document.createElement("canvas");
    canvas.style.imageRendering = "pixelated";
    drawPixelAvatar(canvas, slug, 96);
    ring.appendChild(canvas);

    const label = document.createElement("div");
    label.textContent = `slug "${slug}"`;
    Object.assign(label.style, { color: "#e6edf3", fontSize: "13px", fontWeight: "600" });

    const sub = document.createElement("div");
    const sprite = resolvePortraitSprite(slug);
    sub.textContent = `→ ${sprite.id}`;
    Object.assign(sub.style, { color: "#8b949e", fontSize: "12px" });

    const tag = document.createElement("div");
    tag.textContent = note;
    Object.assign(tag.style, { color: accent, fontSize: "11px" });

    cell.append(ring, label, sub, tag);
    probe.appendChild(cell);
  }
}, [
  { slug: "pam", note: "Wiki desk" },
  { slug: "archivist", note: "wiki byline identity" },
  { slug: "librarian", note: "librarian label" },
  { slug: "ceo", note: "different agent" },
]);

await page.waitForTimeout(300);
await shotElement(page, "#probe", OUT, "01-pam-one-face-everywhere");

console.log(`captured 1 screenshot to ${OUT}`);
// The SPA root we discarded leaves render-loop noise in the console; that is
// the pre-existing main bug, not this change. Only surface genuine probe
// failures (a pageerror from the import/draw path).
const real = errors.filter((e) => e.startsWith("[pageerror]"));
if (real.length > 0) console.error("probe errors:", real);
await browser.close();
