package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	noTasksMessage    = "No tasks in queue."
	noProjectsMessage = "No projects known to the server."
)

const (
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorReset  = "\033[0m"
)

// ColorEnabled reports whether colored output should be used. Colors are
// disabled when NO_COLOR is set to any value (Requirement 14.4) or when
// stdout is not a terminal.
func ColorEnabled() bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	return isTerminal(os.Stdout)
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// TruncateCommand returns cmd unchanged — the full command is always displayed.
func TruncateCommand(cmd string) string {
	return cmd
}

// FormatTimestamp formats t as "YYYY-MM-DD HH:MM" in the local timezone.
func FormatTimestamp(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}

func statusColor(status string) string {
	switch status {
	case "running":
		return colorGreen
	case "pending":
		return colorYellow
	case "failed":
		return colorRed
	default:
		return ""
	}
}

func queueStateColor(state string) string {
	switch state {
	case "running":
		return colorGreen
	case "halted":
		return colorRed
	default:
		return ""
	}
}

// FormatTaskList renders tasks as a column-aligned table with header row
// "#  ID  NAME  STATUS  CREATED  COMMAND", each column padded with trailing
// spaces to the width of its longest value. When tasks is empty, it returns
// a message instead of an empty table (Requirement 14.6). The STATUS column
// is colorized when color is true. COMMAND is placed last so long commands
// don't push other columns out of view.
func FormatTaskList(tasks []TaskListItem, color bool) string {
	if len(tasks) == 0 {
		return noTasksMessage
	}

	headers := []string{"#", "ID", "NAME", "STATUS", "CREATED", "COMMAND"}
	rows := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, []string{
			fmt.Sprintf("%d", t.Position),
			t.ID,
			t.Name,
			t.Status,
			FormatTimestamp(t.CreatedAt),
			TruncateCommand(t.Command),
		})
	}

	return formatTable(headers, rows, 3, statusColor, color)
}

// FormatProjectsList renders projects as a column-aligned table with header
// row "PROJECT  STATE  PENDING" (design.md "taskline projects Command").
// When projects is empty, it returns a message instead of an empty table,
// matching FormatTaskList's empty-state behavior.
func FormatProjectsList(projects []ProjectListItem, color bool) string {
	if len(projects) == 0 {
		return noProjectsMessage
	}

	headers := []string{"PROJECT", "STATE", "PENDING"}
	rows := make([][]string, 0, len(projects))
	for _, p := range projects {
		rows = append(rows, []string{
			p.Name,
			p.State,
			fmt.Sprintf("%d", p.PendingCount),
		})
	}

	return formatTable(headers, rows, 1, queueStateColor, color)
}

// formatTable renders headers+rows as a column-aligned table, each column
// padded with trailing spaces to the width of its longest value. The column
// at colorColIdx is colorized via colorFn when color is true.
func formatTable(headers []string, rows [][]string, colorColIdx int, colorFn func(string) string, color bool) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len([]rune(h))
	}
	for _, row := range rows {
		for i, cell := range row {
			if n := len([]rune(cell)); n > widths[i] {
				widths[i] = n
			}
		}
	}

	writeRow := func(b *strings.Builder, cells []string, colorize bool) {
		for i, cell := range cells {
			padded := padRight(cell, widths[i])
			if colorize && i == colorColIdx {
				if code := colorFn(cell); code != "" {
					padded = code + padded + colorReset
				}
			}
			b.WriteString(padded)
			if i < len(cells)-1 {
				b.WriteString("  ")
			}
		}
		b.WriteString("\n")
	}

	var b strings.Builder
	writeRow(&b, headers, false)
	for _, row := range rows {
		writeRow(&b, row, color)
	}

	return strings.TrimSuffix(b.String(), "\n")
}

func padRight(s string, width int) string {
	n := len([]rune(s))
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}
