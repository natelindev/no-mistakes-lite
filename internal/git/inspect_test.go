package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInspectNoRepo(t *testing.T) {
	state, err := Inspect(context.Background(), t.TempDir(), "origin", "main", false)
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if state.Kind != KindNoRepo {
		t.Fatalf("expected no repo, got %s", state.Kind)
	}
}

func TestInspectCleanMainAndDirtyAndFeatureDelta(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	state, err := Inspect(ctx, dir, "origin", "main", false)
	if err != nil {
		t.Fatalf("Inspect clean main: %v", err)
	}
	if state.Kind != KindCleanMainNoop {
		t.Fatalf("expected clean main noop, got %#v", state)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err = Inspect(ctx, dir, "origin", "main", false)
	if err != nil {
		t.Fatalf("Inspect dirty: %v", err)
	}
	if state.Kind != KindDirty || len(state.ChangedFiles) != 1 {
		t.Fatalf("expected dirty with changed file, got %#v", state)
	}
	runGit(t, dir, "checkout", "--", "file.txt")
	runGit(t, dir, "checkout", "-b", "feature/test")
	state, err = Inspect(ctx, dir, "origin", "main", false)
	if err != nil {
		t.Fatalf("Inspect feature no delta: %v", err)
	}
	if state.Kind != KindFeatureNoDeltaNoop {
		t.Fatalf("expected feature no delta, got %#v", state)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "feat: change file")
	state, err = Inspect(ctx, dir, "origin", "main", false)
	if err != nil {
		t.Fatalf("Inspect feature delta: %v", err)
	}
	if state.Kind != KindFeatureDelta || !state.HasDiff || state.Ahead != 1 {
		t.Fatalf("expected feature delta, got %#v", state)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial")
	runGit(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
