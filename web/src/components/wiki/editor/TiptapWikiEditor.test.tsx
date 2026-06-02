/**
 * Component tests for the Tiptap wiki editor.
 *
 * Tiptap needs a real DOM, so these run under happy-dom (the repo's vitest
 * environment). The assertions stay resilient — they exercise the public
 * contract (seed markdown renders, edits emit markdown, the slash menu opens
 * on `/`) rather than ProseMirror internals, which behave differently between
 * happy-dom and a browser.
 */

import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { WikiCatalogEntry } from "../../../api/wiki";
import TiptapWikiEditor from "./TiptapWikiEditor";

const CATALOG: WikiCatalogEntry[] = [
  {
    path: "team/people/nazz",
    title: "Nazz",
    author_slug: "ceo",
    last_edited_ts: "2026-04-20T10:00:00.000Z",
    group: "people",
  },
];

afterEach(() => {
  vi.restoreAllMocks();
});

describe("<TiptapWikiEditor>", () => {
  it("renders seed markdown as editable HTML", async () => {
    render(
      <TiptapWikiEditor
        content={"# Title\n\nHello world."}
        onChange={() => {}}
      />,
    );
    const content = await screen.findByTestId("wk-tiptap-content");
    await waitFor(() => {
      expect(content.querySelector("h1")?.textContent).toBe("Title");
    });
    expect(content.textContent).toContain("Hello world.");
    // The mounted ProseMirror surface is contenteditable.
    expect(content.querySelector(".ProseMirror")).not.toBeNull();
  });

  it("renders a wikilink from seed markdown as a wikiLink anchor", async () => {
    render(
      <TiptapWikiEditor
        content={"See [[team/people/nazz|Nazz]] for context."}
        onChange={() => {}}
        catalog={CATALOG}
        resolver={(slug) => slug === "team/people/nazz"}
      />,
    );
    const content = await screen.findByTestId("wk-tiptap-content");
    await waitFor(() => {
      const link = content.querySelector('a[data-wikilink="true"]');
      expect(link).not.toBeNull();
      expect(link?.getAttribute("data-slug")).toBe("team/people/nazz");
      expect(link?.textContent).toBe("Nazz");
    });
  });

  it("flags an unresolved wikilink in the broken-links banner", async () => {
    render(
      <TiptapWikiEditor
        content={"See [[does/not/exist]]."}
        onChange={() => {}}
        catalog={CATALOG}
        resolver={(slug) => slug === "team/people/nazz"}
      />,
    );
    const banner = await screen.findByTestId("wk-tiptap-broken-links");
    expect(banner.textContent).toContain("[[does/not/exist]]");
  });

  it("emits markdown via onChange when the document changes", async () => {
    const onChange = vi.fn();
    render(<TiptapWikiEditor content={"Start."} onChange={onChange} />);
    const content = await screen.findByTestId("wk-tiptap-content");
    const pm = content.querySelector(".ProseMirror") as HTMLElement;
    expect(pm).not.toBeNull();

    // Simulate an edit by inserting text into the contenteditable surface and
    // firing the input event ProseMirror listens for.
    await act(async () => {
      const p = pm.querySelector("p");
      if (p) p.textContent = "Start. More.";
      fireEvent.input(pm);
    });

    await waitFor(() => expect(onChange).toHaveBeenCalled());
    const lastArg = onChange.mock.calls.at(-1)?.[0] as string;
    expect(typeof lastArg).toBe("string");
    expect(lastArg).toContain("More.");
  });

  it("opens the slash menu when '/' is pressed on an empty line", async () => {
    render(<TiptapWikiEditor content={""} onChange={() => {}} />);
    const content = await screen.findByTestId("wk-tiptap-content");
    const pm = content.querySelector(".ProseMirror") as HTMLElement;
    expect(pm).not.toBeNull();

    await act(async () => {
      pm.focus();
      fireEvent.keyDown(window, { key: "/" });
    });

    const menu = await screen.findByTestId("wk-slash-menu");
    expect(menu).toBeInTheDocument();
    // Basic blocks and WUPHF inserts both surface.
    expect(screen.getByTestId("wk-slash-basic-h1")).toBeInTheDocument();
    expect(screen.getByTestId("wk-slash-action-citation")).toBeInTheDocument();
  });

  it("filters the slash menu as the query is typed", async () => {
    render(<TiptapWikiEditor content={""} onChange={() => {}} />);
    const content = await screen.findByTestId("wk-tiptap-content");
    const pm = content.querySelector(".ProseMirror") as HTMLElement;

    await act(async () => {
      pm.focus();
      fireEvent.keyDown(window, { key: "/" });
    });
    await screen.findByTestId("wk-slash-menu");

    await act(async () => {
      fireEvent.keyDown(window, { key: "t" });
      fireEvent.keyDown(window, { key: "a" });
      fireEvent.keyDown(window, { key: "b" });
    });

    // "tab" matches the Table basic block; H1 should be filtered out.
    await waitFor(() => {
      expect(screen.getByTestId("wk-slash-basic-table")).toBeInTheDocument();
      expect(screen.queryByTestId("wk-slash-basic-h1")).toBeNull();
    });
  });

  it("closes the slash menu on Escape", async () => {
    render(<TiptapWikiEditor content={""} onChange={() => {}} />);
    const content = await screen.findByTestId("wk-tiptap-content");
    const pm = content.querySelector(".ProseMirror") as HTMLElement;

    await act(async () => {
      pm.focus();
      fireEvent.keyDown(window, { key: "/" });
    });
    await screen.findByTestId("wk-slash-menu");

    await act(async () => {
      fireEvent.keyDown(window, { key: "Escape" });
    });
    await waitFor(() => {
      expect(screen.queryByTestId("wk-slash-menu")).toBeNull();
    });
  });

  it("reflects the active slash option onto the editor via aria-activedescendant", async () => {
    render(<TiptapWikiEditor content={""} onChange={() => {}} />);
    const content = await screen.findByTestId("wk-tiptap-content");
    const pm = content.querySelector(".ProseMirror") as HTMLElement;

    await act(async () => {
      pm.focus();
      fireEvent.keyDown(window, { key: "/" });
    });
    await screen.findByTestId("wk-slash-menu");

    // The first row is active on open; the editor surface (which keeps focus)
    // points at it and the matching option button carries that id.
    await waitFor(() => {
      expect(pm.getAttribute("aria-activedescendant")).toBe("wk-slash-opt-0");
    });
    expect(screen.getByTestId("wk-slash-basic-text").id).toBe("wk-slash-opt-0");

    // ArrowDown advances the active option; the reflected id follows.
    await act(async () => {
      fireEvent.keyDown(window, { key: "ArrowDown" });
    });
    await waitFor(() => {
      expect(pm.getAttribute("aria-activedescendant")).toBe("wk-slash-opt-1");
    });

    // Esc closes the menu and clears the dangling reference on the editor.
    await act(async () => {
      fireEvent.keyDown(window, { key: "Escape" });
    });
    await waitFor(() => {
      expect(screen.queryByTestId("wk-slash-menu")).toBeNull();
    });
    expect(pm.getAttribute("aria-activedescendant")).toBeNull();
  });

  it("gives the editor a textbox role and the parent label", async () => {
    render(
      <TiptapWikiEditor
        content={"Body."}
        onChange={() => {}}
        labelId="wk-editor-source-label"
      />,
    );
    const content = await screen.findByTestId("wk-tiptap-content");
    const pm = content.querySelector(".ProseMirror") as HTMLElement;
    expect(pm.getAttribute("role")).toBe("textbox");
    expect(pm.getAttribute("aria-multiline")).toBe("true");
    expect(pm.getAttribute("aria-labelledby")).toBe("wk-editor-source-label");
  });
});
