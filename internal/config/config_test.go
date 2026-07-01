package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.MainBranch != "main" {
		t.Fatalf("unexpected main branch: %s", cfg.MainBranch)
	}
	if cfg.Remote != "origin" {
		t.Fatalf("unexpected remote: %s", cfg.Remote)
	}
	if !cfg.Commit.StageAllDefault {
		t.Fatal("stage_all_default should default true")
	}
	if !cfg.Docs.Enabled {
		t.Fatal("docs should default enabled")
	}
	if cfg.Review.Yolo {
		t.Fatal("review.yolo should default false")
	}
	if cfg.AutoMerge.Enabled {
		t.Fatal("auto_merge.enabled should default false")
	}
	if cfg.AutoMerge.Method != "squash" {
		t.Fatalf("unexpected merge method: %s", cfg.AutoMerge.Method)
	}
	if !cfg.Cleanup.Auto {
		t.Fatal("cleanup.auto should default true")
	}
}

func TestApplyCanOverrideBoolFalse(t *testing.T) {
	cfg := Defaults()
	falseValue := false
	Apply(&cfg, RawConfig{Docs: &RawDocsConfig{Enabled: &falseValue}, Cleanup: &RawCleanupConfig{Auto: &falseValue}})
	if cfg.Docs.Enabled {
		t.Fatal("expected docs.enabled false override")
	}
	if cfg.Cleanup.Auto {
		t.Fatal("expected cleanup.auto false override")
	}
}

func TestRepoIDStable(t *testing.T) {
	left := RepoID("/tmp/example")
	right := RepoID("/tmp/example")
	if left != right || len(left) != 16 {
		t.Fatalf("unexpected repo id: %q %q", left, right)
	}
}

func TestProjectIDRequiresGitRepository(t *testing.T) {
	if ProjectID(t.TempDir()) != "" {
		t.Fatal("project id should be empty outside a git repository")
	}
}

func TestProjectIDIsStableAcrossGitWorktrees(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	worktree := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitConfigTest(t, repo, "init")
	runGitConfigTest(t, repo, "config", "user.email", "test@example.com")
	runGitConfigTest(t, repo, "config", "user.name", "Test User")
	runGitConfigTest(t, repo, "remote", "add", "origin", "git@github.com:TheAnyInt/anyint-mono-gateway.git")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitConfigTest(t, repo, "add", "file.txt")
	runGitConfigTest(t, repo, "commit", "-m", "initial")
	runGitConfigTest(t, repo, "worktree", "add", worktree)
	if ProjectID(repo) != ProjectID(worktree) {
		t.Fatalf("project id should match across worktrees: %s != %s", ProjectID(repo), ProjectID(worktree))
	}
}

func newConfigTestRepo(t *testing.T, name string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitConfigTest(t, repo, "init")
	return repo
}

func runGitConfigTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestApplyCanOverridePersistentRunSettings(t *testing.T) {
	cfg := Defaults()
	trueValue := true
	method := "rebase"
	Apply(&cfg, RawConfig{
		Review:    &RawReviewConfig{Yolo: &trueValue},
		AutoMerge: &RawAutoMergeConfig{Enabled: &trueValue, Method: &method},
	})
	if !cfg.Review.Yolo {
		t.Fatal("expected review.yolo true override")
	}
	if !cfg.AutoMerge.Enabled {
		t.Fatal("expected auto_merge.enabled true override")
	}
	if cfg.AutoMerge.Method != "rebase" {
		t.Fatalf("auto merge method = %q", cfg.AutoMerge.Method)
	}
}

func TestSaveRepoCommandOverridesOnlyThatRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoA := newConfigTestRepo(t, "a")
	repoB := newConfigTestRepo(t, "b")
	if _, err := SaveRepoCommand(repoA, "test", "go test ./..."); err != nil {
		t.Fatal(err)
	}
	cfgA, _, err := Load(repoA)
	if err != nil {
		t.Fatal(err)
	}
	cfgB, _, err := Load(repoB)
	if err != nil {
		t.Fatal(err)
	}
	if cfgA.Commands.Test != "go test ./..." {
		t.Fatalf("repo A test command = %q", cfgA.Commands.Test)
	}
	if cfgB.Commands.Test != "" {
		t.Fatalf("repo B should not inherit repo A test command, got %q", cfgB.Commands.Test)
	}
}

func TestSaveScopedSettingsSupportsGlobalAndProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoA := newConfigTestRepo(t, "a")
	repoB := newConfigTestRepo(t, "b")
	if _, err := SaveScopedSettings("", "global", map[string]string{
		"review.yolo":        "true",
		"auto_merge.enabled": "true",
		"cleanup.auto":       "false",
		"ci.timeout":         "15m",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveScopedSettings(repoA, "project", map[string]string{"auto_merge.enabled": "false"}); err != nil {
		t.Fatal(err)
	}
	cfgA, _, err := Load(repoA)
	if err != nil {
		t.Fatal(err)
	}
	cfgB, _, err := Load(repoB)
	if err != nil {
		t.Fatal(err)
	}
	if !cfgA.Review.Yolo || cfgA.CI.Timeout != "15m" {
		t.Fatalf("repo A did not inherit global settings: %#v", cfgA)
	}
	if cfgA.AutoMerge.Enabled {
		t.Fatal("repo A project setting should override global auto merge")
	}
	if !cfgB.AutoMerge.Enabled {
		t.Fatal("repo B should inherit global auto merge")
	}
	if cfgB.Cleanup.Auto {
		t.Fatal("repo B should inherit global cleanup.auto false")
	}
}
