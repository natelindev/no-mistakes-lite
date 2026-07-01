# Repository Agent Instructions

- After making changes in this repository, always auto re-install the project before reporting completion.
- Before editing, inspect `git status --short --branch` and preserve unrelated user work.
- Prefer small, focused changes that keep behavior simple, robust, and maintainable.
- For Go changes, run `gofmt` on touched Go files before testing.
- Run `go test ./...` after code changes. If verification is not possible, explain why.
- Update README, docs, help text, and tests when behavior or configuration changes.
- Keep command output and user-facing errors concise, actionable, and suitable for agent automation.
- Do not add agent co-author lines to commits.
