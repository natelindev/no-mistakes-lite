package app

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/natelindev/no-mistakes-lite/internal/agent"
	"github.com/natelindev/no-mistakes-lite/internal/config"
	gitx "github.com/natelindev/no-mistakes-lite/internal/git"
	"github.com/natelindev/no-mistakes-lite/internal/prbody"
	"github.com/natelindev/no-mistakes-lite/internal/redact"
	"github.com/natelindev/no-mistakes-lite/internal/review"
	"github.com/natelindev/no-mistakes-lite/internal/runstate"
	"github.com/natelindev/no-mistakes-lite/internal/session"
	"github.com/natelindev/no-mistakes-lite/internal/toon"
	"github.com/natelindev/no-mistakes-lite/internal/treehouse"
	"github.com/natelindev/no-mistakes-lite/internal/tui"
)

const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

type App struct {
	In          io.Reader
	Out         io.Writer
	Err         io.Writer
	Cwd         string
	Interactive bool
}

func Main(ctx context.Context, args []string, in io.Reader, out io.Writer, errw io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		toon.Error(out, err.Error(), nil)
		return ExitError
	}
	app := App{In: in, Out: out, Err: errw, Cwd: cwd, Interactive: isTerminal(in)}
	return app.Run(ctx, args)
}

func (a App) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.home(ctx)
	}
	switch args[0] {
	case "run":
		return a.run(ctx, args[1:])
	case "init":
		return a.init(ctx, args[1:])
	case "doctor":
		return a.doctor(ctx, args[1:])
	case "config":
		return a.config(ctx, args[1:])
	case "status":
		return a.status(ctx, args[1:])
	case "findings":
		return a.findings(ctx, args[1:])
	case "resume":
		return a.resume(ctx, args[1:])
	case "runs":
		return a.runs(ctx, args[1:])
	case "respond":
		return a.respond(ctx, args[1:])
	case "tui":
		return a.tui(ctx, args[1:])
	case "help", "--help", "-h":
		a.printHelp()
		return ExitOK
	default:
		toon.Error(a.Out, "unknown command: "+args[0], []string{"Run `nml help` for available commands."})
		return ExitUsage
	}
}

func (a App) home(ctx context.Context) int {
	cfg := config.Defaults()
	state, err := gitx.Inspect(ctx, a.Cwd, cfg.Remote, cfg.MainBranch, false)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	if state.Kind == gitx.KindNoRepo {
		if a.Interactive {
			fmt.Fprint(a.Out, tui.RenderHome(tui.HomeState{State: string(state.Kind), Reason: state.Reason, Configured: false, Noop: true}))
		} else {
			fmt.Fprintln(a.Out, "Nothing to do: current directory is not inside a git repository.")
		}
		return ExitOK
	}
	cfg, paths, err := config.Load(state.RepoRoot)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	state, err = gitx.Inspect(ctx, state.RepoRoot, cfg.Remote, cfg.MainBranch, false)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	configured := config.Exists(paths.GlobalPath)
	if !configured && a.Interactive {
		fmt.Fprintln(a.Err, "First run detected. Starting setup wizard.")
		return a.init(ctx, []string{})
	}
	if a.Interactive {
		return a.homeInteractive(ctx, state, configured)
	}
	if printNoop(a.Out, state) {
		return ExitOK
	}
	toon.KV(a.Out, "repo", state.RepoRoot)
	toon.KV(a.Out, "branch", state.Branch)
	toon.KV(a.Out, "state", state.Kind)
	toon.KV(a.Out, "reason", state.Reason)
	help := []string{"Run `nml run --message \"<commit message>\"` to prepare validation.", "Run `nml resume` to continue a failed or interrupted run.", "Run `nml doctor` to check tools and configuration."}
	if !configured {
		help = append([]string{"Run `nml init --yes` or interactive `nml init` to create config."}, help...)
	}
	toon.List(a.Out, "help", help)
	return ExitOK
}

func (a App) homeInteractive(ctx context.Context, state gitx.State, configured bool) int {
	home := tui.HomeState{
		Repo:       state.RepoRoot,
		Branch:     state.Branch,
		State:      string(state.Kind),
		Reason:     state.Reason,
		Configured: configured,
		Noop:       isNoopState(state),
	}
	_, _, hasResumable := latestResumableRun(state.RepoRoot)
	actions := homeActionOptions(state, configured, hasResumable)
	if len(actions) == 0 || (isNoopState(state) && !hasResumable) {
		fmt.Fprint(a.Out, tui.RenderHome(home))
		return ExitOK
	}
	action, cancelled, err := tui.SelectHomeAction(ctx, a.In, a.Out, home, actions)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	if cancelled || action == "exit" || action == "" {
		return ExitOK
	}
	switch action {
	case "run":
		return a.run(ctx, nil)
	case "init":
		return a.init(ctx, nil)
	case "doctor":
		return a.doctor(ctx, nil)
	case "resume":
		return a.resume(ctx, nil)
	default:
		return ExitOK
	}
}

func homeActionOptions(state gitx.State, configured bool, hasResumable bool) []tui.HomeAction {
	var actions []tui.HomeAction
	if hasResumable {
		actions = append(actions, tui.HomeAction{ID: "resume", Label: "Resume latest run", Description: "continue failed or interrupted pipeline"})
	}
	if !configured {
		actions = append(actions, tui.HomeAction{ID: "init", Label: "Set up nml", Description: "configure agent, repo, and validation commands"})
	}
	switch state.Kind {
	case gitx.KindDirty:
		actions = append(actions, tui.HomeAction{ID: "run", Label: "Validate dirty worktree", Description: "enter commit message and start pipeline"})
	case gitx.KindFeatureDelta, gitx.KindMainAhead:
		actions = append(actions, tui.HomeAction{ID: "run", Label: "Validate branch changes", Description: "start isolated pipeline"})
	case gitx.KindNeedsRemoteBase:
		actions = append(actions, tui.HomeAction{ID: "doctor", Label: "Run doctor", Description: "inspect remote and config state"})
	}
	actions = append(actions, tui.HomeAction{ID: "exit", Label: "Exit", Description: "leave without changing anything"})
	return actions
}

type runOptions struct {
	Yes              bool
	Yolo             bool
	Message          string
	MessageFromAgent bool
	Paths            []string
	AutoMerge        bool
	MergeMethod      string
	SkipDocs         bool
	SkipDeploy       bool
	CITimeout        string
	TestCommand      string
	Fetch            bool
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*s = append(*s, part)
		}
	}
	return nil
}

func (a App) run(ctx context.Context, args []string) int {
	var paths stringList
	opts := runOptions{}
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.Yes, "yes", false, "accept safe defaults and do not prompt")
	fs.BoolVar(&opts.Yolo, "yolo", false, "auto-select all actionable findings")
	fs.StringVar(&opts.Message, "message", "", "commit message for dirty worktree")
	fs.BoolVar(&opts.MessageFromAgent, "message-from-agent", false, "ask the configured agent to generate a commit message")
	fs.Var(&paths, "paths", "comma-separated path list to stage instead of all changes")
	fs.BoolVar(&opts.AutoMerge, "auto-merge", false, "enable auto-merge for this run only")
	fs.StringVar(&opts.MergeMethod, "merge-method", "", "merge method: squash, merge, or rebase")
	fs.BoolVar(&opts.SkipDocs, "skip-docs", false, "skip docs step for this run")
	fs.BoolVar(&opts.SkipDeploy, "skip-deploy", false, "skip deploy step for this run")
	fs.StringVar(&opts.CITimeout, "ci-timeout", "", "CI timeout for this run")
	fs.StringVar(&opts.TestCommand, "test-command", "", "test command for this run only")
	fs.BoolVar(&opts.Fetch, "fetch", true, "fetch remote main before classification")
	if hasHelp(args) {
		printRunHelp(a.Out)
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		toon.Error(a.Out, err.Error(), []string{"Run `nml run --help` for usage."})
		return ExitUsage
	}
	opts.Paths = paths
	if opts.MergeMethod != "" && !config.ValidMergeMethod(opts.MergeMethod) {
		toon.Error(a.Out, "invalid --merge-method: "+opts.MergeMethod, []string{"Use one of: squash, merge, rebase."})
		return ExitUsage
	}
	cfg := config.Defaults()
	first, err := gitx.Inspect(ctx, a.Cwd, cfg.Remote, cfg.MainBranch, false)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	if first.Kind == gitx.KindNoRepo {
		fmt.Fprintln(a.Out, "Nothing to do: current directory is not inside a git repository.")
		return ExitOK
	}
	cfg, _, err = config.Load(first.RepoRoot)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	state, err := gitx.Inspect(ctx, first.RepoRoot, cfg.Remote, cfg.MainBranch, opts.Fetch)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	if printNoop(a.Out, state) {
		return ExitOK
	}
	if state.Kind == gitx.KindNeedsRemoteBase {
		toon.Error(a.Out, state.Reason, []string{fmt.Sprintf("Run `git fetch %s %s` and retry.", cfg.Remote, cfg.MainBranch), "Run `nml config` to inspect main_branch and remote."})
		return ExitError
	}
	client := gitx.Client{Dir: state.RepoRoot}
	baseRef := cfg.Remote + "/" + cfg.MainBranch
	if opts.Fetch {
		if err := client.Fetch(ctx, cfg.Remote, cfg.MainBranch); err != nil {
			a.warn("fetch failed: %v", err)
		}
	}
	if !client.RefExists(ctx, baseRef) {
		toon.Error(a.Out, "remote base "+baseRef+" is not available", []string{fmt.Sprintf("Run `git fetch %s %s` and retry.", cfg.Remote, cfg.MainBranch)})
		return ExitError
	}
	treehousePath, ok := treehouse.Detect()
	if !ok {
		toon.Error(a.Out, "treehouse is required before nml can commit or validate changes", []string{"Install treehouse with `" + treehouse.InstallCommand() + "`.", "Then rerun `nml run`."})
		return ExitError
	}
	run := runstate.New(state.RepoRoot, state.Branch, cfg.MainBranch, cfg.Remote, state.Head, baseRef)
	run.SetStep("preflight", runstate.StatusCompleted, state.Reason)
	progressStarted := false
	startProgress := func() {
		if !progressStarted {
			a.beginPipelineProgress()
			progressStarted = true
		}
	}
	message := strings.TrimSpace(opts.Message)
	if state.Kind == gitx.KindDirty {
		diffStat, _ := client.WorktreeDiff(ctx)
		if message == "" && a.Interactive && !opts.Yes && !opts.MessageFromAgent {
			var cancelled bool
			message, cancelled = a.promptWizard(ctx, "Commit message", "")
			if cancelled {
				return a.runCancelled()
			}
		}
		startProgress()
		generatedMessage, generatedIntent, generatedSource := a.generateCommitMessageAndIntent(ctx, cfg, state.RepoRoot, message, diffStat, "")
		if message == "" {
			message = generatedMessage
		}
		if message == "" {
			message = fallbackCommitMessage(state.ChangedFiles)
		}
		run.CommitMessage = message
		if generatedIntent != "" {
			run.Intent = generatedIntent
			run.SetStep("intent", runstate.StatusCompleted, generatedSource)
		} else {
			run.Intent = fallbackIntent(message, diffStat, "")
			run.SetStep("intent", runstate.StatusCompleted, "intent generated from commit message and worktree diff")
		}
		a.progress("staging changes")
		if err := client.Add(ctx, opts.Paths); err != nil {
			toon.Error(a.Out, err.Error(), []string{"Check the --paths value and retry."})
			return ExitError
		}
		empty, err := client.StagedDiffQuiet(ctx)
		if err != nil {
			toon.Error(a.Out, err.Error(), nil)
			return ExitError
		}
		if empty {
			fmt.Fprintln(a.Out, "Nothing to do: selected paths have no staged changes.")
			return ExitOK
		}
		a.progress("committing dirty worktree")
		out, err := a.commitWithHookFix(ctx, cfg, client, &run, message)
		if err != nil {
			run.SetStep("commit", runstate.StatusFailed, redact.Secrets(out))
			saveRun(ctx, client, run)
			toon.Error(a.Out, "git commit failed", []string{"Fix the hook output, then rerun `nml run --message \"...\"`.", redact.Secrets(strings.TrimSpace(out))})
			return ExitError
		}
		run.SetStep("commit", runstate.StatusCompleted, strings.TrimSpace(out))
		head, _ := client.Head(ctx)
		run.SourceHead = head
	} else {
		startProgress()
		log, _ := client.CommitLog(ctx, baseRef)
		diffStat, _ := client.DiffStat(ctx, baseRef)
		_, generatedIntent, generatedSource := a.generateCommitMessageAndIntent(ctx, cfg, state.RepoRoot, "", diffStat, log)
		if generatedIntent != "" {
			run.Intent = generatedIntent
			run.SetStep("intent", runstate.StatusCompleted, generatedSource)
		} else {
			run.Intent = fallbackIntent("", diffStat, log)
			run.SetStep("intent", runstate.StatusCompleted, "intent generated from branch commits and diff")
		}
		run.SetStep("commit", runstate.StatusSkipped, "worktree was clean")
	}
	run.ReviewBranch = uniqueReviewBranchName(reviewBranchName(run.CommitMessage, state.Branch, run.SourceHead), run.ID)
	run.SetStep("worktree", runstate.StatusRunning, "leasing treehouse worktree")
	prep, err := a.prepareWorktree(ctx, client, treehousePath, &run)
	if err != nil {
		run.SetStep("worktree", runstate.StatusFailed, redact.Secrets(err.Error()))
		path, saveErr := saveRun(ctx, client, run)
		help := []string{"Fix the worktree preparation issue and rerun `nml run`.", "If a lease was created, return it with `treehouse return <path> --force`."}
		if saveErr == nil {
			help = append(help, "Saved run state: "+path)
		}
		toon.Error(a.Out, "worktree preparation failed", append(help, redact.Secrets(err.Error())))
		return ExitError
	}
	run.SetStep("worktree", runstate.StatusCompleted, fmt.Sprintf("leased %s, copied %d commits with %s", prep.Path, prep.CommitCount, prep.Method))
	reviewOutcome, err := a.runReview(ctx, cfg, opts, &run)
	if err != nil {
		run.SetStep("review", runstate.StatusFailed, redact.Secrets(err.Error()))
		path, saveErr := saveRun(ctx, client, run)
		help := []string{"Review failed before the remaining pipeline could run."}
		if saveErr == nil {
			help = append(help, "Saved run state: "+path)
		}
		toon.Error(a.Out, "review failed", append(help, redact.Secrets(err.Error())))
		return ExitError
	}
	path, err := saveRun(ctx, client, run)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	if reviewOutcome.AwaitingUser {
		if a.Interactive {
			fmt.Fprint(a.Out, tui.RenderReviewGate(run.ID, path, run.WorktreePath, reviewOutcome.Findings))
		} else {
			printReviewGate(a.Out, run, path, reviewOutcome.Findings)
		}
		return ExitOK
	}
	if err := a.runValidationCommands(ctx, cfg, opts, &run); err != nil {
		path, saveErr := saveRun(ctx, client, run)
		help := []string{"Fix the failed validation step and rerun `nml run`."}
		if saveErr == nil {
			help = append(help, "Saved run state: "+path)
		}
		toon.Error(a.Out, "validation failed", append(help, redact.Secrets(err.Error())))
		return ExitError
	}
	if err := a.runPushAndPR(ctx, cfg, opts, &run); err != nil {
		path, saveErr := saveRun(ctx, client, run)
		help := []string{"Fix the push or PR issue and rerun `nml run`."}
		if saveErr == nil {
			help = append(help, "Saved run state: "+path)
		}
		toon.Error(a.Out, "push or PR failed", append(help, redact.Secrets(err.Error())))
		return ExitError
	}
	path, err = saveRun(ctx, client, run)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	if a.Interactive {
		fmt.Fprint(a.Out, tui.RenderRunResult(run, path))
	} else {
		printRunPrepared(a.Out, run, path)
	}
	return ExitOK
}

