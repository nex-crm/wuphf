import { useEffect, useState } from "react";
import { continueRender, delayRender } from "remotion";

const HREF =
  "https://fonts.googleapis.com/css2?family=Fraunces:opsz,wght@9..144,500;9..144,600&family=Source+Serif+4:ital,opsz,wght@0,8..60,400;0,8..60,500;1,8..60,400&family=Geist+Mono:wght@400;500&display=swap";

let loadingPromise: Promise<void> | null = null;

function ensureFontsLoaded(): Promise<void> {
  if (loadingPromise) return loadingPromise;
  loadingPromise = (async () => {
    if (typeof document === "undefined") return;
    if (!document.querySelector(`link[data-wk-fonts="1"]`)) {
      const link = document.createElement("link");
      link.rel = "stylesheet";
      link.href = HREF;
      link.dataset.wkFonts = "1";
      document.head.appendChild(link);
    }
    // Force the browser to actually fetch the weights we use before the
    // first frame that needs them renders, otherwise the first paint
    // shows a system serif.
    if ("fonts" in document && typeof (document as Document).fonts.load === "function") {
      await Promise.all([
        // Fraunces (display) — title 52 + section heading 28
        document.fonts.load("500 52px Fraunces"),
        document.fonts.load("500 28px Fraunces"),
        // Source Serif 4 — body 18 + strapline/hatnote 15 + bold via <strong>
        document.fonts.load("400 18px 'Source Serif 4'"),
        document.fonts.load("400 15px 'Source Serif 4'"),
        document.fonts.load("italic 400 18px 'Source Serif 4'"),
        document.fonts.load("italic 400 15px 'Source Serif 4'"),
        document.fonts.load("italic 400 12px 'Source Serif 4'"),
        document.fonts.load("500 18px 'Source Serif 4'"),
        // Geist Mono — wikilinks & mono chips
        document.fonts.load("400 13px 'Geist Mono'"),
        document.fonts.load("400 11px 'Geist Mono'"),
        document.fonts.load("400 12px 'Geist Mono'"),
      ]);
      // Also wait for every other font in the stylesheet — catches
      // variable-font axis combinations (Fraunces opsz) that a single
      // `.load(weight size family)` call doesn't cover.
      await (document as Document).fonts.ready;
    }
  })();
  return loadingPromise;
}

export const WikiFonts: React.FC = () => {
  const [handle] = useState(() => delayRender("Loading wiki fonts"));
  useEffect(() => {
    let cancelled = false;
    ensureFontsLoaded()
      .catch(() => {
        // Fall back to system serif silently — never block render.
      })
      .finally(() => {
        if (!cancelled) continueRender(handle);
      });
    return () => {
      cancelled = true;
    };
  }, [handle]);
  return null;
};
