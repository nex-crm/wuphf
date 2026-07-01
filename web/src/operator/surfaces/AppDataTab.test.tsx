import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AppDataTab } from "./AppDataTab";

// vi.mock is hoisted above this file's const declarations, so the mock handle
// must be hoisted too or the factory hits the TDZ.
const { get } = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("../../api/client", () => ({
  get,
}));

function wrap(node: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

describe("AppDataTab", () => {
  beforeEach(() => {
    get.mockReset();
  });

  it("reads the app DB directly (GET /apps/{id}/db, no AI call)", async () => {
    get.mockResolvedValue({ tables: [] });
    wrap(<AppDataTab appId="app_abc" />);
    await waitFor(() => expect(get).toHaveBeenCalledWith("/apps/app_abc/db"));
    // Exactly one read; no /apps/ai derivation.
    expect(get).toHaveBeenCalledTimes(1);
    expect(get.mock.calls.every(([p]) => !String(p).includes("/apps/ai"))).toBe(
      true,
    );
  });

  it("shows an empty state when the app has no tables yet", async () => {
    get.mockResolvedValue({ tables: [] });
    const { getByText } = wrap(<AppDataTab appId="app_abc" />);
    await waitFor(() => expect(getByText(/no data yet/i)).toBeTruthy());
  });

  it("renders each table with typed headers and row values", async () => {
    get.mockResolvedValue({
      tables: [
        {
          name: "Emails",
          columns: [
            { name: "sender", type: "string" },
            { name: "urgency", type: "number" },
          ],
          rows: [
            { sender: "a@b.com", urgency: 72 },
            { sender: "c@d.com", urgency: 15 },
          ],
        },
      ],
    });
    const { getByText, getAllByText } = wrap(<AppDataTab appId="app_abc" />);
    await waitFor(() => expect(getByText("Emails")).toBeTruthy());
    expect(getByText("sender")).toBeTruthy();
    expect(getByText("urgency")).toBeTruthy();
    // Typed header shows the column type.
    expect(getAllByText("number").length).toBeGreaterThan(0);
    expect(getByText("a@b.com")).toBeTruthy();
    expect(getByText("2 rows")).toBeTruthy();
  });

  it("shows a defined-but-empty note for a table with no rows", async () => {
    get.mockResolvedValue({
      tables: [
        { name: "Log", columns: [{ name: "msg", type: "string" }], rows: [] },
      ],
    });
    const { getByText } = wrap(<AppDataTab appId="app_abc" />);
    await waitFor(() =>
      expect(getByText(/defined, no rows yet/i)).toBeTruthy(),
    );
  });

  it("drops malformed row entries (null, array) without crashing", async () => {
    get.mockResolvedValue({
      tables: [
        {
          name: "Emails",
          columns: [{ name: "sender", type: "string" }],
          rows: [null, ["not", "a", "row"], { sender: "a@b.com" }],
        },
      ],
    });
    const { getByText } = wrap(<AppDataTab appId="app_abc" />);
    await waitFor(() => expect(getByText("Emails")).toBeTruthy());
    // Only the plain-object row survives; the tab renders instead of crashing.
    expect(getByText("a@b.com")).toBeTruthy();
    expect(getByText("1 row")).toBeTruthy();
  });

  it("shows an error state when the DB read fails", async () => {
    get.mockRejectedValue(new Error("boom"));
    const { getByText } = wrap(<AppDataTab appId="app_abc" />);
    await waitFor(() =>
      expect(getByText(/could not read this agent’s data/i)).toBeTruthy(),
    );
  });
});