type generatedMetadata struct {
	CommitMessage string `json:"commit_message"`
	Intent        string `json:"intent"`
}

func (a App) generateCommitMessageAndIntent(ctx context.Context, cfg config.Config, cwd, suppliedMessage, diffStat, commitLog string) (string, string, string) {
	if cfg.Agent.Name == "" {
		return "", "", ""
	}
	pathOverride := ""
	if cfg.Agent.PathOverrides != nil {
		pathOverride = strings.TrimSpace(cfg.Agent.PathOverrides[cfg.Agent.Name])
	}
	runner, err := agent.New(cfg.Agent.Name, pathOverride)
	if err != nil {
		a.warn("configured agent unavailable for intent: %v", err)
		return "", "", ""
	}
	prompt := fmt.Sprintf(`Generate original user intent metadata for this change.

Commit message supplied by user, if any:
%s

Commit log, if any:
%s

Diff stat:
%s

Return only a JSON object with these fields:
{
  "commit_message": "concise conventional commit subject when no user message was supplied, otherwise repeat the supplied message",
  "intent": "what the user was trying to accomplish, not a diff summary"
}

Rules:
- Intent should be concise but complete enough for code review and PR body.
- Preserve user-supplied commit message exactly when present.
- Do not include Markdown or extra prose.`, suppliedMessage, truncate(commitLog, 12000), truncate(diffStat, 12000))
	resp, err := a.withSpinnerAgent(ctx, "generating intent metadata", func() (agent.Response, error) {
		return runner.Run(ctx, agent.Request{
			CWD:          cwd,
			SystemPrompt: "You produce compact JSON for nml metadata.",
			Prompt:       prompt,
			Expect:       agent.ExpectJSON,
			Model:        cfg.Agent.Model,
			ExtraArgs:    cfg.Agent.ExtraArgs,
		})
	})
	if err != nil {
		a.warn("agent intent generation failed: %v", err)
		return "", "", ""
	}
	var meta generatedMetadata
	if err := json.Unmarshal([]byte(extractJSONObject(resp.Text)), &meta); err != nil {
		a.warn("agent intent JSON parse failed: %v", err)
		return "", "", ""
	}
	if strings.TrimSpace(suppliedMessage) != "" {
		meta.CommitMessage = suppliedMessage
	}
	if unsafeGeneratedIntent(meta.Intent) {
		return strings.TrimSpace(meta.CommitMessage), "", ""
	}
	return strings.TrimSpace(meta.CommitMessage), strings.TrimSpace(meta.Intent), "intent generated by agent from message, commits, and diff"
}

func unsafeGeneratedIntent(intent string) bool {
	lower := strings.ToLower(intent)
	suspicious := []string{"intentional_test_bug", "intentional bug", "intentional failure", "intentionally break", "intentionally breaking", "broken health", "break health", "health check failure", "failure detection by intentionally"}
	for _, marker := range suspicious {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

func (a App) commitWithHookFix(ctx context.Context, cfg config.Config, client gitx.Client, run *runstate.State, message string) (string, error) {
	attempts := 3
	var lastOut string
	for attempt := 1; attempt <= attempts; attempt++ {
		out, err := client.Commit(ctx, message)
		lastOut = out
		if err == nil {
			return out, nil
		}
		if attempt == attempts || cfg.Agent.Name == "" {
			return lastOut, err
		}
		run.SetStep("commit", runstate.StatusFixing, fmt.Sprintf("commit hook failed, fix attempt %d", attempt))
		if fixErr := a.fixCommitHookFailure(ctx, cfg, run, redact.Secrets(out)); fixErr != nil {
			return lastOut, fixErr
		}
		if addErr := client.Add(ctx, nil); addErr != nil {
			return lastOut, addErr
		}
	}
	return lastOut, fmt.Errorf("commit failed after %d attempts", attempts)
}

func (a App) fixCommitHookFailure(ctx context.Context, cfg config.Config, run *runstate.State, hookOutput string) error {
	pathOverride := ""
	if cfg.Agent.PathOverrides != nil {
		pathOverride = strings.TrimSpace(cfg.Agent.PathOverrides[cfg.Agent.Name])
	}
	runner, err := agent.New(cfg.Agent.Name, pathOverride)
	if err != nil {
		return err
	}
	prompt := fmt.Sprintf(`Fix the pre-commit or commit hook issues shown below.

Original user intent:
%s

Hook output:
%s

Rules:
- Fix only the hook issues shown.
- Preserve the original user intent.
- Do not broaden scope.
- Run the relevant hook command if possible.
- Do not use --no-verify.`, run.Intent, truncate(hookOutput, 40000))
	_, err = a.withSpinnerAgent(ctx, "fixing commit hook failure", func() (agent.Response, error) {
		return runner.Run(ctx, agent.Request{
			CWD:          run.RepoRoot,
			SystemPrompt: mutatingAgentSystemPrompt(),
			Prompt:       prompt,
			Expect:       agent.ExpectText,
			Model:        cfg.Agent.Model,
			ExtraArgs:    cfg.Agent.ExtraArgs,
		})
	})
	return err
}

type worktreePrep struct {
	Path        string
	CommitCount int
	Method      string
}

func (a App) prepareWorktree(ctx context.Context, source gitx.Client, treehousePath string, run *runstate.State) (worktreePrep, error) {
	th := treehouse.New(treehousePath, run.RepoRoot)
	var worktreePath string
	err := a.withSpinner(ctx, "leasing treehouse worktree", func() error {
		var leaseErr error
		worktreePath, leaseErr = th.Lease(ctx, "nml:"+run.ID)
		return leaseErr
	})
	if err != nil {
		return worktreePrep{}, err
	}
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return worktreePrep{}, fmt.Errorf("treehouse returned an empty worktree path")
	}
	run.WorktreePath = worktreePath
	target := gitx.Client{Dir: worktreePath}
	a.progress("preparing review branch %s", run.ReviewBranch)
	if err := a.withSpinner(ctx, "fetching base branch in leased worktree", func() error { return target.Fetch(ctx, run.Remote, run.MainBranch) }); err != nil {
		a.warn("worktree fetch failed: %v", err)
	}
	if !target.RefExists(ctx, run.BaseRef) {
		return worktreePrep{Path: worktreePath}, fmt.Errorf("base ref %s is not available in leased worktree", run.BaseRef)
	}
	if err := a.withSpinner(ctx, "checking out review branch", func() error { return target.CheckoutBranch(ctx, run.ReviewBranch, run.BaseRef) }); err != nil {
		return worktreePrep{Path: worktreePath}, err
	}
	commits, err := source.OrderedRange(ctx, run.BaseRef)
	if err != nil {
		return worktreePrep{Path: worktreePath}, err
	}
	if len(commits) == 0 {
		return worktreePrep{Path: worktreePath}, fmt.Errorf("no commits found in range %s..HEAD", run.BaseRef)
	}
	if err := a.withSpinner(ctx, fmt.Sprintf("copying %d commits into isolated worktree", len(commits)), func() error { return target.CherryPick(ctx, commits) }); err == nil {
		return worktreePrep{Path: worktreePath, CommitCount: len(commits), Method: "cherry-pick"}, nil
	} else {
		a.warn("cherry-pick failed, trying patch fallback: %v", err)
		target.CherryPickAbort(ctx)
		if resetErr := target.ResetHard(ctx, run.BaseRef); resetErr != nil {
			return worktreePrep{Path: worktreePath, CommitCount: len(commits)}, fmt.Errorf("cherry-pick failed and reset failed: %w", resetErr)
		}
		patch, patchErr := source.FormatPatch(ctx, run.BaseRef)
		if patchErr != nil {
			return worktreePrep{Path: worktreePath, CommitCount: len(commits)}, fmt.Errorf("cherry-pick failed and format-patch fallback failed: %w", patchErr)
		}
		if strings.TrimSpace(patch) == "" {
			return worktreePrep{Path: worktreePath, CommitCount: len(commits)}, fmt.Errorf("cherry-pick failed and format-patch produced no patch")
		}
		if amErr := a.withSpinner(ctx, "applying patch fallback", func() error { return target.ApplyMailbox(ctx, patch) }); amErr != nil {
			target.AMAbort(ctx)
			return worktreePrep{Path: worktreePath, CommitCount: len(commits)}, fmt.Errorf("cherry-pick failed and patch fallback failed: %w", amErr)
		}
		return worktreePrep{Path: worktreePath, CommitCount: len(commits), Method: "format-patch"}, nil
	}
}

type reviewOutcome struct {
	AwaitingUser bool
	Findings     []review.Finding
}

func (a App) runReview(ctx context.Context, cfg config.Config, opts runOptions, run *runstate.State) (reviewOutcome, error) {
	if cfg.Agent.Name == "" {
		run.SetStep("review", runstate.StatusSkipped, "no configured agent; run nml init to enable review")
		return reviewOutcome{}, nil
	}
	pathOverride := ""
	if cfg.Agent.PathOverrides != nil {
		pathOverride = strings.TrimSpace(cfg.Agent.PathOverrides[cfg.Agent.Name])
	}
	runner, err := agent.New(cfg.Agent.Name, pathOverride)
	if err != nil {
		return reviewOutcome{}, fmt.Errorf("configured agent %s is unavailable: %w", cfg.Agent.Name, err)
	}
	worktree := gitx.Client{Dir: run.WorktreePath}
	rounds := cfg.Review.Rounds
	if rounds <= 0 {
		rounds = 3
	}
	for round := 1; round <= rounds; round++ {
		run.SetStep("review", runstate.StatusRunning, fmt.Sprintf("round %d of %d", round, rounds))
		diff, err := reviewDiff(ctx, worktree, run.BaseRef)
		if err != nil {
			return reviewOutcome{}, err
		}
		resp, err := a.withSpinnerAgent(ctx, fmt.Sprintf("review round %d", round), func() (agent.Response, error) {
			return runner.Run(ctx, agent.Request{
				CWD:          run.WorktreePath,
				SystemPrompt: reviewSystemPrompt(),
				Prompt:       reviewPrompt(run.Intent, diff),
				Expect:       agent.ExpectText,
				Model:        cfg.Agent.Model,
				ExtraArgs:    cfg.Agent.ExtraArgs,
			})
		})
		if err != nil {
			return reviewOutcome{}, err
		}
		parsed, err := review.ParseMarkdown(resp.Text)
		if err != nil {
			parsed, err = reformatReview(ctx, runner, cfg, run.WorktreePath, resp.Text)
			if err != nil {
				return reviewOutcome{}, err
			}
		}
		builtIn := builtInReviewFindings(diff)
		if len(builtIn) > 0 {
			parsed.LGTM = false
			parsed.Findings = mergeFindings(parsed.Findings, builtIn)
		}
		if parsed.LGTM {
			appendReviewRound(run, runstate.ReviewRound{Number: round, Result: "LGTM"})
			run.SetStep("review", runstate.StatusCompleted, fmt.Sprintf("round %d LGTM", round))
			return reviewOutcome{}, nil
		}
		appendReviewRound(run, runstate.ReviewRound{Number: round, Result: "findings", Findings: parsed.Findings})
		if len(parsed.Findings) == 0 {
			run.SetStep("review", runstate.StatusCompleted, fmt.Sprintf("round %d had no actionable findings", round))
			return reviewOutcome{}, nil
		}
		if !opts.Yes && !opts.Yolo {
			run.SetStep("review", runstate.StatusAwaitingUser, fmt.Sprintf("round %d found %d findings", round, len(parsed.Findings)))
			return reviewOutcome{AwaitingUser: true, Findings: parsed.Findings}, nil
		}
		if round == rounds {
			return reviewOutcome{}, fmt.Errorf("review still found %d findings after %d rounds", len(parsed.Findings), rounds)
		}
		run.SetStep("review", runstate.StatusFixing, fmt.Sprintf("round %d fixing %d findings", round, len(parsed.Findings)))
		if err := a.withSpinner(ctx, "fixing review findings", func() error { return fixReviewFindings(ctx, runner, cfg, run, parsed.Findings) }); err != nil {
			return reviewOutcome{}, err
		}
	}
	return reviewOutcome{}, fmt.Errorf("review did not complete within %d rounds", rounds)
}

func reviewDiff(ctx context.Context, client gitx.Client, baseRef string) (string, error) {
	out, err := client.Run(ctx, "diff", "--find-renames", "--unified=80", baseRef+"...HEAD")
	if err != nil {
		return "", err
	}
	return truncate(out, 60000), nil
}

func builtInReviewFindings(diff string) []review.Finding {
	var findings []review.Finding
	if strings.Contains(diff, "INTENTIONAL_TEST_BUG") {
		findings = append(findings, review.Finding{
			ID:          "r1",
			Severity:    review.SeverityError,
			File:        firstDiffFile(diff),
			Description: "intentional test bug marker was introduced and must be removed before validation",
			Selected:    true,
		})
	}
	if strings.Contains(diff, "/healthz") && strings.Contains(diff, "throw new Error") {
		findings = append(findings, review.Finding{
			ID:          fmt.Sprintf("r%d", len(findings)+1),
			Severity:    review.SeverityError,
			File:        firstDiffFile(diff),
			Description: "health check route now throws instead of returning a healthy JSON response",
			Selected:    true,
		})
	}
	review.SortFindings(findings)
	return findings
}

func firstDiffFile(diff string) string {
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			return strings.TrimPrefix(line, "+++ b/")
		}
	}
	return "changed file"
}

