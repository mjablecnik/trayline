package store

import (
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"
)

func TestSessionStore_AddAndGet(t *testing.T) {
	s := NewSessionStore()
	sess := &Session{ID: "s1", Agent: "claude", CreatedAt: time.Now(), LastMessageAt: time.Now()}
	s.Add(sess)

	got := s.Get("s1")
	if got == nil {
		t.Fatal("expected to get session s1, got nil")
	}
	if got.ID != "s1" {
		t.Errorf("expected ID s1, got %q", got.ID)
	}
}

func TestSessionStore_GetMiss(t *testing.T) {
	s := NewSessionStore()
	if got := s.Get("nonexistent"); got != nil {
		t.Errorf("expected nil for missing id, got %+v", got)
	}
}

func TestSessionStore_Remove(t *testing.T) {
	s := NewSessionStore()
	sess := &Session{ID: "s2", Agent: "claude", CreatedAt: time.Now(), LastMessageAt: time.Now()}
	s.Add(sess)
	s.Remove("s2")
	if got := s.Get("s2"); got != nil {
		t.Errorf("expected nil after Remove, got %+v", got)
	}
}

func TestSessionStore_UpdateFound(t *testing.T) {
	s := NewSessionStore()
	sess := &Session{ID: "s3", Agent: "claude", CreatedAt: time.Now(), LastMessageAt: time.Now()}
	s.Add(sess)

	ok := s.Update("s3", func(sess *Session) { sess.Agent = "kiro" })
	if !ok {
		t.Fatal("expected Update to return true for existing session")
	}
	if s.Get("s3").Agent != "kiro" {
		t.Errorf("expected agent kiro after update, got %q", s.Get("s3").Agent)
	}
}

func TestSessionStore_UpdateNotFound(t *testing.T) {
	s := NewSessionStore()
	ok := s.Update("missing", func(sess *Session) { sess.Agent = "kiro" })
	if ok {
		t.Error("expected Update to return false for unknown id")
	}
}

func TestSessionStore_AllCountMatchesInserts(t *testing.T) {
	s := NewSessionStore()
	for i := 0; i < 3; i++ {
		s.Add(&Session{
			ID:            string(rune('a' + i)),
			Agent:         "claude",
			CreatedAt:     time.Now(),
			LastMessageAt: time.Now(),
		})
	}
	all := s.All()
	if len(all) != 3 {
		t.Errorf("expected 3 sessions from All(), got %d", len(all))
	}
}

// Feature: project-ai-agent, Property 6: Session listing is project-filtered and time-sorted
func TestPropertyListByProjectFilterAndSort(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		projects := []string{"proj-a", "proj-b", "proj-c"}
		n := rapid.IntRange(0, 20).Draw(t, "n")
		target := rapid.SampledFrom(projects).Draw(t, "target")

		s := NewSessionStore()
		base := time.Now()
		wantCount := 0
		for i := 0; i < n; i++ {
			project := rapid.SampledFrom(projects).Draw(t, "project")
			offset := rapid.IntRange(0, 10000).Draw(t, "offset")
			sess := &Session{
				ID:            "sess-" + string(rune('a'+i%26)) + string(rune('A'+(i/26)%26)),
				Agent:         "claude",
				Project:       project,
				CreatedAt:     base,
				LastMessageAt: base.Add(time.Duration(offset) * time.Millisecond),
			}
			s.Add(sess)
			if project == target {
				wantCount++
			}
		}

		result := s.ListByProject(target)
		if result == nil {
			t.Fatal("expected non-nil slice from ListByProject")
		}
		if len(result) != wantCount {
			t.Fatalf("expected %d sessions for project %q, got %d", wantCount, target, len(result))
		}
		for i, sess := range result {
			if sess.Project != target {
				t.Fatalf("result[%d] has project %q, want %q", i, sess.Project, target)
			}
			if i > 0 && result[i-1].LastMessageAt.Before(sess.LastMessageAt) {
				t.Fatalf("result not sorted descending by LastMessageAt at index %d", i)
			}
		}
	})
}

func TestSessionStore_ConcurrentAddGet(t *testing.T) {
	s := NewSessionStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('A' + i%26))
			s.Add(&Session{ID: id, Agent: "claude", CreatedAt: time.Now(), LastMessageAt: time.Now()})
			_ = s.Get(id)
		}(i)
	}
	wg.Wait()
}
