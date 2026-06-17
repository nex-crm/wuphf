import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import "./styles.css";

// NOTE: the live-preview "select to edit" + error-capture inspector is injected
// by vite.config.ts (dev only), NOT imported here — so it survives any rewrite
// of this entry file by the App Builder and never ships in the sealed build.

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
