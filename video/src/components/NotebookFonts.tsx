import { useEffect, useState } from "react";
import { continueRender, delayRender } from "remotion";

// Matches the fonts used by the real notebook surface (web/src/styles/notebook.css):
// Covered By Your Grace (handwriting display) + IBM Plex Serif (body).
const HREF =
  "https://fonts.googleapis.com/css2?family=Covered+By+Your+Grace&family=IBM+Plex+Serif:ital,wght@0,400;0,500;0,700;1,400;1,500&family=Geist+Mono:wght@400;500&display=swap";

let loadingPromise: Promise<void> | null = null;

function ensureFontsLoaded(): Promise<void> {
  if (loadingPromise) return loadingPromise;
  loadingPromise = (async () => {
    if (typeof document === "undefined") return;
    if (!document.querySelector(`link[data-nb-fonts="1"]`)) {
      const link = document.createElement("link");
      link.rel = "stylesheet";
      link.href = HREF;
      link.dataset.nbFonts = "1";
      document.head.appendChild(link);
    }
    if ("fonts" in document && typeof (document as Document).fonts.load === "function") {
      await Promise.all([
        document.fonts.load("400 40px 'Covered By Your Grace'"),
        document.fonts.load("400 17px 'IBM Plex Serif'"),
        document.fonts.load("500 17px 'IBM Plex Serif'"),
      ]);
    }
  })();
  return loadingPromise;
}

export const NotebookFonts: React.FC = () => {
  const [handle] = useState(() => delayRender("Loading notebook fonts"));
  useEffect(() => {
    let cancelled = false;
    ensureFontsLoaded()
      .catch(() => {})
      .finally(() => {
        if (!cancelled) continueRender(handle);
      });
    return () => {
      cancelled = true;
    };
  }, [handle]);
  return null;
};
