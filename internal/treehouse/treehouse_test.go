package treehouse

import (
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
