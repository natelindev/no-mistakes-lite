package prbody

import (
	"strings"
	"testing"
)

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
