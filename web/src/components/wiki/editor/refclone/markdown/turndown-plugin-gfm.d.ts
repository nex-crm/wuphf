// Ambient types for `turndown-plugin-gfm`, which ships no type declarations.
// Declaring the minimal surface the vendored markdown library uses keeps the
// import type-safe without an ignore comment.
declare module "turndown-plugin-gfm" {
  import type TurndownService from "turndown";

  /** Turndown plugin adding GFM support (tables, strikethrough, task lists). */
  export const gfm: TurndownService.Plugin;
  export const tables: TurndownService.Plugin;
  export const strikethrough: TurndownService.Plugin;
  export const taskListItems: TurndownService.Plugin;
  export const highlightedCodeBlock: TurndownService.Plugin;
}
