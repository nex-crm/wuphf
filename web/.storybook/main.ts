import type { StorybookConfig } from "@storybook/react-vite";
import { mergeConfig } from "vite";

import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const config: StorybookConfig = {
  stories: ["../src/**/*.stories.@(ts|tsx|mdx)"],
  staticDirs: ["../public"],
  addons: ["@storybook/addon-a11y"],
  framework: {
    name: "@storybook/react-vite",
    options: {},
  },
  typescript: {
    reactDocgen: "react-docgen-typescript",
  },
  viteFinal: async (vite) =>
    mergeConfig(vite, {
      resolve: {
        alias: {
          "@": path.resolve(__dirname, "../src"),
        },
      },
    }),
};

export default config;
