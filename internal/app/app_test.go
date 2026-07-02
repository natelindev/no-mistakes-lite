package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/natelindev/no-mistakes-lite/internal/config"
	gitx "github.com/natelindev/no-mistakes-lite/internal/git"
	"github.com/natelindev/no-mistakes-lite/internal/review"
	"github.com/natelindev/no-mistakes-lite/internal/runstate"
)

func TestHomeNoRepoIsNoop(t *testing.T) {
	var out, errw bytes.Buffer
	app := App{Out: &out, Err: &errw, Cwd: t.TempDir(), Interactive: false}
	code := app.Run(context.Background(), nil)
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d", code)
	}
	text := out.String()
	for _, want := range []string{"bin:", "description:", "status: noop", "state: no_repo", "reason:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("home output missing %q in:\n%s", want, text)
		}
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	var out, errw bytes.Buffer
	app := App{Out: &out, Err: &errw, Cwd: t.TempDir(), Interactive: false}
	code := app.Run(context.Background(), []string{"wat"})
	if code != ExitUsage {
		t.Fatalf("expected usage exit, got %d", code)
	}
	if !strings.Contains(out.String(), "unknown command: wat") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestInteractiveHomeStaysHeadless(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".config", "nml")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("main_branch: main\nremote: origin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := newAppTestRepo(t, filepath.Join(root, "repo"))
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	app := App{In: strings.NewReader("q"), Out: &out, Err: &errw, Cwd: repo, Interactive: true}
	code := app.Run(context.Background(), nil)
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d", code)
	}
	text := out.String()
	for _, want := range []string{"bin:", "description:", "state: dirty", "status: actionable", "changed_files[1]:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("home output missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Choose next action") {
		t.Fatalf("home must not prompt in AXI mode:\n%s", text)
	}
}

func TestInitRequiresExplicitMode(t *testing.T) {
	var out, errw bytes.Buffer
	app := App{Out: &out, Err: &errw, Cwd: t.TempDir(), Interactive: true}
	code := app.Run(context.Background(), []string{"init"})
	if code != ExitUsage {
		t.Fatalf("expected usage exit, got %d", code)
	}
	if !strings.Contains(out.String(), "init requires --yes or --interactive") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestHooksInstallWritesUserIntegrations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out, errw bytes.Buffer
	app := App{Out: &out, Err: &errw, Cwd: t.TempDir(), Interactive: false}
	code := app.Run(context.Background(), []string{"hooks", "install", "--apps", "claude,codex,opencode"})
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errw.String())
	}
	for _, path := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".codex", "hooks.json"),
		filepath.Join(home, ".codex", "config.toml"),
		filepath.Join(home, ".config", "opencode", "plugins", "nml-context.js"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected integration file %s: %v", path, err)
		}
	}
	codexConfig, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codexConfig), "hooks = true") {
		t.Fatalf("codex config did not enable hooks:\n%s", codexConfig)
	}
	plugin, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "plugins", "nml-context.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plugin), "experimental.chat.system.transform") {
		t.Fatalf("opencode plugin missing system transform:\n%s", plugin)
	}
}

func TestConfigInteractiveRequiresTerminal(t *testing.T) {
	var out, errw bytes.Buffer
	app := App{Out: &out, Err: &errw, Cwd: t.TempDir(), Interactive: false}
	code := app.Run(context.Background(), []string{"config", "--interactive"})
	if code != ExitUsage {
		t.Fatalf("expected usage exit, got %d", code)
	}
	if !strings.Contains(out.String(), "--interactive requires a terminal") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestConfigSetPersistsProjectSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := newAppTestRepo(t, filepath.Join(t.TempDir(), "repo"))
	var out, errw bytes.Buffer
	app := App{Out: &out, Err: &errw, Cwd: repo, Interactive: false}
	code := app.Run(context.Background(), []string{"config", "--scope", "project", "--set", "review.yolo=true", "--set", "ci.timeout=15m", "--set", "auto_merge.enabled=true"})
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errw.String())
	}
	out.Reset()
	code = app.Run(context.Background(), []string{"config"})
	if code != ExitOK {
		t.Fatalf("expected config exit 0, got %d", code)
	}
	text := out.String()
	for _, want := range []string{"review.yolo,\"true\"", "ci.timeout,15m", "auto_merge.enabled,\"true\""} {
		if !strings.Contains(text, want) {
			t.Fatalf("config output missing %q in:\n%s", want, text)
		}
	}
}

func TestConfigSetPersistsGlobalSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out, errw bytes.Buffer
	app := App{Out: &out, Err: &errw, Cwd: t.TempDir(), Interactive: false}
	code := app.Run(context.Background(), []string{"config", "--scope", "global", "--set", "auto_merge.enabled=true"})
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errw.String())
	}
	out.Reset()
	code = app.Run(context.Background(), []string{"config"})
	if code != ExitOK {
		t.Fatalf("expected config exit 0, got %d", code)
	}
	if !strings.Contains(out.String(), "auto_merge.enabled,\"true\"") {
		t.Fatalf("config output missing global auto merge setting:\n%s", out.String())
	}
}

func TestPromptWizardContextCancelCancels(t *testing.T) {
	var errw bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := App{In: strings.NewReader(""), Err: &errw, Interactive: true}
	value, cancelled := app.promptWizard(ctx, "Main branch", "main")
	if !cancelled {
		t.Fatalf("expected context cancellation to cancel, got value %q", value)
	}
}

