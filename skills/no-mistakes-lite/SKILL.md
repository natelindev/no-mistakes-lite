---
name: no-mistakes-lite
description: Validate, review, resume, and ship local git changes with the nml no-mistakes-lite pipeline.
user-invocable: true
---

# no-mistakes-lite

Use `nml` to inspect a git workspace, validate changes in an isolated worktree, handle review gates, run tests and lint, create or update PRs, watch CI, and resume interrupted runs. `nml` writes compact TOON to stdout and progress to stderr.

## Before you start

- Confirm the tool exists with `command -v nml`. If missing and `scripts/install.sh` exists in this repo, run `./scripts/install.sh`.
- Run `nml` first for the content-first workspace dashboard. It never prompts.
- Run `nml doctor` when setup, auth, local tools, or repository health is unclear.
- Inspect `git status --short --branch` before editing files. Preserve unrelated user work.
- Use non-interactive setup: `nml init --yes --agent <name>`. Use `nml init --interactive` only when the user explicitly wants a guided terminal wizard.
- If there is no configured test command, pass `--test-command` when the correct command is obvious. Otherwise let `nml` skip tests and report the skip.
- Use long timeouts. Review, test, PR, CI, and deploy steps can take several minutes.

## Core commands

```sh
nml
nml doctor
nml init --yes --agent <name>
nml run --message "<commit message>"
nml run --test-command "go test ./..."
nml status --format toon
nml status --run <id> --full
nml findings --format toon
nml findings --run <id> --full
nml runs --resumable
nml resume --run <id>
nml config --interactive
nml config --scope project --set review.yolo=true --set ci.timeout=15m
nml config --scope global --set auto_merge.enabled=true
nml tui
```

Common run flags:

- `--paths <a,b>` stages selected paths only.
- `--yes` accepts safe defaults without prompts.
- `--yolo` auto-selects all actionable findings. Use only with explicit user consent.
- `--auto-merge` makes nml run `gh pr merge` after checks pass. It does not use GitHub's repository-level auto-merge feature. Use only with explicit user consent.
- `--skip-docs`, `--skip-deploy`, `--ci-timeout <duration>`, `--merge-method <squash|merge|rebase>`, and `--fetch <bool>` tune the run.
- Persist defaults with `nml config --interactive`, `nml config --scope project --set review.yolo=true --set ci.timeout=15m`, or `nml config --scope global --set auto_merge.enabled=true`. Project settings override global settings.

## Review gates

When `nml` parks at a review gate, inspect findings and respond:

```sh
nml tui --run <id>
nml findings --format toon
nml respond --action fix --run <id>
nml respond --action fix --findings r1,r2 --run <id>
nml respond --action approve --run <id>
nml respond --action skip --run <id>
```

Guidelines:

- Use `nml tui --run <id>` when a human wants to choose approve, skip, fix all, or specific findings interactively.
- Use `respond --action fix` for clear, mechanical findings. Omitting `--findings` fixes all latest findings.
- Ask the user before approving, skipping, or fixing anything that changes product behavior or conflicts with stated intent.
- While a saved gate is active, prefer `nml respond` over manual edits. If the run ends as failed or cancelled, fix the problem, then start a fresh run or resume if `nml runs --resumable` shows it.
- Use `--full` when output says long fields were truncated.

## Agent integrations

Prefer the session hook when the user wants live nml context injected at agent startup:

```sh
nml hooks install --apps claude,codex,opencode
```

Use `--scope project` to install repo-local integrations. The hook runs `nml` at session start, so agents see current repo state before taking action. This skill is the lower-overhead on-demand path and complements the hook.

## Report back

End with a concise summary: what ran, what passed, what was skipped, any fixes `nml` made, and the PR URL if one was created.