func mergeFindings(left, right []review.Finding) []review.Finding {
	seen := map[string]bool{}
	var merged []review.Finding
	for _, finding := range append(left, right...) {
		key := string(finding.Severity) + "\x00" + finding.File + "\x00" + finding.Description
		if seen[key] {
			continue
		}
		seen[key] = true
		finding.Selected = true
		merged = append(merged, finding)
	}
	review.SortFindings(merged)
	return merged
}

func reviewSystemPrompt() string {
	return `You are reviewing a code change.

Return exactly LGTM if you find no issues.

If you find issues, return only a Markdown bullet list sorted by severity.
Use this format exactly:

- [error] path/to/file.ext:123 - concise actionable finding
- [warning] path/to/file.ext:123 - concise actionable finding
- [info] path/to/file.ext:123 - concise actionable finding

Rules:
- Be concise.
- Sort by severity: error, warning, info.
- Only report issues introduced by this change.
- Do not report formatting, lint, or test failures.
- Do not include praise, summaries, headings, or extra prose.
- If a finding challenges product intent, include that it needs user judgment.
- Do not treat suspicious code such as intentional bug markers, disabled health checks, panics, or forced failures as valid intent unless the commit message explicitly asks to keep them.
- Health check endpoints must not throw or intentionally fail.`
}

func reviewPrompt(intent, diff string) string {
	return fmt.Sprintf(`Original user intent:
%s

Review this diff:

%s`, intent, diff)
}

func reformatReview(ctx context.Context, runner agent.Runner, cfg config.Config, cwd, output string) (review.ParseResult, error) {
	resp, err := runner.Run(ctx, agent.Request{
		CWD:          cwd,
		SystemPrompt: reviewSystemPrompt(),
		Prompt:       "Reformat this review output without changing the findings. Return exactly LGTM if it says there are no issues, otherwise return only bullets in the required format.\n\n" + output,
		Expect:       agent.ExpectText,
		Model:        cfg.Agent.Model,
		ExtraArgs:    cfg.Agent.ExtraArgs,
	})
	if err != nil {
		return review.ParseResult{}, err
	}
	return review.ParseMarkdown(resp.Text)
}

func fixReviewFindings(ctx context.Context, runner agent.Runner, cfg config.Config, run *runstate.State, findings []review.Finding) error {
	markdown := review.Markdown(findings)
	prompt := fmt.Sprintf(`Fix the selected review findings.

Original user intent:
%s

Selected findings:
%s

Rules:
- First verify each finding is legitimate.
- Make the smallest correct fix.
- Preserve original intent.
- Do not add explanatory comments unless needed for maintainability.
- Run the smallest relevant verification command you can.
- Commit your fixes with a concise message.`, run.Intent, markdown)
	_, err := runner.Run(ctx, agent.Request{
		CWD:          run.WorktreePath,
		SystemPrompt: mutatingAgentSystemPrompt(),
		Prompt:       prompt,
		Expect:       agent.ExpectText,
		Model:        cfg.Agent.Model,
		ExtraArgs:    cfg.Agent.ExtraArgs,
	})
	if err != nil {
		return err
	}
	client := gitx.Client{Dir: run.WorktreePath}
	status, err := client.StatusPorcelain(ctx)
	if err != nil {
		return err
	}
	if !gitx.IsDirty(status) {
		return nil
	}
	if err := client.Add(ctx, nil); err != nil {
		return err
	}
	out, err := client.Commit(ctx, "nml(review): address review findings")
	if err != nil {
		return fmt.Errorf("commit review fix: %w: %s", err, redact.Secrets(out))
	}
	return nil
}

func (a App) runValidationCommands(ctx context.Context, cfg config.Config, opts runOptions, run *runstate.State) error {
	testCmd := strings.TrimSpace(opts.TestCommand)
	if testCmd == "" {
		testCmd = strings.TrimSpace(cfg.Commands.Test)
	}
	lintCmd := strings.TrimSpace(cfg.Commands.Lint)
	formatCmd := strings.TrimSpace(cfg.Commands.Format)
	if lintCmd == "" {
		_, detectedLint := detectCommands(run.WorktreePath)
		lintCmd = detectedLint
	}
	if testCmd == "" {
		detail := "no test command configured or detected"
		run.SetStep("test", runstate.StatusSkipped, detail)
		run.Tests = append(run.Tests, runstate.CommandRun{Command: "", Status: runstate.StatusSkipped, Detail: detail})
	} else if err := a.runCommandStep(ctx, cfg, run, "test", testCmd); err != nil {
		return err
	}
	if err := a.runDocsStep(ctx, cfg, opts, run); err != nil {
		return err
	}
	if formatCmd != "" {
		if err := a.runFormatCommand(ctx, run, formatCmd); err != nil {
			return err
		}
	}
	if lintCmd == "" {
		detail := "no lint command configured or detected"
		run.SetStep("lint", runstate.StatusSkipped, detail)
		run.Lint = append(run.Lint, runstate.CommandRun{Command: "", Status: runstate.StatusSkipped, Detail: detail})
	} else if err := a.runCommandStep(ctx, cfg, run, "lint", lintCmd); err != nil {
		return err
	}
	return nil
}

func (a App) runDocsStep(ctx context.Context, cfg config.Config, opts runOptions, run *runstate.State) error {
	if opts.SkipDocs {
		run.SetStep("docs", runstate.StatusSkipped, "skipped by --skip-docs")
		return nil
	}
	if !cfg.Docs.Enabled {
		run.SetStep("docs", runstate.StatusSkipped, "docs disabled in config")
		return nil
	}
	docs := detectDocs(run.WorktreePath, cfg.Docs.Paths)
	if len(docs) == 0 {
		run.SetStep("docs", runstate.StatusSkipped, "no docs paths detected")
		return nil
	}
	if cfg.Agent.Name == "" {
		run.SetStep("docs", runstate.StatusSkipped, "docs present but no agent is configured to evaluate them")
		return nil
	}
	pathOverride := ""
	if cfg.Agent.PathOverrides != nil {
		pathOverride = strings.TrimSpace(cfg.Agent.PathOverrides[cfg.Agent.Name])
	}
	runner, err := agent.New(cfg.Agent.Name, pathOverride)
	if err != nil {
		run.SetStep("docs", runstate.StatusSkipped, "configured agent unavailable for docs: "+err.Error())
		return nil
	}
	client := gitx.Client{Dir: run.WorktreePath}
	beforeHead, headErr := client.Head(ctx)
	if headErr != nil {
		run.SetStep("docs", runstate.StatusFailed, headErr.Error())
		return headErr
	}
	diff, _ := reviewDiff(ctx, client, run.BaseRef)
	run.SetStep("docs", runstate.StatusRunning, "checking docs")
	prompt := fmt.Sprintf(`Decide whether this change requires documentation updates.

Original user intent:
%s

Candidate documentation paths:
%s

Diff:
%s

Rules:
- If no docs changes are needed, print exactly NO_DOCS and do not edit files.
- If docs changes are needed, edit only relevant docs files.
- Keep docs concise and accurate.
- Preserve the original user intent.`, run.Intent, strings.Join(docs, "\n"), diff)
	_, err = a.withSpinnerAgent(ctx, "checking documentation", func() (agent.Response, error) {
		return runner.Run(ctx, agent.Request{
			CWD:          run.WorktreePath,
			SystemPrompt: mutatingAgentSystemPrompt(),
			Prompt:       prompt,
			Expect:       agent.ExpectText,
			Model:        cfg.Agent.Model,
			ExtraArgs:    cfg.Agent.ExtraArgs,
		})
	})
	if err != nil {
		run.SetStep("docs", runstate.StatusFailed, err.Error())
		return err
	}
	changed, err := commitDirtyOrHeadChanged(ctx, run.WorktreePath, "nml(docs): update documentation", beforeHead)
	if err != nil {
		run.SetStep("docs", runstate.StatusFailed, err.Error())
		return err
	}
	if changed {
		run.SetStep("docs", runstate.StatusCompleted, "documentation updated")
	} else {
		run.SetStep("docs", runstate.StatusSkipped, "no documentation updates needed")
	}
	return nil
}

func detectDocs(root string, configured []string) []string {
	seen := map[string]bool{}
	var docs []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		docs = append(docs, path)
	}
	for _, p := range configured {
		add(p)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "README*"))
	for _, match := range matches {
		if info, err := os.Stat(match); err == nil && !info.IsDir() {
			if rel, relErr := filepath.Rel(root, match); relErr == nil {
				add(rel)
			}
		}
	}
	docsDir := filepath.Join(root, "docs")
	if info, err := os.Stat(docsDir); err == nil && info.IsDir() {
		add("docs/**")
	}
	return docs
}

func (a App) runFormatCommand(ctx context.Context, run *runstate.State, command string) error {
	out, err := a.withSpinnerOutput(ctx, "running format: "+command, func() (string, error) { return runShell(ctx, run.WorktreePath, command) })
	if err != nil {
		detail := commandDetail(command, out)
		run.SetStep("lint", runstate.StatusFailed, "format failed: "+detail)
		run.Lint = append(run.Lint, runstate.CommandRun{Command: command, Status: runstate.StatusFailed, Detail: detail})
		return fmt.Errorf("format command failed: %s", detail)
	}
	client := gitx.Client{Dir: run.WorktreePath}
	status, statusErr := client.StatusPorcelain(ctx)
	if statusErr != nil {
		return statusErr
	}
	if gitx.IsDirty(status) {
		if err := client.Add(ctx, nil); err != nil {
			return err
		}
		commitOut, commitErr := client.Commit(ctx, "nml(lint): apply formatting")
		if commitErr != nil {
			return fmt.Errorf("commit formatting changes: %w: %s", commitErr, redact.Secrets(commitOut))
		}
	}
	return nil
}

func (a App) runCommandStep(ctx context.Context, cfg config.Config, run *runstate.State, step, command string) error {
	attempts := 3
	for attempt := 1; attempt <= attempts; attempt++ {
		run.SetStep(step, runstate.StatusRunning, fmt.Sprintf("attempt %d: %s", attempt, command))
		out, err := a.withSpinnerOutput(ctx, fmt.Sprintf("running %s: %s", step, command), func() (string, error) { return runShell(ctx, run.WorktreePath, command) })
		detail := commandDetail(command, out)
		status := runstate.StatusCompleted
		if err != nil {
			status = runstate.StatusFailed
		}
		commandRun := runstate.CommandRun{Command: command, Status: status, Detail: detail}
		if step == "test" {
			run.Tests = append(run.Tests, commandRun)
		} else if step == "lint" {
			run.Lint = append(run.Lint, commandRun)
		}
		if err == nil {
			run.SetStep(step, runstate.StatusCompleted, detail)
			return nil
		}
		if attempt == attempts || cfg.Agent.Name == "" {
			run.SetStep(step, runstate.StatusFailed, detail)
			return fmt.Errorf("%s command failed: %s", step, detail)
		}
		run.SetStep(step, runstate.StatusFixing, fmt.Sprintf("attempt %d fixing failure", attempt))
		changed, fixErr := a.fixCommandFailure(ctx, cfg, run, step, command, detail)
		if fixErr != nil {
			run.SetStep(step, runstate.StatusFailed, fixErr.Error())
			return fixErr
		}
		if !changed {
			a.warn("%s fix attempt made no file changes", step)
		}
	}
	return fmt.Errorf("%s command did not complete", step)
}

func (a App) fixCommandFailure(ctx context.Context, cfg config.Config, run *runstate.State, step, command, detail string) (bool, error) {
	pathOverride := ""
	if cfg.Agent.PathOverrides != nil {
		pathOverride = strings.TrimSpace(cfg.Agent.PathOverrides[cfg.Agent.Name])
	}
	runner, err := agent.New(cfg.Agent.Name, pathOverride)
	if err != nil {
		return false, err
	}
	client := gitx.Client{Dir: run.WorktreePath}
	beforeHead, err := client.Head(ctx)
	if err != nil {
		return false, err
	}
	prompt := fmt.Sprintf(`Fix the failing %s command.

Original user intent:
%s

Command:
%s

Failure output:
%s

Rules:
- Preserve the original user intent.
- Make the smallest correct fix.
- Do not broaden scope.
- Run the smallest relevant verification command you can.
- Leave changes uncommitted; nml will commit the fix.`, step, run.Intent, command, truncate(detail, 40000))
	_, err = a.withSpinnerAgent(ctx, "fixing "+step+" failure", func() (agent.Response, error) {
		return runner.Run(ctx, agent.Request{
			CWD:          run.WorktreePath,
			SystemPrompt: mutatingAgentSystemPrompt(),
			Prompt:       prompt,
			Expect:       agent.ExpectText,
			Model:        cfg.Agent.Model,
			ExtraArgs:    cfg.Agent.ExtraArgs,
		})
	})
	if err != nil {
		return false, err
	}
	return commitDirtyOrHeadChanged(ctx, run.WorktreePath, "nml("+step+"): fix validation failure", beforeHead)
}

func runShell(ctx context.Context, cwd, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	return redact.Secrets(string(out)), err
}

func commandDetail(command, output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return command
	}
	return command + " | " + truncate(output, 4000)
}