func TestInteractiveProgressUsesLeftAlignedTUIStep(t *testing.T) {
	var errw bytes.Buffer
	app := App{Err: &errw, Interactive: true}
	app.progress("checking documentation")
	got := appStripANSI(errw.String())
	want := "⠋  checking documentation\n│\n"
	if got != want {
		t.Fatalf("progress output = %q, want %q", got, want)
	}
	if strings.Contains(got, "│  ⠋") {
		t.Fatalf("progress marker should be left aligned, got %q", got)
	}
}

func TestInteractiveProgressDoneUsesCompletedMarker(t *testing.T) {
	var errw bytes.Buffer
	app := App{Err: &errw, Interactive: true}
	app.progressDone("reusing current treehouse worktree %s", "/tmp/worktree")
	got := appStripANSI(errw.String())
	want := "◇  reusing current treehouse worktree /tmp/worktree\n│\n"
	if got != want {
		t.Fatalf("progress done output = %q, want %q", got, want)
	}
	if strings.Contains(got, "│  ◇") {
		t.Fatalf("progress marker should be left aligned, got %q", got)
	}
}

func TestRunValidationCommandsSkipsEmptyTestWithoutDetection(t *testing.T) {
	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := runstate.New(worktree, "feature/test", "main", "origin", "abc", "origin/main")
	state.WorktreePath = worktree
	cfg := config.Defaults()
	cfg.Commands.Lint = "true"
	var errw bytes.Buffer
	app := App{Err: &errw, Interactive: false}
	if err := app.runValidationCommands(context.Background(), cfg, runOptions{}, &state); err != nil {
		t.Fatalf("runValidationCommands returned error: %v", err)
	}
	if got := appTestStepStatus(state, "test"); got != runstate.StatusSkipped {
		t.Fatalf("test status = %s, want skipped", got)
	}
	if got := appTestStepStatus(state, "lint"); got != runstate.StatusCompleted {
		t.Fatalf("lint status = %s, want completed", got)
	}
}

