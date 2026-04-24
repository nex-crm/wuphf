import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { getChannels } from "../api/client";
import { useAppStore } from "../stores/app";

/**
 * `?` opens the global help/shortcut reference, but only when the user
 * is not currently typing. Returning true here means we intercept the
 * keystroke; false means let it through to the focused field.
 */
function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  if (target.isContentEditable) return true;
  return false;
}

/** Global keyboard shortcuts matching legacy behavior. */
export function useKeyboardShortcuts() {
  const setSearchOpen = useAppStore((s) => s.setSearchOpen);
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug);
  const setActiveThreadId = useAppStore((s) => s.setActiveThreadId);
  const setCurrentApp = useAppStore((s) => s.setCurrentApp);
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel);
  const setLastMessageId = useAppStore((s) => s.setLastMessageId);
  const setComposerHelpOpen = useAppStore((s) => s.setComposerHelpOpen);
  const queryClient = useQueryClient();

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      // Cmd+K or Ctrl+K → command palette
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        const state = useAppStore.getState();
        setSearchOpen(!state.searchOpen);
        return;
      }

      // Cmd+/ or Ctrl+/ → focus composer
      if ((e.metaKey || e.ctrlKey) && e.key === "/") {
        e.preventDefault();
        const ta =
          document.querySelector<HTMLTextAreaElement>(".composer-input");
        ta?.focus();
        return;
      }

      // Cmd+1..9 → quick-jump to nth channel
      if ((e.metaKey || e.ctrlKey) && e.key >= "1" && e.key <= "9") {
        const target = e.target as HTMLElement | null;
        // Don't intercept inside text inputs unless modifier is also present
        if (target?.tagName === "INPUT" || target?.tagName === "TEXTAREA") {
          // Only the modifier+digit combo lands here, so still safe.
        }
        const cached = queryClient.getQueryData<{
          channels: { slug: string }[];
        }>(["channels"]);
        const channels = cached?.channels;
        if (!channels) {
          // Fetch once if cache cold
          getChannels()
            .then((data) => {
              queryClient.setQueryData(["channels"], data);
            })
            .catch(() => {});
          return;
        }
        const idx = parseInt(e.key, 10) - 1;
        const ch = channels[idx];
        if (!ch) return;
        e.preventDefault();
        setCurrentApp(null);
        setCurrentChannel(ch.slug);
        setLastMessageId(null);
        return;
      }

      // `?` → open keyboard + command reference. Only when not typing,
      // since `?` is a plain character inside inputs. Shift+/ also
      // produces `?` on US layouts, so we match on e.key rather than
      // juggling modifier state. Skip during onboarding since the
      // HelpModalHost lives in Shell — toggling composerHelpOpen there
      // would set hidden state and then surprise the user after the
      // wizard completes.
      if (e.key === "?" && !e.metaKey && !e.ctrlKey && !e.altKey) {
        if (isTypingTarget(e.target)) return;
        const state = useAppStore.getState();
        if (!state.onboardingComplete) return;
        e.preventDefault();
        setComposerHelpOpen(!state.composerHelpOpen);
        return;
      }

      // Escape → close panels in priority order
      if (e.key === "Escape") {
        const state = useAppStore.getState();
        if (state.composerHelpOpen) {
          setComposerHelpOpen(false);
          return;
        }
        if (state.searchOpen) {
          setSearchOpen(false);
          return;
        }
        if (state.activeAgentSlug) {
          setActiveAgentSlug(null);
          return;
        }
        if (state.activeThreadId) {
          setActiveThreadId(null);
          return;
        }
      }
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [
    setSearchOpen,
    setActiveAgentSlug,
    setActiveThreadId,
    setCurrentApp,
    setCurrentChannel,
    setLastMessageId,
    setComposerHelpOpen,
    queryClient,
  ]);
}
