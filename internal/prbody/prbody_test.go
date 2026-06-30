package prbody

import (
	"strings"
	"testing"
)

func TestGenerateIndentsMultilineStepDetails(t *testing.T) {
	body := Generate(Input{
		Review: []StepSummary{{Name: "Round 1", Status: "findings with 2 findings", Detail: "- `r1` WARNING `a.go:1` - fix a\n- `r2` ERROR `b.go:2` - fix b"}},
	})
	for _, want := range []string{"- Round 1: findings with 2 findings", "  - `r1` WARNING `a.go:1` - fix a", "  - `r2` ERROR `b.go:2` - fix b"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func TestGenerateIncludesRequiredSections(t *testing.T) {
	body := Generate(Input{
		OriginalIntent: "Handle empty input without crashing.",
		WhatChanged:    []string{"Added an empty input guard"},
		Review:         []StepSummary{{Name: "Round 1", Status: "LGTM"}},
		Tests:          []StepSummary{{Name: "Test", Status: "passed", Detail: "go test ./..."}},
	})
	for _, section := range []string{"## Original Intent", "## What Changed", "## Review", "## Tests and Lint", "## Docs", "## CI"} {
		if !strings.Contains(body, section) {
			t.Fatalf("missing section %s in body:\n%s", section, body)
		}
	}
	if !strings.Contains(body, "Handle empty input") {
		t.Fatalf("intent missing from body:\n%s", body)
	}
}
