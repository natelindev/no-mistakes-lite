package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/natelindev/no-mistakes-lite/internal/review"
	"github.com/natelindev/no-mistakes-lite/internal/runstate"
)

type model struct {
	state   runstate.State
	quit    bool
	spinner int
}

type Option struct {
	Label       string
	Description string
}

type HomeState struct {
	Repo       string
	Branch     string
	State      string
	Reason     string
	Configured bool
	Noop       bool
}

type selectModel struct {
	title       string
	options     []Option
	cursor      int
	selected    int
	cancelled   bool
	optionStart int
}

type findingSelectModel struct {
	title       string
	findings    []review.Finding
	cursor      int
	selected    map[int]bool
	cancelled   bool
	submitted   bool
	optionStart int
}

type inputModel struct {
	title        string
	value        []rune
	defaultValue string
	cursor       int
	cancelled    bool
	submitted    bool
}

type HomeAction struct {
	Label       string
	Description string
	ID          string
}

type homeActionModel struct {
	state       HomeState
	actions     []HomeAction
	cursor      int
	selected    int
	cancelled   bool
	optionStart int
}

type tickMsg struct{}

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	activeStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	branchStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
)

var spinnerFrames = []string{"◐", "◓", "◑", "◒"}

func ShowRun(ctx context.Context, out io.Writer, state runstate.State) error {
	program := tea.NewProgram(model{state: state}, tea.WithOutput(out), tea.WithContext(ctx))
	_, err := program.Run()
	return err
}

func RenderHome(state HomeState) string {
	var b strings.Builder
	writeHomeStatus(&b, state)
	b.WriteString(activeStyle.Render("◆  Next actions"))
	b.WriteString("\n")
	for _, action := range homeActions(state) {
		b.WriteString(branchStyle.Render("│"))
		b.WriteString("  ◻ ")
		b.WriteString(action)
		b.WriteString("\n")
	}
	b.WriteString(branchStyle.Render("└"))
	b.WriteString(mutedStyle.Render("  run a command above to continue"))
	b.WriteString("\n")
	return b.String()
}

