import type { NotebookCatalogSummary } from "../../api/notebook";
import AgentShelf from "./AgentShelf";

/**
 * `/notebooks` landing — vertical stack of AgentShelf rows.
 */

interface BookshelfCatalogProps {
  catalog: NotebookCatalogSummary;
  onOpenAgent: (agentSlug: string) => void;
  onOpenEntry: (agentSlug: string, entrySlug: string) => void;
}

export default function BookshelfCatalog({
  catalog,
  onOpenAgent,
  onOpenEntry,
}: BookshelfCatalogProps) {
  const metaText = `${catalog.total_agents} agents · ${catalog.total_entries} entries · ${catalog.pending_promotion} pending promotion`;

  return (
    <main className="nb-catalog" aria-label="Team notebooks">
      <header className="nb-catalog-header">
        <h1 className="nb-catalog-title">Team notebooks</h1>
        <div className="nb-catalog-meta" aria-live="polite">
          {metaText}
        </div>
      </header>
      {catalog.agents.map((agent) => (
        <AgentShelf
          key={agent.agent_slug}
          agent={agent}
          onOpenAgent={onOpenAgent}
          onOpenEntry={onOpenEntry}
        />
      ))}
    </main>
  );
}
