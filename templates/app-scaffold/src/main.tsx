import "@mantine/core/styles.css";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createTheme, MantineProvider } from "@mantine/core";
import { Refine } from "@refinedev/core";

import { App } from "./App";
import { bridgeDataProvider } from "./bridgeDataProvider";
import "./styles.css";

// A real theme is the single highest-leverage design move — default Mantine is
// the #1 "AI-generated" tell, and the publish step REJECTS an app that ships it
// unchanged. CUSTOMIZE this for the tool you are building (a calm digest vs. a
// dense console vs. a triage queue want different accents/rhythm) — see DESIGN.md
// §2. Keep it a deliberate override, not the default.
const appTheme = createTheme({
  primaryColor: "indigo",
  defaultRadius: "md",
  fontFamilyMonospace:
    "ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace",
  headings: { fontWeight: "650", sizes: { h1: { fontSize: "1.6rem" } } },
  spacing: { md: "1rem" },
});

// NOTE: the live-preview "select to edit" + error-capture inspector is injected
// by vite.config.ts (dev only), NOT imported here — so it survives any rewrite
// of this entry file by the App Builder and never ships in the sealed build.
//
// Provider setup — keep this shape:
//   - MantineProvider forceColorScheme="dark": apps render on the operator's
//     muted-dark surface so they BLEND into the WUPHF shell instead of being a
//     white card on a dark surface. Pinning the scheme also sidesteps Mantine's
//     localStorage color manager, which the sealed sandbox (opaque origin)
//     blocks. Mantine guards that access, so it degrades safely either way.
//   - <Refine dataProvider={bridgeDataProvider}>: routes every refine data hook
//     through the WUPHF bridge. NO routerProvider (no app-owned URL in the
//     sandbox), NO authProvider (the bridge runs as the signed-in user), NO
//     localStorage-backed providers. Declare your resources here.
const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <MantineProvider theme={appTheme} forceColorScheme="dark">
        <Refine
          dataProvider={bridgeDataProvider}
          resources={[
            { name: "tasks" },
            { name: "members" },
            { name: "emails" },
          ]}
          options={{ disableTelemetry: true, warnWhenUnsavedChanges: false }}
        >
          <App />
        </Refine>
      </MantineProvider>
    </StrictMode>,
  );
}
