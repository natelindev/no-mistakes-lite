package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/natelindev/no-mistakes-lite/internal/runstate"
)

func TestSaveSnapshotAndLatest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	state := runstate.New("/repo/example", "feature", "main", "origin", "abc", "origin/main")
	state.ReviewBranch = "nml/example"
	state.WorktreePath = "/tmp/worktree"
	state.SetStep("review", runstate.StatusFailed, "needs retry")

	globalPath, err := SaveSnapshot(state, "/repo/example/.git/nml/runs/"+state.ID+".json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(globalPath); err != nil {
		t.Fatal(err)
	}

	path, loaded, err := Latest(state.RepoRoot, true)
	if err != nil {
		t.Fatal(err)
	}
	if path != globalPath {
		t.Fatalf("latest path = %q, want %q", path, globalPath)
	}
	if loaded.ID != state.ID {
		t.Fatalf("loaded id = %s, want %s", loaded.ID, state.ID)
	}
	if filepath.Base(filepath.Dir(path)) != state.ID {
		t.Fatalf("state path should be under run id dir, got %s", path)
	}
}

func TestCompletedRunIsNotResumable(t *testing.T) {
	state := runstate.New("/repo/example", "feature", "main", "origin", "abc", "origin/main")
	state.SetStep("final", runstate.StatusCompleted, "done")
	if Resumable(state) {
		t.Fatal("completed run should not be resumable")
	}
}

func TestWriteLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	state := runstate.New("/repo/example", "feature", "main", "origin", "abc", "origin/main")
	path, err := WriteLog(state, "ci/failed.log", "failure")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "failure" {
		t.Fatalf("log = %q", data)
	}
	if filepath.Base(path) != "ci-failed.log" {
		t.Fatalf("unsafe log name was not normalized: %s", path)
	}
}
