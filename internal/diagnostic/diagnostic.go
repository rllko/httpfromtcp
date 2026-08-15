// Package diagnostic
package diagnostic

import (
	"fmt"
	"strings"
)

type Diagnostic struct {
	File       string
	Line       int
	Message    string
	Source     string
	Start, End int
	Help       string
}

func Underline(s string, start, end int) string {
	if start < 0 {
		start = 0
	}

	if end < 0 {
		end = 0
	}

	if start > end {
		start = end
	}

	if start > len(s) {
		start = len(s)
	}

	if end > len(s) {
		end = len(s)
	}
	return fmt.Sprintf("%s\n%s%s", s, strings.Repeat(" ", start), strings.Repeat("^", end-start))
}

func (d Diagnostic) Render() string {
	b := strings.Builder{}
	u := Underline(d.Source, d.Start, d.End)

	fmt.Fprintf(&b, "error: %s\n", d.Message)

	if d.File != "" {
		fmt.Fprintf(&b, "  --> %s:%d\n", d.File, d.Line)
	}

	fmt.Fprintf(&b, "  |\n")

	for part := range strings.SplitSeq(u, "\n") {
		fmt.Fprintf(&b, "  | %s\n", part)
	}

	if d.Help != "" {
		fmt.Fprintf(&b, "  |\n")

		fmt.Fprintf(&b, "  = help: %s\n", d.Help)
	}

	return b.String()
}
