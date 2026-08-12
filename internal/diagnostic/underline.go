// Package diagnostic
package diagnostic

import (
	"fmt"
	"strings"
)

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
	fmt.Println(render("wildcard segment must be last", fmt.Sprintf("%s\n%s%s", s, strings.Repeat(" ", start), strings.Repeat("^", end-start))))
	return fmt.Sprintf("%s\n%s%s", s, strings.Repeat(" ", start), strings.Repeat("^", end-start))
}

func render(msg, body string) string {
	b := strings.Builder{}

	fmt.Fprintf(&b, "error: %s\n", msg)
	fmt.Fprintf(&b, "  | \n")
	for part := range strings.SplitSeq(body, "\n") {
		fmt.Fprintf(&b, "  | %s\n", part)
	}

	fmt.Fprintf(&b, "  |\n")

	return b.String()
}
