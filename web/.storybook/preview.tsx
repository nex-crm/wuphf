import { useEffect } from "react";
import type { Decorator, Preview } from "@storybook/react-vite";

import { DEFAULT_THEME, THEMES, type Theme } from "../src/lib/themes";

import "../src/styles/fonts.css";
import "../src/styles/shadcn.css";
import "../src/styles/global.css";
import "../src/styles/layout.css";
import "../src/styles/messages.css";
import "../src/styles/agents.css";
import "../src/styles/search.css";
import "../src/styles/command.css";
import "../src/styles/wiki-shell.css";
import "../src/styles/wiki.css";
import "../src/styles/kbd.css";
import "../src/styles/pixel-skill-card.css";
import "../src/styles/lifecycle.css";
import "../src/styles/onboarding.css";
import "../src/styles/office-tour.css";
import "../src/styles/office-tour-slides.css";
import "../src/styles/pam.css";
import "../src/styles/rich-artifacts.css";

const THEME_LINK_ID = "wuphf-theme-link";

function applyTheme(theme: Theme) {
  const def = THEMES.find((t) => t.id === theme) ?? THEMES[0];
  document.documentElement.setAttribute("data-theme", def.id);
  let link = document.getElementById(THEME_LINK_ID) as HTMLLinkElement | null;
  if (!link) {
    link = document.createElement("link");
    link.id = THEME_LINK_ID;
    link.rel = "stylesheet";
    document.head.appendChild(link);
  }
  if (link.href.split("?")[0]?.endsWith(def.cssPath) !== true) {
    link.href = def.cssPath;
  }
}

const withTheme: Decorator = (Story, context) => {
  const theme = (context.globals.theme ?? DEFAULT_THEME) as Theme;
  useEffect(() => {
    applyTheme(theme);
  }, [theme]);
  return <Story />;
};

const preview: Preview = {
  parameters: {
    layout: "centered",
    controls: { expanded: true },
    options: {
      storySort: {
        order: [
          "About",
          ["Introduction"],
          "Design System",
          [
            "Foundations",
            [
              "Color",
              "Typography",
              "Spacing",
              "Radii",
              "Shadows",
              "Motion",
              "Iconography",
            ],
            "Atoms",
            "Molecules",
            "Organisms",
            "Templates",
          ],
          "Patterns",
          "Sidebar",
          ["Modules", "Full view"],
          "Features",
          ["Agents", "Notebook", "Wiki"],
          "Pages",
          "*",
        ],
        method: "alphabetical",
      },
    },
    backgrounds: {
      default: "surface",
      values: [
        { name: "surface", value: "var(--surface, #ffffff)" },
        { name: "elevated", value: "var(--surface-2, #f5f5f5)" },
        { name: "ink", value: "var(--ink, #1e1f1f)" },
      ],
    },
  },
  globalTypes: {
    theme: {
      name: "Theme",
      description: "Wuphf theme",
      defaultValue: DEFAULT_THEME,
      toolbar: {
        icon: "paintbrush",
        items: THEMES.map((t) => ({ value: t.id, title: t.name })),
        dynamicTitle: true,
      },
    },
  },
  decorators: [withTheme],
};

export default preview;
