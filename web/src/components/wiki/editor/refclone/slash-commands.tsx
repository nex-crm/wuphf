import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import type { Editor } from "@tiptap/react";
import {
  AlertTriangle,
  CheckSquare,
  Code,
  File,
  Heading1,
  Heading2,
  Heading3,
  ImageIcon,
  Info,
  List,
  ListOrdered,
  Minus,
  Quote,
  Sigma,
  Smile,
  Sparkles,
  Table,
  Type,
  Video,
} from "lucide-react";

import { EmbedPopover } from "./embed-popover";
import { useLocale } from "./lib/use-locale";
import { cn, isSafeLinkHref } from "./lib/utils";
import { type MediaKind, MediaPopover } from "./media-popover";
import { useEditorStore } from "./stores/editor-store";

// Defer emoji-mart (~1 MB of emoji data + picker runtime) until the user
// actually opens the emoji popover from the slash menu. The reference app used
// next/dynamic; React.lazy + Suspense is the framework-agnostic equivalent.
const EmojiPicker = lazy(() =>
  import("./emoji-picker").then((m) => ({ default: m.EmojiPicker })),
);

type PopoverKind =
  | null
  | { type: "media"; kind: MediaKind }
  | { type: "embed" }
  | { type: "emoji" };

interface SlashCommand {
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  description: string;
  category: "basic" | "media" | "advanced";
  /**
   * Either a direct editor action or a popover request.
   */
  action:
    | { type: "direct"; run: (editor: Editor) => void }
    | { type: "popover"; kind: Exclude<PopoverKind, null> };
}

const commands: SlashCommand[] = [
  // Basic
  {
    label: "Text",
    icon: Type,
    description: "Start writing plain text",
    category: "basic",
    action: {
      type: "direct",
      run: (editor) => editor.chain().focus().setParagraph().run(),
    },
  },
  {
    label: "Heading 1",
    icon: Heading1,
    description: "Large section heading",
    category: "basic",
    action: {
      type: "direct",
      run: (editor) => editor.chain().focus().toggleHeading({ level: 1 }).run(),
    },
  },
  {
    label: "Heading 2",
    icon: Heading2,
    description: "Medium section heading",
    category: "basic",
    action: {
      type: "direct",
      run: (editor) => editor.chain().focus().toggleHeading({ level: 2 }).run(),
    },
  },
  {
    label: "Heading 3",
    icon: Heading3,
    description: "Small section heading",
    category: "basic",
    action: {
      type: "direct",
      run: (editor) => editor.chain().focus().toggleHeading({ level: 3 }).run(),
    },
  },
  {
    label: "Bullet List",
    icon: List,
    description: "Create a bullet list",
    category: "basic",
    action: {
      type: "direct",
      run: (editor) => editor.chain().focus().toggleBulletList().run(),
    },
  },
  {
    label: "Numbered List",
    icon: ListOrdered,
    description: "Create a numbered list",
    category: "basic",
    action: {
      type: "direct",
      run: (editor) => editor.chain().focus().toggleOrderedList().run(),
    },
  },
  {
    label: "Checklist",
    icon: CheckSquare,
    description: "Create a task checklist",
    category: "basic",
    action: {
      type: "direct",
      run: (editor) => editor.chain().focus().toggleTaskList().run(),
    },
  },
  {
    label: "Code Block",
    icon: Code,
    description: "Insert a code block",
    category: "basic",
    action: {
      type: "direct",
      run: (editor) => editor.chain().focus().toggleCodeBlock().run(),
    },
  },
  {
    label: "Blockquote",
    icon: Quote,
    description: "Insert a blockquote",
    category: "basic",
    action: {
      type: "direct",
      run: (editor) => editor.chain().focus().toggleBlockquote().run(),
    },
  },
  {
    label: "Divider",
    icon: Minus,
    description: "Insert a horizontal rule",
    category: "basic",
    action: {
      type: "direct",
      run: (editor) => editor.chain().focus().setHorizontalRule().run(),
    },
  },
  {
    label: "Table",
    icon: Table,
    description: "Insert a 3×3 table",
    category: "basic",
    action: {
      type: "direct",
      run: (editor) =>
        editor
          .chain()
          .focus()
          .insertTable({ rows: 3, cols: 3, withHeaderRow: true })
          .run(),
    },
  },

  // Media — each opens a popover with Upload + URL tabs
  {
    label: "Image",
    icon: ImageIcon,
    description: "Upload, paste URL, or drop an image",
    category: "media",
    action: { type: "popover", kind: { type: "media", kind: "image" } },
  },
  {
    label: "Video",
    icon: Video,
    description: "Upload or paste a video URL",
    category: "media",
    action: { type: "popover", kind: { type: "media", kind: "video" } },
  },
  {
    label: "Embed",
    icon: Sparkles,
    description: "YouTube, X, Vimeo, Loom, TikTok, Spotify…",
    category: "media",
    action: { type: "popover", kind: { type: "embed" } },
  },
  {
    label: "File",
    icon: File,
    description: "Attach any file to this page",
    category: "media",
    action: { type: "popover", kind: { type: "media", kind: "file" } },
  },

  // Advanced
  {
    label: "Callout",
    icon: Info,
    description: "Insert an info callout",
    category: "advanced",
    action: {
      type: "direct",
      run: (editor) =>
        editor.chain().focus().wrapIn("callout", { type: "info" }).run(),
    },
  },
  {
    label: "Warning",
    icon: AlertTriangle,
    description: "Insert a warning callout",
    category: "advanced",
    action: {
      type: "direct",
      run: (editor) =>
        editor.chain().focus().wrapIn("callout", { type: "warning" }).run(),
    },
  },
  {
    label: "Math",
    icon: Sigma,
    description: "Insert a LaTeX math expression",
    category: "advanced",
    action: {
      type: "direct",
      run: (editor) => editor.chain().focus().insertContent("$x=y$").run(),
    },
  },
  {
    label: "Emoji",
    icon: Smile,
    description: "Pick an emoji",
    category: "advanced",
    action: { type: "popover", kind: { type: "emoji" } },
  },
];

