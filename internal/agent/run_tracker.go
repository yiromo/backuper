package agent

import (
	"bytes"
	"io"
	"sync"
	"time"
)

// ActiveRun tracks an in-progress backup run.
type ActiveRun struct {
	ID          string
	Target      string
	Destination string
	StartedAt   time.Time
	Cancel      func()
	LogBuf      *threadSafeBuffer
	Done        chan struct{} // closed when run finishes
	Record      any          // *backup.Record, set on completion
	Err         error        // set on failure
}

// ActiveRunInfo is the JSON-serializable summary of an active run.
type ActiveRunInfo struct {
	ID          string `json:"id"`
	Target      string `json:"target"`
	Destination string `json:"destination"`
	StartedAt   string `json:"started_at"`
}

func (ar *ActiveRun) Info() ActiveRunInfo {
	return ActiveRunInfo{
		ID:          ar.ID,
		Target:      ar.Target,
		Destination: ar.Destination,
		StartedAt:   ar.StartedAt.Format(time.RFC3339),
	}
}

// threadSafeBuffer is a mutex-guarded buffer that notifies subscribers
// on each write, enabling SSE log streaming.
type threadSafeBuffer struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	notify chan struct{}
}

func newThreadSafeBuffer() *threadSafeBuffer {
	return &threadSafeBuffer{
		notify: make(chan struct{}, 1),
	}
}

func (b *threadSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	n, err := b.buf.Write(p)
	b.mu.Unlock()
	if err == nil && n > 0 {
		// Non-blocking signal.
		select {
		case b.notify <- struct{}{}:
		default:
		}
	}
	return n, err
}

func (b *threadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Notify returns a channel that receives a value whenever new data is written.
// The channel is buffered (size 1) and non-blocking on the write side.
func (b *threadSafeBuffer) Notify() <-chan struct{} {
	return b.notify
}

func (b *threadSafeBuffer) ReadFrom(offset int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.buf.String()
	if offset >= int64(len(s)) {
		return ""
	}
	return s[offset:]
}

func (b *threadSafeBuffer) Len() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int64(b.buf.Len())
}

var _ io.Writer = (*threadSafeBuffer)(nil)
