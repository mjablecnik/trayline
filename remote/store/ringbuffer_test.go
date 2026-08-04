package store

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Feature: 010-dashboard-workflow-runner, Property 9: Ring buffer size invariant
func TestRingBuffer_SizeInvariant(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxSize := rapid.IntRange(1, 64).Draw(t, "maxSize")
		chunks := rapid.SliceOfN(rapid.SliceOfN(rapid.ByteMin(1), 0, 20), 0, 20).Draw(t, "chunks")

		rb := NewRingBuffer(maxSize)
		var total int
		for _, c := range chunks {
			n, err := rb.Write(c)
			if err != nil {
				t.Fatalf("Write returned error: %v", err)
			}
			if n != len(c) {
				t.Fatalf("Write returned %d, expected %d", n, len(c))
			}
			total += len(c)
		}

		content := rb.String()
		if len(content) > maxSize {
			t.Fatalf("buffer content length %d exceeds maxSize %d", len(content), maxSize)
		}

		var want []byte
		for _, c := range chunks {
			want = append(want, c...)
		}
		if total > maxSize {
			want = want[total-maxSize:]
			if !rb.Wrapped() {
				t.Fatalf("expected Wrapped() to report true after writing %d bytes > maxSize %d", total, maxSize)
			}
		}
		if content != string(want) {
			t.Fatalf("buffer content mismatch: got %q, want %q", content, string(want))
		}
	})
}

func TestRingBuffer_EmptyBuffer(t *testing.T) {
	rb := NewRingBuffer(16)
	if rb.String() != "" {
		t.Errorf("expected empty string for unwritten buffer, got %q", rb.String())
	}
	if rb.Wrapped() {
		t.Error("expected Wrapped() false for unwritten buffer")
	}
}

func TestRingBuffer_ExactFitDoesNotWrap(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Write([]byte("abcde"))
	if rb.Wrapped() {
		t.Error("expected Wrapped() false when written content exactly fits maxSize")
	}
	if rb.String() != "abcde" {
		t.Errorf("expected %q, got %q", "abcde", rb.String())
	}
}

func TestRingBuffer_SingleWriteLargerThanMax(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Write([]byte("abcdefgh"))
	if !rb.Wrapped() {
		t.Error("expected Wrapped() true after a single write larger than maxSize")
	}
	if rb.String() != "efgh" {
		t.Errorf("expected tail %q, got %q", "efgh", rb.String())
	}
}

func TestRingBuffer_MultipleSmallWritesWrap(t *testing.T) {
	rb := NewRingBuffer(5)
	for _, s := range []string{"ab", "cd", "ef", "gh"} {
		rb.Write([]byte(s))
	}
	if !rb.Wrapped() {
		t.Error("expected Wrapped() true after writing more than maxSize total bytes")
	}
	want := "defgh"
	if rb.String() != want {
		t.Errorf("expected %q, got %q", want, rb.String())
	}
}

func TestRingBuffer_ConcurrentWriteAndRead(t *testing.T) {
	rb := NewRingBuffer(64)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			rb.Write([]byte(strings.Repeat("x", 3)))
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		_ = rb.String()
		_ = rb.Wrapped()
	}
	<-done
}
