package toon

import "testing"

func TestScalarQuotesAmbiguousStrings(t *testing.T) {
	cases := map[string]string{
		"":        `""`,
		"true":    `"true"`,
		"42":      `"42"`,
		"1E+9":    `"1E+9"`,
		"-value":  `"-value"`,
		"a,b":     `"a,b"`,
		"a:b":     `"a:b"`,
		"a\"b":    `"a\"b"`,
		"a\\b":    `"a\\b"`,
		" a":      `" a"`,
		"line\n2": `"line\n2"`,
	}
	for input, want := range cases {
		if got := Scalar(input); got != want {
			t.Fatalf("Scalar(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValueKeepsTypedBooleansAndNumbers(t *testing.T) {
	if got := Value(true); got != "true" {
		t.Fatalf("Value(true) = %q", got)
	}
	if got := Value(42); got != "42" {
		t.Fatalf("Value(42) = %q", got)
	}
	if got := Value("42"); got != `"42"` {
		t.Fatalf("Value string 42 = %q", got)
	}
}
