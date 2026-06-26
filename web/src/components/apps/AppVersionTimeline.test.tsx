import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { CustomAppVersion } from "../../api/apps";
import { AppVersionTimeline } from "./AppVersionTimeline";

function v(version: number, current: boolean, by?: string): CustomAppVersion {
  return {
    version,
    current,
    updatedBy: by,
    updatedAt: by ? "2026-06-15T12:00:00Z" : undefined,
  };
}

describe("AppVersionTimeline", () => {
  it("lists versions newest-first with a Current pill and author meta", () => {
    render(
      <AppVersionTimeline
        versions={[v(3, true, "pam"), v(2, false, "app-builder"), v(1, false)]}
        isLoading={false}
        selectedVersion={null}
        currentVersion={3}
        onSelect={vi.fn()}
      />,
    );
    expect(screen.getByText("v3")).toBeInTheDocument();
    expect(screen.getByText("Current")).toBeInTheDocument();
    expect(screen.getByText(/app-builder/)).toBeInTheDocument();
  });

  it("marks the current build active when nothing older is selected", () => {
    render(
      <AppVersionTimeline
        versions={[v(2, true, "pam"), v(1, false, "pam")]}
        isLoading={false}
        selectedVersion={null}
        currentVersion={2}
        onSelect={vi.fn()}
      />,
    );
    const active = document.querySelector(".app-version-timeline__row--active");
    expect(active?.textContent).toContain("v2");
  });

  it("calls onSelect with the clicked version", () => {
    const onSelect = vi.fn();
    render(
      <AppVersionTimeline
        versions={[v(2, true, "pam"), v(1, false, "pam")]}
        isLoading={false}
        selectedVersion={null}
        currentVersion={2}
        onSelect={onSelect}
      />,
    );
    fireEvent.click(screen.getByText("v1"));
    expect(onSelect).toHaveBeenCalledWith(1);
  });

  it("shows an empty state when there are no versions", () => {
    render(
      <AppVersionTimeline
        versions={[]}
        isLoading={false}
        selectedVersion={null}
        currentVersion={0}
        onSelect={vi.fn()}
      />,
    );
    expect(screen.getByText(/No saved versions yet/i)).toBeInTheDocument();
  });
});
