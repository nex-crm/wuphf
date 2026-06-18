/**
 * A B2-generated entity article, byte-shaped like the golden output of
 * `internal/team/entity_article_test.go` (TestBuildEntityArticle_Skeleton):
 * header marker comment, H1, bold lead, `## Summary` definition-list
 * infobox, themed sections with `[^n]` footnote citations, `[[kind/slug]]`
 * wikilinks, `## Associated`, and a `## References` footnote block.
 *
 * Used by the renderer-transform tests and the Storybook stories so the
 * Wikipedia-parity read view is exercised against the REAL generated
 * article shape, not a hand-idealized one.
 */
export const ENTITY_ARTICLE_FIXTURE = `<!-- wuphf:entity-article — generated from the team knowledge graph (fact log + entity graph); regenerated deterministically when completed tasks record new facts. The generated body is fully managed: human edits are detected via last_human_edit_ts and preserved by moving the generated article into a managed block. -->

# Acme Corp

**Acme Corp** is a company.

## Summary

Kind
: company

Tasks
: TASK-3

Artifacts
: [team/playbooks/acme-renewal.md](team/playbooks/acme-renewal.md)

Associated
: [[people/eng]]

## Work history

- Completed task TASK-3 ("Close the renewal") involved this entity. Goal: Renew Acme. Produced artifact: team/playbooks/acme-renewal.md. Co-occurring entities: [[people/eng]].[^1]

## Observations

- Prefers quarterly billing.[^2]

## Associated

- [[people/eng]] — 2 shared facts

## References

[^1]: Task TASK-3 — artifact: [team/playbooks/acme-renewal.md](team/playbooks/acme-renewal.md); recorded by eng on 2026-06-10.
[^2]: artifact: [agents/eng/notes.md](agents/eng/notes.md); recorded by ceo on 2026-06-10.
`;

/** The article title the broker derives for the fixture. */
export const ENTITY_ARTICLE_FIXTURE_TITLE = "Acme Corp";

/** The canonical wiki path of the fixture article. */
export const ENTITY_ARTICLE_FIXTURE_PATH = "team/companies/acme-corp.md";
