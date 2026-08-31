package gen

import (
	"fmt"
	"strings"
)

// Diagnostic is a compile- or load-time message. Errors fail the build.
type Diagnostic struct {
	Severity string // "error" | "warning"
	Code     string
	Message  string
	Where    string // optional source hint (file, tool, view, component)
}

func errf(code, where, format string, args ...any) Diagnostic {
	return Diagnostic{Severity: "error", Code: code, Message: fmt.Sprintf(format, args...), Where: where}
}

func warnf(code, where, format string, args ...any) Diagnostic {
	return Diagnostic{Severity: "warning", Code: code, Message: fmt.Sprintf(format, args...), Where: where}
}

// HasErrors reports whether any diagnostic is an error.
func HasErrors(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

// CountErrors returns the number of error-severity diagnostics.
func CountErrors(diags []Diagnostic) int {
	n := 0
	for _, d := range diags {
		if d.Severity == "error" {
			n++
		}
	}
	return n
}

// Format renders diagnostics for the terminal.
func Format(diags []Diagnostic) string {
	var b strings.Builder
	for i, d := range diags {
		if i > 0 {
			b.WriteByte('\n')
		}
		mark := "⚠"
		if d.Severity == "error" {
			mark = "✗"
		}
		b.WriteString(mark)
		b.WriteString(" [")
		b.WriteString(d.Code)
		b.WriteString("] ")
		b.WriteString(d.Message)
		if d.Where != "" {
			b.WriteString(" (")
			b.WriteString(d.Where)
			b.WriteString(")")
		}
	}
	return b.String()
}
