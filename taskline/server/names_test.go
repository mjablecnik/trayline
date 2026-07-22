package main

import (
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

var nameFormatPattern = regexp.MustCompile(`^[a-z]+-[a-z]+$`)

// Feature: taskline, Property 5: Name generation format
//
// For any auto-generated Task_Name, it shall match the pattern [a-z]+-[a-z]+
// (a lowercase adjective, a hyphen, and a lowercase noun). The adjective and
// noun components shall each be at least 2 characters long.
func TestProperty_NameGenerationFormat(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := NewNameGenerator()
		name := g.GenerateName()

		if !nameFormatPattern.MatchString(name) {
			t.Fatalf("generated name %q does not match pattern [a-z]+-[a-z]+", name)
		}

		parts := strings.SplitN(name, "-", 2)
		if len(parts) != 2 {
			t.Fatalf("generated name %q does not split into two hyphenated parts", name)
		}
		if len(parts[0]) < 2 {
			t.Fatalf("adjective part %q of name %q is shorter than 2 characters", parts[0], name)
		}
		if len(parts[1]) < 2 {
			t.Fatalf("noun part %q of name %q is shorter than 2 characters", parts[1], name)
		}
	})
}

// Feature: taskline, Property 7: Name validation rules
//
// For any user-provided Task_Name, if the name exceeds 64 characters,
// contains characters other than lowercase letters, digits, or hyphens, or
// does not start with a lowercase letter, the request shall be rejected. If
// the name satisfies all constraints, it shall be accepted.
func TestProperty_NameValidationRules(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		valid := rapid.Bool().Draw(t, "valid")

		var name string
		if valid {
			name = rapid.StringMatching(`[a-z][a-z0-9-]{0,63}`).Draw(t, "validName")
		} else {
			kind := rapid.IntRange(0, 2).Draw(t, "invalidKind")
			switch kind {
			case 0:
				// Too long: 65+ chars, still otherwise well-formed.
				name = "a" + strings.Repeat("b", rapid.IntRange(64, 100).Draw(t, "extraLen"))
			case 1:
				// Contains a disallowed character (uppercase letter).
				name = "a" + rapid.StringMatching(`[a-z0-9-]{0,10}`).Draw(t, "middle") + "A"
			case 2:
				// Does not start with a lowercase letter.
				name = rapid.OneOf(rapid.Just("-abc"), rapid.Just("1abc"), rapid.Just("")).Draw(t, "badStart")
			}
		}

		err := ValidateName(name)
		if valid {
			if err != nil {
				t.Fatalf("expected name %q to be valid, got error: %v", name, err)
			}
		} else {
			if err == nil {
				t.Fatalf("expected name %q to be rejected", name)
			}
		}
	})
}
