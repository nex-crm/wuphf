import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import { installInspector } from "./wuphf-inspector";
import "./styles.css";

// Dev-only: wires up "select to edit" + runtime-error surfacing for the WUPHF
// live preview. No-op (and tree-shaken) in the sealed production build.
installInspector();

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
