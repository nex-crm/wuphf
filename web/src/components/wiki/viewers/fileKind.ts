/**
 * Pure file-classification helpers for the cabinet viewer dispatcher.
 *
 * Kept free of React so the tree, the wiki main pane, and tests can decide
 * "is this path a viewable file, and which viewer handles it" without pulling
 * in the (lazily-loaded) viewer modules. The dispatcher (`FileViewer`) maps
 * each `FileKind` to a `React.lazy` viewer; everything else (folders, `.md`
 * pages, and binaries with no dedicated viewer) routes to the fallback.
 *
 * The extension sets mirror what the individual viewers actually accept
 * (see ImageViewer / MediaViewer / SourceViewer / etc.) so routing never
 * mounts a viewer that would immediately fall through to its own empty state.
 */

/**
 * The viewer family a file path resolves to. `fallback` is the catch-all for
 * binaries (and anything unrecognised): a filename + download + open-in-new-tab
 * card rather than a blank pane. `markdown` is intentionally NOT a kind here —
 * `.md` pages render through the existing article view, not a file viewer.
 */
export type FileKind =
  | "image"
  | "media"
  | "pdf"
  | "csv"
  | "xlsx"
  | "docx"
  | "pptx"
  | "notebook"
  | "mermaid"
  | "source"
  | "fallback";

const IMAGE_EXTS = new Set([
  "png",
  "jpg",
  "jpeg",
  "gif",
  "webp",
  "avif",
  "svg",
  "bmp",
  "ico",
]);

const MEDIA_EXTS = new Set([
  // video
  "mp4",
  "webm",
  "mov",
  "m4v",
  // audio
  "mp3",
  "wav",
  "ogg",
  "m4a",
  "aac",
  "flac",
]);

// CsvViewer is comma-delimited only, so `.tsv` routes to the source viewer
// (where it renders as readable plain text) rather than mis-parsing here.
const CSV_EXTS = new Set(["csv"]);

const XLSX_EXTS = new Set(["xlsx", "xls"]);

const NOTEBOOK_EXTS = new Set(["ipynb"]);

const MERMAID_EXTS = new Set(["mmd", "mermaid"]);

/**
 * Extensions the SourceViewer can syntax-highlight or render as plain text.
 * Mirrors SourceViewer's EXT_TO_LANG plus common plaintext companions so a
 * `.txt`/`.log`/`.env` opens in the source view instead of the fallback.
 */
const SOURCE_EXTS = new Set([
  "ts",
  "tsx",
  "js",
  "jsx",
  "mjs",
  "cjs",
  "json",
  "jsonc",
  "py",
  "go",
  "rust",
  "rs",
  "sql",
  "css",
  "scss",
  "less",
  "html",
  "htm",
  "xml",
  "yaml",
  "yml",
  "toml",
  "ini",
  "sh",
  "bash",
  "zsh",
  "txt",
  "text",
  "tsv",
  "log",
  "env",
  // `.md`/`.markdown` are intentionally absent: they route to the article
  // view, not a file viewer (see `isMarkdownPath` + the early return below).
  // `.mdx` has no article surface, so it falls through to the source viewer.
  "mdx",
]);

/** Extensions that route to their own dedicated single-format viewer. */
const SINGLETON_KIND_BY_EXT: Record<string, FileKind> = {
  pdf: "pdf",
  docx: "docx",
  pptx: "pptx",
};

/**
 * Lowercase extension (without the dot) of the final path segment, or "" when
 * the leaf has no extension (e.g. `LICENSE`, a dotfile like `.gitignore`).
 */
export function extensionOf(path: string): string {
  const name = path.split("/").pop() ?? path;
  const dot = name.lastIndexOf(".");
  // dot <= 0 covers both "no dot" and a leading-dot dotfile (".gitignore"),
  // neither of which carries a meaningful extension for viewer routing.
  if (dot <= 0 || dot === name.length - 1) return "";
  return name.slice(dot + 1).toLowerCase();
}

/**
 * Classify a repo-root-relative cabinet path into a `FileKind`.
 *
 * `.md`/`.markdown` return `fallback` — they are owned by the article view, so
 * callers that route article-vs-file should check `isMarkdownPath` first.
 * Everything that isn't a recognised viewable extension also lands on
 * `fallback` (binaries, archives, fonts, extensionless files, …).
 */
export function fileKindForPath(path: string): FileKind {
  const ext = extensionOf(path);
  if (!ext) return "fallback";
  // Markdown is owned by the article view, never a file viewer. Returning
  // fallback here means that even if the dispatcher is handed a `.md` path by
  // mistake it degrades to the download card rather than a half-rendered
  // source view.
  if (ext === "md" || ext === "markdown") return "fallback";
  if (IMAGE_EXTS.has(ext)) return "image";
  if (MEDIA_EXTS.has(ext)) return "media";
  if (CSV_EXTS.has(ext)) return "csv";
  if (XLSX_EXTS.has(ext)) return "xlsx";
  if (NOTEBOOK_EXTS.has(ext)) return "notebook";
  if (MERMAID_EXTS.has(ext)) return "mermaid";
  const singleton = SINGLETON_KIND_BY_EXT[ext];
  if (singleton) return singleton;
  if (SOURCE_EXTS.has(ext)) return "source";
  return "fallback";
}

/** True when `path`'s leaf ends in `.md` (the wiki article view owns these). */
export function isMarkdownPath(path: string): boolean {
  const ext = extensionOf(path);
  return ext === "md" || ext === "markdown";
}

/**
 * True when `path` has a dedicated viewer (anything but the fallback). Lets the
 * tree/route decide whether opening a node lands in a real viewer or the
 * filename-only fallback card. Markdown is excluded because it routes to the
 * article view, not a file viewer.
 */
export function isViewablePath(path: string): boolean {
  if (isMarkdownPath(path)) return false;
  return fileKindForPath(path) !== "fallback";
}

/**
 * A short, human-facing label for the file kind — surfaced in the fallback
 * card and available to callers that want a chip. Falls back to the uppercased
 * extension (or "File") when no nicer label exists.
 */
export function fileKindLabel(path: string): string {
  const kind = fileKindForPath(path);
  switch (kind) {
    case "image":
      return "Image";
    case "media":
      return "Media";
    case "pdf":
      return "PDF";
    case "csv":
      return "Spreadsheet";
    case "xlsx":
      return "Spreadsheet";
    case "docx":
      return "Document";
    case "pptx":
      return "Presentation";
    case "notebook":
      return "Notebook";
    case "mermaid":
      return "Diagram";
    case "source":
      return "Source";
    default: {
      const ext = extensionOf(path);
      return ext ? ext.toUpperCase() : "File";
    }
  }
}
