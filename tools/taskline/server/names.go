package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
)

const idCharset = "abcdefghijklmnopqrstuvwxyz0123456789"

var nameAdjectives = []string{
	"brave", "calm", "eager", "fancy", "gentle", "happy", "jolly", "kind",
	"lively", "mighty", "noble", "proud", "quiet", "rapid", "silly", "tidy",
	"upbeat", "vivid", "witty", "zealous", "amber", "bold", "clever", "daring",
	"epic", "fierce", "grand", "humble", "icy", "keen", "lucky", "merry",
	"nimble", "odd", "plucky", "quick", "royal", "sturdy", "trusty", "wise",
}

var nameNouns = []string{
	"tiger", "river", "falcon", "meadow", "comet", "harbor", "canyon", "otter",
	"summit", "willow", "ember", "glacier", "heron", "island", "juniper", "lagoon",
	"maple", "nebula", "oasis", "panther", "quarry", "ridge", "sparrow", "thicket",
	"valley", "wren", "boulder", "cascade", "delta", "forest", "grove", "horizon",
	"inlet", "jungle", "kestrel", "lantern", "mesa", "orchard", "pebble", "quail",
}

// NameGenerator produces unique Task_IDs and Task_Names for a single server
// session, tracking every value it has ever handed out (or that a caller has
// reserved) so removed tasks never have their identifiers reused.
type NameGenerator struct {
	mu        sync.Mutex
	usedIDs   map[string]bool
	usedNames map[string]bool
}

// NewNameGenerator returns an initialized NameGenerator with empty used sets.
func NewNameGenerator() *NameGenerator {
	return &NameGenerator{
		usedIDs:   make(map[string]bool),
		usedNames: make(map[string]bool),
	}
}

// GenerateID returns an 8-character lowercase alphanumeric string that has
// not been previously generated or reserved within this session.
func (g *NameGenerator) GenerateID() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	for {
		id := randomID()
		if !g.usedIDs[id] {
			g.usedIDs[id] = true
			return id
		}
	}
}

// GenerateName returns a Docker-style "adjective-noun" name that has not been
// previously generated or reserved within this session.
func (g *NameGenerator) GenerateName() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	for {
		name := nameAdjectives[rand.IntN(len(nameAdjectives))] + "-" + nameNouns[rand.IntN(len(nameNouns))]
		if !g.usedNames[name] {
			g.usedNames[name] = true
			return name
		}
	}
}

// ReserveName marks a user-provided name as used so it can never be
// auto-generated or reused later, returning false if it was already taken.
func (g *NameGenerator) ReserveName(name string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.usedNames[name] {
		return false
	}
	g.usedNames[name] = true
	return true
}

// MarkUsed records id and name as already used, e.g. when restoring Tasks
// persisted from a State_File, so they are never handed out again by
// GenerateID or GenerateName.
func (g *NameGenerator) MarkUsed(id, name string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if id != "" {
		g.usedIDs[id] = true
	}
	if name != "" {
		g.usedNames[name] = true
	}
}

func randomID() string {
	b := make([]byte, 8)
	for i := range b {
		b[i] = idCharset[rand.IntN(len(idCharset))]
	}
	return string(b)
}

// ValidateName checks a user-provided Task_Name against the naming rules:
// at most 64 characters, only lowercase letters, digits, and hyphens, and
// starting with a lowercase letter.
func ValidateName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("name must not be empty")
	}
	if len(name) > 64 {
		return fmt.Errorf("name must not exceed 64 characters")
	}
	first := name[0]
	if first < 'a' || first > 'z' {
		return fmt.Errorf("name must start with a lowercase letter")
	}
	if strings.IndexFunc(name, func(r rune) bool {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		return !isLower && !isDigit && r != '-'
	}) != -1 {
		return fmt.Errorf("name may only contain lowercase letters, digits, and hyphens")
	}
	return nil
}
