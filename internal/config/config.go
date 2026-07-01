package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	AppName = "nml"
)

type AgentConfig struct {
	Name          string            `yaml:"name" json:"name"`
	Model         string            `yaml:"model" json:"model"`
	ExtraArgs     []string          `yaml:"extra_args" json:"extra_args"`
	PathOverrides map[string]string `yaml:"path_overrides" json:"path_overrides"`
}

type CommandsConfig struct {
	Test   string `yaml:"test" json:"test"`
	Lint   string `yaml:"lint" json:"lint"`
	Format string `yaml:"format" json:"format"`
}

type CommitConfig struct {
	StageAllDefault bool `yaml:"stage_all_default" json:"stage_all_default"`
	AskFilePicker   bool `yaml:"ask_file_picker" json:"ask_file_picker"`
}

type ReviewConfig struct {
	Rounds                 int      `yaml:"rounds" json:"rounds"`
	Yolo                   bool     `yaml:"yolo" json:"yolo"`
	AutoApproveAfterRounds bool     `yaml:"auto_approve_after_rounds" json:"auto_approve_after_rounds"`
	IgnorePatterns         []string `yaml:"ignore_patterns" json:"ignore_patterns"`
}

type CIConfig struct {
	Timeout      string `yaml:"timeout" json:"timeout"`
	PollInterval string `yaml:"poll_interval" json:"poll_interval"`
}

type AutoMergeConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Method  string `yaml:"method" json:"method"`
}

type DocsConfig struct {
	Enabled bool     `yaml:"enabled" json:"enabled"`
	Paths   []string `yaml:"paths" json:"paths"`
}

type DeployConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Command string `yaml:"command" json:"command"`
	When    string `yaml:"when" json:"when"`
}

type CleanupConfig struct {
	Auto bool `yaml:"auto" json:"auto"`
}

type Config struct {
	Agent      AgentConfig     `yaml:"agent" json:"agent"`
	MainBranch string          `yaml:"main_branch" json:"main_branch"`
	Remote     string          `yaml:"remote" json:"remote"`
	Commands   CommandsConfig  `yaml:"commands" json:"commands"`
	Commit     CommitConfig    `yaml:"commit" json:"commit"`
	Review     ReviewConfig    `yaml:"review" json:"review"`
	CI         CIConfig        `yaml:"ci" json:"ci"`
	AutoMerge  AutoMergeConfig `yaml:"auto_merge" json:"auto_merge"`
	Docs       DocsConfig      `yaml:"docs" json:"docs"`
	Deploy     DeployConfig    `yaml:"deploy" json:"deploy"`
	Cleanup    CleanupConfig   `yaml:"cleanup" json:"cleanup"`
}

type Paths struct {
	Root       string
	GlobalPath string
	RepoPath   string
	RepoID     string
}

func Defaults() Config {
	return Config{
		Agent: AgentConfig{
			Name:      "",
			Model:     "",
			ExtraArgs: []string{},
			PathOverrides: map[string]string{
				"pi":       "",
				"opencode": "",
				"codex":    "",
				"claude":   "",
			},
		},
		MainBranch: "main",
		Remote:     "origin",
		Commands:   CommandsConfig{},
		Commit: CommitConfig{
			StageAllDefault: true,
			AskFilePicker:   false,
		},
		Review: ReviewConfig{
			Rounds: 3,
		},
		CI: CIConfig{
			Timeout:      "30m",
			PollInterval: "20s",
		},
		AutoMerge: AutoMergeConfig{Method: "squash"},
		Docs:      DocsConfig{Enabled: true},
		Deploy:    DeployConfig{Enabled: false, When: "after_ci"},
		Cleanup:   CleanupConfig{Auto: true},
	}
}

// RawConfig uses pointers so local config can intentionally set false or zero values.
type RawConfig struct {
	Agent      *RawAgentConfig     `yaml:"agent"`
	MainBranch *string             `yaml:"main_branch"`
	Remote     *string             `yaml:"remote"`
	Commands   *RawCommandsConfig  `yaml:"commands"`
	Commit     *RawCommitConfig    `yaml:"commit"`
	Review     *RawReviewConfig    `yaml:"review"`
	CI         *RawCIConfig        `yaml:"ci"`
	AutoMerge  *RawAutoMergeConfig `yaml:"auto_merge"`
	Docs       *RawDocsConfig      `yaml:"docs"`
	Deploy     *RawDeployConfig    `yaml:"deploy"`
	Cleanup    *RawCleanupConfig   `yaml:"cleanup"`
}

type RawAgentConfig struct {
	Name          *string           `yaml:"name"`
	Model         *string           `yaml:"model"`
	ExtraArgs     []string          `yaml:"extra_args"`
	PathOverrides map[string]string `yaml:"path_overrides"`
}

