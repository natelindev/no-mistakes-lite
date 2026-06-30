package redact

import "regexp"

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(gh[pousr]_[A-Za-z0-9_]{20,})`),
	regexp.MustCompile(`(?i)(github_pat_[A-Za-z0-9_]{20,})`),
	regexp.MustCompile(`(?i)(npm_[A-Za-z0-9]{20,})`),
	regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16})`),
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-+/=]{12,}`),
	regexp.MustCompile(`(?i)(https?://)[^\s:/@]+:[^\s/@]+@`),
}

func Secrets(s string) string {
	for _, re := range patterns {
		s = re.ReplaceAllStringFunc(s, func(match string) string {
			sub := re.FindStringSubmatch(match)
			if len(sub) > 1 && len(sub[1]) < len(match) {
				return sub[1] + "[REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return s
}