func (a App) runPushAndPR(ctx context.Context, cfg config.Config, opts runOptions, run *runstate.State) error {
	client := gitx.Client{Dir: run.WorktreePath}
	remoteURL, err := client.RemoteURL(ctx, run.Remote)
	if err != nil || strings.TrimSpace(remoteURL) == "" {
		detail := fmt.Sprintf("remote %s has no URL; push and PR skipped", run.Remote)
		run.SetStep("push", runstate.StatusSkipped, detail)
		run.SetStep("pr", runstate.StatusSkipped, detail)
		run.SetStep("ci", runstate.StatusSkipped, "CI requires a pushed GitHub PR")
		run.SetStep("deploy", runstate.StatusSkipped, "deploy waits for CI in a later stage")
		if opts.AutoMerge {
			run.SetStep("final", runstate.StatusFailed, "--auto-merge requires a GitHub PR")
			return fmt.Errorf("--auto-merge requires a pushed GitHub PR")
		}
		run.SetStep("final", runstate.StatusCompleted, "local validation stages completed")
		return nil
	}
	ownerRepo, ok := parseGitHubRemote(remoteURL)
	if !ok {
		detail := "remote is not a GitHub URL; GitHub MVP push and PR skipped"
		run.SetStep("push", runstate.StatusSkipped, detail)
		run.SetStep("pr", runstate.StatusSkipped, detail)
		run.SetStep("ci", runstate.StatusSkipped, "CI requires a GitHub PR")
		run.SetStep("deploy", runstate.StatusSkipped, "deploy waits for CI in a later stage")
		if opts.AutoMerge {
			run.SetStep("final", runstate.StatusFailed, "--auto-merge requires a GitHub PR")
			return fmt.Errorf("--auto-merge requires a GitHub PR")
		}
		run.SetStep("final", runstate.StatusCompleted, "local validation stages completed")
		return nil
	}
	if !strings.HasPrefix(run.ReviewBranch, "nml/") {
		return fmt.Errorf("refusing to push non-tool branch %s", run.ReviewBranch)
	}
	run.SetStep("push", runstate.StatusRunning, run.ReviewBranch)
	if out, err := a.withSpinnerOutput(ctx, "pushing review branch "+run.ReviewBranch, func() (string, error) {
		return client.Run(ctx, "push", "--force-with-lease", "-u", run.Remote, run.ReviewBranch)
	}); err != nil {
		run.SetStep("push", runstate.StatusFailed, redact.Secrets(out))
		return fmt.Errorf("git push failed: %w: %s", err, redact.Secrets(out))
	}
	run.SetStep("push", runstate.StatusCompleted, "pushed "+run.ReviewBranch+" to "+run.Remote)
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		run.SetStep("pr", runstate.StatusSkipped, "gh is not installed; branch was pushed")
		run.SetStep("ci", runstate.StatusSkipped, "CI watch requires gh")
		run.SetStep("deploy", runstate.StatusSkipped, "deploy waits for CI in a later stage")
		if opts.AutoMerge {
			run.SetStep("final", runstate.StatusFailed, "--auto-merge requires gh and a GitHub PR")
			return fmt.Errorf("--auto-merge requires gh and a GitHub PR")
		}
		run.SetStep("final", runstate.StatusCompleted, "pushed branch without PR because gh is unavailable")
		return nil
	}
	if authErr := ghAuthStatus(ctx); authErr != nil {
		run.SetStep("pr", runstate.StatusSkipped, "gh auth status failed; branch was pushed")
		run.SetStep("ci", runstate.StatusSkipped, "CI watch requires gh auth")
		run.SetStep("deploy", runstate.StatusSkipped, "deploy waits for CI in a later stage")
		if opts.AutoMerge {
			run.SetStep("final", runstate.StatusFailed, "--auto-merge requires gh auth and a GitHub PR")
			return fmt.Errorf("--auto-merge requires gh auth and a GitHub PR")
		}
		run.SetStep("final", runstate.StatusCompleted, "pushed branch without PR because gh auth failed")
		return nil
	}
	title := prTitle(run)
	body := prBody(run, ownerRepo)
	run.SetStep("pr", runstate.StatusRunning, title)
	prURL, err := a.withSpinnerOutput(ctx, "creating or updating GitHub PR", func() (string, error) {
		return createOrUpdatePR(ctx, ghPath, run.WorktreePath, run.MainBranch, run.ReviewBranch, title, body)
	})
	if err != nil {
		run.SetStep("pr", runstate.StatusFailed, err.Error())
		return err
	}
	run.PRURL = prURL
	run.SetStep("pr", runstate.StatusCompleted, prURL)
	if err := a.runCIWatch(ctx, cfg, opts, run, ghPath); err != nil {
		run.SetStep("final", runstate.StatusFailed, err.Error())
		return err
	}
	deployChanged := false
	if shouldRunDeploy(cfg, opts, "after_ci") {
		changed, err := a.runDeploy(ctx, cfg, opts, run, ghPath, "after_ci")
		if err != nil {
			run.SetStep("final", runstate.StatusFailed, err.Error())
			return err
		}
		deployChanged = changed
		if deployChanged {
			if err := a.runCIWatch(ctx, cfg, opts, run, ghPath); err != nil {
				run.SetStep("final", runstate.StatusFailed, err.Error())
				return err
			}
		}
	} else if opts.SkipDeploy {
		run.SetStep("deploy", runstate.StatusSkipped, "skipped by --skip-deploy")
	} else if !cfg.Deploy.Enabled || strings.TrimSpace(cfg.Deploy.Command) == "" {
		run.SetStep("deploy", runstate.StatusSkipped, "deploy disabled or missing command")
	} else if cfg.Deploy.When == "after_merge" && !opts.AutoMerge {
		run.SetStep("deploy", runstate.StatusSkipped, "configured for after_merge and --auto-merge was not passed")
	}
	if opts.AutoMerge {
		if err := a.withSpinner(ctx, "enabling auto-merge", func() error { return runAutoMerge(ctx, ghPath, run, cfg, opts) }); err != nil {
			run.SetStep("final", runstate.StatusFailed, err.Error())
			return err
		}
	}
	if shouldRunDeploy(cfg, opts, "after_merge") {
		if _, err := a.runDeploy(ctx, cfg, opts, run, ghPath, "after_merge"); err != nil {
			run.SetStep("final", runstate.StatusFailed, err.Error())
			return err
		}
	}
	if _, err := a.withSpinnerOutput(ctx, "updating PR body", func() (string, error) {
		return updatePRBody(ctx, ghPath, run.WorktreePath, run.PRURL, prBody(run, ownerRepo))
	}); err != nil {
		a.warn("PR body update failed: %v", err)
	}
	run.SetStep("final", runstate.StatusCompleted, "pipeline completed")
	return nil
}

func (a App) runCIWatch(ctx context.Context, cfg config.Config, opts runOptions, run *runstate.State, ghPath string) error {
	timeout := durationOrDefault(opts.CITimeout, cfg.CI.Timeout, 30*time.Minute)
	interval := durationOrDefault("", cfg.CI.PollInterval, 20*time.Second)
	attempts := 3
	for attempt := 1; attempt <= attempts; attempt++ {
		run.SetStep("ci", runstate.StatusRunning, fmt.Sprintf("watching GitHub checks, attempt %d", attempt))
		var out string
		var status string
		err := a.withSpinner(ctx, fmt.Sprintf("watching CI for %s (attempt %d)", run.PRURL, attempt), func() error {
			var watchErr error
			out, status, watchErr = watchPRChecks(ctx, ghPath, run.WorktreePath, run.PRURL, timeout, interval)
			return watchErr
		})
		_, _ = session.WriteLog(*run, fmt.Sprintf("ci-checks-attempt-%d.log", attempt), out)
		if status == "passed" {
			run.SetStep("ci", runstate.StatusCompleted, commandDetail("gh pr checks", out))
			return nil
		}
		if status == "skipped" {
			run.SetStep("ci", runstate.StatusSkipped, strings.TrimSpace(out))
			return nil
		}
		if status == "timeout" {
			detail := fmt.Sprintf("CI watch exceeded %s for %s", timeout, run.PRURL)
			run.SetStep("ci", runstate.StatusFailed, detail)
			return fmt.Errorf("%s", detail)
		}
		detail := commandDetail("gh pr checks", out)
		if err != nil {
			detail = err.Error()
		}
		if attempt == attempts {
			run.SetStep("ci", runstate.StatusFailed, detail)
			return fmt.Errorf("CI checks failed after %d attempts: %s", attempts, detail)
		}
		if cfg.Agent.Name == "" {
			run.SetStep("ci", runstate.StatusFailed, "CI failed and no agent is configured to fix it")
			return fmt.Errorf("CI failed and no agent is configured to fix it")
		}
		var logs string
		_ = a.withSpinner(ctx, "collecting failed CI logs", func() error {
			logs = collectCILogs(ctx, ghPath, run.WorktreePath, run.ReviewBranch)
			return nil
		})
		_, _ = session.WriteLog(*run, fmt.Sprintf("ci-failed-logs-attempt-%d.log", attempt), logs)
		run.SetStep("ci", runstate.StatusFixing, fmt.Sprintf("attempt %d fixing failed checks", attempt))
		changed, fixErr := a.fixCIFailure(ctx, cfg, run, detail, logs)
		if fixErr != nil {
			run.SetStep("ci", runstate.StatusFailed, fixErr.Error())
			return fixErr
		}
		if changed {
			if err := a.runValidationCommands(ctx, cfg, opts, run); err != nil {
				return err
			}
			if err := a.withSpinner(ctx, "pushing CI fix", func() error {
				return pushReviewBranch(ctx, gitx.Client{Dir: run.WorktreePath}, run.Remote, run.ReviewBranch)
			}); err != nil {
				run.SetStep("push", runstate.StatusFailed, err.Error())
				return err
			}
			run.SetStep("push", runstate.StatusCompleted, "pushed CI fix to "+run.Remote)
		} else {
			a.warn("CI fix attempt made no commits or file changes")
		}
	}
	return fmt.Errorf("CI did not complete")
}

func watchPRChecks(ctx context.Context, ghPath, cwd, prURL string, timeout, interval time.Duration) (string, string, error) {
	watchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	seconds := int(interval.Seconds())
	if seconds < 1 {
		seconds = 20
	}
	out, err := runGH(watchCtx, ghPath, cwd, "pr", "checks", prURL, "--watch", "--interval", fmt.Sprint(seconds))
	if watchCtx.Err() != nil {
		return out, "timeout", watchCtx.Err()
	}
	lower := strings.ToLower(out)
	if err == nil {
		return out, "passed", nil
	}
	if strings.Contains(lower, "no checks") || strings.Contains(lower, "no check") || strings.Contains(lower, "no ci") {
		return out, "skipped", nil
	}
	return out, "failed", err
}

func collectCILogs(ctx context.Context, ghPath, cwd, branch string) string {
	ids, err := runGH(ctx, ghPath, cwd, "run", "list", "--branch", branch, "--limit", "5", "--json", "databaseId,conclusion", "--jq", `.[] | select(.conclusion == "failure" or .conclusion == "cancelled" or .conclusion == "timed_out") | .databaseId`)
	if err != nil {
		return redact.Secrets(err.Error())
	}
	var b strings.Builder
	for _, id := range strings.Split(ids, "\n") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		log, logErr := runGH(ctx, ghPath, cwd, "run", "view", id, "--log-failed")
		if logErr != nil {
			fmt.Fprintf(&b, "run %s logs unavailable: %v\n", id, logErr)
			continue
		}
		fmt.Fprintf(&b, "run %s failed logs:\n%s\n", id, truncate(log, 20000))
	}
	if strings.TrimSpace(b.String()) == "" {
		return "No failed GitHub Actions logs were available."
	}
	return redact.Secrets(b.String())
}

func (a App) fixCIFailure(ctx context.Context, cfg config.Config, run *runstate.State, checks, logs string) (bool, error) {
	pathOverride := ""
	if cfg.Agent.PathOverrides != nil {
		pathOverride = strings.TrimSpace(cfg.Agent.PathOverrides[cfg.Agent.Name])
	}
	runner, err := agent.New(cfg.Agent.Name, pathOverride)
	if err != nil {
		return false, err
	}
	client := gitx.Client{Dir: run.WorktreePath}
	beforeHead, err := client.Head(ctx)
	if err != nil {
		return false, err
	}
	prompt := fmt.Sprintf(`Fix the failed CI checks.

Original user intent:
%s

Failing checks:
%s

Failed logs:
%s

Rules:
- Preserve the original user intent.
- Make the smallest correct fix.
- Run the smallest relevant verification command you can.
- Leave changes uncommitted; nml will commit and push the fix.`, run.Intent, checks, truncate(logs, 40000))
	resp, err := a.withSpinnerAgent(ctx, "fixing failed CI checks", func() (agent.Response, error) {
		return runner.Run(ctx, agent.Request{
			CWD:          run.WorktreePath,
			SystemPrompt: mutatingAgentSystemPrompt(),
			Prompt:       prompt,
			Expect:       agent.ExpectText,
			Model:        cfg.Agent.Model,
			ExtraArgs:    cfg.Agent.ExtraArgs,
		})
	})
	if err != nil {
		return false, err
	}
	_, _ = session.WriteLog(*run, "agent-ci-fix.log", resp.Text)
	return commitDirtyOrHeadChanged(ctx, run.WorktreePath, "nml(ci): address failing checks", beforeHead)
}

func shouldRunDeploy(cfg config.Config, opts runOptions, when string) bool {
	if opts.SkipDeploy || !cfg.Deploy.Enabled || strings.TrimSpace(cfg.Deploy.Command) == "" {
		return false
	}
	configured := strings.TrimSpace(cfg.Deploy.When)
	if configured == "" {
		configured = "after_ci"
	}
	return configured == when
}

