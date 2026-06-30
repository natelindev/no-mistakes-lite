package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/natelindev/no-mistakes-lite/internal/config"
	"github.com/natelindev/no-mistakes-lite/internal/runstate"
)

const dirMode = 0o755

type Entry struct {
	RunID           string    `json:"run_id"`
	RepoID          string    `json:"repo_id"`
	RepoRoot        string    `json:"repo_root"`
	SourceBranch    string    `json:"source_branch"`
	ReviewBranch    string    `json:"review_branch"`
	WorktreePath    string    `json:"worktree_path"`
	PRURL           string    `json:"pr_url"`
	RepoStatePath   string    `json:"repo_state_path"`
	GlobalStatePath string    `json:"global_state_path"`
	Status          string    `json:"status"`
	UpdatedAt       time.Time `json:"updated_at"`
	Resumable       bool      `json:"resumable"`
}

type Event struct {
	Time          time.Time       `json:"time"`
	Kind          string          `json:"kind"`
	Detail        string          `json:"detail,omitempty"`
	RunID         string          `json:"run_id"`
	RepoID        string          `json:"repo_id"`
	Status        string          `json:"status"`
	ReviewBranch  string          `json:"review_branch,omitempty"`
	WorktreePath  string          `json:"worktree_path,omitempty"`
	PRURL         string          `json:"pr_url,omitempty"`
	StepSummaries []StepSummary   `json:"steps,omitempty"`
	Extra         json.RawMessage `json:"extra,omitempty"`
}

type StepSummary struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nml"), nil
}

func SaveSnapshot(state runstate.State, repoStatePath string) (string, error) {
	if strings.TrimSpace(state.ID) == "" {
		return "", fmt.Errorf("run id is required")
	}
	dir, err := runDir(state)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "state.json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return "", err
	}
	entry := NewEntry(state, repoStatePath, path)
	entryData, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeFileAtomic(filepath.Join(dir, "meta.json"), entryData, 0o600); err != nil {
		return "", err
	}
	_ = AppendEvent(state, "snapshot", "state saved")
	return path, nil
}

