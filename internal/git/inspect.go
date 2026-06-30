package git

import (
	"context"
	"errors"
	"fmt"
)

type Kind string

const (
	KindNoRepo             Kind = "no_repo"
	KindDirty              Kind = "dirty"
	KindCleanMainNoop      Kind = "clean_main_noop"
	KindFeatureNoDeltaNoop Kind = "feature_no_delta_noop"
	KindFeatureDelta       Kind = "feature_delta"
	KindMainAhead          Kind = "main_ahead"
	KindNeedsRemoteBase    Kind = "needs_remote_base"
)

type State struct {
	Kind          Kind     `json:"kind"`
	RepoRoot      string   `json:"repo_root"`
	Branch        string   `json:"branch"`
	MainBranch    string   `json:"main_branch"`
	Remote        string   `json:"remote"`
	BaseRef       string   `json:"base_ref"`
	BaseAvailable bool     `json:"base_available"`
	Dirty         bool     `json:"dirty"`
	ChangedFiles  []string `json:"changed_files"`
	Ahead         int      `json:"ahead"`
	Behind        int      `json:"behind"`
	HasDiff       bool     `json:"has_diff"`
	PatchOnly     bool     `json:"patch_only"`
	Head          string   `json:"head"`
	Reason        string   `json:"reason"`
}

func Inspect(ctx context.Context, cwd, remote, mainBranch string, fetch bool) (State, error) {
	if remote == "" {
		remote = "origin"
	}
	if mainBranch == "" {
		mainBranch = "main"
	}
	client := Client{Dir: cwd}
	root, err := client.Root(ctx)
	if errors.Is(err, ErrNotRepo) {
		return State{Kind: KindNoRepo, Reason: "current directory is not inside a git repository"}, nil
	}
	if err != nil {
		return State{}, err
	}
	client.Dir = root
	branch, err := client.CurrentBranch(ctx)
	if err != nil {
		return State{}, err
	}
	if detected, err := client.RemoteHead(ctx, remote); err == nil && detected != "" && mainBranch == "" {
		mainBranch = detected
	}
	status, err := client.StatusPorcelain(ctx)
	if err != nil {
		return State{}, err
	}
	dirty := IsDirty(status)
	head, _ := client.Head(ctx)
	state := State{
		RepoRoot:     root,
		Branch:       branch,
		MainBranch:   mainBranch,
		Remote:       remote,
		BaseRef:      remote + "/" + mainBranch,
		Dirty:        dirty,
		ChangedFiles: ChangedFiles(status),
		Head:         head,
	}
	if dirty {
		state.Kind = KindDirty
		state.Reason = "worktree has uncommitted or staged changes"
		return state, nil
	}
	if fetch {
		_ = client.Fetch(ctx, remote, mainBranch)
	}
	state.BaseAvailable = client.RefExists(ctx, state.BaseRef)
	if !state.BaseAvailable {
		state.Kind = KindNeedsRemoteBase
		state.Reason = fmt.Sprintf("remote base %s is not available", state.BaseRef)
		return state, nil
	}
	ahead, behind, err := client.AheadBehind(ctx, state.BaseRef, "HEAD")
	if err != nil {
		return State{}, err
	}
	state.Ahead = ahead
	state.Behind = behind
	hasDiff, err := client.HasDiff(ctx, state.BaseRef)
	if err != nil {
		return State{}, err
	}
	state.HasDiff = hasDiff
	if branch == mainBranch {
		if ahead > 0 {
			state.Kind = KindMainAhead
			state.Reason = fmt.Sprintf("%s is ahead of %s", mainBranch, state.BaseRef)
			return state, nil
		}
		state.Kind = KindCleanMainNoop
		state.Reason = fmt.Sprintf("%s is clean and not ahead of %s", mainBranch, state.BaseRef)
		return state, nil
	}
	rightOnly, err := client.RightOnlyCherry(ctx, state.BaseRef)
	if err != nil {
		return State{}, err
	}
	if !hasDiff || len(rightOnly) == 0 {
		state.Kind = KindFeatureNoDeltaNoop
		state.PatchOnly = len(rightOnly) == 0 && ahead > 0
		if state.PatchOnly {
			state.Reason = "changes are already on main"
		} else {
			state.Reason = fmt.Sprintf("branch has no changes relative to %s", state.BaseRef)
		}
		return state, nil
	}
	state.Kind = KindFeatureDelta
	state.Reason = fmt.Sprintf("branch has changes relative to %s", state.BaseRef)
	return state, nil
}
