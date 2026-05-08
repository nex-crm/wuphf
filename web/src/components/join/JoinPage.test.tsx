import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { submitJoinInvite } from "../../api/joinInvite";
import { JoinPage } from "./JoinPage";

vi.mock("../../api/joinInvite", () => ({
  submitJoinInvite: vi.fn(),
}));

const submitJoinInviteMock = vi.mocked(submitJoinInvite);

describe("JoinPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders an empty-state when the token is blank", () => {
    render(<JoinPage token="   " />);
    expect(
      screen.getByText("Invite link is missing its token"),
    ).toBeInTheDocument();
  });

  it("renders an empty-state when the token is the empty string", () => {
    render(<JoinPage token="" />);
    expect(
      screen.getByText("Invite link is missing its token"),
    ).toBeInTheDocument();
  });

  it("requires a display name before submitting", async () => {
    const user = userEvent.setup();
    render(<JoinPage token="invite-1" />);

    await user.click(screen.getByRole("button", { name: /enter office/i }));

    expect(screen.getByRole("alert")).toHaveTextContent("Add a display name");
    expect(submitJoinInviteMock).not.toHaveBeenCalled();
  });

  it("submits the trimmed display name and calls onAccepted with the redirect", async () => {
    const user = userEvent.setup();
    submitJoinInviteMock.mockResolvedValue({
      ok: true,
      redirect: "/#/channels/general",
      display_name: "Maya",
    });
    const onAccepted = vi.fn();
    render(<JoinPage token="invite-1" onAccepted={onAccepted} />);

    await user.type(screen.getByLabelText(/display name/i), "  Maya  ");
    await user.click(screen.getByRole("button", { name: /enter office/i }));

    expect(submitJoinInviteMock).toHaveBeenCalledWith({
      token: "invite-1",
      displayName: "Maya",
      passcode: undefined,
    });
    expect(onAccepted).toHaveBeenCalledWith("/#/channels/general");
  });

  // Phase 2: tunnel-mode invites need a 6-digit passcode the host reads
  // out-of-band. The first submit goes without one, the server says
  // passcode_required, the form reveals the passcode field, the joiner
  // types the code, the second submit goes through.
  it("reveals the passcode field on passcode_required and resubmits with it", async () => {
    const user = userEvent.setup();
    submitJoinInviteMock
      .mockResolvedValueOnce({
        ok: false,
        code: "passcode_required",
        message: "This invite needs a passcode.",
      })
      .mockResolvedValueOnce({
        ok: true,
        redirect: "/#/channels/general",
        display_name: "Maya",
      });
    const onAccepted = vi.fn();
    render(<JoinPage token="invite-1" onAccepted={onAccepted} />);

    // No passcode field on first render.
    expect(screen.queryByLabelText(/passcode/i)).toBeNull();

    await user.type(screen.getByLabelText(/display name/i), "Maya");
    await user.click(screen.getByRole("button", { name: /enter office/i }));

    // First submit went without a passcode.
    expect(submitJoinInviteMock).toHaveBeenNthCalledWith(1, {
      token: "invite-1",
      displayName: "Maya",
      passcode: undefined,
    });

    // Field appears, alert renders, submit button re-enables.
    const passcodeInput = await screen.findByLabelText(/passcode/i);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("This invite needs a passcode.");
    expect(alert).toHaveAttribute("data-error-code", "passcode_required");

    await user.type(passcodeInput, "835291");
    await user.click(screen.getByRole("button", { name: /enter office/i }));

    expect(submitJoinInviteMock).toHaveBeenNthCalledWith(2, {
      token: "invite-1",
      displayName: "Maya",
      passcode: "835291",
    });
    expect(onAccepted).toHaveBeenCalledWith("/#/channels/general");
  });

  it("strips non-digit characters typed into the passcode field", async () => {
    const user = userEvent.setup();
    submitJoinInviteMock
      .mockResolvedValueOnce({
        ok: false,
        code: "passcode_required",
        message: "passcode needed",
      })
      .mockResolvedValueOnce({
        ok: true,
        redirect: "/#/channels/general",
        display_name: "Maya",
      });
    render(<JoinPage token="invite-1" onAccepted={vi.fn()} />);

    await user.type(screen.getByLabelText(/display name/i), "Maya");
    await user.click(screen.getByRole("button", { name: /enter office/i }));

    const passcodeInput = await screen.findByLabelText(/passcode/i);
    // Pasted with whitespace + dashes — the input should normalise to digits.
    await user.type(passcodeInput, "835-291");
    await user.click(screen.getByRole("button", { name: /enter office/i }));

    expect(submitJoinInviteMock).toHaveBeenNthCalledWith(2, {
      token: "invite-1",
      displayName: "Maya",
      passcode: "835291",
    });
  });

  it("renders the server error message when the invite is no longer valid", async () => {
    const user = userEvent.setup();
    submitJoinInviteMock.mockResolvedValue({
      ok: false,
      code: "invite_expired_or_used",
      message: "This invite is no longer valid.",
    });
    render(<JoinPage token="invite-1" />);

    await user.type(screen.getByLabelText(/display name/i), "Maya");
    await user.click(screen.getByRole("button", { name: /enter office/i }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("This invite is no longer valid.");
    expect(alert).toHaveAttribute("data-error-code", "invite_expired_or_used");
    // The submit button should be re-enabled so the joiner can retry once
    // the host hands them a fresh invite.
    expect(screen.getByRole("button", { name: /enter office/i })).toBeEnabled();
  });

  it("disables the submit button while a request is in flight", async () => {
    const user = userEvent.setup();
    let resolveSubmit: (value: {
      ok: false;
      code: "invite_invalid";
      message: string;
    }) => void = () => {};
    submitJoinInviteMock.mockReturnValue(
      new Promise((resolve) => {
        resolveSubmit = resolve;
      }),
    );
    render(<JoinPage token="invite-1" />);

    await user.type(screen.getByLabelText(/display name/i), "Maya");
    await user.click(screen.getByRole("button", { name: /enter office/i }));

    const pendingButton = screen.getByRole("button", { name: /entering/i });
    expect(pendingButton).toBeDisabled();
    expect(pendingButton).toHaveAttribute("aria-busy", "true");

    await act(async () => {
      resolveSubmit({
        ok: false,
        code: "invite_invalid",
        message: "Invite could not be accepted.",
      });
    });
    await screen.findByRole("alert");
  });

  // submitJoinInvite is contractually never-rejecting (it catches fetch
  // failures and returns a `network` JoinInviteFailure), but if a future
  // refactor lets a rejection escape we want JoinPage to recover instead of
  // hanging the user on a disabled button.
  it("recovers from an unexpected rejection in submitJoinInvite", async () => {
    const user = userEvent.setup();
    submitJoinInviteMock.mockRejectedValueOnce(new Error("boom"));
    render(<JoinPage token="invite-1" />);

    await user.type(screen.getByLabelText(/display name/i), "Maya");
    await user.click(screen.getByRole("button", { name: /enter office/i }));

    const alert = await screen.findByRole("alert");
    expect(alert).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /enter office/i })).toBeEnabled();
  });
});
