# nml usage

## Quick start

```sh
nml init
nml doctor
nml run --message "fix: handle empty input"
nml run --test-command "go test ./..."
```

First-run setup uses one consistent prompt-style TUI for both option pickers and text inputs. Move pickers with `j/k` or arrow keys, press `space` or `enter` to select, or click an option with the mouse. Text inputs accept with `enter` and clear with `ctrl+u`. Press `ctrl+c`, `ctrl+d`, `esc`, or `q` to cancel setup where shown.

Test commands have no auto-detected default. If no per-repo test command is configured and no `--test-command` is supplied for the run, the test step is skipped with a reason.

Configure a repo-specific test command:

```sh
nml config --set-test-command "go test ./..."
```

`nml` writes structured output to stdout and progress to stderr. Headless commands use compact TOON tables and return exit code 0 for success or no-op, 1 for runtime errors, and 2 for usage errors.

## Pipeline

The implemented local pipeline is:

```text
preflight -> intent -> commit -> worktree -> review -> test -> docs -> lint -> push -> pr -> ci -> deploy -> final
```

- Dirty work is staged and committed first.
- Intent is stored before agent fixes mutate the change.
- A treehouse lease provides the isolated review worktree.
- Review uses exact `LGTM` or Markdown findings.
- Tests, docs, lint, CI, and deploy can ask the configured agent for fixes.
- Auto-merge only runs when `--auto-merge` is passed for that run.

## Resuming runs

Each saved run is written to the repository under `.git/nml/runs` and mirrored under `~/.nml/runs/<repo-id>/<run-id>`. The global mirror includes `state.json`, `events.jsonl`, and logs such as CI check output and failed GitHub Actions logs.

List and continue saved work:

```sh
nml runs --resumable
nml resume
nml resume --run <id>
```

`nml resume` continues the latest failed or interrupted run. It reuses the leased worktree, reuses or updates the existing PR, pushes local CI-fix commits that are ahead of the remote review branch, and then resumes the remaining validation steps. If the run is stopped at a review gate, resume prints the gate instead of making an approval decision.

## Review gates

When review finds issues in interactive mode, answer the saved gate:

```sh
nml findings --run <id> --format toon
nml respond --action fix --findings r1,r2 --run <id>
nml respond --action approve --run <id>
nml respond --action skip --run <id>
```

## TUI

```sh
nml tui
nml tui --run <id>
```

The default interactive `nml` home view, setup wizard, commit-message input, review gate, and run timeline all use the same `◆`, `◇`, `│`, `◻`, and `└` prompt style. From the home view, choose an action with `space`, `enter`, or a mouse click. Dirty worktree runs ask for the commit message inside the same TUI flow. Long-running operations show a CLI spinner with the current step, such as `review round 1`, `running test`, `pushing review branch`, or `watching CI`. Running and fixing steps animate with spinner frames. Press `q`, `esc`, `ctrl+c`, or `ctrl+d` to quit where shown.
