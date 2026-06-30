package review

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Finding struct {
	ID          string   `json:"id"`
	Severity    Severity `json:"severity"`
	File        string   `json:"file"`
	Line        int      `json:"line,omitempty"`
	Description string   `json:"description"`
	Selected    bool     `json:"selected"`
}

type ParseResult struct {
	LGTM     bool
	Findings []Finding
	Ignored  []string
}

var bulletRE = regexp.MustCompile(`^- \[(error|warning|info)\] ([^\n:]+)(?::([0-9]+))? - (.+)$`)

func ParseMarkdown(output string) (ParseResult, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "LGTM" {
		return ParseResult{LGTM: true}, nil
	}
	var result ParseResult
	for _, raw := range strings.Split(trimmed, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		match := bulletRE.FindStringSubmatch(line)
		if match == nil {
			result.Ignored = append(result.Ignored, line)
			continue
		}
		lineNo := 0
		if match[3] != "" {
			parsed, err := strconv.Atoi(match[3])
			if err != nil {
				return result, err
			}
			lineNo = parsed
		}
		finding := Finding{
			ID:          fmt.Sprintf("r%d", len(result.Findings)+1),
			Severity:    Severity(match[1]),
			File:        strings.TrimSpace(match[2]),
			Line:        lineNo,
			Description: strings.TrimSpace(match[4]),
			Selected:    true,
		}
		result.Findings = append(result.Findings, finding)
	}
	SortFindings(result.Findings)
	if len(result.Findings) == 0 && len(result.Ignored) > 0 {
		return result, fmt.Errorf("review output did not contain LGTM or parseable findings")
	}
	return result, nil
}

func SortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		left := severityRank(findings[i].Severity)
		right := severityRank(findings[j].Severity)
		if left != right {
			return left < right
		}
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	for i := range findings {
		findings[i].ID = fmt.Sprintf("r%d", i+1)
	}
}

func severityRank(s Severity) int {
	switch s {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInfo:
		return 2
	default:
		return 3
	}
}

func Markdown(findings []Finding) string {
	var b strings.Builder
	for _, f := range findings {
		line := ""
		if f.Line > 0 {
			line = fmt.Sprintf(":%d", f.Line)
		}
		fmt.Fprintf(&b, "- [%s] %s%s - %s\n", f.Severity, f.File, line, f.Description)
	}
	return strings.TrimSpace(b.String())
}
