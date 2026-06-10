import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SearchModal } from "./SearchModal";

// useChannels / useOfficeMembers return `undefined` data (the loading state) so
// the component's `= []` defaults hand back a FRESH array on every render —
// which rebuilds `runSearch` → `handleQueryChange` each render. That is the
// exact condition that made the combined open/close effect loop
// ("Maximum update depth exceeded") before the reset branch was split onto the
// stable `searchOpen` boolean. Rendering the (closed) modal under that
// condition must NOT loop.
vi.mock("../../hooks/useChannels", () => ({
  useChannels: () => ({ data: undefined }),
}));
vi.mock("../../hooks/useMembers", () => ({
  useOfficeMembers: () => ({ data: undefined }),
}));

describe("<SearchModal> render-loop regression", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("does not enter a setState-in-effect loop while channels/members load", () => {
    // With the regression this render throws "Maximum update depth exceeded"
    // as the reset effect re-runs off the churning callback identity. The
    // modal is closed by default, so it renders null when healthy.
    const { container } = render(<SearchModal />);
    expect(container).toBeTruthy();
  });
});