interface SlashCommandsProps {
  editor: Editor | null;
}

export function SlashCommands({ editor }: SlashCommandsProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [position, setPosition] = useState<{
    top: number;
    left?: number;
    right?: number;
  }>({ top: 0, left: 0 });
  const [popover, setPopover] = useState<PopoverKind>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const pagePath = useEditorStore((s) => s.currentPath);
  const { dir } = useLocale();

  const filtered = commands.filter(
    (cmd) =>
      cmd.label.toLowerCase().includes(query.toLowerCase()) ||
      cmd.description.toLowerCase().includes(query.toLowerCase()),
  );

  const handleClose = useCallback(() => {
    setOpen(false);
    setQuery("");
    setSelectedIndex(0);
  }, []);

  const handleSelect = useCallback(
    (command: SlashCommand) => {
      if (!editor) return;
      // Delete the slash and query text
      const { from } = editor.state.selection;
      const slashStart = from - query.length - 1;
      editor.chain().focus().deleteRange({ from: slashStart, to: from }).run();

      if (command.action.type === "direct") {
        command.action.run(editor);
        handleClose();
      } else {
        setPopover(command.action.kind);
        setOpen(false);
        setQuery("");
      }
    },
    [editor, query, handleClose],
  );

  useEffect(() => {
    if (!editor) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (!open) {
        if (event.key === "/") {
          const { from } = editor.state.selection;
          const textBefore = editor.state.doc.textBetween(
            Math.max(0, from - 1),
            from,
          );
          if (
            from === 1 ||
            textBefore === "" ||
            textBefore === "\n" ||
            textBefore === " "
          ) {
            const coords = editor.view.coordsAtPos(from);
            const editorRect = editor.view.dom.getBoundingClientRect();
            setPosition(
              dir === "rtl"
                ? {
                    top: coords.bottom - editorRect.top + 4,
                    // Anchor from the editor's right edge so the menu opens
                    // toward the logical start in RTL.
                    right: editorRect.right - coords.right,
                  }
                : {
                    top: coords.bottom - editorRect.top + 4,
                    left: coords.left - editorRect.left,
                  },
            );
            setOpen(true);
            setQuery("");
            setSelectedIndex(0);
          }
        }
        return;
      }

      if (event.key === "Escape") {
        event.preventDefault();
        handleClose();
      } else if (event.key === "ArrowDown") {
        event.preventDefault();
        setSelectedIndex((i) => Math.min(i + 1, filtered.length - 1));
      } else if (event.key === "ArrowUp") {
        event.preventDefault();
        setSelectedIndex((i) => Math.max(i - 1, 0));
      } else if (event.key === "Enter") {
        event.preventDefault();
        if (filtered[selectedIndex]) handleSelect(filtered[selectedIndex]);
      } else if (event.key === "Backspace") {
        if (query.length === 0) handleClose();
        else {
          setQuery((q) => q.slice(0, -1));
          setSelectedIndex(0);
        }
      } else if (event.key === " ") {
        handleClose();
      } else if (event.key.length === 1 && !event.metaKey && !event.ctrlKey) {
        setQuery((q) => q + event.key);
        setSelectedIndex(0);
      }
    };

    window.addEventListener("keydown", handleKeyDown, true);
    return () => window.removeEventListener("keydown", handleKeyDown, true);
  }, [
    editor,
    open,
    query,
    selectedIndex,
    filtered,
    handleClose,
    handleSelect,
    dir,
  ]);

  useEffect(() => {
    if (!open) return;
    const handleClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        handleClose();
      }
    };
    window.addEventListener("mousedown", handleClick);
    return () => window.removeEventListener("mousedown", handleClick);
  }, [open, handleClose]);

  const insertMedia = (
    kind: MediaKind,
    payload: { url: string; alt?: string; mimeType?: string },
  ) => {
    if (!editor) return;
    const { url, alt, mimeType } = payload;
    const type = mimeType ?? "";
    const isImage =
      kind === "image" ||
      type.startsWith("image/") ||
      /\.(png|jpe?g|gif|webp|svg|avif)(\?|$)/i.test(url);
    const isVideo =
      kind === "video" ||
      type.startsWith("video/") ||
      /\.(mp4|webm|ogg|mov|m4v)(\?|$)/i.test(url);

    if (isImage) {
      editor
        .chain()
        .focus()
        .setImage({ src: url, alt: alt ?? "" })
        .run();
    } else if (isVideo) {
      editor
        .chain()
        .focus()
        .insertContent({
          type: "embed",
          attrs: { provider: "video", src: url, originalUrl: url },
        })
        .run();
    } else {
      // Insert as a structured text node + link mark rather than an HTML
      // string so the user-supplied url/alt can't inject markup, and drop the
      // link mark entirely for unsafe schemes (javascript:/data:).
      editor
        .chain()
        .focus()
        .insertContent(
          isSafeLinkHref(url)
            ? {
                type: "text",
                text: alt ?? url,
                marks: [{ type: "link", attrs: { href: url } }],
              }
            : { type: "text", text: alt ?? url },
        )
        .run();
    }
    setPopover(null);
  };

  const insertEmbed = (url: string) => {
    if (!editor) return;
    editor.commands.setEmbed({ url });
    setPopover(null);
  };

  const insertEmoji = (native: string) => {
    if (!editor) return;
    editor.chain().focus().insertContent(native).run();
    setPopover(null);
  };

  const renderPopover = () => {
    if (!(popover && editor)) return null;
    const anchor = position;
    if (popover.type === "media") {
      if (!pagePath) return null;
      return (
        <MediaPopover
          kind={popover.kind}
          pagePath={pagePath}
          anchor={anchor}
          onCancel={() => setPopover(null)}
          onInsert={(payload) => insertMedia(popover.kind, payload)}
        />
      );
    }
    if (popover.type === "embed") {
      return (
        <EmbedPopover
          anchor={anchor}
          onCancel={() => setPopover(null)}
          onInsert={insertEmbed}
        />
      );
    }
    if (popover.type === "emoji") {
      return (
        <Suspense fallback={null}>
          <EmojiPicker
            anchor={anchor}
            onSelect={insertEmoji}
            onClose={() => setPopover(null)}
          />
        </Suspense>
      );
    }
    return null;
  };

  if ((!open || filtered.length === 0) && !popover) return null;

  // Group filtered commands by category for rendering headers
  const byCategory = new Map<string, SlashCommand[]>();
  for (const cmd of filtered) {
    const list = byCategory.get(cmd.category) ?? [];
    list.push(cmd);
    byCategory.set(cmd.category, list);
  }
  const order: { key: string; title: string }[] = [
    { key: "basic", title: "Basic" },
    { key: "media", title: "Media" },
    { key: "advanced", title: "Advanced" },
  ];

  // Flat index list for keyboard nav
  const flatCommands: SlashCommand[] = filtered;

  return (
    <>
      {open && filtered.length > 0 && (
        <div
          ref={menuRef}
          className="absolute z-50 w-[300px] bg-popover border border-border rounded-xl shadow-lg p-1.5 overflow-hidden max-h-[380px] overflow-y-auto"
          style={{
            top: position.top,
            left: position.left,
            right: position.right,
          }}
        >
          {order.map((group) => {
            const items = byCategory.get(group.key);
            if (!items || items.length === 0) return null;
            return (
              <div key={group.key}>
                <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground px-2.5 pt-2 pb-1.5">
                  {group.title}
                </div>
                {items.map((cmd) => {
                  const flatIndex = flatCommands.indexOf(cmd);
                  const Icon = cmd.icon;
                  return (
                    <button
                      key={cmd.label}
                      onMouseDown={(e) => {
                        e.preventDefault();
                        handleSelect(cmd);
                      }}
                      onMouseEnter={() => setSelectedIndex(flatIndex)}
                      className={cn(
                        "flex items-start gap-2.5 w-full px-2.5 py-2 text-left rounded-md transition-colors",
                        flatIndex === selectedIndex
                          ? "bg-accent text-accent-foreground"
                          : "hover:bg-accent/50",
                      )}
                    >
                      <Icon className="mt-0.5 h-4 w-4 text-muted-foreground shrink-0" />
                      <div className="flex flex-col min-w-0">
                        <p className="text-[12px] font-medium leading-snug truncate">
                          {cmd.label}
                        </p>
                        <p className="mt-0.5 text-[10px] leading-snug text-muted-foreground truncate">
                          {cmd.description}
                        </p>
                      </div>
                    </button>
                  );
                })}
              </div>
            );
          })}
        </div>
      )}
      {renderPopover()}
    </>
  );
}
