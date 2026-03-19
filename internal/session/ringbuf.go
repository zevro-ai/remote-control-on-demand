package session

import (
	"strings"
	"sync"
)

type RingBuffer struct {
	mu          sync.Mutex
	lines       []string
	size        int
	pos         int
	full        bool
	subMu       sync.Mutex
	subscribers []func(line string)
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		lines: make([]string, size),
		size:  size,
	}
}

// Subscribe registers a callback for each line written to the buffer.
// fn MUST be non-blocking (push to channel or buffer).
// Returns an unsubscribe function.
func (rb *RingBuffer) Subscribe(fn func(line string)) func() {
	rb.subMu.Lock()
	defer rb.subMu.Unlock()
	rb.subscribers = append(rb.subscribers, fn)
	idx := len(rb.subscribers) - 1
	return func() {
		rb.subMu.Lock()
		defer rb.subMu.Unlock()
		rb.subscribers[idx] = nil
	}
}

func (rb *RingBuffer) Write(p []byte) (n int, err error) {
	rb.mu.Lock()

	text := string(p)
	var written []string
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		rb.lines[rb.pos] = line
		rb.pos = (rb.pos + 1) % rb.size
		if rb.pos == 0 {
			rb.full = true
		}
		written = append(written, line)
	}
	rb.mu.Unlock()

	if len(written) > 0 {
		rb.subMu.Lock()
		subs := make([]func(string), len(rb.subscribers))
		copy(subs, rb.subscribers)
		rb.subMu.Unlock()

		for _, line := range written {
			for _, fn := range subs {
				if fn != nil {
					fn(line)
				}
			}
		}
	}

	return len(p), nil
}

func (rb *RingBuffer) Lines(n int) []string {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	var total int
	if rb.full {
		total = rb.size
	} else {
		total = rb.pos
	}

	if n > total {
		n = total
	}

	result := make([]string, n)
	start := rb.pos - n
	if start < 0 {
		start += rb.size
	}
	for i := 0; i < n; i++ {
		result[i] = rb.lines[(start+i)%rb.size]
	}
	return result
}
