import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Workspace } from "../../../api/workspaces";
import { ShredConfirmModal } from "../ShredConfirmModal";

const mainWorkspace: Workspace = {
  name: "main",
  runtime_home: "/Users/me/.wuphf-spaces/main",
  broker_port: 7890,
  web_port: 7891,
  state: "running",
  created_at: "2026-01-01T00:00:00Z",
};

const demoWorkspace: Workspace = {
  ...mainWorkspace,
  name: "demo-launch",
  state: "paused",
};

describe("<ShredConfirmModal>", () => {
  it("for non-main workspaces, lets the user confirm immediately", () => {
    const onConfirm = vi.fn();
    render(
      <ShredConfirmModal
        workspace={demoWorkspace}
        onConfirm={onConfirm}
        onCancel={() => {}}
      />,
    );

    expect(screen.queryByTestId("shred-confirm-phrase")).toBeNull();

    fireEvent.click(screen.getByTestId("shred-confirm-submit"));
    expect(onConfirm).toHaveBeenCalledWith({ permanent: false });
  });

  it("for the main workspace, requires typing 'main' before submit enables", () => {
    const onConfirm = vi.fn();
    render(
      <ShredConfirmModal
        workspace={mainWorkspace}
        onConfirm={onConfirm}
        onCancel={() => {}}
      />,
    );

    const submit = screen.getByTestId(
      "shred-confirm-submit",
    ) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);

    const input = screen.getByTestId("shred-confirm-phrase");
    fireEvent.change(input, { target: { value: "main" } });

    expect(submit.disabled).toBe(false);
    fireEvent.click(submit);
    expect(onConfirm).toHaveBeenCalledWith({ permanent: false });
  });

  it("toggling 'Skip trash' yields a permanent confirm", () => {
    const onConfirm = vi.fn();
    render(
      <ShredConfirmModal
        workspace={demoWorkspace}
        onConfirm={onConfirm}
        onCancel={() => {}}
      />,
    );

    fireEvent.click(screen.getByTestId("shred-permanent-toggle"));
    fireEvent.click(screen.getByTestId("shred-confirm-submit"));

    expect(onConfirm).toHaveBeenCalledWith({ permanent: true });
  });

  it("Esc fires onCancel when not busy", () => {
    const onCancel = vi.fn();
    render(
      <ShredConfirmModal
        workspace={demoWorkspace}
        busy={false}
        onConfirm={() => {}}
        onCancel={onCancel}
      />,
    );

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onCancel).toHaveBeenCalled();
  });

  it("submit shows busy label while pending", () => {
    render(
      <ShredConfirmModal
        workspace={demoWorkspace}
        busy={true}
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );

    expect(
      (screen.getByTestId("shred-confirm-submit") as HTMLButtonElement)
        .textContent,
    ).toMatch(/shredding/i);
  });
});
