# no-mistakes Lite (`nml`)

`nml` is a lightweight local workflow driver inspired by no-mistakes. It inspects a git repository, records original user intent, prepares committed changes for isolated validation, and is being built toward review, test, lint, PR, CI, optional automerge, and deploy steps.

## Current commands

```sh
nml                  # inspect repo and show the next action
nml init             # create ~/.config/nml/config.yaml
nml doctor           # check local tools, auth, config, and repo state
nml run --message "fix: handle empty input"
nml run --test-command "go test ./..."
nml status --format toon
nml findings --format toon
nml runs --resumable
nml resume --run <id>
nml respond --action approve
nml config --format yaml
nml tui
```

Headless commands keep stdout structured and compact. Progress goes to stderr. Exit codes are 0 for success or no-op, 1 for runtime errors, and 2 for usage errors.

## Implemented first

- Go module and `nml` binary skeleton.
- Global and per-repo config loading from `~/.config/nml`.
- Git preflight classification and no-op exits.
- First-run setup command with tool detection and TUI option pickers that support keyboard and mouse selection.
- Doctor command with TOON output.
- Dirty worktree staging, agent-generated commit metadata, and commit hook fix retries.
- Agent or fallback intent generation and JSON run state under `.git/nml/runs`, mirrored to `~/.nml/runs` with event and log files for resume.
- Treehouse worktree leasing, review branch checkout from `origin/<main>`, and branch delta copy by cherry-pick with format-patch fallback.
- First review loop using configured agents, exact `LGTM`, Markdown finding parsing, review gate output, and `--yes` or `--yolo` automatic fix attempts.
- Test command execution only when configured per repo or supplied per run, plus lint command execution with skip reasons and agent fix retries.
- Docs evaluation and optional agent documentation updates.
- GitHub remote detection, safe push of tool-owned `nml/*` branches, and GitHub PR create/update through `gh` when available.
- Bounded CI watch, failed-log collection, persisted CI logs, agent CI fix retries, optional per-run auto-merge, and optional deploy command retries.
- Resumable failed or interrupted runs via `nml resume`, review finding parser, secret redaction, PR body generation, Bubble Tea run timeline, install script, usage docs, and unit tests.

See `docs/usage.md` for workflow details. Future work is mostly provider expansion and richer TUI interactions.
