package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Feature: taskline, Property 15: Command truncation
//
// For any string, if its display length exceeds 40 characters, the truncated
// output shall be exactly 40 characters with the last character being "…"
// (ellipsis). If its display length is <= 40 characters, the output shall
// equal the original string unchanged.
func TestProperty_CommandTruncation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[a-zA-Z0-9 _/.-]{0,80}`).Draw(t, "s")

		out := TruncateCommand(s)
		outRunes := []rune(out)
		inLen := len([]rune(s))

		if inLen <= maxCommandDisplayLen {
			if out != s {
				t.Fatalf("expected unchanged string for input of length %d, got %q from %q", inLen, out, s)
			}
			return
		}

		if len(outRunes) != maxCommandDisplayLen {
			t.Fatalf("expected truncated output length %d, got %d (%q)", maxCommandDisplayLen, len(outRunes), out)
		}
		if outRunes[len(outRunes)-1] != []rune(ellipsis)[0] {
			t.Fatalf("expected last rune to be ellipsis %q, got %q", ellipsis, string(outRunes[len(outRunes)-1]))
		}
	})
}

// Feature: taskline, Property 16: Timestamp formatting
//
// For any valid RFC 3339 timestamp, the CLI formatter shall produce a string
// matching the pattern "YYYY-MM-DD HH:MM" (using the local timezone), where
// YYYY is a 4-digit year, MM is a 2-digit month, DD is a 2-digit day, HH is a
// 2-digit hour (00-23), and MM is a 2-digit minute.
func TestProperty_TimestampFormatting(t *testing.T) {
	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$`)

	rapid.Check(t, func(t *rapid.T) {
		// Range covers 1970-01-01 to 2100-01-01, always producing a 4-digit year.
		sec := rapid.Int64Range(0, 4102444800).Draw(t, "sec")
		nsec := rapid.Int64Range(0, 999999999).Draw(t, "nsec")
		ts := time.Unix(sec, nsec)

		out := FormatTimestamp(ts)
		if !pattern.MatchString(out) {
			t.Fatalf("expected output matching %q, got %q", pattern.String(), out)
		}

		parsed, err := time.ParseInLocation("2006-01-02 15:04", out, time.Local)
		if err != nil {
			t.Fatalf("formatted timestamp %q does not parse back: %v", out, err)
		}
		expected := ts.Local().Truncate(time.Minute)
		if !parsed.Equal(expected) {
			t.Fatalf("expected formatted timestamp to represent %v, got %v (%q)", expected, parsed, out)
		}
	})
}

func TestFormatTaskList_Empty(t *testing.T) {
	out := FormatTaskList(nil, false)
	if out != noTasksMessage {
		t.Fatalf("expected %q, got %q", noTasksMessage, out)
	}
}

func TestFormatTaskList_HeaderAndColumnPadding(t *testing.T) {
	tasks := []TaskListItem{
		{Position: 1, ID: "short", Name: "n", Command: "echo hi", Status: "pending", CreatedAt: time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)},
		{Position: 2, ID: "a-much-longer-id-value", Name: "name2", Command: "sleep 1", Status: "running", CreatedAt: time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC)},
	}

	out := FormatTaskList(tasks, false)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 rows), got %d: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "#") || !strings.Contains(lines[0], "ID") || !strings.Contains(lines[0], "STATUS") {
		t.Fatalf("expected header row with column names, got %q", lines[0])
	}

	idWidth := len([]rune("a-much-longer-id-value"))
	idColRow0 := lines[1][strings.Index(lines[1], "short") : strings.Index(lines[1], "short")+idWidth]
	if len([]rune(idColRow0)) != idWidth {
		t.Fatalf("expected ID column padded to width %d, got %q", idWidth, idColRow0)
	}
	if !strings.Contains(lines[1], "short") || !strings.Contains(lines[2], "a-much-longer-id-value") {
		t.Fatalf("expected both rows present, got %q", out)
	}
}