func AppendEvent(state runstate.State, kind, detail string) error {
	dir, err := runDir(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	event := Event{
		Time:          time.Now().UTC(),
		Kind:          kind,
		Detail:        detail,
		RunID:         state.ID,
		RepoID:        config.RepoID(state.RepoRoot),
		Status:        Status(state),
		ReviewBranch:  state.ReviewBranch,
		WorktreePath:  state.WorktreePath,
		PRURL:         state.PRURL,
		StepSummaries: stepSummaries(state),
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func WriteLog(state runstate.State, name, text string) (string, error) {
	name = safeLogName(name)
	if name == "" {
		return "", fmt.Errorf("log name is required")
	}
	dir, err := runDir(state)
	if err != nil {
		return "", err
	}
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, dirMode); err != nil {
		return "", err
	}
	path := filepath.Join(logDir, name)
	if err := writeFileAtomic(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	_ = AppendEvent(state, "log", name)
	return path, nil
}

func Load(runRef, repoRoot string) (string, runstate.State, error) {
	path, err := Resolve(runRef, repoRoot)
	if err != nil {
		return "", runstate.State{}, err
	}
	state, err := runstate.Load(path)
	if err != nil {
		return "", runstate.State{}, err
	}
	return path, state, nil
}

func Resolve(runRef, repoRoot string) (string, error) {
	runRef = strings.TrimSpace(runRef)
	if runRef == "" {
		return "", fmt.Errorf("run reference is required")
	}
	if strings.Contains(runRef, string(os.PathSeparator)) || strings.HasSuffix(runRef, ".json") {
		if _, err := os.Stat(runRef); err != nil {
			return "", err
		}
		return runRef, nil
	}
	root, err := Root()
	if err != nil {
		return "", err
	}
	var candidates []string
	if strings.TrimSpace(repoRoot) != "" {
		candidates = append(candidates, filepath.Join(root, "runs", config.RepoID(repoRoot), runRef, "state.json"))
	}
	matches, _ := filepath.Glob(filepath.Join(root, "runs", "*", runRef, "state.json"))
	candidates = append(candidates, matches...)
	for _, candidate := range dedupe(candidates) {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}

func Latest(repoRoot string, resumableOnly bool) (string, runstate.State, error) {
	entries, err := List(repoRoot, resumableOnly)
	if err != nil {
		return "", runstate.State{}, err
	}
	if len(entries) == 0 {
		return "", runstate.State{}, os.ErrNotExist
	}
	entry := entries[0]
	state, err := runstate.Load(entry.GlobalStatePath)
	if err != nil {
		return "", runstate.State{}, err
	}
	return entry.GlobalStatePath, state, nil
}

func List(repoRoot string, resumableOnly bool) ([]Entry, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(root, "runs")
	patterns := []string{filepath.Join(base, "*", "*", "state.json")}
	if strings.TrimSpace(repoRoot) != "" {
		patterns = []string{filepath.Join(base, config.RepoID(repoRoot), "*", "state.json")}
	}
	var entries []Entry
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			state, err := runstate.Load(path)
			if err != nil {
				continue
			}
			entry := NewEntry(state, "", path)
			metaPath := filepath.Join(filepath.Dir(path), "meta.json")
			if data, err := os.ReadFile(metaPath); err == nil {
				var meta Entry
				if json.Unmarshal(data, &meta) == nil {
					entry.RepoStatePath = meta.RepoStatePath
				}
			}
			if resumableOnly && !entry.Resumable {
				continue
			}
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})
	return entries, nil
}

func NewEntry(state runstate.State, repoStatePath, globalStatePath string) Entry {
	updated := state.UpdatedAt
	if updated.IsZero() {
		updated = state.CreatedAt
	}
	return Entry{
		RunID:           state.ID,
		RepoID:          config.RepoID(state.RepoRoot),
		RepoRoot:        state.RepoRoot,
		SourceBranch:    state.SourceBranch,
		ReviewBranch:    state.ReviewBranch,
		WorktreePath:    state.WorktreePath,
		PRURL:           state.PRURL,
		RepoStatePath:   repoStatePath,
		GlobalStatePath: globalStatePath,
		Status:          Status(state),
		UpdatedAt:       updated,
		Resumable:       Resumable(state),
	}
}

func Status(state runstate.State) string {
	for _, step := range state.Steps {
		if step.Name == "final" && step.Status == runstate.StatusCompleted {
			return "completed"
		}
	}
	for _, step := range state.Steps {
		if step.Status == runstate.StatusAwaitingUser {
			return "awaiting_user"
		}
	}
	for _, step := range state.Steps {
		if step.Status == runstate.StatusFailed || step.Status == runstate.StatusCancelled {
			return string(step.Status)
		}
	}
	for _, step := range state.Steps {
		if step.Status == runstate.StatusRunning || step.Status == runstate.StatusFixing {
			return "interrupted"
		}
	}
	return "in_progress"
}

func Resumable(state runstate.State) bool {
	return Status(state) != "completed"
}

func runDir(state runstate.State) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(state.RepoRoot) == "" {
		return "", fmt.Errorf("repo root is required")
	}
	if strings.TrimSpace(state.ID) == "" {
		return "", fmt.Errorf("run id is required")
	}
	return filepath.Join(root, "runs", config.RepoID(state.RepoRoot), state.ID), nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func safeLogName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, string(os.PathSeparator), "-")
	name = strings.ReplaceAll(name, "..", ".")
	return name
}

func stepSummaries(state runstate.State) []StepSummary {
	out := make([]StepSummary, 0, len(state.Steps))
	for _, step := range state.Steps {
		out = append(out, StepSummary{Name: step.Name, Status: string(step.Status), Detail: step.Detail})
	}
	return out
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func IsNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
