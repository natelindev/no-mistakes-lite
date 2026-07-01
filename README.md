# no-mistakes Lite (`nml`)

`nml` is a lightweight local workflow driver inspired by [no-mistakes](https://github.com/kunchenguid/no-mistakes). It inspects a git repository, records original user intent, prepares committed changes for isolated validation, and is being built toward review, test, lint, PR, CI, optional automerge, and deploy steps.

## Inspiration and key differences

[`no-mistakes`](https://github.com/kunchenguid/no-mistakes) gates changes by putting a local git proxy in front of your real remote: push to the gate, let the AI validation pipeline run in a disposable worktree, then forward only clean work to the configured push target and PR flow.

`nml` explores a smaller command-first take on the same idea:

- **No git proxy remote required** - run `nml run`, `nml status`, `nml respond`, and `nml resume` directly from the repository.
- **Agent-friendly by default** - headless commands emit compact TOON on stdout, progress on stderr, and avoid prompts unless an explicit interactive command is used.
- **Lightweight local state** - run state lives under `.git/nml/runs` and is mirrored to `~/.nml/runs` for resume and logs.
- **Explicit worktree isolation** - Treehouse leases provide isolated validation worktrees, with current worktree reuse when already inside a Treehouse-managed checkout.
- **Incremental scope** - `nml` focuses on a small binary, simple config, direct GitHub PR/CI automation, and hooks or skills that can be installed separately.

Use `no-mistakes` when you want the full git-push gate workflow. Use `nml` when you want a minimal CLI that agents and scripts can call directly.

## Current commands

```sh
nml                                  # print compact workspace status in TOON
nml init --yes --agent <name>         # create ~/.config/nml/config.yaml
nml init --interactive                # guided setup wizard for humans
nml doctor                            # check local tools, auth, config, and repo state
nml run --message "fix: handle empty input"
nml run --test-command "go test ./..."
nml run --skip-review --test-command "go test ./..."
nml status --format toon
nml status --run <id> --full
nml findings --format toon
nml findings --run <id> --full
nml runs --resumable
nml resume --run <id>
nml respond --action fix --run <id>      # fix all latest review findings
nml respond --action approve
nml config --format toon
nml config --interactive
nml config --scope project --set review.yolo=true --set ci.timeout=15m
nml config --scope global --set auto_merge.enabled=true
nml config --scope project --set cleanup.auto=false
nml hooks install --apps claude,codex,opencode
nml tui                                 # timeline, or interactive review gate
```

Headless commands keep stdout structured and compact. Running `nml` with no arguments is content-first and never prompts. Progress goes to stderr. Exit codes are 0 for success or no-op, 1 for runtime errors, and 2 for usage errors.

## Agent integrations

Install live session context hooks when you want agents to see `nml` workspace status at startup:

```sh
nml hooks install --apps claude,codex,opencode
nml hooks install --scope project --apps claude,codex
```

The repository also carries an Agent Skill at `skills/no-mistakes-lite/SKILL.md`. `scripts/install.sh` installs only the `nml` binary and does not install the skill automatically. If you want the skill, install or copy it explicitly. The hook and skill are complementary: use the hook for ambient live state, and the skill for on-demand workflow guidance.

## Implemented first

- Go module and `nml` binary skeleton.
- Global and per-repo config loading from `~/.config/nml`, including persistent run defaults such as yolo review fixing, auto-merge, auto-cleanup, CI timeout, and validation commands.
- Git preflight classification and no-op exits.
- Non-interactive first-run setup with `--yes`, plus an explicit `--interactive` wizard for humans.
- Doctor command with TOON output.
- Dirty worktree staging, agent-generated commit metadata, and commit hook fix retries.
- Agent or fallback intent generation and JSON run state under `.git/nml/runs`, mirrored to `~/.nml/runs` with event and log files for resume.
- Treehouse worktree leasing with completed-run auto-cleanup that preserves a worktree when the current terminal started inside it, current Treehouse worktree reuse, exact source commit reuse, and cherry-pick or format-patch fallback.
- First review loop using configured agents, exact `LGTM`, Markdown finding parsing, review gate output, `--skip-review`, and `--yes` or `--yolo` automatic fix attempts.
- Test command execution only when configured per repo or supplied per run, plus lint command execution with skip reasons and agent fix retries.
- Docs evaluation and optional agent documentation updates.
- GitHub remote detection, safe push of tool-owned `nml/*` branches, and GitHub PR create/update through `gh` when available.
- Bounded CI watch with delayed check registration handling, failed-log collection, persisted CI logs, stricter no-checks handling for yolo or skipped-review runs, agent CI fix retries, optional per-run auto-merge with GitHub CLI prompts disabled, and optional deploy command retries.
- Resumable failed or interrupted runs via `nml resume`, review finding parser, secret redaction, PR body generation, Bubble Tea run timeline and interactive review gate response picker, AXI session hook installation for Claude Code, Codex, and OpenCode, installable Agent Skill, binary-only install script, usage docs, and unit tests.

See `docs/usage.md` for workflow details. Future work is mostly provider expansion.
