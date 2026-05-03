# CI Triage Playbook Fixture

## Failure Triage

1. Identify the failing job, command, commit, and changed files.
2. Separate infrastructure failure from product failure with evidence.
3. Reproduce locally with the closest documented command.
4. If the failure is real, fix the code or test. Do not suppress lint or type
   errors with ignore comments.
5. Record the command output summary in the task evidence.

## Common Checks

- Go: `bash scripts/test-go.sh` or a narrower documented package command.
- Web: from `web/`, run `bunx tsc --noEmit` and `bun run build`; for
  unit/component tests run `bash scripts/test-web.sh` from the repo root.
- Focused Web tests: `bash scripts/test-web.sh web/src/path/to/file.test.ts`.
- Code quality: `bash scripts/check-file-size.sh`, `golangci-lint run ./...`,
  `bunx biome check --write`.

## Escalation

Ask for human input before destructive actions such as deleting state, clearing
Docker volumes, changing production infrastructure, or applying irreversible
migrations.
