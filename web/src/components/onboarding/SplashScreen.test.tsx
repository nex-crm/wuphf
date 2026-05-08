import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SplashScreen } from "./SplashScreen";

afterEach(() => {
  vi.useRealTimers();
});

describe("SplashScreen", () => {
  it("calls onDone once when dismiss is triggered more than once", () => {
    vi.useFakeTimers();
    const onDone = vi.fn();

    render(<SplashScreen onDone={onDone} />);

    const dismissButton = screen.getByRole("button", {
      name: /Dismiss splash screen/i,
    });
    fireEvent.click(dismissButton);
    fireEvent.click(dismissButton);

    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(onDone).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(onDone).toHaveBeenCalledTimes(1);
  });
});
