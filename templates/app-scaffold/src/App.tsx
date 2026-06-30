import { Center, Loader, Stack, Text, Title } from "@mantine/core";

/**
 * Starter App — a NEUTRAL placeholder the live preview shows the instant a build
 * begins, so the human watches a clean "being built" state (never dead air, and
 * never an unrelated demo like a task list that isn't what they asked for).
 *
 * The App Builder REPLACES this file with the real tool. Build it WITHIN Mantine
 * (override the theme in src/main.tsx, see DESIGN.md) and read workspace data
 * ONLY through src/wuphf-bridge.ts — its helpers (getTasks / getOfficeMembers /
 * callIntegration / ai / createTask) are the only way out of the sandbox. Use
 * refine hooks (useTable / useList) for tabular data; drop the unused Refine in
 * main.tsx if the tool only needs ai() / createTask.
 */
export function App() {
  return (
    <Center mih="100vh" p="xl">
      <Stack align="center" gap="sm" maw={380}>
        <Loader size="sm" />
        <Title order={3}>Building your app…</Title>
        <Text c="dimmed" size="sm" ta="center">
          Your AI is writing this tool now. It will take shape right here, live,
          as each part comes together.
        </Text>
      </Stack>
    </Center>
  );
}
