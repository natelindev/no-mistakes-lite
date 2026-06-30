package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var Supported = []string{"pi", "opencode", "codex", "claude"}

type Found struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type Request struct {
	CWD          string
	SystemPrompt string
	Prompt       string
	Expect       ExpectMode
	Model        string
	ExtraArgs    []string
}

type Response struct {
	Text string
}

type ExpectMode string

const (
	ExpectText ExpectMode = "text"
	ExpectJSON ExpectMode = "json"
)

type Runner struct {
	Name string
	Path string
}

type RunError struct {
	Name   string
	Err    error
	Stdout string
	Stderr string
}

func (e *RunError) Error() string {
	out := strings.TrimSpace(strings.TrimSpace(e.Stdout) + "\n" + strings.TrimSpace(e.Stderr))
	if out != "" {
		return fmt.Sprintf("%s failed: %v: %s", e.Name, e.Err, out)
	}
	return fmt.Sprintf("%s failed: %v", e.Name, e.Err)
}

func Detect(pathOverrides map[string]string) []Found {
	var found []Found
	for _, name := range Supported {
		path := ""
		if pathOverrides != nil {
			path = strings.TrimSpace(pathOverrides[name])
		}
		if path == "" {
			var err error
			path, err = exec.LookPath(name)
			if err != nil {
				continue
			}
		}
		found = append(found, Found{Name: name, Path: path})
	}
	return found
}

func PickDefault(found []Found) Found {
	if len(found) == 0 {
		return Found{}
	}
	return found[0]
}

func New(name, path string) (Runner, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Runner{}, errors.New("agent name is required")
	}
	if path == "" {
		resolved, err := exec.LookPath(name)
		if err != nil {
			return Runner{}, err
		}
		path = resolved
	}
	return Runner{Name: name, Path: path}, nil
}

func (r Runner) Run(ctx context.Context, req Request) (Response, error) {
	if r.Path == "" {
		return Response{}, errors.New("agent executable path is empty")
	}
	args := invocationArgs(r.Name, req)
	cmd := exec.CommandContext(ctx, r.Path, args...)
	cmd.Dir = req.CWD
	stdin := strings.TrimSpace(req.SystemPrompt + "\n\n" + req.Prompt)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stdoutText := strings.TrimSpace(stdout.String())
		stderrText := strings.TrimSpace(stderr.String())
		if stdoutText != "" && (req.Expect == ExpectJSON || req.Expect == ExpectText) && looksUseful(stdoutText) {
			return Response{Text: stdoutText}, nil
		}
		return Response{}, &RunError{Name: r.Name, Err: err, Stdout: stdoutText, Stderr: stderrText}
	}
	return Response{Text: strings.TrimSpace(stdout.String())}, nil
}

func looksUseful(text string) bool {
	text = strings.TrimSpace(text)
	return text == "LGTM" || strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") || strings.HasPrefix(text, "- [")
}

func invocationArgs(name string, req Request) []string {
	args := []string{}
	switch name {
	case "pi":
		args = append(args, "--print", "--no-session", "--no-extensions", "--approve")
	case "claude":
		args = append(args, "--print")
	}
	args = append(args, req.ExtraArgs...)
	if req.Model != "" {
		switch name {
		case "pi", "codex", "claude", "opencode":
			args = append(args, "--model", req.Model)
		}
	}
	return args
}
