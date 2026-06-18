import "@mantine/core/styles.css";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { MantineProvider } from "@mantine/core";
import { Refine } from "@refinedev/core";

import { App } from "./App";
import { bridgeDataProvider } from "./bridgeDataProvider";
import "./styles.css";

// NOTE: the live-preview "select to edit" + error-capture inspector is injected
// by vite.config.ts (dev only), NOT imported here — so it survives any rewrite
// of this entry file by the App Builder and never ships in the sealed build.
//
// Provider setup — keep this shape:
//   - MantineProvider forceColorScheme="light": apps render on white inside
//     WUPHF. Pinning the scheme also sidesteps Mantine's localStorage color
//     manager, which the sealed sandbox (opaque origin) blocks. Mantine guards
//     that access, so it degrades safely either way.
//   - <Refine dataProvider={bridgeDataProvider}>: routes every refine data hook
//     through the WUPHF bridge. NO routerProvider (no app-owned URL in the
//     sandbox), NO authProvider (the bridge runs as the signed-in user), NO
//     localStorage-backed providers. Declare your resources here.
const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <MantineProvider forceColorScheme="light">
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
