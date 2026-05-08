import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Skill } from "../../../api/client";
import { PixelSkillCard } from "./PixelSkillCard";

function makeSkill(overrides: Partial<Skill> = {}): Skill {
  return {
    name: "deploy-on-merge",
    title: "Deploy on Merge",
    description: "Ship to prod when a PR lands on main.",
    trigger: "PR merged to main",
    status: "active",
    owner_agents: ["deploy-bot"],
    created_by: "deploy-bot",
    created_at: "2026-05-07T10:00:00Z",
    updated_at: "2026-05-07T10:00:00Z",
    ...overrides,
  };
}

describe("<PixelSkillCard>", () => {
  it("renders the skill name and creator byline in the header", () => {
    render(<PixelSkillCard skill={makeSkill()} />);
    // Title shows on both faces (front header + back title) so we use
    // getAllByText.  The point of the test is presence + the byline +
    // the absence of the old fake HP indicator.
    expect(screen.getAllByText("Deploy on Merge").length).toBeGreaterThan(0);
    expect(screen.queryByLabelText(/hit points/i)).not.toBeInTheDocument();
    expect(screen.getByText(/by @deploy-bot/i)).toBeInTheDocument();
  });

  it("renders the SKILL.md description as flavor text (no fake label)", () => {
    render(<PixelSkillCard skill={makeSkill()} />);
    expect(
      screen.getByText("Ship to prod when a PR lands on main."),
    ).toBeInTheDocument();
    // The description block is the flavor paragraph — no "Power" / "Ability"
    // chrome label, since neither is a real Skill field.
    expect(screen.queryByText(/Power/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Ability/i)).not.toBeInTheDocument();
  });

  it("surfaces the trigger field on the front face under the stat strip", () => {
    render(<PixelSkillCard skill={makeSkill()} />);
    // Trigger is the field that decides whether a skill is relevant to
    // the current moment, so we promoted it from the back-face detail
    // list onto the front. The label reads "Triggers on" + value.
    expect(screen.getByText(/Triggers on/i)).toBeInTheDocument();
    expect(screen.getByText("PR merged to main")).toBeInTheDocument();
    // The back-face detail list should NOT duplicate it.
    const back = screen.getByTestId("pixel-skill-card-back");
    expect(back.textContent).not.toContain("Trigger:");
    // Invoke / Recall were content-less decorations in earlier drafts.
    expect(screen.queryByText(/^Invoke$/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Recall/i)).not.toBeInTheDocument();
  });

  it("renders the NEEDS REVIEW stamp only on proposed cards", () => {
    const { rerender } = render(
      <PixelSkillCard skill={makeSkill({ status: "proposed" })} />,
    );
    expect(screen.getByLabelText(/Needs review/i)).toBeInTheDocument();

    rerender(<PixelSkillCard skill={makeSkill({ status: "active" })} />);
    expect(screen.queryByLabelText(/Needs review/i)).not.toBeInTheDocument();

    rerender(<PixelSkillCard skill={makeSkill({ status: "disabled" })} />);
    expect(screen.queryByLabelText(/Needs review/i)).not.toBeInTheDocument();
  });

  it("marks the back face inert when not flipped (a11y)", () => {
    render(<PixelSkillCard skill={makeSkill()} />);
    const back = screen.getByTestId("pixel-skill-card-back");
    // React 19 serializes boolean true as the empty string on inert.
    expect(back.hasAttribute("inert")).toBe(true);
  });

  it("marks the front face inert AFTER flipping (symmetric a11y)", () => {
    render(<PixelSkillCard skill={makeSkill()} />);
    const front = screen.getByTestId("pixel-skill-card");
    // Pre-flip: front is interactive, back is inert.
    expect(front.hasAttribute("inert")).toBe(false);
    fireEvent.click(
      screen.getByRole("button", {
        name: /Flip deploy-on-merge card to see details/i,
      }),
    );
    // Post-flip: front (rotated away in 3D) gains inert so its flip
    // button doesn't stay in the tab order.
    expect(front.hasAttribute("inert")).toBe(true);
  });

  it("maps active skills to the electric type", () => {
    render(<PixelSkillCard skill={makeSkill({ status: "active" })} />);
    expect(screen.getByTestId("pixel-skill-card")).toHaveAttribute(
      "data-type",
      "electric",
    );
  });

  it("maps proposed skills to the psychic type", () => {
    render(<PixelSkillCard skill={makeSkill({ status: "proposed" })} />);
    const card = screen.getByTestId("pixel-skill-card");
    expect(card).toHaveAttribute("data-type", "psychic");
    // The old `--holo` decorator was retired; proposed cards now signal
    // "needs review" via a corner stamp, which is asserted in the
    // dedicated stamp test below.
  });

  it("maps disabled and archived skills to dark and steel types", () => {
    const { rerender } = render(
      <PixelSkillCard skill={makeSkill({ status: "disabled" })} />,
    );
    expect(screen.getByTestId("pixel-skill-card")).toHaveAttribute(
      "data-type",
      "dark",
    );

    rerender(<PixelSkillCard skill={makeSkill({ status: "archived" })} />);
    expect(screen.getByTestId("pixel-skill-card")).toHaveAttribute(
      "data-type",
      "steel",
    );
  });

  it("renders the procedural portrait as a canvas inside the art window", () => {
    const { container } = render(<PixelSkillCard skill={makeSkill()} />);
    const canvas = container.querySelector(".pixel-skill-card__portrait");
    expect(canvas).not.toBeNull();
    expect(canvas?.tagName).toBe("CANVAS");
  });

  it("flips the card to the detail back when the flip button is clicked", () => {
    render(<PixelSkillCard skill={makeSkill()} />);
    const card = screen.getByTestId("pixel-skill-card");
    expect(card.closest(".pixel-skill-card-wrap")?.className).not.toContain(
      "is-flipped",
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: /Flip deploy-on-merge card to see details/i,
      }),
    );
    expect(card.closest(".pixel-skill-card-wrap")?.className).toContain(
      "is-flipped",
    );
  });

  it("opens the SKILL.md preview from the back face's CTA", () => {
    const onPreview = vi.fn();
    render(<PixelSkillCard skill={makeSkill()} onPreview={onPreview} />);
    // Flip first — the back face is aria-hidden until flipped, so the
    // CTA button isn't queryable by accessible role until then.
    fireEvent.click(
      screen.getByRole("button", {
        name: /Flip deploy-on-merge card to see details/i,
      }),
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: /View full SKILL.md for deploy-on-merge/i,
      }),
    );
    expect(onPreview).toHaveBeenCalledTimes(1);
  });

  it("flips back to the front via the flip-back affordance", () => {
    render(<PixelSkillCard skill={makeSkill()} />);
    const wrap = screen
      .getByTestId("pixel-skill-card")
      .closest(".pixel-skill-card-wrap") as HTMLElement;
    fireEvent.click(
      screen.getByRole("button", {
        name: /Flip deploy-on-merge card to see details/i,
      }),
    );
    expect(wrap.className).toContain("is-flipped");
    fireEvent.click(
      screen.getByRole("button", {
        name: /Flip deploy-on-merge card back to front/i,
      }),
    );
    expect(wrap.className).not.toContain("is-flipped");
  });

  it("renders real skill metadata on the detail back", () => {
    render(
      <PixelSkillCard
        skill={makeSkill({
          created_by: "ceo",
          created_at: "2026-04-01T10:00:00Z",
          updated_at: "2026-05-07T10:00:00Z",
          source: "wiki:grill-me",
        })}
      />,
    );
    const back = screen.getByTestId("pixel-skill-card-back");
    expect(back).toBeInTheDocument();
    expect(back.textContent).toContain("Status");
    // Status value is Title-cased on the back face for parity with other
    // label/value pairs.
    expect(back.textContent).toContain("Active");
    expect(back.textContent).toContain("Owners");
    expect(back.textContent).toContain("Created by");
    expect(back.textContent).toContain("@ceo");
    expect(back.textContent).toContain("Source");
    expect(back.textContent).toContain("wiki:grill-me");
  });

  it("renders supplied actions below the card frame", () => {
    render(
      <PixelSkillCard
        skill={makeSkill()}
        actions={<button type="button">Disable</button>}
      />,
    );
    expect(screen.getByRole("button", { name: "Disable" })).toBeInTheDocument();
  });

  it("shows lead-routable when no owner agents are scoped", () => {
    render(<PixelSkillCard skill={makeSkill({ owner_agents: [] })} />);
    // Front-face stat strip uses 'Lead-routable'; the back face also lists
    // 'Lead-routable' as the Owners value, so we expect at least one match.
    expect(screen.getAllByText(/Lead-routable/i).length).toBeGreaterThan(0);
  });

  it("shows the @-prefixed owner list when scoped", () => {
    render(
      <PixelSkillCard
        skill={makeSkill({ owner_agents: ["deploy-bot", "csm"] })}
      />,
    );
    // Front strip uses ' / ' as separator; the back face uses ', '. Both
    // are real owners, so confirm the front-strip rendering specifically.
    expect(screen.getByText("@deploy-bot / @csm")).toBeInTheDocument();
  });

  it("does not render fake HP or rarity stars (only real Skill fields)", () => {
    render(<PixelSkillCard skill={makeSkill()} />);
    expect(screen.queryByLabelText(/hit points/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/Rarity/i)).not.toBeInTheDocument();
  });
});
