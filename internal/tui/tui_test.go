package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
	"github.com/natelindev/no-mistakes-lite/internal/review"
	"github.com/natelindev/no-mistakes-lite/internal/runstate"
)

func TestSelectModelSpaceSelectsCursor(t *testing.T) {
	m := selectModel{
		options:     []Option{{Label: "pi"}, {Label: "codex"}},
		cursor:      0,
		selected:    0,
		optionStart: 2,
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(selectModel)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(selectModel)
	if m.selected != 1 || m.cancelled {
		t.Fatalf("selected = %d cancelled = %v, want 1 false", m.selected, m.cancelled)
	}
}

func TestSelectModelMouseClickSelectsRow(t *testing.T) {
	m := selectModel{
		options:     []Option{{Label: "squash"}, {Label: "merge"}, {Label: "rebase"}},
		cursor:      0,
		selected:    0,
		optionStart: 2,
	}
	updated, _ := m.Update(tea.MouseMsg(tea.MouseEvent{X: 3, Y: 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}))
	m = updated.(selectModel)
	if m.cursor != 2 || m.selected != 2 || m.cancelled {
		t.Fatalf("cursor = %d selected = %d cancelled = %v, want 2 2 false", m.cursor, m.selected, m.cancelled)
	}
}

func TestSelectModelCancel(t *testing.T) {
	m := selectModel{options: []Option{{Label: "pi"}}, optionStart: 2}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(selectModel)
	if !m.cancelled {
		t.Fatal("expected cancelled")
	}
}

func TestSelectModelCtrlDCancel(t *testing.T) {
	m := selectModel{options: []Option{{Label: "pi"}}, optionStart: 2}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = updated.(selectModel)
	if !m.cancelled {
		t.Fatal("expected cancelled")
	}
}

func TestFindingSelectModelTogglesFinding(t *testing.T) {
	m := findingSelectModel{
		findings: []review.Finding{
			{ID: "r1", Severity: review.SeverityWarning, File: "a.go"},
			{ID: "r2", Severity: review.SeverityError, File: "b.go"},
		},
		selected:    map[int]bool{0: true, 1: true},
		optionStart: 2,
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(findingSelectModel)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(findingSelectModel)
	if m.selected[1] {
		t.Fatalf("second finding should be deselected")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(findingSelectModel)
	if !m.submitted || m.cancelled {
		t.Fatalf("submitted = %v cancelled = %v, want true false", m.submitted, m.cancelled)
	}
}

func TestFindingSelectModelToggleAll(t *testing.T) {
	m := findingSelectModel{
		findings: []review.Finding{{ID: "r1"}, {ID: "r2"}},
		selected: map[int]bool{0: true, 1: true},
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(findingSelectModel)
	if m.selected[0] || m.selected[1] {
		t.Fatalf("toggle all should deselect every finding")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(findingSelectModel)
	if !m.selected[0] || !m.selected[1] {
		t.Fatalf("toggle all should select every finding")
	}
}

func TestRenderReviewGateShowsFindingIDs(t *testing.T) {
	got := stripANSI(RenderReviewGate("run1", "/tmp/state.json", "/tmp/wt", []review.Finding{
		{ID: "r1", Severity: review.SeverityWarning, File: "app.go", Line: 12, Description: "fix me"},
	}))
	if !strings.Contains(got, "◻ r1 W app.go:12  fix me") {
		t.Fatalf("review gate should show finding id, got:\n%s", got)
	}
	if !strings.Contains(got, "nml tui --run run1") {
		t.Fatalf("review gate should suggest interactive TUI, got:\n%s", got)
	}
}

func TestInputModelAcceptsTypedValue(t *testing.T) {
	m := inputModel{defaultValue: "main"}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("develop")})
	m = updated.(inputModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(inputModel)
	if string(m.value) != "develop" || !m.submitted || m.cancelled {
		t.Fatalf("value = %q submitted = %v cancelled = %v", string(m.value), m.submitted, m.cancelled)
	}
}

func TestInputModelCtrlDCancel(t *testing.T) {
	m := inputModel{defaultValue: "main"}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = updated.(inputModel)
	if !m.cancelled {
		t.Fatal("expected cancelled")
	}
}

func TestRenderProgressStepIsLeftAligned(t *testing.T) {
	got := stripANSI(RenderProgressStep(runstate.StatusCompleted, "checking documentation", 0))
	want := "◇  checking documentation\n│\n"
	if got != want {
		t.Fatalf("progress step = %q, want %q", got, want)
	}
	if strings.Contains(got, "│  ◇") {
		t.Fatalf("progress step should not indent marker under branch line: %q", got)
	}
}

func TestTimelineSpacesStepsWithBranchLine(t *testing.T) {
	m := model{state: runstate.State{Steps: []runstate.Step{
		{Name: "test", Status: runstate.StatusCompleted, Detail: "passed"},
		{Name: "docs", Status: runstate.StatusRunning, Detail: "checking"},
	}}}
	got := stripANSI(m.timeline())
	if !strings.HasPrefix(got, "◇  test") {
		t.Fatalf("timeline should start with left aligned step marker, got %q", got)
	}
	if !strings.Contains(got, "\n│\n⠋  docs") {
		t.Fatalf("timeline should put a branch spacer between steps, got %q", got)
	}
	if strings.Contains(got, "│  ◇") || strings.Contains(got, "│  ⠋") {
		t.Fatalf("timeline should not indent step markers under branch line: %q", got)
	}
}

func TestRunningIndicatorIsAnimated(t *testing.T) {
	if !RunningIndicatorAnimated() {
		t.Fatal("running indicator should animate")
	}
}

func TestRunningIndicatorFramesStaySingleCellWithEastAsianWidth(t *testing.T) {
	cond := runewidth.NewCondition()
	cond.EastAsianWidth = true
	for _, frame := range runningIndicatorFrames {
		if got := cond.StringWidth(frame); got != 1 {
			t.Fatalf("running indicator frame %q has width %d, want 1", frame, got)
		}
	}
}

func TestTimelineKeepsMultilineDetailsOnBranch(t *testing.T) {
	m := model{state: runstate.State{Steps: []runstate.Step{
		{Name: "commit", Status: runstate.StatusCompleted, Detail: "subject\n1 file changed"},
	}}}
	got := stripANSI(m.timeline())
	if !strings.Contains(got, "◇  commit     subject\n│  1 file changed\n│\n") {
		t.Fatalf("timeline should keep multiline details connected to branch, got %q", got)
	}
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}
