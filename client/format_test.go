package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"pgregory.net/rapid"
)

// Property 5: Table formatter produces aligned columns with all fields
func TestProperty_TableFormatterAlignment(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 10).Draw(rt, "n")
		tasks := make([]TaskSummary, n)
		for i := range tasks {
			tasks[i] = TaskSummary{
				ID:        rapid.StringMatching("[a-z0-9]{1,60}").Draw(rt, fmt.Sprintf("id%d", i)),
				Status:    rapid.SampledFrom([]string{"pending", "running", "completed", "failed"}).Draw(rt, "status"),
				Agent:     rapid.SampledFrom([]string{"kiro", "claude"}).Draw(rt, "agent"),
				CreatedAt: time.Unix(int64(i)*1000, 0).UTC(),
			}
		}

		output := FormatTasksTable(tasks)
		lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")

		// Must have exactly 1 header row + n data rows.
		if len(lines) != n+1 {
			rt.Fatalf("expected %d lines (1 header + %d data), got %d\n%s", n+1, n, len(lines), output)
		}

		// All lines must have the same rune length — guarantees column alignment.
		headerLen := utf8.RuneCountInString(lines[0])
		for j, line := range lines[1:] {
			got := utf8.RuneCountInString(line)
			if got != headerLen {
				rt.Fatalf("data row %d rune length %d differs from header %d", j, got, headerLen)
			}
		}

		// Each data row must contain all expected field values.
		for i, task := range tasks {
			row := lines[i+1]
			truncID := TruncateColumn(task.ID)
			if !strings.Contains(row, truncID) {
				rt.Fatalf("row %d missing ID %q (truncated to %q): %q", i, task.ID, truncID, row)
			}
			if !strings.Contains(row, task.Status) {
				rt.Fatalf("row %d missing status %q: %q", i, task.Status, row)
			}
			if !strings.Contains(row, task.Agent) {
				rt.Fatalf("row %d missing agent %q: %q", i, task.Agent, row)
			}
		}
	})
}

// Property 6: Color output is disabled when NO_COLOR is set or output is non-TTY
func TestProperty_ColorOutputControl(t *testing.T) {
	// Save and restore NO_COLOR across the entire property run.
	origVal, hadNoColor := os.LookupEnv("NO_COLOR")
	t.Cleanup(func() {
		if hadNoColor {
			os.Setenv("NO_COLOR", origVal)
		} else {
			os.Unsetenv("NO_COLOR")
		}
	})

	rapid.Check(t, func(rt *rapid.T) {
		isTTY := rapid.SampledFrom([]bool{true, false}).Draw(rt, "isTTY")
		hasNoColor := rapid.SampledFrom([]bool{true, false}).Draw(rt, "hasNoColor")

		if hasNoColor {
			// Set NO_COLOR to empty string — still disables colors per spec.
			os.Setenv("NO_COLOR", "")
		} else {
			os.Unsetenv("NO_COLOR")
		}

		fmtr := &Formatter{isTerminal: func(*os.File) bool { return isTTY }}
		colorsEnabled := isTTY && !hasNoColor

		type colorCase struct {
			name string
			fn   func(*os.File, string) string
			code string
		}
		cases := []colorCase{
			{"Green", fmtr.Green, ansiGreen},
			{"Red", fmtr.Red, ansiRed},
			{"Yellow", fmtr.Yellow, ansiYellow},
			{"Cyan", fmtr.Cyan, ansiCyan},
		}

		msg := "hello"
		for _, c := range cases {
			result := c.fn(os.Stdout, msg)
			if !colorsEnabled {
				if strings.Contains(result, "\x1b[") {
					rt.Fatalf("%s: ANSI escape found when colors disabled (isTTY=%v, NO_COLOR=%v): %q",
						c.name, isTTY, hasNoColor, result)
				}
				if result != msg {
					rt.Fatalf("%s: expected plain %q, got %q", c.name, msg, result)
				}
			} else {
				if !strings.Contains(result, c.code) {
					rt.Fatalf("%s: expected color code %q in output: %q", c.name, c.code, result)
				}
				if !strings.Contains(result, ansiReset) {
					rt.Fatalf("%s: expected reset code in output: %q", c.name, result)
				}
			}
		}
	})
}

// Property 7: Column value truncation at 36 characters
func TestProperty_ColumnTruncation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		length := rapid.IntRange(0, 200).Draw(rt, "length")
		// Use a fixed character so the result is fully predictable.
		s := strings.Repeat("a", length)

		result := TruncateColumn(s)
		resultLen := utf8.RuneCountInString(result)

		if length <= maxColumnWidth {
			if result != s {
				rt.Fatalf("string of length %d was modified: got %q", length, result)
			}
		} else {
			if resultLen != maxColumnWidth {
				rt.Fatalf("truncated length %d != %d for input length %d", resultLen, maxColumnWidth, length)
			}
			runes := []rune(result)
			if runes[maxColumnWidth-1] != '…' {
				rt.Fatalf("last rune is %q, want '…' for input length %d", string(runes[maxColumnWidth-1]), length)
			}
			// The truncated prefix must be the original first 35 characters.
			prefix := string(runes[:maxColumnWidth-1])
			if prefix != s[:maxColumnWidth-1] {
				rt.Fatalf("truncated prefix %q != original prefix %q", prefix, s[:maxColumnWidth-1])
			}
		}
	})
}