func TestRunValidationCommandsUsesPerRunTestCommand(t *testing.T) {
	worktree := t.TempDir()
	state := runstate.New(worktree, "feature/test", "main", "origin", "abc", "origin/main")
	state.WorktreePath = worktree
	cfg := config.Defaults()
	cfg.Commands.Lint = "true"
	var errw bytes.Buffer
	app := App{Err: &errw, Interactive: false}
	opts := runOptions{TestCommand: "echo ran > ran.txt"}
	if err := app.runValidationCommands(context.Background(), cfg, opts, &state); err != nil {
		t.Fatalf("runValidationCommands returned error: %v", err)
	}
	if got := appTestStepStatus(state, "test"); got != runstate.StatusCompleted {
		t.Fatalf("test status = %s, want completed", got)
	}
	data, err := os.ReadFile(filepath.Join(worktree, "ran.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "ran" {
		t.Fatalf("unexpected test output file: %q", data)
	}
}

func TestCommitDirtyOrHeadChangedDetectsAgentCommit(t *testing.T) {
	repo := newAppTestRepo(t, filepath.Join(t.TempDir(), "repo"))
	before := strings.TrimSpace(runGitAppTestOutput(t, repo, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("agent committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, repo, "add", "file.txt")
	runGitAppTest(t, repo, "commit", "-m", "agent fix")
	changed, err := commitDirtyOrHeadChanged(context.Background(), repo, "nml(ci): address failing checks", before)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected helper to detect clean worktree with a new agent commit")
	}
}

func TestPRBodyListsReviewFindingsAndConciseCommands(t *testing.T) {
	state := runstate.New("/tmp/repo", "feature/source", "main", "origin", "abc", "origin/main")
	state.ReviewBranch = "nml/test"
	state.Tests = []runstate.CommandRun{{Command: "bun test", Status: runstate.StatusCompleted, Detail: "bun test | very long output"}}
	state.Lint = []runstate.CommandRun{{Command: "bun biome check --write", Status: runstate.StatusCompleted, Detail: "bun biome check --write | fixed 18 files"}}
	for i := range state.Steps {
		if state.Steps[i].Name == "review" {
			state.Steps[i].Rounds = []runstate.ReviewRound{
				{Number: 1, Result: "findings", Findings: []review.Finding{{ID: "r1", Severity: review.SeverityWarning, File: "app.tsx", Line: 42, Description: "fix fallback identity"}}},
				{Number: 2, Result: "LGTM"},
			}
		}
	}
	body := prBody(&state, "owner/repo")
	for _, want := range []string{"- Round 1: findings with 1 findings", "  - `r1` WARNING `app.tsx:42` - fix fallback identity", "- Round 2: LGTM", "- Test: completed - bun test", "- Lint: completed - bun biome check --write"} {
		if !strings.Contains(body, want) {
			t.Fatalf("PR body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "very long output") || strings.Contains(body, "fixed 18 files") {
		t.Fatalf("PR body should not include verbose command output:\n%s", body)
	}
}

func appTestStepStatus(state runstate.State, name string) runstate.StepStatus {
	for _, step := range state.Steps {
		if step.Name == name {
			return step.Status
		}
	}
	return ""
}

func TestRunReviewSkipReviewDoesNotRequireAgent(t *testing.T) {
	state := runstate.New(t.TempDir(), "feature/test", "main", "origin", "abc", "origin/main")
	state.WorktreePath = t.TempDir()
	cfg := config.Defaults()
	cfg.Agent.Name = "definitely-missing-nml-agent"
	var errw bytes.Buffer
	app := App{Err: &errw, Interactive: false}
	outcome, err := app.runReview(context.Background(), cfg, runOptions{SkipReview: true}, &state)
	if err != nil {
		t.Fatalf("runReview returned error: %v", err)
	}
	if outcome.AwaitingUser {
		t.Fatal("skip review should not wait for user input")
	}
	for _, step := range state.Steps {
		if step.Name == "review" {
			if step.Status != runstate.StatusSkipped {
				t.Fatalf("review status = %s, want skipped", step.Status)
			}
			if step.Detail != "skipped by --skip-review" {
				t.Fatalf("review detail = %q", step.Detail)
			}
			return
		}
	}
	t.Fatal("review step missing")
}

func TestRunReviewAutoApprovesAfterConfiguredRounds(t *testing.T) {
	repo := newAppTestRepo(t, filepath.Join(t.TempDir(), "repo"))
	runGitAppTest(t, repo, "checkout", "-b", "feature/auto-approve")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("hello\nchange\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, repo, "add", "file.txt")
	runGitAppTest(t, repo, "commit", "-m", "feat: change file")
	sourceHead := strings.TrimSpace(runGitAppTestOutput(t, repo, "rev-parse", "HEAD"))

	fakePi := filepath.Join(t.TempDir(), "pi")
	if err := os.WriteFile(fakePi, []byte("#!/bin/sh\necho '- [warning] file.txt:1 - unresolved finding'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Agent.Name = "pi"
	cfg.Agent.PathOverrides["pi"] = fakePi
	cfg.Review.Rounds = 3
	cfg.Review.AutoApproveAfterRounds = true
	state := runstate.New(repo, "feature/auto-approve", "main", "origin", sourceHead, "main")
	state.WorktreePath = repo
	state.Intent = "change file"

	var errw bytes.Buffer
	app := App{Err: &errw, Interactive: false}
	outcome, err := app.runReview(context.Background(), cfg, runOptions{Yolo: true}, &state)
	if err != nil {
		t.Fatalf("runReview returned error: %v\nstderr:\n%s", err, errw.String())
	}
	if outcome.AwaitingUser {
		t.Fatal("auto-approved review should not wait for user input")
	}
	var reviewStep runstate.Step
	for _, step := range state.Steps {
		if step.Name == "review" {
			reviewStep = step
			break
		}
	}
	if reviewStep.Status != runstate.StatusCompleted {
		t.Fatalf("review status = %s, want completed", reviewStep.Status)
	}
	if !strings.Contains(reviewStep.Detail, "auto-approved after configured review rounds") {
		t.Fatalf("review detail should mention auto approval, got %q", reviewStep.Detail)
	}
	if len(reviewStep.Rounds) != 3 {
		t.Fatalf("review rounds = %d, want 3", len(reviewStep.Rounds))
	}
	if reviewStep.Rounds[2].Result != "auto_approved" {
		t.Fatalf("final round result = %q, want auto_approved", reviewStep.Rounds[2].Result)
	}
}

func TestWatchPRChecksWaitsForLateReportedChecks(t *testing.T) {
	calls := 0
	var sleeps []time.Duration
	runner := func(ctx context.Context, ghPath, cwd string, args ...string) (string, error) {
		calls++
		if calls == 1 {
			return "no checks reported on the branch", errors.New("exit status 1")
		}
		return "All checks were successful", nil
	}
	sleeper := func(ctx context.Context, d time.Duration) bool {
		sleeps = append(sleeps, d)
		return true
	}
	out, status, err := watchPRChecksWithRunner(context.Background(), "gh", "/repo", "https://example.test/pr/1", 5*time.Minute, 20*time.Second, 2*time.Minute, runner, sleeper)
	if err != nil {
		t.Fatalf("watchPRChecksWithRunner returned error: %v", err)
	}
	if status != "passed" {
		t.Fatalf("status = %s, want passed; output: %s", status, out)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(sleeps) != 1 || sleeps[0] != 20*time.Second {
		t.Fatalf("unexpected sleeps: %v", sleeps)
	}
}

func TestWatchPRChecksNoChecksAfterGrace(t *testing.T) {
	calls := 0
	runner := func(ctx context.Context, ghPath, cwd string, args ...string) (string, error) {
		calls++
		return "no checks reported on the branch", errors.New("exit status 1")
	}
	sleeper := func(ctx context.Context, d time.Duration) bool { return true }
	out, status, err := watchPRChecksWithRunner(context.Background(), "gh", "/repo", "https://example.test/pr/1", 5*time.Minute, 20*time.Second, 40*time.Second, runner, sleeper)
	if err != nil {
		t.Fatalf("watchPRChecksWithRunner returned error: %v", err)
	}
	if status != "no_checks" {
		t.Fatalf("status = %s, want no_checks; output: %s", status, out)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestCIRequiresReportedChecksForRiskyReviewModes(t *testing.T) {
	base := runstate.New("/repo", "feature", "main", "origin", "abc", "origin/main")
	if !ciRequiresReportedChecks(runOptions{Yolo: true}, base) {
		t.Fatal("yolo mode should require reported checks")
	}
	if !ciRequiresReportedChecks(runOptions{SkipReview: true}, base) {
		t.Fatal("skip review option should require reported checks")
	}
	skipped := base
	skipped.SetStep("review", runstate.StatusSkipped, "skipped by user")
	if !ciRequiresReportedChecks(runOptions{}, skipped) {
		t.Fatal("user-skipped review should require reported checks")
	}
	noAgent := base
	noAgent.SetStep("review", runstate.StatusSkipped, "no configured agent; run nml init to enable review")
	if ciRequiresReportedChecks(runOptions{}, noAgent) {
		t.Fatal("missing review agent should not change no-checks policy")
	}
	autoApproved := base
	autoApproved.SetStep("review", runstate.StatusCompleted, "round 3 found 2 findings; auto-approved after configured review rounds")
	if !ciRequiresReportedChecks(runOptions{}, autoApproved) {
		t.Fatal("auto-approved unresolved review findings should require reported checks")
	}
}

func TestPRMergeConflictDetailDetectsDirtyState(t *testing.T) {
	detail, conflict := prMergeConflictDetail(prMergeState{MergeStateStatus: "DIRTY", Mergeable: "CONFLICTING", HeadRefName: "nml/test", BaseRefName: "main"})
	if !conflict {
		t.Fatal("expected DIRTY PR merge state to be treated as a conflict")
	}
	for _, want := range []string{"PR has merge conflicts", "base=main", "head=nml/test", "merge_state=DIRTY", "mergeable=CONFLICTING"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q: %s", want, detail)
		}
	}
	if _, conflict := prMergeConflictDetail(prMergeState{MergeStateStatus: "CLEAN", Mergeable: "MERGEABLE"}); conflict {
		t.Fatal("clean PR merge state should not be treated as a conflict")
	}
}

func TestRunCIWatchMergesPRMergeConflictByDefaultBeforeWatchingChecks(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	repo := newAppTestRepo(t, filepath.Join(root, "repo"))
	bareRemote := filepath.Join(root, "remote.git")
	runGitAppTest(t, root, "init", "--bare", bareRemote)
	runGitAppTest(t, repo, "remote", "add", "origin", bareRemote)
	runGitAppTest(t, repo, "push", "origin", "main")
	runGitAppTest(t, repo, "checkout", "-b", "nml/test")
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, repo, "add", "feature.txt")
	runGitAppTest(t, repo, "commit", "-m", "feat: add feature")
	runGitAppTest(t, repo, "push", "origin", "nml/test")
	runGitAppTest(t, repo, "fetch", "origin", "refs/heads/nml/test:refs/remotes/origin/nml/test")
	runGitAppTest(t, repo, "checkout", "main")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, repo, "add", "base.txt")
	runGitAppTest(t, repo, "commit", "-m", "feat: advance base")
	runGitAppTest(t, repo, "push", "origin", "main")
	runGitAppTest(t, repo, "checkout", "nml/test")

	fakeGH := filepath.Join(root, "gh")
	ghState := filepath.Join(root, "gh-state")
	ghLog := filepath.Join(root, "gh.log")
	ghScript := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$NML_FAKE_GH_LOG"
case "${1:-} ${2:-}" in
  "pr view")
    count=0
    if [ -f "$NML_FAKE_GH_STATE" ]; then
      count="$(cat "$NML_FAKE_GH_STATE")"
    fi
    count=$((count + 1))
    printf '%s\n' "$count" > "$NML_FAKE_GH_STATE"
    if [ "$count" -eq 1 ]; then
      printf '{"mergeStateStatus":"DIRTY","mergeable":"CONFLICTING","headRefName":"nml/test","baseRefName":"main","isDraft":false}\n'
    else
      printf '{"mergeStateStatus":"CLEAN","mergeable":"MERGEABLE","headRefName":"nml/test","baseRefName":"main","isDraft":false}\n'
    fi
    ;;
  "pr checks")
    echo "All checks were successful"
    ;;
  *)
    echo "unexpected gh command: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fakeGH, []byte(ghScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NML_FAKE_GH_STATE", ghState)
	t.Setenv("NML_FAKE_GH_LOG", ghLog)

	cfg := config.Defaults()
	cfg.CI.Timeout = "5s"
	cfg.CI.PollInterval = "1s"
	cfg.Commands.Test = "true"
	cfg.Commands.Lint = "true"
	cfg.Docs.Enabled = false
	state := runstate.New(repo, "feature/source", "main", "origin", "abc", "origin/main")
	state.WorktreePath = repo
	state.ReviewBranch = "nml/test"
	state.PRURL = "https://github.com/owner/repo/pull/1"
	state.Intent = "add feature file"
	var errw bytes.Buffer
	app := App{Err: &errw, Cwd: repo, Interactive: false}
	if err := app.runCIWatch(context.Background(), cfg, runOptions{}, &state, fakeGH); err != nil {
		t.Fatalf("runCIWatch returned error: %v\nstderr:\n%s", err, errw.String())
	}
	if got := appTestStepStatus(state, "ci"); got != runstate.StatusCompleted {
		t.Fatalf("ci status = %s, want completed", got)
	}
	if _, err := (gitx.Client{Dir: repo}).Run(context.Background(), "merge-base", "--is-ancestor", "origin/main", "HEAD"); err != nil {
		t.Fatalf("review branch did not include origin/main after conflict repair: %v", err)
	}
	parents := strings.Fields(strings.TrimSpace(runGitAppTestOutput(t, repo, "rev-list", "--parents", "-n", "1", "HEAD")))
	if len(parents) != 3 {
		t.Fatalf("default conflict repair should create a merge commit, rev-list --parents fields: %v", parents)
	}
	logData, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "pr checks") {
		t.Fatalf("expected CI checks to be watched after conflict resolution, gh log:\n%s", logData)
	}
}

func TestRunCIWatchRechecksPRMergeConflictAfterUnsuccessfulCheckWatch(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	repo := newAppTestRepo(t, filepath.Join(root, "repo"))
	bareRemote := filepath.Join(root, "remote.git")
	runGitAppTest(t, root, "init", "--bare", bareRemote)
	runGitAppTest(t, repo, "remote", "add", "origin", bareRemote)
	runGitAppTest(t, repo, "push", "origin", "main")
	runGitAppTest(t, repo, "checkout", "-b", "nml/test")
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, repo, "add", "feature.txt")
	runGitAppTest(t, repo, "commit", "-m", "feat: add feature")
	runGitAppTest(t, repo, "push", "origin", "nml/test")
	runGitAppTest(t, repo, "fetch", "origin", "refs/heads/nml/test:refs/remotes/origin/nml/test")
	runGitAppTest(t, repo, "checkout", "main")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, repo, "add", "base.txt")
	runGitAppTest(t, repo, "commit", "-m", "feat: advance base")
	runGitAppTest(t, repo, "push", "origin", "main")
	runGitAppTest(t, repo, "checkout", "nml/test")

	fakeGH := filepath.Join(root, "gh")
	ghState := filepath.Join(root, "gh-state")
	ghLog := filepath.Join(root, "gh.log")
	ghScript := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$NML_FAKE_GH_LOG"
read_count() {
  if [ -f "$NML_FAKE_GH_STATE" ]; then
    cat "$NML_FAKE_GH_STATE"
  else
    echo 0
  fi
}
case "${1:-} ${2:-}" in
  "pr view")
    count="$(read_count)"
    count=$((count + 1))
    printf '%s\n' "$count" > "$NML_FAKE_GH_STATE"
    if [ "$count" -eq 2 ]; then
      printf '{"mergeStateStatus":"DIRTY","mergeable":"CONFLICTING","headRefName":"nml/test","baseRefName":"main","isDraft":false}\n'
    else
      printf '{"mergeStateStatus":"CLEAN","mergeable":"MERGEABLE","headRefName":"nml/test","baseRefName":"main","isDraft":false}\n'
    fi
    ;;
  "pr checks")
    count="$(read_count)"
    if [ "$count" -ge 3 ]; then
      echo "All checks were successful"
    else
      echo "no checks reported on the branch"
      exit 1
    fi
    ;;
  *)
    echo "unexpected gh command: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fakeGH, []byte(ghScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NML_FAKE_GH_STATE", ghState)
	t.Setenv("NML_FAKE_GH_LOG", ghLog)

	cfg := config.Defaults()
	cfg.CI.Timeout = "200ms"
	cfg.CI.PollInterval = "1ms"
	cfg.Commands.Test = "true"
	cfg.Commands.Lint = "true"
	cfg.Docs.Enabled = false
	state := runstate.New(repo, "feature/source", "main", "origin", "abc", "origin/main")
	state.WorktreePath = repo
	state.ReviewBranch = "nml/test"
	state.PRURL = "https://github.com/owner/repo/pull/1"
	state.Intent = "add feature file"
	var errw bytes.Buffer
	app := App{Err: &errw, Cwd: repo, Interactive: false}
	if err := app.runCIWatch(context.Background(), cfg, runOptions{SkipReview: true}, &state, fakeGH); err != nil {
		t.Fatalf("runCIWatch returned error: %v\nstderr:\n%s", err, errw.String())
	}
	if got := appTestStepStatus(state, "ci"); got != runstate.StatusCompleted {
		t.Fatalf("ci status = %s, want completed", got)
	}
	if _, err := (gitx.Client{Dir: repo}).Run(context.Background(), "merge-base", "--is-ancestor", "origin/main", "HEAD"); err != nil {
		t.Fatalf("review branch did not include origin/main after late conflict repair: %v", err)
	}
	logData, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatal(err)
	}
	if views := strings.Count(string(logData), "pr view"); views < 3 {
		t.Fatalf("expected PR merge state to be checked again after unsuccessful CI watch, gh log:\n%s", logData)
	}
	if checks := strings.Count(string(logData), "pr checks"); checks < 2 {
		t.Fatalf("expected CI checks to be watched again after conflict resolution, gh log:\n%s", logData)
	}
}

