/// <reference types="vite/client" />

// `turndown-plugin-gfm` ships no type definitions. Declare the small surface we
// use (the `gfm` plugin passed to `TurndownService#use`) so importing it stays
// type-safe without an ignore comment. A turndown plugin is any function that
// receives the service instance.
declare module "turndown-plugin-gfm" {
  import type TurndownService from "turndown";

  type TurndownPlugin = (service: TurndownService) => void;
  export const gfm: TurndownPlugin;
  export const tables: TurndownPlugin;
  export const strikethrough: TurndownPlugin;
  export const taskListItems: TurndownPlugin;
  export const highlightedCodeBlock: TurndownPlugin;
}