type RawCommandsConfig struct {
	Test   *string `yaml:"test"`
	Lint   *string `yaml:"lint"`
	Format *string `yaml:"format"`
}

type RawCommitConfig struct {
	StageAllDefault *bool `yaml:"stage_all_default"`
	AskFilePicker   *bool `yaml:"ask_file_picker"`
}

type RawReviewConfig struct {
	Rounds                 *int     `yaml:"rounds"`
	Yolo                   *bool    `yaml:"yolo"`
	AutoApproveAfterRounds *bool    `yaml:"auto_approve_after_rounds"`
	IgnorePatterns         []string `yaml:"ignore_patterns"`
}

type RawCIConfig struct {
	Timeout      *string `yaml:"timeout"`
	PollInterval *string `yaml:"poll_interval"`
}

type RawAutoMergeConfig struct {
	Enabled *bool   `yaml:"enabled"`
	Method  *string `yaml:"method"`
}

type RawDocsConfig struct {
	Enabled *bool    `yaml:"enabled"`
	Paths   []string `yaml:"paths"`
}

type RawDeployConfig struct {
	Enabled *bool   `yaml:"enabled"`
	Command *string `yaml:"command"`
	When    *string `yaml:"when"`
}

type RawCleanupConfig struct {
	Auto *bool `yaml:"auto"`
}

func ResolvePaths(repoRoot string) (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	root := filepath.Join(home, ".config", AppName)
	paths := Paths{
		Root:       root,
		GlobalPath: filepath.Join(root, "config.yaml"),
	}
	if repoRoot != "" {
		paths.RepoID = ProjectID(repoRoot)
		if paths.RepoID == "" {
			return paths, fmt.Errorf("project config requires a git repository")
		}
		paths.RepoPath = filepath.Join(root, "repos", paths.RepoID+".yaml")
	}
	return paths, nil
}

func ProjectID(repoRoot string) string {
	commonDir := gitCommonDir(repoRoot)
	if commonDir == "" {
		return ""
	}
	return hashID("git:" + commonDir)
}

func RepoID(repoRoot string) string {
	abs, err := filepath.Abs(repoRoot)
	if err == nil {
		repoRoot = abs
	}
	return hashID("path:" + filepath.Clean(repoRoot))
}

func hashID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

func gitCommonDir(repoRoot string) string {
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return ""
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(repoRoot, value)
	}
	abs, err := filepath.Abs(value)
	if err == nil {
		value = abs
	}
	if realPath, err := filepath.EvalSymlinks(value); err == nil {
		value = realPath
	}
	return filepath.Clean(value)
}

func Load(repoRoot string) (Config, Paths, error) {
	cfg := Defaults()
	paths, err := ResolvePaths(repoRoot)
	if err != nil {
		return cfg, paths, err
	}
	if err := applyFile(&cfg, paths.GlobalPath); err != nil {
		return cfg, paths, err
	}
	if paths.RepoPath != "" {
		if err := applyFile(&cfg, paths.RepoPath); err != nil {
			return cfg, paths, err
		}
	}
	return cfg, paths, nil
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func applyFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var raw RawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	Apply(cfg, raw)
	return nil
}

