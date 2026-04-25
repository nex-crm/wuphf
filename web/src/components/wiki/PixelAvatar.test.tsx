import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import PixelAvatar from "./PixelAvatar";

describe("<PixelAvatar> (wiki wrapper)", () => {
  it("renders a canvas element for the underlying agent sprite", () => {
    // Arrange / Act
    const { container } = render(<PixelAvatar slug="pm" size={22} />);

    // Assert
    const canvas = container.querySelector("canvas");
    expect(canvas).not.toBeNull();
  });

  it("applies a default wiki className when none is provided", () => {
    const { container } = render(<PixelAvatar slug="ceo" size={14} />);
    const canvas = container.querySelector("canvas");
    expect(canvas?.className).toContain("wk-avatar");
  });

  it("passes a custom className through to the canvas", () => {
    const { container } = render(
      <PixelAvatar slug="cro" size={16} className="wk-avatar-custom" />,
    );
    const canvas = container.querySelector("canvas");
    expect(canvas?.className).toContain("wk-avatar-custom");
  });

  it("wraps in a titled span when title prop is provided", () => {
    const { container } = render(
      <PixelAvatar slug="pm" size={14} title="PM avatar" />,
    );
    const wrap = container.querySelector("span.wk-avatar-wrap");
    expect(wrap).not.toBeNull();
    expect(wrap?.getAttribute("title")).toBe("PM avatar");
    expect(wrap?.querySelector("canvas")).not.toBeNull();
  });

  it("defaults size to 14 when omitted and still renders", () => {
    const { container } = render(<PixelAvatar slug="designer" />);
    expect(container.querySelector("canvas")).not.toBeNull();
  });
});
