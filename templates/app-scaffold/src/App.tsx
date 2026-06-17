import { useEffect, useState } from "react";

import { getOfficeMembers, type OfficeMember } from "./wuphf-bridge";

/**
 * Starter App. Replace this with the actual internal tool. It demonstrates the
 * one pattern that matters: read workspace data through the WUPHF bridge
 * (callBroker / getOfficeMembers / getTasks), never fetch() the network.
 */
export function App() {
  const [members, setMembers] = useState<OfficeMember[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getOfficeMembers()
      .then((res) => setMembers(res.members ?? []))
      .catch((err: unknown) =>
        setError(err instanceof Error ? err.message : "failed to load"),
      );
  }, []);

  return (
    <main className="app">
      <header className="app__header">
        <h1 className="app__title">Your new app</h1>
        <p className="app__subtitle">
          Edit <code>src/App.tsx</code>. Read workspace data with the bridge
          (<code>callBroker</code>) — the sandbox blocks all other network.
        </p>
      </header>

      <section className="app__panel">
        <h2 className="app__panel-title">Office roster (bridge demo)</h2>
        {error ? (
          <p className="app__error">{error}</p>
        ) : members === null ? (
          <p className="app__muted">Loading…</p>
        ) : members.length === 0 ? (
          <p className="app__muted">No members.</p>
        ) : (
          <ul className="app__list">
            {members.map((m) => (
              <li key={m.slug} className="app__list-item">
                <span className="app__list-name">{m.name}</span>
                {m.role ? <span className="app__list-meta">{m.role}</span> : null}
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}
