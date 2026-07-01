package treehouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type Client struct {
	Path string
	Dir  string
}

func Detect() (string, bool) {
	path, err := exec.LookPath("treehouse")
	if err != nil {
		return "", false
	}
	return path, true
}

func InstallCommand() string {
	switch runtime.GOOS {
	case "windows":
		return "irm https://kunchenguid.github.io/treehouse/install.ps1 | iex"
	case "darwin", "linux":
		return "curl -fsSL https://kunchenguid.github.io/treehouse/install.sh | sh"
	default:
		return "go install github.com/kunchenguid/treehouse@latest"
	}
}

func New(path, dir string) Client {
	if path == "" {
		path, _ = exec.LookPath("treehouse")
	}
	return Client{Path: path, Dir: dir}
}

func ManagedWorktreeRoot(repoRoot string) (string, bool) {
	repoRoot = cleanPath(repoRoot)
	if repoRoot == "" {
		return "", false
	}
	for dir := repoRoot; ; dir = filepath.Dir(dir) {
		statePath := filepath.Join(dir, "treehouse-state.json")
		data, err := os.ReadFile(statePath)
		if err == nil {
			var state struct {
				Worktrees []struct {
					Path string `json:"path"`
				} `json:"worktrees"`
			}
			if json.Unmarshal(data, &state) == nil {
				for _, wt := range state.Worktrees {
					path := cleanPath(wt.Path)
					if path != "" && samePath(path, repoRoot) {
						return path, true
					}
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return "", false
}

func (c Client) Lease(ctx context.Context, holder string) (string, error) {
	if c.Path == "" {
		return "", fmt.Errorf("treehouse is not installed; install with `%s`", InstallCommand())
	}
	out, err := c.run(ctx, "get", "--lease", "--lease-holder", holder)
	if err != nil {
		return "", err
	}
	path, err := ParseLeasePath(out)
	if err != nil {
		return "", err
	}
	return path, nil
}

func (c Client) Return(ctx context.Context, worktreePath string, force bool) error {
	if c.Path == "" {
		return fmt.Errorf("treehouse is not installed")
	}
	args := []string{"return", worktreePath}
	if force {
		args = append(args, "--force")
	}
	_, err := c.run(ctx, args...)
	return err
}

var leasedAtRE = regexp.MustCompile(`Leased worktree at\s+(.+?)(?:\.\s+Run|$)`) // handles older treehouse banners on stdout.

func ParseLeasePath(output string) (string, error) {
	output = strings.TrimSpace(stripANSI(output))
	if output == "" {
		return "", fmt.Errorf("treehouse returned an empty lease path")
	}
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") || strings.HasPrefix(line, "~/") {
			return expandHome(strings.Fields(line)[0])
		}
		if match := leasedAtRE.FindStringSubmatch(line); match != nil {
			return expandHome(match[1])
		}
	}
	if strings.HasPrefix(output, "/") || strings.HasPrefix(output, "~/") {
		return expandHome(strings.Fields(output)[0])
	}
	return "", fmt.Errorf("could not parse treehouse lease path from output: %s", output)
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

func expandHome(path string) (string, error) {
	path = strings.Trim(path, "'\"")
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return filepath.Clean(path), nil
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		path = realPath
	}
	return filepath.Clean(path)
}

func samePath(a, b string) bool {
	return cleanPath(a) == cleanPath(b)
}

func (c Client) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.Path, args...)
	if c.Dir != "" {
		cmd.Dir = c.Dir
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("treehouse %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}
