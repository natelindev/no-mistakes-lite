package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

var ErrNotRepo = errors.New("not a git repository")

type Client struct {
	Dir string
}

type CommandError struct {
	Args   []string
	Output string
	Err    error
}

func (e *CommandError) Error() string {
	if e == nil {
		return ""
	}
	out := strings.TrimSpace(e.Output)
	if out == "" {
		return fmt.Sprintf("git %s: %v", strings.Join(e.Args, " "), e.Err)
	}
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.Args, " "), e.Err, out)
}

func (c Client) Run(ctx context.Context, args ...string) (string, error) {
	return c.RunInput(ctx, "", args...)
}

func (c Client) RunInput(ctx context.Context, input string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if c.Dir != "" {
		cmd.Dir = c.Dir
	}
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	text := out.String()
	if err != nil {
		return text, &CommandError{Args: args, Output: text, Err: err}
	}
	return text, nil
}

func (c Client) Root(ctx context.Context) (string, error) {
	out, err := c.Run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		if strings.Contains(out, "not a git repository") {
			return "", ErrNotRepo
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c Client) GitDir(ctx context.Context) (string, error) {
	out, err := c.Run(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c Client) CurrentBranch(ctx context.Context) (string, error) {
	out, err := c.Run(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil {
		return strings.TrimSpace(out), nil
	}
	out, err = c.Run(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return "HEAD-" + strings.TrimSpace(out), nil
}

func (c Client) Head(ctx context.Context) (string, error) {
	out, err := c.Run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c Client) ShortHead(ctx context.Context) string {
	out, err := c.Run(context.Background(), "rev-parse", "--short=7", "HEAD")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(out)
}

func (c Client) StatusPorcelain(ctx context.Context) (string, error) {
	out, err := c.Run(ctx, "status", "--porcelain=v1")
	if err != nil {
		return "", err
	}
	return out, nil
}

func IsDirty(status string) bool {
	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return true
	}
	return false
}

func ChangedFiles(status string) []string {
	var files []string
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if path == "" {
			continue
		}
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		files = append(files, path)
	}
	return files
}

func (c Client) RemoteHead(ctx context.Context, remote string) (string, error) {
	if remote == "" {
		remote = "origin"
	}
	out, err := c.Run(ctx, "symbolic-ref", "--quiet", "--short", "refs/remotes/"+remote+"/HEAD")
	if err == nil {
		ref := strings.TrimSpace(out)
		prefix := remote + "/"
		return strings.TrimPrefix(ref, prefix), nil
	}
	return "", err
}

func (c Client) RefExists(ctx context.Context, ref string) bool {
	_, err := c.Run(ctx, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

func (c Client) RemoteURL(ctx context.Context, remote string) (string, error) {
	out, err := c.Run(ctx, "config", "--get", "remote."+remote+".url")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c Client) Fetch(ctx context.Context, remote, branch string) error {
	_, err := c.Run(ctx, "fetch", "--quiet", remote, branch)
	return err
}

func (c Client) AheadBehind(ctx context.Context, baseRef, headRef string) (ahead int, behind int, err error) {
	if headRef == "" {
		headRef = "HEAD"
	}
	out, err := c.Run(ctx, "rev-list", "--left-right", "--count", baseRef+"..."+headRef)
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output: %q", strings.TrimSpace(out))
	}
	left, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, err
	}
	right, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, err
	}
	// git prints left count for base-only commits and right count for head-only commits.
	return right, left, nil
}

func (c Client) HasDiff(ctx context.Context, baseRef string) (bool, error) {
	_, err := c.Run(ctx, "diff", "--quiet", baseRef+"...HEAD")
	if err == nil {
		return false, nil
	}
	var ce *CommandError
	if errors.As(err, &ce) {
		if exitErr, ok := ce.Err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil
		}
	}
	return false, err
}

func (c Client) RightOnlyCherry(ctx context.Context, baseRef string) ([]string, error) {
	out, err := c.Run(ctx, "log", "--cherry-pick", "--right-only", "--no-merges", "--format=%H", baseRef+"...HEAD")
	if err != nil {
		return nil, err
	}
	var commits []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			commits = append(commits, line)
		}
	}
	return commits, nil
}

func (c Client) CommitLog(ctx context.Context, baseRef string) (string, error) {
	out, err := c.Run(ctx, "log", "--format=%s%n%b", baseRef+"..HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c Client) DiffStat(ctx context.Context, baseRef string) (string, error) {
	out, err := c.Run(ctx, "diff", "--stat", baseRef+"...HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c Client) WorktreeDiff(ctx context.Context) (string, error) {
	out, err := c.Run(ctx, "diff", "--stat")
	if err != nil {
		return "", err
	}
	staged, err := c.Run(ctx, "diff", "--cached", "--stat")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.TrimSpace(out) + "\n" + strings.TrimSpace(staged)), nil
}

func (c Client) Add(ctx context.Context, paths []string) error {
	args := []string{"add"}
	if len(paths) == 0 {
		args = append(args, "--all")
	} else {
		args = append(args, "--")
		args = append(args, paths...)
	}
	_, err := c.Run(ctx, args...)
	return err
}

func (c Client) Commit(ctx context.Context, message string) (string, error) {
	out, err := c.Run(ctx, "commit", "-m", message)
	return out, err
}

func (c Client) StagedDiffQuiet(ctx context.Context) (bool, error) {
	_, err := c.Run(ctx, "diff", "--cached", "--quiet")
	if err == nil {
		return true, nil
	}
	var ce *CommandError
	if errors.As(err, &ce) {
		if exitErr, ok := ce.Err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
	}
	return false, err
}

func (c Client) OrderedRange(ctx context.Context, baseRef string) ([]string, error) {
	out, err := c.Run(ctx, "rev-list", "--reverse", baseRef+"..HEAD")
	if err != nil {
		return nil, err
	}
	var commits []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			commits = append(commits, line)
		}
	}
	return commits, nil
}

func (c Client) CheckoutBranch(ctx context.Context, branch, startPoint string) error {
	_, err := c.Run(ctx, "checkout", "-B", branch, startPoint)
	return err
}

func (c Client) CherryPick(ctx context.Context, commits []string) error {
	if len(commits) == 0 {
		return nil
	}
	args := append([]string{"cherry-pick"}, commits...)
	_, err := c.Run(ctx, args...)
	return err
}

func (c Client) CherryPickAbort(ctx context.Context) {
	_, _ = c.Run(ctx, "cherry-pick", "--abort")
}

func (c Client) ResetHard(ctx context.Context, ref string) error {
	_, err := c.Run(ctx, "reset", "--hard", ref)
	return err
}

func (c Client) FormatPatch(ctx context.Context, baseRef string) (string, error) {
	out, err := c.Run(ctx, "format-patch", "--stdout", baseRef+"..HEAD")
	if err != nil {
		return out, err
	}
	return out, nil
}

func (c Client) ApplyMailbox(ctx context.Context, patch string) error {
	_, err := c.RunInput(ctx, patch, "am", "--3way")
	return err
}

func (c Client) AMAbort(ctx context.Context) {
	_, _ = c.Run(ctx, "am", "--abort")
}
