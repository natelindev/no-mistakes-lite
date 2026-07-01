package treehouse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLeasePathPlainPath(t *testing.T) {
	got, err := ParseLeasePath("/tmp/worktree\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/worktree" {
		t.Fatalf("got %q", got)
	}
}

func TestParseLeasePathBannerWithFinalPath(t *testing.T) {
	got, err := ParseLeasePath("🌳 Setting up worktree...\n🌳 Leased worktree at ~/.treehouse/repo/1/repo. Run 'treehouse return ~/.treehouse/repo/1/repo' to release it.\n/tmp/repo-worktree\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/repo-worktree" {
		t.Fatalf("got %q", got)
	}
}

func TestParseLeasePathBannerOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := ParseLeasePath("🌳 Leased worktree at ~/.treehouse/repo/1/repo. Run 'treehouse return ~/.treehouse/repo/1/repo' to release it.")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".treehouse", "repo", "1", "repo")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestManagedWorktreeRootFindsTreehouseState(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	worktree := filepath.Join(pool, "1", "repo")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	quoted, err := json.Marshal(worktree)
	if err != nil {
		t.Fatal(err)
	}
	state := `{"worktrees":[{"name":"1","path":` + string(quoted) + `}]}`
	if err := os.WriteFile(filepath.Join(pool, "treehouse-state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := ManagedWorktreeRoot(worktree)
	if !ok {
		t.Fatal("expected managed worktree detection")
	}
	if got != cleanPath(worktree) {
		t.Fatalf("got %q, want %q", got, cleanPath(worktree))
	}
}
