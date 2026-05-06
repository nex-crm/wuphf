import { createDM, get, post, setMemory } from "../api/client";
import { confirm } from "../components/ui/ConfirmDialog";
import { openProviderSwitcher } from "../components/ui/ProviderSwitcher";
import { showNotice } from "../components/ui/Toast";
import { useAppStore } from "../stores/app";
import { router } from "./router";

function navigateToApp(appId: string): void {
  void router.navigate({ to: "/apps/$appId", params: { appId } });
}

/** Routing prefix for `/ask`: mirrors TUI cmdAsk which always goes to the lead. */
export function askPrefix(leadSlug: string | undefined): string {
  const slug = (leadSlug || "ceo").trim().toLowerCase() || "ceo";
  return `@${slug} `;
}

export function unknownSlashCommandMessage(command: string): string {
  const name = command.trim().split(/\s+/)[0] || "/";
  return `Unknown command: ${name}. Try /help.`;
}

/** Pick the team-lead slug: configured first, else first built-in agent, else 'ceo'. */
export function resolveLeadSlug(
  configured: string | undefined,
  members: { slug?: string; built_in?: boolean }[],
): string {
  const explicit = (configured ?? "").trim().toLowerCase();
  if (explicit) return explicit;
  const builtin = members.find(
    (m) => m.built_in && m.slug && m.slug !== "human" && m.slug !== "you",
  );
  if (builtin?.slug) return builtin.slug;
  return "ceo";
}

export interface SlashHandlers {
  /** Team lead slug used for `/ask` routing. */
  leadSlug: string | undefined;
  /** Send the given text as a normal message (bypasses slash parsing). */
  sendAsMessage: (text: string) => void;
  /** Clear the visible transcript for the active channel. */
  clearMessages: () => void;
  /** Active channel slug for slash commands that scope server requests. */
  channel: string;
}

/**
 * Handle slash commands. Returns true if the input was consumed as a command.
 *
 * Some commands (e.g. `/ask`) rewrite the input and invoke sendAsMessage so
 * the broker sees a normal user message with the right @mention routing.
 */
// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
export function handleSlashCommand(
  input: string,
  handlers: SlashHandlers,
): boolean {
  const parts = input.split(/\s+/);
  const cmd = parts[0].toLowerCase();
  const args = parts.slice(1).join(" ").trim();
  const store = useAppStore.getState();

  switch (cmd) {
    case "/clear":
      handlers.clearMessages();
      return true;
    case "/help":
      store.setComposerHelpOpen(true);
      return true;
    case "/requests":
      navigateToApp("requests");
      return true;
    case "/policies":
      navigateToApp("policies");
      return true;
    case "/skills":
      navigateToApp("skills");
      return true;
    case "/calendar":
      navigateToApp("calendar");
      return true;
    case "/tasks":
      navigateToApp("tasks");
      return true;
    case "/recover":
    case "/doctor":
      navigateToApp("health-check");
      return true;
    case "/provider":
      openProviderSwitcher();
      return true;
    case "/search":
      store.setComposerSearchInitialQuery(args);
      store.setSearchOpen(true);
      return true;
    case "/ask": {
      if (!args) {
        showNotice("Usage: /ask <question>", "info");
        return true;
      }
      // TUI's cmdAsk always routes to the team lead. Mirror that by
      // prefixing an @mention so the broker's routing picks up the lead.
      handlers.sendAsMessage(askPrefix(handlers.leadSlug) + args);
      return true;
    }
    case "/lookup": {
      if (!args) {
        showNotice("Usage: /lookup <question>", "info");
        return true;
      }
      showNotice("Looking up in wiki…", "info");
      get("/wiki/lookup", { q: args, channel: handlers.channel }).catch(
        (e: Error) => {
          showNotice(`Wiki lookup failed: ${e.message}`, "error");
        },
      );
      return true;
    }
    case "/lint": {
      void router.navigate({ to: "/wiki/$", params: { _splat: "_lint" } });
      return true;
    }
    case "/remember": {
      if (!args) {
        showNotice("Usage: /remember <fact>", "info");
        return true;
      }
      // Broker /memory requires namespace + key + value. Use a stable
      // human-owned namespace and a short timestamp key so repeated
      // /remember calls do not collide.
      const key = `note-${Date.now().toString(36)}`;
      setMemory("human-notes", key, args)
        .then(() =>
          showNotice(
            "Stored in memory: " +
              (args.length > 40 ? `${args.slice(0, 40)}…` : args),
            "success",
          ),
        )
        .catch((e: Error) =>
          showNotice(`Remember failed: ${e.message}`, "error"),
        );
      return true;
    }
    case "/focus":
      post("/focus-mode", { focus_mode: true })
        .then(() => showNotice("Switched to delegation mode", "success"))
        .catch(() => showNotice("Failed to switch mode", "error"));
      return true;
    case "/collab":
      post("/focus-mode", { focus_mode: false })
        .then(() => showNotice("Switched to collaborative mode", "success"))
        .catch(() => showNotice("Failed to switch mode", "error"));
      return true;
    case "/pause":
      post("/signals", { kind: "pause", summary: "Human paused all agents" })
        .then(() => showNotice("All agents paused", "success"))
        .catch((e: Error) => showNotice(`Pause failed: ${e.message}`, "error"));
      return true;
    case "/resume":
      post("/signals", { kind: "resume", summary: "Human resumed agents" })
        .then(() => showNotice("Agents resumed", "success"))
        .catch((e: Error) =>
          showNotice(`Resume failed: ${e.message}`, "error"),
        );
      return true;
    case "/reset":
      confirm({
        title: "Reset the office?",
        message:
          "Clears channels back to #general and drops in-memory state. Persisted tasks and requests stay on the broker.",
        confirmLabel: "Reset",
        danger: true,
        onConfirm: () =>
          post("/reset", {})
            .then(() => {
              void router.navigate({
                to: "/channels/$channelSlug",
                params: { channelSlug: "general" },
              });
              showNotice("Office reset", "success");
            })
            .catch((e: Error) =>
              showNotice(`Reset failed: ${e.message}`, "error"),
            ),
      });
      return true;
    case "/1o1": {
      if (!args) {
        showNotice("Usage: /1o1 <agent-slug>", "info");
        return true;
      }
      const slug = args.trim().toLowerCase();
      createDM(slug)
        .then(() => {
          void router.navigate({
            to: "/dm/$agentSlug",
            params: { agentSlug: slug },
          });
        })
        .catch(() => showNotice(`Agent not found: ${args.trim()}`, "error"));
      return true;
    }
    case "/task": {
      const taskParts = args.split(/\s+/);
      const action = (taskParts[0] || "").toLowerCase();
      const taskId = taskParts[1] || "";
      const extra = taskParts.slice(2).join(" ");
      if (!(action && taskId)) {
        showNotice(
          "Usage: /task <claim|release|complete|block|approve> <task-id>",
          "info",
        );
        return true;
      }
      const body: Record<string, string> = {
        action,
        id: taskId,
        channel: handlers.channel,
      };
      if (action === "claim") body.owner = "human";
      if (extra) body.details = extra;
      post("/tasks", body)
        .then(() => showNotice(`Task ${taskId} → ${action}`, "success"))
        .catch((e: Error) =>
          showNotice(`Task action failed: ${e.message}`, "error"),
        );
      return true;
    }
    case "/cancel": {
      if (!args) {
        showNotice("Usage: /cancel <task-id>", "info");
        return true;
      }
      post("/tasks", {
        action: "release",
        id: args.trim(),
        channel: handlers.channel,
      })
        .then(() => showNotice(`Task ${args.trim()} cancelled`, "success"))
        .catch(() => showNotice("Cancel failed", "error"));
      return true;
    }
    case "/connect": {
      // Bare `/connect` opens the provider picker (parity with the TUI's
      // `/connect` 4-option picker — see cmd/wuphf/channel.go:4871). Direct
      // forms like `/connect telegram` skip the picker and land straight in
      // the integration's wizard, also matching TUI behaviour.
      const target = args.trim().toLowerCase();
      if (!target) {
        store.openConnectWizard("provider");
        return true;
      }
      if (target === "telegram") {
        store.openConnectWizard("telegram");
        return true;
      }
      showNotice(
        `Integration "${target}" doesn't have a web wizard yet — try /connect on its own to see what's available.`,
        "info",
      );
      return true;
    }
    default:
      showNotice(unknownSlashCommandMessage(cmd), "info");
      return true;
  }
}
