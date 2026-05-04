# Release Checklist Fixture

## Before Draft PR

- Branch is based on latest `origin/main`.
- Changed files summary is attached to the task.
- Risk list names data loss, auth, workspace, and migration risks when relevant.
- Docs touched by the feature are updated.

## Before Ready For Review

- Relevant Go tests pass.
- Relevant Web tests pass.
- Typecheck/build passes for changed Web contracts.
- File-size and code-quality checks are clean or the PR explains existing debt.
- Surface parity matrix reflects the shipped capability state.

## Before Merge

- Draft PR has CI results linked or summarized.
- Reviewer comments are resolved or explicitly deferred.
- Human gate is recorded for destructive, production, or release actions.
- Next action is clear: merge, follow-up PR, release, or hold.
