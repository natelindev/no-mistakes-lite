package prbody

import (
	"fmt"
	"strings"
)

type StepSummary struct {
	Name   string
	Status string
	Detail string
}

type Input struct {
	OriginalIntent string
	WhatChanged    []string
	Review         []StepSummary
	Tests          []StepSummary
	Lint           []StepSummary
	Docs           []StepSummary
	CI             []StepSummary
}

func Generate(in Input) string {
	var b strings.Builder
	section(&b, "Original Intent", textOrFallback(in.OriginalIntent, "Not recorded."))
	bullets(&b, "What Changed", in.WhatChanged, "Change summary not available.")
	steps(&b, "Review", in.Review, "Review has not run yet.")
	var testLint []StepSummary
	testLint = append(testLint, in.Tests...)
	testLint = append(testLint, in.Lint...)
	steps(&b, "Tests and Lint", testLint, "Tests and lint have not run yet.")
	steps(&b, "Docs", in.Docs, "Docs step has not run yet.")
	steps(&b, "CI", in.CI, "CI is pending.")
	return strings.TrimSpace(b.String()) + "\n"
}

func section(b *strings.Builder, title, body string) {
	fmt.Fprintf(b, "## %s\n\n%s\n\n", title, strings.TrimSpace(body))
}

func bullets(b *strings.Builder, title string, items []string, fallback string) {
	fmt.Fprintf(b, "## %s\n\n", title)
	if len(items) == 0 {
		fmt.Fprintf(b, "%s\n\n", fallback)
		return
	}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		fmt.Fprintf(b, "- %s\n", item)
	}
	b.WriteString("\n")
}

func steps(b *strings.Builder, title string, steps []StepSummary, fallback string) {
	fmt.Fprintf(b, "## %s\n\n", title)
	if len(steps) == 0 {
		fmt.Fprintf(b, "%s\n\n", fallback)
		return
	}
	for _, step := range steps {
		name := strings.TrimSpace(step.Name)
		if name == "" {
			name = title
		}
		detail := strings.TrimSpace(step.Detail)
		if detail == "" {
			fmt.Fprintf(b, "- %s: %s\n", name, step.Status)
		} else {
			fmt.Fprintf(b, "- %s: %s - %s\n", name, step.Status, detail)
		}
	}
	b.WriteString("\n")
}

func textOrFallback(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return strings.TrimSpace(s)
}
