import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { submitJoinInvite } from "../../api/joinInvite";
import { JoinPage } from "./JoinPage";

vi.mock("../../api/joinInvite", () => ({
  submitJoinInvite: vi.fn(),
}));

const submitJoinInviteMock = vi.mocked(submitJoinInvite);

beforeEach(() => {
  vi.clearAllMocks();
});

describe("JoinPage", () => {
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
    });
    expect(onAccepted).toHaveBeenCalledWith("/#/channels/general");
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

    resolveSubmit({
      ok: false,
      code: "invite_invalid",
      message: "Invite could not be accepted.",
    });
    await screen.findByRole("alert");
  });
});
