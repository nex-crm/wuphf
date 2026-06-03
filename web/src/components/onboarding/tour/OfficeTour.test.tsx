import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { OfficeTour } from "./OfficeTour";
import {
  OFFICE_TOUR_COPY,
  OFFICE_TOUR_LABELS,
  OFFICE_TOUR_SLIDE_IDS,
} from "./tourSlides";

interface RenderArgs {
  open?: boolean;
  onClose?: () => void;
  onFinish?: () => void;
}

function renderTour({ open = true, onClose, onFinish }: RenderArgs = {}) {
  const close = onClose ?? vi.fn();
  const finish = onFinish ?? vi.fn();
  const utils = render(
    <OfficeTour open={open} onClose={close} onFinish={finish} />,
  );
  return { ...utils, onClose: close, onFinish: finish };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("OfficeTour rendering", () => {
  it("renders nothing when closed", () => {
    renderTour({ open: false });
    expect(screen.queryByTestId("office-tour")).not.toBeInTheDocument();
  });

  it("renders the modal dialog with the tour label when open", () => {
    renderTour();
    const dialog = screen.getByRole("dialog", {
      name: OFFICE_TOUR_LABELS.dialog,
    });
    expect(dialog).toBeInTheDocument();
    expect(dialog).toHaveAttribute("aria-modal", "true");
  });

  it("opens on the intro slide with the Next CTA, not Finish", () => {
    renderTour();
    expect(
      screen.getByRole("heading", { name: OFFICE_TOUR_COPY.intro.headline }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("office-tour-primary")).toHaveTextContent(
      OFFICE_TOUR_LABELS.next,
    );
    // No Back button on the first slide.
    expect(screen.queryByTestId("office-tour-back")).not.toBeInTheDocument();
  });

  it("renders a progress indicator with one dot per slide", () => {
    renderTour();
    for (const id of OFFICE_TOUR_SLIDE_IDS) {
      expect(screen.getByTestId(`office-tour-dot-${id}`)).toBeInTheDocument();
    }
    expect(
      screen.getByRole("img", {
        name: `Step 1 of ${OFFICE_TOUR_SLIDE_IDS.length}`,
      }),
    ).toBeInTheDocument();
  });
});

describe("OfficeTour navigation", () => {
  it("advances through all four slides with Next, then shows the Finish CTA", async () => {
    const user = userEvent.setup();
    renderTour();

    const primary = screen.getByTestId("office-tour-primary");

    // intro -> agents
    await user.click(primary);
    expect(
      screen.getByRole("heading", { name: OFFICE_TOUR_COPY.agents.headline }),
    ).toBeInTheDocument();

    // agents -> issues
    await user.click(screen.getByTestId("office-tour-primary"));
    expect(
      screen.getByRole("heading", { name: OFFICE_TOUR_COPY.issues.headline }),
    ).toBeInTheDocument();

    // issues -> wiki (last slide)
    await user.click(screen.getByTestId("office-tour-primary"));
    expect(
      screen.getByRole("heading", { name: OFFICE_TOUR_COPY.wiki.headline }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("office-tour-primary")).toHaveTextContent(
      OFFICE_TOUR_LABELS.finish,
    );
  });

  it("Back returns to the previous slide", async () => {
    const user = userEvent.setup();
    renderTour();

    await user.click(screen.getByTestId("office-tour-primary")); // intro -> agents
    expect(
      screen.getByRole("heading", { name: OFFICE_TOUR_COPY.agents.headline }),
    ).toBeInTheDocument();

    await user.click(screen.getByTestId("office-tour-back")); // agents -> intro
    expect(
      screen.getByRole("heading", { name: OFFICE_TOUR_COPY.intro.headline }),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("office-tour-back")).not.toBeInTheDocument();
  });

  it("ArrowRight advances and ArrowLeft goes back", async () => {
    const user = userEvent.setup();
    renderTour();

    await user.keyboard("{ArrowRight}");
    expect(
      screen.getByRole("heading", { name: OFFICE_TOUR_COPY.agents.headline }),
    ).toBeInTheDocument();

    await user.keyboard("{ArrowLeft}");
    expect(
      screen.getByRole("heading", { name: OFFICE_TOUR_COPY.intro.headline }),
    ).toBeInTheDocument();
  });

  it("does not advance past the last slide on repeated Next presses", async () => {
    const user = userEvent.setup();
    const { onFinish } = renderTour();

    // Walk to the last slide.
    await user.click(screen.getByTestId("office-tour-primary"));
    await user.click(screen.getByTestId("office-tour-primary"));
    await user.click(screen.getByTestId("office-tour-primary"));
    expect(screen.getByTestId("office-tour-primary")).toHaveTextContent(
      OFFICE_TOUR_LABELS.finish,
    );
    // The next press is a finish, not an over-advance.
    expect(onFinish).not.toHaveBeenCalled();
  });
});

describe("OfficeTour finish + skip", () => {
  it("the finish button fires onFinish then onClose", async () => {
    const user = userEvent.setup();
    const onFinish = vi.fn();
    const onClose = vi.fn();
    renderTour({ onFinish, onClose });

    // Walk to the wiki slide.
    await user.click(screen.getByTestId("office-tour-primary"));
    await user.click(screen.getByTestId("office-tour-primary"));
    await user.click(screen.getByTestId("office-tour-primary"));

    await user.click(screen.getByTestId("office-tour-primary"));
    expect(onFinish).toHaveBeenCalledTimes(1);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("ArrowRight on the last slide also finishes", async () => {
    const user = userEvent.setup();
    const onFinish = vi.fn();
    renderTour({ onFinish });

    await user.keyboard("{ArrowRight}{ArrowRight}{ArrowRight}");
    expect(screen.getByTestId("office-tour-primary")).toHaveTextContent(
      OFFICE_TOUR_LABELS.finish,
    );

    await user.keyboard("{ArrowRight}");
    expect(onFinish).toHaveBeenCalledTimes(1);
  });

  it("the skip button closes the tour (persist handled by onClose)", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    const onFinish = vi.fn();
    renderTour({ onClose, onFinish });

    await user.click(screen.getByTestId("office-tour-skip"));
    expect(onClose).toHaveBeenCalledTimes(1);
    // Skip is not a finish.
    expect(onFinish).not.toHaveBeenCalled();
  });

  it("Esc closes the tour without firing onFinish", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    const onFinish = vi.fn();
    renderTour({ onClose, onFinish });

    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onFinish).not.toHaveBeenCalled();
  });
});

describe("OfficeTour focus containment", () => {
  it("wraps Tab from the last control back to the first", () => {
    renderTour();
    const skip = screen.getByTestId("office-tour-skip");
    const primary = screen.getByTestId("office-tour-primary");

    // Tab off the last focusable (primary) wraps to the first (skip).
    primary.focus();
    fireEvent.keyDown(window, { key: "Tab" });
    expect(document.activeElement).toBe(skip);
  });

  it("wraps Shift+Tab from the first control to the last", () => {
    renderTour();
    const skip = screen.getByTestId("office-tour-skip");
    const primary = screen.getByTestId("office-tour-primary");

    skip.focus();
    fireEvent.keyDown(window, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(primary);
  });
});

describe("OfficeTour replay reset", () => {
  it("re-opening resets back to the intro slide", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <OfficeTour open={true} onClose={vi.fn()} onFinish={vi.fn()} />,
    );

    // Advance to slide 2, then close.
    await user.click(screen.getByTestId("office-tour-primary"));
    expect(
      screen.getByRole("heading", { name: OFFICE_TOUR_COPY.agents.headline }),
    ).toBeInTheDocument();

    rerender(<OfficeTour open={false} onClose={vi.fn()} onFinish={vi.fn()} />);
    expect(screen.queryByTestId("office-tour")).not.toBeInTheDocument();

    // Re-open: the tour should land on the intro slide again.
    rerender(<OfficeTour open={true} onClose={vi.fn()} onFinish={vi.fn()} />);
    expect(
      screen.getByRole("heading", { name: OFFICE_TOUR_COPY.intro.headline }),
    ).toBeInTheDocument();
  });
});
