import "./styles/globals.css";

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./app/App.tsx";
import { renderStatusPaneFallback } from "./status-pane.tsx";

const rootElement = document.querySelector<HTMLElement>("#app");
if (rootElement === null) {
  throw new Error("Missing #app mount point");
}

renderStatusPaneFallback(rootElement);
createRoot(rootElement).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
