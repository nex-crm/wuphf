# Rich HTML Artifact Tutorial Scenarios

These fixtures model the first WUPHF workflows where agent output should be HTML first:

1. Notebook exploration: an agent creates a dense, interactive HTML artifact next to its working notes.
2. Wiki promotion: the artifact is promoted into a durable wiki article with provenance.
3. Chat handoff: the agent references the artifact with a terse `visual-artifact:<id>` marker so the UI renders a preview card instead of a plain link.

The goal is to keep the viral article's lesson concrete: when the output is a plan, explainer, review packet, incident report, or tuning surface, markdown should carry the summary and provenance while HTML carries the visual, interactive representation.

## Scenarios

- `revops-launch-risk`: a RevOps operator comparing launch blockers, tradeoffs, and owner load before committing a GTM plan.
- `support-incident-room`: a customer/support ops lead turning an incident timeline into a readable command-room artifact.
- `sales-review-brief`: a sales engineer or reviewer explaining a technical change with annotated diff, risks, and reviewer prompts.

Each scenario has:

- `source.md`: the notebook note the agent would write while exploring.
- `artifact.html`: the HTML artifact the agent should create from the note.
- `wiki.md`: the markdown summary used when promoting the artifact to the team wiki.
- `chat.txt`: the agent chat message that should become a rich artifact card in chat.

The `scenarios.json` manifest separates intended deployment paths from local fixture files. `sourceMarkdownPath` and `targetWikiPath` describe where the agent-created source note and promoted wiki article should live in a real workspace; `sourcePath`, `wikiPath`, `htmlPath`, and `chatPath` point at fixture files under this tutorial directory for tests and examples.

`ACCEPTANCE.md` turns the three scenarios into browser-checkable acceptance
criteria for the notebook, wiki, and chat surfaces.

`STYLE.md` defines the WUPHF paper-manual artifact style agents should use by
default: warm paper, editorial text, the exact Making Software cobalt family,
faint figure construction grids, mono metadata labels, muted complementary
state colors, and sparse interactive controls.

## Validation

These examples are executable fixtures, not just documentation.

```bash
bash scripts/test-go.sh ./internal/team
bash scripts/test-web.sh web/src/lib/richArtifactTutorialFixtures.test.ts web/src/lib/richArtifactReferences.test.ts
```

The Go test creates and promotes every HTML artifact through the wiki repo API. The web test parses the same chat fixtures through the chat artifact reference parser.
