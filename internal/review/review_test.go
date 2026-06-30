package review

import "testing"

func TestParseLGTM(t *testing.T) {
	result, err := ParseMarkdown("\nLGTM\n")
	if err != nil {
		t.Fatalf("ParseMarkdown returned error: %v", err)
	}
	if !result.LGTM || len(result.Findings) != 0 {
		t.Fatalf("expected LGTM with no findings, got %#v", result)
	}
}

func TestParseFindingsSortsBySeverity(t *testing.T) {
	input := `- [info] z.go:10 - simplify this
- [error] a.go:2 - nil panic
- [warning] b.go - needs user judgment`
	result, err := ParseMarkdown(input)
	if err != nil {
		t.Fatalf("ParseMarkdown returned error: %v", err)
	}
	if result.LGTM {
		t.Fatal("expected findings, got LGTM")
	}
	if len(result.Findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(result.Findings))
	}
	if result.Findings[0].Severity != SeverityError || result.Findings[0].File != "a.go" || result.Findings[0].Line != 2 {
		t.Fatalf("unexpected first finding: %#v", result.Findings[0])
	}
	if result.Findings[2].Severity != SeverityInfo || result.Findings[2].ID != "r3" {
		t.Fatalf("unexpected final finding: %#v", result.Findings[2])
	}
}

func TestParseRejectsUnknownOutput(t *testing.T) {
	_, err := ParseMarkdown("Looks good except maybe tests.")
	if err == nil {
		t.Fatal("expected parse error")
	}
}
