package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const maxColumnWidth = 36
const columnSep = "  "

const (
	ansiReset  = "\x1b[0m"
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

// Formatter provides color-aware output formatting with injectable TTY detection.
type Formatter struct {
	isTerminal func(*os.File) bool
}

// NewFormatter returns a Formatter with real OS TTY detection.
func NewFormatter() *Formatter {
	return &Formatter{isTerminal: isTerminalFd}
}

// isTerminalFd reports whether f is connected to a character device (TTY).
func isTerminalFd(f *os.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// colorEnabled returns true if ANSI colors should be emitted for the given file.
// Colors are disabled when NO_COLOR is set (to any value including empty) or
// when the output is not a TTY.
func (f *Formatter) colorEnabled(out *os.File) bool {
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return false
	}
	return f.isTerminal(out)
}

// Green wraps s in green ANSI codes if colors are enabled for out.
func (f *Formatter) Green(out *os.File, s string) string {
	if f.colorEnabled(out) {
		return ansiGreen + s + ansiReset
	}
	return s
}

// Red wraps s in red ANSI codes if colors are enabled for out.
func (f *Formatter) Red(out *os.File, s string) string {
	if f.colorEnabled(out) {
		return ansiRed + s + ansiReset
	}
	return s
}

// Yellow wraps s in yellow ANSI codes if colors are enabled for out.
func (f *Formatter) Yellow(out *os.File, s string) string {
	if f.colorEnabled(out) {
		return ansiYellow + s + ansiReset
	}
	return s
}

// Cyan wraps s in cyan ANSI codes if colors are enabled for out.
func (f *Formatter) Cyan(out *os.File, s string) string {
	if f.colorEnabled(out) {
		return ansiCyan + s + ansiReset
	}
	return s
}

// TruncateColumn truncates s to at most maxColumnWidth runes. When truncation
// occurs, the last rune is replaced with the ellipsis character '…'.
func TruncateColumn(s string) string {
	if utf8.RuneCountInString(s) <= maxColumnWidth {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxColumnWidth-1]) + "…"
}

// FormatTimestamp formats t as "YYYY-MM-DD HH:MM".
func FormatTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

// PrintPrompt writes the interactive input prompt "> " to w with green color when w is a TTY.
func PrintPrompt(w io.Writer) {
	f, ok := w.(*os.File)
	if ok {
		fmtr := NewFormatter()
		fmt.Fprint(w, fmtr.Green(f, "> "))
	} else {
		fmt.Fprint(w, "> ")
	}
}

// FormatTasksTable formats tasks as an aligned columnar table with a header row.
func FormatTasksTable(tasks []TaskSummary) string {
	headers := []string{"ID", "STATUS", "AGENT", "CREATED"}
	rows := make([][]string, len(tasks))
	for i, task := range tasks {
		rows[i] = []string{task.ID, task.Status, task.Agent, FormatTimestamp(task.CreatedAt)}
	}
	return renderTable(headers, rows)
}

// FormatSessionsTable formats sessions as an aligned columnar table with a header row.
func FormatSessionsTable(sessions []SessionSummary) string {
	headers := []string{"SESSION ID", "AGENT", "MODEL", "CREATED", "LAST MESSAGE"}
	rows := make([][]string, len(sessions))
	for i, sess := range sessions {
		rows[i] = []string{
			sess.SessionID, sess.Agent, sess.Model,
			FormatTimestamp(sess.CreatedAt),
			FormatTimestamp(sess.LastMessageAt),
		}
	}
	return renderTable(headers, rows)
}

// renderTable produces a header row followed by data rows, with each column
// padded to a consistent width determined by the widest value in that column.
func renderTable(headers []string, rows [][]string) string {
	colCount := len(headers)
	widths := make([]int, colCount)
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i := 0; i < colCount && i < len(row); i++ {
			w := utf8.RuneCountInString(TruncateColumn(row[i]))
			if w > widths[i] {
				widths[i] = w
			}
		}
	}

	var sb strings.Builder
	for i, h := range headers {
		if i > 0 {
			sb.WriteString(columnSep)
		}
		sb.WriteString(padRight(h, widths[i]))
	}
	sb.WriteByte('\n')

	for _, row := range rows {
		for i := 0; i < colCount; i++ {
			if i > 0 {
				sb.WriteString(columnSep)
			}
			var cell string
			if i < len(row) {
				cell = TruncateColumn(row[i])
			}
			sb.WriteString(padRight(cell, widths[i]))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// padRight pads s with trailing spaces to at least w runes wide.
func padRight(s string, w int) string {
	n := utf8.RuneCountInString(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

// FormatWorkflowsTable formats workflows as an aligned columnar table with a header row.
func FormatWorkflowsTable(workflows []WorkflowSummary) string {
	headers := []string{"ID", "PIPELINE", "STATUS", "CREATED", "DURATION"}
	rows := make([][]string, len(workflows))
	for i, wf := range workflows {
		id := wf.ID
		if len(id) > 8 {
			id = id[:8]
		}
		duration := ""
		if wf.StartedAt != nil && wf.CompletedAt != nil {
			d := wf.CompletedAt.Sub(*wf.StartedAt).Round(1000000000)
			duration = d.String()
		} else if wf.StartedAt != nil {
			d := time.Since(*wf.StartedAt).Round(1000000000)
			duration = d.String() + "…"
		}
		rows[i] = []string{id, TruncateColumn(wf.Pipeline), wf.Status, FormatTimestamp(wf.CreatedAt), duration}
	}
	return renderTable(headers, rows)
}
