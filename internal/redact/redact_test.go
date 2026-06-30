package redact

import (
	"strings"
	"testing"
)

func TestSecretsRedactsKnownTokens(t *testing.T) {
	githubToken := "ghp_" + "redaction_test_value_with_underscores"
	input := "token " + githubToken + " and bearer redaction-test-bearer-value"
	got := Secrets(input)
	if strings.Contains(got, githubToken) {
		t.Fatalf("github token was not redacted: %s", got)
	}
	if strings.Contains(got, "redaction-test-bearer-value") {
		t.Fatalf("bearer token was not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redaction marker in %q", got)
	}
}

func TestSecretsRedactsBasicAuthURL(t *testing.T) {
	got := Secrets("fetch https://user:pass@example.com/repo.git")
	if strings.Contains(got, "user:pass") {
		t.Fatalf("basic auth credentials were not redacted: %s", got)
	}
	if !strings.Contains(got, "https://[REDACTED]example.com") {
		t.Fatalf("unexpected URL redaction: %s", got)
	}
}