func RenderPipelineProgressHeader() string {
	var b strings.Builder
	b.WriteString(activeStyle.Render("◆  Pipeline"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	return b.String()
}

func RenderProgressStep(status runstate.StepStatus, label string, frame int) string {
	var b strings.Builder
	b.WriteString(renderProgressLine(status, label, "", frame))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	return b.String()
}

func RenderProgressInline(status runstate.StepStatus, label string, frame int) string {
	return renderProgressLine(status, label, "", frame)
}

func RenderRunResult(state runstate.State, path string) string {
	var b strings.Builder
	b.WriteString(activeStyle.Render("◆  NO MISTAKES LITE"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	b.WriteString(okStyle.Render("◇  Run saved"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("  Run: ")
	b.WriteString(state.ID)
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("  State path: ")
	b.WriteString(path)
	b.WriteString("\n")
	if state.PRURL != "" {
		b.WriteString(branchStyle.Render("│"))
		b.WriteString("  PR: ")
		b.WriteString(state.PRURL)
		b.WriteString("\n")
	}
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	b.WriteString(RenderPipelineProgressHeader())
	for _, step := range state.Steps {
		writeProgressStep(&b, step.Status, step.Name, step.Detail, 0)
	}
	b.WriteString(activeStyle.Render("◆  Next actions"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("  ◻ nml status --run ")
	b.WriteString(state.ID)
	b.WriteString("\n")
	if state.WorktreePath != "" {
		b.WriteString(branchStyle.Render("│"))
		b.WriteString("  ◻ treehouse return ")
		b.WriteString(state.WorktreePath)
		b.WriteString(" --force")
		b.WriteString("\n")
	}
	b.WriteString(branchStyle.Render("└"))
	b.WriteString(mutedStyle.Render("  pipeline result"))
	b.WriteString("\n")
	return b.String()
}

func RenderReviewGate(runID, statePath, worktreePath string, findings []review.Finding) string {
	var b strings.Builder
	b.WriteString(activeStyle.Render("◆  Review findings"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	for _, finding := range findings {
		line := fmt.Sprintf("│  ◻ %s %s %s", finding.ID, severityInitial(finding), finding.File)
		if finding.Line > 0 {
			line += fmt.Sprintf(":%d", finding.Line)
		}
		line += "  " + finding.Description
		b.WriteString(styleForFinding(finding).Render(line))
		b.WriteString("\n")
	}
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	b.WriteString(activeStyle.Render("◆  Next actions"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("  ◻ nml tui --run ")
	b.WriteString(runID)
	b.WriteString("  # choose approve, skip, or findings to fix")
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("  ◻ nml respond --action fix --findings <ids> --run ")
	b.WriteString(runID)
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("  ◻ nml respond --action approve --run ")
	b.WriteString(runID)
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("  ◻ nml respond --action skip --run ")
	b.WriteString(runID)
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("  State path: ")
	b.WriteString(statePath)
	b.WriteString("\n")
	if worktreePath != "" {
		b.WriteString(branchStyle.Render("│"))
		b.WriteString("  Worktree: ")
		b.WriteString(worktreePath)
		b.WriteString("\n")
	}
	b.WriteString(branchStyle.Render("└"))
	b.WriteString(mutedStyle.Render("  review gate"))
	b.WriteString("\n")
	return b.String()
}

func writeHomeStatus(b *strings.Builder, state HomeState) {
	b.WriteString(activeStyle.Render("◆  NO MISTAKES LITE"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	b.WriteString(okStyle.Render("◇  Repository status"))
	b.WriteString("\n")
	if state.Repo != "" {
		b.WriteString(branchStyle.Render("│"))
		b.WriteString("  Repo: ")
		b.WriteString(state.Repo)
		b.WriteString("\n")
	}
	if state.Branch != "" {
		b.WriteString(branchStyle.Render("│"))
		b.WriteString("  Branch: ")
		b.WriteString(state.Branch)
		b.WriteString("\n")
	}
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("  State: ")
	b.WriteString(state.State)
	b.WriteString("\n")
	if state.Reason != "" {
		b.WriteString(branchStyle.Render("│"))
		b.WriteString("  ")
		b.WriteString(state.Reason)
		b.WriteString("\n")
	}
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
}

func homeActions(state HomeState) []string {
	var actions []string
	if !state.Configured {
		actions = append(actions, "nml init")
	}
	switch state.State {
	case "dirty":
		actions = append(actions, "nml run --message \"<commit message>\"")
	case "feature_delta", "main_ahead":
		actions = append(actions, "nml run")
	case "needs_remote_base":
		actions = append(actions, "git fetch origin main")
	case "no_repo":
		actions = append(actions, "cd <git-repository>")
	default:
		if state.Noop {
			actions = append(actions, "nml doctor")
		} else {
			actions = append(actions, "nml run")
		}
	}
	actions = append(actions, "nml doctor")
	return dedupe(actions)
}

func homeOptionStart(state HomeState) int {
	start := 7
	if state.Repo != "" {
		start++
	}
	if state.Branch != "" {
		start++
	}
	if state.Reason != "" {
		start++
	}
	return start
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

func SelectOne(ctx context.Context, in io.Reader, out io.Writer, title string, options []Option, initial int) (int, bool, error) {
	if len(options) == 0 {
		return -1, false, fmt.Errorf("no options available")
	}
	if initial < 0 || initial >= len(options) {
		initial = 0
	}
	program := tea.NewProgram(selectModel{
		title:       title,
		options:     options,
		cursor:      initial,
		selected:    initial,
		optionStart: 2,
	}, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(ctx), tea.WithMouseCellMotion())
	final, err := program.Run()
	if err != nil {
		return -1, false, err
	}
	m, ok := final.(selectModel)
	if !ok {
		return -1, false, fmt.Errorf("unexpected picker state")
	}
	if m.cancelled {
		return -1, true, nil
	}
	return m.selected, false, nil
}

func SelectFindings(ctx context.Context, in io.Reader, out io.Writer, title string, findings []review.Finding) ([]review.Finding, bool, error) {
	if len(findings) == 0 {
		return nil, false, fmt.Errorf("no findings available")
	}
	selected := make(map[int]bool, len(findings))
	for i := range findings {
		selected[i] = true
	}
	program := tea.NewProgram(findingSelectModel{
		title:       title,
		findings:    findings,
		selected:    selected,
		optionStart: 2,
	}, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(ctx), tea.WithMouseCellMotion())
	final, err := program.Run()
	if err != nil {
		return nil, false, err
	}
	m, ok := final.(findingSelectModel)
	if !ok {
		return nil, false, fmt.Errorf("unexpected finding picker state")
	}
	if m.cancelled {
		return nil, true, nil
	}
	var outFindings []review.Finding
	for i, finding := range m.findings {
		if m.selected[i] {
			finding.Selected = true
			outFindings = append(outFindings, finding)
		}
	}
	return outFindings, false, nil
}

func Input(ctx context.Context, in io.Reader, out io.Writer, title, defaultValue string) (string, bool, error) {
	program := tea.NewProgram(inputModel{
		title:        title,
		defaultValue: defaultValue,
	}, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(ctx))
	final, err := program.Run()
	if err != nil {
		return "", false, err
	}
	m, ok := final.(inputModel)
	if !ok {
		return "", false, fmt.Errorf("unexpected input state")
	}
	if m.cancelled {
		return "", true, nil
	}
	value := strings.TrimSpace(string(m.value))
	if value == "" {
		return defaultValue, false, nil
	}
	return value, false, nil
}

func SelectHomeAction(ctx context.Context, in io.Reader, out io.Writer, state HomeState, actions []HomeAction) (string, bool, error) {
	if len(actions) == 0 {
		fmt.Fprint(out, RenderHome(state))
		return "", false, nil
	}
	program := tea.NewProgram(homeActionModel{
		state:       state,
		actions:     actions,
		optionStart: homeOptionStart(state),
	}, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(ctx), tea.WithMouseCellMotion())
	final, err := program.Run()
	if err != nil {
		return "", false, err
	}
	m, ok := final.(homeActionModel)
	if !ok {
		return "", false, fmt.Errorf("unexpected home action state")
	}
	if m.cancelled {
		return "", true, nil
	}
	if m.selected < 0 || m.selected >= len(actions) {
		return "", true, nil
	}
	return actions[m.selected].ID, false, nil
}

func (m model) Init() tea.Cmd {
	if m.hasActiveStep() {
		return nextTick()
	}
	return nil
}

func (m selectModel) Init() tea.Cmd { return nil }

func (m findingSelectModel) Init() tea.Cmd { return nil }

func (m inputModel) Init() tea.Cmd { return nil }

func (m homeActionModel) Init() tea.Cmd { return nil }

func (m homeActionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+d", "q", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.actions)-1 {
				m.cursor++
			}
		case " ", "enter":
			m.selected = m.cursor
			return m, tea.Quit
		}
	case tea.MouseMsg:
		mouse := tea.MouseEvent(msg)
		if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
			idx := mouse.Y - m.optionStart
			if idx >= 0 && idx < len(m.actions) {
				m.cursor = idx
				m.selected = idx
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m homeActionModel) View() string {
	var b strings.Builder
	writeHomeStatus(&b, m.state)
	b.WriteString(activeStyle.Render("◆  Choose next action (space or enter to select)"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	for i, action := range m.actions {
		box := "◻"
		if i == m.cursor {
			box = "◼"
		}
		line := fmt.Sprintf("│  %s %s", box, action.Label)
		if action.Description != "" {
			line += mutedStyle.Render("  " + action.Description)
		}
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(branchStyle.Render("│") + line[3:])
		}
		b.WriteString("\n")
	}
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("└"))
	b.WriteString(mutedStyle.Render("  j/k or arrows move, space/enter select, click select, ctrl+c/ctrl+d/q cancel"))
	b.WriteString("\n")
	return b.String()
}

func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEnter:
			m.submitted = true
			return m, tea.Quit
		case tea.KeyLeft:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyRight:
			if m.cursor < len(m.value) {
				m.cursor++
			}
		case tea.KeyHome:
			m.cursor = 0
		case tea.KeyEnd:
			m.cursor = len(m.value)
		case tea.KeyBackspace, tea.KeyCtrlH:
			if m.cursor > 0 {
				m.value = append(m.value[:m.cursor-1], m.value[m.cursor:]...)
				m.cursor--
			}
		case tea.KeyDelete:
			if m.cursor < len(m.value) {
				m.value = append(m.value[:m.cursor], m.value[m.cursor+1:]...)
			}
		case tea.KeyCtrlU:
			m.value = nil
			m.cursor = 0
		case tea.KeyRunes:
			m.value = append(m.value[:m.cursor], append(msg.Runes, m.value[m.cursor:]...)...)
			m.cursor += len(msg.Runes)
		case tea.KeySpace:
			m.value = append(m.value[:m.cursor], append([]rune{' '}, m.value[m.cursor:]...)...)
			m.cursor++
		}
	}
	return m, nil
}

func (m inputModel) View() string {
	value := string(m.value)
	if value == "" && m.defaultValue != "" {
		value = mutedStyle.Render(m.defaultValue)
	}
	if string(m.value) != "" {
		runes := []rune(string(m.value))
		cursor := m.cursor
		if cursor < 0 {
			cursor = 0
		}
		if cursor > len(runes) {
			cursor = len(runes)
		}
		left := string(runes[:cursor])
		right := string(runes[cursor:])
		value = left + activeStyle.Render("▌") + right
	} else {
		value = activeStyle.Render("▌") + value
	}
	var b strings.Builder
	b.WriteString(activeStyle.Render("◆  " + m.title))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("  ")
	b.WriteString(value)
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("└"))
	b.WriteString(mutedStyle.Render("  enter accept, ctrl+u clear, ctrl+c/ctrl+d/esc cancel"))
	b.WriteString("\n")
	return b.String()
}

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+d", "q", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case " ":
			m.selected = m.cursor
			return m, tea.Quit
		case "enter":
			m.selected = m.cursor
			return m, tea.Quit
		}
	case tea.MouseMsg:
		mouse := tea.MouseEvent(msg)
		if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
			idx := mouse.Y - m.optionStart
			if idx >= 0 && idx < len(m.options) {
				m.cursor = idx
				m.selected = idx
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m findingSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+d", "q", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.findings)-1 {
				m.cursor++
			}
		case " ":
			m.selected[m.cursor] = !m.selected[m.cursor]
		case "a":
			m.toggleAll()
		case "enter":
			m.submitted = true
			return m, tea.Quit
		}
	case tea.MouseMsg:
		mouse := tea.MouseEvent(msg)
		if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
			idx := mouse.Y - m.optionStart
			if idx >= 0 && idx < len(m.findings) {
				m.cursor = idx
				m.selected[idx] = !m.selected[idx]
			}
		}
	}
	return m, nil
}

func (m findingSelectModel) toggleAll() {
	allSelected := true
	for i := range m.findings {
		if !m.selected[i] {
			allSelected = false
			break
		}
	}
	for i := range m.findings {
		m.selected[i] = !allSelected
	}
}

func (m findingSelectModel) View() string {
	var b strings.Builder
	b.WriteString(activeStyle.Render("◆  " + m.title + " (space toggles, enter fixes)"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	for i, finding := range m.findings {
		box := "◻"
		if m.selected[i] {
			box = "◼"
		}
		line := fmt.Sprintf("│  %s %s %s %s", box, finding.ID, severityInitial(finding), finding.File)
		if finding.Line > 0 {
			line += fmt.Sprintf(":%d", finding.Line)
		}
		if finding.Description != "" {
			line += mutedStyle.Render("  " + finding.Description)
		}
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(styleForFinding(finding).Render(line))
		}
		b.WriteString("\n")
	}
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("└"))
	b.WriteString(mutedStyle.Render("  j/k or arrows move, space/click toggle, a toggle all, enter fix, ctrl+c/ctrl+d/q cancel"))
	b.WriteString("\n")
	return b.String()
}

func (m selectModel) View() string {
	var b strings.Builder
	b.WriteString(activeStyle.Render("◆  " + m.title + " (space or enter to select)"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	for i, option := range m.options {
		box := "◻"
		if i == m.selected {
			box = "◼"
		}
		line := fmt.Sprintf("│  %s %s", box, option.Label)
		if option.Description != "" {
			line += mutedStyle.Render("  " + option.Description)
		}
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(branchStyle.Render("│") + line[3:])
		}
		b.WriteString("\n")
	}
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("└"))
	b.WriteString(mutedStyle.Render("  j/k or arrows move, space/enter select, click select, ctrl+c/ctrl+d/q cancel"))
	b.WriteString("\n")
	return b.String()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c", "ctrl+d":
			m.quit = true
			return m, tea.Quit
		}
	case tickMsg:
		m.spinner++
		if m.hasActiveStep() {
			return m, nextTick()
		}
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(activeStyle.Render("◆  NO MISTAKES LITE"))
	b.WriteString("\n")
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
	b.WriteString(m.summary())
	b.WriteString("\n")
	b.WriteString(RenderPipelineProgressHeader())
	b.WriteString(m.timeline())
	b.WriteString(branchStyle.Render("└"))
	b.WriteString(mutedStyle.Render("  q/esc/ctrl+c/ctrl+d quit"))
	b.WriteString("\n")
	return b.String()
}

func (m model) summary() string {
	lines := []string{
		fmt.Sprintf("◇  Run %s", m.state.ID),
		fmt.Sprintf("│  Repo: %s", m.state.RepoRoot),
		fmt.Sprintf("│  Source: %s", m.state.SourceBranch),
		fmt.Sprintf("│  Review: %s", m.state.ReviewBranch),
		fmt.Sprintf("│  Worktree: %s", m.state.WorktreePath),
	}
	if m.state.PRURL != "" {
		lines = append(lines, "│  PR: "+m.state.PRURL)
	}
	lines = append(lines, "│")
	return branchLines(lines)
}

func (m model) timeline() string {
	var b strings.Builder
	for _, step := range m.state.Steps {
		writeProgressStep(&b, step.Status, step.Name, step.Detail, m.spinner)
	}
	return b.String()
}

func (m model) hasActiveStep() bool {
	for _, step := range m.state.Steps {
		if step.Status == runstate.StatusRunning || step.Status == runstate.StatusFixing {
			return true
		}
	}
	return false
}

func nextTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func statusMarker(status runstate.StepStatus, frame int) string {
	switch status {
	case runstate.StatusCompleted:
		return "◇"
	case runstate.StatusSkipped:
		return "◇"
	case runstate.StatusFailed, runstate.StatusCancelled:
		return "✖"
	case runstate.StatusRunning, runstate.StatusFixing:
		return spinnerFrames[frame%len(spinnerFrames)]
	case runstate.StatusAwaitingUser:
		return "◆"
	default:
		return "◌"
	}
}

func severityInitial(finding review.Finding) string {
	severity := string(finding.Severity)
	if severity == "" {
		return "?"
	}
	return strings.ToUpper(string(severity[:1]))
}

func styleForFinding(finding review.Finding) lipgloss.Style {
	switch finding.Severity {
	case review.SeverityError:
		return errStyle
	case review.SeverityWarning:
		return warnStyle
	default:
		return mutedStyle
	}
}

func styleForStatus(status runstate.StepStatus) lipgloss.Style {
	switch status {
	case runstate.StatusCompleted:
		return okStyle
	case runstate.StatusSkipped:
		return mutedStyle
	case runstate.StatusAwaitingUser, runstate.StatusRunning, runstate.StatusFixing:
		return warnStyle
	case runstate.StatusFailed, runstate.StatusCancelled:
		return errStyle
	default:
		return mutedStyle
	}
}

func writeProgressStep(b *strings.Builder, status runstate.StepStatus, name, detail string, frame int) {
	detailLines := splitDetailLines(detail)
	firstDetail := ""
	if len(detailLines) > 0 {
		firstDetail = detailLines[0]
	}
	b.WriteString(renderProgressLine(status, name, firstDetail, frame))
	b.WriteString("\n")
	for _, line := range detailLines[1:] {
		b.WriteString(branchStyle.Render("│"))
		b.WriteString("  ")
		b.WriteString(mutedStyle.Render(line))
		b.WriteString("\n")
	}
	b.WriteString(branchStyle.Render("│"))
	b.WriteString("\n")
}

func renderProgressLine(status runstate.StepStatus, name, detail string, frame int) string {
	marker := statusMarker(status, frame)
	name = strings.TrimSpace(name)
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return styleForStatus(status).Render(fmt.Sprintf("%s  %s", marker, name))
	}
	line := fmt.Sprintf("%s  %-10s %s", marker, name, detail)
	return styleForStatus(status).Render(strings.TrimRight(line, " "))
}

func splitDetailLines(detail string) []string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return nil
	}
	parts := strings.Split(detail, "\n")
	for i, part := range parts {
		parts[i] = strings.TrimRight(part, " \t\r")
	}
	return parts
}

func branchLines(lines []string) string {
	for i, line := range lines {
		if strings.HasPrefix(line, "│") {
			lines[i] = branchStyle.Render("│") + line[3:]
		}
	}
	return strings.Join(lines, "\n")
}
