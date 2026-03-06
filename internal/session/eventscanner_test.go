package session

import (
	"bytes"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/config"
)

func TestEventScanner_PassThrough(t *testing.T) {
	var buf bytes.Buffer
	es := newEventScanner(&buf, nil, nil, nil, nil)
	defer es.Stop()

	data := []byte("hello world\n")
	n, err := es.Write(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}
	if buf.String() != "hello world\n" {
		t.Errorf("expected %q in buffer, got %q", "hello world\n", buf.String())
	}
}

func TestEventScanner_OncePatternFiresOnce(t *testing.T) {
	var buf bytes.Buffer
	var count atomic.Int32

	cfg := &config.NotificationsConfig{
		Patterns: []config.PatternConfig{
			{Name: "done", Regex: "task completed", Once: true},
		},
	}

	es := newEventScanner(&buf, cfg, func(name, match string) {
		count.Add(1)
		if name != "done" {
			t.Errorf("expected pattern name 'done', got %q", name)
		}
	}, nil, nil)
	defer es.Stop()

	es.Write([]byte("task completed\n"))
	es.Write([]byte("task completed again\n"))

	if c := count.Load(); c != 1 {
		t.Errorf("expected callback to fire once, got %d", c)
	}
}

func TestEventScanner_RepeatablePatternWithThrottle(t *testing.T) {
	var buf bytes.Buffer
	var count atomic.Int32

	cfg := &config.NotificationsConfig{
		Patterns: []config.PatternConfig{
			{Name: "error", Regex: "ERROR:", Once: false},
		},
	}

	es := newEventScanner(&buf, cfg, func(name, match string) {
		count.Add(1)
	}, nil, nil)
	defer es.Stop()
	// Use a very short throttle for testing
	es.throttleInterval = 50 * time.Millisecond

	// First match fires immediately
	es.Write([]byte("ERROR: something\n"))
	if c := count.Load(); c != 1 {
		t.Fatalf("expected 1 callback after first match, got %d", c)
	}

	// Second match within throttle window is suppressed
	es.Write([]byte("ERROR: another\n"))
	if c := count.Load(); c != 1 {
		t.Errorf("expected callback still at 1 (throttled), got %d", c)
	}

	// Wait for throttle to expire, then match again
	time.Sleep(60 * time.Millisecond)
	es.Write([]byte("ERROR: third\n"))
	if c := count.Load(); c != 2 {
		t.Errorf("expected 2 callbacks after throttle expired, got %d", c)
	}
}

func TestEventScanner_MultiplePatterns(t *testing.T) {
	var buf bytes.Buffer
	matched := make(map[string]string)

	cfg := &config.NotificationsConfig{
		Patterns: []config.PatternConfig{
			{Name: "done", Regex: "DONE", Once: true},
			{Name: "error", Regex: "ERROR", Once: true},
		},
	}

	es := newEventScanner(&buf, cfg, func(name, match string) {
		matched[name] = match
	}, nil, nil)
	defer es.Stop()

	es.Write([]byte("DONE\n"))
	if _, ok := matched["done"]; !ok {
		t.Error("expected 'done' pattern to match")
	}
	if _, ok := matched["error"]; ok {
		t.Error("did not expect 'error' pattern to match")
	}

	es.Write([]byte("ERROR\n"))
	if _, ok := matched["error"]; !ok {
		t.Error("expected 'error' pattern to match")
	}
}

func TestEventScanner_IdleFires(t *testing.T) {
	var buf bytes.Buffer
	idleCh := make(chan struct{}, 1)

	cfg := &config.NotificationsConfig{
		IdleTimeout: config.Duration(50 * time.Millisecond),
	}

	es := newEventScanner(&buf, cfg, nil, nil, func() {
		idleCh <- struct{}{}
	})
	defer es.Stop()

	select {
	case <-idleCh:
		// success
	case <-time.After(1 * time.Second):
		t.Fatal("idle callback did not fire")
	}
}

func TestEventScanner_IdleResetsOnWrite(t *testing.T) {
	var buf bytes.Buffer
	var idleCount atomic.Int32

	cfg := &config.NotificationsConfig{
		IdleTimeout: config.Duration(100 * time.Millisecond),
	}

	es := newEventScanner(&buf, cfg, nil, nil, func() {
		idleCount.Add(1)
	})
	defer es.Stop()

	// Write every 50ms for 200ms — timer keeps resetting, should not fire
	for i := 0; i < 4; i++ {
		time.Sleep(50 * time.Millisecond)
		es.Write([]byte("output\n"))
	}

	if c := idleCount.Load(); c != 0 {
		t.Errorf("expected no idle callbacks while writing, got %d", c)
	}

	// Now stop writing and wait for idle
	time.Sleep(150 * time.Millisecond)
	if c := idleCount.Load(); c != 1 {
		t.Errorf("expected 1 idle callback after stop, got %d", c)
	}
}

func TestEventScanner_IdleFiresOncePerPeriod(t *testing.T) {
	var buf bytes.Buffer
	var idleCount atomic.Int32

	cfg := &config.NotificationsConfig{
		IdleTimeout: config.Duration(50 * time.Millisecond),
	}

	es := newEventScanner(&buf, cfg, nil, nil, func() {
		idleCount.Add(1)
	})
	defer es.Stop()

	// Wait for first idle fire
	time.Sleep(100 * time.Millisecond)
	if c := idleCount.Load(); c != 1 {
		t.Fatalf("expected 1 idle callback, got %d", c)
	}

	// Write to re-arm, then wait for second idle
	es.Write([]byte("more output\n"))
	time.Sleep(100 * time.Millisecond)
	if c := idleCount.Load(); c != 2 {
		t.Errorf("expected 2 idle callbacks after re-arm, got %d", c)
	}
}

func TestEventScanner_StopPreventsCallbacks(t *testing.T) {
	var buf bytes.Buffer
	var idleCount atomic.Int32

	cfg := &config.NotificationsConfig{
		IdleTimeout: config.Duration(50 * time.Millisecond),
	}

	es := newEventScanner(&buf, cfg, nil, nil, func() {
		idleCount.Add(1)
	})

	es.Stop()
	time.Sleep(100 * time.Millisecond)

	if c := idleCount.Load(); c != 0 {
		t.Errorf("expected no idle callbacks after Stop, got %d", c)
	}
}

func TestEventScanner_NilConfig(t *testing.T) {
	var buf bytes.Buffer
	es := newEventScanner(&buf, nil, nil, nil, nil)
	defer es.Stop()

	n, err := es.Write([]byte("test data\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 10 {
		t.Errorf("expected 10 bytes, got %d", n)
	}
	if buf.String() != "test data\n" {
		t.Errorf("unexpected buffer content: %q", buf.String())
	}
}
