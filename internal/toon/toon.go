package toon

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Row []string

func KV(w io.Writer, key string, value any) {
	fmt.Fprintf(w, "%s: %s\n", key, scalar(fmt.Sprint(value)))
}

func Table(w io.Writer, name string, cols []string, rows []Row) {
	fmt.Fprintf(w, "%s[%d]{%s}:\n", name, len(rows), strings.Join(cols, ","))
	for _, row := range rows {
		vals := make([]string, len(row))
		for i, v := range row {
			vals[i] = scalar(v)
		}
		fmt.Fprintf(w, "  %s\n", strings.Join(vals, ","))
	}
}

func List(w io.Writer, name string, items []string) {
	fmt.Fprintf(w, "%s[%d]:\n", name, len(items))
	for _, item := range items {
		fmt.Fprintf(w, "  %s\n", scalar(item))
	}
}

func Error(w io.Writer, message string, help []string) {
	KV(w, "error", message)
	if len(help) > 0 {
		List(w, "help", help)
	}
}

func scalar(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	needsQuote := strings.ContainsAny(s, ",\n:#[]{}") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ")
	if !needsQuote {
		return s
	}
	return strconv.Quote(s)
}