func TestResolvePRMergeConflictCanRebaseWhenConfigured(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	repo := newAppTestRepo(t, filepath.Join(root, "repo"))
	bareRemote := filepath.Join(root, "remote.git")
	runGitAppTest(t, root, "init", "--bare", bareRemote)
	runGitAppTest(t, repo, "remote", "add", "origin", bareRemote)
	runGitAppTest(t, repo, "push", "origin", "main")
	runGitAppTest(t, repo, "checkout", "-b", "nml/test")
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, repo, "add", "feature.txt")
	runGitAppTest(t, repo, "commit", "-m", "feat: add feature")
	runGitAppTest(t, repo, "push", "origin", "nml/test")
	runGitAppTest(t, repo, "checkout", "main")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, repo, "add", "base.txt")
	runGitAppTest(t, repo, "commit", "-m", "feat: advance base")
	runGitAppTest(t, repo, "push", "origin", "main")
	runGitAppTest(t, repo, "checkout", "nml/test")

	cfg := config.Defaults()
	cfg.ConflictResolution.Mode = "rebase"
	cfg.Commands.Test = "true"
	cfg.Commands.Lint = "true"
	cfg.Docs.Enabled = false
	state := runstate.New(repo, "feature/source", "main", "origin", "abc", "origin/main")
	state.WorktreePath = repo
	state.ReviewBranch = "nml/test"
	state.PRURL = "https://github.com/owner/repo/pull/1"
	state.Intent = "add feature file"
	var errw bytes.Buffer
	app := App{Err: &errw, Cwd: repo, Interactive: false}
	if err := app.resolvePRMergeConflict(context.Background(), cfg, runOptions{}, &state, "PR has merge conflicts"); err != nil {
		t.Fatalf("resolvePRMergeConflict returned error: %v\nstderr:\n%s", err, errw.String())
	}
	if _, err := (gitx.Client{Dir: repo}).Run(context.Background(), "merge-base", "--is-ancestor", "origin/main", "HEAD"); err != nil {
		t.Fatalf("review branch did not include origin/main after rebase repair: %v", err)
	}
	parents := strings.Fields(strings.TrimSpace(runGitAppTestOutput(t, repo, "rev-list", "--parents", "-n", "1", "HEAD")))
	if len(parents) != 2 {
		t.Fatalf("rebase conflict repair should keep a linear head commit, rev-list --parents fields: %v", parents)
	}
}