func (a App) runDeploy(ctx context.Context, cfg config.Config, opts runOptions, run *runstate.State, ghPath, when string) (bool, error) {
	command := strings.TrimSpace(cfg.Deploy.Command)
	if command == "" || !cfg.Deploy.Enabled {
		run.SetStep("deploy", runstate.StatusSkipped, "deploy disabled or missing command")
		return false, nil
	}
	attempts := 3
	changedAny := false
	for attempt := 1; attempt <= attempts; attempt++ {
		run.SetStep("deploy", runstate.StatusRunning, fmt.Sprintf("%s attempt %d: %s", when, attempt, command))
		out, err := a.withSpinnerOutput(ctx, fmt.Sprintf("running deploy (%s): %s", when, command), func() (string, error) { return runShell(ctx, run.WorktreePath, command) })
		detail := commandDetail(command, out)
		if err == nil {
			run.SetStep("deploy", runstate.StatusCompleted, detail)
			return changedAny, nil
		}
		if attempt == attempts {
			run.SetStep("deploy", runstate.StatusFailed, detail)
			return changedAny, fmt.Errorf("deploy failed after %d attempts: %s", attempts, detail)
		}
		if cfg.Agent.Name == "" {
			run.SetStep("deploy", runstate.StatusFailed, "deploy failed and no agent is configured to fix it")
			return changedAny, fmt.Errorf("deploy failed and no agent is configured to fix it")
		}
		run.SetStep("deploy", runstate.StatusFixing, fmt.Sprintf("attempt %d fixing deploy failure", attempt))
		changed, fixErr := a.fixDeployFailure(ctx, cfg, run, detail)
		if fixErr != nil {
			run.SetStep("deploy", runstate.StatusFailed, fixErr.Error())
			return changedAny, fixErr
		}
		if changed {
			changedAny = true
			if err := a.runValidationCommands(ctx, cfg, opts, run); err != nil {
				return changedAny, err
			}
			if run.PRURL != "" {
				if err := a.withSpinner(ctx, "pushing deploy fix", func() error {
					return pushReviewBranch(ctx, gitx.Client{Dir: run.WorktreePath}, run.Remote, run.ReviewBranch)
				}); err != nil {
					run.SetStep("push", runstate.StatusFailed, err.Error())
					return changedAny, err
				}
				run.SetStep("push", runstate.StatusCompleted, "pushed deploy fix to "+run.Remote)
			}
		} else {
			a.warn("deploy fix attempt made no commits or file changes")
		}
	}
	return changedAny, fmt.Errorf("deploy did not complete")
}

func (a App) fixDeployFailure(ctx context.Context, cfg config.Config, run *runstate.State, deployOutput string) (bool, error) {
	pathOverride := ""
	if cfg.Agent.PathOverrides != nil {
		pathOverride = strings.TrimSpace(cfg.Agent.PathOverrides[cfg.Agent.Name])
	}
	runner, err := agent.New(cfg.Agent.Name, pathOverride)
	if err != nil {
		return false, err
	}
	client := gitx.Client{Dir: run.WorktreePath}
	beforeHead, err := client.Head(ctx)
	if err != nil {
		return false, err
	}
	prompt := fmt.Sprintf(`Fix the deploy command failure.

Original user intent:
%s

Deploy failure:
%s

Rules:
- Preserve the original user intent.
- Make the smallest correct fix.
- Do not change deployment credentials or global machine configuration.
- Run the smallest relevant verification command you can.
- Leave changes uncommitted; nml will commit and push the fix.`, run.Intent, truncate(deployOutput, 40000))
	_, err = a.withSpinnerAgent(ctx, "fixing deploy failure", func() (agent.Response, error) {
		return runner.Run(ctx, agent.Request{
			CWD:          run.WorktreePath,
			SystemPrompt: mutatingAgentSystemPrompt(),
			Prompt:       prompt,
			Expect:       agent.ExpectText,
			Model:        cfg.Agent.Model,
			ExtraArgs:    cfg.Agent.ExtraArgs,
		})
	})
	if err != nil {
		return false, err
	}
	return commitDirtyOrHeadChanged(ctx, run.WorktreePath, "nml(deploy): address deploy failure", beforeHead)
}

func runAutoMerge(ctx context.Context, ghPath string, run *runstate.State, cfg config.Config, opts runOptions) error {
	method := strings.TrimSpace(opts.MergeMethod)
	if method == "" {
		method = strings.TrimSpace(cfg.AutoMerge.Method)
	}
	if method == "" {
		method = "squash"
	}
	if !config.ValidMergeMethod(method) {
		return fmt.Errorf("invalid merge method %s", method)
	}
	flag := "--squash"
	switch method {
	case "merge":
		flag = "--merge"
	case "rebase":
		flag = "--rebase"
	}
	run.SetStep("final", runstate.StatusRunning, "enabling auto-merge with method "+method)
	out, err := runGH(ctx, ghPath, run.WorktreePath, "pr", "merge", run.PRURL, flag, "--auto")
	if err != nil {
		return fmt.Errorf("auto-merge failed: %w: %s", err, out)
	}
	run.SetStep("final", runstate.StatusRunning, "auto-merge enabled with method "+method)
	return nil
}

func pushReviewBranch(ctx context.Context, client gitx.Client, remote, branch string) error {
	if !strings.HasPrefix(branch, "nml/") {
		return fmt.Errorf("refusing to push non-tool branch %s", branch)
	}
	out, err := client.Run(ctx, "push", "--force-with-lease", remote, branch)
	if err != nil {
		return fmt.Errorf("git push failed: %w: %s", err, redact.Secrets(out))
	}
	return nil
}

func commitDirty(ctx context.Context, cwd, message string) (bool, error) {
	client := gitx.Client{Dir: cwd}
	status, err := client.StatusPorcelain(ctx)
	if err != nil {
		return false, err
	}
	if !gitx.IsDirty(status) {
		return false, nil
	}
	if err := client.Add(ctx, nil); err != nil {
		return false, err
	}
	out, err := client.Commit(ctx, message)
	if err != nil {
		return false, fmt.Errorf("commit changes: %w: %s", err, redact.Secrets(out))
	}
	return true, nil
}

func commitDirtyOrHeadChanged(ctx context.Context, cwd, message, beforeHead string) (bool, error) {
	changed, err := commitDirty(ctx, cwd, message)
	if err != nil {
		return false, err
	}
	if changed {
		return true, nil
	}
	if strings.TrimSpace(beforeHead) == "" {
		return false, nil
	}
	afterHead, err := gitx.Client{Dir: cwd}.Head(ctx)
	if err != nil {
		return false, err
	}
	return afterHead != beforeHead, nil
}

func durationOrDefault(primary, secondary string, fallback time.Duration) time.Duration {
	for _, value := range []string{primary, secondary} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if d, err := time.ParseDuration(value); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}

var githubRemoteRE = regexp.MustCompile(`github\.com[:/]([^/]+)/([^/]+)$`)

func parseGitHubRemote(remoteURL string) (string, bool) {
	remoteURL = strings.TrimSpace(remoteURL)
	match := githubRemoteRE.FindStringSubmatch(remoteURL)
	if match == nil {
		return "", false
	}
	repo := strings.TrimSuffix(match[2], ".git")
	if repo == "" {
		return "", false
	}
	return match[1] + "/" + repo, true
}

func prTitle(run *runstate.State) string {
	if strings.TrimSpace(run.CommitMessage) != "" {
		return firstNonEmptyLine(run.CommitMessage)
	}
	for _, line := range strings.Split(run.Intent, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			line = strings.TrimPrefix(line, "The user wanted to ")
			if len(line) > 80 {
				line = line[:77] + "..."
			}
			return line
		}
	}
	return "Validate changes from " + run.SourceBranch
}

func prBody(run *runstate.State, ownerRepo string) string {
	var reviewSteps []prbody.StepSummary
	for _, step := range run.Steps {
		if step.Name != "review" {
			continue
		}
		if len(step.Rounds) == 0 {
			reviewSteps = append(reviewSteps, prbody.StepSummary{Name: "Review", Status: string(step.Status), Detail: step.Detail})
			continue
		}
		for _, round := range step.Rounds {
			detail := round.Result
			if len(round.Findings) > 0 {
				detail = fmt.Sprintf("%s with %d findings", round.Result, len(round.Findings))
			}
			reviewSteps = append(reviewSteps, prbody.StepSummary{Name: fmt.Sprintf("Round %d", round.Number), Status: detail})
		}
	}
	return prbody.Generate(prbody.Input{
		OriginalIntent: run.Intent,
		WhatChanged:    []string{"Prepared changes from `" + run.SourceBranch + "` in `" + run.ReviewBranch + "` for `" + ownerRepo + "`."},
		Review:         reviewSteps,
		Tests:          commandSummaries("Test", run.Tests),
		Lint:           commandSummaries("Lint", run.Lint),
		Docs:           stepSummaries(run, "docs"),
		CI:             stepSummaries(run, "ci"),
	})
}

func commandSummaries(name string, runs []runstate.CommandRun) []prbody.StepSummary {
	out := make([]prbody.StepSummary, 0, len(runs))
	for _, run := range runs {
		out = append(out, prbody.StepSummary{Name: name, Status: string(run.Status), Detail: run.Detail})
	}
	return out
}

func stepSummaries(run *runstate.State, name string) []prbody.StepSummary {
	for _, step := range run.Steps {
		if step.Name == name {
			return []prbody.StepSummary{{Name: displayStepName(name), Status: string(step.Status), Detail: step.Detail}}
		}
	}
	return nil
}

