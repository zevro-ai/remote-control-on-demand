package session

import (
	"strings"
	"sync"
)

type RingBuffer struct {
	mu    sync.Mutex
	lines []string
	size  int
	pos   int
	full  bool
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		lines: make([]string, size),
		size:  size,
	}
}

func (rb *RingBuffer) Write(p []byte) (n int, err error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	text := string(p)
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		rb.lines[rb.pos] = line
		rb.pos = (rb.pos + 1) % rb.size
		if rb.pos == 0 {
			rb.full = true
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
