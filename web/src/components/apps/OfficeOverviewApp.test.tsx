import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  getAllRequests,
  getLocalProvidersStatus,
  getOfficeMembers,
  getScheduler,
  getSkillsList,
} from "../../api/client";
import { getOfficeTasks } from "../../api/tasks";
import { OfficeOverviewApp } from "./OfficeOverviewApp";

// ── Mocks ──────────────────────────────────────────────────────────

vi.mock("../../api/client", () => ({
  getAllRequests: vi.fn(),
  getLocalProvidersStatus: vi.fn(),
  getOfficeMembers: vi.fn(),
  getScheduler: vi.fn(),
  getSkillsList: vi.fn(),
}));

vi.mock("../../api/tasks", () => ({
  getOfficeTasks: vi.fn(),
}));

// Router navigation — avoid "not in browser context" errors in jsdom.
vi.mock("../../lib/router", () => ({
  router: { navigate: vi.fn() },
}));

const mockGetOfficeTasks = vi.mocked(getOfficeTasks);
const mockGetOfficeMembers = vi.mocked(getOfficeMembers);
const mockGetAllRequests = vi.mocked(getAllRequests);
const mockGetSkillsList = vi.mocked(getSkillsList);
const mockGetScheduler = vi.mocked(getScheduler);
const mockGetLocalProvidersStatus = vi.mocked(getLocalProvidersStatus);

// ── Helpers ────────────────────────────────────────────────────────

function wrap(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>;
}

function emptyDefaults() {
  mockGetOfficeTasks.mockResolvedValue({ tasks: [] });
  mockGetOfficeMembers.mockResolvedValue({ members: [] });
  mockGetAllRequests.mockResolvedValue({ requests: [] });
  mockGetSkillsList.mockResolvedValue({ skills: [] });
  mockGetScheduler.mockResolvedValue({ jobs: [] });
  mockGetLocalProvidersStatus.mockResolvedValue([]);
}

// ── Tests ──────────────────────────────────────────────────────────

beforeEach(() => {
  vi.clearAllMocks();
});