func TestFormatTaskList_NoColorHasNoANSI(t *testing.T) {
	tasks := []TaskListItem{
		{Position: 1, ID: "id1", Name: "n1", Command: "echo hi", Status: "running", CreatedAt: time.Now()},
		{Position: 2, ID: "id2", Name: "n2", Command: "echo bye", Status: "failed", CreatedAt: time.Now()},
	}

	out := FormatTaskList(tasks, false)
	if strings.Contains(out, "\033[") {
		t.Fatalf("expected no ANSI escape codes with color=false, got %q", out)
	}
}

func TestFormatTaskList_ColorColorizesStatusWithReset(t *testing.T) {
	tasks := []TaskListItem{
		{Position: 1, ID: "id1", Name: "n1", Command: "echo hi", Status: "running", CreatedAt: time.Now()},
	}

	out := FormatTaskList(tasks, true)
	want := colorGreen + "running" + colorReset
	if !strings.Contains(out, want) {
		t.Fatalf("expected colorized status %q in output, got %q", want, out)
	}
}

func TestTruncateCommand_ShortUnchanged(t *testing.T) {
	s := "echo hello"
	if out := TruncateCommand(s); out != s {
		t.Fatalf("expected %q unchanged, got %q", s, out)
	}
}

func TestTruncateCommand_ExactlyMaxUnchanged(t *testing.T) {
	s := strings.Repeat("a", maxCommandDisplayLen)
	if out := TruncateCommand(s); out != s {
		t.Fatalf("expected %q unchanged at exactly max length, got %q", s, out)
	}
}

func TestTruncateCommand_LongerThanMaxTruncates(t *testing.T) {
	s := strings.Repeat("a", maxCommandDisplayLen+10)
	out := TruncateCommand(s)
	outRunes := []rune(out)
	if len(outRunes) != maxCommandDisplayLen {
		t.Fatalf("expected length %d, got %d (%q)", maxCommandDisplayLen, len(outRunes), out)
	}
	if !strings.HasSuffix(out, ellipsis) {
		t.Fatalf("expected suffix %q, got %q", ellipsis, out)
	}
}

func TestTruncateCommand_MultibyteRuneCounted(t *testing.T) {
	// 45 multibyte runes (each 3 bytes in UTF-8): byte length (135) would
	// exceed 40, but rune count is what matters.
	s := strings.Repeat("あ", 45)
	out := TruncateCommand(s)
	outRunes := []rune(out)
	if len(outRunes) != maxCommandDisplayLen {
		t.Fatalf("expected rune-counted length %d, got %d (%q)", maxCommandDisplayLen, len(outRunes), out)
	}
	if !strings.HasSuffix(out, ellipsis) {
		t.Fatalf("expected suffix %q, got %q", ellipsis, out)
	}
}

func TestStatusColor(t *testing.T) {
	cases := map[string]string{
		"running": colorGreen,
		"pending": colorYellow,
		"failed":  colorRed,
		"unknown": "",
		"":        "",
	}
	for status, want := range cases {
		if got := statusColor(status); got != want {
			t.Errorf("statusColor(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestColorEnabled_FalseWhenNoColorSet(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled() {
		t.Fatal("expected ColorEnabled to be false when NO_COLOR is set")
	}
}

func TestColorEnabled_FalseWhenStdoutNotCharDevice(t *testing.T) {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		old := os.Getenv("NO_COLOR")
		os.Unsetenv("NO_COLOR")
		t.Cleanup(func() { os.Setenv("NO_COLOR", old) })
	}

	// go test redirects os.Stdout to a non-terminal (pipe/file), so
	// isTerminal(os.Stdout) is false here regardless of NO_COLOR.
	if ColorEnabled() {
		t.Fatal("expected ColorEnabled to be false when stdout is not a char device")
	}
}

func TestPadRight(t *testing.T) {
	if got := padRight("ab", 5); got != "ab   " {
		t.Fatalf("expected padded string %q, got %q", "ab   ", got)
	}
	if got := padRight("abcdef", 3); got != "abcdef" {
		t.Fatalf("expected long string unchanged, got %q", got)
	}
	if got := padRight("abc", 3); got != "abc" {
		t.Fatalf("expected exact-width string unchanged, got %q", got)
	}
}