func Apply(cfg *Config, raw RawConfig) {
	if raw.Agent != nil {
		if raw.Agent.Name != nil {
			cfg.Agent.Name = *raw.Agent.Name
		}
		if raw.Agent.Model != nil {
			cfg.Agent.Model = *raw.Agent.Model
		}
		if raw.Agent.ExtraArgs != nil {
			cfg.Agent.ExtraArgs = append([]string(nil), raw.Agent.ExtraArgs...)
		}
		if raw.Agent.PathOverrides != nil {
			if cfg.Agent.PathOverrides == nil {
				cfg.Agent.PathOverrides = map[string]string{}
			}
			for k, v := range raw.Agent.PathOverrides {
				cfg.Agent.PathOverrides[k] = v
			}
		}
	}
	if raw.MainBranch != nil {
		cfg.MainBranch = *raw.MainBranch
	}
	if raw.Remote != nil {
		cfg.Remote = *raw.Remote
	}
	if raw.Commands != nil {
		if raw.Commands.Test != nil {
			cfg.Commands.Test = *raw.Commands.Test
		}
		if raw.Commands.Lint != nil {
			cfg.Commands.Lint = *raw.Commands.Lint
		}
		if raw.Commands.Format != nil {
			cfg.Commands.Format = *raw.Commands.Format
		}
	}
	if raw.Commit != nil {
		if raw.Commit.StageAllDefault != nil {
			cfg.Commit.StageAllDefault = *raw.Commit.StageAllDefault
		}
		if raw.Commit.AskFilePicker != nil {
			cfg.Commit.AskFilePicker = *raw.Commit.AskFilePicker
		}
	}
	if raw.Review != nil {
		if raw.Review.Rounds != nil {
			cfg.Review.Rounds = *raw.Review.Rounds
		}
		if raw.Review.Yolo != nil {
			cfg.Review.Yolo = *raw.Review.Yolo
		}
		if raw.Review.AutoApproveAfterRounds != nil {
			cfg.Review.AutoApproveAfterRounds = *raw.Review.AutoApproveAfterRounds
		}
		if raw.Review.IgnorePatterns != nil {
			cfg.Review.IgnorePatterns = append([]string(nil), raw.Review.IgnorePatterns...)
		}
	}
	if raw.CI != nil {
		if raw.CI.Timeout != nil {
			cfg.CI.Timeout = *raw.CI.Timeout
		}
		if raw.CI.PollInterval != nil {
			cfg.CI.PollInterval = *raw.CI.PollInterval
		}
	}
	if raw.AutoMerge != nil {
		if raw.AutoMerge.Enabled != nil {
			cfg.AutoMerge.Enabled = *raw.AutoMerge.Enabled
		}
		if raw.AutoMerge.Method != nil {
			cfg.AutoMerge.Method = *raw.AutoMerge.Method
		}
	}
	if raw.Docs != nil {
		if raw.Docs.Enabled != nil {
			cfg.Docs.Enabled = *raw.Docs.Enabled
		}
		if raw.Docs.Paths != nil {
			cfg.Docs.Paths = append([]string(nil), raw.Docs.Paths...)
		}
	}
	if raw.Deploy != nil {
		if raw.Deploy.Enabled != nil {
			cfg.Deploy.Enabled = *raw.Deploy.Enabled
		}
		if raw.Deploy.Command != nil {
			cfg.Deploy.Command = *raw.Deploy.Command
		}
		if raw.Deploy.When != nil {
			cfg.Deploy.When = *raw.Deploy.When
		}
	}
	if raw.Cleanup != nil {
		if raw.Cleanup.Auto != nil {
			cfg.Cleanup.Auto = *raw.Cleanup.Auto
		}
	}
}

func SaveGlobal(cfg Config) (string, error) {
	paths, err := ResolvePaths("")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(paths.GlobalPath), 0o755); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return paths.GlobalPath, os.WriteFile(paths.GlobalPath, data, 0o600)
}

func SaveRepoCommand(repoRoot, name, value string) (string, error) {
	return SaveScopedSettings(repoRoot, "project", map[string]string{"commands." + name: value})
}

func SaveScopedSettings(repoRoot, scope string, settings map[string]string) (string, error) {
	paths, err := ResolvePaths(repoRoot)
	if err != nil {
		return "", err
	}
	path := paths.GlobalPath
	if scope == "project" {
		if paths.RepoPath == "" {
			return "", fmt.Errorf("repo root is required")
		}
		path = paths.RepoPath
	} else if scope != "global" {
		return "", fmt.Errorf("scope must be global or project")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	doc := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return "", fmt.Errorf("parse %s: %w", path, err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	for key, value := range settings {
		parsed, canonical, err := parseSetting(key, value)
		if err != nil {
			return "", err
		}
		setDotted(doc, canonical, parsed)
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}

func parseSetting(key, value string) (any, string, error) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	switch key {
	case "yolo", "review.yolo":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, "", fmt.Errorf("%s must be true or false", key)
		}
		return parsed, "review.yolo", nil
	case "review.auto_approve_after_rounds", "auto_approve_after_rounds":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, "", fmt.Errorf("%s must be true or false", key)
		}
		return parsed, "review.auto_approve_after_rounds", nil
	case "auto_merge", "auto_merge.enabled":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, "", fmt.Errorf("%s must be true or false", key)
		}
		return parsed, "auto_merge.enabled", nil
	case "auto_merge.method":
		if !ValidMergeMethod(value) {
			return nil, "", fmt.Errorf("auto_merge.method must be squash, merge, or rebase")
		}
		return value, key, nil
	case "auto_cleanup", "cleanup.auto":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, "", fmt.Errorf("%s must be true or false", key)
		}
		return parsed, "cleanup.auto", nil
	case "ci.timeout":
		if _, err := time.ParseDuration(value); err != nil {
			return nil, "", fmt.Errorf("ci.timeout must be a duration like 15m: %w", err)
		}
		return value, key, nil
	case "test", "commands.test":
		return value, "commands.test", nil
	case "lint", "commands.lint":
		return value, "commands.lint", nil
	default:
		return nil, "", fmt.Errorf("unsupported setting %q", key)
	}
}

func setDotted(doc map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	current := doc
	for _, part := range parts[:len(parts)-1] {
		next, _ := current[part].(map[string]any)
		if next == nil {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func MarshalYAML(cfg Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}

func ValidMergeMethod(method string) bool {
	switch method {
	case "squash", "merge", "rebase":
		return true
	default:
		return false
	}
}