func TestBuiltInReviewFindingsCatchesIntentionalBug(t *testing.T) {
	diff := `diff --git a/apps/server/src/http/core-routes.ts b/apps/server/src/http/core-routes.ts
+++ b/apps/server/src/http/core-routes.ts
+app.get("/healthz", () => {
+	throw new Error("INTENTIONAL_TEST_BUG: health check is broken");
+});`
	findings := builtInReviewFindings(diff)
	if len(findings) == 0 {
		t.Fatal("expected built-in finding")
	}
	if findings[0].File != "apps/server/src/http/core-routes.ts" {
		t.Fatalf("unexpected file: %s", findings[0].File)
	}
}

func TestUnsafeGeneratedIntentRejectsIntentionalBug(t *testing.T) {
	unsafe := []string{
		"Introduce an intentional health check failure to test validation",
		"Verify failure detection by intentionally breaking the health check endpoint.",
	}
	for _, intent := range unsafe {
		if !unsafeGeneratedIntent(intent) {
			t.Fatalf("expected unsafe generated intent: %s", intent)
		}
	}
	if unsafeGeneratedIntent("Add a normal health check endpoint") {
		t.Fatal("did not expect safe intent to be rejected")
	}
}

func TestParseGitHubRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:owner/repo.git":      "owner/repo",
		"https://github.com/owner/repo":      "owner/repo",
		"https://github.com/owner/re.po.git": "owner/re.po",
	}
	for input, want := range cases {
		got, ok := parseGitHubRemote(input)
		if !ok || got != want {
			t.Fatalf("parseGitHubRemote(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
	if got, ok := parseGitHubRemote("https://example.com/owner/repo"); ok || got != "" {
		t.Fatalf("expected non-GitHub remote to fail, got %q, %v", got, ok)
	}
}

func TestCreateOrUpdatePRViewsExistingBranch(t *testing.T) {
	dir := t.TempDir()
	fakeGH := filepath.Join(dir, "gh")
	script := `#!/bin/sh
set -eu
case "${1:-} ${2:-}" in
  "pr view")
    test "${3:-}" = "nml/test-branch"
    echo "https://github.com/owner/repo/pull/7"
    ;;
  "pr edit")
    test "${3:-}" = "https://github.com/owner/repo/pull/7"
    ;;
  "pr create")
    echo "pr create should not run when an existing PR is found" >&2
    exit 2
    ;;
  *)
    echo "unexpected gh command: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fakeGH, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	url, err := createOrUpdatePR(context.Background(), fakeGH, dir, "main", "nml/test-branch", "title", "body")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/owner/repo/pull/7" {
		t.Fatalf("url = %q", url)
	}
}

func TestRunGHDisablesPrompts(t *testing.T) {
	dir := t.TempDir()
	fakeGH := filepath.Join(dir, "gh")
	script := `#!/bin/sh
set -eu
if [ "${GH_PROMPT_DISABLED:-}" != "1" ]; then
  echo "GH_PROMPT_DISABLED not set" >&2
  exit 2
fi
if [ "${GIT_TERMINAL_PROMPT:-}" != "0" ]; then
  echo "GIT_TERMINAL_PROMPT not set" >&2
  exit 2
fi
if read line; then
  echo "stdin should be empty" >&2
  exit 2
fi
echo ok
`
	if err := os.WriteFile(fakeGH, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runGH(context.Background(), fakeGH, dir, "pr", "view")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("unexpected gh output: %q", out)
	}
}

func TestRunPreparesTreehouseWorktreeForCleanFeatureBranch(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	fakeBin := filepath.Join(root, "bin")
	leaseRoot := filepath.Join(root, "leases")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeTreehouse := filepath.Join(fakeBin, "treehouse")
	script := `#!/bin/sh
set -eu
case "${1:-}" in
  get)
    target="$NML_FAKE_TREEHOUSE_ROOT/worktree-$$"
    mkdir -p "$NML_FAKE_TREEHOUSE_ROOT"
    git worktree add --detach "$target" HEAD >/dev/null 2>&1
    echo "$target"
    ;;
  return)
    git worktree remove -f "$2" >/dev/null 2>&1
    ;;
  *)
    echo "unexpected treehouse command: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fakeTreehouse, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	fakePi := filepath.Join(fakeBin, "pi")
	if err := os.WriteFile(fakePi, []byte("#!/bin/sh\ncat >/dev/null\necho LGTM\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeGH := filepath.Join(fakeBin, "gh")
	ghScript := `#!/bin/sh
set -eu
case "${1:-} ${2:-}" in
  "auth status")
    exit 0
    ;;
  "pr view")
    exit 1
    ;;
  "pr create")
    echo "https://github.com/owner/repo/pull/1"
    ;;
  "pr edit")
    exit 0
    ;;
  "pr checks")
    echo "All checks were successful"
    ;;
  *)
    echo "unexpected gh command: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fakeGH, []byte(ghScript), 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(home, ".config", "nml")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configData := "agent:\n  name: pi\n  path_overrides:\n    pi: " + fakePi + "\nreview:\n  rounds: 1\ncommands:\n  lint: git diff --quiet\ncleanup:\n  auto: false\nmain_branch: main\nremote: origin\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	oldHome := os.Getenv("HOME")
	oldLeaseRoot := os.Getenv("NML_FAKE_TREEHOUSE_ROOT")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+oldPath)
	t.Setenv("HOME", home)
	t.Setenv("NML_FAKE_TREEHOUSE_ROOT", leaseRoot)
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		_ = os.Setenv("HOME", oldHome)
		_ = os.Setenv("NML_FAKE_TREEHOUSE_ROOT", oldLeaseRoot)
	})

	repo := newAppTestRepo(t, filepath.Join(root, "repo"))
	bareRemote := filepath.Join(root, "remote.git")
	runGitAppTest(t, root, "init", "--bare", bareRemote)
	runGitAppTest(t, repo, "remote", "add", "origin", "git@github.com:owner/repo.git")
	runGitAppTest(t, repo, "remote", "set-url", "--push", "origin", bareRemote)
	runGitAppTest(t, repo, "config", "url."+bareRemote+".insteadOf", "git@github.com:owner/repo.git")
	runGitAppTest(t, repo, "push", "origin", "main")
	runGitAppTest(t, repo, "checkout", "-b", "feature/change")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, repo, "add", "file.txt")
	runGitAppTest(t, repo, "commit", "-m", "feat: change file")
	sourceHead := strings.TrimSpace(runGitAppTestOutput(t, repo, "rev-parse", "HEAD"))

	var out, errw bytes.Buffer
	app := App{Out: &out, Err: &errw, Cwd: repo, Interactive: false}
	code := app.Run(context.Background(), []string{"run", "--fetch=false", "--test-command", "grep -q feature file.txt"})
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errw.String())
	}
	if !strings.Contains(out.String(), "status: completed") {
		t.Fatalf("expected completed output, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "review,completed") {
		t.Fatalf("expected completed review step, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "test,completed") || !strings.Contains(out.String(), "lint,completed") {
		t.Fatalf("expected completed test and lint steps, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "pr,completed") || !strings.Contains(out.String(), "ci,completed") {
		t.Fatalf("expected completed PR and CI steps, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "pr_url: \"https://github.com/owner/repo/pull/1\"") {
		t.Fatalf("expected PR URL in compact completion output, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "cleanup: manual") {
		t.Fatalf("expected manual cleanup status, got:\n%s", out.String())
	}
	if strings.Contains(out.String(), "grep -q feature file.txt") || strings.Contains(out.String(), "All checks were successful") {
		t.Fatalf("expected compact completion output without command logs, got:\n%s", out.String())
	}
	worktreePath := valueFromTOONLine(out.String(), "worktree_path")
	if worktreePath == "" {
		t.Fatalf("worktree_path missing from output:\n%s", out.String())
	}
	content, err := os.ReadFile(filepath.Join(worktreePath, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != "feature" {
		t.Fatalf("expected copied feature content, got %q", content)
	}
	branch := strings.TrimSpace(runGitAppTestOutput(t, worktreePath, "branch", "--show-current"))
	if !strings.HasPrefix(branch, "nml/") {
		t.Fatalf("expected nml review branch, got %q", branch)
	}
	reviewHead := strings.TrimSpace(runGitAppTestOutput(t, worktreePath, "rev-parse", "HEAD"))
	if reviewHead != sourceHead {
		t.Fatalf("expected review branch to reuse source commit %s, got %s", sourceHead, reviewHead)
	}
}

func TestCleanupRunWorktreeReturnsTreehouseLease(t *testing.T) {
	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "return.log")
	fakeTreehouse := filepath.Join(fakeBin, "treehouse")
	script := `#!/bin/sh
set -eu
case "${1:-}" in
  return)
    printf '%s %s\n' "$2" "${3:-}" > "$NML_RETURN_LOG"
    ;;
  *)
    echo "unexpected treehouse command: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fakeTreehouse, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+oldPath)
	t.Setenv("NML_RETURN_LOG", logPath)
	worktree := filepath.Join(root, "worktree")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	state := runstate.New(root, "feature/test", "main", "origin", "abc", "origin/main")
	state.WorktreePath = worktree
	var errw bytes.Buffer
	app := App{Out: &bytes.Buffer{}, Err: &errw, Cwd: root, Interactive: false}
	cleanup := app.cleanupRunWorktree(context.Background(), cfg, state)
	if cleanup.Status != "auto_returned" {
		t.Fatalf("cleanup status = %q", cleanup.Status)
	}
	if strings.TrimSpace(errw.String()) != "" {
		t.Fatalf("expected quiet successful cleanup, got stderr %q", errw.String())
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != worktree+" --force" {
		t.Fatalf("unexpected return command: %q", content)
	}
}

