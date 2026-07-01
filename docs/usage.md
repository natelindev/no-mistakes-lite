# nml usage

## Quick start

```sh
nml
nml init --yes --agent <name>
nml doctor
nml run --message "fix: handle empty input"
nml run --test-command "go test ./..."
nml run --skip-review --test-command "go test ./..."
```

First-run setup is non-interactive by default for agents. Use `nml init --yes --agent <name>` in automation. Humans can run `nml init --interactive` for the guided prompt-style wizard with keyboard and mouse selection.

Test commands have no auto-detected default. If no per-repo test command is configured and no `--test-command` is supplied for the run, the test step is skipped with a reason.

Configure a repo-specific test command:

```sh
nml config --set-test-command "go test ./..."
```

`nml` writes structured output to stdout and progress to stderr. Running `nml` with no arguments prints a compact TOON dashboard and never prompts. Headless commands use compact TOON tables and return exit code 0 for success or no-op, 1 for runtime errors, and 2 for usage errors.

## Pipeline

The implemented local pipeline is:

```text
preflight -> intent -> commit -> worktree -> review -> test -> docs -> lint -> push -> pr -> ci -> deploy -> final
```

- Dirty work is staged and committed first.
- Intent is stored before agent fixes mutate the change.
- A treehouse lease provides the isolated review worktree unless the command is already running inside a Treehouse-managed worktree, in which case nml reuses the current worktree.
- Review uses exact `LGTM` or Markdown findings. PR bodies include the actual review findings for every non-LGTM round.
- Pass `--skip-review` to mark the review step skipped without invoking the configured agent or built-in review checks.
- Tests, docs, lint, CI, and deploy can ask the configured agent for fixes. PR bodies summarize only each command and final status, not full command output.
- Completed runs print a compact summary with PR URL, cleanup status, and step statuses. Use `nml status --run <id>` or `nml status --run <id> --full` for step details and logs.
- Auto-merge means nml runs `gh pr merge` after checks pass. It does not use GitHub's repository-level auto-merge feature.
- Cleanup auto-return is enabled by default. After a completed run, nml returns the Treehouse worktree with `treehouse return --force`. Set `cleanup.auto=false` to keep completed worktrees for inspection.

## Resuming runs

Each saved run is written to the repository under `.git/nml/runs` and mirrored under `~/.nml/runs/<repo-id>/<run-id>`. The global mirror includes `state.json`, `events.jsonl`, and logs such as CI check output and failed GitHub Actions logs.

List and continue saved work:

```sh
nml runs --resumable
nml resume
nml resume --run <id>
```

`nml resume` continues the latest failed or interrupted run. It reuses the leased worktree, reuses or updates the existing PR, pushes local CI-fix commits that are ahead of the remote review branch, and then resumes the remaining validation steps. If the run is stopped at a review gate, resume prints the gate instead of making an approval decision. Use `nml resume --skip-review --run <id>` to bypass a pending or failed review phase and continue with validation.

## Review gates

When review finds issues, answer the saved gate:

```sh
nml tui --run <id>
nml findings --run <id> --format toon
nml findings --run <id> --full
nml respond --action fix --run <id>
nml respond --action fix --findings r1,r2 --run <id>
nml respond --action approve --run <id>
nml respond --action skip --run <id>
```

The TUI review gate lets humans approve, skip, fix all findings, or choose exact findings with space and enter. In headless mode, finding IDs are `r1`, `r2`, and so on; `nml respond --action fix --run <id>` fixes all latest findings when `--findings` is omitted.

## Persistent settings

Persist run defaults globally or for the current project. Project settings override global settings. Project identity is based on Git's common directory, so worktrees for the same repository share settings.

```sh
nml config --interactive
nml config --scope project --set review.yolo=true --set ci.timeout=15m
nml config --scope project --set auto_merge.enabled=true --set auto_merge.method=squash
nml config --scope project --set cleanup.auto=false
nml config --scope global --set review.yolo=false --set ci.timeout=30m
nml config --format toon
```

`nml config --interactive` starts with a scope picker, then prompts for yolo review fixing, auto-merge, merge method, auto-cleanup, CI timeout, test command, and lint command. Supported non-interactive keys are `review.yolo`, `auto_merge.enabled`, `auto_merge.method`, `cleanup.auto`, `ci.timeout`, `commands.test`, and `commands.lint`. CLI flags still override persisted defaults for that invocation.

## Agent integrations

```sh
nml hooks install --apps claude,codex,opencode
nml hooks install --scope project --apps claude,codex
```

The hook runs `nml` at session start so supported agents see live workspace state. The install script installs only the `nml` binary and does not copy the Agent Skill automatically. If you want the skill, install or copy `skills/no-mistakes-lite/SKILL.md` explicitly.

## TUI

```sh
nml tui
nml tui --run <id>
```

The explicit TUI command shows the saved run timeline with the same `◆`, `◇`, `│`, `◻`, and `└` prompt style. If the saved run is waiting at a review gate, it opens an interactive response picker instead. Long-running operations show progress on stderr with the current step, such as `review round 1`, `running test`, `pushing review branch`, or `watching CI`. Running and fixing steps use a compact Braille spinner. Press `q`, `esc`, `ctrl+c`, or `ctrl+d` to quit where shown.
