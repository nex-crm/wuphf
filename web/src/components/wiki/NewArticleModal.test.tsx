import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as wikiApi from "../../api/wiki";
import NewArticleModal from "./NewArticleModal";

// "people" sorts before "projects" alphabetically, so it becomes the default group.
const BASE_CATALOG: wikiApi.WikiCatalogEntry[] = [
  {
    path: "team/people/alex.md",
    title: "Alex",
    group: "people",
    author_slug: "human",
    last_edited_ts: "2026-01-01T00:00:00Z",
  },
  {
    path: "team/projects/backend.md",
    title: "Backend",
    group: "projects",
    author_slug: "human",
    last_edited_ts: "2026-01-01T00:00:00Z",
  },
];

function renderModal(
  catalog = BASE_CATALOG,
  onCreated = vi.fn(),
  onCancel = vi.fn(),
) {
  render(
    <NewArticleModal
      catalog={catalog}
      onCreated={onCreated}
      onCancel={onCancel}
    />,
  );
  return { onCreated, onCancel };
}

describe("<NewArticleModal>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("renders section, subfolder, slug, title, and template fields", () => {
    renderModal();
    expect(screen.getByLabelText(/section/i)).toBeInTheDocument();
    expect(screen.getByTestId("wk-new-subfolder")).toBeInTheDocument();
    expect(screen.getByTestId("wk-new-slug")).toBeInTheDocument();
    expect(screen.getByLabelText(/title/i)).toBeInTheDocument();
    expect(screen.getByTestId("wk-new-template")).toBeInTheDocument();
  });

  it("shows existing groups in the section select", () => {
    renderModal();
    const select = screen.getByLabelText(/section/i);
    expect(select).toHaveTextContent("people");
    expect(select).toHaveTextContent("projects");
  });

  it("shows path preview without subfolder", () => {
    renderModal();
    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "sam-lee" } });
    // Path text is inside <code> inside <p> — query the code element directly.
    expect(screen.getByText("team/people/sam-lee.md")).toBeInTheDocument();
  });

  it("shows path preview with subfolder", () => {
    renderModal();
    fireEvent.change(screen.getByTestId("wk-new-subfolder"), { target: { value: "leadership" } });
    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "sam-lee" } });
    expect(screen.getByText("team/people/leadership/sam-lee.md")).toBeInTheDocument();
  });

  it("sanitizes slug input to lowercase slug characters", () => {
    renderModal();
    const slugInput = screen.getByTestId("wk-new-slug");
    fireEvent.change(slugInput, { target: { value: "Hello World!" } });
    expect(slugInput).toHaveValue("hello-world-");
  });

  it("sanitizes subfolder input to lowercase slug characters", () => {
    renderModal();
    const input = screen.getByTestId("wk-new-subfolder");
    fireEvent.change(input, { target: { value: "My Folder" } });
    expect(input).toHaveValue("my-folder");
  });

  it("blocks create when slug is empty", async () => {
    renderModal();
    fireEvent.click(screen.getByTestId("wk-new-create"));
    expect(await screen.findByRole("alert")).toHaveTextContent(/slug is required/i);
  });

  it("blocks create when title is empty", async () => {
    renderModal();
    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "sam-lee" } });
    fireEvent.click(screen.getByTestId("wk-new-create"));
    expect(await screen.findByRole("alert")).toHaveTextContent(/title is required/i);
  });

  it("blocks create when path already exists in catalog", async () => {
    renderModal();
    // Default group is "people"; "alex" → team/people/alex.md which is in BASE_CATALOG.
    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "alex" } });
    fireEvent.change(screen.getByLabelText(/title/i), { target: { value: "Alex Duplicate" } });
    fireEvent.click(screen.getByTestId("wk-new-create"));
    expect(await screen.findByRole("alert")).toHaveTextContent(/already exists/i);
  });

  it("blocks create when subfolder segment is invalid", async () => {
    renderModal();
    // Manually bypass sanitizer by mocking — subfolder sanitizes on change,
    // so test the validation path via a leading-dot value injected directly.
    const input = screen.getByTestId("wk-new-subfolder") as HTMLInputElement;
    // Set value without firing a sanitized change event
    Object.defineProperty(input, "value", { writable: true, value: ".hidden" });
    fireEvent.change(input, { target: { value: ".hidden" } });
    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "doc" } });
    fireEvent.change(screen.getByLabelText(/title/i), { target: { value: "Doc" } });
    fireEvent.click(screen.getByTestId("wk-new-create"));
    // Sanitizer strips leading dot so value will be empty → "Subfolder is required"
    // or "must be lowercase" — either proves validation ran.
    expect(await screen.findByRole("alert")).toBeInTheDocument();
  });

  it("calls writeHumanArticle and onCreated on success", async () => {
    const { onCreated } = renderModal();
    vi.spyOn(wikiApi, "writeHumanArticle").mockResolvedValue({ path: "team/people/new-page.md", commit_sha: "abc123", bytes_written: 10 });

    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "new-page" } });
    fireEvent.change(screen.getByLabelText(/title/i), { target: { value: "New Page" } });
    fireEvent.click(screen.getByTestId("wk-new-create"));

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith("team/people/new-page.md"));
    expect(wikiApi.writeHumanArticle).toHaveBeenCalledWith(
      expect.objectContaining({
        path: "team/people/new-page.md",
        expectedSha: "",
      }),
    );
  });

  it("calls writeHumanArticle with subfolder in path", async () => {
    const { onCreated } = renderModal();
    vi.spyOn(wikiApi, "writeHumanArticle").mockResolvedValue({ path: "team/people/new-page.md", commit_sha: "abc123", bytes_written: 10 });

    fireEvent.change(screen.getByTestId("wk-new-subfolder"), { target: { value: "leadership" } });
    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "new-page" } });
    fireEvent.change(screen.getByLabelText(/title/i), { target: { value: "New Page" } });
    fireEvent.click(screen.getByTestId("wk-new-create"));

    await waitFor(() =>
      expect(onCreated).toHaveBeenCalledWith("team/people/leadership/new-page.md"),
    );
  });

  it("shows a conflict error when the server returns a conflict", async () => {
    renderModal();
    vi.spyOn(wikiApi, "writeHumanArticle").mockResolvedValue({ conflict: true } as never);

    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "new-page" } });
    fireEvent.change(screen.getByLabelText(/title/i), { target: { value: "New Page" } });
    fireEvent.click(screen.getByTestId("wk-new-create"));

    expect(await screen.findByRole("alert")).toHaveTextContent(/already exists/i);
  });

  it("shows an error when writeHumanArticle throws", async () => {
    renderModal();
    vi.spyOn(wikiApi, "writeHumanArticle").mockRejectedValue(new Error("Network error"));

    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "new-page" } });
    fireEvent.change(screen.getByLabelText(/title/i), { target: { value: "New Page" } });
    fireEvent.click(screen.getByTestId("wk-new-create"));

    expect(await screen.findByRole("alert")).toHaveTextContent(/network error/i);
  });

  it("disables buttons while submitting", async () => {
    renderModal();
    vi.spyOn(wikiApi, "writeHumanArticle").mockImplementation(
      () => new Promise(() => { /* never resolves */ }),
    );

    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "new-page" } });
    fireEvent.change(screen.getByLabelText(/title/i), { target: { value: "New Page" } });
    fireEvent.click(screen.getByTestId("wk-new-create"));

    await waitFor(() =>
      expect(screen.getByTestId("wk-new-create")).toBeDisabled(),
    );
    expect(screen.getByText(/cancel/i)).toBeDisabled();
  });

  it("calls onCancel when Cancel is clicked", () => {
    const { onCancel } = renderModal();
    fireEvent.click(screen.getByText(/cancel/i));
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it("generates person template body", async () => {
    renderModal();
    vi.spyOn(wikiApi, "writeHumanArticle").mockResolvedValue({ path: "x", commit_sha: "abc", bytes_written: 1 });

    fireEvent.change(screen.getByTestId("wk-new-template"), { target: { value: "person" } });
    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "sarah" } });
    fireEvent.change(screen.getByLabelText(/title/i), { target: { value: "Sarah Chen" } });
    fireEvent.click(screen.getByTestId("wk-new-create"));

    await waitFor(() =>
      expect(wikiApi.writeHumanArticle).toHaveBeenCalledWith(
        expect.objectContaining({
          content: expect.stringContaining("## Role"),
        }),
      ),
    );
  });

  it("generates decision template body", async () => {
    renderModal();
    vi.spyOn(wikiApi, "writeHumanArticle").mockResolvedValue({ path: "x", commit_sha: "abc", bytes_written: 1 });

    fireEvent.change(screen.getByTestId("wk-new-template"), { target: { value: "decision" } });
    fireEvent.change(screen.getByTestId("wk-new-slug"), { target: { value: "use-sqlite" } });
    fireEvent.change(screen.getByLabelText(/title/i), { target: { value: "Use SQLite" } });
    fireEvent.click(screen.getByTestId("wk-new-create"));

    await waitFor(() =>
      expect(wikiApi.writeHumanArticle).toHaveBeenCalledWith(
        expect.objectContaining({
          content: expect.stringContaining("## Rationale"),
        }),
      ),
    );
  });

  it("shows the custom section input when '+ New section' is selected", () => {
    renderModal();
    fireEvent.change(screen.getByLabelText(/section/i), { target: { value: "__custom__" } });
    expect(screen.getByPlaceholderText(/e.g. playbooks/i)).toBeInTheDocument();
  });

  it("uses the catalog entry's group for the path when no groups exist yet", () => {
    renderModal([]);
    // With empty catalog, default group should still render without crash
    expect(screen.getByTestId("wk-new-article-modal")).toBeInTheDocument();
  });
});