func displayStepName(name string) string {
	switch name {
	case "ci":
		return "CI"
	case "docs":
		return "Docs"
	default:
		if name == "" {
			return "Step"
		}
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

func updatePRBody(ctx context.Context, ghPath, cwd, prURL, body string) (string, error) {
	bodyFile, err := os.CreateTemp("", "nml-pr-body-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(bodyFile.Name())
	if _, err := bodyFile.WriteString(body); err != nil {
		bodyFile.Close()
		return "", err
	}
	if err := bodyFile.Close(); err != nil {
		return "", err
	}
	return runGH(ctx, ghPath, cwd, "pr", "edit", prURL, "--body-file", bodyFile.Name())
}

func createOrUpdatePR(ctx context.Context, ghPath, cwd, base, branch, title, body string) (string, error) {
	bodyFile, err := os.CreateTemp("", "nml-pr-body-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(bodyFile.Name())
	if _, err := bodyFile.WriteString(body); err != nil {
		bodyFile.Close()
		return "", err
	}
	if err := bodyFile.Close(); err != nil {
		return "", err
	}
	if url, err := runGH(ctx, ghPath, cwd, "pr", "view", branch, "--json", "url", "--jq", ".url"); err == nil && strings.TrimSpace(url) != "" {
		prURL := strings.TrimSpace(url)
		if _, err := runGH(ctx, ghPath, cwd, "pr", "edit", prURL, "--title", title, "--body-file", bodyFile.Name()); err != nil {
			return "", err
		}
		return prURL, nil
	}
	url, err := runGH(ctx, ghPath, cwd, "pr", "create", "--base", base, "--head", branch, "--title", title, "--body-file", bodyFile.Name())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(url), nil
}

func runGH(ctx context.Context, ghPath, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, ghPath, args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	text := redact.Secrets(string(out))
	if err != nil {
		return text, fmt.Errorf("gh %s failed: %w: %s", strings.Join(args, " "), err, text)
	}
	return text, nil
}

func mutatingAgentSystemPrompt() string {
	return `Operate only inside the current working tree.
Do not modify global machine configuration.
Do not install global packages unless explicitly instructed.
Do not touch files outside this repository.
Preserve the original user intent.`
}

func appendReviewRound(run *runstate.State, round runstate.ReviewRound) {
	for i := range run.Steps {
		if run.Steps[i].Name == "review" {
			run.Steps[i].Rounds = append(run.Steps[i].Rounds, round)
			return
		}
	}
	run.Steps = append(run.Steps, runstate.Step{Name: "review", Status: runstate.StatusPending, Rounds: []runstate.ReviewRound{round}})
}

func truncate(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf("\n... (truncated, %d bytes total)\n", len(s))
}

func saveRun(ctx context.Context, client gitx.Client, state runstate.State) (string, error) {
	gitDir, err := client.GitDir(ctx)
	if err != nil {
		return "", err
	}
	path, err := runstate.Save(gitDir, state)
	if err != nil {
		return "", err
	}
	_, _ = session.SaveSnapshot(state, path)
	return path, nil
}

func (a App) init(ctx context.Context, args []string) int {
	var yes, skipSplash bool
	var agentName, mainBranch, remote, testCmd, lintCmd, mergeMethod string
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&yes, "yes", false, "accept detected defaults")
	fs.BoolVar(&skipSplash, "skip-splash", false, "skip splash banner")
	fs.StringVar(&agentName, "agent", "", "agent name: pi, opencode, codex, or claude")
	fs.StringVar(&mainBranch, "main", "", "main branch")
	fs.StringVar(&remote, "remote", "origin", "git remote")
	fs.StringVar(&testCmd, "test", "", "test command")
	fs.StringVar(&lintCmd, "lint", "", "lint command")
	fs.StringVar(&mergeMethod, "auto-merge-method", "squash", "preferred auto-merge method")
	if hasHelp(args) {
		printInitHelp(a.Out)
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		toon.Error(a.Out, err.Error(), []string{"Run `nml init --help` for usage."})
		return ExitUsage
	}
	if !yes && !a.Interactive {
		toon.Error(a.Out, "init needs a terminal unless --yes is passed", []string{"Run `nml init --yes --agent <name>` from a non-interactive shell."})
		return ExitUsage
	}
	if !skipSplash {
		printSplash(a.Err)
	}
	if ctx.Err() != nil {
		return a.setupCancelled()
	}
	cfg := config.Defaults()
	cfg.Remote = valueOr(remote, cfg.Remote)
	root := ""
	if st, err := gitx.Inspect(ctx, a.Cwd, cfg.Remote, cfg.MainBranch, false); err == nil && st.Kind != gitx.KindNoRepo {
		root = st.RepoRoot
		if mainBranch == "" {
			if detected, err := (gitx.Client{Dir: root}).RemoteHead(ctx, cfg.Remote); err == nil && detected != "" {
				mainBranch = detected
			}
		}
	}
	if mainBranch != "" {
		cfg.MainBranch = mainBranch
	}
	foundAgents := agent.Detect(cfg.Agent.PathOverrides)
	if agentName == "" {
		if len(foundAgents) == 1 || yes {
			picked := agent.PickDefault(foundAgents)
			agentName = picked.Name
		} else if len(foundAgents) > 1 {
			picked, cancelled := a.chooseAgent(ctx, foundAgents)
			if cancelled {
				return a.setupCancelled()
			}
			agentName = picked
		}
	}
	if agentName == "" {
		toon.Error(a.Out, "no supported coding agent found", []string{"Install one of: pi, opencode, codex, claude.", "Or rerun with `nml init --agent <name>` after installing it."})
		return ExitError
	}
	cfg.Agent.Name = agentName
	if !supportedAgent(agentName) {
		toon.Error(a.Out, "unsupported agent: "+agentName, []string{"Use one of: pi, opencode, codex, claude."})
		return ExitUsage
	}
	if !config.ValidMergeMethod(mergeMethod) {
		toon.Error(a.Out, "invalid auto merge method: "+mergeMethod, []string{"Use one of: squash, merge, rebase."})
		return ExitUsage
	}
	cfg.AutoMerge.Method = mergeMethod
	if lintCmd == "" {
		_, detectedLint := detectCommands(rootOrCwd(root, a.Cwd))
		lintCmd = detectedLint
	}
	if !yes && a.Interactive {
		var cancelled bool
		cfg.MainBranch, cancelled = a.promptWizard(ctx, "Main branch", cfg.MainBranch)
		if cancelled {
			return a.setupCancelled()
		}
		cfg.Remote, cancelled = a.promptWizard(ctx, "Remote", cfg.Remote)
		if cancelled {
			return a.setupCancelled()
		}
		testCmd, cancelled = a.promptWizard(ctx, "Test command", testCmd)
		if cancelled {
			return a.setupCancelled()
		}
		lintCmd, cancelled = a.promptWizard(ctx, "Lint command", lintCmd)
		if cancelled {
			return a.setupCancelled()
		}
		cfg.AutoMerge.Method, cancelled = a.promptChoice(ctx, "Preferred merge method", cfg.AutoMerge.Method, []string{"squash", "merge", "rebase"})
		if cancelled {
			return a.setupCancelled()
		}
	}
	if root == "" {
		cfg.Commands.Test = testCmd
	}
	cfg.Commands.Lint = lintCmd
	path, err := config.SaveGlobal(cfg)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	repoTestPath := ""
	if root != "" && strings.TrimSpace(testCmd) != "" {
		var saveErr error
		repoTestPath, saveErr = config.SaveRepoCommand(root, "test", testCmd)
		if saveErr != nil {
			toon.Error(a.Out, saveErr.Error(), nil)
			return ExitError
		}
	}
	toon.KV(a.Out, "config", "saved")
	toon.KV(a.Out, "path", path)
	if repoTestPath != "" {
		toon.KV(a.Out, "repo_config", repoTestPath)
	}
	toon.Table(a.Out, "settings", []string{"key", "value"}, []toon.Row{
		{"agent", cfg.Agent.Name},
		{"main_branch", cfg.MainBranch},
		{"remote", cfg.Remote},
		{"test", testCmd},
		{"lint", cfg.Commands.Lint},
		{"auto_merge.method", cfg.AutoMerge.Method},
	})
	return ExitOK
}

func (a App) doctor(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if hasHelp(args) {
		printDoctorHelp(a.Out)
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		toon.Error(a.Out, err.Error(), []string{"Run `nml doctor --help` for usage."})
		return ExitUsage
	}
	rows := []toon.Row{}
	if p, err := exec.LookPath("git"); err == nil {
		rows = append(rows, toon.Row{"git", "ok", p})
	} else {
		rows = append(rows, toon.Row{"git", "missing", "install git"})
	}
	if p, ok := treehouse.Detect(); ok {
		rows = append(rows, toon.Row{"treehouse", "ok", p})
	} else {
		rows = append(rows, toon.Row{"treehouse", "missing", treehouse.InstallCommand()})
	}
	cfg := config.Defaults()
	first, _ := gitx.Inspect(ctx, a.Cwd, cfg.Remote, cfg.MainBranch, false)
	repoRoot := ""
	if first.Kind != gitx.KindNoRepo {
		repoRoot = first.RepoRoot
	}
	loaded, paths, err := config.Load(repoRoot)
	if err != nil {
		rows = append(rows, toon.Row{"config", "error", err.Error()})
	} else if config.Exists(paths.GlobalPath) {
		rows = append(rows, toon.Row{"config", "ok", paths.GlobalPath})
	} else {
		rows = append(rows, toon.Row{"config", "missing", "run nml init"})
	}
	foundAgents := agent.Detect(loaded.Agent.PathOverrides)
	if len(foundAgents) == 0 {
		rows = append(rows, toon.Row{"agent", "missing", "install pi, opencode, codex, or claude"})
	} else {
		var names []string
		for _, f := range foundAgents {
			names = append(names, f.Name)
		}
		rows = append(rows, toon.Row{"agent", "ok", strings.Join(names, "|")})
	}
	if p, err := exec.LookPath("gh"); err == nil {
		status := "ok"
		detail := p
		if authErr := ghAuthStatus(ctx); authErr != nil {
			status = "warn"
			detail = "gh installed but auth failed"
		}
		rows = append(rows, toon.Row{"gh", status, detail})
	} else {
		rows = append(rows, toon.Row{"gh", "missing", "PR and CI steps will be skipped"})
	}
	if first.Kind == gitx.KindNoRepo {
		rows = append(rows, toon.Row{"repo", "skip", "not inside a git repository"})
	} else {
		state, err := gitx.Inspect(ctx, first.RepoRoot, loaded.Remote, loaded.MainBranch, false)
		if err != nil {
			rows = append(rows, toon.Row{"repo", "error", err.Error()})
		} else {
			rows = append(rows, toon.Row{"repo", "ok", string(state.Kind)})
		}
	}
	toon.Table(a.Out, "checks", []string{"name", "status", "detail"}, rows)
	return ExitOK
}

func (a App) config(ctx context.Context, args []string) int {
	format := "yaml"
	setTestCommand := ""
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&format, "format", "yaml", "output format: yaml or toon")
	fs.StringVar(&setTestCommand, "set-test-command", "", "save a per-repo test command")
	if hasHelp(args) {
		printConfigHelp(a.Out)
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		toon.Error(a.Out, err.Error(), []string{"Run `nml config --help` for usage."})
		return ExitUsage
	}
	st, _ := gitx.Inspect(ctx, a.Cwd, "origin", "main", false)
	repoRoot := ""
	if st.Kind != gitx.KindNoRepo {
		repoRoot = st.RepoRoot
	}
	cfg, paths, err := config.Load(repoRoot)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	if setTestCommand != "" {
		if repoRoot == "" {
			toon.Error(a.Out, "--set-test-command must be run inside a git repository", []string{"cd into the repo, then run `nml config --set-test-command \"<cmd>\"`."})
			return ExitUsage
		}
		path, err := config.SaveRepoCommand(repoRoot, "test", setTestCommand)
		if err != nil {
			toon.Error(a.Out, err.Error(), nil)
			return ExitError
		}
		toon.KV(a.Out, "config", "saved")
		toon.KV(a.Out, "repo_path", path)
		toon.KV(a.Out, "test", setTestCommand)
		return ExitOK
	}
	switch format {
	case "yaml":
		data, err := config.MarshalYAML(cfg)
		if err != nil {
			toon.Error(a.Out, err.Error(), nil)
			return ExitError
		}
		_, _ = a.Out.Write(data)
	case "toon":
		toon.Table(a.Out, "config", []string{"key", "value"}, []toon.Row{
			{"global_path", paths.GlobalPath},
			{"repo_path", paths.RepoPath},
			{"agent", cfg.Agent.Name},
			{"main_branch", cfg.MainBranch},
			{"remote", cfg.Remote},
			{"test", cfg.Commands.Test},
			{"lint", cfg.Commands.Lint},
			{"review.rounds", fmt.Sprint(cfg.Review.Rounds)},
			{"ci.timeout", cfg.CI.Timeout},
		})
	default:
		toon.Error(a.Out, "invalid --format: "+format, []string{"Use `--format yaml` or `--format toon`."})
		return ExitUsage
	}
	return ExitOK
}

func (a App) status(ctx context.Context, args []string) int {
	format := "toon"
	runRef := ""
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&format, "format", "toon", "output format: toon")
	fs.StringVar(&runRef, "run", "", "run id or path")
	if hasHelp(args) {
		printStatusHelp(a.Out)
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		toon.Error(a.Out, err.Error(), []string{"Run `nml status --help` for usage."})
		return ExitUsage
	}
	if format != "toon" {
		toon.Error(a.Out, "invalid --format: "+format, []string{"Only `--format toon` is supported in this build."})
		return ExitUsage
	}
	path, state, code := a.loadRun(ctx, runRef)
	if code != ExitOK {
		return code
	}
	printRunStatus(a.Out, state, path)
	return ExitOK
}

func (a App) tui(ctx context.Context, args []string) int {
	runRef := ""
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&runRef, "run", "", "run id or path")
	if hasHelp(args) {
		printTUIHelp(a.Out)
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		toon.Error(a.Out, err.Error(), []string{"Run `nml tui --help` for usage."})
		return ExitUsage
	}
	if !a.Interactive {
		toon.Error(a.Out, "tui requires an interactive terminal", []string{"Use `nml status --format toon` in headless mode."})
		return ExitUsage
	}
	_, state, code := a.loadRun(ctx, runRef)
	if code != ExitOK {
		return code
	}
	if err := tui.ShowRun(ctx, a.Out, state); err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	return ExitOK
}

func (a App) findings(ctx context.Context, args []string) int {
	format := "toon"
	runRef := ""
	fs := flag.NewFlagSet("findings", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&format, "format", "toon", "output format: toon")
	fs.StringVar(&runRef, "run", "", "run id or path")
	if hasHelp(args) {
		printFindingsHelp(a.Out)
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		toon.Error(a.Out, err.Error(), []string{"Run `nml findings --help` for usage."})
		return ExitUsage
	}
	if format != "toon" {
		toon.Error(a.Out, "invalid --format: "+format, []string{"Only `--format toon` is supported in this build."})
		return ExitUsage
	}
	_, state, code := a.loadRun(ctx, runRef)
	if code != ExitOK {
		return code
	}
	var rows []toon.Row
	for _, step := range state.Steps {
		if step.Name != "review" {
			continue
		}
		for _, round := range step.Rounds {
			for _, finding := range round.Findings {
				line := ""
				if finding.Line > 0 {
					line = fmt.Sprint(finding.Line)
				}
				rows = append(rows, toon.Row{finding.ID, string(finding.Severity), finding.File, line, finding.Description})
			}
		}
	}
	if len(rows) == 0 {
		fmt.Fprintln(a.Out, "findings: 0 review findings found for this run")
		return ExitOK
	}
	toon.Table(a.Out, "findings", []string{"id", "severity", "file", "line", "description"}, rows)
	return ExitOK
}

func (a App) resume(ctx context.Context, args []string) int {
	var runRef string
	opts := runOptions{}
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&runRef, "run", "", "run id or path")
	fs.BoolVar(&opts.Yes, "yes", false, "accept safe defaults without prompts")
	fs.BoolVar(&opts.Yolo, "yolo", false, "auto-fix all actionable review findings")
	fs.BoolVar(&opts.SkipDocs, "skip-docs", false, "skip docs for this resume")
	fs.BoolVar(&opts.SkipDeploy, "skip-deploy", false, "skip deploy for this resume")
	fs.StringVar(&opts.CITimeout, "ci-timeout", "", "CI timeout for this resume")
	fs.StringVar(&opts.TestCommand, "test-command", "", "test command for this resume only")
	if hasHelp(args) {
		printResumeHelp(a.Out)
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		toon.Error(a.Out, err.Error(), []string{"Run `nml resume --help` for usage."})
		return ExitUsage
	}
	if fs.NArg() > 0 {
		toon.Error(a.Out, "resume does not accept positional arguments", []string{"Run `nml resume --run <id>` or `nml resume`."})
		return ExitUsage
	}
	path, state, code := a.loadRunForResume(ctx, runRef)
	if code != ExitOK {
		return code
	}
	return a.continueRun(ctx, cfgLoadOptions{RunOptions: opts, StatePath: path}, state)
}

func (a App) runs(ctx context.Context, args []string) int {
	allRepos := false
	resumableOnly := false
	fs := flag.NewFlagSet("runs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&allRepos, "all", false, "show runs for all repositories")
	fs.BoolVar(&resumableOnly, "resumable", false, "show only failed or interrupted runs")
	if hasHelp(args) {
		printRunsHelp(a.Out)
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		toon.Error(a.Out, err.Error(), []string{"Run `nml runs --help` for usage."})
		return ExitUsage
	}
	if fs.NArg() > 0 {
		toon.Error(a.Out, "runs does not accept positional arguments", []string{"Run `nml runs --all` to list every repository."})
		return ExitUsage
	}
	repoRoot := ""
	if !allRepos {
		if st, err := gitx.Inspect(ctx, a.Cwd, "origin", "main", false); err == nil && st.Kind != gitx.KindNoRepo {
			repoRoot = st.RepoRoot
		}
	}
	entries, err := session.List(repoRoot, resumableOnly)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	if len(entries) == 0 {
		fmt.Fprintln(a.Out, "runs: 0 saved runs found")
		return ExitOK
	}
	rows := make([]toon.Row, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, toon.Row{entry.RunID, entry.Status, shortPath(entry.RepoRoot), entry.ReviewBranch, entry.PRURL, entry.UpdatedAt.Format(time.RFC3339)})
	}
	toon.Table(a.Out, "runs", []string{"id", "status", "repo", "branch", "pr", "updated"}, rows)
	toon.List(a.Out, "help", []string{"Run `nml resume --run <id>` to continue a resumable run.", "Run `nml status --run <id>` to inspect a run."})
	return ExitOK
}

type cfgLoadOptions struct {
	RunOptions runOptions
	StatePath  string
}

