package main

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"time"
)

// ProjectLog is a project's continuously-appended output log. It implements
// io.Writer so a Worker can use it directly as the output of the commands it
// executes (Requirement FR-3.1, FR-3.3): every write is split into lines,
// each prefixed with a timestamp and the name of the task currently
// executing (Requirement FR-3.5), appended to the log file, and broadcast to
// any active SSE subscribers (Requirement FR-4.5, NFR-4.2).
type ProjectLog struct {
	mu          sync.Mutex
	file        *os.File
	subs        map[chan []byte]struct{}
	currentTask string
	buf         []byte
}

// NewProjectLog opens (creating if necessary) the log file at path for
// appending and returns a ready-to-use ProjectLog. The file is never
// truncated or rotated (NFR-4.1) — output from every task the project has
// ever run accumulates in the same file.
func NewProjectLog(path string) (*ProjectLog, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return &ProjectLog{file: f, subs: make(map[chan []byte]struct{})}, nil
}

// SetCurrentTask records name as the task identifier used to prefix
// subsequently written lines. The Worker calls this before executing each
// Task.
func (l *ProjectLog) SetCurrentTask(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.currentTask = name
}

// Write implements io.Writer. p may contain any number of complete or
// partial lines; complete lines are formatted and emitted immediately, and
// any trailing partial line is buffered until a later Write completes it (or
// Close flushes it). Write always reports success for the full length of p,
// matching the exec.Cmd contract that a Writer must not fail piped
// stdout/stderr.
func (l *ProjectLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.buf = append(l.buf, p...)
	for {
		idx := bytes.IndexByte(l.buf, '\n')
		if idx < 0 {
			break
		}
		line := l.buf[:idx]
		l.buf = l.buf[idx+1:]
		l.emitLocked(line)
	}
	return len(p), nil
}

// emitLocked formats line with a timestamp and task prefix, appends it to
// the log file, and broadcasts it to every active subscriber. Callers must
// hold l.mu.
func (l *ProjectLog) emitLocked(line []byte) {
	formatted := fmt.Sprintf("[%s] [%s] %s\n", time.Now().UTC().Format(time.RFC3339), l.currentTask, line)
	data := []byte(formatted)

	if _, err := l.file.Write(data); err != nil {
		logError("failed to write project log: %v", err)
	}

	for ch := range l.subs {
		select {
		case ch <- data:
		default:
			// Subscriber isn't keeping up; drop the line for it rather than
			// blocking log writes for every other reader and the task itself.
		}
	}
}

// Subscribe registers a new SSE subscriber and returns the channel it will
// receive newly written, formatted log lines on. The returned channel must
// be passed to Unsubscribe when the caller is done reading from it.
func (l *ProjectLog) Subscribe() chan []byte {
	ch := make(chan []byte, 64)
	l.mu.Lock()
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
	return ch
}

// Unsubscribe removes ch from the set of active subscribers and closes it.
func (l *ProjectLog) Unsubscribe(ch chan []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.subs[ch]; !ok {
		return
	}
	delete(l.subs, ch)
	close(ch)
}

// Close flushes any buffered partial line and closes the underlying log
// file.
func (l *ProjectLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) > 0 {
		l.emitLocked(l.buf)
		l.buf = nil
	}
	return l.file.Close()
}
