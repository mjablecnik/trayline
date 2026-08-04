package store

import "sync"

// RingBuffer captures up to maxSize bytes of written data, discarding the
// oldest content once the buffer is full. It implements io.Writer so it can
// be used directly as a sink for container log streaming.
type RingBuffer struct {
	mu       sync.Mutex
	buf      []byte
	maxSize  int
	writePos int
	wrapped  bool
}

// NewRingBuffer creates a RingBuffer that retains at most maxSize bytes.
func NewRingBuffer(maxSize int) *RingBuffer {
	return &RingBuffer{
		buf:     make([]byte, 0, maxSize),
		maxSize: maxSize,
	}
}

// Write appends p to the buffer, wrapping and discarding the oldest bytes
// once maxSize is reached. Always returns len(p), nil.
func (rb *RingBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n := len(p)

	// A single write larger than the whole buffer: only its tail survives,
	// discarding everything previously buffered.
	if len(p) > rb.maxSize {
		p = p[len(p)-rb.maxSize:]
		rb.buf = append(rb.buf[:0], p...)
		rb.writePos = 0
		rb.wrapped = true
		return n, nil
	}

	for len(p) > 0 {
		if len(rb.buf) < rb.maxSize {
			// Still filling the buffer for the first time.
			room := rb.maxSize - len(rb.buf)
			chunk := min(len(p), room)
			rb.buf = append(rb.buf, p[:chunk]...)
			p = p[chunk:]
			continue
		}

		// Buffer is full: overwrite oldest bytes starting at writePos.
		space := rb.maxSize - rb.writePos
		chunk := min(len(p), space)
		copy(rb.buf[rb.writePos:rb.writePos+chunk], p[:chunk])
		rb.writePos += chunk
		if rb.writePos >= rb.maxSize {
			rb.writePos = 0
		}
		p = p[chunk:]
		rb.wrapped = true
	}

	return n, nil
}

// String returns the buffered content in chronological order.
func (rb *RingBuffer) String() string {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if !rb.wrapped {
		return string(rb.buf)
	}
	ordered := make([]byte, 0, len(rb.buf))
	ordered = append(ordered, rb.buf[rb.writePos:]...)
	ordered = append(ordered, rb.buf[:rb.writePos]...)
	return string(ordered)
}

// Wrapped reports whether the buffer has discarded any content due to
// exceeding maxSize.
func (rb *RingBuffer) Wrapped() bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.wrapped
}
