package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/natelindev/no-mistakes-lite/internal/config"
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
	want := "◶  checking documentation\n│\n"
	if got != want {
		t.Fatalf("progress output = %q, want %q", got, want)
	}
	if strings.Contains(got, "│  ◶") {
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
	app := App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Cwd: root, Interactive: false}
	app.cleanupRunWorktree(context.Background(), cfg, state)
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != worktree+" --force" {
		t.Fatalf("unexpected return command: %q", content)
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
