import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { OperatorBuildExperience } from "./OperatorBuildExperience";

// The mock chat exposes a button that fires onBuildingApp, so the test can
// simulate the build scaffolding (the moment the layout flips to live preview +
// docked chat) through a real event — wrapped in act by fireEvent.
vi.mock("./AppBuilderChat", () => ({
  AppBuilderChat: ({
    panelMode,
    onBuildingApp,
  }: {
    panelMode?: boolean;
    onBuildingApp?: (id: string) => void;
  }) => (
    <div data-testid="builder-chat" data-panel={String(Boolean(panelMode))}>
      <button type="button" onClick={() => onBuildingApp?.("app_live123")}>
        scaffold
      </button>
    </div>
  ),
}));
vi.mock("./OperatorAppDetail", () => ({
  OperatorAppDetail: ({
    appId,
    buildWalk,
  }: {
    appId: string;
    buildWalk?: boolean;
  }) => (
    <div
      data-testid="app-detail"
      data-app-id={appId}
      data-build-walk={String(Boolean(buildWalk))}
    />
  ),
}));

describe("OperatorBuildExperience", () => {
  it("starts as a centered describe chat, then docks beside the live app once it scaffolds", () => {
    const { getByTestId, getByText, queryByTestId, container } = render(
      <OperatorBuildExperience onClose={() => {}} onFinish={() => {}} />,
    );

    // Describe phase: full chat, no app detail, not live.
    expect(getByTestId("builder-chat").getAttribute("data-panel")).toBe(
      "false",
    );
    expect(queryByTestId("app-detail")).toBeNull();
    expect(container.querySelector(".opr-build-exp.is-live")).toBeNull();

    // The build scaffolds → the chat reports the app id.
    fireEvent.click(getByText("scaffold"));

    // Live phase: app detail (with the build walk) appears, chat docks (panel).
    const detail = getByTestId("app-detail");
    expect(detail.getAttribute("data-app-id")).toBe("app_live123");
    expect(detail.getAttribute("data-build-walk")).toBe("true");
    expect(getByTestId("builder-chat").getAttribute("data-panel")).toBe("true");
    expect(container.querySelector(".opr-build-exp.is-live")).not.toBeNull();
  });
});
