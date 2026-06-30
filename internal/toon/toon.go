package toon

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Row []string

var (
	identifierRE  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
	numericLikeRE = regexp.MustCompile(`(?i)^-?\d+(?:\.\d+)?(?:e[+-]?\d+)?$`)
)

func KV(w io.Writer, key string, value any) {
	fmt.Fprintf(w, "%s: %s\n", Key(key), Value(value))
}

func Table(w io.Writer, name string, cols []string, rows []Row) {
	encodedCols := make([]string, len(cols))
	for i, col := range cols {
		encodedCols[i] = Key(col)
	}
	fmt.Fprintf(w, "%s[%d]{%s}:\n", Key(name), len(rows), strings.Join(encodedCols, ","))
	for _, row := range rows {
		vals := make([]string, len(row))
		for i, v := range row {
			vals[i] = Scalar(v)
		}
		fmt.Fprintf(w, "  %s\n", strings.Join(vals, ","))
	}
}

func List(w io.Writer, name string, items []string) {
	fmt.Fprintf(w, "%s[%d]:\n", Key(name), len(items))
	for _, item := range items {
		fmt.Fprintf(w, "  %s\n", Scalar(item))
	}
}

func Error(w io.Writer, message string, help []string) {
	KV(w, "error", message)
	if len(help) > 0 {
		List(w, "help", help)
	}
}

func Key(s string) string {
	if identifierRE.MatchString(s) {
		return s
	}
	return quote(s)
}

func Value(v any) string {
	switch value := v.(type) {
	case bool:
		if value {
			return "true"
		}
		return "false"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64:
		return fmt.Sprint(value)
	default:
		return Scalar(fmt.Sprint(value))
	}
}

func Scalar(s string) string {
	if needsQuote(s) {
		return quote(s)
	}
	return s
}

func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	if strings.TrimSpace(s) != s {
		return true
	}
	switch s {
	case "true", "false", "null":
		return true
	}
	if numericLikeRE.MatchString(s) {
		return true
	}
	if strings.HasPrefix(s, "-") {
		return true
	}
	if strings.ContainsAny(s, ",:\"\\[]{}") {
		return true
	}
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r < 0x20 {
			return true
		}
		s = s[size:]
	}
	return false
}

func quote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
