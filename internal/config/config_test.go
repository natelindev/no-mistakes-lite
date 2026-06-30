package config

import (
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
	if cfg.AutoMerge.Method != "squash" {
		t.Fatalf("unexpected merge method: %s", cfg.AutoMerge.Method)
	}
}

func TestApplyCanOverrideBoolFalse(t *testing.T) {
	cfg := Defaults()
	falseValue := false
	Apply(&cfg, RawConfig{Docs: &RawDocsConfig{Enabled: &falseValue}})
	if cfg.Docs.Enabled {
		t.Fatal("expected docs.enabled false override")
	}
}

func TestRepoIDStable(t *testing.T) {
	left := RepoID("/tmp/example")
	right := RepoID("/tmp/example")
	if left != right || len(left) != 16 {
		t.Fatalf("unexpected repo id: %q %q", left, right)
	}
}

func TestSaveRepoCommandOverridesOnlyThatRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoA := filepath.Join(t.TempDir(), "a")
	repoB := filepath.Join(t.TempDir(), "b")
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