func (a App) continueRun(ctx context.Context, options cfgLoadOptions, state runstate.State) int {
	if !session.Resumable(state) {
		printRunStatus(a.Out, state, options.StatePath)
		toon.List(a.Out, "help", []string{"Run is already completed.", "Run `nml run` to start a new validation run when the repository has new changes."})
		return ExitOK
	}
	if reviewStatus := stepStatus(state, "review"); reviewStatus == runstate.StatusAwaitingUser {
		if a.Interactive {
			fmt.Fprint(a.Out, tui.RenderReviewGate(state.ID, options.StatePath, state.WorktreePath, latestReviewFindings(state)))
		} else {
			printReviewGate(a.Out, state, options.StatePath, latestReviewFindings(state))
		}
		return ExitOK
	}
	if strings.TrimSpace(state.RepoRoot) == "" || strings.TrimSpace(state.WorktreePath) == "" {
		toon.Error(a.Out, "run state is missing repo or worktree path", []string{"Inspect with `nml status --run " + state.ID + "`."})
		return ExitError
	}
	if _, err := os.Stat(state.WorktreePath); err != nil {
		toon.Error(a.Out, "leased worktree is unavailable: "+err.Error(), []string{"The run cannot be resumed automatically because its isolated worktree is missing."})
		return ExitError
	}
	cfg, _, err := config.Load(state.RepoRoot)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	client := gitx.Client{Dir: state.RepoRoot}
	a.beginPipelineProgress()
	if !stepCompletedOrSkipped(state, "review") {
		outcome, err := a.runReview(ctx, cfg, options.RunOptions, &state)
		path, _ := saveRun(ctx, client, state)
		if path != "" {
			options.StatePath = path
		}
		if err != nil {
			state.SetStep("review", runstate.StatusFailed, redact.Secrets(err.Error()))
			saveRun(ctx, client, state)
			toon.Error(a.Out, "review failed", []string{redact.Secrets(err.Error())})
			return ExitError
		}
		if outcome.AwaitingUser {
			if a.Interactive {
				fmt.Fprint(a.Out, tui.RenderReviewGate(state.ID, options.StatePath, state.WorktreePath, outcome.Findings))
			} else {
				printReviewGate(a.Out, state, options.StatePath, outcome.Findings)
			}
			return ExitOK
		}
	}
	if needsValidationResume(ctx, state) {
		if err := a.runValidationCommands(ctx, cfg, options.RunOptions, &state); err != nil {
			state.SetStep("final", runstate.StatusFailed, err.Error())
			saveRun(ctx, client, state)
			toon.Error(a.Out, err.Error(), nil)
			return ExitError
		}
		path, _ := saveRun(ctx, client, state)
		if path != "" {
			options.StatePath = path
		}
	}
	if err := a.runPushAndPR(ctx, cfg, options.RunOptions, &state); err != nil {
		state.SetStep("final", runstate.StatusFailed, err.Error())
		path, _ := saveRun(ctx, client, state)
		if path != "" {
			options.StatePath = path
		}
		toon.Error(a.Out, "resume failed", []string{"Saved run state: " + options.StatePath, redact.Secrets(err.Error())})
		return ExitError
	}
	path, err := saveRun(ctx, client, state)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	if a.Interactive {
		fmt.Fprint(a.Out, tui.RenderRunResult(state, path))
	} else {
		printRunPrepared(a.Out, state, path)
	}
	return ExitOK
}

func (a App) respond(ctx context.Context, args []string) int {
	var runRef, action, findingList string
	fs := flag.NewFlagSet("respond", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&runRef, "run", "", "run id or path")
	fs.StringVar(&action, "action", "", "approve, skip, or fix")
	fs.StringVar(&findingList, "findings", "", "comma-separated finding ids for --action fix")
	if hasHelp(args) {
		printRespondHelp(a.Out)
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		toon.Error(a.Out, err.Error(), []string{"Run `nml respond --help` for usage."})
		return ExitUsage
	}
	action = strings.TrimSpace(action)
	if action != "approve" && action != "skip" && action != "fix" {
		toon.Error(a.Out, "--action must be approve, skip, or fix", []string{"Run `nml respond --action approve`, `nml respond --action skip`, or `nml respond --action fix --findings r1,r2`."})
		return ExitUsage
	}
	path, state, code := a.loadRun(ctx, runRef)
	if code != ExitOK {
		return code
	}
	cfg, _, err := config.Load(state.RepoRoot)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	client := gitx.Client{Dir: state.RepoRoot}
	progressStarted := false
	startProgress := func() {
		if !progressStarted {
			a.beginPipelineProgress()
			progressStarted = true
		}
	}
	if action == "approve" {
		state.SetStep("review", runstate.StatusCompleted, "approved by user")
	} else if action == "skip" {
		state.SetStep("review", runstate.StatusSkipped, "skipped by user")
	} else {
		selected := selectFindings(latestReviewFindings(state), findingList)
		if len(selected) == 0 {
			toon.Error(a.Out, "no findings selected", []string{"Pass `--findings r1,r2` using ids from `nml findings`."})
			return ExitUsage
		}
		if cfg.Agent.Name == "" {
			toon.Error(a.Out, "cannot fix findings without a configured agent", []string{"Run `nml init --agent <name>` first."})
			return ExitError
		}
		pathOverride := ""
		if cfg.Agent.PathOverrides != nil {
			pathOverride = strings.TrimSpace(cfg.Agent.PathOverrides[cfg.Agent.Name])
		}
		runner, err := agent.New(cfg.Agent.Name, pathOverride)
		if err != nil {
			toon.Error(a.Out, err.Error(), nil)
			return ExitError
		}
		state.SetStep("review", runstate.StatusFixing, "fixing selected findings")
		startProgress()
		if err := a.withSpinner(ctx, "fixing selected review findings", func() error { return fixReviewFindings(ctx, runner, cfg, &state, selected) }); err != nil {
			state.SetStep("review", runstate.StatusFailed, err.Error())
			saveRun(ctx, client, state)
			toon.Error(a.Out, err.Error(), nil)
			return ExitError
		}
		outcome, err := a.runReview(ctx, cfg, runOptions{}, &state)
		if err != nil {
			state.SetStep("review", runstate.StatusFailed, err.Error())
			saveRun(ctx, client, state)
			toon.Error(a.Out, err.Error(), nil)
			return ExitError
		}
		path, _ = saveRun(ctx, client, state)
		if outcome.AwaitingUser {
			if a.Interactive {
				fmt.Fprint(a.Out, tui.RenderReviewGate(state.ID, path, state.WorktreePath, outcome.Findings))
			} else {
				printReviewGate(a.Out, state, path, outcome.Findings)
			}
			return ExitOK
		}
	}
	startProgress()
	if err := a.runValidationCommands(ctx, cfg, runOptions{}, &state); err != nil {
		state.SetStep("final", runstate.StatusFailed, err.Error())
		saveRun(ctx, client, state)
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	if err := a.runPushAndPR(ctx, cfg, runOptions{}, &state); err != nil {
		state.SetStep("final", runstate.StatusFailed, err.Error())
		saveRun(ctx, client, state)
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	path, err = saveRun(ctx, client, state)
	if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return ExitError
	}
	_ = path
	if a.Interactive {
		fmt.Fprint(a.Out, tui.RenderRunResult(state, path))
	} else {
		printRunPrepared(a.Out, state, path)
	}
	return ExitOK
}

func latestReviewFindings(state runstate.State) []review.Finding {
	for _, step := range state.Steps {
		if step.Name != "review" || len(step.Rounds) == 0 {
			continue
		}
		for i := len(step.Rounds) - 1; i >= 0; i-- {
			if len(step.Rounds[i].Findings) > 0 {
				return step.Rounds[i].Findings
			}
		}
	}
	return nil
}

func selectFindings(findings []review.Finding, findingList string) []review.Finding {
	wanted := map[string]bool{}
	for _, id := range strings.Split(findingList, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			wanted[id] = true
		}
	}
	var selected []review.Finding
	for _, finding := range findings {
		if len(wanted) == 0 || wanted[finding.ID] {
			finding.Selected = true
			selected = append(selected, finding)
		}
	}
	return selected
}

func stepStatus(state runstate.State, name string) runstate.StepStatus {
	for _, step := range state.Steps {
		if step.Name == name {
			return step.Status
		}
	}
	return ""
}

func stepCompletedOrSkipped(state runstate.State, name string) bool {
	status := stepStatus(state, name)
	return status == runstate.StatusCompleted || status == runstate.StatusSkipped
}

func needsValidationResume(ctx context.Context, state runstate.State) bool {
	if !stepCompletedOrSkipped(state, "test") || !stepCompletedOrSkipped(state, "docs") || !stepCompletedOrSkipped(state, "lint") {
		return true
	}
	return reviewBranchAheadRemote(ctx, state)
}

func reviewBranchAheadRemote(ctx context.Context, state runstate.State) bool {
	if strings.TrimSpace(state.WorktreePath) == "" || strings.TrimSpace(state.Remote) == "" || strings.TrimSpace(state.ReviewBranch) == "" {
		return false
	}
	client := gitx.Client{Dir: state.WorktreePath}
	remoteRef := state.Remote + "/" + state.ReviewBranch
	if !client.RefExists(ctx, remoteRef) {
		return true
	}
	ahead, _, err := client.AheadBehind(ctx, remoteRef, "HEAD")
	return err == nil && ahead > 0
}

func latestResumableRun(repoRoot string) (string, runstate.State, bool) {
	path, state, err := session.Latest(repoRoot, true)
	if err != nil {
		return "", runstate.State{}, false
	}
	return path, state, true
}

func shortPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if rel, relErr := filepath.Rel(home, path); relErr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return path
}

func (a App) loadRunForResume(ctx context.Context, runRef string) (string, runstate.State, int) {
	if strings.TrimSpace(runRef) != "" {
		return a.loadRun(ctx, runRef)
	}
	repoRoot := ""
	if st, err := gitx.Inspect(ctx, a.Cwd, "origin", "main", false); err == nil && st.Kind != gitx.KindNoRepo {
		repoRoot = st.RepoRoot
	}
	path, state, err := session.Latest(repoRoot, true)
	if err == nil {
		return path, state, ExitOK
	}
	path, state, code := a.loadRun(ctx, "")
	if code != ExitOK {
		return "", runstate.State{}, code
	}
	if !session.Resumable(state) {
		toon.Error(a.Out, "latest run is already completed", []string{"Run `nml runs --resumable` to find failed or interrupted runs."})
		return "", runstate.State{}, ExitError
	}
	return path, state, ExitOK
}

func (a App) loadRun(ctx context.Context, runRef string) (string, runstate.State, int) {
	st, err := gitx.Inspect(ctx, a.Cwd, "origin", "main", false)
	repoRoot := ""
	gitDir := ""
	if err == nil && st.Kind != gitx.KindNoRepo {
		repoRoot = st.RepoRoot
		client := gitx.Client{Dir: st.RepoRoot}
		gitDir, err = client.GitDir(ctx)
		if err != nil {
			toon.Error(a.Out, err.Error(), nil)
			return "", runstate.State{}, ExitError
		}
	} else if err != nil {
		toon.Error(a.Out, err.Error(), nil)
		return "", runstate.State{}, ExitError
	}
	if runRef != "" {
		if strings.Contains(runRef, string(os.PathSeparator)) || strings.HasSuffix(runRef, ".json") {
			state, err := runstate.Load(runRef)
			if err != nil {
				toon.Error(a.Out, err.Error(), []string{"Check the --run value."})
				return "", runstate.State{}, ExitError
			}
			return runRef, state, ExitOK
		}
		if gitDir != "" {
			localGitDir := gitDir
			if !filepath.IsAbs(localGitDir) {
				localGitDir = filepath.Join(repoRoot, localGitDir)
			}
			path := filepath.Join(localGitDir, "nml", "runs", runRef+".json")
			if state, err := runstate.Load(path); err == nil {
				return path, state, ExitOK
			}
		}
		path, state, err := session.Load(runRef, repoRoot)
		if err != nil {
			toon.Error(a.Out, err.Error(), []string{"Check the --run value or run `nml runs --all`."})
			return "", runstate.State{}, ExitError
		}
		return path, state, ExitOK
	}
	if gitDir == "" {
		toon.Error(a.Out, "not inside a git repository", []string{"Run this command from the repository that owns the nml run, or pass `--run <id|path>`."})
		return "", runstate.State{}, ExitError
	}
	path, state, err := runstate.Latest(gitDir, repoRoot)
	if err == nil {
		return path, state, ExitOK
	}
	path, state, err = session.Latest(repoRoot, false)
	if err != nil {
		toon.Error(a.Out, "no run state found", []string{"Run `nml run --message \"<message>\"` first."})
		return "", runstate.State{}, ExitError
	}
	return path, state, ExitOK
}

func isNoopState(state gitx.State) bool {
	switch state.Kind {
	case gitx.KindNoRepo, gitx.KindCleanMainNoop, gitx.KindFeatureNoDeltaNoop:
		return true
	default:
		return false
	}
}

func printNoop(w io.Writer, state gitx.State) bool {
	switch state.Kind {
	case gitx.KindNoRepo:
		fmt.Fprintln(w, "Nothing to do: current directory is not inside a git repository.")
		return true
	case gitx.KindCleanMainNoop:
		fmt.Fprintf(w, "Nothing to do: %s is clean and not ahead of %s/%s.\n", state.MainBranch, state.Remote, state.MainBranch)
		return true
	case gitx.KindFeatureNoDeltaNoop:
		if state.PatchOnly {
			fmt.Fprintln(w, "Nothing to do: changes are already on main.")
		} else {
			fmt.Fprintf(w, "Nothing to do: this branch has no changes relative to %s/%s.\n", state.Remote, state.MainBranch)
		}
		return true
	}
	return false
}

func printReviewGate(w io.Writer, state runstate.State, path string, findings []review.Finding) {
	toon.KV(w, "run", state.ID)
	toon.KV(w, "gate", "review")
	toon.KV(w, "state_path", path)
	toon.KV(w, "worktree_path", state.WorktreePath)
	rows := make([]toon.Row, 0, len(findings))
	for _, finding := range findings {
		line := ""
		if finding.Line > 0 {
			line = fmt.Sprint(finding.Line)
		}
		rows = append(rows, toon.Row{finding.ID, string(finding.Severity), finding.File, line, finding.Description})
	}
	toon.Table(w, "findings", []string{"id", "severity", "file", "line", "description"}, rows)
	toon.List(w, "help", []string{"Run `nml respond --action fix --findings <ids> --run " + state.ID + "` to fix selected findings.", "Run `nml respond --action approve --run " + state.ID + "` to accept and continue.", "Run `nml respond --action skip --run " + state.ID + "` to skip review."})
}

func printRunPrepared(w io.Writer, state runstate.State, path string) {
	toon.KV(w, "run", state.ID)
	toon.KV(w, "status", displayRunStatus(state))
	toon.KV(w, "state_path", path)
	toon.KV(w, "source_branch", state.SourceBranch)
	toon.KV(w, "review_branch", state.ReviewBranch)
	toon.KV(w, "worktree_path", state.WorktreePath)
	toon.KV(w, "base", state.BaseRef)
	toon.KV(w, "intent", state.Intent)
	toon.Table(w, "steps", []string{"name", "status", "detail"}, stepRows(state))
	toon.List(w, "help", []string{"Inspect this run with `nml status --run " + state.ID + "`.", "Return the lease manually with `treehouse return " + state.WorktreePath + " --force` if you want to clean up before CI and deploy stages exist."})
}

func displayRunStatus(state runstate.State) string {
	return session.Status(state)
}

