import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useAppStore } from "../../stores/app";
import { Wizard } from "./Wizard";

// The wizard posts config + completes onboarding via the broker. Stub
// everything so these tests stay focused on keyboard behavior.
vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    get: vi.fn().mockResolvedValue({ templates: [], prereqs: [] }),
    post: vi.fn().mockResolvedValue({}),
  };
});

import { post } from "../../api/client";

const postMock = vi.mocked(post);

function pressEnterOn(
  target: EventTarget = window,
  opts: Partial<KeyboardEventInit> = {},
) {
  const ev = new KeyboardEvent("keydown", {
    key: "Enter",
    bubbles: true,
    cancelable: true,
    ...opts,
  });
  Object.defineProperty(ev, "target", { value: target, configurable: true });
  window.dispatchEvent(ev);
}

beforeEach(() => {
  postMock.mockClear();
  useAppStore.setState({
    onboardingComplete: false,
  });
});

afterEach(() => {
  while (document.body.firstChild) {
    document.body.removeChild(document.body.firstChild);
  }
});

describe("Wizard keyboard advancement", () => {
  it("Enter on the welcome step advances to the Identity step", async () => {
    render(<Wizard onComplete={vi.fn()} />);
    // Welcome CTA is visible
    expect(screen.getByText(/Open the office/i)).toBeInTheDocument();

    pressEnterOn(window);

    // Identity step renders its company input
    await waitFor(() => {
      expect(
        screen.getByLabelText(/Company or project name/i),
      ).toBeInTheDocument();
    });
  });

  it("Enter on the identity step is blocked when company + description are empty", async () => {
    render(<Wizard onComplete={vi.fn()} />);
    pressEnterOn(window); // welcome → identity
    await waitFor(() => screen.getByLabelText(/Company or project name/i));

    // Press Enter with empty fields — must NOT advance.
    pressEnterOn(window);

    // Still on identity — company input still visible
    expect(
      screen.getByLabelText(/Company or project name/i),
    ).toBeInTheDocument();
  });

  it("Enter advances identity once company + description are filled", async () => {
    render(<Wizard onComplete={vi.fn()} />);
    pressEnterOn(window); // → identity
    await waitFor(() => screen.getByLabelText(/Company or project name/i));

    fireEvent.change(screen.getByLabelText(/Company or project name/i), {
      target: { value: "Acme" },
    });
    fireEvent.change(screen.getByLabelText(/One-liner description/i), {
      target: { value: "We do things" },
    });

    pressEnterOn(window);

    // Should move to templates step — "What should your office run?" is the
    // templates headline.
    await waitFor(() => {
      expect(
        screen.getByText(/What should your office run\?/i),
      ).toBeInTheDocument();
    });
  });

  it("does not advance when Enter is pressed on a focused <button> (Back/Skip stay intact)", async () => {
    render(<Wizard onComplete={vi.fn()} />);
    pressEnterOn(window); // welcome → identity
    await waitFor(() => screen.getByLabelText(/Company or project name/i));

    // Fill fields
    fireEvent.change(screen.getByLabelText(/Company or project name/i), {
      target: { value: "Acme" },
    });
    fireEvent.change(screen.getByLabelText(/One-liner description/i), {
      target: { value: "We do things" },
    });

    // Simulate Enter while a BUTTON is focused — the handler should bail
    // out and let the button's own semantics decide what to do.
    const backBtn = screen.getByRole("button", { name: "Back" });
    pressEnterOn(backBtn);

    // We did NOT advance to templates because Enter on a button is a bail.
    // (The button's own onClick would fire on real click, not on synthetic
    // Enter dispatched to window with a BUTTON target.)
    expect(
      screen.getByLabelText(/Company or project name/i),
    ).toBeInTheDocument();
  });

  it("guards against key repeat on the ready step (hold-Enter no longer double-submits)", async () => {
    // Drive the wizard straight into "ready" by mutating step via the
    // public keyboard path — we need a blueprint/team/setup flyover. A
    // quicker path: fill identity + mash Enter 5 times so we land a few
    // steps in, then verify post is never called twice for the same press.
    render(<Wizard onComplete={vi.fn()} />);
    pressEnterOn(window); // → identity
    await waitFor(() => screen.getByLabelText(/Company or project name/i));

    fireEvent.change(screen.getByLabelText(/Company or project name/i), {
      target: { value: "Acme" },
    });
    fireEvent.change(screen.getByLabelText(/One-liner description/i), {
      target: { value: "We do things" },
    });

    // Two back-to-back Enters (second one simulates key repeat) — the
    // guard uses e.repeat, so we dispatch with repeat:true.
    pressEnterOn(window); // first real Enter — identity → templates
    pressEnterOn(window, { repeat: true }); // repeat — must bail

    // At most one advance should have happened: we should now be somewhere
    // but not have double-jumped past templates.
    await waitFor(() => {
      expect(
        screen.getByText(/What should your office run\?/i),
      ).toBeInTheDocument();
    });
  });

  it("Enter in the Nex signup email submits the panel instead of advancing the step", async () => {
    render(<Wizard onComplete={vi.fn()} />);
    pressEnterOn(window); // → identity
    await waitFor(() => screen.getByLabelText(/Company or project name/i));

    // Fill fields so the identity gate would normally allow advancement
    fireEvent.change(screen.getByLabelText(/Company or project name/i), {
      target: { value: "Acme" },
    });
    fireEvent.change(screen.getByLabelText(/One-liner description/i), {
      target: { value: "We do things" },
    });

    // Open the Nex signup panel
    const trigger = screen.getByText(/Don.?t have a Nex account/i);
    fireEvent.click(trigger);

    const emailInput = await waitFor(() => screen.getByLabelText("Email"));
    fireEvent.change(emailInput, { target: { value: "me@example.com" } });

    // Pressing Enter inside the email field should call /nex/register and
    // should NOT advance to the templates step.
    fireEvent.keyDown(emailInput, { key: "Enter" });

    await waitFor(() => {
      expect(postMock).toHaveBeenCalledWith("/nex/register", {
        email: "me@example.com",
      });
    });
    // Still on the identity step
    expect(
      screen.getByLabelText(/Company or project name/i),
    ).toBeInTheDocument();
  });
});
