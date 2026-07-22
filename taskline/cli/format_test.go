package main

import (
	"regexp"
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
