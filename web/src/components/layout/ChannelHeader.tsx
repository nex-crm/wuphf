import { useChannels } from "../../hooks/useChannels";
import { deriveBreadcrumbs } from "../../hooks/useObjectBreadcrumb";
import { appTitle } from "../../lib/constants";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { Breadcrumb } from "./Breadcrumb";

function headerTitleAndDesc(
  route: ReturnType<typeof useCurrentRoute>,
  channels: { slug: string; description?: string }[],
): { title: string; desc: string } {
  switch (route.kind) {
    case "channel": {
      const ch = channels.find((c) => c.slug === route.channelSlug);
      return {
        title: `# ${route.channelSlug}`,
        desc: ch?.description || "",
      };
    }
    case "dm":
      return { title: `@${route.agentSlug}`, desc: "" };
    case "app":
      return { title: appTitle(route.appId), desc: "" };
    case "task-board":
    case "task-detail":
      return { title: appTitle("tasks"), desc: "" };
    case "wiki":
    case "wiki-article":
    case "wiki-lookup":
      return { title: appTitle("wiki"), desc: "" };
    case "notebook-catalog":
    case "notebook-agent":
    case "notebook-entry":
      return { title: "Notebooks", desc: "" };
    case "reviews":
      return { title: "Reviews", desc: "" };
    case "inbox":
      return { title: "Decision Inbox", desc: "" };
    case "task-decision":
      return { title: `Task ${route.taskId}`, desc: "" };
    // Phase 3 — Issues surface
    case "issues-list":
      return { title: "Issues", desc: "" };
    case "issue-detail":
      return { title: `Issue ${route.issueId}`, desc: "" };
    case "issue-new":
      return { title: "New issue", desc: "" };
    case "unknown":
      return { title: "", desc: "" };
    default: {
      const _exhaustive: never = route;
      void _exhaustive;
      return { title: "", desc: "" };
    }
  }
}

export function ChannelHeader() {
  const route = useCurrentRoute();
  const { data: channels = [] } = useChannels();

  const { title, desc } = headerTitleAndDesc(route, channels);
  const breadcrumbItems = deriveBreadcrumbs(route);

  return (
    <div className="channel-header">
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 10,
          minWidth: 0,
          flex: 1,
        }}
      >
        {breadcrumbItems.length > 0 ? (
          <Breadcrumb items={breadcrumbItems} />
        ) : (
          <>
            <span className="channel-title">{title}</span>
            {desc ? <span className="channel-desc">{desc}</span> : null}
          </>
        )}
      </div>
    </div>
  );
}