describe("OfficeOverviewApp", () => {
  describe("full data render", () => {
    it("renders all section headings with data loaded", async () => {
      mockGetOfficeTasks.mockResolvedValue({
        tasks: [
          {
            id: "t1",
            title: "Ship landing page",
            status: "in_progress",
            owner: "alex",
            channel: "general",
            updated_at: new Date().toISOString(),
          },
          {
            id: "t2",
            title: "Fix auth bug",
            status: "blocked",
            owner: "riley",
            channel: "eng",
            updated_at: new Date().toISOString(),
          },
        ],
      });
      mockGetOfficeMembers.mockResolvedValue({
        members: [
          {
            slug: "alex",
            name: "Alex",
            role: "engineer",
            status: "shipping",
            task: "Ship landing page",
          },
        ],
      });
      mockGetAllRequests.mockResolvedValue({
        requests: [
          {
            id: "r1",
            from: "alex",
            question: "Should I proceed with the deployment?",
            status: "open",
            blocking: true,
          },
        ],
      });
      mockGetSkillsList.mockResolvedValue({
        skills: [
          {
            name: "deploy-frontend",
            title: "Deploy frontend",
            status: "proposed" as const,
            created_by: "alex",
          },
        ],
      });
      mockGetScheduler.mockResolvedValue({
        jobs: [
          {
            slug: "daily-digest",
            label: "Daily digest",
            next_run: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
            enabled: true,
          },
        ],
      });
      mockGetLocalProvidersStatus.mockResolvedValue([]);

      render(wrap(<OfficeOverviewApp />));

      expect(
        await screen.findByTestId("office-overview-app"),
      ).toBeInTheDocument();

      expect(screen.getByText("Office overview")).toBeInTheDocument();
      expect(screen.getByText("Active runs")).toBeInTheDocument();
      expect(screen.getByText("Blocked tasks")).toBeInTheDocument();
      expect(screen.getByText("Agents working now")).toBeInTheDocument();
      expect(screen.getByText("Pending reviews")).toBeInTheDocument();
      expect(screen.getByText("Wiki proposals")).toBeInTheDocument();
      expect(screen.getByText("Next scheduled jobs")).toBeInTheDocument();
      expect(screen.getByText("Recent artifacts")).toBeInTheDocument();

      // Spot-check data in sections. Task titles, questions, etc. can appear
      // in multiple sections (active runs, recent artifacts, agent task label,
      // request label + body). Use getAllByText to allow duplicates.
      const shipLandingMatches =
        await screen.findAllByText("Ship landing page");
      expect(shipLandingMatches.length).toBeGreaterThanOrEqual(1);
      const fixAuthMatches = screen.getAllByText("Fix auth bug");
      expect(fixAuthMatches.length).toBeGreaterThanOrEqual(1);
      expect(screen.getByText("Alex")).toBeInTheDocument();
      // The question text shows up as both label and body in the request card.
      const deployQuestionMatches = screen.getAllByText(
        "Should I proceed with the deployment?",
      );
      expect(deployQuestionMatches.length).toBeGreaterThanOrEqual(1);
      expect(screen.getByText("Deploy frontend")).toBeInTheDocument();
      expect(screen.getByText("Daily digest")).toBeInTheDocument();
    });
  });

  describe("empty states", () => {
    beforeEach(() => {
      emptyDefaults();
    });

    it("shows empty states for active runs when no tasks are running", async () => {
      render(wrap(<OfficeOverviewApp />));

      expect(
        await screen.findByText("No tasks are running right now."),
      ).toBeInTheDocument();
    });

    it("shows empty state for blocked tasks when nothing is blocked", async () => {
      render(wrap(<OfficeOverviewApp />));

      expect(
        await screen.findByText(
          "Nothing is blocked. Agents are moving freely.",
        ),
      ).toBeInTheDocument();
    });

    it("shows empty state for agents when all are idle", async () => {
      render(wrap(<OfficeOverviewApp />));

      expect(
        await screen.findByText("No agents are visibly active right now."),
      ).toBeInTheDocument();
    });

    it("shows empty state for pending reviews when no requests exist", async () => {
      render(wrap(<OfficeOverviewApp />));

      expect(
        await screen.findByText("No pending requests from agents."),
      ).toBeInTheDocument();
    });

    it("shows empty state for wiki proposals when none are pending", async () => {
      render(wrap(<OfficeOverviewApp />));

      expect(
        await screen.findByText("No skill proposals awaiting review."),
      ).toBeInTheDocument();
    });

    it("shows empty state for scheduled jobs when none exist", async () => {
      render(wrap(<OfficeOverviewApp />));

      expect(
        await screen.findByText("No upcoming scheduled jobs."),
      ).toBeInTheDocument();
    });

    it("shows empty state for recent artifacts when no tasks exist", async () => {
      render(wrap(<OfficeOverviewApp />));

      expect(
        await screen.findByText("No recent task activity."),
      ).toBeInTheDocument();
    });
  });

  describe("provider warnings", () => {
    beforeEach(() => {
      emptyDefaults();
    });

    it("shows provider warning section when a provider is unhealthy", async () => {
      mockGetLocalProvidersStatus.mockResolvedValue([
        {
          kind: "ollama",
          binary_installed: true,
          binary_path: "/usr/local/bin/ollama",
          endpoint: "http://localhost:11434",
          model: "llama3",
          reachable: false,
          probed: true,
          platform_supported: true,
        },
      ]);

      render(wrap(<OfficeOverviewApp />));

      expect(
        await screen.findByText("1 provider unreachable"),
      ).toBeInTheDocument();
      expect(screen.getByText("ollama")).toBeInTheDocument();
    });

    it("shows links to Settings and Provider Doctor when providers are unhealthy", async () => {
      mockGetLocalProvidersStatus.mockResolvedValue([
        {
          kind: "exo",
          binary_installed: false,
          endpoint: "http://localhost:52415",
          model: "llama-3.2-3b",
          reachable: false,
          probed: true,
          platform_supported: true,
        },
      ]);

      render(wrap(<OfficeOverviewApp />));

      expect(await screen.findByText("Settings")).toBeInTheDocument();
      expect(screen.getByText("Provider Doctor")).toBeInTheDocument();
    });

    it("does not show provider warning section when all providers are healthy", async () => {
      mockGetLocalProvidersStatus.mockResolvedValue([
        {
          kind: "ollama",
          binary_installed: true,
          endpoint: "http://localhost:11434",
          model: "llama3",
          reachable: true,
          probed: true,
          platform_supported: true,
        },
      ]);

      render(wrap(<OfficeOverviewApp />));

      // Wait for data to load; then confirm no warning banner.
      await screen.findByText("Active runs");
      expect(
        screen.queryByText("1 provider unreachable"),
      ).not.toBeInTheDocument();
    });

    it("does not show provider warning section when providers list is empty", async () => {
      mockGetLocalProvidersStatus.mockResolvedValue([]);

      render(wrap(<OfficeOverviewApp />));

      await screen.findByText("Active runs");
      expect(
        screen.queryByText(/provider.*unreachable/),
      ).not.toBeInTheDocument();
    });
  });

  describe("section links", () => {
    beforeEach(() => {
      emptyDefaults();
    });

    it("renders View board link when active tasks exist", async () => {
      mockGetOfficeTasks.mockResolvedValue({
        tasks: [
          {
            id: "t1",
            title: "Active task",
            status: "in_progress",
            updated_at: new Date().toISOString(),
          },
        ],
      });

      render(wrap(<OfficeOverviewApp />));

      // There are two "View board" links (active + blocked sections).
      const links = await screen.findAllByText("View board");
      expect(links.length).toBeGreaterThanOrEqual(1);
    });

    it("renders Answer link when pending requests exist", async () => {
      mockGetAllRequests.mockResolvedValue({
        requests: [
          {
            id: "r1",
            from: "alex",
            question: "Approve this?",
            status: "open",
          },
        ],
      });

      render(wrap(<OfficeOverviewApp />));

      expect(await screen.findByText("Answer")).toBeInTheDocument();
    });

    it("renders Review link when skill proposals exist", async () => {
      mockGetSkillsList.mockResolvedValue({
        skills: [
          {
            name: "my-skill",
            status: "proposed" as const,
          },
        ],
      });

      render(wrap(<OfficeOverviewApp />));

      expect(await screen.findByText("Review")).toBeInTheDocument();
    });
  });
});