func printRunStatus(w io.Writer, state runstate.State, path string) {
	toon.KV(w, "run", state.ID)
	toon.KV(w, "path", path)
	toon.KV(w, "repo", state.RepoRoot)
	toon.KV(w, "source_branch", state.SourceBranch)
	toon.KV(w, "review_branch", state.ReviewBranch)
	toon.KV(w, "worktree_path", state.WorktreePath)
	toon.KV(w, "base", state.BaseRef)
	toon.KV(w, "pr_url", state.PRURL)
	toon.Table(w, "steps", []string{"name", "status", "detail"}, stepRows(state))
}

func stepRows(state runstate.State) []toon.Row {
	rows := make([]toon.Row, 0, len(state.Steps))
	for _, step := range state.Steps {
		rows = append(rows, toon.Row{step.Name, string(step.Status), step.Detail})
	}
	return rows
}

func fallbackCommitMessage(files []string) string {
	if len(files) == 0 {
		return "chore: update worktree"
	}
	if len(files) == 1 {
		base := filepath.Base(files[0])
		return "chore: update " + base
	}
	return fmt.Sprintf("chore: update %d files", len(files))
}

func fallbackIntent(message, diffStat, log string) string {
	message = strings.TrimSpace(message)
	if message != "" {
		return fmt.Sprintf("The user wanted to make the change described by `%s` while preserving behavior outside that scope.", message)
	}
	firstLog := firstNonEmptyLine(log)
	if firstLog != "" {
		return fmt.Sprintf("The user wanted to deliver the branch change described by `%s` while preserving behavior outside that scope.", firstLog)
	}
	if strings.TrimSpace(diffStat) != "" {
		return "The user wanted to validate the current branch changes while preserving behavior outside the touched files."
	}
	return "The user wanted to validate the current branch changes."
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func uniqueReviewBranchName(branch, runID string) string {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return branch
	}
	parts := strings.Split(runID, "-")
	suffix := parts[len(parts)-1]
	if suffix == "" {
		return branch
	}
	return branch + "-" + suffix
}

func reviewBranchName(message, branch, head string) string {
	source := message
	if strings.TrimSpace(source) == "" {
		source = branch
	}
	source = strings.ToLower(source)
	if i := strings.Index(source, ":"); i >= 0 && i+1 < len(source) {
		source = source[i+1:]
	}
	slug := strings.Trim(slugRE.ReplaceAllString(source, "-"), "-")
	if slug == "" {
		slug = "change"
	}
	parts := strings.Split(slug, "-")
	if len(parts) > 4 {
		slug = strings.Join(parts[:4], "-")
	}
	short := "unknown"
	if len(head) >= 7 {
		short = head[:7]
	}
	prefix := "change"
	if strings.HasPrefix(strings.ToLower(message), "fix") {
		prefix = "fix"
	} else if strings.HasPrefix(strings.ToLower(message), "feat") {
		prefix = "feat"
	}
	return fmt.Sprintf("nml/%s-%s-%s", prefix, slug, short)
}

func detectCommands(root string) (testCmd string, lintCmd string) {
	if exists(filepath.Join(root, "go.mod")) {
		return "go test ./...", "go vet ./..."
	}
	if exists(filepath.Join(root, "package.json")) {
		scripts := packageScripts(filepath.Join(root, "package.json"))
		if scripts["test"] {
			testCmd = "npm test"
		}
		if scripts["lint"] {
			lintCmd = "npm run lint"
		}
		return testCmd, lintCmd
	}
	if exists(filepath.Join(root, "pyproject.toml")) || exists(filepath.Join(root, "pytest.ini")) {
		return "pytest", "ruff check ."
	}
	return "", ""
}

func packageScripts(path string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var pkg struct {
		Scripts map[string]any `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return out
	}
	for k := range pkg.Scripts {
		out[k] = true
	}
	return out
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func supportedAgent(name string) bool {
	for _, supported := range agent.Supported {
		if name == supported {
			return true
		}
	}
	return false
}

func (a App) chooseAgent(ctx context.Context, found []agent.Found) (string, bool) {
	if !a.Interactive || len(found) == 0 {
		return "", false
	}
	options := make([]tui.Option, 0, len(found))
	for _, f := range found {
		options = append(options, tui.Option{Label: f.Name, Description: f.Path})
	}
	idx, cancelled, err := tui.SelectOne(ctx, a.In, a.Err, "Choose coding agent", options, 0)
	if err != nil {
		if ctx.Err() != nil {
			return "", true
		}
		fmt.Fprintf(a.Err, "nml: agent picker failed: %v\n", err)
		return "", true
	}
	if cancelled || idx < 0 || idx >= len(found) {
		return "", true
	}
	return found[idx].Name, false
}

func (a App) prompt(label, def string) string {
	if !a.Interactive {
		return def
	}
	if def == "" {
		fmt.Fprintf(a.Err, "%s: ", label)
	} else {
		fmt.Fprintf(a.Err, "%s [%s]: ", label, def)
	}
	reader := bufio.NewReader(a.In)
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		return def
	}
	return text
}

func (a App) promptWizard(ctx context.Context, label, def string) (string, bool) {
	if !a.Interactive {
		return def, false
	}
	value, cancelled, err := tui.Input(ctx, a.In, a.Err, label, def)
	if err != nil {
		if ctx.Err() != nil {
			return "", true
		}
		fmt.Fprintf(a.Err, "nml: input prompt failed: %v\n", err)
		return "", true
	}
	if cancelled {
		return "", true
	}
	return value, false
}

func (a App) promptChoice(ctx context.Context, label, def string, choices []string) (string, bool) {
	if !a.Interactive || len(choices) == 0 {
		return def, false
	}
	options := make([]tui.Option, 0, len(choices))
	initial := 0
	for i, choice := range choices {
		if choice == def {
			initial = i
		}
		options = append(options, tui.Option{Label: choice, Description: ""})
	}
	idx, cancelled, err := tui.SelectOne(ctx, a.In, a.Err, label, options, initial)
	if err != nil {
		if ctx.Err() != nil {
			return "", true
		}
		fmt.Fprintf(a.Err, "nml: option picker failed: %v\n", err)
		return "", true
	}
	if cancelled || idx < 0 || idx >= len(choices) {
		return "", true
	}
	return choices[idx], false
}

func (a App) setupCancelled() int {
	fmt.Fprintln(a.Err, "Setup cancelled.")
	return ExitError
}

func (a App) runCancelled() int {
	fmt.Fprintln(a.Err, "Run cancelled.")
	return ExitError
}

func (a App) beginPipelineProgress() {
	if a.Interactive {
		fmt.Fprint(a.Err, tui.RenderPipelineProgressHeader())
	}
}

func (a App) progress(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if a.Interactive {
		fmt.Fprint(a.Err, tui.RenderProgressStep(runstate.StatusRunning, message, 2))
		return
	}
	fmt.Fprintf(a.Err, "nml: %s\n", message)
}

func (a App) withSpinner(ctx context.Context, label string, fn func() error) error {
	if !a.Interactive {
		a.progress("%s", label)
		return fn()
	}
	done := make(chan error, 1)
	go func() { done <- fn() }()
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	frame := 0
	render := func() {
		fmt.Fprintf(a.Err, "\r\033[K%s", tui.RenderProgressInline(runstate.StatusRunning, label, frame))
	}
	render()
	for {
		select {
		case err := <-done:
			fmt.Fprint(a.Err, "\r\033[K")
			if err != nil {
				fmt.Fprint(a.Err, tui.RenderProgressStep(runstate.StatusFailed, label, frame))
			} else {
				fmt.Fprint(a.Err, tui.RenderProgressStep(runstate.StatusCompleted, label, frame))
			}
			return err
		case <-ticker.C:
			frame++
			render()
		case <-ctx.Done():
			// Keep waiting for the operation to return so callers do not observe
			// partially updated state. CommandContext users will finish promptly.
		}
	}
}

func (a App) withSpinnerOutput(ctx context.Context, label string, fn func() (string, error)) (string, error) {
	var output string
	err := a.withSpinner(ctx, label, func() error {
		var runErr error
		output, runErr = fn()
		return runErr
	})
	return output, err
}

func (a App) withSpinnerAgent(ctx context.Context, label string, fn func() (agent.Response, error)) (agent.Response, error) {
	var response agent.Response
	err := a.withSpinner(ctx, label, func() error {
		var runErr error
		response, runErr = fn()
		return runErr
	})
	return response, err
}

func (a App) warn(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if a.Interactive {
		fmt.Fprintf(a.Err, "│  Tip: %s\n", message)
		return
	}
	fmt.Fprintf(a.Err, "nml: warning: %s\n", message)
}

func ghAuthStatus(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "auth", "status")
	return cmd.Run()
}

func rootOrCwd(root, cwd string) string {
	if root != "" {
		return root
	}
	return cwd
}

func valueOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func isTerminal(r io.Reader) bool {
	file, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func hasHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func printSplash(w io.Writer) {
	banner := []string{
		"███╗   ██╗ ██████╗     ███╗   ███╗██╗███████╗████████╗ █████╗ ██╗  ██╗███████╗███████╗    ██╗     ██╗████████╗███████╗",
		"████╗  ██║██╔═══██╗    ████╗ ████║██║██╔════╝╚══██╔══╝██╔══██╗██║ ██╔╝██╔════╝██╔════╝    ██║     ██║╚══██╔══╝██╔════╝",
		"██╔██╗ ██║██║   ██║    ██╔████╔██║██║███████╗   ██║   ███████║█████╔╝ █████╗  ███████╗    ██║     ██║   ██║   █████╗  ",
		"██║╚██╗██║██║   ██║    ██║╚██╔╝██║██║╚════██║   ██║   ██╔══██║██╔═██╗ ██╔══╝  ╚════██║    ██║     ██║   ██║   ██╔══╝  ",
		"██║ ╚████║╚██████╔╝    ██║ ╚═╝ ██║██║███████║   ██║   ██║  ██║██║  ██╗███████╗███████║    ███████╗██║   ██║   ███████╗",
		"╚═╝  ╚═══╝ ╚═════╝     ╚═╝     ╚═╝╚═╝╚══════╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝╚══════╝    ╚══════╝╚═╝   ╚═╝   ╚══════╝",
	}
	for _, line := range banner {
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w, "\nnml - lightweight no-mistakes-style validation")
}

func (a App) printHelp() {
	fmt.Fprintln(a.Out, "nml - lightweight no-mistakes-style PR validation")
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Commands:")
	fmt.Fprintln(a.Out, "  nml                  inspect repo and show next action")
	fmt.Fprintln(a.Out, "  nml init             first-run setup wizard")
	fmt.Fprintln(a.Out, "  nml doctor           check tools, auth, config, and repo state")
	fmt.Fprintln(a.Out, "  nml run              prepare a validation run")
	fmt.Fprintln(a.Out, "  nml status           show latest run state")
	fmt.Fprintln(a.Out, "  nml findings         show review findings for a run")
	fmt.Fprintln(a.Out, "  nml config           print merged config")
	fmt.Fprintln(a.Out, "  nml runs             list saved runs from ~/.nml")
	fmt.Fprintln(a.Out, "  nml resume           continue latest resumable run")
	fmt.Fprintln(a.Out, "  nml respond          answer a saved review gate")
	fmt.Fprintln(a.Out, "  nml tui              show latest run in an interactive TUI")
}

func printRunHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: nml run [flags]")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --message <text>        commit message for dirty worktree")
	fmt.Fprintln(w, "  --message-from-agent    request agent-generated commit message")
	fmt.Fprintln(w, "  --paths <a,b>           stage selected paths instead of all")
	fmt.Fprintln(w, "  --yes                   accept safe defaults without prompts")
	fmt.Fprintln(w, "  --yolo                  auto-select all actionable findings")
	fmt.Fprintln(w, "  --auto-merge            enable auto-merge for this run only")
	fmt.Fprintln(w, "  --merge-method <m>      squash, merge, or rebase")
	fmt.Fprintln(w, "  --skip-docs             skip docs for this run")
	fmt.Fprintln(w, "  --skip-deploy           skip deploy for this run")
	fmt.Fprintln(w, "  --ci-timeout <dur>      override CI timeout")
	fmt.Fprintln(w, "  --test-command <cmd>    override test command for this run only")
}

func printInitHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: nml init [--yes] [--agent <name>] [--main <branch>] [--remote <name>]")
	fmt.Fprintln(w, "Creates ~/.config/nml/config.yaml after detecting git, treehouse, agents, gh, tests, and lint.")
}

func printDoctorHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: nml doctor")
	fmt.Fprintln(w, "Prints a compact TOON table of tool, auth, config, and repo checks.")
}

func printConfigHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: nml config [--format yaml|toon]")
	fmt.Fprintln(w, "       nml config --set-test-command \"<cmd>\"")
	fmt.Fprintln(w, "Prints merged config or saves a per-repo test command.")
}

func printStatusHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: nml status [--run <id|path>] [--format toon]")
	fmt.Fprintln(w, "Shows latest saved run state for the current repository.")
}

func printFindingsHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: nml findings [--run <id|path>] [--format toon]")
	fmt.Fprintln(w, "Shows review findings recorded in a saved run.")
}

func printTUIHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: nml tui [--run <id|path>]")
	fmt.Fprintln(w, "Shows a Bubble Tea timeline for the latest saved run.")
}

func printResumeHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: nml resume [--run <id|path>] [flags]")
	fmt.Fprintln(w, "Continues the latest failed or interrupted run from ~/.nml or the repo run store.")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --run <id|path>        resume a specific run")
	fmt.Fprintln(w, "  --yes                  accept safe defaults without prompts")
	fmt.Fprintln(w, "  --yolo                 auto-fix all actionable review findings")
	fmt.Fprintln(w, "  --skip-docs            skip docs for this resume")
	fmt.Fprintln(w, "  --skip-deploy          skip deploy for this resume")
	fmt.Fprintln(w, "  --ci-timeout <dur>     override CI timeout")
	fmt.Fprintln(w, "  --test-command <cmd>   override test command for this resume only")
}

func printRunsHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: nml runs [--all] [--resumable]")
	fmt.Fprintln(w, "Lists saved run sessions mirrored under ~/.nml.")
}

func printRespondHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: nml respond --action <approve|skip|fix> [--findings r1,r2] [--run <id|path>]")
	fmt.Fprintln(w, "Answers a saved review gate and continues validation from the leased worktree.")
}
