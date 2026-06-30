package runstate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/natelindev/no-mistakes-lite/internal/review"
)

type StepStatus string

const (
	StatusPending      StepStatus = "pending"
	StatusRunning      StepStatus = "running"
	StatusAwaitingUser StepStatus = "awaiting_user"
	StatusFixing       StepStatus = "fixing"
	StatusCompleted    StepStatus = "completed"
	StatusSkipped      StepStatus = "skipped"
	StatusFailed       StepStatus = "failed"
	StatusCancelled    StepStatus = "cancelled"
)

type State struct {
	ID            string       `json:"id"`
	RepoRoot      string       `json:"repo_root"`
	SourceBranch  string       `json:"source_branch"`
	ReviewBranch  string       `json:"review_branch,omitempty"`
	MainBranch    string       `json:"main_branch"`
	Remote        string       `json:"remote"`
	SourceHead    string       `json:"source_head"`
	BaseRef       string       `json:"base_ref"`
	Intent        string       `json:"intent"`
	CommitMessage string       `json:"commit_message"`
	WorktreePath  string       `json:"worktree_path,omitempty"`
	PRURL         string       `json:"pr_url,omitempty"`
	Steps         []Step       `json:"steps"`
	Tests         []CommandRun `json:"tests,omitempty"`
	Lint          []CommandRun `json:"lint,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

type Step struct {
	Name     string        `json:"name"`
	Status   StepStatus    `json:"status"`
	Detail   string        `json:"detail,omitempty"`
	Rounds   []ReviewRound `json:"rounds,omitempty"`
	Started  *time.Time    `json:"started_at,omitempty"`
	Finished *time.Time    `json:"finished_at,omitempty"`
}

type ReviewRound struct {
	Number   int              `json:"number"`
	Result   string           `json:"result"`
	Findings []review.Finding `json:"findings,omitempty"`
}

type CommandRun struct {
	Command string     `json:"command"`
	Status  StepStatus `json:"status"`
	Detail  string     `json:"detail,omitempty"`
}

func NewID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102T150405Z"), hex.EncodeToString(b[:]))
}

func New(repoRoot, sourceBranch, mainBranch, remote, sourceHead, baseRef string) State {
	now := time.Now().UTC()
	return State{
		ID:           NewID(),
		RepoRoot:     repoRoot,
		SourceBranch: sourceBranch,
		MainBranch:   mainBranch,
		Remote:       remote,
		SourceHead:   sourceHead,
		BaseRef:      baseRef,
		CreatedAt:    now,
		UpdatedAt:    now,
		Steps: []Step{
			{Name: "preflight", Status: StatusPending},
			{Name: "intent", Status: StatusPending},
			{Name: "commit", Status: StatusPending},
			{Name: "worktree", Status: StatusPending},
			{Name: "review", Status: StatusPending},
			{Name: "test", Status: StatusPending},
			{Name: "docs", Status: StatusPending},
			{Name: "lint", Status: StatusPending},
			{Name: "push", Status: StatusPending},
			{Name: "pr", Status: StatusPending},
			{Name: "ci", Status: StatusPending},
			{Name: "deploy", Status: StatusPending},
			{Name: "final", Status: StatusPending},
		},
	}
}

func (s *State) SetStep(name string, status StepStatus, detail string) {
	now := time.Now().UTC()
	for i := range s.Steps {
		if s.Steps[i].Name == name {
			s.Steps[i].Status = status
			s.Steps[i].Detail = detail
			if status == StatusRunning && s.Steps[i].Started == nil {
				s.Steps[i].Started = &now
			}
			if isTerminal(status) {
				s.Steps[i].Finished = &now
			}
			s.UpdatedAt = now
			return
		}
	}
	s.Steps = append(s.Steps, Step{Name: name, Status: status, Detail: detail})
	s.UpdatedAt = now
}

func isTerminal(status StepStatus) bool {
	switch status {
	case StatusCompleted, StatusSkipped, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

func Save(gitDir string, s State) (string, error) {
	if gitDir == "" {
		return "", fmt.Errorf("git dir is required")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(s.RepoRoot, gitDir)
	}
	dir := filepath.Join(gitDir, "nml", "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	s.UpdatedAt = time.Now().UTC()
	path := filepath.Join(dir, s.ID+".json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}

func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, err
	}
	return s, nil
}

func Latest(gitDir string, repoRoot string) (string, State, error) {
	if gitDir == "" {
		return "", State{}, fmt.Errorf("git dir is required")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoRoot, gitDir)
	}
	dir := filepath.Join(gitDir, "nml", "runs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", State{}, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	if len(paths) == 0 {
		return "", State{}, os.ErrNotExist
	}
	sort.Strings(paths)
	path := paths[len(paths)-1]
	state, err := Load(path)
	return path, state, err
}
