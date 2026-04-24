import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Kbd, KbdSequence, MOD_KEY } from "./Kbd";

describe("<Kbd>", () => {
  it("renders a semantic <kbd> with the base .kbd class", () => {
    const { container } = render(<Kbd>K</Kbd>);
    const kbd = container.querySelector("kbd");
    expect(kbd).not.toBeNull();
    expect(kbd?.textContent).toBe("K");
    expect(kbd?.className).toContain("kbd");
  });

  it("applies the size modifier class", () => {
    const { container } = render(<Kbd size="sm">K</Kbd>);
    expect(container.querySelector("kbd")?.className).toContain("kbd-sm");
  });

  it("applies the inverse variant class", () => {
    const { container } = render(<Kbd variant="inverse">K</Kbd>);
    expect(container.querySelector("kbd")?.className).toContain("kbd-inverse");
  });

  it("does not apply kbd-inverse for the default variant", () => {
    const { container } = render(<Kbd>K</Kbd>);
    expect(container.querySelector("kbd")?.className).not.toContain(
      "kbd-inverse",
    );
  });

  it("passes through an extra className", () => {
    const { container } = render(<Kbd className="extra-class">K</Kbd>);
    expect(container.querySelector("kbd")?.className).toContain("extra-class");
  });
});

describe("<KbdSequence>", () => {
  it('renders flat keys as a single chord with no "then" separator', () => {
    const { container } = render(<KbdSequence keys={["⌘", "K"]} />);
    const kbds = container.querySelectorAll("kbd");
    expect(kbds.length).toBe(2);
    expect(kbds[0].textContent).toBe("⌘");
    expect(kbds[1].textContent).toBe("K");
    expect(container.querySelector(".kbd-then")).toBeNull();
  });

  it('renders chord sequences with "then" separators between chords', () => {
    const { container } = render(<KbdSequence keys={[["g"], ["g"]]} />);
    const kbds = container.querySelectorAll("kbd");
    const thens = container.querySelectorAll(".kbd-then");
    expect(kbds.length).toBe(2);
    expect(thens.length).toBe(1);
    expect(thens[0].textContent).toBe("then");
  });

  it("propagates size to every rendered <kbd>", () => {
    const { container } = render(<KbdSequence keys={["⌘", "K"]} size="sm" />);
    const kbds = container.querySelectorAll("kbd");
    kbds.forEach((k) => expect(k.className).toContain("kbd-sm"));
  });

  it("propagates inverse variant to every rendered <kbd>", () => {
    const { container } = render(
      <KbdSequence keys={["⌘", "K"]} variant="inverse" />,
    );
    const kbds = container.querySelectorAll("kbd");
    kbds.forEach((k) => expect(k.className).toContain("kbd-inverse"));
  });

  it('marks the "then" separator aria-hidden so screen readers skip it', () => {
    const { container } = render(<KbdSequence keys={[["g"], ["g"]]} />);
    const then = container.querySelector(".kbd-then");
    expect(then?.getAttribute("aria-hidden")).toBe("true");
  });
});

describe("MOD_KEY", () => {
  it('resolves to either the macOS ⌘ glyph or "Ctrl" — never anything else', () => {
    expect(["⌘", "Ctrl"]).toContain(MOD_KEY);
  });
});