func TestCleanupRunWorktreeSkipsCurrentTerminalWorktree(t *testing.T) {
	root := t.TempDir()
	worktree := filepath.Join(root, "worktree")
	nested := filepath.Join(worktree, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	state := runstate.New(root, "feature/test", "main", "origin", "abc", "origin/main")
	state.WorktreePath = worktree
	var errw bytes.Buffer
	app := App{Out: &bytes.Buffer{}, Err: &errw, Cwd: nested, Interactive: false}
	cleanup := app.cleanupRunWorktree(context.Background(), cfg, state)
	if cleanup.Status != "current_terminal" {
		t.Fatalf("cleanup status = %q", cleanup.Status)
	}
	if !strings.Contains(errw.String(), "current terminal") || !strings.Contains(errw.String(), worktree) {
		t.Fatalf("expected current terminal cleanup warning, got %q", errw.String())
	}
}

func TestRunReusesCurrentTreehouseWorktree(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeTreehouse := filepath.Join(fakeBin, "treehouse")
	script := `#!/bin/sh
set -eu
case "${1:-}" in
  get)
    echo "treehouse get should not run from a managed worktree" >&2
    exit 2
    ;;
  return)
    exit 0
    ;;
  *)
    echo "unexpected treehouse command: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fakeTreehouse, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	fakePi := filepath.Join(fakeBin, "pi")
	if err := os.WriteFile(fakePi, []byte("#!/bin/sh\ncat >/dev/null\necho LGTM\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(home, ".config", "nml")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configData := "agent:\n  name: pi\n  path_overrides:\n    pi: " + fakePi + "\nreview:\n  rounds: 1\ncleanup:\n  auto: false\nmain_branch: main\nremote: origin\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+oldPath)
	t.Setenv("HOME", home)

	pool := filepath.Join(root, "pool")
	repo := filepath.Join(pool, "1", "repo")
	repo = newAppTestRepo(t, repo)
	quoted, err := json.Marshal(repo)
	if err != nil {
		t.Fatal(err)
	}
	state := `{"worktrees":[{"name":"1","path":` + string(quoted) + `}]}`
	if err := os.WriteFile(filepath.Join(pool, "treehouse-state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, repo, "checkout", "-b", "feature/current-treehouse")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("treehouse\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, repo, "add", "file.txt")
	runGitAppTest(t, repo, "commit", "-m", "feat: current treehouse")
	sourceHead := strings.TrimSpace(runGitAppTestOutput(t, repo, "rev-parse", "HEAD"))

	var out, errw bytes.Buffer
	app := App{Out: &out, Err: &errw, Cwd: repo, Interactive: false}
	code := app.Run(context.Background(), []string{"run", "--fetch=false", "--test-command", "grep -q treehouse file.txt"})
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errw.String())
	}
	worktreePath := valueFromTOONLine(out.String(), "worktree_path")
	if cleanAbs(worktreePath) != cleanAbs(repo) {
		t.Fatalf("expected current worktree path %q, got %q\n%s", cleanAbs(repo), cleanAbs(worktreePath), out.String())
	}
	branch := strings.TrimSpace(runGitAppTestOutput(t, repo, "branch", "--show-current"))
	if !strings.HasPrefix(branch, "nml/") {
		t.Fatalf("expected nml review branch, got %q", branch)
	}
	reviewHead := strings.TrimSpace(runGitAppTestOutput(t, repo, "rev-parse", "HEAD"))
	if reviewHead != sourceHead {
		t.Fatalf("expected reused source commit %s, got %s", sourceHead, reviewHead)
	}
}

func valueFromTOONLine(output, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func newAppTestRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, dir, "init", "-b", "main")
	runGitAppTest(t, dir, "config", "user.email", "test@example.com")
	runGitAppTest(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAppTest(t, dir, "add", "file.txt")
	runGitAppTest(t, dir, "commit", "-m", "initial")
	runGitAppTest(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	return dir
}

func runGitAppTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = runGitAppTestOutput(t, dir, args...)
}

func runGitAppTestOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

var appANSIRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func appStripANSI(s string) string {
	return appANSIRE.ReplaceAllString(s, "")
}
